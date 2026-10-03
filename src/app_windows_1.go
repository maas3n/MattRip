//go:build windows

package main

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"runtime"
	"syscall"
	"unsafe"
)

func windowsGUIMain() {
	runtime.LockOSThread()
	initLogging()
	defer closeLogging()
	log.Printf("%s %s starting", appName, appVersion)

	if err := procSetProcessDpiAwarenessContext.Find(); err == nil {
		_, _, _ = procSetProcessDpiAwarenessContext.Call(^uintptr(3))
	} else if err := procSetProcessDPIAware.Find(); err == nil {
		_, _, _ = procSetProcessDPIAware.Call()
	}
	_, _, _ = procOleInitialize.Call(0)
	defer procOleUninitialize.Call()
	icc := INITCOMMONCONTROLSEX{DwSize: uint32(unsafe.Sizeof(INITCOMMONCONTROLSEX{})), DwICC: ICC_PROGRESS_CLASS | ICC_LISTVIEW_CLASSES | 0x8}
	procInitCommonControlsEx.Call(uintptr(unsafe.Pointer(&icc)))
	if err := createMainWindow(); err != nil {
		messageBox(0, "MattRip could not start", err.Error(), MB_OK|MB_ICONERROR)
		return
	}
	var msg MSG
	for {
		r, _, _ := procGetMessageW.Call(uintptr(unsafe.Pointer(&msg)), 0, 0, 0)
		if int32(r) <= 0 {
			break
		}
		procTranslateMessage.Call(uintptr(unsafe.Pointer(&msg)))
		procDispatchMessageW.Call(uintptr(unsafe.Pointer(&msg)))
	}
}

func windowDPI(hwnd uintptr) int32 {
	hdc, _, _ := procGetDC.Call(hwnd)
	dpi, _, _ := procGetDeviceCaps.Call(hdc, LOGPIXELSY)
	procReleaseDC.Call(hwnd, hdc)
	if dpi == 0 {
		return 96
	}
	return int32(dpi)
}

func scale96(v, dpi int32) int32 {
	if dpi <= 0 {
		dpi = 96
	}
	return (v*dpi + 48) / 96
}

func createMainWindow() error {
	hInstance, _, _ := procGetModuleHandleW.Call(0)
	className := utf16Ptr("MattRipWindowClass")
	cursor, _, _ := procLoadCursorW.Call(0, 32512)
	icon := loadAppIcon()
	// Use the system control background. Stock object 5 is NULL_BRUSH, which
	// leaves old tab pixels untouched even when RedrawWindow requests erasure.
	const colorBtnFace = 15
	bg, _, _ := user32.NewProc("GetSysColorBrush").Call(colorBtnFace)
	wc := WNDCLASSEX{CbSize: uint32(unsafe.Sizeof(WNDCLASSEX{})), LpfnWndProc: syscall.NewCallback(windowProc), HInstance: hInstance, HIcon: icon, HCursor: cursor, HbrBackground: bg, HIconSm: icon, LpszClassName: className}
	if r, _, err := procRegisterClassExW.Call(uintptr(unsafe.Pointer(&wc))); r == 0 {
		return fmt.Errorf("RegisterClassExW failed: %v", err)
	}

	// The UI is authored on a 96-DPI design grid. Scale both the window and every
	// child control to the monitor DPI; previously only the fonts were scaled,
	// which caused clipping and overlap at 125%/150%/175% Windows scaling.
	dpi := windowDPI(0)
	winW, winH := scale96(820, dpi), scale96(655, dpi)
	style := uintptr(WS_OVERLAPPED | WS_CAPTION | WS_SYSMENU | WS_MINIMIZEBOX)
	hwnd, _, err := procCreateWindowExW.Call(WS_EX_CONTROLPARENT, uintptr(unsafe.Pointer(className)), uintptr(unsafe.Pointer(utf16Ptr(appName+" "+appVersion))), style, 0, 0, uintptr(winW), uintptr(winH), 0, 0, hInstance, 0)
	if hwnd == 0 {
		return fmt.Errorf("CreateWindowExW failed: %v", err)
	}
	app.hwnd = hwnd
	createFonts(hwnd)
	createControls(hwnd, hInstance)
	createMergerWindowsControls(hwnd, hInstance)
	centerWindow(hwnd, winW, winH)
	procDragAcceptFiles.Call(hwnd, 1)
	procShowWindow.Call(hwnd, SW_SHOW)
	procUpdateWindow.Call(hwnd)
	settings := loadSettings()
	if settings.OutputDir == "" {
		settings.OutputDir = defaultOutputDir()
	}
	setText(app.outputEdit, settings.OutputDir)
	setChecked(app.preserveChapters, settings.PreserveChapters)
	setStatus("Choose a DVD folder or ISO file to begin.")
	setProgress(0)
	return nil
}

