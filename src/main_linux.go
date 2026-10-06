//go:build linux && !cli

package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/app"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/layout"
	"fyne.io/fyne/v2/storage"
	"fyne.io/fyne/v2/widget"
)

type linuxSettings struct {
	OutputDir        string `json:"output_dir"`
	PreserveChapters bool   `json:"preserve_chapters"`
}
type linuxGUI struct {
	window                                                                     fyne.Window
	sourceEntry, outputEntry                                                   *widget.Entry
	titleSelect                                                                *widget.Select
	preserve                                                                   *widget.Check
	progress                                                                   *widget.ProgressBar
	status, trackSummary                                                       *widget.Label
	scanBtn, remuxBtn, demuxBtn, cancelBtn, dvdBtn, isoBtn, driveBtn, outputBtn *widget.Button
	mu                                                                         sync.Mutex
	busy                                                                       bool
	cancel                                                                     context.CancelFunc
	titles                                                                     []titleInfo
	titlesSource                                                               string
	trackSource                                                                string
	trackTitle                                                                 int
	tracks                                                                     []trackOption
	selectedTracks                                                             map[int]bool
}

func main() {
	if len(os.Args) > 1 && os.Args[1] == "--cli" {
		os.Args = append(os.Args[:1], os.Args[2:]...)
		desktopCLI()
		return
	}
	a := app.NewWithID("io.github.maas3n.mattrip")
	w := a.NewWindow(fmt.Sprintf("MattRip %s", appVersion))
	g := &linuxGUI{window: w}
	g.build()
	w.Resize(fyne.NewSize(840, 620))
	w.CenterOnScreen()
	w.Show()
	if len(os.Args) > 1 && os.Args[1] == "--graphics-self-test" {
		// Exercise the same Fyne window/context as normal startup, then let the
		// event loop render before closing. Used by the clean-runtime CI check.
		go func() {
			time.Sleep(time.Second)
			fyne.Do(func() { w.Close() })
		}()
		a.Run()
		fmt.Println("MattRip GUI graphics self-test: OK")
		return
	}
	go g.checkInstalledTools()
	a.Run()
}

func (g *linuxGUI) build() {
	s := loadLinuxSettings()
	if s.OutputDir == "" {
		s.OutputDir = defaultLinuxOutputDir()
	}
	g.sourceEntry = widget.NewEntry()
	g.sourceEntry.SetPlaceHolder("/path/to/DVD, disc.iso, or /dev/sr0")
	g.outputEntry = widget.NewEntry()
	g.outputEntry.SetText(s.OutputDir)
	g.titleSelect = widget.NewSelect(nil, func(string) { g.clearTrackSelection() })
	g.titleSelect.PlaceHolder = "Scan/select streams first"
	g.trackSummary = widget.NewLabel("Tracks: all streams (default)")
	g.trackSummary.Wrapping = fyne.TextWrapWord
	g.preserve = widget.NewCheck("Include chapters in remux / demux", func(bool) { g.saveSettings() })
	g.preserve.SetChecked(s.PreserveChapters)
	g.progress = widget.NewProgressBar()
	g.status = widget.NewLabel("Checking installed FFmpeg / FFprobe / MediaInfo…")
	g.status.Wrapping = fyne.TextWrapWord
	g.dvdBtn = widget.NewButton("DVD Folder…", g.chooseDVDFolder)
	g.isoBtn = widget.NewButton("ISO / MKV File…", g.chooseISO)
	g.driveBtn = widget.NewButton("DVD Drive…", g.chooseDVDDrive)
	g.outputBtn = widget.NewButton("Browse…", g.chooseOutput)
	g.scanBtn = widget.NewButton("SCAN/SELECT STREAMS", func() { g.startAsync("Scanning source and loading selectable streams…", g.scanSelectStreams) })
	g.remuxBtn = widget.NewButton("REMUX", func() { g.startAsync("Preparing remux…", g.remux) })
	g.demuxBtn = widget.NewButton("DEMUX", g.chooseDemux)
	g.remuxBtn.Importance = widget.HighImportance
	g.cancelBtn = widget.NewButton("Cancel", g.cancelCurrent)
	g.cancelBtn.Disable()
	aboutBtn := widget.NewButton("About", g.showAbout)
	g.sourceEntry.OnChanged = func(string) { g.invalidateTitles() }
	g.outputEntry.OnChanged = func(string) { g.saveSettings() }
	header := container.NewVBox(widget.NewLabelWithStyle("MattRip", fyne.TextAlignLeading, fyne.TextStyle{Bold: true}), widget.NewLabel("Lossless DVD title remuxing to Matroska — video, audio, subtitles, chapters and metadata."))
	sourceRow := container.NewBorder(nil, nil, nil, container.NewHBox(g.dvdBtn, g.isoBtn, g.driveBtn), g.sourceEntry)
	outputRow := container.NewBorder(nil, nil, nil, g.outputBtn, g.outputEntry)
	titleRow := container.NewBorder(nil, nil, nil, container.NewHBox(g.scanBtn, aboutBtn), g.titleSelect)
	timestampNotice := widget.NewLabel("Remux uses fixed timestamps (-fflags +genpts).")
	timestampNotice.Wrapping = fyne.TextWrapWord
	actions := container.NewHBox(layout.NewSpacer(), g.remuxBtn, g.demuxBtn, g.cancelBtn)
	dvdTab := container.NewPadded(container.NewVBox(header, widget.NewSeparator(), widget.NewLabelWithStyle("Source", fyne.TextAlignLeading, fyne.TextStyle{Bold: true}), sourceRow, widget.NewLabel("MEDIA: DRIVE / VIDEO_TS / ISO / MKV — choose a DVD folder / VIDEO_TS structure, ISO image, MKV file, or physical DVD drive."), widget.NewSeparator(), widget.NewLabelWithStyle("Destination", fyne.TextAlignLeading, fyne.TextStyle{Bold: true}), outputRow, widget.NewSeparator(), widget.NewLabelWithStyle("DVD Title", fyne.TextAlignLeading, fyne.TextStyle{Bold: true}), titleRow, g.trackSummary, g.preserve, widget.NewSeparator(), g.progress, g.status, layout.NewSpacer(), timestampNotice, actions))
	g.window.SetContent(container.NewAppTabs(container.NewTabItem("REMUX/DEMUX", dvdTab), container.NewTabItem("ADVANCED", g.buildAdvancedMerger()), container.NewTabItem("BATCH", g.buildBatch()), container.NewTabItem("CLI", g.buildCLI())))
}

