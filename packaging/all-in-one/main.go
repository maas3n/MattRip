//go:build windows

package main

import (
	"archive/zip"
	"bytes"
	_ "embed"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"unsafe"
)

//go:embed assets/MattRip-Portable.zip
var portableZip []byte

//go:embed assets/MattRip-Thin-Setup.exe
var thinSetup []byte

var appVersion = "dev"

const (
	wmDestroy = 0x0002
	wmClose   = 0x0010
	wmCommand = 0x0111

	wsCaption = 0x00C00000
	wsSysMenu = 0x00080000
	wsVisible = 0x10000000
	wsChild   = 0x40000000
	ssCenter  = 0x00000001

	swShow = 5

	idRun     = 1001
	idInstall = 1002
	idExit    = 1003

	seeMaskNoCloseProcess = 0x00000040
	infinite              = 0xFFFFFFFF
)

var (
	user32   = syscall.NewLazyDLL("user32.dll")
	kernel32 = syscall.NewLazyDLL("kernel32.dll")
	shell32  = syscall.NewLazyDLL("shell32.dll")

	procRegisterClassExW = user32.NewProc("RegisterClassExW")
	procCreateWindowExW  = user32.NewProc("CreateWindowExW")
	procDefWindowProcW   = user32.NewProc("DefWindowProcW")
	procDestroyWindow    = user32.NewProc("DestroyWindow")
	procPostQuitMessage  = user32.NewProc("PostQuitMessage")
	procGetMessageW      = user32.NewProc("GetMessageW")
	procTranslateMessage = user32.NewProc("TranslateMessage")
	procDispatchMessageW = user32.NewProc("DispatchMessageW")
	procShowWindow       = user32.NewProc("ShowWindow")
	procUpdateWindow     = user32.NewProc("UpdateWindow")
	procLoadCursorW      = user32.NewProc("LoadCursorW")
	procMessageBoxW      = user32.NewProc("MessageBoxW")

	procGetModuleHandleW = kernel32.NewProc("GetModuleHandleW")
	procWaitForSingleObj = kernel32.NewProc("WaitForSingleObject")
	procCloseHandle      = kernel32.NewProc("CloseHandle")

	procShellExecuteExW = shell32.NewProc("ShellExecuteExW")
)

type wndClassEx struct {
	cbSize        uint32
	style         uint32
	lpfnWndProc   uintptr
	cbClsExtra    int32
	cbWndExtra    int32
	hInstance     uintptr
	hIcon         uintptr
	hCursor       uintptr
	hbrBackground uintptr
	lpszMenuName  *uint16
	lpszClassName *uint16
	hIconSm       uintptr
}

type point struct{ x, y int32 }

type msg struct {
	hwnd     uintptr
	message  uint32
	wParam   uintptr
	lParam   uintptr
	time     uint32
	pt       point
	lPrivate uint32
}

type shellExecuteInfo struct {
	cbSize       uint32
	fMask        uint32
	hwnd         uintptr
	lpVerb       *uint16
	lpFile       *uint16
	lpParameters *uint16
	lpDirectory  *uint16
	nShow        int32
	hInstApp     uintptr
	lpIDList     uintptr
	lpClass      *uint16
	hkeyClass    uintptr
	dwHotKey     uint32
	hIconMonitor uintptr
	hProcess     uintptr
}

var selection int

func utf16Ptr(s string) *uint16 {
	p, _ := syscall.UTF16PtrFromString(s)
	return p
}

func wndProc(hwnd uintptr, message uint32, wParam, lParam uintptr) uintptr {
	switch message {
	case wmCommand:
		switch int(uint16(wParam & 0xffff)) {
		case idRun, idInstall, idExit:
			selection = int(uint16(wParam & 0xffff))
			procDestroyWindow.Call(hwnd)
			return 0
		}
	case wmClose:
		selection = idExit
		procDestroyWindow.Call(hwnd)
		return 0
	case wmDestroy:
		procPostQuitMessage.Call(0)
		return 0
	}
	r, _, _ := procDefWindowProcW.Call(hwnd, uintptr(message), wParam, lParam)
	return r
}

