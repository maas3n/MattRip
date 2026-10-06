package io.github.maas3n.mattmux

import android.app.Activity
import android.app.AlertDialog
import android.content.Intent
import android.graphics.Typeface
import android.net.Uri
import android.os.Build
import android.os.Bundle
import android.provider.OpenableColumns
import android.view.Gravity
import android.view.View
import android.view.ViewGroup
import android.view.WindowInsets
import android.widget.Button
import android.widget.LinearLayout
import android.widget.ScrollView
import android.widget.TextView
import android.widget.Toast

class MainActivity : Activity(), BillingManager.Listener {

    companion object {
        private const val REQUEST_SOURCE_ISO = 1001
        private const val REQUEST_SOURCE_FOLDER = 1002
        private const val REQUEST_OUTPUT_FOLDER = 1003
        private const val STATE_SOURCE_URI = "source_uri"
        private const val STATE_OUTPUT_URI = "output_uri"
        private const val STATE_TRACK_SELECTION_SET = "track_selection_set"
        private const val STATE_SELECTED_TRACKS = "selected_tracks"
    }

    private lateinit var advancedMerger: AdvancedMergerPanel
    private lateinit var batchPanel: BatchPanel
    private lateinit var cliPanel: CliPanel

    private val engine: RemuxEngine = AndroidNativeRemuxEngine()
    private val tabMedia by lazy { TabMediaEngine(this, engine as AndroidNativeRemuxEngine) }
    private lateinit var demuxButton: Button
    private lateinit var includeChapters: android.widget.CheckBox
    private var billing: BillingManager? = null

    private lateinit var sourceValue: TextView
    private lateinit var outputValue: TextView
    private lateinit var proValue: TextView
    private lateinit var billingValue: TextView
    private lateinit var remuxStatus: TextView
    private lateinit var buyButton: Button
    private lateinit var remuxButton: Button
    private lateinit var cancelButton: Button
    private lateinit var tracksButton: Button

    private var sourceUri: Uri? = null
    private var outputUri: Uri? = null
    private var selectedTrackIndexes: Set<Int>? = null
    private var proOwned = false
    @Volatile private var remuxRunning = false
    private var tabOperation = false
    @Volatile private var metadataBusy = false

    override fun onCreate(savedInstanceState: Bundle?) {
        super.onCreate(savedInstanceState)
        val dvd = buildUi()
        advancedMerger = AdvancedMergerPanel(this)
        batchPanel = BatchPanel(this)
        cliPanel = CliPanel(this)
        val host = android.widget.TabHost(this).apply { id = android.R.id.tabhost }
        val layout = LinearLayout(this).apply { orientation = LinearLayout.VERTICAL }
        val tabs = android.widget.TabWidget(this).apply { id = android.R.id.tabs }
        val frame = android.widget.FrameLayout(this).apply { id = android.R.id.tabcontent }
        layout.addView(tabs); layout.addView(frame, LinearLayout.LayoutParams(-1, 0, 1f)); host.addView(layout); host.setup()
        applySystemBarInsets(layout, 0, 0, 0, 0)
        // Material themes may not supply the legacy TabHost indicator layout.
        // Providing our own views avoids attempting to inflate resource ID 0.
        host.addTab(host.newTabSpec("dvd").setIndicator(tabIndicator("REMUX/DEMUX")).setContent { dvd })
        host.addTab(host.newTabSpec("merger").setIndicator(tabIndicator("ADVANCED")).setContent { advancedMerger.view })
        host.addTab(host.newTabSpec("batch").setIndicator(tabIndicator("BATCH")).setContent { batchPanel.view })
        host.addTab(host.newTabSpec("cli").setIndicator(tabIndicator("CLI")).setContent { cliPanel.view })
        setContentView(host)
        restoreSelectionState(savedInstanceState)

        engine.setProgressListener { percent ->
            runOnUiThread {
                // DVD staging is only the first phase of demux; its 100% is not completion.
                if (remuxRunning && !tabOperation) remuxStatus.text = "Remuxing… $percent%"
            }
        }
        if (BuildConfig.ENABLE_BILLING_PURCHASES) {
            billing = BillingManager(this, this).also { it.start() }
        } else {
            proValue.text = "MattRip Pro: purchases disabled in this alpha"
            billingValue.text = "Billing is off until production device validation and release readiness are complete."
            buyButton.visibility = View.GONE
        }
        updateRemuxButton()
    }