func (g *linuxGUI) chooseDVDFolder() {
	d := dialog.NewFolderOpen(func(uri fyne.ListableURI, err error) {
		if err != nil {
			g.showError(err)
			return
		}
		if uri != nil {
			g.sourceEntry.SetText(uri.Path())
		}
	}, g.window)
	d.Show()
}
func (g *linuxGUI) chooseDVDDrive() {
	drives, err := listPhysicalDVDDrives()
	if err != nil {
		g.showError(err)
		return
	}
	if len(drives) == 0 {
		dialog.ShowInformation("DVD Drive", "No physical optical DVD/CD-ROM drive was detected.", g.window)
		return
	}
	options := make([]string, 0, len(drives))
	byLabel := make(map[string]string, len(drives))
	for _, drive := range drives {
		label := drive.Label + " — " + drive.Input
		options = append(options, label)
		byLabel[label] = drive.Input
	}
	selected := options[0]
	picker := widget.NewSelect(options, func(value string) { selected = value })
	picker.SetSelected(selected)
	dialog.NewCustomConfirm("Choose DVD Drive", "Use Drive", "Cancel", container.NewVBox(
		widget.NewLabel("Select the physical DVD drive to scan through FFmpeg dvdvideo/libdvdread/libdvdnav."),
		picker,
	), func(ok bool) {
		if ok {
			g.sourceEntry.SetText(byLabel[selected])
		}
	}, g.window).Show()
}

