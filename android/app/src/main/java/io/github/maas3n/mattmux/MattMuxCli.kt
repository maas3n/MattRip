package io.github.maas3n.mattmux

import android.content.Context
import android.net.Uri
import android.provider.DocumentsContract
import java.io.File
import java.util.Locale

internal sealed class MattMuxCliCommand {
    data class Batch(val inputRoot: String, val outputRoot: String?, val logFile: String?) : MattMuxCliCommand()
    data class Scan(val source: String) : MattMuxCliCommand()
    data class Metadata(val source: String, val title: Int?) : MattMuxCliCommand()
    data class Remux(val source: String, val title: Int?, val outputRoot: String?, val noChapters: Boolean, val streams: List<Int>? = null) : MattMuxCliCommand()
    object Version : MattMuxCliCommand()
    object Help : MattMuxCliCommand()
}

internal object MattMuxCliSyntax {
    private const val BATCH_USAGE = "Usage: mattmux-cli --batch [--log FILE] MOVIES_ROOT [OUTPUT_ROOT]"
    private const val METADATA_USAGE = "Usage: mattmux-cli metadata [--title N] SOURCE"
    private const val REMUX_USAGE = "Usage: mattmux-cli remux [--title N] [--output OUTPUT_ROOT] [--no-chapters] [--streams 0,2] SOURCE"

    fun parse(commandLine: String): MattMuxCliCommand {
        val tokens = tokenize(commandLine).toMutableList()
        if (tokens.firstOrNull() == "mattrip-cli") tokens.removeAt(0)
        if (tokens.isEmpty() || tokens == listOf("--help") || tokens == listOf("-h") || tokens == listOf("help")) return MattMuxCliCommand.Help
        if (tokens == listOf("--version") || tokens == listOf("-version") || tokens == listOf("version")) return MattMuxCliCommand.Version
        return when (tokens.removeAt(0)) {
            "--batch" -> parseBatch(tokens)
            "scan" -> {
                require(tokens.size == 1) { "Usage: mattmux-cli scan SOURCE" }
                MattMuxCliCommand.Scan(tokens.single())
            }
            "metadata" -> parseMetadata(tokens)
            "remux" -> parseRemux(tokens)
            else -> error("Unknown command. Run mattmux-cli --help")
        }
    }

    private fun parseBatch(tokens: List<String>): MattMuxCliCommand.Batch {
        var logFile: String? = null
        val positional = mutableListOf<String>()
        var index = 0
        while (index < tokens.size) {
            val token = tokens[index]
            when {
                token == "--log" -> {
                    require(index + 1 < tokens.size) { "--log requires a filename" }
                    logFile = tokens[index + 1]
                    index += 2
                }
                token.startsWith("--log=") -> {
                    logFile = token.substringAfter("--log=").also { require(it.isNotBlank()) { "--log requires a filename" } }
                    index++
                }
                token.startsWith("-") -> error("Unknown option: $token")
                else -> { positional += token; index++ }
            }
        }
        require(positional.size in 1..2) { BATCH_USAGE }
        return MattMuxCliCommand.Batch(positional[0], positional.getOrNull(1), logFile)
    }

    private fun parseMetadata(tokens: List<String>): MattMuxCliCommand.Metadata {
        var title: Int? = null
        val positional = mutableListOf<String>()
        var index = 0
        while (index < tokens.size) {
            val token = tokens[index]
            when {
                token == "--title" -> {
                    require(index + 1 < tokens.size) { "--title requires a number" }
                    title = titleValue(tokens[index + 1])
                    index += 2
                }
                token.startsWith("--title=") -> {
                    title = titleValue(token.substringAfter("="))
                    index++
                }
                token.startsWith("-") -> error("Unknown option: $token")
                else -> { positional += token; index++ }
            }
        }
        require(positional.size == 1) { METADATA_USAGE }
        return MattMuxCliCommand.Metadata(positional.single(), title)
    }

    private fun parseRemux(tokens: List<String>): MattMuxCliCommand.Remux {
        var title: Int? = null
        var output: String? = null
        var noChapters = false
        var streams: List<Int>? = null
        val positional = mutableListOf<String>()
        var index = 0
        while (index < tokens.size) {
            val token = tokens[index]
            when {
                token == "--title" -> {
                    require(index + 1 < tokens.size) { "--title requires a number" }
                    title = titleValue(tokens[index + 1])
                    index += 2
                }
                token.startsWith("--title=") -> {
                    title = titleValue(token.substringAfter("="))
                    index++
                }
                token == "--output" -> {
                    require(index + 1 < tokens.size) { "--output requires a document-tree URI" }
                    output = tokens[index + 1]
                    index += 2
                }
                token.startsWith("--output=") -> {
                    output = token.substringAfter("=").also { require(it.isNotBlank()) { "--output requires a document-tree URI" } }
                    index++
                }
                token == "--streams" -> {
                    require(index + 1 < tokens.size) { "--streams requires comma-separated stream indexes" }
                    streams = streamValues(tokens[index + 1])
                    index += 2
                }
                token.startsWith("--streams=") -> {
                    streams = streamValues(token.substringAfter("="))
                    index++
                }
                token == "--no-chapters" -> { noChapters = true; index++ }
                token.startsWith("-") -> error("Unknown option: $token")
                else -> { positional += token; index++ }
            }
        }
        require(positional.size == 1) { REMUX_USAGE }
        return MattMuxCliCommand.Remux(positional.single(), title, output, noChapters, streams)
    }