    private fun tabIndicator(label: String): TextView = TextView(this).apply {
        text = label
        contentDescription = label
        gravity = Gravity.CENTER
        textSize = 13f
        minHeight = dp(48)
        setPadding(dp(8), dp(10), dp(8), dp(10))
        setTypeface(typeface, Typeface.BOLD)
        val accent = android.util.TypedValue()
        theme.resolveAttribute(android.R.attr.colorAccent, accent, true)
        background = android.graphics.drawable.StateListDrawable().apply {
            addState(intArrayOf(android.R.attr.state_selected),
                android.graphics.drawable.ColorDrawable((accent.data and 0x00ffffff) or 0x22000000))
            addState(intArrayOf(), android.graphics.drawable.ColorDrawable(android.graphics.Color.TRANSPARENT))
        }
    }

    override fun onSaveInstanceState(outState: Bundle) {
        outState.putString(STATE_SOURCE_URI, sourceUri?.toString())
        outState.putString(STATE_OUTPUT_URI, outputUri?.toString())
        outState.putBoolean(STATE_TRACK_SELECTION_SET, selectedTrackIndexes != null)
        selectedTrackIndexes?.let { outState.putIntArray(STATE_SELECTED_TRACKS, it.sorted().toIntArray()) }
        super.onSaveInstanceState(outState)
    }

    override fun onDestroy() {
        if (::advancedMerger.isInitialized) advancedMerger.destroy()
        if (::batchPanel.isInitialized) batchPanel.destroy()
        if (::cliPanel.isInitialized) cliPanel.destroy()
        tabMedia.destroy()
        if (remuxRunning) engine.cancel()
        engine.setProgressListener(null)
        billing?.close()
        super.onDestroy()
    }

    override fun onBillingState(state: BillingManager.State) {
        runOnUiThread {
            proOwned = state.proOwned
            proValue.text = if (state.proOwned) "MattRip Pro: unlocked" else "MattRip Pro: not unlocked"
            val price = state.price ?: "price loads from Google Play"
            buyButton.text = if (state.proOwned) "MattRip Pro owned" else "Buy MattRip Pro ($price)"
            buyButton.isEnabled = !state.proOwned && BuildConfig.ENABLE_BILLING_PURCHASES
            billingValue.text = state.message ?: if (state.connected) "Google Play connected" else "Google Play unavailable"
            updateRemuxButton()
        }
    }

    @Deprecated("Uses the platform document picker result API for minSdk simplicity.")
    override fun onActivityResult(requestCode: Int, resultCode: Int, data: Intent?) {
        super.onActivityResult(requestCode, resultCode, data)
        if (advancedMerger.onResult(requestCode, resultCode, data)) return
        if (batchPanel.onResult(requestCode, resultCode, data)) return
        if (cliPanel.onResult(requestCode, resultCode, data)) return
        if (resultCode != RESULT_OK || remuxRunning || metadataBusy) return
        val resultData = data ?: return
        val uri = resultData.data ?: return
        persistUriPermission(uri, resultData)

        when (requestCode) {
            REQUEST_SOURCE_ISO, REQUEST_SOURCE_FOLDER -> {
                sourceUri = uri
                sourceValue.text = describeUri(uri)
                clearTrackSelection()
            }
            REQUEST_OUTPUT_FOLDER -> {
                outputUri = uri
                outputValue.text = describeUri(uri)
            }
        }
        updateRemuxButton()
    }

