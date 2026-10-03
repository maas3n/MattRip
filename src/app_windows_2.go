//go:build windows

package main

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"syscall"
	"time"
	"unsafe"
)

func setSource(p string) {
	p = strings.TrimSpace(p)
	normalized, err := normalizeTabSource(p)
	if err != nil {
		messageBox(app.hwnd, "Invalid source", err.Error(), MB_OK|MB_ICONWARNING)
		return
	}
	setText(app.sourceEdit, normalized)
	app.invalidateTitles()
	setProgress(0)
	setStatus("Source selected. Scan titles to identify the main feature.")
}
func droppedPath(hDrop uintptr) string {
	defer procDragFinish.Call(hDrop)
	n, _, _ := procDragQueryFileW.Call(hDrop, 0, 0, 0)
	if n == 0 {
		return ""
	}
	buf := make([]uint16, n+1)
	procDragQueryFileW.Call(hDrop, 0, uintptr(unsafe.Pointer(&buf[0])), uintptr(len(buf)))
	return syscall.UTF16ToString(buf)
}
func (a *application) invalidateTitles() {
	a.titlesMu.Lock()
	a.titles = nil
	a.titlesSource = ""
	a.titlesMu.Unlock()
	clearWindowsTrackSelection()
	if a.titleCombo != 0 {
		procSendMessageW.Call(a.titleCombo, CB_RESETCONTENT, 0, 0)
	}
}

