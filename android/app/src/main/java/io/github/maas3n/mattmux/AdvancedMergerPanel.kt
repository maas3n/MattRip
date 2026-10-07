package io.github.maas3n.mattmux

import android.app.Activity
import android.app.AlertDialog
import android.content.Intent
import android.net.Uri
import android.provider.DocumentsContract
import android.provider.OpenableColumns
import android.view.View
import android.widget.Button
import android.widget.CheckBox
import android.widget.EditText
import android.widget.LinearLayout
import android.widget.ScrollView
import android.widget.TextView
import java.io.File
import java.util.concurrent.atomic.AtomicBoolean

/** Ordinary files use seekable copies; DVD DEMUX reads the original source directly. */
class AdvancedMergerPanel(private val activity: Activity) {
    companion object {
        const val FIRST_REQUEST = 8100
        private const val MEDIA_FOLDER_REQUEST = 8105
    }
    private val native = AdvancedMergerNative()
    private val dvdEngine = AndroidNativeRemuxEngine()
    private val root = File(activity.cacheDir, "merger-${System.nanoTime()}").apply { mkdirs() }
    private val controls = mutableListOf<View>()
    private val sources = linkedMapOf<Uri, File>()
    private data class Selection(val file: File, val track: TrackInfo, val check: CheckBox, val demuxIndex: Int = track.index)
    private data class Candidate(val file: File, val track: TrackInfo, val demuxIndex: Int)
    private data class DvdSource(val uri: Uri, val input: AdvancedDvdInput)
    private val selections = mutableListOf<Selection>()
    private val dvdSources = mutableMapOf<File, DvdSource>()
    private var chapters: File? = null
    private var output: Uri? = null
    @Volatile private var destroyed = false
    @Volatile private var busy = false
    private val status = TextView(activity).apply { text = "Choose files and select streams. Temporary space is needed for input copies and the output MKV." }
    private val chapterLabel = TextView(activity).apply { text = "No chapter override (optional FFMETADATA1 .txt or MKV with chapters)" }
    private val outputLabel = TextView(activity).apply { text = "Choose output folder" }
    private val filename = EditText(activity).apply { setSingleLine(); setText("merged.mkv") }
    private val streamList = LinearLayout(activity).apply { orientation = LinearLayout.VERTICAL }
    private val cancelButton = Button(activity).apply { text = "Cancel"; isEnabled = false; setOnClickListener { native.cancelled.set(true); dvdEngine.cancel() } }
    val view: View

    init {
        val padding = (24 * activity.resources.displayMetrics.density).toInt()
        val content = LinearLayout(activity).apply { orientation = LinearLayout.VERTICAL; setPadding(padding, padding, padding, padding) }
        fun button(label: String, action: () -> Unit) { content.addView(Button(activity).apply { text = label; setOnClickListener { action() }; controls += this }) }
        button("MEDIA(All streams included)") { chooseMedia() }
        button("ADD AUDIO(Only audio streams will be included)") { choose(1) }
        button("ADD SUBTITLE(Only subtitle streams will be Included)") { choose(2) }
        content.addView(TextView(activity).apply { text = "Select Streams — MEDIA includes every stream and embedded chapters. DVD / VIDEO_TS folders and DVD ISO inputs use the longest DVD title." })
        content.addView(streamList)
        button("Clear streams") { selections.clear(); streamList.removeAllViews() }
        button("ADD CHAPTER .txt FILE(FFMETADATA1 Format)") { choose(3) }
        content.addView(chapterLabel)
        button("Clear chapter override") { chapters = null; chapterLabel.text = "No chapter override" }
        button("CHOOSE OUTPUT FOLDER") { activity.startActivityForResult(Intent(Intent.ACTION_OPEN_DOCUMENT_TREE), FIRST_REQUEST + 4) }
        content.addView(outputLabel)
        content.addView(filename); controls += filename
        button("DEMUX") { chooseDemux() }
        button("MUX") { mux() }
        content.addView(cancelButton)
        content.addView(status)
        view = ScrollView(activity).apply { addView(content) }
    }

