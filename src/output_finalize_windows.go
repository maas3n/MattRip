//go:build windows

package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"syscall"
	"unsafe"
)

var procMattRipMoveFileExW = syscall.NewLazyDLL("kernel32.dll").NewProc("MoveFileExW")

const mattMuxMoveFileWriteThrough = 0x00000008

func reservePartialOutput(final string) (string, error) {
	pattern := ".mattrip-" + filepath.Base(final) + ".*.partial.mkv"
	f, err := os.CreateTemp(filepath.Dir(final), pattern)
	if err != nil {
		return "", fmt.Errorf("create unique temporary output: %w", err)
	}
	name := f.Name()
	if err := f.Close(); err != nil {
		_ = os.Remove(name)
		return "", fmt.Errorf("close temporary output reservation: %w", err)
	}
	return name, nil
}

func validateAndSyncOutput(path string) error {
	st, err := os.Stat(path)
	if err != nil {
		return fmt.Errorf("FFmpeg finished but the partial MKV was not created: %w", err)
	}
	if !st.Mode().IsRegular() {
		return fmt.Errorf("FFmpeg output is not a regular file: %s", path)
	}
	if st.Size() == 0 {
		return errors.New("FFmpeg finished successfully but produced an empty MKV")
	}

	f, err := os.OpenFile(path, os.O_RDWR, 0)
	if err != nil {
		return fmt.Errorf("open completed MKV for final sync: %w", err)
	}
	if err := f.Sync(); err != nil {
		_ = f.Close()
		return fmt.Errorf("sync completed MKV: %w", err)
	}
	if err := f.Close(); err != nil {
		return fmt.Errorf("close completed MKV: %w", err)
	}
	return nil
}

func finalizeRemuxOutput(partial, final string) error {
	if err := validateAndSyncOutput(partial); err != nil {
		return err
	}
	return commitOutputNoReplace(partial, final)
}

func commitOutputNoReplace(partial, final string) error {
	from, err := syscall.UTF16PtrFromString(partial)
	if err != nil {
		return fmt.Errorf("encode temporary output path: %w", err)
	}
	to, err := syscall.UTF16PtrFromString(final)
	if err != nil {
		return fmt.Errorf("encode final output path: %w", err)
	}

	r1, _, callErr := procMattRipMoveFileExW.Call(
		uintptr(unsafe.Pointer(from)),
		uintptr(unsafe.Pointer(to)),
		uintptr(mattMuxMoveFileWriteThrough),
	)
	if r1 != 0 {
		return nil
	}
	if callErr == syscall.Errno(0) {
		callErr = syscall.EINVAL
	}
	if errno, ok := callErr.(syscall.Errno); ok && (errno == 80 || errno == 183) {
		return fmt.Errorf("output already exists: %s", final)
	}
	return fmt.Errorf("remux finished but atomic no-overwrite commit failed: %w", callErr)
}
