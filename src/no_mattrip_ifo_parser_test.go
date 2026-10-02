package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestNoMattRipWrittenIFOParser(t *testing.T) {
	banned := []string{
		"ReadDVDChapters(",
		"func readIFO(",
		"func mapGlobalTitle(",
		"func parseVTSPTTTable(",
		"func parsePGC(",
		"func sectorTable(",
		"ErrNativeDVDChaptersUnsupported",
		"native DVD IFO parser",
	}
	entries, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}
	for _, path := range entries {
		if filepath.Base(path) == "no_mattrip_ifo_parser_test.go" {
			continue
		}
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		text := string(data)
		for _, needle := range banned {
			if strings.Contains(text, needle) {
				t.Fatalf("MattRip-written IFO parser pattern %q remains in %s", needle, path)
			}
		}
	}
	if _, err := os.Stat("dvdchapters.go"); !os.IsNotExist(err) {
		t.Fatalf("legacy MattRip IFO parser file dvdchapters.go must not exist")
	}
}
