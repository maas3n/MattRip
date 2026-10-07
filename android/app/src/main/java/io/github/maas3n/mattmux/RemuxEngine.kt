package io.github.maas3n.mattmux

import android.content.Context
import android.net.Uri
import android.provider.DocumentsContract
import android.provider.DocumentsContract.Document
import android.os.ParcelFileDescriptor
import java.io.File
import java.util.concurrent.atomic.AtomicBoolean
import java.util.concurrent.locks.ReentrantLock

data class RemuxResult(val outputUri: Uri, val title: Int, val durationMs: Long, val planJson: String)

data class TrackInfo(
    val index: Int, val kind: String, val codec: String, val language: String?, val title: String?,
    val width: Int, val height: Int, val channels: Int, val channelLayout: String?,
) {
    fun displayLabel(): String {
        val typeName = when (kind) { "video" -> "Video"; "audio" -> "Audio"; "subtitle" -> "Subtitle"; else -> kind }
        val details = mutableListOf<String>()
        if (width > 0 && height > 0) details += "${width}x${height}"
        if (channels > 0) details += if (channelLayout.isNullOrBlank()) "$channels ch" else "$channels ch ($channelLayout)"
        language?.takeIf { it.isNotBlank() }?.let { details += "[$it]" }
        title?.takeIf { it.isNotBlank() }?.let { details += it }
        return buildString {
            append(typeName); append("  #"); append(index); append("  "); append(codec)
            if (details.isNotEmpty()) { append("  "); append(details.joinToString("  ")) }
        }
    }
}

data class TrackProbeResult(val title: Int, val tracks: List<TrackInfo>, val details: String = "")
internal data class DvdTitleScanInfo(val title: Int, val durationMs: Long, val longest: Boolean)
internal data class DvdScanResult(val titles: List<DvdTitleScanInfo>, val longestTitle: Int)
internal data class DvdMetadataResult(
    val title: Int,
    val durationMs: Long,
    val chapterStartsMs: LongArray,
    val chapterEndsMs: LongArray,
    val tracks: List<TrackInfo>,
    val planJson: String,
)

interface RemuxEngine {
    val isAvailable: Boolean
    val unavailableReason: String?
    val runtimeInfo: String?
    fun probeTracks(context: Context, sourceUri: Uri): TrackProbeResult
    fun remux(context: Context, sourceUri: Uri, outputTreeUri: Uri, selectedStreamIndexes: IntArray? = null): RemuxResult
    fun cancel()
    fun setProgressListener(listener: ((Int) -> Unit)?)
}

class AndroidNativeRemuxEngine : RemuxEngine {
    companion object {
        private val remuxLock = ReentrantLock()
        private const val DVDNAV_TICKS_PER_MS = 90L
        private val loadFailure: Throwable? = runCatching {
            System.loadLibrary("avutil")
            System.loadLibrary("avcodec")
            System.loadLibrary("avformat")
            System.loadLibrary("udfread")
            System.loadLibrary("mattmux_jni")
        }.exceptionOrNull()
    }

    private val cancelled = AtomicBoolean(false)

    @Volatile
    private var progressListener: ((Int) -> Unit)? = null
    @Volatile private var stagingProgressListener: ((Int) -> Unit)? = null

    override val runtimeInfo: String? by lazy {
        if (loadFailure != null) null else runCatching { nativeVersionSummary() }
            .getOrElse { "Bundled FFmpeg libraries loaded (version probe failed)" }
    }

    override val isAvailable: Boolean
        get() = loadFailure == null

    override val unavailableReason: String?
        get() = loadFailure?.let { "Bundled FFmpeg runtime failed to load: ${it.javaClass.simpleName}" }

    override fun setProgressListener(listener: ((Int) -> Unit)?) {
        progressListener = listener
    }

    override fun cancel() {
        cancelled.set(true)
    }

    override fun probeTracks(context: Context, sourceUri: Uri): TrackProbeResult {
        val metadata = probeTitleMetadata(context, sourceUri, null)
        return TrackProbeResult(metadata.title, metadata.tracks)
    }