func loadAppIcon() uintptr {
	if exe, err := os.Executable(); err == nil {
		iconPath := filepath.Join(filepath.Dir(exe), "MattRip.ico")
		if fileExists(iconPath) {
			r, _, _ := procLoadImageW.Call(0, uintptr(unsafe.Pointer(utf16Ptr(iconPath))), IMAGE_ICON, 0, 0, LR_LOADFROMFILE|LR_DEFAULTSIZE)
			if r != 0 {
				return r
			}
		}
	}
	r, _, _ := procLoadIconW.Call(0, 32512)
	return r
}

func createFonts(hwnd uintptr) {
	dpi := uintptr(windowDPI(hwnd))
	fontHeight := func(pt int32) int32 { r, _, _ := procMulDiv.Call(uintptr(pt), dpi, 72); return -int32(r) }
	app.bodyFont = createFont(fontHeight(10), FW_NORMAL, "Segoe UI")
	app.headerFont = createFont(fontHeight(22), FW_SEMIBOLD, "Segoe UI")
	app.monoFont = createFont(fontHeight(10), FW_NORMAL, "Consolas")
}

func createFont(height int32, weight int32, face string) uintptr {
	r, _, _ := procCreateFontW.Call(uintptr(height), 0, 0, 0, uintptr(weight), 0, 0, 0, 1, 0, 0, 5, 0, uintptr(unsafe.Pointer(utf16Ptr(face))))
	return r
}

