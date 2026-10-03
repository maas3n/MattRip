//go:build windows

package main

import (
	"context"
	"os"
	"sync"
	"sync/atomic"
	"syscall"
	"time"
)

const (
	appName = "MattRip"

	// Fixed, immutable FFmpeg autobuild. We verify the release checksum manifest
	// against this hard-coded SHA-256 before trusting the archive checksum inside it.
	ffmpegReleaseTag   = "autobuild-2026-09-08-23-15"
	ffmpegAssetName    = "ffmpeg-N-126479-g08cd8df29d-win64-gpl-shared.zip"
	ffmpegManifestSHA  = "f64be162403094773397bfcc299a4a059507028afa7563591fd05c17d56b3214"
	mediaInfoVersion   = "26.05"
	mediaInfoAssetName = "MediaInfo_CLI_26.05_Windows_x64.zip"
	mediaInfoSHA       = "f7f80620ce6d14f4995f0de6f98e3ef18ad29496db01899571152ee3311229f9"
)

var appVersion = "dev"

const (
	WS_OVERLAPPED       = 0x00000000
	WS_CAPTION          = 0x00C00000
	WS_SYSMENU          = 0x00080000
	WS_THICKFRAME       = 0x00040000
	WS_MINIMIZEBOX      = 0x00020000
	WS_MAXIMIZEBOX      = 0x00010000
	WS_VISIBLE          = 0x10000000
	WS_CHILD            = 0x40000000
	WS_TABSTOP          = 0x00010000
	WS_BORDER           = 0x00800000
	WS_VSCROLL          = 0x00200000
	WS_EX_CLIENTEDGE    = 0x00000200
	WS_EX_CONTROLPARENT = 0x00010000

	ES_LEFT        = 0x0000
	ES_MULTILINE   = 0x0004
	ES_AUTOVSCROLL = 0x0040
	ES_AUTOHSCROLL = 0x0080
	ES_READONLY    = 0x0800

	BS_PUSHBUTTON    = 0x00000000
	BS_DEFPUSHBUTTON = 0x00000001
	BS_AUTOCHECKBOX  = 0x00000003
	BS_GROUPBOX      = 0x00000007

	BM_GETCHECK   = 0x00F0
	BM_SETCHECK   = 0x00F1
	BST_UNCHECKED = 0
	BST_CHECKED   = 1

	CBS_DROPDOWNLIST = 0x0003
	EN_CHANGE        = 0x0300
	SW_SHOW          = 5
	CW_USEDEFAULT    = 0x80000000

	WM_DESTROY   = 0x0002
	WM_CLOSE     = 0x0010
	WM_COMMAND   = 0x0111
	WM_SETFONT   = 0x0030
	WM_DROPFILES = 0x0233
	WM_USER      = 0x0400
	WM_APP       = 0x8000
	WM_SHOWTEXT  = WM_APP + 1

	CB_GETCURSEL    = 0x0147
	CB_RESETCONTENT = 0x014B
	CB_ADDSTRING    = 0x0143
	CB_SETCURSEL    = 0x014E

	PBM_SETPOS     = WM_USER + 2
	PBM_SETRANGE32 = WM_USER + 6

	MB_OK              = 0x00000000
	MB_OKCANCEL        = 0x00000001
	MB_ICONERROR       = 0x00000010
	MB_ICONQUESTION    = 0x00000020
	MB_ICONWARNING     = 0x00000030
	MB_ICONINFORMATION = 0x00000040

	IDOK     = 1
	IDCANCEL = 2

	OFN_FILEMUSTEXIST = 0x00001000
	OFN_PATHMUSTEXIST = 0x00000800
	OFN_EXPLORER      = 0x00080000
	OFN_NOCHANGEDIR   = 0x00000008

	BIF_RETURNONLYFSDIRS = 0x00000001
	BIF_NEWDIALOGSTYLE   = 0x00000040
	ICC_PROGRESS_CLASS   = 0x00000020
	DEFAULT_GUI_FONT     = 17
	IMAGE_ICON           = 1
	LR_LOADFROMFILE      = 0x0010
	LR_DEFAULTSIZE       = 0x0040
	FW_NORMAL            = 400
	FW_SEMIBOLD          = 600
	LOGPIXELSY           = 90
)

