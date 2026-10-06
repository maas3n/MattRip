//go:build windows

package main

import (
	"runtime"
	"syscall"
	"testing"
	"unsafe"
)

func TestWindowsAdvancedMergerAndBatchTabs(t *testing.T) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	icc := INITCOMMONCONTROLSEX{DwSize: uint32(unsafe.Sizeof(INITCOMMONCONTROLSEX{})), DwICC: ICC_PROGRESS_CLASS | ICC_LISTVIEW_CLASSES | 0x8}
	procInitCommonControlsEx.Call(uintptr(unsafe.Pointer(&icc)))
	if err := createMainWindow(); err != nil {
		t.Fatal(err)
	}
	defer procDestroyWindow.Call(app.hwnd)
	if mergerWindow.tab == 0 || mergerWindow.list == 0 || mergerWindow.progress == 0 || mergerWindow.demuxBtn == 0 {
		t.Fatal("missing merger controls")
	}
	if got, want := getText(app.scanBtn), "SCAN/SELECT STREAMS"; got != want {
		t.Fatalf("scan/select button = %q; want %q", got, want)
	}
	if got, want := getText(app.remuxBtn), "REMUX"; got != want {
		t.Fatalf("remux button = %q; want %q", got, want)
	}
	if got, want := getText(app.demuxBtn), "DEMUX"; got != want {
		t.Fatalf("demux button = %q; want %q", got, want)
	}
	if got, want := getText(batchWindow.oneClick), "BATCH REMUX"; got != want {
		t.Fatalf("batch button = %q; want %q", got, want)
	}
	if len(mergerWindow.controls) < 9 {
		t.Fatal("missing Advanced Merger controls")
	}
	wantSourceLabels := []string{
		"MEDIA(All streams included)",
		"DVD DRIVE",
		"ADD AUDIO(Only audio streams will be included)",
		"ADD SUBTITLE(Only subtitle streams will be Included)",
	}
	for i, want := range wantSourceLabels {
		if got := getText(mergerWindow.controls[i]); got != want {
			t.Fatalf("source button %d = %q; want %q", i, got, want)
		}
	}
	if got, want := getText(mergerWindow.controls[8]), "ADD CHAPTER .txt FILE(FFMETADATA1 Format)"; got != want {
		t.Fatalf("chapter button = %q; want %q", got, want)
	}
	foundMux := false
	for _, control := range mergerWindow.controls {
		if getText(control) == "MUX" {
			foundMux = true
		}
	}
	if !foundMux {
		t.Fatal("Advanced Merger is missing renamed MUX button")
	}
	visible := syscall.NewLazyDLL("user32.dll").NewProc("IsWindowVisible")

	if len(mergerWindow.dvd) == 0 || len(mergerWindow.controls) == 0 || len(batchWindow.controls) == 0 {
		t.Fatal("missing tab page controls")
	}
	pages := [][]uintptr{mergerWindow.dvd, mergerWindow.controls, batchWindow.controls, cliWindow.controls}
	setWindowsMergerProgress(true)
	// Exercise the notification handler used by real clicks, including repeated
	// returns to DVD Remux. TCM_SETCURSEL alone does not send TCN_SELCHANGE.
	for _, selected := range []uintptr{1, 2, 3, 0, 2, 1, 3, 0} {
		procSendMessageW.Call(mergerWindow.tab, 0x130c, selected, 0)
		header := mergerNotifyHeader{From: mergerWindow.tab, Code: -551}
		procSendMessageW.Call(app.hwnd, 0x004e, 0, uintptr(unsafe.Pointer(&header)))
		for page, controls := range pages {
			for _, c := range controls {
				v, _, _ := visible.Call(c)
				if c == 0 || (v != 0) != (uintptr(page) == selected) {
					t.Fatalf("tab %d: page %d control %#x has incorrect visibility", selected, page, c)
				}
			}
		}
		progressVisible, _, _ := visible.Call(mergerWindow.progress)
		if (progressVisible != 0) != (selected == 1) {
			t.Fatalf("tab %d: Advanced Merger activity bar visibility is incorrect", selected)
		}
		assertWindowsTabBackgroundErased(t)
	}

	setWindowsMergerProgress(false)
	procSendMessageW.Call(mergerWindow.tab, 0x130c, 1, 0)
	showMergerWindowsTab()
	progressVisible, _, _ := visible.Call(mergerWindow.progress)
	if progressVisible != 0 {
		t.Fatal("Advanced Merger activity bar remains visible while idle")
	}
}

func assertWindowsTabBackgroundErased(t *testing.T) {
	t.Helper()
	// Paint into an offscreen DC so this tests actual background erasure without
	// depending on screen capture, desktop occlusion, or the CI monitor's DPI.
	gdi := syscall.NewLazyDLL("gdi32.dll")
	dc, _, _ := gdi.NewProc("CreateCompatibleDC").Call(0)
	if dc == 0 {
		t.Fatal("CreateCompatibleDC failed")
	}
	defer gdi.NewProc("DeleteDC").Call(dc)
	bitmap, _, _ := gdi.NewProc("CreateBitmap").Call(4, 4, 1, 32, 0)
	if bitmap == 0 {
		t.Fatal("CreateBitmap failed")
	}
	defer gdi.NewProc("DeleteObject").Call(bitmap)
	old, _, _ := gdi.NewProc("SelectObject").Call(dc, bitmap)
	defer gdi.NewProc("SelectObject").Call(dc, old)
	expected, _, _ := user32.NewProc("GetSysColor").Call(15) // COLOR_BTNFACE
	marker := expected ^ 0x00ffffff
	gdi.NewProc("SetPixel").Call(dc, 1, 1, marker)
	before, _, _ := gdi.NewProc("GetPixel").Call(dc, 1, 1)
	if before != marker {
		t.Fatalf("could not seed stale pixel: got %#x, want %#x", before, marker)
	}
	procSendMessageW.Call(app.hwnd, 0x0014, dc, 0) // WM_ERASEBKGND
	after, _, _ := gdi.NewProc("GetPixel").Call(dc, 1, 1)
	if after != expected {
		t.Fatalf("old tab pixel survived background erase: got %#x, want %#x", after, expected)
	}
}
