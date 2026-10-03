package io.github.maas3n.mattmux

import android.content.Context
import android.net.Uri
import android.provider.DocumentsContract
import android.provider.DocumentsContract.Document
import java.util.concurrent.atomic.AtomicBoolean

internal data class AndroidBatchMovie(
    val name: String,
    val sourceUri: Uri,
    val defaultOutputTree: Uri,
)
internal data class AndroidBatchFailure(val movie: String, val message: String)
internal data class AndroidBatchResult(
    val total: Int,
    val completed: Int,
    val outputs: List<Uri>,
    val failures: List<AndroidBatchFailure>,
    val cancelled: Boolean,
)

/** Android/ChromeOS equivalent of the desktop ONECLICK BATCH engine. */
internal class AndroidBatchProcessor(
    private val context: Context,
    private val engine: RemuxEngine = AndroidNativeRemuxEngine(),
) {
    private val cancelled = AtomicBoolean(false)

    val isAvailable: Boolean get() = engine.isAvailable
    val unavailableReason: String? get() = engine.unavailableReason

    fun cancel() {
        cancelled.set(true)
        engine.cancel()
    }

    /**
     * BATCH scans one collection level only:
     *  - Movie/VIDEO_TS/VIDEO_TS.IFO -> Movie/Movie.mkv when OUTPUT_ROOT is blank.
     *  - Movie.iso -> Movie.mkv beside the ISO when OUTPUT_ROOT is blank.
     *  - Movie/Movie.iso -> Movie/Movie.mkv beside the ISO when OUTPUT_ROOT is blank.
     */
    fun discover(inputRoot: Uri): List<AndroidBatchMovie> {
        require(DocumentsContract.isTreeUri(inputRoot)) { "MOVIES_ROOT must be an Android document-tree URI" }
        val rootId = documentTreeRootId(inputRoot)
        val movies = discoverBatchDocuments(rootId) { id -> listChildren(inputRoot, id) }.map { movie ->
            AndroidBatchMovie(
                movie.name,
                DocumentsContract.buildDocumentUriUsingTree(inputRoot, movie.sourceId),
                DocumentsContract.buildDocumentUriUsingTree(inputRoot, movie.outputParentId),
            )
        }
        require(movies.isNotEmpty()) {
            "No immediate movie folders containing VIDEO_TS/VIDEO_TS.IFO or unmounted ISO files were found"
        }
        return movies
    }

    fun run(
        inputRoot: Uri,
        outputRoot: Uri?,
        progress: (Int, String) -> Unit = { _, _ -> },
        log: (String) -> Unit = {},
    ): AndroidBatchResult {
        check(engine.isAvailable) { engine.unavailableReason ?: "Remux engine unavailable" }
        outputRoot?.let { require(DocumentsContract.isTreeUri(it)) { "OUTPUT_ROOT must be an Android document-tree URI" } }
        cancelled.set(false)
        val movies = discover(inputRoot)
        val outputs = mutableListOf<Uri>()
        val failures = mutableListOf<AndroidBatchFailure>()
        var completed = 0
        var activeIndex = -1
        var activeName = ""

        engine.setProgressListener { moviePercent ->
            if (activeIndex >= 0 && movies.isNotEmpty()) {
                val overall = (((activeIndex + moviePercent / 100.0) / movies.size) * 100.0).toInt().coerceIn(0, 100)
                progress(overall, "[${activeIndex + 1}/${movies.size}] $activeName — remuxing $moviePercent%")
            }
        }
        try {
            log("MattRip Android batch start: movies=${movies.size} input=$inputRoot output=${outputRoot ?: "<beside source>"}")
            movies.forEachIndexed { index, movie ->
                if (cancelled.get()) return@forEachIndexed
                activeIndex = index
                activeName = movie.name
                val targetTree = outputRoot ?: movie.defaultOutputTree
                val desiredName = BatchNaming.outputName(movie.name)
                val basePercent = ((index.toDouble() / movies.size) * 100.0).toInt()
                progress(basePercent, "[${index + 1}/${movies.size}] ${movie.name} — scanning DVD titles")
                log("[${index + 1}/${movies.size}] scanning ${movie.name}: ${movie.sourceUri}")

                if (documentNameExists(targetTree, desiredName)) {
                    val message = "output already exists: $desiredName"
                    failures += AndroidBatchFailure(movie.name, message)
                    log("[${index + 1}/${movies.size}] skipped ${movie.name}: $message")
                    return@forEachIndexed
                }

                try {
                    val remux = engine.remux(context, movie.sourceUri, targetTree, null)
                    if (cancelled.get()) return@forEachIndexed
                    val renamed = DocumentsContract.renameDocument(context.contentResolver, remux.outputUri, desiredName)
                        ?: error("remux completed but output provider could not rename ${remux.outputUri} to $desiredName; completed MKV retained at ${remux.outputUri}")
                    outputs += renamed
                    completed++
                    progress((((index + 1).toDouble() / movies.size) * 100.0).toInt(), "Completed ${index + 1} of ${movies.size} movie(s).")
                    log("[${index + 1}/${movies.size}] completed ${movie.name}: title=${remux.title} durationMs=${remux.durationMs} -> $renamed")
                } catch (error: Throwable) {
                    if (cancelled.get()) return@forEachIndexed
                    val message = error.message ?: error.javaClass.simpleName
                    failures += AndroidBatchFailure(movie.name, message)
                    log("[${index + 1}/${movies.size}] failed ${movie.name}: $message")
                }
            }
        } finally {
            engine.setProgressListener(null)
        }

        val wasCancelled = cancelled.get()
        if (wasCancelled) log("MattRip Android batch cancelled: completed=$completed total=${movies.size}")
        else log("MattRip Android batch complete: completed=$completed failed=${failures.size} total=${movies.size}")
        return AndroidBatchResult(movies.size, completed, outputs, failures, wasCancelled)
    }

    private fun documentNameExists(treeUri: Uri, name: String): Boolean =
        listChildren(treeUri, documentTreeRootId(treeUri)).any { it.name.equals(name, true) }

    private fun listChildren(treeUri: Uri, parentId: String): List<BatchDocument> {
        val children = DocumentsContract.buildChildDocumentsUriUsingTree(treeUri, parentId)
        return context.contentResolver.query(
            children,
            arrayOf(Document.COLUMN_DISPLAY_NAME, Document.COLUMN_DOCUMENT_ID, Document.COLUMN_MIME_TYPE),
            null, null, null,
        )?.use { cursor ->
            val nameCol = cursor.getColumnIndexOrThrow(Document.COLUMN_DISPLAY_NAME)
            val idCol = cursor.getColumnIndexOrThrow(Document.COLUMN_DOCUMENT_ID)
            val mimeCol = cursor.getColumnIndexOrThrow(Document.COLUMN_MIME_TYPE)
            buildList {
                while (cursor.moveToNext()) {
                    add(BatchDocument(cursor.getString(nameCol), cursor.getString(idCol), cursor.getString(mimeCol) == Document.MIME_TYPE_DIR))
                }
            }
        } ?: error("Selected document provider did not return directory contents")
    }
}
