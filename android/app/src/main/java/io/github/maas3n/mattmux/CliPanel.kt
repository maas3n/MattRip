package io.github.maas3n.mattmux

import android.app.Activity
import android.content.Intent
import android.net.Uri
import android.view.View
import android.widget.Button
import android.widget.EditText
import android.widget.LinearLayout
import android.widget.ProgressBar
import android.widget.ScrollView
import android.widget.TextView

class CliPanel(private val activity: Activity) {
    companion object {
        private const val REQUEST_SOURCE_ISO = 8300
        private const val REQUEST_SOURCE_FOLDER = 8301
        private const val REQUEST_OUTPUT = 8302
    }

    private enum class Preset { HELP, SCAN, METADATA, REMUX, BATCH }

    private val runner = MattMuxCliRunner(activity)
    private val controls = mutableListOf<View>()
    private var source: Uri? = null
    private var output: Uri? = null
    private var preset = Preset.HELP
    @Volatile private var busy = false
    @Volatile private var destroyed = false

    private val command = EditText(activity).apply { setText("mattrip-cli --help"); minLines = 2 }
    private val progress = ProgressBar(activity, null, android.R.attr.progressBarStyleHorizontal).apply { max = 100 }
    private val console = TextView(activity).apply { text = "Android mattrip-cli ready. Pick a DVD folder or ISO, choose a command preset, then edit the command if needed." }
    private val cancel = Button(activity).apply { text = "Cancel"; isEnabled = false; setOnClickListener { runner.cancel(); append("Cancelling…") } }
    val view: View

    init {
        val padding = (24 * activity.resources.displayMetrics.density).toInt()
        val content = LinearLayout(activity).apply { orientation = LinearLayout.VERTICAL; setPadding(padding, padding, padding, padding) }
        content.addView(TextView(activity).apply { text = "mattrip-cli"; textSize = 20f })
        content.addView(TextView(activity).apply {
            text = "Native Android CLI surface: scan, metadata, remux and --batch. Android storage uses persisted content:// URIs instead of shell filesystem paths."
        })
        fun button(label: String, action: () -> Unit) = Button(activity).apply { text = label; setOnClickListener { action() }; controls += this; content.addView(this) }

        button("CHOOSE ISO SOURCE") { chooseIso() }
        button("CHOOSE DVD FOLDER SOURCE") { chooseTree(REQUEST_SOURCE_FOLDER) }
        button("CHOOSE OUTPUT FOLDER") { chooseTree(REQUEST_OUTPUT) }
        button("CLEAR OUTPUT FOLDER") { output = null; refreshCommand() }

        button("PRESET: SCAN") { preset = Preset.SCAN; refreshCommand() }
        button("PRESET: METADATA") { preset = Preset.METADATA; refreshCommand() }
        button("PRESET: REMUX") { preset = Preset.REMUX; refreshCommand() }
        button("PRESET: BATCH") { preset = Preset.BATCH; refreshCommand() }

        content.addView(command); controls += command
        button("RUN mattrip-cli") { runCommand() }
        content.addView(cancel)
        content.addView(progress)
        content.addView(console)
        view = ScrollView(activity).apply { addView(content) }
    }

    fun onResult(request: Int, result: Int, data: Intent?): Boolean {
        if (request !in setOf(REQUEST_SOURCE_ISO, REQUEST_SOURCE_FOLDER, REQUEST_OUTPUT)) return false
        if (result != Activity.RESULT_OK || data == null || busy) return true
        val uri = data.data ?: return true
        persist(uri, data.flags)
        if (request == REQUEST_OUTPUT) {
            output = uri
        } else {
            source = uri
            if (preset == Preset.HELP) preset = Preset.SCAN
        }
        refreshCommand()
        return true
    }

    fun destroy() { destroyed = true; if (busy) runner.cancel() }

    private fun chooseIso() {
        activity.startActivityForResult(Intent(Intent.ACTION_OPEN_DOCUMENT).apply {
            addCategory(Intent.CATEGORY_OPENABLE)
            type = "*/*"
            addFlags(Intent.FLAG_GRANT_READ_URI_PERMISSION or Intent.FLAG_GRANT_PERSISTABLE_URI_PERMISSION)
        }, REQUEST_SOURCE_ISO)
    }

    private fun chooseTree(request: Int) {
        activity.startActivityForResult(Intent(Intent.ACTION_OPEN_DOCUMENT_TREE).apply {
            addFlags(Intent.FLAG_GRANT_READ_URI_PERMISSION or Intent.FLAG_GRANT_WRITE_URI_PERMISSION or Intent.FLAG_GRANT_PERSISTABLE_URI_PERMISSION)
        }, request)
    }

    private fun persist(uri: Uri, flags: Int) {
        val wanted = flags and (Intent.FLAG_GRANT_READ_URI_PERMISSION or Intent.FLAG_GRANT_WRITE_URI_PERMISSION)
        if (wanted != 0) runCatching { activity.contentResolver.takePersistableUriPermission(uri, wanted) }
    }

    private fun refreshCommand() {
        val src = source
        val text = when (preset) {
            Preset.HELP -> "mattrip-cli --help"
            Preset.SCAN -> src?.let { "mattrip-cli scan \"$it\"" } ?: "mattrip-cli scan SOURCE"
            Preset.METADATA -> src?.let { "mattrip-cli metadata \"$it\"" } ?: "mattrip-cli metadata SOURCE"
            Preset.REMUX -> src?.let {
                buildString {
                    append("mattrip-cli remux")
                    output?.let { out -> append(" --output \""); append(out); append('"') }
                    append(" \""); append(it); append('"')
                }
            } ?: "mattrip-cli remux SOURCE"
            Preset.BATCH -> src?.let {
                buildString {
                    append("mattrip-cli --batch \""); append(it); append('"')
                    output?.let { out -> append(" \""); append(out); append('"') }
                }
            } ?: "mattrip-cli --batch MOVIES_ROOT"
        }
        command.setText(text)
    }

    private fun runCommand() {
        if (busy) return
        busy = true
        console.text = ""
        progress.progress = 0
        controls.forEach { it.isEnabled = false }
        cancel.isEnabled = true
        val text = command.text.toString()
        Thread {
            val result = runCatching {
                runner.execute(text, emit = { line -> activity.runOnUiThread { if (!destroyed) append(line) } }, progress = { percent, message ->
                    activity.runOnUiThread { if (!destroyed) { progress.progress = percent; append(message) } }
                })
            }
            activity.runOnUiThread {
                if (destroyed) return@runOnUiThread
                busy = false
                controls.forEach { it.isEnabled = true }
                cancel.isEnabled = false
                result.onSuccess { code -> append("mattrip-cli exit code: $code") }
                    .onFailure { append("mattrip-cli error: ${it.message ?: it.javaClass.simpleName}") }
            }
        }.apply { name = "MattRip-Android-CLI" }.start()
    }

    private fun append(line: String) {
        console.text = if (console.text.isEmpty()) line else "${console.text}\n$line"
    }
}