func createControls(hwnd, hInstance uintptr) {
	dpi := windowDPI(hwnd)
	s := func(v int32) int32 { return scale96(v, dpi) }
	add := func(ex uint32, class, text string, style uint32, x, y, w, h int32, id int, font uintptr) uintptr {
		c, _, _ := procCreateWindowExW.Call(uintptr(ex), uintptr(unsafe.Pointer(utf16Ptr(class))), uintptr(unsafe.Pointer(utf16Ptr(text))), uintptr(style), uintptr(s(x)), uintptr(s(y+40)), uintptr(s(w)), uintptr(s(h)), hwnd, uintptr(id), hInstance, 0)
		if font != 0 {
			procSendMessageW.Call(c, WM_SETFONT, font, 1)
		}
		mergerWindow.dvd = append(mergerWindow.dvd, c)
		return c
	}
	add(0, "STATIC", "MattRip", WS_CHILD|WS_VISIBLE, 28, 22, 300, 42, 0, app.headerFont)
	add(0, "STATIC", "Lossless DVD title remuxing to Matroska — video, audio, subtitles, chapters and metadata.", WS_CHILD|WS_VISIBLE, 30, 64, 750, 24, 0, app.bodyFont)
	add(0, "BUTTON", "Source", WS_CHILD|WS_VISIBLE|BS_GROUPBOX, 22, 102, 770, 112, 0, app.bodyFont)
	add(0, "STATIC", "DVD / DRIVE / VIDEO_TS / ISO / MKV", WS_CHILD|WS_VISIBLE, 38, 127, 270, 22, 0, app.bodyFont)
	app.sourceEdit = add(WS_EX_CLIENTEDGE, "EDIT", "", WS_CHILD|WS_VISIBLE|WS_TABSTOP|ES_AUTOHSCROLL, 38, 150, 392, 28, idSourceEdit, app.bodyFont)
	app.sourceDVDButton = add(0, "BUTTON", "DVD Folder…", WS_CHILD|WS_VISIBLE|WS_TABSTOP|BS_PUSHBUTTON, 442, 148, 100, 31, idDVDButton, app.bodyFont)
	app.sourceISOButton = add(0, "BUTTON", "ISO / MKV…", WS_CHILD|WS_VISIBLE|WS_TABSTOP|BS_PUSHBUTTON, 550, 148, 96, 31, idISOButton, app.bodyFont)
	app.sourceDriveButton = add(0, "BUTTON", "DVD Drive…", WS_CHILD|WS_VISIBLE|WS_TABSTOP|BS_PUSHBUTTON, 654, 148, 116, 31, idDriveButton, app.bodyFont)
	add(0, "STATIC", "Choose a DVD folder, ISO/MKV file, physical DVD drive, or drag a folder/file here.", WS_CHILD|WS_VISIBLE, 38, 184, 590, 20, 0, app.bodyFont)
	add(0, "BUTTON", "Destination", WS_CHILD|WS_VISIBLE|BS_GROUPBOX, 22, 224, 770, 94, 0, app.bodyFont)
	add(0, "STATIC", "Output folder", WS_CHILD|WS_VISIBLE, 38, 249, 120, 22, 0, app.bodyFont)
	app.outputEdit = add(WS_EX_CLIENTEDGE, "EDIT", "", WS_CHILD|WS_VISIBLE|WS_TABSTOP|ES_AUTOHSCROLL, 38, 272, 622, 28, idOutputEdit, app.bodyFont)
	app.outputButton = add(0, "BUTTON", "Browse…", WS_CHILD|WS_VISIBLE|WS_TABSTOP|BS_PUSHBUTTON, 674, 270, 96, 31, idOutputBtn, app.bodyFont)
	add(0, "BUTTON", "DVD Title", WS_CHILD|WS_VISIBLE|BS_GROUPBOX, 22, 328, 770, 101, 0, app.bodyFont)
	add(0, "STATIC", "Title", WS_CHILD|WS_VISIBLE, 38, 354, 45, 22, 0, app.bodyFont)
	app.titleCombo = add(0, "COMBOBOX", "", WS_CHILD|WS_VISIBLE|WS_TABSTOP|CBS_DROPDOWNLIST|WS_VSCROLL, 82, 349, 355, 250, idTitleCombo, app.bodyFont)
	app.scanBtn = add(0, "BUTTON", "Scan Titles", WS_CHILD|WS_VISIBLE|WS_TABSTOP|BS_PUSHBUTTON, 452, 348, 105, 32, idScanBtn, app.bodyFont)
	app.metaBtn = add(0, "BUTTON", "Show Metadata", WS_CHILD|WS_VISIBLE|WS_TABSTOP|BS_PUSHBUTTON, 568, 348, 124, 32, idMetaBtn, app.bodyFont)
	add(0, "BUTTON", "About", WS_CHILD|WS_VISIBLE|WS_TABSTOP|BS_PUSHBUTTON, 702, 348, 68, 32, idAboutBtn, app.bodyFont)
	app.preserveChapters = add(0, "BUTTON", "Include chapters in remux / demux", WS_CHILD|WS_VISIBLE|WS_TABSTOP|BS_AUTOCHECKBOX, 38, 389, 300, 24, idPreserveChapters, app.bodyFont)
	app.progress = add(0, "msctls_progress32", "", WS_CHILD|WS_VISIBLE, 28, 450, 764, 16, 0, app.bodyFont)
	procSendMessageW.Call(app.progress, PBM_SETRANGE32, 0, 1000)
	app.statusText = add(0, "STATIC", "", WS_CHILD|WS_VISIBLE, 30, 474, 755, 42, 0, app.bodyFont)
	add(0, "STATIC", "Remux uses fixed timestamps (-fflags +genpts).", WS_CHILD|WS_VISIBLE, 30, 516, 480, 18, 0, app.bodyFont)
	app.remuxBtn = add(0, "BUTTON", "Start Remux", WS_CHILD|WS_VISIBLE|WS_TABSTOP|BS_DEFPUSHBUTTON, 396, 535, 132, 38, idRemuxBtn, app.bodyFont)
	app.demuxBtn = add(0, "BUTTON", "Demux", WS_CHILD|WS_VISIBLE|WS_TABSTOP|BS_PUSHBUTTON, 538, 535, 132, 38, idDemuxBtn, app.bodyFont)
	app.cancelBtn = add(0, "BUTTON", "Cancel", WS_CHILD|WS_VISIBLE|WS_TABSTOP|BS_PUSHBUTTON, 680, 535, 110, 38, idCancelBtn, app.bodyFont)
	procEnableWindow.Call(app.cancelBtn, 0)
}

func chooseWindowsPhysicalDVDDrive(owner uintptr) string {
	drives, err := listPhysicalDVDDrives()
	if err != nil {
		messageBox(owner, "DVD Drive", err.Error(), MB_OK|MB_ICONERROR)
		return ""
	}
	if len(drives) == 0 {
		messageBox(owner, "DVD Drive", "No physical optical DVD/CD-ROM drive was detected.", MB_OK|MB_ICONWARNING)
		return ""
	}
	if len(drives) == 1 {
		return drives[0].Input
	}
	p := browseFolder(owner, "Choose one of the detected physical DVD drives")
	if p == "" {
		return ""
	}
	drive, ok, err := resolvePhysicalDVDDrive(p)
	if err != nil {
		messageBox(owner, "DVD Drive", err.Error(), MB_OK|MB_ICONERROR)
		return ""
	}
	if !ok {
		messageBox(owner, "DVD Drive", "The selected location is not an optical DVD/CD-ROM drive.", MB_OK|MB_ICONWARNING)
		return ""
	}
	return drive.Input
}