func createControl(className, text string, style uint32, x, y, w, h int32, parent uintptr, id int, instance uintptr) {
	procCreateWindowExW.Call(
		0,
		uintptr(unsafe.Pointer(utf16Ptr(className))),
		uintptr(unsafe.Pointer(utf16Ptr(text))),
		uintptr(style),
		uintptr(x), uintptr(y), uintptr(w), uintptr(h),
		parent, uintptr(id), instance, 0,
	)
}

func chooseAction() error {
	// Win32 windows and their message queues belong to the creating OS thread.
	// A Go goroutine may otherwise migrate while a syscall is blocked.
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	instance, _, _ := procGetModuleHandleW.Call(0)
	className := utf16Ptr("MattRipAllInOneWindow")
	cursor, _, _ := procLoadCursorW.Call(0, 32512)
	wc := wndClassEx{
		cbSize:        uint32(unsafe.Sizeof(wndClassEx{})),
		lpfnWndProc:   syscall.NewCallback(wndProc),
		hInstance:     instance,
		hCursor:       cursor,
		hbrBackground: 6,
		lpszClassName: className,
	}
	atom, _, regErr := procRegisterClassExW.Call(uintptr(unsafe.Pointer(&wc)))
	if atom == 0 {
		return fmt.Errorf("could not register chooser window: %v", regErr)
	}

	hwnd, _, createErr := procCreateWindowExW.Call(
		0,
		uintptr(unsafe.Pointer(className)),
		uintptr(unsafe.Pointer(utf16Ptr("MattRip "+appVersion+" — All-in-One"))),
		uintptr(wsCaption|wsSysMenu),
		0x80000000, 0x80000000,
		520, 230,
		0, 0, instance, 0,
	)
	if hwnd == 0 {
		return fmt.Errorf("could not create chooser window: %v", createErr)
	}

	createControl("STATIC", "Choose how you want to use MattRip.\r\nBoth choices include FFmpeg, FFprobe, and MediaInfo.", wsChild|wsVisible|ssCenter, 35, 30, 450, 52, hwnd, 0, instance)
	createControl("BUTTON", "Run MattRip", wsChild|wsVisible, 35, 115, 135, 38, hwnd, idRun, instance)
	createControl("BUTTON", "Install MattRip", wsChild|wsVisible, 192, 115, 135, 38, hwnd, idInstall, instance)
	createControl("BUTTON", "Exit", wsChild|wsVisible, 349, 115, 135, 38, hwnd, idExit, instance)

	procShowWindow.Call(hwnd, swShow)
	procUpdateWindow.Call(hwnd)

	var m msg
	for {
		r, _, msgErr := procGetMessageW.Call(uintptr(unsafe.Pointer(&m)), 0, 0, 0)
		if int32(r) == -1 {
			return fmt.Errorf("chooser message loop failed: %v", msgErr)
		}
		if r == 0 {
			break
		}
		procTranslateMessage.Call(uintptr(unsafe.Pointer(&m)))
		procDispatchMessageW.Call(uintptr(unsafe.Pointer(&m)))
	}
	return nil
}

func safeZipPath(root, name string) (string, error) {
	clean := filepath.Clean(filepath.FromSlash(name))
	if clean == "." || filepath.IsAbs(clean) || clean == ".." || strings.HasPrefix(clean, ".."+string(os.PathSeparator)) {
		return "", fmt.Errorf("unsafe archive path: %q", name)
	}
	out := filepath.Join(root, clean)
	rel, err := filepath.Rel(root, out)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(os.PathSeparator)) {
		return "", fmt.Errorf("unsafe archive path: %q", name)
	}
	return out, nil
}

func copyZipFile(f *zip.File, out string) error {
	if err := os.MkdirAll(filepath.Dir(out), 0755); err != nil {
		return err
	}
	rc, err := f.Open()
	if err != nil {
		return err
	}
	defer rc.Close()
	dst, err := os.OpenFile(out, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0644)
	if err != nil {
		return err
	}
	_, copyErr := io.Copy(dst, rc)
	closeErr := dst.Close()
	if copyErr != nil {
		return copyErr
	}
	return closeErr
}

