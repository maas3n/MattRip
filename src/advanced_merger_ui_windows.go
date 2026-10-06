//go:build windows

package main

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"unsafe"
)

const mergerFirstID = 7100
const mergerDoneMessage = WM_APP + 30

const (
	mergerPBSMarquee    = 0x00000008
	mergerPBMSetMarquee = 0x040A
)

var mergerWindow struct {
	tab, list, chapter, output, name, status, progress, demuxBtn uintptr
	dvd, controls                                      []uintptr
	cancelBtn                                          uintptr
	progressActive                                     bool
	streams                                            []mergerStream
	pendingMu                                          sync.Mutex
	pending                                            func()
}

type mergerTabItem struct {
	Mask      uint32
	State     uint32
	StateMask uint32
	Text      *uint16
	MaxText   int32
	Image     int32
	Param     uintptr
}
type mergerNotifyHeader struct {
	From uintptr
	ID   uintptr
	Code int32
}

func createMergerWindowsControls(hwnd, hInstance uintptr) {
	dpi := windowDPI(hwnd)
	add := func(class, text string, style uint32, x, y, w, h int32, id int) uintptr {
		c, _, _ := procCreateWindowExW.Call(0, uintptr(unsafe.Pointer(utf16Ptr(class))), uintptr(unsafe.Pointer(utf16Ptr(text))), uintptr(WS_CHILD|WS_TABSTOP|style), uintptr(scale96(x, dpi)), uintptr(scale96(y, dpi)), uintptr(scale96(w, dpi)), uintptr(scale96(h, dpi)), hwnd, uintptr(id), hInstance, 0)
		procSendMessageW.Call(c, WM_SETFONT, app.bodyFont, 1)
		mergerWindow.controls = append(mergerWindow.controls, c)
		return c
	}
	mergerWindow.tab = add("SysTabControl32", "", WS_VISIBLE, 22, 5, 770, 32, 7199)
	mergerWindow.controls = nil
	for i, label := range []string{"DVD Remux", "Advanced Merger", "BATCH", "CLI"} {
		item := mergerTabItem{Mask: 1, Text: utf16Ptr(label)}
		procSendMessageW.Call(mergerWindow.tab, 0x133e, uintptr(i), uintptr(unsafe.Pointer(&item)))
	}
	// Keep the explanatory source labels readable without widening the main window.
	// MEDIA and DVD DRIVE occupy the first row; the category-filtered additions use
	// the second row so the parenthetical behavior stays visible at normal DPI.
	add("BUTTON", "MEDIA(All streams included)", BS_PUSHBUTTON, 28, 52, 370, 34, mergerFirstID)
	add("BUTTON", "DVD DRIVE", BS_PUSHBUTTON, 408, 52, 370, 34, mergerFirstID+9)
	add("BUTTON", "ADD AUDIO(Only audio streams will be included)", BS_PUSHBUTTON, 28, 94, 370, 34, mergerFirstID+1)
	add("BUTTON", "ADD SUBTITLE(Only subtitle streams will be Included)", BS_PUSHBUTTON, 408, 94, 370, 34, mergerFirstID+2)
	add("STATIC", "Select Streams", 0, 28, 136, 750, 22, 0)
	mergerWindow.list = add("SysListView32", "", WS_BORDER|LVS_REPORT|LVS_SHOWSELALWAYS, 28, 162, 750, 193, 0)
	procSendMessageW.Call(mergerWindow.list, LVM_SETEXTENDEDLISTVIEWSTYLE, 0, LVS_EX_CHECKBOXES|LVS_EX_FULLROWSELECT)
	col := LVCOLUMNW{Mask: LVCF_WIDTH | LVCF_TEXT, Cx: scale96(725, dpi), PszText: utf16Ptr("File / stream")}
	procSendMessageW.Call(mergerWindow.list, LVM_INSERTCOLUMNW, 0, uintptr(unsafe.Pointer(&col)))
	add("BUTTON", "Clear streams", BS_PUSHBUTTON, 28, 365, 130, 28, mergerFirstID+3)
	mergerWindow.chapter = add("EDIT", "", WS_BORDER|ES_AUTOHSCROLL, 28, 405, 430, 28, 0)
	add("BUTTON", "ADD CHAPTER .txt FILE(FFMETADATA1 Format)", BS_PUSHBUTTON, 468, 403, 310, 32, mergerFirstID+4)
	mergerWindow.output = add("EDIT", loadSettings().OutputDir, WS_BORDER|ES_AUTOHSCROLL, 28, 445, 500, 28, 0)
	if getText(mergerWindow.output) == "" {
		setText(mergerWindow.output, defaultOutputDir())
	}
	add("BUTTON", "CHOOSE OUTPUT FOLDER", BS_PUSHBUTTON, 538, 443, 240, 32, mergerFirstID+5)
	add("STATIC", "Output filename", 0, 28, 487, 120, 24, 0)
	mergerWindow.name = add("EDIT", "merged.mkv", WS_BORDER|ES_AUTOHSCROLL, 155, 484, 623, 28, 0)
	mergerWindow.status = add("STATIC", "Choose files and select streams. Chapters are optional (MKV or FFMETADATA1).", 0, 28, 522, 750, 24, 0)
	// Use the same native Windows progress-control family as DVD Remux. Advanced
	// Merger cannot know an exact percentage for arbitrary stream-copy inputs, so
	// show the native marquee while an operation is active instead of a fake value.
	mergerWindow.progress, _, _ = procCreateWindowExW.Call(0, uintptr(unsafe.Pointer(utf16Ptr("msctls_progress32"))), 0, uintptr(WS_CHILD|mergerPBSMarquee), uintptr(scale96(28, dpi)), uintptr(scale96(550, dpi)), uintptr(scale96(750, dpi)), uintptr(scale96(16, dpi)), hwnd, 0, hInstance, 0)
	mergerWindow.demuxBtn = add("BUTTON", "DEMUX", BS_PUSHBUTTON, 296, 574, 180, 34, mergerFirstID+7)
	add("BUTTON", "MUX TO MKV", BS_PUSHBUTTON, 488, 574, 180, 34, mergerFirstID+6)
	mergerWindow.cancelBtn = add("BUTTON", "Cancel", BS_PUSHBUTTON, 680, 574, 98, 34, mergerFirstID+8)
	procEnableWindow.Call(mergerWindow.cancelBtn, 0)
	createBatchWindowsControls(hwnd, hInstance)
	createCLIWindowsControls(hwnd, hInstance)
}