    internal fun probeTitleMetadata(context: Context, sourceUri: Uri, requestedTitle: Int?): DvdMetadataResult {
        check(isAvailable) { unavailableReason ?: "Remux engine unavailable" }
        check(remuxLock.tryLock()) { "Another native operation is still stopping. Try again shortly." }
        try {
            check(!cancelled.get()) { "Metadata read cancelled" }
            openTitle(context, sourceUri, requestedTitle).use { title ->
                val fds = IntArray(title.vobs.size) { title.vobs[it].fd }
                val starts = LongArray(title.plan.cells.size) { title.plan.cells[it].startSector }
                val ends = LongArray(title.plan.cells.size) { title.plan.cells[it].endSectorExclusive }
                val languageRecords = title.plan.streamLanguages.map { "${it.streamId}\t${it.language}" }.toTypedArray()
                val records = nativeProbeTracks(fds, starts, ends, title.isoHandle, title.plan.titleSet, languageRecords, title.plan.subtitlePalette)
                    ?: error("Could not probe DVD streams")
                return DvdMetadataResult(
                    title = title.plan.globalTitle,
                    durationMs = title.plan.durationMs,
                    chapterStartsMs = title.plan.chapterStartsMs.copyOf(),
                    chapterEndsMs = title.plan.chapterEndsMs.copyOf(),
                    tracks = records.map(::parseTrackRecord),
                    planJson = title.plan.diagnosticJson(),
                )
            }
        } finally {
            cancelled.set(false)
            remuxLock.unlock()
        }
    }

    internal fun scanTitles(context: Context, sourceUri: Uri): DvdScanResult {
        check(isAvailable) { unavailableReason ?: "Remux engine unavailable" }
        check(remuxLock.tryLock()) { "Another native operation is still stopping. Try again shortly." }
        try {
            check(!cancelled.get()) { "DVD scan cancelled" }
            return scanSourceWithDvdNav(context, sourceUri)
        } finally {
            cancelled.set(false)
            remuxLock.unlock()
        }
    }

    private fun parseTrackRecord(record: String): TrackInfo {
        val fields = record.split('\t', limit = 9)
        require(fields.size == 9) { "Invalid native track metadata" }
        fun optional(value: String): String? = value.takeUnless { it == "-" || it.isBlank() }
        return TrackInfo(
            index = fields[0].toInt(), kind = fields[1], codec = fields[2],
            language = optional(fields[3]), title = optional(fields[4]),
            width = fields[5].toInt(), height = fields[6].toInt(), channels = fields[7].toInt(),
            channelLayout = optional(fields[8]),
        )
    }

    override fun remux(context: Context, sourceUri: Uri, outputTreeUri: Uri, selectedStreamIndexes: IntArray?): RemuxResult =
        remuxTitle(context, sourceUri, outputTreeUri, null, selectedStreamIndexes, preserveChapters = true)

    internal fun remuxTitle(
        context: Context,
        sourceUri: Uri,
        outputTreeUri: Uri,
        requestedTitle: Int?,
        selectedStreamIndexes: IntArray? = null,
        preserveChapters: Boolean = true,
        outputName: String? = null,
    ): RemuxResult {
        check(isAvailable) { unavailableReason ?: "Remux engine unavailable" }
        require(DocumentsContract.isTreeUri(outputTreeUri)) { "Output must be a document-tree folder" }
        require(requestedTitle == null || requestedTitle > 0) { "DVD title must be greater than zero" }
        require(selectedStreamIndexes == null || selectedStreamIndexes.isNotEmpty()) { "Select at least one video, audio, or subtitle track before remuxing" }
        check(remuxLock.tryLock()) { "Another remux is still stopping. Try again shortly." }
        try {
            check(!cancelled.get()) { "Remux cancelled" }
            return remuxLocked(context, sourceUri, outputTreeUri, requestedTitle, selectedStreamIndexes, preserveChapters, outputName)
        } finally {
            cancelled.set(false)
            remuxLock.unlock()
        }
    }

