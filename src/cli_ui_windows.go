//go:build windows

package main

import (
	"context"
	"unsafe"
)

const cliFirstID = 7300

var cliWindow struct {
	controls        []uintptr
	command, output uintptr
}

func createCLIWindowsControls(hwnd, hInstance uintptr) {
	dpi := windowDPI(hwnd)
	add := func(class, text string, style uint32, x, y, w, h int32, id int) uintptr {
		c, _, _ := procCreateWindowExW.Call(0, uintptr(unsafe.Pointer(utf16Ptr(class))), uintptr(unsafe.Pointer(utf16Ptr(text))), uintptr(WS_CHILD|WS_TABSTOP|style), uintptr(scale96(x, dpi)), uintptr(scale96(y, dpi)), uintptr(scale96(w, dpi)), uintptr(scale96(h, dpi)), hwnd, uintptr(id), hInstance, 0)
		procSendMessageW.Call(c, WM_SETFONT, app.bodyFont, 1)
		cliWindow.controls = append(cliWindow.controls, c)
		return c
	}
	add("STATIC", "MattRip CLI — scan, metadata, remux, --batch. Quote paths containing spaces.", 0, 28, 60, 750, 44, 0)
	cliWindow.command = add("EDIT", "mattrip-cli --help", WS_BORDER|ES_AUTOHSCROLL, 28, 112, 750, 32, 0)
	add("BUTTON", "Run", BS_PUSHBUTTON, 28, 158, 120, 32, cliFirstID)
	add("BUTTON", "Cancel", BS_PUSHBUTTON, 158, 158, 120, 32, cliFirstID+1)
	cliWindow.output = add("EDIT", "", WS_BORDER|ES_MULTILINE|ES_AUTOVSCROLL|ES_READONLY|WS_VSCROLL, 28, 204, 750, 360, 0)
}
func showWindowsCLITab(selected uintptr) {
	for _, c := range cliWindow.controls {
		show := uintptr(0)
		if selected == 3 {
			show = SW_SHOW
		}
		procShowWindow.Call(c, show)
	}
}
func handleWindowsCLICommand(id int) bool {
	if id != cliFirstID && id != cliFirstID+1 {
		return false
	}
	if id == cliFirstID+1 {
		app.cancelCurrent()
		return true
	}
	if app.busy.Load() {
		return true
	}
	line := getText(cliWindow.command)
	setText(cliWindow.output, "Running…")
	runWindowsMerger("Running CLI…", func(ctx context.Context) (func(), error) {
		text, err := runInAppCLI(ctx, line)
		if err != nil {
			text += "\r\n" + err.Error()
		}
		return func() { setText(cliWindow.output, text) }, nil
	})
	return true
}
