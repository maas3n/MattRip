package io.github.maas3n.mattmux

import android.content.ContentResolver
import android.net.Uri
import android.provider.DocumentsContract
import android.provider.DocumentsContract.Document
import android.os.ParcelFileDescriptor
import java.util.Locale

internal class DvdDocumentSource(
    private val resolver: ContentResolver,
    private val treeUri: Uri,
) {
    internal data class OpenTitle(
        val plan: DvdTitlePlan,
        val vobs: List<ParcelFileDescriptor>,
    ) : AutoCloseable {
        override fun close() = vobs.forEach { runCatching { it.close() } }
    }

    private data class Entry(val name: String, val documentId: String, val mimeType: String)

    fun openPlan(plan: DvdTitlePlan): OpenTitle {
        val rootId = documentTreeRootId(treeUri)
        val rootChildren = listChildren(rootId)
        val videoTsId = if (rootChildren.any { it.name.equals("VIDEO_TS.IFO", true) }) {
            rootId
        } else {
            rootChildren.firstOrNull {
                it.name.equals("VIDEO_TS", true) && it.mimeType == Document.MIME_TYPE_DIR
            }?.documentId ?: error("VIDEO_TS folder was not found in the selected tree")
        }
        val entries = listChildren(videoTsId)
        val prefix = String.format(Locale.ROOT, "VTS_%02d_", plan.titleSet)
        val vobEntries = entries
            .filter { it.name.uppercase(Locale.ROOT).matches(Regex("${prefix}[1-9]\\.VOB")) }
            .sortedBy { it.name.uppercase(Locale.ROOT) }
        require(vobEntries.isNotEmpty()) { "No title VOB files were found for VTS ${plan.titleSet}" }
        vobEntries.forEachIndexed { index, entry ->
            require(entry.name.equals("${prefix}${index + 1}.VOB", true)) { "Title has a missing VOB part" }
        }
        val opened = mutableListOf<ParcelFileDescriptor>()
        try {
            vobEntries.forEach { entry ->
                opened += resolver.openFileDescriptor(documentUri(entry.documentId), "r")
                    ?: error("Could not open ${entry.name}")
            }
            return OpenTitle(plan, opened)
        } catch (t: Throwable) {
            opened.forEach { runCatching { it.close() } }
            throw t
        }
    }

    private fun listChildren(parentId: String): List<Entry> {
        val childrenUri = DocumentsContract.buildChildDocumentsUriUsingTree(treeUri, parentId)
        return resolver.query(
            childrenUri,
            arrayOf(Document.COLUMN_DISPLAY_NAME, Document.COLUMN_DOCUMENT_ID, Document.COLUMN_MIME_TYPE),
            null, null, null,
        )?.use { cursor ->
            val nameCol = cursor.getColumnIndexOrThrow(Document.COLUMN_DISPLAY_NAME)
            val idCol = cursor.getColumnIndexOrThrow(Document.COLUMN_DOCUMENT_ID)
            val mimeCol = cursor.getColumnIndexOrThrow(Document.COLUMN_MIME_TYPE)
            buildList {
                while (cursor.moveToNext()) add(Entry(cursor.getString(nameCol), cursor.getString(idCol), cursor.getString(mimeCol)))
            }
        } ?: error("Selected document provider did not return directory contents")
    }

    private fun documentUri(documentId: String): Uri = DocumentsContract.buildDocumentUriUsingTree(treeUri, documentId)
}

internal class DvdDocumentOutput(
    private val resolver: ContentResolver,
    private val treeUri: Uri,
) {
    internal data class Pending(val uri: Uri, val descriptor: ParcelFileDescriptor, val finalName: String, val reservedFinal: Boolean = false)

    fun create(title: Int, requestedName: String? = null): Pending {
        if (requestedName != null) return reserveNamedOutput(requestedName)
        val parentId = documentTreeRootId(treeUri)
        val parent = DocumentsContract.buildDocumentUriUsingTree(treeUri, parentId)
        val finalName = "MattRip-title-%02d.mkv".format(title)
        val partialName = "$finalName.partial"
        val uri = DocumentsContract.createDocument(resolver, parent, "video/x-matroska", partialName)
            ?: error("The output provider could not create $partialName")
        try {
            val descriptor = resolver.openFileDescriptor(uri, "rw") ?: error("The output provider could not open $partialName")
            return Pending(uri, descriptor, finalName)
        } catch (t: Throwable) {
            runCatching { DocumentsContract.deleteDocument(resolver, uri) }; throw t
        }
    }

    // SAF createDocument creates a new document (renaming on name collision).
    // Reserve the final name before writing, never rename over an existing file.
    private fun reserveNamedOutput(name: String): Pending {
        require(name.endsWith(".mkv", true) && name.none { it == '/' || it == '\\' || it.code < 32 }) { "Invalid output filename" }
        val parentId = documentTreeRootId(treeUri)
        val children = DocumentsContract.buildChildDocumentsUriUsingTree(treeUri, parentId)
        resolver.query(children, arrayOf(Document.COLUMN_DISPLAY_NAME), null, null, null)?.use { cursor ->
            while (cursor.moveToNext()) {
                check(!cursor.getString(0).equals(name, true)) { "Output already exists: $name" }
            }
        } ?: error("Cannot check the output folder; specify --output with a writable folder")
        val parent = DocumentsContract.buildDocumentUriUsingTree(treeUri, parentId)
        val uri = DocumentsContract.createDocument(resolver, parent, "video/x-matroska", name)
            ?: error("Output folder required / no write access to ISO folder")
        try {
            val actualName = resolver.query(uri, arrayOf(Document.COLUMN_DISPLAY_NAME), null, null, null)?.use {
                if (it.moveToFirst()) it.getString(0) else null
            }
            check(actualName == name) { "Output name unavailable: $name (provider created $actualName)" }
            val descriptor = resolver.openFileDescriptor(uri, "rw") ?: error("Could not open output: $name")
            return Pending(uri, descriptor, name, reservedFinal = true)
        } catch (error: Throwable) {
            runCatching { DocumentsContract.deleteDocument(resolver, uri) }
            throw error
        }
    }

    fun commit(pending: Pending): Uri {
        pending.descriptor.close()
        if (pending.reservedFinal) return pending.uri
        return DocumentsContract.renameDocument(resolver, pending.uri, pending.finalName)
            ?: error("Remux completed, but the output provider could not rename the temporary file. Completed MKV kept at ${pending.uri}")
    }
    fun preserve(pending: Pending) { runCatching { pending.descriptor.close() } }
    fun abort(pending: Pending) {
        runCatching { pending.descriptor.close() }
        runCatching { DocumentsContract.deleteDocument(resolver, pending.uri) }
    }
}
