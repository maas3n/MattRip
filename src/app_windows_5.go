//go:build windows

package main

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"time"
	"unsafe"
)

func friendlyCodec(s string) string {
	if s == "" {
		return "Unknown"
	}
	m := map[string]string{"mpeg2video": "MPEG-2 Video", "ac3": "Dolby Digital (AC-3)", "eac3": "Dolby Digital Plus (E-AC-3)", "dts": "DTS", "pcm_dvd": "PCM", "mp2": "MPEG Audio Layer II", "dvd_subtitle": "DVD Subtitle"}
	if v, ok := m[s]; ok {
		return v
	}
	return strings.ToUpper(s)
}
func formatDuration(d time.Duration) string {
	if d < 0 {
		d = 0
	}
	total := int64(d.Round(time.Second) / time.Second)
	h := total / 3600
	m := (total % 3600) / 60
	s := total % 60
	if h > 0 {
		return fmt.Sprintf("%d:%02d:%02d", h, m, s)
	}
	return fmt.Sprintf("%d:%02d", m, s)
}
func formatChapterTimestamp(d time.Duration) string {
	if d < 0 {
		d = 0
	}
	ms := d.Round(time.Millisecond).Milliseconds()
	h := ms / 3_600_000
	m := (ms % 3_600_000) / 60_000
	sec := (ms % 60_000) / 1000
	milli := ms % 1000
	return fmt.Sprintf("%02d:%02d:%02d.%03d", h, m, sec, milli)
}

