package io.github.maas3n.mattmux

import android.content.Context
import android.net.Uri
import android.provider.DocumentsContract
import android.provider.DocumentsContract.Document
import java.io.File
import java.util.Locale

/** Stages only DVD IFO metadata for libdvdnav/libdvdread discovery and title planning. */
internal object DvdNavScanner {
    private data class Entry(val name: String, val documentId: String, val mimeType: String)

    fun stageTreeIfos(context: Context, treeUri: Uri): File {
        val resolver = context.contentResolver
        val rootId = documentTreeRootId(treeUri)
        val rootChildren = listChildren(context, treeUri, rootId)
        val videoTsId = if (rootChildren.any { it.name.equals("VIDEO_TS.IFO", true) }) {
            rootId
        } else {
            rootChildren.firstOrNull {
                it.name.equals("VIDEO_TS", true) && it.mimeType == Document.MIME_TYPE_DIR
            }?.documentId ?: error("VIDEO_TS folder was not found in the selected tree")
        }

        val stageRoot = createStageRoot(context)
        val videoTs = File(stageRoot, "VIDEO_TS").apply { mkdirs() }
        var copied = 0
        for (entry in listChildren(context, treeUri, videoTsId)) {
            val upper = entry.name.uppercase(Locale.ROOT)
            val keep = upper == "VIDEO_TS.IFO" || upper == "VIDEO_TS.BUP" ||
                Regex("VTS_[0-9]{2}_0\\.(IFO|BUP)").matches(upper)
            if (!keep) continue
            val uri = DocumentsContract.buildDocumentUriUsingTree(treeUri, entry.documentId)
            resolver.openInputStream(uri)?.use { input ->
                File(videoTs, upper).outputStream().use { output -> input.copyTo(output) }
            } ?: error("Could not read ${entry.name}")
            copied++
        }
        require(File(videoTs, "VIDEO_TS.IFO").isFile) { "VIDEO_TS.IFO is missing" }
        require(copied > 0) { "No DVD IFO metadata could be staged" }
        return stageRoot
    }

    fun stageIsoIfos(context: Context, vmg: ByteArray, loader: (Int) -> ByteArray?): File {
        val stageRoot = createStageRoot(context)
        val videoTs = File(stageRoot, "VIDEO_TS").apply { mkdirs() }
        File(videoTs, "VIDEO_TS.IFO").writeBytes(vmg)
        for (titleSet in 1..99) {
            val data = loader(titleSet) ?: continue
            File(videoTs, "VTS_%02d_0.IFO".format(Locale.ROOT, titleSet)).writeBytes(data)
        }
        return stageRoot
    }

    private fun createStageRoot(context: Context): File {
        val root = File(context.cacheDir, "mattrip-dvdnav-${System.nanoTime()}")
        require(root.mkdirs()) { "Could not create libdvdnav staging directory" }
        return root
    }

    private fun listChildren(context: Context, treeUri: Uri, parentId: String): List<Entry> {
        val childrenUri = DocumentsContract.buildChildDocumentsUriUsingTree(treeUri, parentId)
        return context.contentResolver.query(
            childrenUri,
            arrayOf(Document.COLUMN_DISPLAY_NAME, Document.COLUMN_DOCUMENT_ID, Document.COLUMN_MIME_TYPE),
            null, null, null,
        )?.use { cursor ->
            val nameCol = cursor.getColumnIndexOrThrow(Document.COLUMN_DISPLAY_NAME)
            val idCol = cursor.getColumnIndexOrThrow(Document.COLUMN_DOCUMENT_ID)
            val mimeCol = cursor.getColumnIndexOrThrow(Document.COLUMN_MIME_TYPE)
            buildList {
                while (cursor.moveToNext()) {
                    add(Entry(cursor.getString(nameCol), cursor.getString(idCol), cursor.getString(mimeCol)))
                }
            }
        } ?: error("Selected document provider did not return directory contents")
    }
}