    private fun chooseMedia() {
        AlertDialog.Builder(activity)
            .setTitle("Add media")
            .setItems(arrayOf("Media file(s) / DVD ISO", "DVD / VIDEO_TS folder")) { _, choice ->
                if (choice == 0) {
                    choose(0)
                } else {
                    activity.startActivityForResult(
                        Intent(Intent.ACTION_OPEN_DOCUMENT_TREE).apply {
                            addFlags(Intent.FLAG_GRANT_READ_URI_PERMISSION or Intent.FLAG_GRANT_PERSISTABLE_URI_PERMISSION)
                        },
                        MEDIA_FOLDER_REQUEST,
                    )
                }
            }
            .setNegativeButton("Cancel", null)
            .show()
    }

    private fun choose(kind: Int) {
        val intent = Intent(Intent.ACTION_OPEN_DOCUMENT).apply {
            addCategory(Intent.CATEGORY_OPENABLE); type = "*/*"
            putExtra(Intent.EXTRA_ALLOW_MULTIPLE, kind != 3)
            addFlags(Intent.FLAG_GRANT_READ_URI_PERMISSION or Intent.FLAG_GRANT_PERSISTABLE_URI_PERMISSION)
        }
        activity.startActivityForResult(intent, FIRST_REQUEST + kind)
    }

    fun onResult(request: Int, result: Int, data: Intent?): Boolean {
        if (request !in FIRST_REQUEST..MEDIA_FOLDER_REQUEST) return false
        if (result != Activity.RESULT_OK || data == null || busy) return true
        if (request == FIRST_REQUEST + 4) {
            output = data.data; outputLabel.text = output.toString(); return true
        }
        val uris = mutableListOf<Uri>()
        data.clipData?.let { clip -> repeat(clip.itemCount) { uris += clip.getItemAt(it).uri } }
        if (uris.isEmpty()) data.data?.let { uris += it }
        if (uris.isEmpty()) return true
        val movieFolder = request == MEDIA_FOLDER_REQUEST
        val kind = if (movieFolder) 0 else request - FIRST_REQUEST
        run("Preparing inputs…") {
            // Copy ordinary files, but only read metadata for DVDs. Their MKV is deferred until MUX.
            val files = uris.map { uri -> sources[uri] ?: prepareInput(uri, kind, movieFolder).also { sources[uri] = it } }
            if (kind == 3) {
                val file = files.single()
                native.validateChapters(file.absolutePath)?.let { error(it) }
                return@run { chapters = file; chapterLabel.text = file.name; status.text = "Chapter override ready" }
            }
            val type = when (kind) { 0 -> null; 1 -> "audio"; else -> "subtitle" }
            val added = files.flatMap { file ->
                // A VobSub .sub is data for the matching .idx, not another selectable subtitle source.
                if (kind == 2 && file.extension.equals("sub", true) && files.any { it.nameWithoutExtension == file.nameWithoutExtension && it.extension.equals("idx", true) }) emptyList()
                else {
                    val allTracks = dvdSources[file]?.input?.tracks ?: native.probe(file.absolutePath).map(::parseTrack)
                    val tracks = allTracks.filter { type == null || it.kind == type }
                    require(tracks.isNotEmpty()) { "${file.name} contains no ${type ?: "streams"}" }
                    tracks.map { Candidate(file, it, it.index) }
                }
            }
            return@run {
                added.forEach { (file, track, demuxIndex) ->
                    if (selections.none { it.file == file && it.track.index == track.index && it.track.kind == track.kind }) {
                        val label = if (track.kind == "chapters") "Chapters  ${track.title ?: "embedded chapter set"}" else track.displayLabel()
                        val check = CheckBox(activity).apply { text = "${file.name} — $label"; isChecked = true }
                        selections += Selection(file, track, check, demuxIndex); streamList.addView(check)
                    }
                }
                status.text = "${selections.size} streams / chapter sets available"
            }
        }
        return true
    }

    private fun prepareInput(uri: Uri, kind: Int, movieFolder: Boolean = false): File {
        val name = displayName(uri)
        if (kind == 0 && (movieFolder || name.endsWith(".iso", ignoreCase = true))) {
            check(dvdEngine.isAvailable) { dvdEngine.unavailableReason ?: "Native DVD engine unavailable" }
            val stem = if (name.endsWith(".iso", ignoreCase = true)) name.dropLast(4) else name
            val safeStem = stem.trim().ifBlank { "DVD" }.replace(Regex("[^A-Za-z0-9._ -]"), "_")
            // A unique, uncreated path identifies the DVD in the existing selection model.
            // No movie bytes are copied until the user requests MUX.
            val staged = File(root, "dvd-${sources.size}/$safeStem-dvd-title.mkv")
            activity.runOnUiThread { status.text = "Reading DVD title and streams: $name" }
            check(!native.cancelled.get()) { "Cancelled" }
            val metadata = dvdEngine.probeTitleMetadata(activity, uri, null)
            check(!native.cancelled.get()) { "Cancelled" }
            dvdSources[staged] = DvdSource(uri, AdvancedDvdInput(staged, metadata))
            return staged
        }
        return copyInput(uri, name)
    }