    private fun streamValues(value: String): List<Int> = value.split(',').map { token ->
        token.trim().toIntOrNull()?.takeIf { it >= 0 }
            ?: throw IllegalArgumentException("--streams requires comma-separated nonnegative stream indexes")
    }.distinct().sorted()

    private fun titleValue(value: String): Int? {
        val parsed = value.toIntOrNull() ?: throw IllegalArgumentException("--title must be an integer")
        require(parsed >= 0) { "--title must be >= 0" }
        return parsed.takeIf { it > 0 }
    }

    private fun tokenize(text: String): List<String> {
        val out = mutableListOf<String>()
        val current = StringBuilder()
        var quote: Char? = null
        var escaped = false
        fun flush() { if (current.isNotEmpty()) { out += current.toString(); current.setLength(0) } }
        text.forEach { ch ->
            when {
                escaped -> { current.append(ch); escaped = false }
                ch == '\\' && quote != '\'' -> escaped = true
                quote != null && ch == quote -> quote = null
                quote != null -> current.append(ch)
                ch == '\'' || ch == '"' -> quote = ch
                ch.isWhitespace() -> flush()
                else -> current.append(ch)
            }
        }
        require(!escaped && quote == null) { "Unterminated quote or escape in command" }
        flush()
        return out
    }
}

internal class MattMuxCliRunner(private val context: Context) {
    private val engine = AndroidNativeRemuxEngine()
    @Volatile private var cancelled = false
    @Volatile private var processor: AndroidBatchProcessor? = null

    fun cancel() {
        cancelled = true
        processor?.cancel()
        engine.cancel()
    }

    fun execute(
        commandLine: String,
        emit: (String) -> Unit,
        progress: (Int, String) -> Unit,
    ): Int {
        cancelled = false
        return when (val command = MattMuxCliSyntax.parse(commandLine)) {
            MattMuxCliCommand.Help -> {
                emit("MattRip CLI ${BuildConfig.VERSION_NAME}")
                emit("Usage:")
                emit("  mattmux-cli scan SOURCE")
                emit("  mattmux-cli metadata [--title N] SOURCE")
                emit("  mattmux-cli remux [--title N] [--output OUTPUT_ROOT] [--no-chapters] [--streams 0,2] SOURCE")
                emit("  mattmux-cli --batch [--log FILE] MOVIES_ROOT [OUTPUT_ROOT]")
                emit("  mattmux-cli --version")
                emit("Android SOURCE may be a persisted content:// DVD-folder tree URI or ISO document URI.")
                emit("MOVIES_ROOT and OUTPUT_ROOT are persisted content:// document-tree URIs.")
                emit("BATCH accepts Movie/VIDEO_TS folders plus unmounted ISO files. With no OUTPUT_ROOT, VIDEO_TS outputs go in the movie folder beside VIDEO_TS and ISO outputs go beside the ISO.")
                emit("--streams selects absolute stream indexes listed by metadata; omitted means all streams.")
                emit("ISO output defaults to Movie.mkv beside Movie.iso when its parent folder is writable; otherwise specify --output.")
                emit("For a DVD-folder SOURCE, output defaults to that folder. Existing MKVs are never overwritten.")
                0
            }
            MattMuxCliCommand.Version -> { emit("MattRip CLI ${BuildConfig.VERSION_NAME} (Android/ChromeOS native)"); 0 }
            is MattMuxCliCommand.Batch -> runBatch(command, emit, progress)
            is MattMuxCliCommand.Scan -> runScan(command, emit)
            is MattMuxCliCommand.Metadata -> runMetadata(command, emit)
            is MattMuxCliCommand.Remux -> runRemux(command, emit, progress)
        }
    }

    private fun runScan(command: MattMuxCliCommand.Scan, emit: (String) -> Unit): Int {
        requireEngine()
        val source = parseSourceUri(command.source, "SOURCE")
        val scan = engine.scanTitles(context, source)
        emit("TITLE   DURATION       DEFAULT")
        scan.titles.forEach { title ->
            emit(String.format(Locale.ROOT, "%-7d %-14s %s", title.title, formatDuration(title.durationMs), if (title.longest) "longest" else ""))
        }
        return 0
    }