    /** Losslessly materializes one DVD title into an app-private MKV for internal consumers. */
    internal fun remuxTitleToFile(
        context: Context,
        sourceUri: Uri,
        outputFile: File,
        requestedTitle: Int? = null,
        preserveChapters: Boolean = true,
        progress: ((Int) -> Unit)? = null,
    ): Int {
        check(isAvailable) { unavailableReason ?: "Remux engine unavailable" }
        require(requestedTitle == null || requestedTitle > 0) { "DVD title must be greater than zero" }
        check(remuxLock.tryLock()) { "Another native operation is still stopping. Try again shortly." }
        try {
            stagingProgressListener = progress
            check(!cancelled.get()) { "Remux cancelled" }
            outputFile.parentFile?.mkdirs()
            if (outputFile.exists()) check(outputFile.delete()) { "Could not replace temporary merger input" }
            openTitle(context, sourceUri, requestedTitle).use { title ->
                ParcelFileDescriptor.open(
                    outputFile,
                    ParcelFileDescriptor.MODE_CREATE or ParcelFileDescriptor.MODE_TRUNCATE or ParcelFileDescriptor.MODE_READ_WRITE,
                ).use { output ->
                    val fds = IntArray(title.vobs.size) { title.vobs[it].fd }
                    val starts = LongArray(title.plan.cells.size) { title.plan.cells[it].startSector }
                    val ends = LongArray(title.plan.cells.size) { title.plan.cells[it].endSectorExclusive }
                    val chapterStarts = if (preserveChapters) title.plan.chapterStartsMs else LongArray(0)
                    val chapterEnds = if (preserveChapters) title.plan.chapterEndsMs else LongArray(0)
                    val nativeError = nativeRemux(
                        fds, starts, ends, output.fd, chapterStarts, chapterEnds, null,
                        title.isoHandle, title.plan.titleSet,
                        title.plan.streamLanguages.map { "${it.streamId}\t${it.language}" }.toTypedArray(),
                        title.plan.subtitlePalette,
                    )
                    if (nativeError != null) throw IllegalStateException(nativeError)
                }
                check(!cancelled.get()) { "Remux cancelled" }
                check(outputFile.isFile && outputFile.length() > 0L) { "DVD title staging produced an empty file" }
                return title.plan.globalTitle
            }
        } catch (t: Throwable) {
            outputFile.delete()
            throw t
        } finally {
            stagingProgressListener = null
            cancelled.set(false)
            remuxLock.unlock()
        }
    }

    /** Extract the planned DVD cells directly to elementary/VOB outputs. */
    internal fun demuxTitleToDirectory(
        context: Context, sourceUri: Uri, directory: File, indexes: IntArray?,
        chapters: Boolean, vob: Boolean, requestedTitle: Int? = null, progress: (Int) -> Unit,
    ) {
        check(isAvailable) { unavailableReason ?: "Remux engine unavailable" }
        require(indexes == null || indexes.isNotEmpty()) { "Select at least one track" }
        check(remuxLock.tryLock()) { "Another native operation is still stopping. Try again shortly." }
        try {
            stagingProgressListener = progress
            check(!cancelled.get()) { "Demux cancelled" }
            openTitle(context, sourceUri, requestedTitle).use { title ->
                check(!cancelled.get()) { "Demux cancelled" }
                nativeDemux(
                    IntArray(title.vobs.size) { title.vobs[it].fd },
                    LongArray(title.plan.cells.size) { title.plan.cells[it].startSector },
                    LongArray(title.plan.cells.size) { title.plan.cells[it].endSectorExclusive },
                    directory.absolutePath,
                    if (chapters) title.plan.chapterStartsMs else LongArray(0),
                    if (chapters) title.plan.chapterEndsMs else LongArray(0),
                    indexes, title.isoHandle, title.plan.titleSet,
                    title.plan.streamLanguages.map { "${it.streamId}\t${it.language}" }.toTypedArray(),
                    title.plan.subtitlePalette, vob,
                )?.let { error(it) }
                check(!cancelled.get()) { "Demux cancelled" }
            }
        } finally {
            stagingProgressListener = null
            cancelled.set(false)
            remuxLock.unlock()
        }
    }