    private fun displayName(uri: Uri): String {
        val queryUri = if (DocumentsContract.isTreeUri(uri)) {
            runCatching {
                DocumentsContract.buildDocumentUriUsingTree(uri, DocumentsContract.getTreeDocumentId(uri))
            }.getOrDefault(uri)
        } else {
            uri
        }
        return activity.contentResolver.query(queryUri, arrayOf(OpenableColumns.DISPLAY_NAME), null, null, null)?.use {
            if (it.moveToFirst()) it.getString(0) else null
        } ?: "input-${sources.size}"
    }

    private fun copyInput(uri: Uri, providedName: String? = null): File {
        val name = providedName ?: displayName(uri)
        require(name == File(name).name && name != "." && name != "..") { "Invalid input filename" }
        // Avoid silently pairing two unrelated sources with the same basename.
        val file = File(root, name)
        require(!file.exists()) { "Two inputs have the same filename: $name. Rename one before adding it." }
        try {
            activity.contentResolver.openInputStream(uri)?.use { input -> file.outputStream().use { output ->
                val buffer = ByteArray(256 * 1024)
                while (true) { check(!native.cancelled.get()) { "Cancelled" }; val n = input.read(buffer); if (n < 0) break; output.write(buffer, 0, n) }
            } } ?: error("Cannot read $name")
        } catch (error: Throwable) { file.delete(); throw error }
        return file
    }

    private fun parseTrack(record: String): TrackInfo {
        val f = record.split('\t', limit = 9)
        require(f.size == 9) { "Invalid stream metadata" }
        return TrackInfo(f[0].toInt(), f[1], f[2], f[3].takeUnless { it == "-" }, f[4].takeUnless { it == "-" }, f[5].toInt(), f[6].toInt(), f[7].toInt(), f[8].takeUnless { it == "-" })
    }

    private fun chooseDemux() {
        AlertDialog.Builder(activity)
            .setTitle("MPEG-2 video export format")
            .setItems(arrayOf("MPEG2 elementary video (.mpeg2)", "VOB video (.VOB)")) { _, choice ->
                demux(choice == 1)
            }
            .setNegativeButton("Cancel", null)
            .show()
    }

