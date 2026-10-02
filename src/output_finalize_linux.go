//go:build linux

package main

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"golang.org/x/sys/unix"
)

var renameOutputNoReplace = func(partial, final string) error {
	return unix.Renameat2(unix.AT_FDCWD, partial, unix.AT_FDCWD, final, unix.RENAME_NOREPLACE)
}

var linkOutputNoReplace = os.Link

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
	if err := commitOutputNoReplace(partial, final); err != nil {
		return err
	}
	return syncDirectory(filepath.Dir(final))
}

func commitOutputNoReplace(partial, final string) error {
	if err := renameOutputNoReplace(partial, final); err == nil {
		return nil
	} else if errors.Is(err, unix.EEXIST) {
		return fmt.Errorf("output already exists: %s", final)
	} else if !noReplacePrimitiveUnsupported(err) {
		return fmt.Errorf("remux finished but atomic no-overwrite rename failed: %w", err)
	}

	if err := linkOutputNoReplace(partial, final); err == nil {
		_ = os.Remove(partial)
		return nil
	} else if errors.Is(err, os.ErrExist) {
		return fmt.Errorf("output already exists: %s", final)
	} else if !noReplacePrimitiveUnsupported(err) {
		return fmt.Errorf("remux finished but no-overwrite hard-link commit failed: %w", err)
	}

	// Some removable/provider-backed filesystems support neither renameat2
	// RENAME_NOREPLACE nor hard links. O_EXCL still preserves the no-overwrite
	// guarantee. If copying fails, the incomplete destination is removed and
	// the caller keeps the completed partial for recovery.
	if err := copyOutputExclusive(partial, final); err != nil {
		return fmt.Errorf("remux finished but no-overwrite copy commit failed: %w", err)
	}
	_ = os.Remove(partial)
	return nil
}

func noReplacePrimitiveUnsupported(err error) bool {
	return errors.Is(err, unix.ENOSYS) ||
		errors.Is(err, unix.EINVAL) ||
		errors.Is(err, unix.EOPNOTSUPP) ||
		errors.Is(err, unix.EPERM) ||
		errors.Is(err, unix.EXDEV)
}

func copyOutputExclusive(src, dst string) (retErr error) {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()

	out, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		if errors.Is(err, os.ErrExist) {
			return fmt.Errorf("output already exists: %s", dst)
		}
		return err
	}
	keep := false
	defer func() {
		_ = out.Close()
		if !keep {
			_ = os.Remove(dst)
		}
	}()

	if _, err := io.Copy(out, in); err != nil {
		return err
	}
	if err := out.Sync(); err != nil {
		return err
	}
	if err := out.Close(); err != nil {
		return err
	}
	keep = true
	return nil
}

func syncDirectory(dir string) error {
	f, err := os.Open(dir)
	if err != nil {
		return fmt.Errorf("open output directory for sync: %w", err)
	}
	if err := f.Sync(); err != nil {
		_ = f.Close()
		return fmt.Errorf("sync output directory: %w", err)
	}
	if err := f.Close(); err != nil {
		return fmt.Errorf("close output directory after sync: %w", err)
	}
	return nil
}
