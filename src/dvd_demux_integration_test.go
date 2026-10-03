//go:build windows || linux

package main

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// The fixture is authored with make-dvd-demux-fixture.py, never parsed here.
func TestAuthoredDVDTabDemux(t *testing.T) {
	folder := os.Getenv("MATTRIP_TEST_DEMUX_DVD")
	if folder == "" {
		t.Skip("authored subtitled DVD fixture not supplied")
	}
	ffmpeg, err := exec.LookPath("ffmpeg")
	if err != nil {
		t.Fatal(err)
	}
	ffprobe, err := exec.LookPath("ffprobe")
	if err != nil {
		t.Fatal(err)
	}
	tools := toolPaths{ffmpeg: ffmpeg, ffprobe: ffprobe}
	sources := []string{folder}
	if iso := os.Getenv("MATTRIP_TEST_DEMUX_ISO"); iso != "" {
		sources = append(sources, iso)
	}
	for _, source := range sources {
		for _, format := range []string{"mpeg2", "vob"} {
			t.Run(filepath.Base(source)+"/"+format, func(t *testing.T) {
				output, err := demuxTab(context.Background(), tools, source, 1, t.TempDir(), nil, true, format)
				if err != nil {
					t.Fatal(err)
				}
				ext := "mpeg2"
				if format == "vob" {
					ext = "VOB"
				}
				for _, name := range []string{"track-00." + ext, "track-01.ac3", "track-02.sub", "track-02.idx", "Chapters.txt"} {
					st, err := os.Stat(filepath.Join(output, name))
					if err != nil || st.Size() == 0 {
						t.Fatalf("Missing DVD export %s: %v", name, err)
					}
				}
				idx, _ := os.ReadFile(filepath.Join(output, "track-02.idx"))
				if !strings.Contains(string(idx), "size: 720x576") || !strings.Contains(string(idx), "palette:") || strings.Count(string(idx), "timestamp:") != 2 {
					t.Fatalf("Invalid DVD subtitle index: %s", idx)
				}
				chapters, _ := os.ReadFile(filepath.Join(output, "Chapters.txt"))
				if !strings.Contains(string(chapters), "CHAPTER02=") {
					t.Fatal("Missing second chapter")
				}
				if b, err := exec.Command(ffprobe, "-v", "error", filepath.Join(output, "track-02.idx")).CombinedOutput(); err != nil {
					t.Fatalf("Unreadable subtitle export: %v %s", err, b)
				}
			})
		}
	}
}