    private fun demux(vob: Boolean) {
        val selected = selections.filter { it.check.isChecked }
        val unsupported = selected.filter { it.track.kind !in setOf("video", "audio", "subtitle", "chapters") }
        val folder = output
        val mediaCount = selected.count { it.track.kind in setOf("video", "audio", "subtitle") }
        if (folder == null || mediaCount == 0 || unsupported.isNotEmpty()) {
            val message = if (unsupported.isNotEmpty())
                "DEMUX supports video, audio, subtitle streams and embedded chapters. Deselect attachment/data rows."
            else
                "Select at least one video, audio, or subtitle stream and choose an output folder."
            AlertDialog.Builder(activity).setMessage(message).setPositiveButton("OK", null).show()
            return
        }
        val grouped = linkedMapOf<File, MutableList<Selection>>()
        for (selection in selected) grouped.getOrPut(selection.file) { mutableListOf() }.add(selection)
        for ((file, group) in grouped) {
            if (group.any { it.track.kind == "chapters" } && group.none { it.track.kind in setOf("video", "audio", "subtitle") }) {
                AlertDialog.Builder(activity)
                    .setMessage("Select at least one media stream from ${file.name} to demux its chapters.")
                    .setPositiveButton("OK", null).show()
                return
            }
        }
        run("Demuxing selected streams…") {
            val parent = DocumentsContract.buildDocumentUriUsingTree(folder, DocumentsContract.getTreeDocumentId(folder))
            var destination: Uri? = null
            try {
                destination = DocumentsContract.createDocument(
                    activity.contentResolver, parent, DocumentsContract.Document.MIME_TYPE_DIR,
                    "Demux-${System.currentTimeMillis()}",
                ) ?: error("Cannot create demux output folder")
                val outputFolder = destination ?: error("Cannot create demux output folder")
                for ((file, group) in grouped) {
                    val media = group.filter { it.track.kind in setOf("video", "audio", "subtitle") }
                    if (media.isEmpty()) continue
                    val includeChapters = group.any { it.track.kind == "chapters" }
                    val temporary = File(root, "demux-${System.nanoTime()}").apply { check(mkdir()) }
                    try {
                        val dvd = dvdSources[file]
                        if (dvd != null) {
                            dvdEngine.demuxTitleToDirectory(
                                activity, dvd.uri, temporary,
                                media.map { it.demuxIndex }.toIntArray(), includeChapters, vob,
                                requestedTitle = dvd.input.metadata.title,
                            ) { percent ->
                                activity.runOnUiThread {
                                    if (!destroyed) status.text = "Demuxing ${file.name} directly from DVD… $percent%"
                                }
                            }
                        } else {
                            native.progressListener = { percent ->
                                activity.runOnUiThread {
                                    if (!destroyed) status.text = "Demuxing ${file.name}… $percent%"
                                }
                            }
                            native.demux(
                                file.absolutePath, temporary.absolutePath,
                                media.map { it.demuxIndex }.toIntArray(), includeChapters, vob,
                            )?.let { error("Demux failed for ${file.name}: $it") }
                        }
                        check(!native.cancelled.get()) { "Cancelled" }
                        val rawStem = file.name.substringBeforeLast('.', file.name)
                        val stem = rawStem.replace(Regex("[^A-Za-z0-9._ -]"), "_").trim().ifBlank { "source" }
                        val files = temporary.listFiles()?.sortedBy { it.name } ?: error("No demux output files")
                        check(files.isNotEmpty() && files.all { it.length() > 0L }) { "Demux produced an empty output" }
                        for (source in files) {
                            check(!native.cancelled.get()) { "Cancelled" }
                            val name = if (source.name == "Chapters.txt") "$stem-Chapters.txt" else "$stem-${source.name}"
                            val document = DocumentsContract.createDocument(
                                activity.contentResolver, outputFolder, "application/octet-stream", name,
                            ) ?: error("Cannot create $name")
                            try {
                                activity.contentResolver.openOutputStream(document, "w")?.use { out ->
                                    source.inputStream().use { input ->
                                        val buffer = ByteArray(256 * 1024)
                                        while (true) {
                                            check(!native.cancelled.get()) { "Cancelled" }
                                            val n = input.read(buffer)
                                            if (n < 0) break
                                            out.write(buffer, 0, n)
                                        }
                                    }
                                } ?: error("Cannot write $name")
                            } catch (error: Throwable) {
                                runCatching { DocumentsContract.deleteDocument(activity.contentResolver, document) }
                                throw error
                            }
                        }
                    } finally {
                        native.progressListener = null
                        temporary.deleteRecursively()
                    }
                }
                val completed = outputFolder
                return@run { status.text = "Demux complete: $completed" }
            } catch (error: Throwable) {
                destination?.let { runCatching { DocumentsContract.deleteDocument(activity.contentResolver, it) } }
                throw error
            } finally {
                native.progressListener = null
            }
        }
    }