    private fun remuxLocked(
        context: Context,
        sourceUri: Uri,
        outputTreeUri: Uri,
        requestedTitle: Int?,
        selectedStreamIndexes: IntArray?,
        preserveChapters: Boolean,
        outputName: String?,
    ): RemuxResult {
        val resolver = context.contentResolver
        openTitle(context, sourceUri, requestedTitle).use { title ->
            check(!cancelled.get()) { "Remux cancelled" }
            android.util.Log.i("MattRipPlan", title.plan.diagnosticJson())
            val output = DvdDocumentOutput(resolver, outputTreeUri)
            val pending = output.create(title.plan.globalTitle, outputName)
            var remuxCompleted = false
            try {
                val fds = IntArray(title.vobs.size) { title.vobs[it].fd }
                val starts = LongArray(title.plan.cells.size) { title.plan.cells[it].startSector }
                val ends = LongArray(title.plan.cells.size) { title.plan.cells[it].endSectorExclusive }
                val chapterStarts = if (preserveChapters) title.plan.chapterStartsMs else LongArray(0)
                val chapterEnds = if (preserveChapters) title.plan.chapterEndsMs else LongArray(0)
                val nativeError = nativeRemux(
                    fds,
                    starts,
                    ends,
                    pending.descriptor.fd,
                    chapterStarts,
                    chapterEnds,
                    selectedStreamIndexes,
                    title.isoHandle,
                    title.plan.titleSet,
                    title.plan.streamLanguages.map { "${it.streamId}\t${it.language}" }.toTypedArray(),
                    title.plan.subtitlePalette,
                )
                if (nativeError != null) {
                    throw IllegalStateException(nativeError)
                }
                remuxCompleted = true
                check(!cancelled.get()) { "Remux cancelled" }
                val finalUri = output.commit(pending)
                return RemuxResult(finalUri, title.plan.globalTitle, title.plan.durationMs, title.plan.diagnosticJson())
            } catch (t: Throwable) {
                if (remuxCompleted) output.preserve(pending) else output.abort(pending)
                throw t
            }
        }
    }

    private class NativeTitle(
        val plan: DvdTitlePlan,
        val vobs: List<ParcelFileDescriptor>,
        val isoHandle: Long = 0,
        val cleanup: () -> Unit,
    ) : AutoCloseable {
        override fun close() = cleanup()
    }

    private fun parseDvdNavScan(values: LongArray): DvdScanResult {
        require(values.size >= 3) { "libdvdnav scanner returned invalid data" }
        val count = values[0].toInt()
        val bestTitle = values[1].toInt()
        require(count > 0 && bestTitle in 1..count) { "libdvdnav scanner found no usable titles" }
        require(values.size >= count + 3) { "libdvdnav scanner did not return per-title durations" }
        val titles = (1..count).map { title ->
            val ticks = values[title + 2].coerceAtLeast(0)
            DvdTitleScanInfo(title, ticks / DVDNAV_TICKS_PER_MS, title == bestTitle)
        }
        return DvdScanResult(titles, bestTitle)
    }

    private fun scanWithDvdNav(stageRoot: java.io.File): Int {
        val values = nativeScanDvdNav(stageRoot.absolutePath)
            ?: error("libdvdnav/libdvdread could not scan the staged DVD metadata")
        val result = parseDvdNavScan(values)
        android.util.Log.i(
            "MattRipDVDNav",
            "libdvdnav/libdvdread scanned ${result.titles.size} title(s); selected title ${result.longestTitle} as longest",
        )
        return result.longestTitle
    }

    private fun planWithDvdNav(stageRoot: java.io.File, globalTitle: Int): DvdTitlePlan {
        val records = nativePlanDvdNav(stageRoot.absolutePath, globalTitle)
            ?: error("libdvdnav/libdvdread could not build the selected DVD title plan")
        val plan = DvdNavPlanCodec.parse(records)
        require(plan.globalTitle == globalTitle) { "libdvdnav/libdvdread returned the wrong DVD title" }
        return plan
    }

    private fun scanSourceWithDvdNav(context: Context, uri: Uri): DvdScanResult {
        val resolver = context.contentResolver
        if (isDirectorySource(context, uri)) {
            val stageRoot = DvdNavScanner.stageTreeIfos(context, uri)
            return try {
                parseDvdNavScan(nativeScanDvdNav(stageRoot.absolutePath)
                    ?: error("libdvdnav/libdvdread could not scan the staged DVD metadata"))
            } finally {
                stageRoot.deleteRecursively()
            }
        }

        val handle = resolver.openFileDescriptor(uri, "r")?.use { nativeOpenIso(it.fd) }
            ?: error("Could not open ISO image")
        check(handle != 0L) { "Could not open UDF filesystem" }
        try {
            val vmg = nativeReadIsoIfo(handle, 0) ?: error("ISO has no VIDEO_TS/VIDEO_TS.IFO")
            val stageRoot = DvdNavScanner.stageIsoIfos(context, vmg) { titleSet -> nativeReadIsoIfo(handle, titleSet) }
            return try {
                parseDvdNavScan(nativeScanDvdNav(stageRoot.absolutePath)
                    ?: error("libdvdnav/libdvdread could not scan the staged DVD metadata"))
            } finally {
                stageRoot.deleteRecursively()
            }
        } finally {
            nativeCloseIso(handle)
        }
    }

