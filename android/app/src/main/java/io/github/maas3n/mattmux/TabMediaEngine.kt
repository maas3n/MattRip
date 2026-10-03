package io.github.maas3n.mattmux

import android.content.Context
import android.net.Uri
import android.provider.DocumentsContract
import android.provider.OpenableColumns
import java.io.File
import java.util.concurrent.atomic.AtomicBoolean

class MediaInfoNative {
    companion object { init { System.loadLibrary("mediainfo_jni") } }
    external fun metadata(path: String): ByteArray
}

/** DVD-tab operations only; the DVD batch and CLI source contracts stay separate. */
class TabMediaEngine(private val context: Context, private val dvd: AndroidNativeRemuxEngine) {
    private val native = AdvancedMergerNative()
    private val root = File(context.cacheDir, "dvd-tab-${System.nanoTime()}").apply { mkdirs() }
    private val destroyed = AtomicBoolean(false)
    @Volatile private var busy = false
    @Volatile private var preparingDVD = false
    private var cachedUri: Uri? = null
    private var cachedFile: File? = null
    private var dvdOriginalTracks: List<TrackInfo> = emptyList()

    fun isMKV(uri: Uri): Boolean {
        if (DocumentsContract.isTreeUri(uri)) return false
        val name = context.contentResolver.query(uri, arrayOf(OpenableColumns.DISPLAY_NAME), null, null, null)?.use { c -> if (c.moveToFirst()) c.getString(0) else "" } ?: ""
        return name.endsWith(".mkv", true)
    }

    private fun checkCancelled() { check(!destroyed.get() && !native.cancelled.get()) { "Operation cancelled" } }
    fun cancel() { native.cancelled.set(true); if (preparingDVD) dvd.cancel() }
    fun destroy() { destroyed.set(true); cancel(); if (!busy) root.deleteRecursively() }

    private fun <T> operation(work: () -> T): T {
        check(!destroyed.get()) { "Activity closed" }
        check(!busy) { "Another operation is running" }
        busy = true; native.cancelled.set(false)
        try { return work() } finally { busy = false; if (destroyed.get()) root.deleteRecursively() }
    }

    private fun prepare(uri: Uri, status: (String) -> Unit = {}): File {
        checkCancelled()
        cachedFile?.takeIf { cachedUri == uri && it.isFile }?.let { return it }
        cachedFile?.delete(); cachedFile = null; cachedUri = null; dvdOriginalTracks = emptyList()
        val file = File(root, "source.mkv")
        try {
            if (isMKV(uri)) {
                val size = context.contentResolver.query(uri, arrayOf(OpenableColumns.SIZE), null, null, null)?.use { c ->
                    if (c.moveToFirst() && !c.isNull(0)) c.getLong(0) else -1L
                } ?: -1L
                var copied = 0L
                var reported = -1L
                status("Copying MKV source…")
                context.contentResolver.openInputStream(uri)?.use { input -> file.outputStream().use { output ->
                    val buffer = ByteArray(256 * 1024)
                    while (true) {
                        checkCancelled(); val n = input.read(buffer); if (n < 0) break
                        output.write(buffer, 0, n); copied += n
                        val value = if (size > 0) (copied * 100 / size).coerceAtMost(99) else copied / (1024 * 1024)
                        if (value != reported) {
                            reported = value
                            status(if (size > 0) "Copying MKV source… $value%" else "Copying MKV source… $value MiB")
                        }
                    }
                } } ?: error("Cannot read MKV source")
            } else {
                preparingDVD = true
                try {
                    checkCancelled()
                    status("Scanning DVD streams…")
                    dvdOriginalTracks = dvd.probeTracks(context, uri).tracks
                    checkCancelled()
                    status("Preparing DVD title… 0%")
                    dvd.remuxTitleToFile(context, uri, file, preserveChapters = true) { percent ->
                        status("Preparing DVD title… $percent%")
                    }
                } finally { preparingDVD = false }
            }
            checkCancelled()
            cachedUri = uri; cachedFile = file
            return file
        } catch (error: Throwable) { file.delete(); throw error }
    }

    private fun tracks(file: File): List<TrackInfo> = native.probe(file.absolutePath).map { row ->
        val f = row.split('\t', limit = 9)
        require(f.size == 9) { "Invalid stream metadata" }
        fun optional(s: String): String? = s.takeUnless { it == "-" || it.isBlank() }
        TrackInfo(f[0].toInt(), f[1], f[2], optional(f[3]), optional(f[4]), f[5].toInt(), f[6].toInt(), f[7].toInt(), optional(f[8]))
    }

    fun probeMKV(uri: Uri): TrackProbeResult = operation {
        val file = prepare(uri)
        val details = String(MediaInfoNative().metadata(file.absolutePath), Charsets.UTF_8)
        check(details.isNotBlank()) { "MediaInfo returned no MKV metadata" }
        checkCancelled()
        TrackProbeResult(1, tracks(file).filter { it.kind in setOf("video", "audio", "subtitle") }, details)
    }

