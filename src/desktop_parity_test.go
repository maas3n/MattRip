//go:build linux || windows

package main

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestDesktopCLIArguments(t *testing.T) {
	got, err := splitCLICommand(`mattrip-cli remux "C:\Movie Files\Alien.iso" --streams 0,2 --no-chapters`)
	if err != nil || len(got) != 5 || got[1] != `C:\Movie Files\Alien.iso` {
		t.Fatalf("%v %v", got, err)
	}
	if _, err = splitCLICommand(`remux "unterminated`); err == nil {
		t.Fatal("accepted unclosed quote")
	}
	indexes, err := parseCLIStreams("0,2,4")
	if err != nil || !reflect.DeepEqual(indexes, []int{0, 2, 4}) {
		t.Fatal(indexes, err)
	}
	for _, bad := range []string{"-1", "1,", "all", "0,x"} {
		if _, err := parseCLIStreams(bad); err == nil {
			t.Fatalf("accepted %q", bad)
		}
	}
	if got := cliFlagsFirst([]string{"disc.iso", "--title", "2", "--no-chapters"}); !reflect.DeepEqual(got, []string{"--title", "2", "--no-chapters", "--", "disc.iso"}) {
		t.Fatal(got)
	}
}
func TestDesktopBatchISOSiblingAndNoOverwrite(t *testing.T) {
	root := t.TempDir()
	for _, n := range []string{"Alien.ISO", "Other.iso"} {
		if err := os.WriteFile(filepath.Join(root, n), []byte("fixture"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	existing := filepath.Join(root, "Alien.mkv")
	os.WriteFile(existing, []byte("original"), 0600)
	deps := batchDeps{
		ensureTools: func(context.Context, batchProgressFunc) (toolPaths, error) { return toolPaths{}, nil },
		discoverDVDTitlesViaDVDVideo: func(context.Context, string, toolPaths, batchProgressFunc) ([]titleInfo, error) {
			return []titleInfo{{Number: 2, Duration: time.Minute}}, nil
		},
		remuxTitle: func(_ context.Context, src string, _ titleInfo, out string, _ toolPaths, _ batchProgressFunc) (string, error) {
			if out != root {
				t.Fatalf("ISO output %s != parent %s", out, root)
			}
			p := filepath.Join(out, "completed.mkv")
			return p, os.WriteFile(p, []byte("mkv"), 0600)
		},
	}
	result, err := runBatchWithDeps(context.Background(), batchOptions{InputRoot: root}, nil, deps)
	if err == nil || result.Completed != 1 || len(result.Failures) != 1 || result.Outputs[0] != filepath.Join(root, "Other.mkv") {
		t.Fatalf("%+v %v", result, err)
	}
	data, _ := os.ReadFile(existing)
	if string(data) != "original" {
		t.Fatal("overwrote existing output")
	}
	movies, _ := discoverBatchMovies(root)
	override := t.TempDir()
	if batchOutputPath(movies[0], override) != filepath.Join(override, "Alien.mkv") {
		t.Fatal("output override ignored")
	}
}
func TestMergerKeepsSelectedDVDTitle(t *testing.T) {
	streams := []mergerStream{{"disc.iso", trackOption{DVDTitle: 2, Index: 0, Kind: "video"}}, {"disc.iso", trackOption{DVDTitle: 2, Index: -1, Kind: "chapters"}}, {"audio.ac3", trackOption{Index: 0, Kind: "audio"}}}
	args, err := mergerArgs(streams, "", "out.mkv")
	if err != nil {
		t.Fatal(err)
	}
	s := strings.Join(args, " ")
	for _, want := range []string{"-f dvdvideo -title 2 -i disc.iso", "-map 0:0", "-map 1:0", "-map_chapters 0", "-c copy"} {
		if !strings.Contains(s, want) {
			t.Fatalf("missing %s in %s", want, s)
		}
	}
	streams[1].Track.DVDTitle = 1
	if _, err = mergerArgs(streams, "", "out.mkv"); err == nil {
		t.Fatal("accepted inconsistent DVD titles")
	}
}

// CI supplies an authored two-title ISO and the exact bundled DVD-capable tools.
func TestRealDVDISOParity(t *testing.T) {
	iso := os.Getenv("MATTRIP_TEST_DVD_ISO")
	if iso == "" {
		t.Skip("set MATTRIP_TEST_DVD_ISO to an authored two-title DVD ISO")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	tools, err := desktopCLITools(ctx, false)
	if err != nil {
		t.Fatal(err)
	}
	streams, err := probeMergerFile(ctx, tools.ffprobe, iso, "all")
	if err != nil {
		t.Fatal(err)
	}
	var selected []mergerStream
	for _, s := range streams {
		if s.Track.DVDTitle != 2 {
			t.Fatalf("expected longest DVD title 2: %+v", s)
		}
		if s.Track.Kind == "video" || s.Track.Kind == "chapters" {
			selected = append(selected, s)
		}
	}
	output := filepath.Join(t.TempDir(), "merged.mkv")
	if err = muxMerger(ctx, tools, selected, "", output); err != nil {
		t.Fatal(err)
	}
	got, err := probeMergerFile(ctx, tools.ffprobe, output, "all")
	if err != nil {
		t.Fatal(err)
	}
	videos := 0
	chapters := 0
	for _, s := range got {
		switch s.Track.Kind {
		case "video":
			videos++
		case "chapters":
			chapters++
		default:
			t.Fatalf("unselected stream in merger result: %+v", s)
		}
	}
	if videos != 1 || chapters != 1 {
		t.Fatalf("merger streams: %+v", got)
	}
	sources := []string{iso}
	if folder := os.Getenv("MATTRIP_TEST_DVD_FOLDER"); folder != "" {
		sources = append(sources, folder)
	}
	for _, source := range sources {
		for _, format := range []string{"mpeg2", "vob"} {
			directory, e := demuxTab(ctx, tools, source, 2, t.TempDir(), nil, true, format)
			if e != nil {
				t.Fatalf("DVD demux %s %s: %v", source, format, e)
			}
			files, e := os.ReadDir(directory)
			if e != nil || len(files) < 3 {
				t.Fatalf("DVD demux incomplete: %v %v", files, e)
			}
			if _, e := os.Stat(filepath.Join(directory, "Chapters.txt")); e != nil {
				t.Fatal(e)
			}
		}
	}
	if err = muxMerger(ctx, tools, selected, "", output); err == nil {
		t.Fatal("merger overwrote output")
	}
}

func TestNestedBatchISOMatchesAndroid(t *testing.T) {
	root := t.TempDir()
	folder := filepath.Join(root, "Movie")
	if err := os.Mkdir(folder, 0700); err != nil {
		t.Fatal(err)
	}
	iso := filepath.Join(folder, "Alien.iso")
	if err := os.WriteFile(iso, []byte("fixture"), 0600); err != nil {
		t.Fatal(err)
	}
	movies, err := discoverBatchMovies(root)
	if err != nil || len(movies) != 1 {
		t.Fatal(movies, err)
	}
	if movies[0].Source != iso || batchOutputPath(movies[0], "") != filepath.Join(folder, "Alien.mkv") {
		t.Fatal(movies)
	}
}