    private fun restoreSelectionState(savedInstanceState: Bundle?) {
        sourceUri = savedInstanceState?.getString(STATE_SOURCE_URI)?.takeIf { it.isNotBlank() }?.let(Uri::parse)
        outputUri = savedInstanceState?.getString(STATE_OUTPUT_URI)?.takeIf { it.isNotBlank() }?.let(Uri::parse)
        selectedTrackIndexes = if (savedInstanceState?.getBoolean(STATE_TRACK_SELECTION_SET) == true) savedInstanceState.getIntArray(STATE_SELECTED_TRACKS)?.toSet() ?: emptySet() else null
        sourceValue.text = sourceUri?.let(::describeUri) ?: "No source selected"
        outputValue.text = outputUri?.let(::describeUri) ?: "No output folder selected"
    }

    private fun persistUriPermission(uri: Uri, data: Intent) {
        val readGranted = data.flags and Intent.FLAG_GRANT_READ_URI_PERMISSION != 0
        val writeGranted = data.flags and Intent.FLAG_GRANT_WRITE_URI_PERMISSION != 0
        try {
            when {
                readGranted && writeGranted -> contentResolver.takePersistableUriPermission(uri, Intent.FLAG_GRANT_READ_URI_PERMISSION or Intent.FLAG_GRANT_WRITE_URI_PERMISSION)
                writeGranted -> contentResolver.takePersistableUriPermission(uri, Intent.FLAG_GRANT_WRITE_URI_PERMISSION)
                readGranted -> contentResolver.takePersistableUriPermission(uri, Intent.FLAG_GRANT_READ_URI_PERMISSION)
            }
        } catch (_: SecurityException) {
        }
    }

    private fun buildUi(): ViewGroup {
        val horizontalPadding = dp(28)
        val topPadding = dp(24)
        val bottomPadding = dp(28)
        val root = LinearLayout(this).apply {
            orientation = LinearLayout.VERTICAL
            setPadding(horizontalPadding, topPadding, horizontalPadding, bottomPadding)
        }
        root.addView(TextView(this).apply {
            text = "MattRip"
            textSize = 30f
            setTypeface(typeface, Typeface.BOLD)
        })
        root.addView(TextView(this).apply {
            text = "DVD / VIDEO_TS / ISO / MKV → Remux or Demux"
            textSize = 16f
            setPadding(0, dp(4), 0, dp(22))
        })

        root.addView(section("Source"))
        sourceValue = value("No source selected")
        root.addView(sourceValue)
        val sourceButtons = sourceButtonContainer()
        sourceButtons.addView(button("Choose ISO / MKV") { chooseIso() })
        sourceButtons.addView(button("Choose DVD folder") { chooseSourceFolder() })
        root.addView(sourceButtons)

        root.addView(section("Output"))
        outputValue = value("No output folder selected")
        root.addView(outputValue)
        root.addView(button("Choose output folder") { chooseOutputFolder() })

        root.addView(section("Google Play"))
        proValue = value("MattRip Pro: checking…")
        billingValue = value("Connecting to Google Play…")
        root.addView(proValue)
        root.addView(billingValue)
        buyButton = button("Buy MattRip Pro") { billing?.launchProPurchase(this) }
        root.addView(buyButton)

        root.addView(section("Remux"))
        val runtimeMessage = engine.runtimeInfo?.let {
            "Bundled native runtime: $it\n\nVIDEO_TS folders and UDF ISO images use the same DVD title/cell planner. Select an unencrypted DVD; ISO files must be on storage that supports seeking. Interleaved multi-angle discs are not supported in this alpha."
        } ?: "The bundled native FFmpeg runtime could not be loaded in this build."
        root.addView(value(runtimeMessage))
        tracksButton = button("Scan / Show Metadata") { showTrackMetadata() }
        root.addView(tracksButton)
        remuxStatus = value("Ready")
        root.addView(remuxStatus)
        includeChapters = android.widget.CheckBox(this).apply { text = "Include chapters"; isChecked = true }
        root.addView(includeChapters)
        root.addView(value("DVD demux reads the title directly. Exported files need temporary space while saving; MKV inputs also need an input copy."))
        remuxButton = button("Start Remux") { startRemux() }
        demuxButton = button("Demux") { chooseDemux() }
        root.addView(LinearLayout(this).apply {
            orientation = LinearLayout.HORIZONTAL
            addView(remuxButton, LinearLayout.LayoutParams(0, LinearLayout.LayoutParams.WRAP_CONTENT, 1f))
            addView(demuxButton, LinearLayout.LayoutParams(0, LinearLayout.LayoutParams.WRAP_CONTENT, 1f))
        })
        cancelButton = button("Cancel") {
            if (tabOperation) tabMedia.cancel() else engine.cancel()
            remuxStatus.text = "Cancelling…"
        }.apply { isEnabled = false }
        root.addView(cancelButton)

        return ScrollView(this).apply { addView(root) }
    }