func extractPortable(root string) (string, error) {
	zr, err := zip.NewReader(bytes.NewReader(portableZip), int64(len(portableZip)))
	if err != nil {
		return "", err
	}
	var portableExe string
	for _, f := range zr.File {
		out, err := safeZipPath(root, f.Name)
		if err != nil {
			return "", err
		}
		if f.FileInfo().IsDir() {
			if err := os.MkdirAll(out, 0755); err != nil {
				return "", err
			}
			continue
		}
		if err := copyZipFile(f, out); err != nil {
			return "", err
		}
		if strings.EqualFold(filepath.Base(out), "MattRip-Portable.exe") {
			portableExe = out
		}
	}
	if portableExe == "" {
		return "", fmt.Errorf("MattRip-Portable.exe was not found in the embedded package")
	}
	return portableExe, nil
}

func runPortable() error {
	root, err := os.MkdirTemp("", "MattRip-"+appVersion+"-Run-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(root)

	exePath, err := extractPortable(root)
	if err != nil {
		return fmt.Errorf("could not prepare portable MattRip: %w", err)
	}
	cmd := exec.Command(exePath)
	cmd.Dir = filepath.Dir(exePath)
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("MattRip exited with an error: %w", err)
	}
	return nil
}

func preloadTools() error {
	local := strings.TrimSpace(os.Getenv("LOCALAPPDATA"))
	if local == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return fmt.Errorf("LOCALAPPDATA is unavailable")
		}
		local = filepath.Join(home, "AppData", "Local")
	}
	destRoot := filepath.Join(local, "MattRip")

	zr, err := zip.NewReader(bytes.NewReader(portableZip), int64(len(portableZip)))
	if err != nil {
		return err
	}
	copied := 0
	const marker = "MattRipData/tools/"
	for _, f := range zr.File {
		norm := strings.ReplaceAll(f.Name, "\\", "/")
		idx := strings.Index(norm, marker)
		if idx < 0 {
			continue
		}
		rel := norm[idx+len("MattRipData/"):]
		out, err := safeZipPath(destRoot, rel)
		if err != nil {
			return err
		}
		if f.FileInfo().IsDir() {
			if err := os.MkdirAll(out, 0755); err != nil {
				return err
			}
			continue
		}
		if err := copyZipFile(f, out); err != nil {
			return err
		}
		copied++
	}
	if copied == 0 {
		return fmt.Errorf("no bundled tools were found in the embedded package")
	}
	for _, required := range []string{
		filepath.Join(destRoot, "tools", "ffmpeg-2026-09-08", "ffmpeg.exe"),
		filepath.Join(destRoot, "tools", "ffmpeg-2026-09-08", "ffprobe.exe"),
		filepath.Join(destRoot, "tools", "mediainfo-26.05", "MediaInfo.exe"),
	} {
		if _, err := os.Stat(required); err != nil {
			return fmt.Errorf("required bundled tool is missing: %s", required)
		}
	}
	return nil
}

func shellRunAsAndWait(file string) error {
	sei := shellExecuteInfo{
		fMask:  seeMaskNoCloseProcess,
		lpVerb: utf16Ptr("runas"),
		lpFile: utf16Ptr(file),
		nShow:  swShow,
	}
	sei.cbSize = uint32(unsafe.Sizeof(sei))
	r, _, callErr := procShellExecuteExW.Call(uintptr(unsafe.Pointer(&sei)))
	if r == 0 {
		return fmt.Errorf("could not start installer: %v", callErr)
	}
	if sei.hProcess != 0 {
		procWaitForSingleObj.Call(sei.hProcess, infinite)
		procCloseHandle.Call(sei.hProcess)
	}
	return nil
}

func installMattRip() error {
	if err := preloadTools(); err != nil {
		return fmt.Errorf("could not preload bundled tools: %w", err)
	}
	root, err := os.MkdirTemp("", "MattRip-"+appVersion+"-Install-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(root)
	setupPath := filepath.Join(root, "MattRip-"+appVersion+"-Setup.exe")
	if err := os.WriteFile(setupPath, thinSetup, 0644); err != nil {
		return err
	}
	return shellRunAsAndWait(setupPath)
}

func showError(err error) {
	if err == nil {
		return
	}
	procMessageBoxW.Call(
		0,
		uintptr(unsafe.Pointer(utf16Ptr(err.Error()))),
		uintptr(unsafe.Pointer(utf16Ptr("MattRip All-in-One"))),
		0x00000010,
	)
}

func main() {
	if err := chooseAction(); err != nil {
		showError(err)
		return
	}
	var err error
	switch selection {
	case idRun:
		err = runPortable()
	case idInstall:
		err = installMattRip()
	default:
		return
	}
	showError(err)
}
