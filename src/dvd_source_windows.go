//go:build windows

package main

import (
	"fmt"
	"strings"
	"syscall"
	"unicode"
	"unsafe"
)

const driveCDROM = 5

var (
	dvdKernel32             = syscall.NewLazyDLL("kernel32.dll")
	procDVDGetDriveTypeW    = dvdKernel32.NewProc("GetDriveTypeW")
	procDVDGetVolumeInfoW   = dvdKernel32.NewProc("GetVolumeInformationW")
)

func windowsDriveLetter(raw string) (byte, bool) {
	s := strings.TrimSpace(strings.Trim(raw, """))
	if len(s) < 2 || s[1] != ':' {
		return 0, false
	}
	r := rune(s[0])
	if !unicode.IsLetter(r) {
		return 0, false
	}
	for _, c := range s[2:] {
		if c != '\\' && c != '/' {
			return 0, false
		}
	}
	return byte(unicode.ToUpper(r)), true
}

func windowsDriveRoot(letter byte) string {
	return fmt.Sprintf("%c:\\", letter)
}

func windowsDriveInput(letter byte) string {
	return fmt.Sprintf("%c:", letter)
}

func windowsDVDDriveLabel(letter byte) string {
	root := windowsDriveRoot(letter)
	rootPtr, _ := syscall.UTF16PtrFromString(root)
	volume := make([]uint16, 256)
	r, _, _ := procDVDGetVolumeInfoW.Call(
		uintptr(unsafe.Pointer(rootPtr)),
		uintptr(unsafe.Pointer(&volume[0])),
		uintptr(len(volume)),
		0, 0, 0, 0, 0,
	)
	name := ""
	if r != 0 {
		name = syscall.UTF16ToString(volume)
	}
	if strings.TrimSpace(name) == "" {
		return fmt.Sprintf("DVD Drive %c:", letter)
	}
	return fmt.Sprintf("%s (%c:)", name, letter)
}

func resolvePhysicalDVDDrive(raw string) (dvdDrive, bool, error) {
	letter, ok := windowsDriveLetter(raw)
	if !ok {
		return dvdDrive{}, false, nil
	}
	rootPtr, _ := syscall.UTF16PtrFromString(windowsDriveRoot(letter))
	t, _, _ := procDVDGetDriveTypeW.Call(uintptr(unsafe.Pointer(rootPtr)))
	if t != driveCDROM {
		return dvdDrive{}, false, nil
	}
	return dvdDrive{Input: windowsDriveInput(letter), Label: windowsDVDDriveLabel(letter)}, true, nil
}

func platformDVDDrives() ([]dvdDrive, error) {
	var drives []dvdDrive
	for letter := byte('A'); letter <= 'Z'; letter++ {
		rootPtr, _ := syscall.UTF16PtrFromString(windowsDriveRoot(letter))
		t, _, _ := procDVDGetDriveTypeW.Call(uintptr(unsafe.Pointer(rootPtr)))
		if t == driveCDROM {
			drives = append(drives, dvdDrive{Input: windowsDriveInput(letter), Label: windowsDVDDriveLabel(letter)})
		}
	}
	return drives, nil
}
