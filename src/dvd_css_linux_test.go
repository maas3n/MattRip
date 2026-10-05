//go:build linux

package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestConfigureDVDLibrarySearchUsesToolAdjacentRuntime(t *testing.T) {
	dir := t.TempDir()
	tool := filepath.Join(dir, "ffmpeg")
	if err := os.WriteFile(tool, []byte("#!/bin/sh\n"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "libdvdcss.so.2"), []byte("fixture"), 0644); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(tool)
	cmd.Env = []string{"PATH=/usr/bin", "LD_LIBRARY_PATH=/system/lib"}
	configureDVDLibrarySearch(cmd, tool)

	var got string
	for _, item := range cmd.Env {
		if strings.HasPrefix(item, "LD_LIBRARY_PATH=") {
			got = strings.TrimPrefix(item, "LD_LIBRARY_PATH=")
		}
	}
	parts := strings.Split(got, string(os.PathListSeparator))
	if len(parts) < 2 || parts[0] != dir || parts[1] != "/system/lib" {
		t.Fatalf("LD_LIBRARY_PATH=%q, want tool dir first and previous value preserved", got)
	}
}

func TestConfigureDVDLibrarySearchDoesNotInventRuntimePath(t *testing.T) {
	tool := filepath.Join(t.TempDir(), "ffmpeg")
	cmd := exec.Command(tool)
	configureDVDLibrarySearch(cmd, tool)
	if cmd.Env != nil {
		t.Fatalf("command environment changed without a bundled libdvdcss: %#v", cmd.Env)
	}
}
