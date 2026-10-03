//go:build windows

package main

import (
	"fmt"
	"sort"
	"sync"
	"syscall"
	"unsafe"
)

const (
	ICC_LISTVIEW_CLASSES = 0x00000001
	WM_SHOWTRACKS        = WM_APP + 2

	LVS_REPORT        = 0x0001
	LVS_SHOWSELALWAYS = 0x0008

	LVS_EX_GRIDLINES     = 0x00000001
	LVS_EX_CHECKBOXES    = 0x00000004
	LVS_EX_FULLROWSELECT = 0x00000020

	LVM_FIRST                    = 0x1000
	LVM_INSERTITEMW              = LVM_FIRST + 77
	LVM_INSERTCOLUMNW            = LVM_FIRST + 97
	LVM_SETITEMSTATE             = LVM_FIRST + 43
	LVM_GETITEMSTATE             = LVM_FIRST + 44
	LVM_SETEXTENDEDLISTVIEWSTYLE = LVM_FIRST + 54
	LVM_SETCOLUMNWIDTH           = LVM_FIRST + 30

	LVIF_TEXT           = 0x0001
	LVIF_STATE          = 0x0008
	LVIS_STATEIMAGEMASK = 0xF000
	LVCF_WIDTH          = 0x0002
	LVCF_TEXT           = 0x0004

	idTrackList       = 6200
	idTrackDetails    = 6201
	idTrackSelectAll  = 6202
	idTrackSelectNone = 6203
	idTrackClose      = 6204

	wmSize = 0x0005
)

type LVCOLUMNW struct {
	Mask       uint32
	Fmt        int32
	Cx         int32
	PszText    *uint16
	CchTextMax int32
	ISubItem   int32
	IImage     int32
	IOrder     int32
	CxMin      int32
	CxDefault  int32
	CxIdeal    int32
}

type LVITEMW struct {
	Mask       uint32
	IItem      int32
	ISubItem   int32
	State      uint32
	StateMask  uint32
	PszText    *uint16
	CchTextMax int32
	IImage     int32
	LParam     uintptr
	IIndent    int32
	IGroupID   int32
	CColumns   uint32
	PuColumns  *uint32
	PiColFmt   *int32
	IGroup     int32
}

type windowsTrackRequest struct {
	source   string
	title    int
	options  []trackOption
	selected map[int]bool
	details  string
}

type windowsTrackDialog struct {
	hwnd        uintptr
	list        uintptr
	details     uintptr
	instruction uintptr
	selectAll   uintptr
	selectNone  uintptr
	closeBtn    uintptr
	source      string
	title       int
	options     []trackOption
}

var windowsTracks struct {
	sync.Mutex
	source   string
	title    int
	options  []trackOption
	selected map[int]bool
	pending  *windowsTrackRequest
	dialog   *windowsTrackDialog
}

func clearWindowsTrackSelection() {
	windowsTracks.Lock()
	windowsTracks.source = ""
	windowsTracks.title = 0
	windowsTracks.options = nil
	windowsTracks.selected = nil
	windowsTracks.Unlock()
}