func setWindowsMergerProgress(active bool) {
	mergerWindow.progressActive = active
	if mergerWindow.progress == 0 {
		return
	}
	if active {
		procSendMessageW.Call(mergerWindow.progress, mergerPBMSetMarquee, 1, 30)
		selected, _, _ := procSendMessageW.Call(mergerWindow.tab, 0x130b, 0, 0)
		if selected == 1 {
			procShowWindow.Call(mergerWindow.progress, SW_SHOW)
		}
		return
	}
	procSendMessageW.Call(mergerWindow.progress, mergerPBMSetMarquee, 0, 0)
	procShowWindow.Call(mergerWindow.progress, 0)
}

func showMergerWindowsTab() {
	selected, _, _ := procSendMessageW.Call(mergerWindow.tab, 0x130b, 0, 0)
	for _, c := range mergerWindow.dvd {
		show := uintptr(0)
		if selected == 0 {
			show = SW_SHOW
		}
		procShowWindow.Call(c, show)
	}
	for _, c := range mergerWindow.controls {
		show := uintptr(0)
		if selected == 1 {
			show = SW_SHOW
		}
		procShowWindow.Call(c, show)
	}
	progressShow := uintptr(0)
	if selected == 1 && mergerWindow.progressActive {
		progressShow = SW_SHOW
	}
	procShowWindow.Call(mergerWindow.progress, progressShow)
	showWindowsBatchTab(selected)
	showWindowsCLITab(selected)
	redrawWindowsTabPage()
}

func redrawWindowsTabPage() {
	// The three tab pages are sibling child controls of the same top-level window.
	// Hiding a page can otherwise leave stale pixels from the previous page until
	// Windows happens to repaint the parent. Force a complete erase/redraw after
	// every tab change so hidden controls cannot visually bleed into the new page.
	redrawWindow := user32.NewProc("RedrawWindow")
	const (
		rdwInvalidate  = 0x0001
		rdwErase       = 0x0004
		rdwAllChildren = 0x0080
		rdwUpdateNow   = 0x0100
	)
	redrawWindow.Call(app.hwnd, 0, 0, rdwInvalidate|rdwErase|rdwAllChildren|rdwUpdateNow)
}

func browseMergerFiles(owner uintptr, multiple bool) []string {
	buf := make([]uint16, 65536)
	filter := utf16FromStringWithNulls("All files (*.*)\x00*.*\x00\x00")
	flags := uint32(OFN_FILEMUSTEXIST | OFN_PATHMUSTEXIST | OFN_EXPLORER | OFN_NOCHANGEDIR)
	if multiple {
		flags |= 0x200
	}
	ofn := OPENFILENAME{LStructSize: uint32(unsafe.Sizeof(OPENFILENAME{})), HwndOwner: owner, LpstrFilter: &filter[0], NFilterIndex: 1, LpstrFile: &buf[0], NMaxFile: uint32(len(buf)), LpstrTitle: utf16Ptr("Choose media files"), Flags: flags}
	r, _, _ := procGetOpenFileNameW.Call(uintptr(unsafe.Pointer(&ofn)))
	if r == 0 {
		return nil
	}
	var parts []string
	start := 0
	for i, c := range buf {
		if c == 0 {
			if i == start {
				break
			}
			parts = append(parts, syscall.UTF16ToString(buf[start:i]))
			start = i + 1
		}
	}
	if len(parts) <= 1 {
		return parts
	}
	var paths []string
	for _, p := range parts[1:] {
		paths = append(paths, filepath.Join(parts[0], p))
	}
	return paths
}

