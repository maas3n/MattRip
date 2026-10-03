package io.github.maas3n.mattmux

import android.app.Activity
import android.app.Instrumentation
import android.os.Bundle
import java.io.File

/** Development APK only: exercise the packaged JNI binaries on real Android. */
class DemuxSmokeInstrumentation : Instrumentation() {
    override fun onCreate(arguments: Bundle?) {
        super.onCreate(arguments)
        start()
    }
    override fun onStart() {
        val result = Bundle()
        val root = File(targetContext.cacheDir, "demux-smoke").apply { mkdirs() }
        try {
            val runtime = AndroidNativeRemuxEngine()
            check(runtime.isAvailable) { runtime.unavailableReason ?: "Native runtime unavailable" }
            val native = AdvancedMergerNative()
            for (name in listOf("mixed.mkv", "subtitles.mkv", "raw-h264.mkv")) {
                val source = File(root, name)
                targetContext.assets.open("demux-smoke/$name").use { input -> source.outputStream().use { input.copyTo(it) } }
                val metadata = String(MediaInfoNative().metadata(source.absolutePath), Charsets.UTF_8)
                check(metadata.contains("Matroska")) { "MediaInfo did not identify $name: $metadata" }
                val indexes = native.probe(source.absolutePath).map { it.substringBefore('\t').toInt() }.filter { it >= 0 }.toIntArray()
                val output = File(root, "$name-export").apply { mkdirs() }
                native.demux(source.absolutePath, output.absolutePath, indexes, true, false)?.let { error(it) }
                check(output.listFiles()?.all { it.length() > 0 } == true) { "Empty export: $name" }
                when (name) {
                    "mixed.mkv" -> {
                        check(File(output, "track-04.srt").readText().contains("Hello"))
                        check(File(output, "Chapters.txt").readText().contains("CHAPTER01NAME=Opening"))
                    }
                    "subtitles.mkv" -> {
                        val idx = File(output, "track-01.idx").readText()
                        check(idx.contains("00:00:00:200") && idx.contains("00:00:00:600") && idx.contains("palette:")) { idx }
                        check(File(output, "track-01.sub").length() > 0)
                    }
                    "raw-h264.mkv" -> check(File(output, "track-00.h264").length() > 0)
                }
            }
            // Exercise the actual DVD-tab engine, including direct DVD reading and SAF export.
            val safRoot = File(targetContext.cacheDir, "dvd-saf-test").apply { deleteRecursively(); mkdirs() }
            for ((asset, folder) in listOf("VIDEO_TS" to "movie", "CLOCK_RESET" to "clock-reset")) {
                val movie = File(safRoot, "$folder/VIDEO_TS").apply { mkdirs() }
                for (name in targetContext.assets.list("demux-smoke/$asset")!!) {
                    targetContext.assets.open("demux-smoke/$asset/$name").use { input -> File(movie, name).outputStream().use { input.copyTo(it) } }
                }
                check(movie.listFiles()!!.isNotEmpty())
            }
            File(safRoot, "output").mkdirs()
            val authority = "io.github.maas3n.mattrip.demux-test"
            val source = android.provider.DocumentsContract.buildTreeDocumentUri(authority, "movie")
            val output = android.provider.DocumentsContract.buildTreeDocumentUri(authority, "output")
            for (input in listOf(source, android.provider.DocumentsContract.buildTreeDocumentUri(authority, "clock-reset"))) {
                for (vob in listOf(false, true)) {
                    val engine = TabMediaEngine(targetContext, runtime)
                    try {
                        val statuses = mutableListOf<String>()
                        val exported = engine.demux(input, output, null, true, vob) { statuses.add(it) }
                        for (phase in listOf("Extracting selected streams…")) {
                            val progress = statuses.filter { it.startsWith(phase) && it.endsWith("%") }
                            check(progress.distinct().size > 3) { "Missing $phase progress: $statuses" }
                        }
                        check(statuses.none { it.startsWith("Preparing DVD title") }) { "DVD demux staged an MKV" }
                        check(targetContext.cacheDir.listFiles().orEmpty().filter { it.name.startsWith("dvd-tab-") }
                            .none { File(it, "source.mkv").exists() }) { "DVD demux created a temporary MKV" }
                        val folder = File(safRoot, android.provider.DocumentsContract.getDocumentId(exported))
                        val files = folder.listFiles()!!.toList()
                        check(files.any { it.extension == if (vob) "VOB" else "mpeg2" }) { "Missing DVD video: $files" }
                        check(files.any { it.extension == "ac3" }) { "Missing DVD audio: $files" }
                        check(File(folder, "Chapters.txt").readText().contains("CHAPTER02="))
                        check(files.any { it.extension == "sub" }) { "Missing DVD subtitle data: $files" }
                        val idx = files.single { it.extension == "idx" }.readText()
                        check(idx.contains("size: 720x576") && idx.contains("palette:") && idx.contains("timestamp:")) { "Invalid DVD subtitle index: $idx" }
                        check(files.all { it.length() > 0 })
                        android.util.Log.i("MattMuxDemuxTest", "DVD SAF export completed: vob=$vob files=${files.map { it.name }}")
                    } finally { engine.destroy() }
                }
            }
            // The same Demux button path must still accept MKV documents.
            File(root, "mixed.mkv").copyTo(File(safRoot, "mixed.mkv"), overwrite = true)
            val mkvInput = android.provider.DocumentsContract.buildDocumentUri(authority, "mixed.mkv")
            val mkvEngine = TabMediaEngine(targetContext, runtime)
            try {
                val exported = mkvEngine.demux(mkvInput, output, intArrayOf(1, 3, 4), true, false)
                val folder = File(safRoot, android.provider.DocumentsContract.getDocumentId(exported))
                check(folder.listFiles()!!.size == 4) { "MKV selection was not preserved" }
                check(File(folder, "track-04.srt").readText().contains("Hello"))
                check(File(folder, "Chapters.txt").readText().contains("CHAPTER01NAME=Opening"))
                check(folder.listFiles()!!.all { it.length() > 0 })
            } finally { mkvEngine.destroy() }
            val activity = startActivitySync(android.content.Intent(targetContext, MainActivity::class.java).addFlags(android.content.Intent.FLAG_ACTIVITY_NEW_TASK)) as MainActivity
            try {
                fun field(name: String) = MainActivity::class.java.getDeclaredField(name).apply { isAccessible = true }
                runOnMainSync {
                    field("sourceUri").set(activity, source)
                    field("outputUri").set(activity, output)
                    MainActivity::class.java.getDeclaredMethod("startDemux", Boolean::class.javaPrimitiveType).apply { isAccessible = true }.invoke(activity, false)
                }
                val deadline = android.os.SystemClock.elapsedRealtime() + 60000
                var finished = false
                var message = ""
                while (!finished && android.os.SystemClock.elapsedRealtime() < deadline) {
                    runOnMainSync {
                        finished = !field("remuxRunning").getBoolean(activity)
                        message = (field("remuxStatus").get(activity) as android.widget.TextView).text.toString()
                    }
                    if (!finished) android.os.SystemClock.sleep(50)
                }
                check(finished && message.startsWith("Demux complete:")) { "DVD tab did not complete: $message" }
                android.util.Log.i("MattMuxDemuxTest", "DVD tab completion verified: $message")
            } finally { runOnMainSync { activity.finish() } }
            safRoot.deleteRecursively()
            result.putString("stream", "MATTMUX_DEMUX_SMOKE_PASS")
            finish(Activity.RESULT_OK, result)
        } catch (error: Throwable) {
            result.putString("stream", error.stackTraceToString())
            finish(Activity.RESULT_CANCELED, result)
        } finally { root.deleteRecursively() }
    }
}