    private fun mux() {
        val selected = selections.filter { it.check.isChecked }
        val media = selected.filter { it.track.kind != "chapters" }
        val selectedChapterFiles = selected.filter { it.track.kind == "chapters" }.map { it.file }.distinct()
        val folder = output
        val name = filename.text.toString().trim()
        if (media.isEmpty() || folder == null || name != File(name).name || !name.endsWith(".mkv", true)) {
            AlertDialog.Builder(activity).setMessage("Select at least one media or attachment stream, choose an output folder, and enter a filename ending in .mkv.").setPositiveButton("OK", null).show(); return
        }
        if (chapters == null && selectedChapterFiles.size > 1) {
            AlertDialog.Builder(activity).setMessage("Select only one movie chapter set, or choose a chapter file to override them.").setPositiveButton("OK", null).show(); return
        }
        val inputs = media.map { it.file }.distinct()
        val chapterFile = chapters ?: selectedChapterFiles.singleOrNull()
        val chapter = chapterFile?.absolutePath
        run("Muxing selected streams…") {
            val temporary = File.createTempFile("merged-", ".mkv", root)
            var destination: Uri? = null
            try {
                val dvdIndexes = linkedMapOf<File, Map<Int, Int>>()
                // Include a DVD used only for chapters, even when all its media is unchecked.
                for (file in (inputs + listOfNotNull(chapterFile)).distinct()) {
                    check(!native.cancelled.get()) { "Cancelled" }
                    val dvd = dvdSources[file] ?: continue
                    activity.runOnUiThread { if (!destroyed) status.text = "Preparing DVD title for MUX: ${file.name}" }
                    dvdIndexes[file] = dvd.input.prepareForMux(
                        stage = {
                            dvdEngine.remuxTitleToFile(
                                activity, dvd.uri, file, requestedTitle = dvd.input.metadata.title,
                                preserveChapters = true,
                            ) { percent ->
                                activity.runOnUiThread { if (!destroyed) status.text = "Preparing ${file.name} for MUX… $percent%" }
                            }
                        },
                        probe = { native.probe(file.absolutePath).map(::parseTrack) },
                        cancelled = { native.cancelled.get() || destroyed },
                    )
                }
                check(!native.cancelled.get()) { "Cancelled" }
                val indexes = media.map { selection ->
                    dvdIndexes[selection.file]?.let { it.getValue(selection.track.index) } ?: selection.track.index
                }.toIntArray()
                activity.runOnUiThread { if (!destroyed) status.text = "Muxing selected streams…" }
                native.mux(inputs.map { it.absolutePath }.toTypedArray(), media.map { inputs.indexOf(it.file) }.toIntArray(), indexes, chapter, temporary.absolutePath)?.let { error(it) }
                check(!native.cancelled.get()) { "Cancelled" }
                val parent = DocumentsContract.buildDocumentUriUsingTree(folder, DocumentsContract.getTreeDocumentId(folder))
                // createDocument creates a new document; it never opens an existing movie for replacement.
                destination = DocumentsContract.createDocument(activity.contentResolver, parent, "video/x-matroska", name) ?: error("Cannot create output document")
                activity.contentResolver.openOutputStream(destination, "w")?.use { out -> temporary.inputStream().use { input ->
                    val buffer = ByteArray(256 * 1024)
                    while (true) { check(!native.cancelled.get()) { "Cancelled" }; val n = input.read(buffer); if (n < 0) break; out.write(buffer, 0, n) }
                } } ?: error("Cannot write output document")
                val completed = destination
                return@run { status.text = "Completed: $completed" }
            } catch (error: Throwable) {
                destination?.let { runCatching { DocumentsContract.deleteDocument(activity.contentResolver, it) } }
                throw error
            } finally { temporary.delete() }
        }
    }

    private fun run(label: String, work: () -> (() -> Unit)) {
        if (busy) return
        busy = true; native.cancelled.set(false); status.text = label
        controls.forEach { it.isEnabled = false }; selections.forEach { it.check.isEnabled = false }; cancelButton.isEnabled = true
        Thread {
            val result = runCatching(work)
            activity.runOnUiThread {
                busy = false
                if (!destroyed) {
                    controls.forEach { it.isEnabled = true }; selections.forEach { it.check.isEnabled = true }; cancelButton.isEnabled = false
                    result.fold({ it() }, { status.text = it.message ?: "Merge failed" })
                }
            }
            if (destroyed) root.deleteRecursively()
        }.start()
    }

    fun destroy() { destroyed = true; native.cancelled.set(true); dvdEngine.cancel(); if (!busy) root.deleteRecursively() }
}

class AdvancedMergerNative {
    @Volatile var progressListener: ((Int) -> Unit)? = null
    @Suppress("unused") private fun onNativeProgress(percent: Int) { progressListener?.invoke(percent.coerceIn(0, 100)) }
    val cancelled = AtomicBoolean(false)
    @Suppress("unused") private fun isNativeCancelled(): Boolean = cancelled.get()
    external fun demux(path: String, directory: String, streams: IntArray, chapters: Boolean, vob: Boolean): String?
    external fun probe(path: String): Array<String>
    external fun validateChapters(path: String): String?
    external fun mux(paths: Array<String>, inputIndexes: IntArray, streamIndexes: IntArray, chapters: String?, output: String): String?
}