const (
	idSourceEdit       = 1001
	idDVDButton        = 1002
	idISOButton        = 1003
	idDriveButton      = 1014
	idOutputEdit       = 1004
	idOutputBtn        = 1005
	idTitleCombo       = 1006
	idScanBtn          = 1007
	idMetaBtn          = 1008
	idRemuxBtn         = 1009
	idDemuxBtn         = 1013
	idCancelBtn        = 1010
	idAboutBtn         = 1011
	idPreserveChapters = 1012
)

type POINT struct{ X, Y int32 }
type MSG struct {
	Hwnd     uintptr
	Message  uint32
	WParam   uintptr
	LParam   uintptr
	Time     uint32
	Pt       POINT
	LPrivate uint32
}
type WNDCLASSEX struct {
	CbSize        uint32
	Style         uint32
	LpfnWndProc   uintptr
	CbClsExtra    int32
	CbWndExtra    int32
	HInstance     uintptr
	HIcon         uintptr
	HCursor       uintptr
	HbrBackground uintptr
	LpszMenuName  *uint16
	LpszClassName *uint16
	HIconSm       uintptr
}
type BROWSEINFO struct {
	HwndOwner      uintptr
	PidlRoot       uintptr
	PszDisplayName *uint16
	LpszTitle      *uint16
	UlFlags        uint32
	Lpfn           uintptr
	LParam         uintptr
	IImage         int32
}
type OPENFILENAME struct {
	LStructSize       uint32
	HwndOwner         uintptr
	HInstance         uintptr
	LpstrFilter       *uint16
	LpstrCustomFilter *uint16
	NMaxCustFilter    uint32
	NFilterIndex      uint32
	LpstrFile         *uint16
	NMaxFile          uint32
	LpstrFileTitle    *uint16
	NMaxFileTitle     uint32
	LpstrInitialDir   *uint16
	LpstrTitle        *uint16
	Flags             uint32
	NFileOffset       uint16
	NFileExtension    uint16
	LpstrDefExt       *uint16
	LCustData         uintptr
	LpfnHook          uintptr
	LpTemplateName    *uint16
	PvReserved        uintptr
	DwReserved        uint32
	FlagsEx           uint32
}
type INITCOMMONCONTROLSEX struct {
	DwSize uint32
	DwICC  uint32
}

type appSettings struct {
	OutputDir        string `json:"output_dir"`
	PreserveChapters bool   `json:"preserve_chapters"`
}
type titleInfo struct {
	Number   int
	Duration time.Duration
}
type ffprobeResult struct {
	Streams []struct {
		Index         int               `json:"index"`
		CodecName     string            `json:"codec_name"`
		CodecType     string            `json:"codec_type"`
		Width         int               `json:"width"`
		Height        int               `json:"height"`
		Channels      int               `json:"channels"`
		ChannelLayout string            `json:"channel_layout"`
		Tags          map[string]string `json:"tags"`
	} `json:"streams"`
}
type ffprobeChapterResult struct {
	Chapters []struct {
		ID        int               `json:"id"`
		StartTime string            `json:"start_time"`
		EndTime   string            `json:"end_time"`
		Tags      map[string]string `json:"tags"`
	} `json:"chapters"`
}

type application struct {
	hwnd             uintptr
	sourceEdit       uintptr
	sourceDVDButton  uintptr
	sourceISOButton  uintptr
	sourceDriveButton uintptr
	outputEdit       uintptr
	outputButton     uintptr
	titleCombo       uintptr
	scanBtn          uintptr
	metaBtn          uintptr
	preserveChapters uintptr
	remuxBtn         uintptr
	demuxBtn         uintptr
	cancelBtn        uintptr
	statusText       uintptr
	progress         uintptr
	bodyFont         uintptr
	headerFont       uintptr
	monoFont         uintptr
	busy             atomic.Bool
	cancelMu         sync.Mutex
	cancel           context.CancelFunc
	titlesMu         sync.RWMutex
	titles           []titleInfo
	titlesSource     string
	logFile          *os.File
	pendingTextMu    sync.Mutex
	pendingTitle     string
	pendingText      string
}

