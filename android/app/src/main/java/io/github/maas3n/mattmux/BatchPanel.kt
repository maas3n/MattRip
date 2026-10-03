package io.github.maas3n.mattmux

import android.app.Activity
import android.content.Intent
import android.net.Uri
import android.view.View
import android.widget.Button
import android.widget.LinearLayout
import android.widget.ProgressBar
import android.widget.ScrollView
import android.widget.TextView

class BatchPanel(private val activity: Activity) {
    companion object { private const val REQUEST_INPUT = 8200; private const val REQUEST_OUTPUT = 8201 }

    private val processor = AndroidBatchProcessor(activity)
    private val controls = mutableListOf<View>()
    private var input: Uri? = null
    private var output: Uri? = null
    @Volatile private var busy = false
    @Volatile private var destroyed = false

    private val inputLabel = TextView(activity).apply { text = "No movie collection folder selected" }
    private val outputLabel = TextView(activity).apply { text = "Optional — blank writes beside VIDEO_TS or beside each ISO" }
    private val progress = ProgressBar(activity, null, android.R.attr.progressBarStyleHorizontal).apply { max = 100; progress = 0 }
    private val status = TextView(activity).apply { text = "Choose the collection folder containing Movie Title/VIDEO_TS folders and/or unmounted ISO files, then click ONECLICK BATCH." }
    private val cancel = Button(activity).apply { text = "Cancel"; isEnabled = false; setOnClickListener { processor.cancel(); status.text = "Cancelling…" } }
    val view: View

    init {
        val padding = (24 * activity.resources.displayMetrics.density).toInt()
        val content = LinearLayout(activity).apply { orientation = LinearLayout.VERTICAL; setPadding(padding, padding, padding, padding) }
        content.addView(TextView(activity).apply { text = "BATCH — lossless DVD collection remux"; textSize = 20f })
        content.addView(TextView(activity).apply {
            text = "MattRip scans Movie Title/VIDEO_TS folders and unmounted ISO files, uses libdvdnav/libdvdread to select the longest title, then native libav stream-copy to MKV. With no output folder, VIDEO_TS outputs are written in the movie folder beside VIDEO_TS and ISO outputs are written beside the ISO."
        })
        fun button(label: String, action: () -> Unit) = Button(activity).apply { text = label; setOnClickListener { action() }; controls += this; content.addView(this) }
        button("CHOOSE MOVIE FOLDER") { choose(REQUEST_INPUT) }
        content.addView(inputLabel)
        button("CHOOSE OUTPUT FOLDER (OPTIONAL)") { choose(REQUEST_OUTPUT) }
        button("CLEAR OUTPUT FOLDER") { output = null; outputLabel.text = "Optional — blank writes beside VIDEO_TS or beside each ISO" }
        content.addView(outputLabel)
        button("ONECLICK BATCH") { start() }
        content.addView(cancel)
        content.addView(progress)
        content.addView(status)
        view = ScrollView(activity).apply { addView(content) }
    }

    fun onResult(request: Int, result: Int, data: Intent?): Boolean {
        if (request != REQUEST_INPUT && request != REQUEST_OUTPUT) return false
        if (result != Activity.RESULT_OK || data == null || busy) return true
        val uri = data.data ?: return true
        persist(uri, data.flags)
        if (request == REQUEST_INPUT) {
            input = uri
            inputLabel.text = uri.toString()
        } else {
            output = uri
            outputLabel.text = uri.toString()
        }
        return true
    }

    fun destroy() {
        destroyed = true
        if (busy) processor.cancel()
    }

    private fun choose(request: Int) {
        activity.startActivityForResult(Intent(Intent.ACTION_OPEN_DOCUMENT_TREE).apply {
            addFlags(Intent.FLAG_GRANT_READ_URI_PERMISSION or Intent.FLAG_GRANT_WRITE_URI_PERMISSION or Intent.FLAG_GRANT_PERSISTABLE_URI_PERMISSION)
        }, request)
    }

    private fun persist(uri: Uri, flags: Int) {
        val wanted = flags and (Intent.FLAG_GRANT_READ_URI_PERMISSION or Intent.FLAG_GRANT_WRITE_URI_PERMISSION)
        if (wanted == 0) return
        runCatching { activity.contentResolver.takePersistableUriPermission(uri, wanted) }
    }

    private fun start() {
        val root = input ?: run { status.text = "Choose the movie collection folder first."; return }
        if (busy) return
        busy = true
        progress.progress = 0
        status.text = "Discovering DVD folders and ISO files…"
        controls.forEach { it.isEnabled = false }
        cancel.isEnabled = true
        Thread {
            val result = runCatching {
                processor.run(root, output, progress = { percent, message ->
                    activity.runOnUiThread { if (!destroyed) { progress.progress = percent; status.text = message } }
                })
            }
            activity.runOnUiThread {
                if (destroyed) return@runOnUiThread
                busy = false
                controls.forEach { it.isEnabled = true }
                cancel.isEnabled = false
                result.onSuccess { batch ->
                    progress.progress = if (batch.cancelled) progress.progress else 100
                    status.text = when {
                        batch.cancelled -> "Batch cancelled: ${batch.completed}/${batch.total} completed."
                        batch.failures.isEmpty() -> "Batch complete: ${batch.completed} movie(s) remuxed."
                        else -> "Batch complete: ${batch.completed}/${batch.total} completed; ${batch.failures.size} failed. " + batch.failures.joinToString("; ") { "${it.movie}: ${it.message}" }
                    }
                }.onFailure { status.text = "Batch failed: ${it.message ?: it.javaClass.simpleName}" }
            }
        }.apply { name = "MattRip-Android-BATCH" }.start()
    }
}