    private fun isDirectorySource(context: Context, uri: Uri): Boolean {
        if (!DocumentsContract.isTreeUri(uri)) return false
        val documentUri = runCatching {
            DocumentsContract.buildDocumentUriUsingTree(uri, documentTreeRootId(uri))
        }.getOrElse { uri }
        return context.contentResolver.getType(documentUri) == Document.MIME_TYPE_DIR
    }

    private fun openTitle(context: Context, uri: Uri, requestedTitle: Int? = null): NativeTitle {
        require(requestedTitle == null || requestedTitle > 0) { "DVD title must be greater than zero" }
        val resolver = context.contentResolver
        if (isDirectorySource(context, uri)) {
            val stageRoot = DvdNavScanner.stageTreeIfos(context, uri)
            val plan = try {
                val title = requestedTitle ?: scanWithDvdNav(stageRoot)
                planWithDvdNav(stageRoot, title)
            } finally { stageRoot.deleteRecursively() }
            val title = DvdDocumentSource(resolver, uri).openPlan(plan)
            return NativeTitle(title.plan, title.vobs, cleanup = { title.close() })
        }

        val handle = resolver.openFileDescriptor(uri, "r")?.use { nativeOpenIso(it.fd) }
            ?: error("Could not open ISO image")
        check(handle != 0L) { "Could not open UDF filesystem" }
        try {
            val vmg = nativeReadIsoIfo(handle, 0) ?: error("ISO has no VIDEO_TS/VIDEO_TS.IFO")
            val stageRoot = DvdNavScanner.stageIsoIfos(context, vmg) { titleSet -> nativeReadIsoIfo(handle, titleSet) }
            val plan = try {
                val title = requestedTitle ?: scanWithDvdNav(stageRoot)
                planWithDvdNav(stageRoot, title)
            } finally { stageRoot.deleteRecursively() }
            return NativeTitle(plan, emptyList(), handle, cleanup = { nativeCloseIso(handle) })
        } catch (t: Throwable) {
            nativeCloseIso(handle)
            throw t
        }
    }

    @Suppress("unused")
    private fun isNativeCancelled(): Boolean = cancelled.get()

    @Suppress("unused")
    private fun onNativeProgress(percent: Int) {
        (stagingProgressListener ?: progressListener)?.invoke(percent.coerceIn(0, 100))
    }

    private external fun nativeVersionSummary(): String
    private external fun nativeScanDvdNav(path: String): LongArray?
    private external fun nativePlanDvdNav(path: String, globalTitle: Int): Array<String>?
    private external fun nativeOpenIso(fd: Int): Long
    private external fun nativeReadIsoIfo(handle: Long, titleSet: Int): ByteArray?
    private external fun nativeCloseIso(handle: Long)
    private external fun nativeProbeTracks(vobFds: IntArray, cellStartSectors: LongArray, cellEndSectors: LongArray, isoHandle: Long, titleSet: Int, streamLanguages: Array<String>, subtitlePalette: IntArray): Array<String>?
    private external fun nativeDemux(
        vobFds: IntArray, cellStartSectors: LongArray, cellEndSectors: LongArray,
        directory: String, chapterStartsMs: LongArray, chapterEndsMs: LongArray,
        selectedStreamIndexes: IntArray?, isoHandle: Long, titleSet: Int,
        streamLanguages: Array<String>, subtitlePalette: IntArray, vob: Boolean,
    ): String?
    private external fun nativeRemux(
        vobFds: IntArray,
        cellStartSectors: LongArray,
        cellEndSectors: LongArray,
        outputFd: Int,
        chapterStartsMs: LongArray,
        chapterEndsMs: LongArray,
        selectedStreamIndexes: IntArray?,
        isoHandle: Long,
        titleSet: Int,
        streamLanguages: Array<String>,
        subtitlePalette: IntArray,
    ): String?
}