func startAsync(label string, fn func(context.Context) error) {
	if !app.busy.CompareAndSwap(false, true) {
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	app.cancelMu.Lock()
	app.cancel = cancel
	app.cancelMu.Unlock()
	setBusyUI(true)
	setStatus(label)
	log.Printf("operation start: %s", label)
	go func() {
		err := fn(ctx)
		cancelled := errors.Is(err, context.Canceled) || errors.Is(ctx.Err(), context.Canceled)
		if err != nil && !cancelled {
			log.Printf("operation failed: %v", err)
			setStatus("Failed: " + firstLine(err.Error()))
			messageBox(app.hwnd, "MattRip", err.Error(), MB_OK|MB_ICONERROR)
		} else if cancelled {
			log.Printf("operation cancelled")
			setStatus("Operation cancelled.")
			setProgress(0)
		}
		app.cancelMu.Lock()
		app.cancel = nil
		app.cancelMu.Unlock()
		app.busy.Store(false)
		setBusyUI(false)
	}()
}
func (a *application) cancelCurrent() {
	a.cancelMu.Lock()
	c := a.cancel
	a.cancelMu.Unlock()
	if c != nil {
		setStatus("Cancelling…")
		c()
	}
}
func setBusyUI(busy bool) {
	enabled := uintptr(1)
	if busy {
		enabled = 0
	}
	procEnableWindow.Call(app.scanBtn, enabled)
	procEnableWindow.Call(app.metaBtn, enabled)
	procEnableWindow.Call(app.remuxBtn, enabled)
	procEnableWindow.Call(app.demuxBtn, enabled)
	procEnableWindow.Call(app.sourceEdit, enabled)
	procEnableWindow.Call(app.sourceDVDButton, enabled)
	procEnableWindow.Call(app.sourceISOButton, enabled)
	procEnableWindow.Call(app.outputEdit, enabled)
	procEnableWindow.Call(app.outputButton, enabled)
	procEnableWindow.Call(app.titleCombo, enabled)
	procEnableWindow.Call(app.preserveChapters, enabled)
	procEnableWindow.Call(app.cancelBtn, 1-enabled)
}

func discoverDVDTitlesViaDVDVideo(ctx context.Context) error {
	src, err := currentSource()
	if err != nil {
		return err
	}
	tools, err := ensureTools(ctx, isMKVSource(src))
	if err != nil {
		return err
	}

	if isMKVSource(src) {
		titles, err := scanMKV(ctx, tools, src)
		if err != nil {
			return err
		}
		app.titlesMu.Lock()
		app.titles = titles
		app.titlesSource = src
		app.titlesMu.Unlock()
		procSendMessageW.Call(app.titleCombo, CB_RESETCONTENT, 0, 0)
		label := utf16Ptr("Title 1 — MKV — " + formatDuration(titles[0].Duration))
		procSendMessageW.Call(app.titleCombo, CB_ADDSTRING, 0, uintptr(unsafe.Pointer(label)))
		procSendMessageW.Call(app.titleCombo, CB_SETCURSEL, 0, 0)
		setProgress(1)
		setStatus("MKV scanned with MediaInfo. Show Metadata to select tracks.")
		return nil
	}

	// FFmpeg's dvdvideo demuxer accepts title numbers 1..99 and uses
	// libdvdread/libdvdnav as its source of truth. Deliberately probe the full
	// title-number range instead of parsing VIDEO_TS.IFO in MattRip. This keeps
	// folder and ISO title discovery on the same libdvdread/libdvdnav path.
	maxTitle := 99

	var titles []titleInfo
	for n := 1; n <= maxTitle; n++ {
		if err := ctx.Err(); err != nil {
			return err
		}
		setStatus(fmt.Sprintf("Scanning DVD title %d of %d…", n, maxTitle))
		setProgress(float64(n-1) / float64(maxTitle))
		d, err := readDVDVideoTitleDuration(ctx, tools.ffprobe, src, n)
		if err != nil {
			continue
		}
		titles = append(titles, titleInfo{Number: n, Duration: d})
	}
	if len(titles) == 0 {
		return errors.New("No readable DVD titles were found. The disc may be encrypted, damaged, or unsupported by this FFmpeg build.")
	}
	sort.Slice(titles, func(i, j int) bool { return titles[i].Number < titles[j].Number })
	app.titlesMu.Lock()
	app.titles = titles
	app.titlesSource = src
	app.titlesMu.Unlock()
	procSendMessageW.Call(app.titleCombo, CB_RESETCONTENT, 0, 0)
	longestIdx := 0
	for i, t := range titles {
		if t.Duration > titles[longestIdx].Duration {
			longestIdx = i
		}
		label := fmt.Sprintf("Title %d  —  %s", t.Number, formatDuration(t.Duration))
		p := utf16Ptr(label)
		procSendMessageW.Call(app.titleCombo, CB_ADDSTRING, 0, uintptr(unsafe.Pointer(p)))
	}
	procSendMessageW.Call(app.titleCombo, CB_SETCURSEL, uintptr(longestIdx), 0)
	setProgress(1)
	setStatus(fmt.Sprintf("Found %d title(s). Selected title %d (%s) as the longest.", len(titles), titles[longestIdx].Number, formatDuration(titles[longestIdx].Duration)))
	return nil
}

func showMetadata(ctx context.Context) error {
	src, err := currentSource()
	if err != nil {
		return err
	}
	t, err := selectedTitle()
	if err != nil {
		return err
	}
	tools, err := ensureTools(ctx, true)
	if err != nil {
		return err
	}
	if isMKVSource(src) {
		probe, details, err := probeTabMKV(ctx, tools, src)
		if err != nil {
			return err
		}
		requestWindowsTrackWindow(src, t.Number, trackOptionsFromProbe(probe), details)
		setProgress(1)
		setStatus("MKV metadata loaded with MediaInfo. Select tracks for remux or demux.")
		return nil
	}

	setStatus(fmt.Sprintf("Reading metadata for title %d…", t.Number))
	setProgress(.15)
	probe, err := probeStreams(ctx, tools.ffprobe, src, t.Number)
	if err != nil {
		return err
	}
	options := trackOptionsFromProbe(probe)
	if len(options) == 0 {
		return errors.New("this DVD title contains no selectable video, audio, or subtitle tracks")
	}
	setProgress(.45)
	setStatus(fmt.Sprintf("Detecting chapters for title %d…", t.Number))
	chapters, chapterSource, chapterErr := detectChapters(ctx, tools.ffprobe, src, t.Number)
	if chapterErr != nil {
		log.Printf("chapter detection unavailable for title %d: %v", t.Number, chapterErr)
	}
	setProgress(.7)
	var b strings.Builder
	fmt.Fprintf(&b, "MattRip — DVD title %d\r\nDuration: %s\r\n\r\n", t.Number, formatDuration(t.Duration))
	videoN, audioN, subN := 0, 0, 0
	for _, s := range probe.Streams {
		lang := strings.TrimSpace(s.Tags["language"])
		title := strings.TrimSpace(s.Tags["title"])
		suffix := ""
		if lang != "" {
			suffix += "  [" + lang + "]"
		}
		if title != "" {
			suffix += "  " + title
		}
		switch s.CodecType {
		case "video":
			videoN++
			fmt.Fprintf(&b, "Video %d: %s", videoN, friendlyCodec(s.CodecName))
			if s.Width > 0 && s.Height > 0 {
				fmt.Fprintf(&b, "  %dx%d", s.Width, s.Height)
			}
			fmt.Fprintf(&b, "%s\r\n", suffix)
		case "audio":
			audioN++
			fmt.Fprintf(&b, "Audio %d: %s", audioN, friendlyCodec(s.CodecName))
			if s.Channels > 0 {
				fmt.Fprintf(&b, "  %d ch", s.Channels)
			}
			if s.ChannelLayout != "" {
				fmt.Fprintf(&b, " (%s)", s.ChannelLayout)
			}
			fmt.Fprintf(&b, "%s\r\n", suffix)
		case "subtitle":
			subN++
			fmt.Fprintf(&b, "Subtitle %d: %s%s\r\n", subN, friendlyCodec(s.CodecName), suffix)
		}
	}
	b.WriteString("\r\n────────────────────────────────────────\r\nChapters\r\n────────────────────────────────────────\r\n")
	if chapterErr != nil {
		fmt.Fprintf(&b, "Chapter timestamps could not be displayed: %s\r\n", firstLine(chapterErr.Error()))
		b.WriteString("FFmpeg will still attempt chapter handling during remux when Preserve chapters is enabled.\r\n")
	} else {
		fmt.Fprintf(&b, "%d chapter(s) detected via %s\r\n", len(chapters), chapterSource)
		if isChecked(app.preserveChapters) {
			b.WriteString("Preserve chapters: ON\r\n\r\n")
		} else {
			b.WriteString("Preserve chapters: OFF\r\n\r\n")
		}
		b.WriteString(" #   Start          Duration\r\n---  -------------  -------------\r\n")
		for _, ch := range chapters {
			fmt.Fprintf(&b, "%2d   %s  %s\r\n", ch.Number, formatChapterTimestamp(ch.Start), formatChapterTimestamp(ch.Duration))
		}
	}
	target := mediaInfoTarget(src)
	if target != "" && tools.mediainfo != "" {
		b.WriteString("\r\n────────────────────────────────────────\r\nMediaInfo source details\r\n────────────────────────────────────────\r\n")
		out, miErr := runHidden(ctx, tools.mediainfo, target)
		if miErr != nil {
			log.Printf("MediaInfo read failed for %q: %v", target, miErr)
			fmt.Fprintf(&b, "MediaInfo could not read source details: %s\r\n", firstLine(miErr.Error()))
		} else if len(bytes.TrimSpace(out)) == 0 {
			log.Printf("MediaInfo returned no output for %q", target)
			b.WriteString("MediaInfo returned no source details.\r\n")
		} else {
			txt := strings.ReplaceAll(string(out), "\r\n", "\n")
			txt = strings.ReplaceAll(txt, "\n", "\r\n")
			b.WriteString(txt)
		}
	}
	setProgress(1)
	setStatus(fmt.Sprintf("Metadata loaded for title %d (%d chapters detected).", t.Number, len(chapters)))
	requestWindowsTrackWindow(src, t.Number, options, b.String())
	return nil
}

func remuxSelected(ctx context.Context) error {
	src, err := currentSource()
	if err != nil {
		return err
	}
	t, err := selectedTitle()
	if err != nil {
		return err
	}
	outDir := strings.TrimSpace(getText(app.outputEdit))
	if outDir == "" {
		return errors.New("Choose an output folder first.")
	}
	if err := validateOutputDir(outDir); err != nil {
		return err
	}
	saveCurrentSettings()
	tools, err := ensureTools(ctx, false)
	if err != nil {
		return err
	}
	if isMKVSource(src) {
		indexes, selected := windowsSelectedTrackIndexes(src, t.Number)
		if !selected {
			indexes = nil
		}
		final, err := remuxMKV(ctx, tools, src, outDir, indexes, isChecked(app.preserveChapters))
		if err == nil {
			setProgress(1)
			setStatus("Complete: " + final)
		}
		return err
	}

	final := outputPath(src, outDir, t.Number)
	if _, err := os.Stat(final); err == nil {
		return fmt.Errorf("Output already exists:\n%s\n\nChoose another output folder or move/rename the existing file.", final)
	} else if !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("Could not check output path: %w", err)
	}
	partial, err := reservePartialOutput(final)
	if err != nil {
		return err
	}
	defer os.Remove(partial)
	preserveChapters := isChecked(app.preserveChapters)
	setStatus(fmt.Sprintf("Remuxing title %d with fixed timestamps to %s…", t.Number, filepath.Base(final)))
	setProgress(0)
	args := []string{"-hide_banner", "-nostdin", "-y"}
	args = appendDesktopDVDInput(args, t.Number, src)
	selected, hasSelection := windowsSelectedTrackIndexes(src, t.Number)
	mapArgs, mapErr := ffmpegStreamMapArgs(nil)
	if hasSelection {
		mapArgs, mapErr = ffmpegStreamMapArgs(selected)
	}
	if mapErr != nil {
		return mapErr
	}
	args = append(args, mapArgs...)
	args = append(args, "-c", "copy", "-map_metadata", "0")
	if preserveChapters {
		args = append(args, "-map_chapters", "0")
	} else {
		args = append(args, "-map_chapters", "-1")
	}
	args = append(args, "-progress", "pipe:1", "-nostats", partial)
	log.Printf("ffmpeg remux: title=%d source=%q output=%q preserve_chapters=%t", t.Number, src, final, preserveChapters)
	cmd := exec.CommandContext(ctx, tools.ffmpeg, args...)
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return err
	}
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("could not start FFmpeg: %w", err)
	}
	var errBuf bytes.Buffer
	doneErr := make(chan struct{})
	go func() { _, _ = io.Copy(&errBuf, stderr); close(doneErr) }()
	scanner := bufio.NewScanner(stdout)
	for scanner.Scan() {
		if err := ctx.Err(); err != nil {
			break
		}
		line := scanner.Text()
		if strings.HasPrefix(line, "out_time_us=") {
			us, _ := strconv.ParseInt(strings.TrimPrefix(line, "out_time_us="), 10, 64)
			if t.Duration > 0 && us > 0 {
				ratio := float64(time.Duration(us)*time.Microsecond) / float64(t.Duration)
				if ratio > .995 {
					ratio = .995
				}
				setProgress(ratio)
				setStatus(fmt.Sprintf("Remuxing title %d with fixed timestamps… %d%%", t.Number, int(ratio*100)))
			}
		}
	}
	waitErr := cmd.Wait()
	<-doneErr
	if ctx.Err() != nil {
		return ctx.Err()
	}
	if waitErr != nil {
		log.Printf("ffmpeg stderr: %s", tail(errBuf.String(), 8000))
		detail := tail(errBuf.String(), 5000)
		if strings.TrimSpace(detail) == "" {
			detail = waitErr.Error()
		}
		return fmt.Errorf("FFmpeg remux failed:\n\n%s", detail)
	}
	if err := finalizeRemuxOutput(partial, final); err != nil {
		return fmt.Errorf("remux finished but output finalization failed:\n%s\n\n%v", partial, err)
	}
	setProgress(1)
	setStatus("Completed: " + final)
	log.Printf("remux completed: %s", final)
	messageBox(app.hwnd, "Remux complete", "Created:\n"+final, MB_OK|MB_ICONINFORMATION)
	return nil
}

