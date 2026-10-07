package io.github.maas3n.mattmux

import java.io.File

/** Keeps original DVD indexes for selection/DEMUX; materializes a file only for MUX. */
internal class AdvancedDvdInput(val file: File, val metadata: DvdMetadataResult) {
    val tracks: List<TrackInfo> = metadata.tracks + if (metadata.chapterStartsMs.isNotEmpty()) {
        listOf(TrackInfo(-1, "chapters", "", null, "${metadata.chapterStartsMs.size} chapters", 0, 0, 0, null))
    } else emptyList()

    private var muxIndexes: Map<Int, Int>? = null

    fun prepareForMux(
        stage: () -> Int,
        probe: () -> List<TrackInfo>,
        cancelled: () -> Boolean,
    ): Map<Int, Int> {
        check(!cancelled()) { "Cancelled" }
        muxIndexes?.let { if (file.isFile && file.length() > 0L) return it }
        muxIndexes = null
        try {
            check(stage() == metadata.title) { "DVD title changed while staging" }
            check(!cancelled()) { "Cancelled" }
            check(file.isFile && file.length() > 0L) { "DVD title staging produced an empty file" }
            val staged = probe()
            val media = staged.filter { it.kind in setOf("video", "audio", "subtitle") }
            require(metadata.tracks.size == media.size && metadata.tracks.zip(media).all { (original, copy) ->
                original.kind == copy.kind && original.codec == copy.codec
            }) { "DVD stream mapping changed while staging ${file.name}" }
            if (metadata.chapterStartsMs.isNotEmpty()) {
                require(staged.any { it.kind == "chapters" }) { "DVD chapters missing after staging ${file.name}" }
            }
            check(!cancelled()) { "Cancelled" }
            return metadata.tracks.zip(media).associate { (original, copy) -> original.index to copy.index }
                .also { muxIndexes = it }
        } catch (error: Throwable) {
            file.delete()
            throw error
        }
    }
}
