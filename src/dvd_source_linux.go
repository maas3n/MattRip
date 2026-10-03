//go:build linux

package main

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

func linuxOpticalBlockDevice(path string) bool {
	resolved, err := filepath.EvalSymlinks(path)
	if err != nil {
		resolved = path
	}
	st, err := os.Stat(resolved)
	if err != nil || st.Mode()&os.ModeDevice == 0 || st.Mode()&os.ModeCharDevice != 0 {
		return false
	}
	name := filepath.Base(resolved)
	data, err := os.ReadFile(filepath.Join("/sys/class/block", name, "device", "type"))
	if err != nil {
		return strings.HasPrefix(name, "sr")
	}
	return strings.TrimSpace(string(data)) == "5"
}

var linuxOpticalDevice = linuxOpticalBlockDevice

func linuxDVDLabel(device string) string {
	canonical, _ := filepath.EvalSymlinks(device)
	if canonical == "" {
		canonical = device
	}
	entries, _ := os.ReadDir("/dev/disk/by-label")
	for _, entry := range entries {
		candidate := filepath.Join("/dev/disk/by-label", entry.Name())
		target, err := filepath.EvalSymlinks(candidate)
		if err == nil && target == canonical {
			return fmt.Sprintf("%s (%s)", entry.Name(), device)
		}
	}
	return "DVD Drive " + device
}

func resolvePhysicalDVDDrive(raw string) (dvdDrive, bool, error) {
	p := strings.TrimSpace(strings.Trim(raw, "\""))
	if p == "" || !strings.HasPrefix(p, "/dev/") {
		return dvdDrive{}, false, nil
	}
	resolved, err := filepath.EvalSymlinks(p)
	if err != nil {
		resolved = p
	}
	if !linuxOpticalDevice(resolved) {
		return dvdDrive{}, false, nil
	}
	return dvdDrive{Input: resolved, Label: linuxDVDLabel(resolved), BaseName: "DVD-" + filepath.Base(resolved)}, true, nil
}

func platformDVDDrives() ([]dvdDrive, error) {
	entries, err := os.ReadDir("/sys/class/block")
	if err != nil {
		return nil, nil
	}
	var drives []dvdDrive
	for _, entry := range entries {
		device := filepath.Join("/dev", entry.Name())
		if linuxOpticalDevice(device) {
			drives = append(drives, dvdDrive{Input: device, Label: linuxDVDLabel(device), BaseName: "DVD-" + filepath.Base(device)})
		}
	}
	sort.Slice(drives, func(i, j int) bool { return drives[i].Input < drives[j].Input })
	return drives, nil
}
