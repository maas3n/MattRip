//go:build linux

package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// configureDVDLibrarySearch keeps MattRip's private libdvdcss scoped to child
// media tools. libdvdread loads libdvdcss dynamically, so FFmpeg itself does not
// need to be rebuilt or linked against it.
func configureDVDLibrarySearch(cmd *exec.Cmd, toolPath string) {
	if cmd == nil {
		return
	}
	var dirs []string
	add := func(dir string) {
		if dir == "" || !fileExists(filepath.Join(dir, "libdvdcss.so.2")) {
			return
		}
		for _, existing := range dirs {
			if existing == dir {
				return
			}
		}
		dirs = append(dirs, dir)
	}
	add(filepath.Dir(toolPath))
	if exe, err := os.Executable(); err == nil {
		add(filepath.Dir(exe))
	}
	if len(dirs) == 0 {
		return
	}

	env := cmd.Env
	if env == nil {
		env = os.Environ()
	}
	old := ""
	for _, item := range env {
		if strings.HasPrefix(item, "LD_LIBRARY_PATH=") {
			old = strings.TrimPrefix(item, "LD_LIBRARY_PATH=")
			break
		}
	}
	value := strings.Join(dirs, string(os.PathListSeparator))
	if old != "" {
		value += string(os.PathListSeparator) + old
	}
	prefix := "LD_LIBRARY_PATH="
	out := make([]string, 0, len(env)+1)
	for _, item := range env {
		if !strings.HasPrefix(item, prefix) {
			out = append(out, item)
		}
	}
	cmd.Env = append(out, prefix+value)
}