func browseWindowsMergerMediaSources(owner uintptr) []string {
	choice := messageBox(owner, "Add media", "Choose what to add.\n\nYes: media file(s) or DVD ISO\nNo: DVD / VIDEO_TS folder\nCancel: return", 0x00000003|MB_ICONQUESTION)
	switch choice {
	case 6:
		return browseMergerFiles(owner, true)
	case 7:
		if path := browseFolder(owner, "Choose DVD / VIDEO_TS folder"); path != "" {
			return []string{path}
		}
	}
	return nil
}

func runWindowsMerger(label string, work func(context.Context) (func(), error)) {
	if !app.busy.CompareAndSwap(false, true) {
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	app.cancelMu.Lock()
	app.cancel = cancel
	app.cancelMu.Unlock()
	for _, c := range mergerWindow.controls {
		procEnableWindow.Call(c, 0)
	}
	procEnableWindow.Call(mergerWindow.cancelBtn, 1)
	procEnableWindow.Call(mergerWindow.tab, 0)
	setText(mergerWindow.status, label)
	setWindowsMergerProgress(true)
	go func() {
		done, err := work(ctx)
		cancel()
		mergerWindow.pendingMu.Lock()
		mergerWindow.pending = func() {
			app.cancelMu.Lock()
			app.cancel = nil
			app.cancelMu.Unlock()
			app.busy.Store(false)
			setWindowsMergerProgress(false)
			for _, c := range mergerWindow.controls {
				procEnableWindow.Call(c, 1)
			}
			procEnableWindow.Call(mergerWindow.cancelBtn, 0)
			procEnableWindow.Call(mergerWindow.tab, 1)
			if err != nil {
				setText(mergerWindow.status, err.Error())
				messageBox(app.hwnd, "Advanced Merger", err.Error(), MB_OK|MB_ICONERROR)
			} else if done != nil {
				done()
			}
		}
		mergerWindow.pendingMu.Unlock()
		procPostMessageW.Call(app.hwnd, mergerDoneMessage, 0, 0)
	}()
}

func handleWindowsMergerCommand(id int) bool {
	if handleWindowsCLICommand(id) || handleWindowsBatchCommand(id) {
		return true
	}
	if id < mergerFirstID || id > mergerFirstID+9 {
		return false
	}
	if id == mergerFirstID+8 {
		app.cancelCurrent()
		return true
	}
	if app.busy.Load() {
		return true
	}
	switch id - mergerFirstID {
	case 0, 1, 2:
		kind := []string{"all", "audio", "subtitle"}[id-mergerFirstID]
		var paths []string
		if kind == "all" {
			paths = browseWindowsMergerMediaSources(app.hwnd)
		} else {
			paths = browseMergerFiles(app.hwnd, true)
		}
		if len(paths) == 0 {
			return true
		}
		runWindowsMerger("Reading streams…", func(ctx context.Context) (func(), error) {
			tools, err := mergerTools(ctx)
			if err != nil {
				return nil, err
			}
			var added []mergerStream
			for _, path := range paths {
				streams, err := probeMergerFile(ctx, tools.ffprobe, path, kind)
				if err != nil {
					return nil, err
				}
				added = append(added, streams...)
			}
			return func() {
				for _, s := range added {
					duplicate := false
					for _, old := range mergerWindow.streams {
						if strings.EqualFold(old.Path, s.Path) && old.Track.Index == s.Track.Index {
							duplicate = true
						}
					}
					if duplicate {
						continue
					}
					idx := len(mergerWindow.streams)
					mergerWindow.streams = append(mergerWindow.streams, s)
					item := LVITEMW{Mask: LVIF_TEXT | LVIF_STATE, IItem: int32(idx), PszText: utf16Ptr(dvdSourceDisplayName(s.Path) + " — " + s.Track.Label()), State: 2 << 12, StateMask: LVIS_STATEIMAGEMASK}
					procSendMessageW.Call(mergerWindow.list, LVM_INSERTITEMW, 0, uintptr(unsafe.Pointer(&item)))
				}
				setText(mergerWindow.status, fmt.Sprintf("%d streams available", len(mergerWindow.streams)))
			}, nil
		})
	case 9:
		path := chooseWindowsPhysicalDVDDrive(app.hwnd)
		if path == "" {
			return true
		}
		runWindowsMerger("Reading DVD streams…", func(ctx context.Context) (func(), error) {
			tools, err := mergerTools(ctx)
			if err != nil {
				return nil, err
			}
			added, err := probeMergerFile(ctx, tools.ffprobe, path, "all")
			if err != nil {
				return nil, err
			}
			return func() {
				for _, s := range added {
					duplicate := false
					for _, old := range mergerWindow.streams {
						if strings.EqualFold(old.Path, s.Path) && old.Track.Index == s.Track.Index {
							duplicate = true
						}
					}
					if duplicate {
						continue
					}
					idx := len(mergerWindow.streams)
					mergerWindow.streams = append(mergerWindow.streams, s)
					item := LVITEMW{Mask: LVIF_TEXT | LVIF_STATE, IItem: int32(idx), PszText: utf16Ptr(dvdSourceDisplayName(s.Path) + " — " + s.Track.Label()), State: 2 << 12, StateMask: LVIS_STATEIMAGEMASK}
					procSendMessageW.Call(mergerWindow.list, LVM_INSERTITEMW, 0, uintptr(unsafe.Pointer(&item)))
				}
				setText(mergerWindow.status, fmt.Sprintf("%d streams available", len(mergerWindow.streams)))
			}, nil
		})
	case 3:
		mergerWindow.streams = nil
		procSendMessageW.Call(mergerWindow.list, LVM_FIRST+9, 0, 0)
	case 4:
		paths := browseMergerFiles(app.hwnd, false)
		if len(paths) == 0 {
			return true
		}
		path := paths[0]
		runWindowsMerger("Checking chapters…", func(ctx context.Context) (func(), error) {
			tools, err := mergerTools(ctx)
			if err != nil {
				return nil, err
			}
			if err = validateMergerChapters(ctx, tools.ffprobe, path); err != nil {
				return nil, err
			}
			return func() { setText(mergerWindow.chapter, path); setText(mergerWindow.status, "Chapter file ready") }, nil
		})
	case 5:
		if path := browseFolder(app.hwnd, "Choose output folder"); path != "" {
			setText(mergerWindow.output, path)
		}
	case 6:
		var selected []mergerStream
		for i, s := range mergerWindow.streams {
			state, _, _ := procSendMessageW.Call(mergerWindow.list, LVM_GETITEMSTATE, uintptr(i), LVIS_STATEIMAGEMASK)
			if state>>12 == 2 {
				selected = append(selected, s)
			}
		}
		filename := strings.TrimSpace(getText(mergerWindow.name))
		if filepath.Base(filename) != filename || !strings.HasSuffix(strings.ToLower(filename), ".mkv") {
			messageBox(app.hwnd, "Output filename", "Enter a filename ending in .mkv without folders.", MB_OK|MB_ICONWARNING)
			return true
		}
		dir, chap := getText(mergerWindow.output), getText(mergerWindow.chapter)
		runWindowsMerger("Muxing selected streams…", func(ctx context.Context) (func(), error) {
			if err := validateOutputDir(dir); err != nil {
				return nil, err
			}
			tools, err := mergerTools(ctx)
			if err != nil {
				return nil, err
			}
			out := filepath.Join(dir, filename)
			if err = muxMerger(ctx, tools, selected, chap, out); err != nil {
				return nil, err
			}
			return func() { setText(mergerWindow.status, "Completed: "+out) }, nil
		})
	case 7:
		var selected []mergerStream
		for i, s := range mergerWindow.streams {
			state, _, _ := procSendMessageW.Call(mergerWindow.list, LVM_GETITEMSTATE, uintptr(i), LVIS_STATEIMAGEMASK)
			if state>>12 == 2 {
				selected = append(selected, s)
			}
		}
		choice := messageBox(app.hwnd, "MPEG-2 video export format", "Save MPEG-2 video as VOB?\n\nYes: .VOB video\nNo: .mpeg2 elementary video\nCancel: return", 0x00000003|MB_ICONQUESTION)
		if choice != 6 && choice != 7 {
			return true
		}
		video := "mpeg2"
		if choice == 6 {
			video = "vob"
		}
		dir := getText(mergerWindow.output)
		runWindowsMerger("Demuxing selected streams…", func(ctx context.Context) (func(), error) {
			if err := validateOutputDir(dir); err != nil {
				return nil, err
			}
			tools, err := mergerTools(ctx)
			if err != nil {
				return nil, err
			}
			final, err := demuxMerger(ctx, tools, selected, dir, video)
			if err != nil {
				return nil, err
			}
			return func() { setText(mergerWindow.status, "Demux complete: "+final) }, nil
		})
	}
	return true
}