    private fun selected(file: File, indexes: IntArray?): IntArray {
        val staged = tracks(file).filter { it.kind in setOf("video", "audio", "subtitle") }
        val wanted = indexes?.toSet()
        if (dvdOriginalTracks.isNotEmpty()) {
            check(dvdOriginalTracks.size == staged.size && dvdOriginalTracks.zip(staged).all { (a,b) -> a.kind == b.kind && a.codec == b.codec }) { "DVD stream mapping changed while preparing the title" }
            check(wanted == null || wanted.all { i -> dvdOriginalTracks.any { it.index == i } }) { "Scan the source again" }
            return dvdOriginalTracks.zip(staged).filter { (original,_) -> wanted == null || original.index in wanted }.map { it.second.index }.toIntArray()
        }
        check(wanted == null || wanted.all { i -> staged.any { it.index == i } }) { "Scan the source again" }
        return staged.filter { wanted == null || it.index in wanted }.map { it.index }.toIntArray()
    }

    private fun copy(file: File, parent: Uri, mime: String, progress: (Int) -> Unit = {}): Uri {
        val document = DocumentsContract.createDocument(context.contentResolver, parent, mime, file.name) ?: error("Cannot create output file")
        try {
            context.contentResolver.openOutputStream(document, "w")?.use { out -> file.inputStream().use { input ->
                val buffer = ByteArray(256 * 1024)
                var copied = 0L
                var reported = -1
                while (true) {
                    checkCancelled(); val n = input.read(buffer); if (n < 0) break
                    out.write(buffer, 0, n); copied += n
                    val percent = (copied * 100 / file.length().coerceAtLeast(1)).toInt().coerceAtMost(99)
                    if (percent != reported) { reported = percent; progress(percent) }
                }
            } } ?: error("Cannot write output file")
            checkCancelled()
            return document
        } catch (error: Throwable) { runCatching { DocumentsContract.deleteDocument(context.contentResolver, document) }; throw error }
    }

    fun remuxMKV(uri: Uri, tree: Uri, indexes: IntArray?, chapters: Boolean): Uri = operation {
        val source = prepare(uri); val selection = selected(source, indexes)
        require(selection.isNotEmpty()) { "Select at least one track" }
        val output = File(root, "remux.mkv")
        try {
            val chapterSource = if (chapters && tracks(source).any { it.kind == "chapters" }) source.absolutePath else null
            native.mux(arrayOf(source.absolutePath), IntArray(selection.size), selection, chapterSource, output.absolutePath)?.let { error(it) }
            checkCancelled()
            copy(output, DocumentsContract.buildDocumentUriUsingTree(tree, DocumentsContract.getTreeDocumentId(tree)), "video/x-matroska")
        } finally { output.delete() }
    }

    fun demux(uri: Uri, tree: Uri, indexes: IntArray?, chapters: Boolean, vob: Boolean, status: (String) -> Unit = {}): Uri = operation {
        fun report(message: String) {
            android.util.Log.i("MattRipDemux", message)
            status(message)
        }
        val directory = File(root, "export-${System.nanoTime()}").apply { check(mkdir()) }
        var destination: Uri? = null
        try {
            if (isMKV(uri)) {
                val source = prepare(uri, ::report)
                val selection = selected(source, indexes)
                require(selection.isNotEmpty()) { "Select at least one track" }
                native.progressListener = { percent -> status("Extracting selected streams… $percent%") }
                native.demux(source.absolutePath, directory.absolutePath, selection, chapters, vob)?.let { error("Demux failed: $it") }
            } else {
                preparingDVD = true
                try {
                    checkCancelled()
                    report("Reading DVD title and stream metadata…")
                    dvd.demuxTitleToDirectory(context, uri, directory, indexes, chapters, vob) { percent ->
                        status("Extracting selected streams… $percent%")
                    }
                } finally { preparingDVD = false }
            }
            checkCancelled()
            val files = directory.listFiles()?.sortedBy { it.name } ?: error("No output files")
            check(files.isNotEmpty() && files.all { it.length() > 0 }) { "An exported stream is empty" }
            val parent = DocumentsContract.buildDocumentUriUsingTree(tree, DocumentsContract.getTreeDocumentId(tree))
            val folder = DocumentsContract.createDocument(context.contentResolver, parent, DocumentsContract.Document.MIME_TYPE_DIR, "Demux-${System.currentTimeMillis()}") ?: error("Cannot create export folder")
            destination = folder
            for ((index, file) in files.withIndex()) {
                report("Saving ${index + 1}/${files.size}: ${file.name}…")
                copy(file, folder, "application/octet-stream") { percent ->
                    status("Saving ${index + 1}/${files.size}: ${file.name} — $percent%")
                }
            }
            checkCancelled()
            report("Export saved: ${files.size} files")
            folder
        } catch (error: Throwable) {
            destination?.let { runCatching { DocumentsContract.deleteDocument(context.contentResolver, it) } }
            throw error
        } finally { native.progressListener = null; directory.deleteRecursively() }
    }
}