func (g *linuxGUI) chooseISO() {
	d := dialog.NewFileOpen(func(r fyne.URIReadCloser, err error) {
		if err != nil {
			g.showError(err)
			return
		}
		if r == nil {
			return
		}
		defer r.Close()
		g.sourceEntry.SetText(r.URI().Path())
	}, g.window)
	d.SetFilter(storage.NewExtensionFileFilter([]string{".iso", ".ISO", ".mkv", ".MKV"}))
	d.Show()
}
func (g *linuxGUI) chooseOutput() {
	d := dialog.NewFolderOpen(func(uri fyne.ListableURI, err error) {
		if err != nil {
			g.showError(err)
			return
		}
		if uri != nil {
			g.outputEntry.SetText(uri.Path())
		}
	}, g.window)
	d.Show()
}
func (g *linuxGUI) checkInstalledTools() {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	line := "System FFmpeg is missing/incompatible; MattRip will prepare its verified fallback when needed."
	if tools, err := inspectSystemTools(ctx); err == nil {
		line = fmt.Sprintf("Ready — using system FFmpeg (%s).", tools.ffmpeg)
	}
	fyne.Do(func() { g.status.SetText(line) })
}
func (g *linuxGUI) startAsync(label string, fn func(context.Context) error) {
	g.mu.Lock()
	if g.busy {
		g.mu.Unlock()
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	g.busy, g.cancel = true, cancel
	g.mu.Unlock()
	g.setBusy(true)
	g.setProgress(0, label)
	go func() {
		err := fn(ctx)
		g.mu.Lock()
		g.busy, g.cancel = false, nil
		g.mu.Unlock()
		fyne.Do(func() {
			g.setBusy(false)
			if errors.Is(err, context.Canceled) {
				g.progress.SetValue(0)
				g.status.SetText("Operation cancelled.")
				return
			}
			if err != nil {
				g.status.SetText("Failed: " + firstLine(err.Error()))
				dialog.ShowError(err, g.window)
			}
		})
	}()
}
func (g *linuxGUI) setBusy(b bool) {
	controls := []interface {
		Disable()
		Enable()
	}{g.sourceEntry, g.outputEntry, g.titleSelect, g.preserve, g.scanBtn, g.remuxBtn, g.demuxBtn, g.dvdBtn, g.isoBtn, g.driveBtn, g.outputBtn}
	for _, c := range controls {
		if b {
			c.Disable()
		} else {
			c.Enable()
		}
	}
	if b {
		g.cancelBtn.Enable()
	} else {
		g.cancelBtn.Disable()
	}
}
func (g *linuxGUI) cancelCurrent() {
	g.mu.Lock()
	c := g.cancel
	g.mu.Unlock()
	if c != nil {
		g.status.SetText("Cancelling…")
		c()
	}
}

func (g *linuxGUI) scan(ctx context.Context) error {
	src, err := normalizeTabSource(g.sourceEntry.Text)
	if err != nil {
		return err
	}
	tools, err := ensureTools(ctx, isMKVSource(src), g.progressCallback())
	if err != nil {
		return err
	}
	var titles []titleInfo
	if isMKVSource(src) {
		titles, err = scanMKV(ctx, tools, src)
	} else {
		titles, err = discoverDVDTitlesViaDVDVideo(ctx, src, tools, g.progressCallback())
	}
	if err != nil {
		return err
	}
	best, _ := longestTitle(titles)
	opts := make([]string, 0, len(titles))
	selected := ""
	for _, t := range titles {
		label := fmt.Sprintf("Title %d — %s", t.Number, formatDuration(t.Duration))
		opts = append(opts, label)
		if t.Number == best.Number {
			selected = label
		}
	}
	g.mu.Lock()
	g.titles = append([]titleInfo(nil), titles...)
	g.titlesSource = src
	g.mu.Unlock()
	fyne.Do(func() {
		g.titleSelect.Options = opts
		g.titleSelect.Refresh()
		g.titleSelect.SetSelected(selected)
		g.progress.SetValue(1)
		g.status.SetText(fmt.Sprintf("Found %d title(s). Selected title %d (%s) as the longest.", len(titles), best.Number, formatDuration(best.Duration)))
	})
	return nil
}
func parseTitleLabel(label string) int {
	fields := strings.Fields(strings.TrimSpace(label))
	if len(fields) < 2 || fields[0] != "Title" {
		return 0
	}
	n, err := strconv.Atoi(fields[1])
	if err != nil || n < 1 {
		return 0
	}
	return n
}
func (g *linuxGUI) selectedTitle() (string, titleInfo, error) {
	src, err := normalizeTabSource(g.sourceEntry.Text)
	if err != nil {
		return "", titleInfo{}, err
	}
	n := parseTitleLabel(g.titleSelect.Selected)
	if n < 1 {
		return "", titleInfo{}, errors.New("click SCAN/SELECT STREAMS and choose a title first")
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	if filepath.Clean(src) != filepath.Clean(g.titlesSource) {
		return "", titleInfo{}, errors.New("source changed; click SCAN/SELECT STREAMS again")
	}
	for _, t := range g.titles {
		if t.Number == n {
			return src, t, nil
		}
	}
	return "", titleInfo{}, errors.New("selected title is no longer available; click SCAN/SELECT STREAMS again")
}
func (g *linuxGUI) scanSelectStreams(ctx context.Context) error {
	if src, title, err := g.selectedTitle(); err == nil {
		return g.showMetadataFor(ctx, src, title)
	}
	if err := g.scan(ctx); err != nil {
		return err
	}
	g.mu.Lock()
	titles := append([]titleInfo(nil), g.titles...)
	src := g.titlesSource
	g.mu.Unlock()
	best, err := longestTitle(titles)
	if err != nil {
		return err
	}
	return g.showMetadataFor(ctx, src, best)
}

func (g *linuxGUI) showMetadata(ctx context.Context) error {
	src, title, err := g.selectedTitle()
	if err != nil {
		return err
	}
	return g.showMetadataFor(ctx, src, title)
}

func (g *linuxGUI) showMetadataFor(ctx context.Context, src string, title titleInfo) error {
	tools, err := ensureTools(ctx, true, g.progressCallback())
	if err != nil {
		return err
	}
	var probe ffprobeResult
	var mkvDetails string
	if isMKVSource(src) {
		probe, mkvDetails, err = probeTabMKV(ctx, tools, src)
	} else {
		probe, err = probeStreams(ctx, tools.ffprobe, src, title.Number)
	}
	if err != nil {
		return err
	}
	options := trackOptionsFromProbe(probe)
	if len(options) == 0 {
		return errors.New("this DVD title contains no selectable video, audio, or subtitle tracks")
	}
	text := mkvDetails
	if !isMKVSource(src) {
		text, err = metadataText(ctx, src, title, tools, g.preserve.Checked)
	}
	if err != nil {
		return err
	}
	g.setTrackOptions(src, title.Number, options)
	fyne.Do(func() {
		g.updateTrackSummary(src, title.Number)
		g.showTrackDialog(src, title, options, text)
		g.progress.SetValue(1)
		g.status.SetText(fmt.Sprintf("Metadata loaded for title %d. Choose the tracks to include in the next remux.", title.Number))
	})
	return nil
}

func (g *linuxGUI) setTrackOptions(src string, title int, options []trackOption) {
	g.mu.Lock()
	defer g.mu.Unlock()
	selected := make(map[int]bool, len(options))
	if g.trackSource == src && g.trackTitle == title && g.selectedTracks != nil {
		for _, option := range options {
			value, ok := g.selectedTracks[option.Index]
			if !ok {
				value = true
			}
			selected[option.Index] = value
		}
	} else {
		for _, option := range options {
			selected[option.Index] = true
		}
	}
	g.trackSource = src
	g.trackTitle = title
	g.tracks = append([]trackOption(nil), options...)
	g.selectedTracks = selected
}

func (g *linuxGUI) clearTrackSelection() {
	g.mu.Lock()
	g.trackSource = ""
	g.trackTitle = 0
	g.tracks = nil
	g.selectedTracks = nil
	g.mu.Unlock()
	if g.trackSummary != nil {
		g.trackSummary.SetText("Tracks: all streams (default)")
	}
}

func (g *linuxGUI) setTrackChecked(src string, title, index int, checked bool) {
	g.mu.Lock()
	valid := g.trackSource == src && g.trackTitle == title && g.selectedTracks != nil
	if valid {
		g.selectedTracks[index] = checked
	}
	g.mu.Unlock()
	if valid {
		g.updateTrackSummary(src, title)
	}
}

func (g *linuxGUI) trackSelectionSnapshot(src string, title int) map[int]bool {
	g.mu.Lock()
	defer g.mu.Unlock()
	result := make(map[int]bool, len(g.selectedTracks))
	if g.trackSource != src || g.trackTitle != title || g.selectedTracks == nil {
		return result
	}
	for index, value := range g.selectedTracks {
		result[index] = value
	}
	return result
}

func (g *linuxGUI) selectedTrackIndexes(src string, title int) ([]int, bool) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.trackSource != src || g.trackTitle != title || g.selectedTracks == nil {
		return nil, false
	}
	indexes := make([]int, 0, len(g.tracks))
	for _, option := range g.tracks {
		if g.selectedTracks[option.Index] {
			indexes = append(indexes, option.Index)
		}
	}
	return indexes, true
}

func (g *linuxGUI) updateTrackSummary(src string, title int) {
	if g.trackSummary == nil {
		return
	}
	indexes, hasSelection := g.selectedTrackIndexes(src, title)
	if !hasSelection {
		g.trackSummary.SetText("Tracks: all streams (default)")
		return
	}
	if len(indexes) == 0 {
		g.trackSummary.SetText(fmt.Sprintf("Tracks: none selected for title %d", title))
		return
	}
	g.trackSummary.SetText(fmt.Sprintf("Tracks: %d selected for title %d", len(indexes), title))
}

func (g *linuxGUI) showTrackDialog(src string, title titleInfo, options []trackOption, detailsText string) {
	selected := g.trackSelectionSnapshot(src, title.Number)
	instruction := widget.NewLabel("Choose the video, audio, and subtitle tracks to include in the next remux. All tracks are selected by default.")
	instruction.Wrapping = fyne.TextWrapWord
	checks := make([]*widget.Check, len(options))
	rows := container.NewVBox()
	for i, option := range options {
		index := option.Index
		check := widget.NewCheck(option.Label(), func(value bool) { g.setTrackChecked(src, title.Number, index, value) })
		check.SetChecked(selected[index])
		checks[i] = check
		rows.Add(check)
	}
	trackScroll := container.NewVScroll(rows)
	// A disabled Entry uses Fyne's disabled foreground color, which made the
	// metadata nearly unreadable on dark Linux themes. Render it as normal theme
	// text inside a scroll pane instead; the metadata remains read-only.
	details := widget.NewLabel(detailsText)
	details.Wrapping = fyne.TextWrapWord
	detailsScroll := container.NewVScroll(container.NewPadded(details))
	split := container.NewVSplit(trackScroll, detailsScroll)
	split.Offset = 0.55
	selectAll := widget.NewButton("Select all", func() {
		for _, check := range checks {
			check.SetChecked(true)
		}
	})
	selectNone := widget.NewButton("Select none", func() {
		for _, check := range checks {
			check.SetChecked(false)
		}
	})
	var trackDialog *dialog.CustomDialog
	closeBtn := widget.NewButton("Close & use selection", func() {
		indexes, _ := g.selectedTrackIndexes(src, title.Number)
		if len(indexes) == 0 {
			g.status.SetText("No tracks selected. Choose at least one track before starting the remux.")
			dialog.ShowInformation("Choose at least one track", "Select at least one video, audio, or subtitle track before closing the track selector.", g.window)
			return
		}
		g.updateTrackSummary(src, title.Number)
		g.status.SetText(fmt.Sprintf("Track selection updated: %d track(s) will be included in the next remux.", len(indexes)))
		trackDialog.Dismiss()
	})
	buttons := container.NewHBox(selectAll, selectNone, layout.NewSpacer(), closeBtn)
	content := container.NewBorder(instruction, buttons, nil, nil, split)
	trackDialog = dialog.NewCustomWithoutButtons(fmt.Sprintf("MattRip — Title %d Tracks / Metadata", title.Number), content, g.window)
	trackDialog.Show()
	trackDialog.Resize(fyne.NewSize(800, 680))
}

func (g *linuxGUI) remux(ctx context.Context) error {
	src, title, err := g.selectedTitle()
	if err != nil {
		return err
	}
	out := strings.TrimSpace(g.outputEntry.Text)
	if out == "" {
		return errors.New("choose an output folder")
	}
	g.saveSettings()
	indexes, hasSelection := g.selectedTrackIndexes(src, title.Number)
	if hasSelection && len(indexes) == 0 {
		return errors.New("select at least one video, audio, or subtitle track before remuxing")
	}
	if !hasSelection {
		indexes = nil
	}
	tools, err := ensureTools(ctx, false, g.progressCallback())
	if err != nil {
		return err
	}
	var final string
	if isMKVSource(src) {
		final, err = remuxMKV(ctx, tools, src, out, indexes, g.preserve.Checked)
	} else {
		final, err = remuxTitle(ctx, src, title, out, g.preserve.Checked, indexes, tools, g.progressCallback())
	}
	if err != nil {
		return err
	}
	fyne.Do(func() { dialog.ShowInformation("Remux complete", "Created:\n"+final, g.window) })
	return nil
}

func (g *linuxGUI) progressCallback() progressFunc {
	return func(frac float64, status string) {
		fyne.Do(func() {
			if frac < 0 {
				frac = 0
			}
			if frac > 1 {
				frac = 1
			}
			g.progress.SetValue(frac)
			if status != "" {
				g.status.SetText(status)
			}
		})
	}
}
func (g *linuxGUI) setProgress(frac float64, status string) {
	g.progress.SetValue(frac)
	g.status.SetText(status)
}
func (g *linuxGUI) invalidateTitles() {
	g.mu.Lock()
	g.titles = nil
	g.titlesSource = ""
	g.trackSource = ""
	g.trackTitle = 0
	g.tracks = nil
	g.selectedTracks = nil
	g.mu.Unlock()
	if g.trackSummary != nil {
		g.trackSummary.SetText("Tracks: all streams (default)")
	}
	g.titleSelect.ClearSelected()
	g.titleSelect.Options = nil
	g.titleSelect.Refresh()
}
func (g *linuxGUI) showError(err error) {
	if err != nil {
		dialog.ShowError(err, g.window)
	}
}
func (g *linuxGUI) showAbout() {
	dialog.ShowInformation("About MattRip", fmt.Sprintf("MattRip %s\n\nLinux desktop + CLI DVD-to-MKV remuxer.\n\nMattRip first uses compatible system ffmpeg/ffprobe tools. If FFmpeg does not expose the dvdvideo demuxer, a pinned SHA-256-verified fallback is prepared in your user cache. MediaInfo is optional.\n\nMattRip does not bypass DVD copy protection such as CSS.", appVersion), g.window)
}
func firstLine(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return s[:i]
	}
	return s
}
func defaultLinuxOutputDir() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return "."
	}
	videos := filepath.Join(home, "Videos")
	if st, err := os.Stat(videos); err == nil && st.IsDir() {
		return videos
	}
	return home
}
func linuxSettingsPath() string {
	root, err := os.UserConfigDir()
	if err != nil || root == "" {
		home, _ := os.UserHomeDir()
		root = filepath.Join(home, ".config")
	}
	dir := filepath.Join(root, "mattrip")
	_ = os.MkdirAll(dir, 0700)
	return filepath.Join(dir, "settings.json")
}
func loadLinuxSettings() linuxSettings {
	s := linuxSettings{PreserveChapters: true}
	if b, err := os.ReadFile(linuxSettingsPath()); err == nil {
		_ = json.Unmarshal(b, &s)
	}
	return s
}
func (g *linuxGUI) saveSettings() {
	if g.outputEntry == nil || g.preserve == nil {
		return
	}
	s := linuxSettings{OutputDir: strings.TrimSpace(g.outputEntry.Text), PreserveChapters: g.preserve.Checked}
	b, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return
	}
	path := linuxSettingsPath()
	tmp := path + ".tmp"
	if os.WriteFile(tmp, b, 0600) == nil {
		_ = os.Rename(tmp, path)
	}
}