func requestWindowsTrackWindow(source string, title int, options []trackOption, details string) {
	windowsTracks.Lock()
	selected := make(map[int]bool, len(options))
	if windowsTracks.source == source && windowsTracks.title == title && windowsTracks.selected != nil {
		for _, option := range options {
			value, ok := windowsTracks.selected[option.Index]
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
	windowsTracks.source = source
	windowsTracks.title = title
	windowsTracks.options = append([]trackOption(nil), options...)
	windowsTracks.selected = selected
	requestSelected := make(map[int]bool, len(selected))
	for index, value := range selected {
		requestSelected[index] = value
	}
	windowsTracks.pending = &windowsTrackRequest{
		source: source, title: title, options: append([]trackOption(nil), options...),
		selected: requestSelected, details: details,
	}
	windowsTracks.Unlock()
	procPostMessageW.Call(app.hwnd, WM_SHOWTRACKS, 0, 0)
}

func takePendingWindowsTrackRequest() (windowsTrackRequest, bool) {
	windowsTracks.Lock()
	defer windowsTracks.Unlock()
	if windowsTracks.pending == nil {
		return windowsTrackRequest{}, false
	}
	req := *windowsTracks.pending
	windowsTracks.pending = nil
	return req, true
}

func windowsSelectedTrackIndexes(source string, title int) ([]int, bool) {
	windowsTracks.Lock()
	defer windowsTracks.Unlock()
	if windowsTracks.source != source || windowsTracks.title != title || windowsTracks.selected == nil {
		return nil, false
	}
	indexes := make([]int, 0, len(windowsTracks.options))
	for _, option := range windowsTracks.options {
		if windowsTracks.selected[option.Index] {
			indexes = append(indexes, option.Index)
		}
	}
	sort.Ints(indexes)
	return indexes, true
}

func showWindowsTrackWindow(req windowsTrackRequest) {
	windowsTracks.Lock()
	if windowsTracks.dialog != nil && windowsTracks.dialog.hwnd != 0 {
		procDestroyWindow.Call(windowsTracks.dialog.hwnd)
	}
	windowsTracks.Unlock()

	hInstance, _, _ := procGetModuleHandleW.Call(0)
	className := utf16Ptr("MattRipTracksWindowClass")
	cursor, _, _ := procLoadCursorW.Call(0, 32512)
	bg, _, _ := procGetStockObject.Call(5)
	wc := WNDCLASSEX{CbSize: uint32(unsafe.Sizeof(WNDCLASSEX{})), LpfnWndProc: syscall.NewCallback(windowsTrackWindowProc), HInstance: hInstance, HCursor: cursor, HbrBackground: bg, LpszClassName: className}
	procRegisterClassExW.Call(uintptr(unsafe.Pointer(&wc)))
	dpi := windowDPI(app.hwnd)
	width, height := scale96(900, dpi), scale96(720, dpi)
	hwnd, _, _ := procCreateWindowExW.Call(WS_EX_CONTROLPARENT, uintptr(unsafe.Pointer(className)), uintptr(unsafe.Pointer(utf16Ptr(fmt.Sprintf("MattRip — Title %d Tracks / Metadata", req.title)))), WS_OVERLAPPED|WS_CAPTION|WS_SYSMENU|WS_THICKFRAME|WS_MINIMIZEBOX|WS_MAXIMIZEBOX|WS_VISIBLE, CW_USEDEFAULT, CW_USEDEFAULT, uintptr(width), uintptr(height), app.hwnd, 0, hInstance, 0)
	if hwnd == 0 {
		messageBox(app.hwnd, "MattRip — Tracks / Metadata", req.details, MB_OK|MB_ICONINFORMATION)
		return
	}
	instruction, _, _ := procCreateWindowExW.Call(0, uintptr(unsafe.Pointer(utf16Ptr("STATIC"))), uintptr(unsafe.Pointer(utf16Ptr("Choose the video, audio, and subtitle tracks to include in the next remux. All tracks are selected by default."))), WS_CHILD|WS_VISIBLE, 0, 0, 0, 0, hwnd, 0, hInstance, 0)
	list, _, _ := procCreateWindowExW.Call(WS_EX_CLIENTEDGE, uintptr(unsafe.Pointer(utf16Ptr("SysListView32"))), 0, WS_CHILD|WS_VISIBLE|WS_TABSTOP|LVS_REPORT|LVS_SHOWSELALWAYS, 0, 0, 0, 0, hwnd, idTrackList, hInstance, 0)
	details, _, _ := procCreateWindowExW.Call(WS_EX_CLIENTEDGE, uintptr(unsafe.Pointer(utf16Ptr("EDIT"))), uintptr(unsafe.Pointer(utf16Ptr(req.details))), WS_CHILD|WS_VISIBLE|WS_VSCROLL|ES_MULTILINE|ES_AUTOVSCROLL|ES_READONLY, 0, 0, 0, 0, hwnd, idTrackDetails, hInstance, 0)
	selectAll, _, _ := procCreateWindowExW.Call(0, uintptr(unsafe.Pointer(utf16Ptr("BUTTON"))), uintptr(unsafe.Pointer(utf16Ptr("Select all"))), WS_CHILD|WS_VISIBLE|WS_TABSTOP|BS_PUSHBUTTON, 0, 0, 0, 0, hwnd, idTrackSelectAll, hInstance, 0)
	selectNone, _, _ := procCreateWindowExW.Call(0, uintptr(unsafe.Pointer(utf16Ptr("BUTTON"))), uintptr(unsafe.Pointer(utf16Ptr("Select none"))), WS_CHILD|WS_VISIBLE|WS_TABSTOP|BS_PUSHBUTTON, 0, 0, 0, 0, hwnd, idTrackSelectNone, hInstance, 0)
	closeBtn, _, _ := procCreateWindowExW.Call(0, uintptr(unsafe.Pointer(utf16Ptr("BUTTON"))), uintptr(unsafe.Pointer(utf16Ptr("Close && use selection"))), WS_CHILD|WS_VISIBLE|WS_TABSTOP|BS_DEFPUSHBUTTON, 0, 0, 0, 0, hwnd, idTrackClose, hInstance, 0)
	for _, control := range []uintptr{instruction, list, selectAll, selectNone, closeBtn} {
		if app.bodyFont != 0 {
			procSendMessageW.Call(control, WM_SETFONT, app.bodyFont, 1)
		}
	}
	if app.monoFont != 0 {
		procSendMessageW.Call(details, WM_SETFONT, app.monoFont, 1)
	}
	procSendMessageW.Call(list, LVM_SETEXTENDEDLISTVIEWSTYLE, 0, LVS_EX_CHECKBOXES|LVS_EX_FULLROWSELECT|LVS_EX_GRIDLINES)
	columnText := utf16Ptr("Track")
	column := LVCOLUMNW{Mask: LVCF_TEXT | LVCF_WIDTH, Cx: scale96(820, dpi), PszText: columnText}
	procSendMessageW.Call(list, LVM_INSERTCOLUMNW, 0, uintptr(unsafe.Pointer(&column)))
	for i, option := range req.options {
		text := utf16Ptr(option.Label())
		item := LVITEMW{Mask: LVIF_TEXT, IItem: int32(i), PszText: text}
		procSendMessageW.Call(list, LVM_INSERTITEMW, 0, uintptr(unsafe.Pointer(&item)))
		setWindowsListChecked(list, i, req.selected[option.Index])
	}
	dlg := &windowsTrackDialog{hwnd: hwnd, list: list, details: details, instruction: instruction, selectAll: selectAll, selectNone: selectNone, closeBtn: closeBtn, source: req.source, title: req.title, options: req.options}
	windowsTracks.Lock()
	windowsTracks.dialog = dlg
	windowsTracks.Unlock()
	layoutWindowsTrackDialog(dlg)
	procShowWindow.Call(hwnd, SW_SHOW)
}

func setWindowsListChecked(list uintptr, row int, checked bool) {
	stateImage := uint32(1)
	if checked {
		stateImage = 2
	}
	item := LVITEMW{Mask: LVIF_STATE, StateMask: LVIS_STATEIMAGEMASK, State: stateImage << 12}
	procSendMessageW.Call(list, LVM_SETITEMSTATE, uintptr(row), uintptr(unsafe.Pointer(&item)))
}

func windowsListChecked(list uintptr, row int) bool {
	state, _, _ := procSendMessageW.Call(list, LVM_GETITEMSTATE, uintptr(row), LVIS_STATEIMAGEMASK)
	return ((state >> 12) & 0xF) == 2
}

func saveWindowsTrackDialog(dlg *windowsTrackDialog) {
	if dlg == nil || dlg.list == 0 {
		return
	}
	selected := make(map[int]bool, len(dlg.options))
	count := 0
	for row, option := range dlg.options {
		checked := windowsListChecked(dlg.list, row)
		selected[option.Index] = checked
		if checked {
			count++
		}
	}
	windowsTracks.Lock()
	if windowsTracks.source == dlg.source && windowsTracks.title == dlg.title {
		windowsTracks.selected = selected
	}
	windowsTracks.Unlock()
	if count == 0 {
		setStatus("No tracks selected. Choose at least one track before starting the remux.")
	} else {
		setStatus(fmt.Sprintf("Track selection updated: %d track(s) will be included in the next remux.", count))
	}
}

func closeWindowsTrackDialog(hwnd uintptr) {
	windowsTracks.Lock()
	dlg := windowsTracks.dialog
	windowsTracks.Unlock()
	if dlg != nil && dlg.hwnd == hwnd {
		saveWindowsTrackDialog(dlg)
	}
	procDestroyWindow.Call(hwnd)
}

func layoutWindowsTrackDialog(dlg *windowsTrackDialog) {
	if dlg == nil || dlg.hwnd == 0 {
		return
	}
	var rect struct{ Left, Top, Right, Bottom int32 }
	procGetClientRect.Call(dlg.hwnd, uintptr(unsafe.Pointer(&rect)))
	dpi := windowDPI(dlg.hwnd)
	m := scale96(12, dpi)
	gap := scale96(8, dpi)
	headerH := scale96(42, dpi)
	buttonH := scale96(32, dpi)
	buttonW := scale96(110, dpi)
	closeW := scale96(170, dpi)
	clientW := rect.Right - rect.Left
	clientH := rect.Bottom - rect.Top
	detailsH := clientH / 3
	if detailsH < scale96(150, dpi) {
		detailsH = scale96(150, dpi)
	}
	buttonY := clientH - m - buttonH
	detailsY := buttonY - gap - detailsH
	listY := m + headerH
	listH := detailsY - gap - listY
	if listH < scale96(120, dpi) {
		listH = scale96(120, dpi)
	}
	procSetWindowPos.Call(dlg.instruction, 0, uintptr(m), uintptr(m), uintptr(max32(scale96(100, dpi), clientW-2*m)), uintptr(headerH), 0x0004)
	procSetWindowPos.Call(dlg.list, 0, uintptr(m), uintptr(listY), uintptr(max32(scale96(100, dpi), clientW-2*m)), uintptr(listH), 0x0004)
	procSendMessageW.Call(dlg.list, LVM_SETCOLUMNWIDTH, 0, uintptr(max32(scale96(100, dpi), clientW-2*m-scale96(6, dpi))))
	procSetWindowPos.Call(dlg.details, 0, uintptr(m), uintptr(detailsY), uintptr(max32(scale96(100, dpi), clientW-2*m)), uintptr(detailsH), 0x0004)
	procSetWindowPos.Call(dlg.selectAll, 0, uintptr(m), uintptr(buttonY), uintptr(buttonW), uintptr(buttonH), 0x0004)
	procSetWindowPos.Call(dlg.selectNone, 0, uintptr(m+buttonW+gap), uintptr(buttonY), uintptr(buttonW), uintptr(buttonH), 0x0004)
	procSetWindowPos.Call(dlg.closeBtn, 0, uintptr(clientW-m-closeW), uintptr(buttonY), uintptr(closeW), uintptr(buttonH), 0x0004)
}

func windowsTrackWindowProc(hwnd uintptr, msg uint32, wParam, lParam uintptr) uintptr {
	switch msg {
	case wmSize:
		windowsTracks.Lock()
		dlg := windowsTracks.dialog
		windowsTracks.Unlock()
		if dlg != nil && dlg.hwnd == hwnd {
			layoutWindowsTrackDialog(dlg)
		}
		return 0
	case WM_COMMAND:
		id := int(wParam & 0xffff)
		windowsTracks.Lock()
		dlg := windowsTracks.dialog
		windowsTracks.Unlock()
		if dlg == nil || dlg.hwnd != hwnd {
			break
		}
		switch id {
		case idTrackSelectAll:
			for row := range dlg.options {
				setWindowsListChecked(dlg.list, row, true)
			}
			return 0
		case idTrackSelectNone:
			for row := range dlg.options {
				setWindowsListChecked(dlg.list, row, false)
			}
			return 0
		case idTrackClose:
			closeWindowsTrackDialog(hwnd)
			return 0
		}
	case WM_CLOSE:
		closeWindowsTrackDialog(hwnd)
		return 0
	case WM_DESTROY:
		windowsTracks.Lock()
		if windowsTracks.dialog != nil && windowsTracks.dialog.hwnd == hwnd {
			windowsTracks.dialog = nil
		}
		windowsTracks.Unlock()
		return 0
	}
	r, _, _ := procDefWindowProcW.Call(hwnd, uintptr(msg), wParam, lParam)
	return r
}