    private fun applySystemBarInsets(view: View, left: Int, top: Int, right: Int, bottom: Int) {
        view.setOnApplyWindowInsetsListener { target, insets ->
            val topInset: Int
            val bottomInset: Int
            if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.R) {
                topInset = insets.getInsets(WindowInsets.Type.statusBars()).top
                bottomInset = insets.getInsets(WindowInsets.Type.navigationBars()).bottom
            } else {
                @Suppress("DEPRECATION")
                topInset = insets.systemWindowInsetTop
                @Suppress("DEPRECATION")
                bottomInset = insets.systemWindowInsetBottom
            }
            target.setPadding(left, top + topInset, right, bottom + bottomInset)
            insets
        }
        view.post { view.requestApplyInsets() }
    }

    private fun startRemux() {
        val source = sourceUri ?: return
        val output = outputUri ?: return
        if (!engine.isAvailable) {
            toast(engine.unavailableReason ?: "Remux engine unavailable")
            return
        }
        if (BuildConfig.ENABLE_BILLING_PURCHASES && !proOwned) {
            billing?.launchProPurchase(this)
            return
        }
        if (remuxRunning) return
        val selectedStreams = selectedTrackIndexes?.sorted()?.toIntArray()
        if (selectedStreams != null && selectedStreams.isEmpty()) {
            toast("Select at least one video, audio, or subtitle track first")
            return
        }

        tabOperation = tabMedia.isMKV(source)
        val preserve = includeChapters.isChecked
        remuxRunning = true
        remuxStatus.text = "Preparing source…"
        updateRemuxButton()
        Thread {
            val result = runCatching {
                if (tabOperation) tabMedia.remuxMKV(source, output, selectedStreams, preserve)
                else (engine as AndroidNativeRemuxEngine).remuxTitle(this, source, output, null, selectedStreams, preserve).outputUri
            }
            runOnUiThread {
                remuxRunning = false
                result.onSuccess {
                    remuxStatus.text = "Complete: ${describeUri(it)}"
                    toast("Remux complete")
                }.onFailure {
                    remuxStatus.text = "Remux failed: ${it.message ?: it.javaClass.simpleName}"
                    toast(it.message ?: "Remux failed")
                }
                updateRemuxButton()
            }
        }.apply { name = "MattRip-remux" }.start()
    }

    private fun chooseDemux() {
        if (sourceUri?.let { tabMedia.isMKV(it) } == true) { startDemux(false); return }
        AlertDialog.Builder(this).setTitle("DVD video export format")
            .setItems(arrayOf("MPEG2 elementary video (.mpeg2)", "VOB video (.VOB)")) { _, choice -> startDemux(choice == 1) }
            .setNegativeButton("Cancel", null).show()
    }

    private fun startDemux(vob: Boolean) {
        val source = sourceUri ?: return
        val output = outputUri ?: return
        if (remuxRunning || metadataBusy || !engine.isAvailable) return
        if (BuildConfig.ENABLE_BILLING_PURCHASES && !proOwned) { billing?.launchProPurchase(this); return }
        val selection = selectedTrackIndexes?.sorted()?.toIntArray()
        val chapters = includeChapters.isChecked
        tabOperation = true
        remuxRunning = true; remuxStatus.text = "Preparing selected streams for demux…"; updateRemuxButton()
        Thread {
            val result = runCatching {
                tabMedia.demux(source, output, selection, chapters, vob) { message ->
                    runOnUiThread { remuxStatus.text = message }
                }
            }
            runOnUiThread {
                remuxRunning = false
                remuxStatus.text = result.fold({ "Demux complete: $it" }, { "Demux failed: ${it.message}" })
                toast(result.fold({ "Demux complete" }, { "Demux failed: ${it.message}" }))
                updateRemuxButton()
            }
        }.apply { name = "MattRip-demux" }.start()
    }

    private fun showTrackMetadata() {
        val source = sourceUri ?: run { toast("Choose a DVD or MKV source first"); return }
        if (!engine.isAvailable) { toast(engine.unavailableReason ?: "Remux engine unavailable"); return }
        if (metadataBusy || remuxRunning) return
        metadataBusy = true
        remuxStatus.text = "Reading title metadata…"
        updateRemuxButton()
        Thread {
            val result = runCatching { if (tabMedia.isMKV(source)) tabMedia.probeMKV(source) else engine.probeTracks(this, source) }
            runOnUiThread {
                metadataBusy = false
                result.onSuccess { probe ->
                    if (probe.tracks.isEmpty()) {
                        remuxStatus.text = "No selectable tracks were found"
                        toast("This source contains no selectable tracks")
                    } else {
                        showTrackDialog(probe)
                        remuxStatus.text = "Metadata loaded for title ${probe.title}. Choose tracks to include."
                    }
                }.onFailure {
                    remuxStatus.text = "Metadata failed: ${it.message ?: it.javaClass.simpleName}"
                    toast(it.message ?: "Metadata read failed")
                }
                updateRemuxButton()
            }
        }.apply { name = "MattRip-metadata" }.start()
    }

    private fun showTrackDialog(probe: TrackProbeResult) {
        val previous = selectedTrackIndexes
        val checked = BooleanArray(probe.tracks.size) { index -> previous?.contains(probe.tracks[index].index) ?: true }
        val dialog = AlertDialog.Builder(this)
            .setTitle("Title ${probe.title} — Tracks / Metadata")
            .setNeutralButton("MediaInfo details", null)
            .setMultiChoiceItems(probe.tracks.map { it.displayLabel() }.toTypedArray(), checked) { _, which, value -> checked[which] = value }
            .setPositiveButton("Use selection") { _, _ ->
                selectedTrackIndexes = probe.tracks.indices.filter { checked[it] }.map { probe.tracks[it].index }.toSet()
                val count = selectedTrackIndexes?.size ?: 0
                remuxStatus.text = if (count == 0) "No tracks selected. Select at least one track before remuxing or demuxing." else "$count track(s) selected for the next remux or demux."
                updateRemuxButton()
            }
            .setNegativeButton("Close", null)
            .create()
        dialog.show()
        dialog.getButton(AlertDialog.BUTTON_NEUTRAL).setOnClickListener {
            AlertDialog.Builder(this).setTitle("Source metadata")
                .setMessage(probe.details.ifBlank { "DVD metadata is supplied by libdvdnav/libdvdread." })
                .setPositiveButton("OK", null).show()
        }
    }

    private fun clearTrackSelection() {
        selectedTrackIndexes = null
        if (::remuxStatus.isInitialized) remuxStatus.text = "Source changed. Show Metadata to choose tracks, or remux all tracks by default."
        if (::tracksButton.isInitialized) updateRemuxButton()
    }

    private fun chooseIso() {
        val intent = Intent(Intent.ACTION_OPEN_DOCUMENT).apply {
            addCategory(Intent.CATEGORY_OPENABLE)
            type = "*/*"
            putExtra(Intent.EXTRA_MIME_TYPES, arrayOf("application/x-iso9660-image", "application/octet-stream", "video/x-matroska", "application/x-matroska"))
            addFlags(Intent.FLAG_GRANT_READ_URI_PERMISSION or Intent.FLAG_GRANT_PERSISTABLE_URI_PERMISSION)
        }
        startActivityForResult(intent, REQUEST_SOURCE_ISO)
    }

    private fun chooseSourceFolder() {
        startActivityForResult(Intent(Intent.ACTION_OPEN_DOCUMENT_TREE).apply {
            addFlags(Intent.FLAG_GRANT_READ_URI_PERMISSION or Intent.FLAG_GRANT_PERSISTABLE_URI_PERMISSION)
        }, REQUEST_SOURCE_FOLDER)
    }

    private fun chooseOutputFolder() {
        startActivityForResult(Intent(Intent.ACTION_OPEN_DOCUMENT_TREE).apply {
            addFlags(Intent.FLAG_GRANT_READ_URI_PERMISSION or Intent.FLAG_GRANT_WRITE_URI_PERMISSION or Intent.FLAG_GRANT_PERSISTABLE_URI_PERMISSION)
        }, REQUEST_OUTPUT_FOLDER)
    }

    private fun updateRemuxButton() {
        val hasPaths = sourceUri != null && outputUri != null
        val hasSelectedTracks = selectedTrackIndexes?.isNotEmpty() ?: true
        remuxButton.isEnabled = hasPaths && hasSelectedTracks && !remuxRunning && !metadataBusy && engine.isAvailable
        demuxButton.isEnabled = remuxButton.isEnabled
        includeChapters.isEnabled = !remuxRunning && !metadataBusy
        tracksButton.isEnabled = sourceUri != null && !remuxRunning && !metadataBusy && engine.isAvailable
        cancelButton.isEnabled = remuxRunning
        remuxButton.text = when {
            remuxRunning -> "Working…"
            !engine.isAvailable -> "Remux engine unavailable"
            selectedTrackIndexes != null && selectedTrackIndexes!!.isEmpty() -> "Select at least one track"
            BuildConfig.ENABLE_BILLING_PURCHASES && !proOwned -> "Unlock Pro to remux"
            else -> "Start Remux"
        }
    }

    private fun describeUri(uri: Uri): String {
        try {
            contentResolver.query(uri, arrayOf(OpenableColumns.DISPLAY_NAME), null, null, null)?.use { cursor ->
                val index = cursor.getColumnIndex(OpenableColumns.DISPLAY_NAME)
                if (index >= 0 && cursor.moveToFirst()) return cursor.getString(index)
            }
        } catch (_: SecurityException) {
        } catch (_: RuntimeException) {
        }
        return uri.toString()
    }

    private fun section(text: String) = TextView(this).apply {
        this.text = text
        textSize = 18f
        setTypeface(typeface, Typeface.BOLD)
        setPadding(0, dp(22), 0, dp(6))
    }

    private fun value(text: String) = TextView(this).apply {
        this.text = text
        textSize = 14f
        setPadding(0, dp(3), 0, dp(8))
    }

    private fun sourceButtonContainer() = LinearLayout(this).apply {
        orientation = if (WindowLayoutPolicy.stackSourceButtons(resources.configuration.screenWidthDp, resources.configuration.fontScale)) LinearLayout.VERTICAL else LinearLayout.HORIZONTAL
        gravity = Gravity.START
    }

    private fun button(text: String, onClick: () -> Unit) = Button(this).apply {
        this.text = text
        setOnClickListener { onClick() }
    }

    private fun toast(message: String) = Toast.makeText(this, message, Toast.LENGTH_LONG).show()
    private fun dp(value: Int): Int = (value * resources.displayMetrics.density).toInt()
}
