//go:build windows

package main

import (
	"os"
	"path/filepath"
	"strings"
)

func resolveAppDataRoot() string {
	exePath, _ := os.Executable()
	home, _ := os.UserHomeDir()
	return resolveAppDataRootFor(exePath, strings.TrimSpace(os.Getenv("LOCALAPPDATA")), home)
}

func resolveAppDataRootFor(exePath, localAppData, home string) string {
	if strings.TrimSpace(exePath) != "" {
		portable := filepath.Join(filepath.Dir(exePath), "MattRipData")
		if st, err := os.Stat(portable); err == nil && st.IsDir() {
			_ = os.MkdirAll(portable, 0755)
			return portable
		}
	}

	root := strings.TrimSpace(localAppData)
	if root == "" && strings.TrimSpace(home) != "" {
		root = filepath.Join(home, "AppData", "Local")
	}
	p := filepath.Join(root, "MattRip")
	_ = os.MkdirAll(p, 0755)
	return p
}