func (g *linuxGUI) chooseDemux() {
	format := widget.NewSelect([]string{"MPEG2 elementary video (.mpeg2)", "VOB video (.VOB)"}, nil)
	format.SetSelectedIndex(0)
	confirm := func(ok bool) {
		if !ok {
			return
		}
		video := "mpeg2"
		if format.SelectedIndex() == 1 {
			video = "vob"
		}
		g.startAsync("Demuxing selected tracks…", func(ctx context.Context) error {
			src, title, err := g.selectedTitle()
			if err != nil {
				return err
			}
			indexes, selected := g.selectedTrackIndexes(src, title.Number)
			if !selected {
				indexes = nil
			}
			tools, err := ensureTools(ctx, false, g.progressCallback())
			if err != nil {
				return err
			}
			final, err := demuxTab(ctx, tools, src, title.Number, strings.TrimSpace(g.outputEntry.Text), indexes, g.preserve.Checked, video)
			if err != nil {
				return err
			}
			fyne.Do(func() {
				g.progress.SetValue(1)
				g.status.SetText("Demux complete: " + final)
				dialog.ShowInformation("Demux complete", final, g.window)
			})
			return nil
		})
	}
	if src, _, err := g.selectedTitle(); err == nil && isMKVSource(src) {
		confirm(true)
		return
	}
	dialog.NewCustomConfirm("Demux selected tracks", "Demux", "Cancel", container.NewVBox(widget.NewLabel("DVD MPEG-2 video export format"), format), confirm, g.window).Show()
}