type toolPaths struct {
	ffmpeg    string
	ffprobe   string
	mediainfo string
}

func chooseWindowsDemux() {
	// Yes/No/Cancel offers both lossless MPEG-2 forms without changing other codecs.
	choice := 7
	if src, err := currentSource(); err == nil && !isMKVSource(src) {
		choice = messageBox(app.hwnd, "DVD video export format", "Save MPEG-2 video as VOB?\n\nYes: .VOB video\nNo: .mpeg2 elementary video\nCancel: return", 0x00000003|MB_ICONQUESTION)
	}
	if choice != 6 && choice != 7 {
		return
	}
	video := "mpeg2"
	if choice == 6 {
		video = "vob"
	}
	startAsync("Demuxing selected streams…", func(ctx context.Context) error {
		src, err := currentSource()
		if err != nil {
			return err
		}
		title, err := selectedTitle()
		if err != nil {
			return err
		}
		indexes, selected := windowsSelectedTrackIndexes(src, title.Number)
		if !selected {
			indexes = nil
		}
		tools, err := ensureTools(ctx, false)
		if err != nil {
			return err
		}
		final, err := demuxTab(ctx, tools, src, title.Number, strings.TrimSpace(getText(app.outputEdit)), indexes, isChecked(app.preserveChapters), video)
		if err == nil {
			setProgress(1)
			setStatus("Demux complete: " + final)
			messageBox(app.hwnd, "Demux complete", final, MB_OK|MB_ICONINFORMATION)
		}
		return err
	})
}
