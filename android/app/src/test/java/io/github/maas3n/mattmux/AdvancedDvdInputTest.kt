package io.github.maas3n.mattmux

import org.junit.Assert.*
import org.junit.Rule
import org.junit.Test
import org.junit.rules.TemporaryFolder
import java.io.File

class AdvancedDvdInputTest {
    @get:Rule val temporary = TemporaryFolder()

    private fun track(index: Int, kind: String, codec: String) =
        TrackInfo(index, kind, codec, null, null, 0, 0, 0, null)

    private fun input(chapters: Boolean = true): AdvancedDvdInput {
        val metadata = DvdMetadataResult(
            3, 1000, if (chapters) longArrayOf(0) else longArrayOf(),
            if (chapters) longArrayOf(1000) else longArrayOf(),
            listOf(track(2, "video", "mpeg2video"), track(5, "audio", "ac3"), track(9, "subtitle", "dvd_subtitle")), "plan",
        )
        return AdvancedDvdInput(File(temporary.root, "dvd/title.mkv"), metadata)
    }

    private fun stagedTracks() = listOf(
        track(0, "video", "mpeg2video"), track(1, "audio", "ac3"),
        track(2, "subtitle", "dvd_subtitle"), track(-1, "chapters", ""),
    )

    private fun stage(input: AdvancedDvdInput): Int {
        input.file.parentFile!!.mkdirs()
        input.file.writeText("staged fixture")
        return input.metadata.title
    }

    @Test fun selectionAndDemuxUseOriginalIndexesWithoutCreatingAnMkv() {
        val dvd = input()
        assertEquals(listOf(2, 5, 9, -1), dvd.tracks.map { it.index })
        assertEquals(listOf("video", "audio", "subtitle", "chapters"), dvd.tracks.map { it.kind })
        assertFalse(dvd.file.exists())
        assertFalse(dvd.file.parentFile!!.exists())
        assertEquals(listOf(5, 9), dvd.tracks.filter { it.kind in setOf("audio", "subtitle") }.map { it.index })
    }

    @Test fun muxMapsOriginalIndexesAndReusesOnlySuccessfulStaging() {
        val dvd = input()
        var stages = 0
        val stage = { stages++; stage(dvd) }
        val indexes = dvd.prepareForMux(stage, ::stagedTracks) { false }
        assertEquals(mapOf(2 to 0, 5 to 1, 9 to 2), indexes)
        // Mixed MUX selections use this mapping only for DVD rows.
        assertEquals(listOf(2, 1), listOf(9, 5).map { indexes.getValue(it) })
        assertEquals(indexes, dvd.prepareForMux(stage, ::stagedTracks) { false })
        assertEquals(1, stages)
        dvd.file.delete()
        dvd.prepareForMux(stage, ::stagedTracks) { false }
        assertEquals(2, stages)
        assertEquals(listOf(2, 5, 9, -1), dvd.tracks.map { it.index })
    }

    @Test fun failedMappingDeletesStagedFileAndAllowsRetry() {
        val dvd = input()
        assertThrows(IllegalArgumentException::class.java) {
            dvd.prepareForMux({ stage(dvd) }, { stagedTracks().drop(1) }) { false }
        }
        assertFalse(dvd.file.exists())
        assertEquals(1, dvd.prepareForMux({ stage(dvd) }, ::stagedTracks) { false }.getValue(5))
    }

    @Test fun wrongTitleAndMissingChaptersFailClosed() {
        val dvd = input()
        assertThrows(IllegalStateException::class.java) {
            dvd.prepareForMux({ stage(dvd); 7 }, ::stagedTracks) { false }
        }
        assertFalse(dvd.file.exists())
        assertThrows(IllegalArgumentException::class.java) {
            dvd.prepareForMux({ stage(dvd) }, { stagedTracks().filter { it.kind != "chapters" } }) { false }
        }
        assertFalse(dvd.file.exists())
    }

    @Test fun cancellationDuringStagingCleansUpAndCanBeRetried() {
        val dvd = input()
        var cancelled = false
        assertThrows(IllegalStateException::class.java) {
            dvd.prepareForMux({ stage(dvd).also { cancelled = true } }, ::stagedTracks) { cancelled }
        }
        assertFalse(dvd.file.exists())
        dvd.prepareForMux({ stage(dvd) }, ::stagedTracks) { false }
        assertTrue(dvd.file.exists())
    }

    @Test fun cancellationBeforeMuxNeverStartsStaging() {
        val dvd = input()
        assertThrows(IllegalStateException::class.java) {
            dvd.prepareForMux({ error("Must not stage") }, { error("Must not probe") }) { true }
        }
        assertFalse(dvd.file.exists())
    }

    @Test fun chapterlessDvdHasNoSyntheticChapterRow() {
        val dvd = input(chapters = false)
        assertEquals(listOf(2, 5, 9), dvd.tracks.map { it.index })
        dvd.prepareForMux({ stage(dvd) }, { stagedTracks().filter { it.kind != "chapters" } }) { false }
    }
}