    private fun runMetadata(command: MattMuxCliCommand.Metadata, emit: (String) -> Unit): Int {
        requireEngine()
        val source = parseSourceUri(command.source, "SOURCE")
        val result = engine.probeTitleMetadata(context, source, command.title)
        emit("Title: ${result.title}")
        emit("Duration: ${formatDuration(result.durationMs)}")
        emit("Chapters: ${result.chapterStartsMs.size}")
        result.chapterStartsMs.indices.forEach { index ->
            emit("  Chapter %02d  %s - %s".format(Locale.ROOT, index + 1, formatDuration(result.chapterStartsMs[index]), formatDuration(result.chapterEndsMs[index])))
        }
        emit("Streams: ${result.tracks.size}")
        result.tracks.forEach { emit("  ${it.displayLabel()}") }
        emit("Plan: ${result.planJson}")
        return 0
    }

    private fun runRemux(command: MattMuxCliCommand.Remux, emit: (String) -> Unit, progress: (Int, String) -> Unit): Int {
        requireEngine()
        val source = parseSourceUri(command.source, "SOURCE")
        val explicitOutput = command.outputRoot?.let { parseTreeUri(it, "OUTPUT_ROOT") }
        val destination = AndroidCliOutputResolver(context) {
            check(!cancelled) { "Remux cancelled" }
        }.resolve(source, explicitOutput)
        var title = command.title
        val indexes = command.streams?.let { requested ->
            val metadata = engine.probeTitleMetadata(context, source, title)
            validateCliStreamSelection(requested, metadata.tracks.filter { it.kind in listOf("video", "audio", "subtitle") }.map { it.index })
            title = metadata.title
            requested.toIntArray()
        }
        check(!cancelled) { "Remux cancelled" }
        engine.setProgressListener { percent -> progress(percent, "Remuxing… $percent%") }
        return try {
            val result = engine.remuxTitle(context, source, destination.folder, title, indexes, preserveChapters = !command.noChapters, outputName = destination.filename)
            emit("Output: ${result.outputUri}")
            emit("Title: ${result.title}")
            emit("Duration: ${formatDuration(result.durationMs)}")
            0
        } finally {
            engine.setProgressListener(null)
        }
    }

    private fun runBatch(command: MattMuxCliCommand.Batch, emit: (String) -> Unit, progress: (Int, String) -> Unit): Int {
        val input = parseTreeUri(command.inputRoot, "MOVIES_ROOT")
        val output = command.outputRoot?.let { parseTreeUri(it, "OUTPUT_ROOT") }
        val writer = command.logFile?.let { name ->
            require(name == File(name).name && name != "." && name != "..") { "Android --log must be an app-private filename, not a filesystem path" }
            File(context.filesDir, name).bufferedWriter()
        }
        val localProcessor = AndroidBatchProcessor(context)
        processor = localProcessor
        return try {
            val result = localProcessor.run(input, output, progress, log = { line ->
                writer?.apply { appendLine(line); flush() }
                emit(line)
            })
            emit("Result: completed=${result.completed} failed=${result.failures.size} total=${result.total}${if (result.cancelled) " cancelled=true" else ""}")
            result.outputs.forEach { emit("Output: $it") }
            result.failures.forEach { emit("Failure: ${it.movie}: ${it.message}") }
            when {
                result.cancelled -> 130
                result.failures.isNotEmpty() -> 1
                else -> 0
            }
        } finally {
            writer?.close()
            processor = null
        }
    }

    private fun requireEngine() {
        check(engine.isAvailable) { engine.unavailableReason ?: "Native MattRip engine unavailable" }
    }

    private fun parseSourceUri(value: String, label: String): Uri {
        val uri = Uri.parse(value)
        require(uri.scheme == "content" && (DocumentsContract.isTreeUri(uri) || DocumentsContract.isDocumentUri(context, uri))) {
            "$label must be a persisted content:// document or document-tree URI"
        }
        return uri
    }

    private fun parseTreeUri(value: String, label: String): Uri {
        val uri = Uri.parse(value)
        require(uri.scheme == "content" && DocumentsContract.isTreeUri(uri)) { "$label must be a content:// document-tree URI" }
        return uri
    }

    private fun formatDuration(ms: Long): String {
        val safe = ms.coerceAtLeast(0)
        val hours = safe / 3_600_000
        val minutes = (safe / 60_000) % 60
        val seconds = (safe / 1_000) % 60
        val millis = safe % 1_000
        return String.format(Locale.ROOT, "%d:%02d:%02d.%03d", hours, minutes, seconds, millis)
    }
}
