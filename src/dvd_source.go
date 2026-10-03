//go:build windows || linux

package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

type dvdSourceKind uint8

const (
	dvdSourceDirectory dvdSourceKind = iota + 1
	dvdSourceISO
	dvdSourcePhysicalDrive
)

type dvdDrive struct {
	Input string
	Label string
}

type dvdSource struct {
	Kind     dvdSourceKind
	Input    string
	Label    string
	BaseName string
}

func resolveDVDSource(raw string) (dvdSource, error) {
	raw = strings.TrimSpace(strings.Trim(raw, """))
	if raw == "" {
		return dvdSource{}, errors.New("choose a DVD folder, VIDEO_TS folder, ISO file, or physical DVD drive")
	}
	if drive, ok, err := resolvePhysicalDVDDrive(raw); err != nil {
		return dvdSource{}, err
	} else if ok {
		base := strings.TrimSpace(drive.Label)
		if base == "" {
			base = "DVD"
		}
		return dvdSource{Kind: dvdSourcePhysicalDrive, Input: drive.Input, Label: drive.Label, BaseName: base}, nil
	}

	p := filepath.Clean(raw)
	if p == "." || p == "" {
		return dvdSource{}, errors.New("choose a DVD folder, VIDEO_TS folder, ISO file, or physical DVD drive")
	}
	info, err := os.Stat(p)
	if err != nil {
		return dvdSource{}, fmt.Errorf("source does not exist: %s", p)
	}
	if !info.IsDir() {
		if !strings.EqualFold(filepath.Ext(p), ".iso") {
			return dvdSource{}, errors.New("source file must be a DVD ISO (.iso)")
		}
		base := strings.TrimSuffix(filepath.Base(p), filepath.Ext(p))
		return dvdSource{Kind: dvdSourceISO, Input: p, Label: filepath.Base(p), BaseName: base}, nil
	}

	if strings.EqualFold(filepath.Base(p), "VIDEO_TS") && dvdFileExistsFold(p, "VIDEO_TS.IFO") {
		root := filepath.Dir(p)
		return dvdSource{Kind: dvdSourceDirectory, Input: root, Label: filepath.Base(root), BaseName: filepath.Base(root)}, nil
	}
	if child := dvdFindChildDirFold(p, "VIDEO_TS"); child != "" && dvdFileExistsFold(child, "VIDEO_TS.IFO") {
		return dvdSource{Kind: dvdSourceDirectory, Input: p, Label: filepath.Base(p), BaseName: filepath.Base(p)}, nil
	}
	if dvdFileExistsFold(p, "VIDEO_TS.IFO") {
		root := filepath.Dir(p)
		return dvdSource{Kind: dvdSourceDirectory, Input: root, Label: filepath.Base(root), BaseName: filepath.Base(root)}, nil
	}
	return dvdSource{}, errors.New("the selected folder does not contain a VIDEO_TS DVD structure")
}

func listPhysicalDVDDrives() ([]dvdDrive, error) {
	return platformDVDDrives()
}

func dvdSourceBaseName(path string) string {
	if src, err := resolveDVDSource(path); err == nil {
		if base := strings.TrimSpace(src.BaseName); base != "" {
			return base
		}
	}
	base := filepath.Base(filepath.Clean(path))
	if strings.EqualFold(filepath.Ext(base), ".iso") {
		base = strings.TrimSuffix(base, filepath.Ext(base))
	}
	if strings.EqualFold(base, "VIDEO_TS") {
		base = filepath.Base(filepath.Dir(filepath.Clean(path)))
	}
	if base == "." || base == string(os.PathSeparator) {
		return "DVD"
	}
	return base
}

func dvdSourceDisplayName(path string) string {
	if src, err := resolveDVDSource(path); err == nil {
		if src.Kind == dvdSourcePhysicalDrive && src.Label != "" {
			return src.Label
		}
		if src.Label != "" {
			return src.Label
		}
	}
	base := filepath.Base(filepath.Clean(path))
	if base == "" || base == "." || base == string(os.PathSeparator) {
		return path
	}
	return base
}

func isPhysicalDVDSource(path string) bool {
	src, err := resolveDVDSource(path)
	return err == nil && src.Kind == dvdSourcePhysicalDrive
}

func dvdSourceMediaInfoTarget(path string) string {
	src, err := resolveDVDSource(path)
	if err != nil || src.Kind == dvdSourcePhysicalDrive {
		return ""
	}
	if src.Kind == dvdSourceISO {
		return src.Input
	}
	videoTS := dvdFindChildDirFold(src.Input, "VIDEO_TS")
	if videoTS == "" && strings.EqualFold(filepath.Base(src.Input), "VIDEO_TS") {
		videoTS = src.Input
	}
	if videoTS == "" && dvdFileExistsFold(src.Input, "VIDEO_TS.IFO") {
		videoTS = src.Input
	}
	if videoTS != "" {
		if name := dvdFindFileFold(videoTS, "VIDEO_TS.IFO"); name != "" {
			return filepath.Join(videoTS, name)
		}
	}
	return ""
}

func dvdFindChildDirFold(dir, name string) string {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return ""
	}
	for _, entry := range entries {
		if entry.IsDir() && strings.EqualFold(entry.Name(), name) {
			return filepath.Join(dir, entry.Name())
		}
	}
	return ""
}

func dvdFindFileFold(dir, name string) string {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return ""
	}
	for _, entry := range entries {
		if !entry.IsDir() && strings.EqualFold(entry.Name(), name) {
			return entry.Name()
		}
	}
	return ""
}

func dvdFileExistsFold(dir, name string) bool {
	return dvdFindFileFold(dir, name) != ""
}
