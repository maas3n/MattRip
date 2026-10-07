//go:build windows || linux

package main

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestAdvancedMergerDemuxInputPolicy(t *testing.T) {
	dvd := appendMergerDemuxInput([]string{"-y"}, "disc.iso", 2, true)
	joined := strings.Join(dvd, " ")
	for _, want := range []string{"-analyzeduration 100M", "-probesize 100M", "-fflags +genpts", "-f dvdvideo", "-title 2"} {
		if !strings.Contains(joined, want) {
			t.Fatalf("DVD demux input missing %s: %q", want, dvd)
		}
	}
	mkv := appendMergerDemuxInput([]string{"-y"}, "movie.mkv", 0, true)
	if got := strings.Join(mkv, " "); got != "-y -copyts -i movie.mkv" {
		t.Fatalf("MKV demux input = %q", got)
	}
	raw := appendMergerDemuxInput([]string{"-y"}, "video.m2v", 0, true)
	rawJoined := strings.Join(raw, " ")
	if !strings.Contains(rawJoined, "-fflags +genpts") || strings.Contains(rawJoined, "-analyzeduration") || strings.Contains(rawJoined, "-probesize") {
		t.Fatalf("raw demux input policy = %q", raw)
	}
}

// Both desktop UIs use this shared input builder. Preserve the original DVD
// source/title for probing and extraction rather than introducing an MKV stage.
func TestAdvancedMergerDemuxOpensOriginalDVDSource(t *testing.T) {
	for _, source := range []string{"disc.iso", "/movies/Film/VIDEO_TS", "/dev/sr0", "D:"} {
		for _, copyTS := range []bool{false, true} {
			args := appendMergerDemuxInput(nil, source, 3, copyTS)
			if len(args) < 2 || args[len(args)-2] != "-i" || args[len(args)-1] != source {
				t.Fatalf("demux must open original source %q: %q", source, args)
			}
			joined := strings.Join(args, " ")
			if strings.Contains(joined, ".mkv") || !strings.Contains(joined, "-f dvdvideo -title 3") {
				t.Fatalf("demux must use dvdvideo directly: %q", args)
			}
		}
	}
}

func TestAdvancedMergerDemuxSelectedStreams(t *testing.T) {
	ffmpeg, err := exec.LookPath("ffmpeg")
	if err != nil {
		t.Skip("ffmpeg required")
	}
	ffprobe, err := exec.LookPath("ffprobe")
	if err != nil {
		t.Skip("ffprobe required")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	dir := t.TempDir()
	sub := filepath.Join(dir, "captions.srt")
	chap := filepath.Join(dir, "chapters.txt")
	source := filepath.Join(dir, "mixed.mkv")
	if err = os.WriteFile(sub, []byte("1\n00:00:00,000 --> 00:00:00,900\nHello\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(chap, []byte(";FFMETADATA1\n[CHAPTER]\nTIMEBASE=1/1000\nSTART=0\nEND=1000\ntitle=Opening\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err = runMergerCommand(ctx, ffmpeg,
		"-v", "error",
		"-f", "lavfi", "-i", "color=size=32x32:rate=25:duration=1",
		"-f", "lavfi", "-i", "sine=duration=1",
		"-i", sub, "-f", "ffmetadata", "-i", chap,
		"-map", "0:v", "-map", "1:a", "-map", "2:s", "-map_chapters", "3",
		"-c:v", "mpeg4", "-c:a", "ac3", "-c:s", "srt", source,
	); err != nil {
		t.Fatal(err)
	}
	all, err := probeMergerFile(ctx, ffprobe, source, "all")
	if err != nil {
		t.Fatal(err)
	}
	var selected []mergerStream
	for _, row := range all {
		if row.Track.Kind == "video" || row.Track.Kind == "audio" || row.Track.Kind == "chapters" {
			selected = append(selected, row)
		}
	}
	outRoot := filepath.Join(dir, "out")
	if err = os.Mkdir(outRoot, 0755); err != nil {
		t.Fatal(err)
	}
	final, err := demuxMerger(ctx, toolPaths{ffmpeg: ffmpeg, ffprobe: ffprobe}, selected, outRoot, "mpeg2")
	if err != nil {
		t.Fatal(err)
	}
	entries, err := os.ReadDir(final)
	if err != nil {
		t.Fatal(err)
	}
	var video, audio, chapters bool
	for _, entry := range entries {
		name := entry.Name()
		if strings.HasSuffix(name, ".m4v") {
			video = true
		}
		if strings.HasSuffix(name, ".ac3") {
			audio = true
		}
		if strings.HasSuffix(name, "-Chapters.txt") {
			chapters = true
			data, readErr := os.ReadFile(filepath.Join(final, name))
			if readErr != nil || !strings.Contains(string(data), "CHAPTER01NAME=Opening") {
				t.Fatalf("chapter export = %q, %v", data, readErr)
			}
		}
		if strings.HasSuffix(name, ".srt") {
			t.Fatalf("unchecked subtitle was demuxed: %s", name)
		}
		info, statErr := entry.Info()
		if statErr != nil || info.Size() == 0 {
			t.Fatalf("empty demux output %s: %v", name, statErr)
		}
	}
	if !video || !audio || !chapters {
		t.Fatalf("missing selected demux output: video=%t audio=%t chapters=%t files=%v", video, audio, chapters, entries)
	}
}

func TestAdvancedMergerDemuxMPEG2OrVOB(t *testing.T) {
	ffmpeg, err := exec.LookPath("ffmpeg")
	if err != nil { t.Skip("ffmpeg required") }
	ffprobe, err := exec.LookPath("ffprobe")
	if err != nil { t.Skip("ffprobe required") }
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	dir := t.TempDir()
	source := filepath.Join(dir, "mpeg2.mkv")
	if _, err = runMergerCommand(ctx, ffmpeg, "-v", "error", "-f", "lavfi", "-i", "color=size=64x48:rate=25:duration=1", "-c:v", "mpeg2video", "-bf", "0", source); err != nil {
		t.Fatal(err)
	}
	streams, err := probeMergerFile(ctx, ffprobe, source, "video")
	if err != nil || len(streams) != 1 { t.Fatalf("probe MPEG-2: %v %v", streams, err) }
	for _, tc := range []struct{ mode, ext string }{{"mpeg2", ".mpeg2"}, {"vob", ".VOB"}} {
		t.Run(tc.mode, func(t *testing.T) {
			outRoot := filepath.Join(dir, tc.mode)
			if err := os.Mkdir(outRoot, 0755); err != nil { t.Fatal(err) }
			final, err := demuxMerger(ctx, toolPaths{ffmpeg: ffmpeg, ffprobe: ffprobe}, streams, outRoot, tc.mode)
			if err != nil { t.Fatal(err) }
			entries, err := os.ReadDir(final)
			if err != nil || len(entries) != 1 { t.Fatalf("demux entries = %v, %v", entries, err) }
			if !strings.HasSuffix(entries[0].Name(), tc.ext) { t.Fatalf("mode %s produced %s; want %s", tc.mode, entries[0].Name(), tc.ext) }
			info, err := entries[0].Info()
			if err != nil || info.Size() == 0 { t.Fatalf("empty %s output: %v", tc.mode, err) }
		})
	}
}

func TestAdvancedMergerDemuxRejectsUnknownVideoMode(t *testing.T) {
	_, err := demuxMerger(context.Background(), toolPaths{}, []mergerStream{{Path: "movie.mkv", Track: trackOption{Index: 0, Kind: "video"}}}, t.TempDir(), "invalid")
	if err == nil { t.Fatal("invalid MPEG-2/VOB choice was accepted") }
}