func browseFolder(owner uintptr, title string) string {
	display := make([]uint16, 260)
	bi := BROWSEINFO{HwndOwner: owner, PszDisplayName: &display[0], LpszTitle: utf16Ptr(title), UlFlags: BIF_RETURNONLYFSDIRS | BIF_NEWDIALOGSTYLE}
	pidl, _, _ := procSHBrowseForFolderW.Call(uintptr(unsafe.Pointer(&bi)))
	if pidl == 0 {
		return ""
	}
	defer procCoTaskMemFree.Call(pidl)
	path := make([]uint16, 32768)
	r, _, _ := procSHGetPathFromIDListW.Call(pidl, uintptr(unsafe.Pointer(&path[0])))
	if r == 0 {
		return ""
	}
	return syscall.UTF16ToString(path)
}
func browseISO(owner uintptr) string {
	fileBuf := make([]uint16, 32768)
	filter := utf16FromStringWithNulls("DVD ISO / MKV (*.iso;*.mkv)\x00*.iso;*.mkv\x00All files (*.*)\x00*.*\x00\x00")
	ofn := OPENFILENAME{LStructSize: uint32(unsafe.Sizeof(OPENFILENAME{})), HwndOwner: owner, LpstrFilter: &filter[0], NFilterIndex: 1, LpstrFile: &fileBuf[0], NMaxFile: uint32(len(fileBuf)), LpstrTitle: utf16Ptr("Choose a DVD ISO or MKV"), Flags: OFN_FILEMUSTEXIST | OFN_PATHMUSTEXIST | OFN_EXPLORER | OFN_NOCHANGEDIR, LpstrDefExt: utf16Ptr("iso")}
	r, _, _ := procGetOpenFileNameW.Call(uintptr(unsafe.Pointer(&ofn)))
	if r == 0 {
		return ""
	}
	return syscall.UTF16ToString(fileBuf)
}
func showAbout() {
	msg := fmt.Sprintf("MattRip %s\n\nLossless DVD-title remuxing to MKV.\n\nHighlights in this build:\n• native DVD IFO chapter detection with FFmpeg fallback\n• optional Preserve chapters setting, enabled by default\n• accurate FFmpeg dvdvideo chapter pre-indexing when preservation is enabled\n• pinned and verified FFmpeg / MediaInfo downloads\n• partial-file output before atomic rename\n• protected-folder checks and cancelable operations\n• portable mode keeps tools, settings, and logs in MattRipData beside the executable\n• installed mode keeps local data under %%LOCALAPPDATA%%\\MattRip\n\nFFmpeg and MediaInfo are third-party projects with their own licenses.", appVersion)
	messageBox(app.hwnd, "About MattRip", msg, MB_OK|MB_ICONINFORMATION)
}
func requestTextWindow(title, text string) {
	app.pendingTextMu.Lock()
	app.pendingTitle, app.pendingText = title, text
	app.pendingTextMu.Unlock()
	procPostMessageW.Call(app.hwnd, WM_SHOWTEXT, 0, 0)
}
func showTextWindow(title, text string) {
	hInstance, _, _ := procGetModuleHandleW.Call(0)
	className := utf16Ptr("MattRipTextWindowClass")
	cursor, _, _ := procLoadCursorW.Call(0, 32512)
	bg, _, _ := procGetStockObject.Call(5)
	wc := WNDCLASSEX{CbSize: uint32(unsafe.Sizeof(WNDCLASSEX{})), LpfnWndProc: syscall.NewCallback(textWindowProc), HInstance: hInstance, HCursor: cursor, HbrBackground: bg, LpszClassName: className}
	procRegisterClassExW.Call(uintptr(unsafe.Pointer(&wc)))
	dpi := windowDPI(app.hwnd)
	w, h := scale96(760, dpi), scale96(610, dpi)
	margin := scale96(12, dpi)
	hwnd, _, _ := procCreateWindowExW.Call(0, uintptr(unsafe.Pointer(className)), uintptr(unsafe.Pointer(utf16Ptr(title))), WS_OVERLAPPED|WS_CAPTION|WS_SYSMENU|WS_THICKFRAME|WS_MINIMIZEBOX|WS_MAXIMIZEBOX|WS_VISIBLE, CW_USEDEFAULT, CW_USEDEFAULT, uintptr(w), uintptr(h), app.hwnd, 0, hInstance, 0)
	if hwnd == 0 {
		messageBox(app.hwnd, title, text, MB_OK|MB_ICONINFORMATION)
		return
	}
	edit, _, _ := procCreateWindowExW.Call(WS_EX_CLIENTEDGE, uintptr(unsafe.Pointer(utf16Ptr("EDIT"))), uintptr(unsafe.Pointer(utf16Ptr(text))), WS_CHILD|WS_VISIBLE|WS_VSCROLL|ES_MULTILINE|ES_AUTOVSCROLL|ES_READONLY, uintptr(margin), uintptr(margin), uintptr(max32(scale96(100, dpi), w-2*margin)), uintptr(max32(scale96(100, dpi), h-2*margin-scale96(40, dpi))), hwnd, 1, hInstance, 0)
	if app.monoFont != 0 {
		procSendMessageW.Call(edit, WM_SETFONT, app.monoFont, 1)
	}
	procShowWindow.Call(hwnd, SW_SHOW)
}
func textWindowProc(hwnd uintptr, msg uint32, wParam, lParam uintptr) uintptr {
	switch msg {
	case 0x0005:
		var r struct{ Left, Top, Right, Bottom int32 }
		procGetClientRect.Call(hwnd, uintptr(unsafe.Pointer(&r)))
		getDlgItem := user32.NewProc("GetDlgItem")
		child, _, _ := getDlgItem.Call(hwnd, 1)
		if child != 0 {
			dpi := windowDPI(hwnd)
			m := scale96(12, dpi)
			procSetWindowPos.Call(child, 0, uintptr(m), uintptr(m), uintptr(max32(scale96(100, dpi), r.Right-2*m)), uintptr(max32(scale96(100, dpi), r.Bottom-2*m)), 0x0004)
		}
		return 0
	case WM_CLOSE:
		procDestroyWindow.Call(hwnd)
		return 0
	}
	r, _, _ := procDefWindowProcW.Call(hwnd, uintptr(msg), wParam, lParam)
	return r
}
func max32(a, b int32) int32 {
	if a > b {
		return a
	}
	return b
}
func defaultOutputDir() string {
	if home, err := os.UserHomeDir(); err == nil && home != "" {
		videos := filepath.Join(home, "Videos")
		if st, err := os.Stat(videos); err == nil && st.IsDir() {
			return videos
		}
		return home
	}
	return "."
}
func appDataRoot() string  { return resolveAppDataRoot() }
func settingsPath() string { return filepath.Join(appDataRoot(), "settings.json") }
func loadSettings() appSettings {
	s := appSettings{PreserveChapters: true}
	b, err := os.ReadFile(settingsPath())
	if err == nil {
		_ = json.Unmarshal(b, &s)
	}
	return s
}
func saveCurrentSettings() {
	saveSettings(appSettings{OutputDir: strings.TrimSpace(getText(app.outputEdit)), PreserveChapters: isChecked(app.preserveChapters)})
}
func saveSettings(s appSettings) {
	b, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return
	}
	tmp := settingsPath() + ".tmp"
	if os.WriteFile(tmp, b, 0600) == nil {
		_ = os.Rename(tmp, settingsPath())
	}
}
func initLogging() {
	root := appDataRoot()
	dir := filepath.Join(root, "logs")
	_ = os.MkdirAll(dir, 0755)
	p := filepath.Join(dir, "mattrip.log")
	f, err := os.OpenFile(p, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err == nil {
		app.logFile = f
		log.SetOutput(f)
		log.SetFlags(log.Ldate | log.Ltime | log.Lmicroseconds)
	}
}
func closeLogging() {
	if app.logFile != nil {
		_ = app.logFile.Close()
	}
}
func setChecked(hwnd uintptr, checked bool) {
	state := uintptr(BST_UNCHECKED)
	if checked {
		state = BST_CHECKED
	}
	procSendMessageW.Call(hwnd, BM_SETCHECK, state, 0)
}
func isChecked(hwnd uintptr) bool {
	r, _, _ := procSendMessageW.Call(hwnd, BM_GETCHECK, 0, 0)
	return r == BST_CHECKED
}
func setText(hwnd uintptr, text string) {
	p := utf16Ptr(text)
	procSetWindowTextW.Call(hwnd, uintptr(unsafe.Pointer(p)))
}
func getText(hwnd uintptr) string {
	n, _, _ := procGetWindowTextLengthW.Call(hwnd)
	if n == 0 {
		return ""
	}
	buf := make([]uint16, int(n)+1)
	procGetWindowTextW.Call(hwnd, uintptr(unsafe.Pointer(&buf[0])), uintptr(len(buf)))
	return syscall.UTF16ToString(buf)
}
func setStatus(s string) {
	if app.statusText != 0 {
		setText(app.statusText, s)
	}
}
func setProgress(frac float64) {
	if frac < 0 {
		frac = 0
	}
	if frac > 1 {
		frac = 1
	}
	if app.progress != 0 {
		procSendMessageW.Call(app.progress, PBM_SETPOS, uintptr(int(frac*1000)), 0)
	}
}
func messageBox(owner uintptr, title, text string, flags uint32) int {
	r, _, _ := procMessageBoxW.Call(owner, uintptr(unsafe.Pointer(utf16Ptr(text))), uintptr(unsafe.Pointer(utf16Ptr(title))), uintptr(flags))
	return int(r)
}