var app application

var (
	user32                            = syscall.NewLazyDLL("user32.dll")
	kernel32                          = syscall.NewLazyDLL("kernel32.dll")
	gdi32                             = syscall.NewLazyDLL("gdi32.dll")
	shell32                           = syscall.NewLazyDLL("shell32.dll")
	comdlg32                          = syscall.NewLazyDLL("comdlg32.dll")
	comctl32                          = syscall.NewLazyDLL("comctl32.dll")
	ole32                             = syscall.NewLazyDLL("ole32.dll")
	procCreateWindowExW               = user32.NewProc("CreateWindowExW")
	procDefWindowProcW                = user32.NewProc("DefWindowProcW")
	procDestroyWindow                 = user32.NewProc("DestroyWindow")
	procDispatchMessageW              = user32.NewProc("DispatchMessageW")
	procEnableWindow                  = user32.NewProc("EnableWindow")
	procGetClientRect                 = user32.NewProc("GetClientRect")
	procGetDC                         = user32.NewProc("GetDC")
	procGetMessageW                   = user32.NewProc("GetMessageW")
	procGetSystemMetrics              = user32.NewProc("GetSystemMetrics")
	procGetWindowTextLengthW          = user32.NewProc("GetWindowTextLengthW")
	procGetWindowTextW                = user32.NewProc("GetWindowTextW")
	procLoadCursorW                   = user32.NewProc("LoadCursorW")
	procLoadIconW                     = user32.NewProc("LoadIconW")
	procLoadImageW                    = user32.NewProc("LoadImageW")
	procMessageBoxW                   = user32.NewProc("MessageBoxW")
	procPostQuitMessage               = user32.NewProc("PostQuitMessage")
	procRegisterClassExW              = user32.NewProc("RegisterClassExW")
	procSendMessageW                  = user32.NewProc("SendMessageW")
	procSetProcessDpiAwarenessContext = user32.NewProc("SetProcessDpiAwarenessContext")
	procSetProcessDPIAware            = user32.NewProc("SetProcessDPIAware")
	procPostMessageW                  = user32.NewProc("PostMessageW")
	procSetWindowTextW                = user32.NewProc("SetWindowTextW")
	procSetWindowPos                  = user32.NewProc("SetWindowPos")
	procShowWindow                    = user32.NewProc("ShowWindow")
	procTranslateMessage              = user32.NewProc("TranslateMessage")
	procUpdateWindow                  = user32.NewProc("UpdateWindow")
	procGetModuleHandleW              = kernel32.NewProc("GetModuleHandleW")
	procMulDiv                        = kernel32.NewProc("MulDiv")
	procCreateFontW                   = gdi32.NewProc("CreateFontW")
	procGetDeviceCaps                 = gdi32.NewProc("GetDeviceCaps")
	procGetStockObject                = gdi32.NewProc("GetStockObject")
	procReleaseDC                     = user32.NewProc("ReleaseDC")
	procSHBrowseForFolderW            = shell32.NewProc("SHBrowseForFolderW")
	procSHGetPathFromIDListW          = shell32.NewProc("SHGetPathFromIDListW")
	procCoTaskMemFree                 = ole32.NewProc("CoTaskMemFree")
	procOleInitialize                 = ole32.NewProc("OleInitialize")
	procOleUninitialize               = ole32.NewProc("OleUninitialize")
	procDragAcceptFiles               = shell32.NewProc("DragAcceptFiles")
	procDragQueryFileW                = shell32.NewProc("DragQueryFileW")
	procDragFinish                    = shell32.NewProc("DragFinish")
	procGetOpenFileNameW              = comdlg32.NewProc("GetOpenFileNameW")
	procInitCommonControlsEx          = comctl32.NewProc("InitCommonControlsEx")
)