func windowProc(hwnd uintptr, msg uint32, wParam, lParam uintptr) uintptr {
	switch msg {
	case mergerDoneMessage:
		mergerWindow.pendingMu.Lock()
		done := mergerWindow.pending
		mergerWindow.pending = nil
		mergerWindow.pendingMu.Unlock()
		if done != nil {
			done()
		}
		return 0
	case 0x004e:
		var header mergerNotifyHeader
		kernel32.NewProc("RtlMoveMemory").Call(uintptr(unsafe.Pointer(&header)), lParam, unsafe.Sizeof(header))
		if header.From == mergerWindow.tab && header.Code == -551 {
			showMergerWindowsTab()
			return 0
		}
	case WM_COMMAND:
		id := int(wParam & 0xffff)
		if handleWindowsMergerCommand(id) {
			return 0
		}
		notify := uint16((wParam >> 16) & 0xffff)
		switch id {
		case idSourceEdit:
			if notify == EN_CHANGE && !app.busy.Load() {
				app.invalidateTitles()
				setStatus("Source changed. Scan titles again.")
			}
		case idTitleCombo:
			if notify == 1 && !app.busy.Load() {
				clearWindowsTrackSelection()
				setStatus("Title changed. Show Metadata to review track selection for this title.")
			}
		case idDVDButton:
			if !app.busy.Load() {
				if p := browseFolder(hwnd, "Choose the DVD folder (or its VIDEO_TS folder)"); p != "" {
					setSource(p)
				}
			}
		case idISOButton:
			if !app.busy.Load() {
				if p := browseISO(hwnd); p != "" {
					setSource(p)
				}
			}
		case idDriveButton:
			if !app.busy.Load() {
				if p := chooseWindowsPhysicalDVDDrive(hwnd); p != "" {
					setSource(p)
				}
			}
		case idOutputBtn:
			if !app.busy.Load() {
				if p := browseFolder(hwnd, "Choose where MattRip should save the MKV"); p != "" {
					setText(app.outputEdit, p)
					saveCurrentSettings()
				}
			}
		case idPreserveChapters:
			if !app.busy.Load() {
				saveCurrentSettings()
				if isChecked(app.preserveChapters) {
					setStatus("Chapter preservation enabled. FFmpeg will write detected DVD chapters to the MKV.")
				} else {
					setStatus("Chapter preservation disabled. The output MKV will not contain chapter markers.")
				}
			}
		case idScanBtn:
			startAsync("Scanning DVD titles through FFmpeg dvdvideo/libdvdread/libdvdnav…", discoverDVDTitlesViaDVDVideo)
		case idMetaBtn:
			startAsync("Reading title metadata…", showMetadata)
		case idRemuxBtn:
			startAsync("Preparing remux…", remuxSelected)
		case idDemuxBtn:
			chooseWindowsDemux()
		case idCancelBtn:
			app.cancelCurrent()
		case idAboutBtn:
			showAbout()
		}
		return 0
	case WM_SHOWTEXT:
		app.pendingTextMu.Lock()
		title, text := app.pendingTitle, app.pendingText
		app.pendingTitle, app.pendingText = "", ""
		app.pendingTextMu.Unlock()
		if text != "" {
			showTextWindow(title, text)
		}
		return 0
	case WM_SHOWTRACKS:
		if req, ok := takePendingWindowsTrackRequest(); ok {
			showWindowsTrackWindow(req)
		}
		return 0
	case WM_DROPFILES:
		if !app.busy.Load() {
			if p := droppedPath(wParam); p != "" {
				setSource(p)
			}
		} else {
			procDragFinish.Call(wParam)
		}
		return 0
	case WM_CLOSE:
		if app.busy.Load() {
			if messageBox(hwnd, "Operation in progress", "Cancel the current operation and close MattRip?", MB_OKCANCEL|MB_ICONQUESTION) != IDOK {
				return 0
			}
			app.cancelCurrent()
		}
		procDestroyWindow.Call(hwnd)
		return 0
	case WM_DESTROY:
		procPostQuitMessage.Call(0)
		return 0
	}
	r, _, _ := procDefWindowProcW.Call(hwnd, uintptr(msg), wParam, lParam)
	return r
}
