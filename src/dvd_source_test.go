//go:build windows || linux

package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDVDSourceDirectoryAndVideoTSNormalizeToDiscRoot(t *testing.T) {
	root := t.TempDir()
	movie := filepath.Join(root, "Movie")
	videoTS := filepath.Join(movie, "VIDEO_TS")
	if err := os.MkdirAll(videoTS, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(videoTS, "VIDEO_TS.IFO"), []byte("fixture"), 0644); err != nil {
		t.Fatal(err)
	}

	for _, input := range []string{movie, videoTS} {
		source, err := resolveDVDSource(input)
		if err != nil {
			t.Fatalf("resolve %q: %v", input, err)
		}
		if source.Kind != dvdSourceDirectory {
			t.Fatalf("resolve %q kind=%v want directory", input, source.Kind)
		}
		if filepath.Clean(source.Input) != filepath.Clean(movie) {
			t.Fatalf("resolve %q input=%q want %q", input, source.Input, movie)
		}
		if source.BaseName != "Movie" {
			t.Fatalf("resolve %q basename=%q want Movie", input, source.BaseName)
		}
	}
	if got := dvdSourceMediaInfoTarget(movie); !strings.EqualFold(filepath.Base(got), "VIDEO_TS.IFO") {
		t.Fatalf("MediaInfo target=%q", got)
	}
}

func TestDVDSourceISO(t *testing.T) {
	iso := filepath.Join(t.TempDir(), "Feature Disc.iso")
	if err := os.WriteFile(iso, []byte("not-a-real-iso"), 0644); err != nil {
		t.Fatal(err)
	}
	source, err := resolveDVDSource(iso)
	if err != nil {
		t.Fatal(err)
	}
	if source.Kind != dvdSourceISO || source.Input != iso || source.BaseName != "Feature Disc" {
		t.Fatalf("unexpected ISO source: %#v", source)
	}
	if got := dvdSourceMediaInfoTarget(iso); got != iso {
		t.Fatalf("MediaInfo target=%q want %q", got, iso)
	}
}

func TestDVDSourceRejectsOrdinaryFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "movie.mkv")
	if err := os.WriteFile(path, []byte("fixture"), 0644); err != nil {
		t.Fatal(err)
	}
	if _, err := resolveDVDSource(path); err == nil {
		t.Fatal("expected ordinary media file to be rejected as DVDSource")
	}
}

func TestDVDSourceBaseNameFallsBackSafely(t *testing.T) {
	if got := dvdSourceBaseName("Example.iso"); got != "Example" {
		t.Fatalf("got %q", got)
	}
}
