//go:build windows

package main

import (
	"context"
	"fmt"
	"strings"
	"unsafe"
)

const batchFirstID = 7200

var batchWindow struct {
	controls                 []uintptr
	input, output, status    uintptr
	oneClick, cancel         uintptr
}

func createBatchWindowsControls(hwnd, hInstance uintptr) {
	dpi := windowDPI(hwnd)
	add := func(class, text string, style uint32, x, y, w, h int32, id int) uintptr {
		c, _, _ := procCreateWindowExW.Call(0, uintptr(unsafe.Pointer(utf16Ptr(class))), uintptr(unsafe.Pointer(utf16Ptr(text))), uintptr(WS_CHILD|WS_TABSTOP|style), uintptr(scale96(x, dpi)), uintptr(scale96(y, dpi)), uintptr(scale96(w, dpi)), uintptr(scale96(h, dpi)), hwnd, uintptr(id), hInstance, 0)
		procSendMessageW.Call(c, WM_SETFONT, app.bodyFont, 1)
		batchWindow.controls = append(batchWindow.controls, c)
		return c
	}
	add("STATIC", "BATCH", 0, 28, 58, 750, 28, 0)
	add("STATIC", "One-click batch uses FFmpeg dvdvideo (libdvdread/libdvdnav), selects the longest title, includes all streams, preserves chapters, and always remuxes with -analyzeduration 100M -probesize 100M -fflags +genpts.", 0, 28, 92, 750, 58, 0)
	add("STATIC", "Movie collection folder", 0, 28, 170, 180, 24, 0)
	batchWindow.input = add("EDIT", "", WS_BORDER|ES_AUTOHSCROLL, 28, 198, 500, 30, 0)
	add("BUTTON", "CHOOSE MOVIE FOLDER", BS_PUSHBUTTON, 538, 196, 240, 34, batchFirstID)
	add("STATIC", "Accepts movie folders with VIDEO_TS and unmounted .iso files.", 0, 28, 235, 750, 24, 0)
	add("STATIC", "Output folder (optional)", 0, 28, 280, 180, 24, 0)
	batchWindow.output = add("EDIT", "", WS_BORDER|ES_AUTOHSCROLL, 28, 308, 500, 30, 0)
	add("BUTTON", "CHOOSE OUTPUT FOLDER", BS_PUSHBUTTON, 538, 306, 240, 34, batchFirstID+1)
	add("STATIC", "Leave blank to place each completed MKV beside its ISO or VIDEO_TS folder.", 0, 28, 345, 750, 24, 0)
	batchWindow.status = add("STATIC", "Choose the movie collection folder, then click BATCH REMUX.", 0, 28, 405, 750, 62, 0)
	batchWindow.oneClick = add("BUTTON", "BATCH REMUX", BS_PUSHBUTTON, 392, 530, 276, 42, batchFirstID+2)
	batchWindow.cancel = add("BUTTON", "Cancel", BS_PUSHBUTTON, 680, 530, 98, 42, batchFirstID+3)
	procEnableWindow.Call(batchWindow.cancel, 0)
	for _, c := range batchWindow.controls {
		procShowWindow.Call(c, 0)
	}
}

func showWindowsBatchTab(selected uintptr) {
	for _, c := range batchWindow.controls {
		show := uintptr(0)
		if selected == 2 {
			show = SW_SHOW
		}
		procShowWindow.Call(c, show)
	}
}

func setWindowsBatchBusy(busy bool) {
	enabled := uintptr(1)
	if busy {
		enabled = 0
	}
	for _, c := range batchWindow.controls {
		procEnableWindow.Call(c, enabled)
	}
	procEnableWindow.Call(batchWindow.cancel, 1-enabled)
	procEnableWindow.Call(mergerWindow.tab, enabled)
}

func runWindowsBatch() {
	root := strings.TrimSpace(getText(batchWindow.input))
	if root == "" {
		messageBox(app.hwnd, "BATCH", "Choose the folder containing the movie title folders first.", MB_OK|MB_ICONWARNING)
		return
	}
	outRoot := strings.TrimSpace(getText(batchWindow.output))
	if !app.busy.CompareAndSwap(false, true) {
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	app.cancelMu.Lock()
	app.cancel = cancel
	app.cancelMu.Unlock()
	setWindowsBatchBusy(true)
	setText(batchWindow.status, "Starting one-click batch mux…")
	go func() {
		result, err := runBatch(ctx, batchOptions{InputRoot: root, OutputRoot: outRoot}, func(frac float64, text string) {
			if text != "" {
				setText(batchWindow.status, fmt.Sprintf("%d%% — %s", int(frac*100), text))
			}
		})
		cancel()
		mergerWindow.pendingMu.Lock()
		mergerWindow.pending = func() {
			app.cancelMu.Lock()
			app.cancel = nil
			app.cancelMu.Unlock()
			app.busy.Store(false)
			setWindowsBatchBusy(false)
			if err != nil {
				if ctx.Err() != nil {
					setText(batchWindow.status, "Batch cancelled.")
					return
				}
				setText(batchWindow.status, fmt.Sprintf("Batch finished with errors: %d completed, %d failed.", result.Completed, len(result.Failures)))
				messageBox(app.hwnd, "BATCH", err.Error(), MB_OK|MB_ICONERROR)
				return
			}
			setText(batchWindow.status, fmt.Sprintf("Batch complete: %d movie(s) remuxed.", result.Completed))
			messageBox(app.hwnd, "BATCH complete", fmt.Sprintf("Remuxed %d movie(s).", result.Completed), MB_OK)
		}
		mergerWindow.pendingMu.Unlock()
		procPostMessageW.Call(app.hwnd, mergerDoneMessage, 0, 0)
	}()
}

func handleWindowsBatchCommand(id int) bool {
	if id < batchFirstID || id > batchFirstID+3 {
		return false
	}
	if id == batchFirstID+3 {
		app.cancelCurrent()
		return true
	}
	if app.busy.Load() {
		return true
	}
	switch id - batchFirstID {
	case 0:
		if path := browseFolder(app.hwnd, "Choose movie collection folder"); path != "" {
			setText(batchWindow.input, path)
		}
	case 1:
		if path := browseFolder(app.hwnd, "Choose output folder"); path != "" {
			setText(batchWindow.output, path)
		}
	case 2:
		runWindowsBatch()
	}
	return true
}
