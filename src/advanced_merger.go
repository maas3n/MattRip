//go:build windows || linux

package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

type mergerStream struct {
	Path  string
	Track trackOption
}

func probeMergerFile(ctx context.Context, probe, path, kind string) ([]mergerStream, error) {
	if kind != "all" && kind != "video" && kind != "audio" && kind != "subtitle" {
		return nil, errors.New("invalid stream category")
	}

	args := []string{"-v", "error", "-show_streams", "-show_chapters", "-of", "json"}
	title := 0
	inputPath := ""

	if source, sourceErr := resolveDVDSource(path); sourceErr == nil {
		inputPath = source.Input
		tools, err := desktopCLITools(ctx, false)
		if err != nil {
			return nil, err
		}
		probe = tools.ffprobe
		titles, err := batchPlatformDeps().discoverDVDTitlesViaDVDVideo(ctx, inputPath, tools, func(float64, string) {})
		if err != nil {
			return nil, err
		}
		best, err := longestTitle(titles)
		if err != nil {
			return nil, err
		}
		title = best.Number
		args = appendDesktopDVDInput(args, title, inputPath)
	} else {
		abs, err := filepath.Abs(path)
		if err != nil {
			return nil, err
		}
		st, err := os.Stat(abs)
		if err != nil {
			return nil, err
		}
		if !st.Mode().IsRegular() {
			return nil, errors.New("choose a regular media file or DVD source")
		}
		inputPath = abs
		args = append(args, abs)
	}

	data, err := runMergerCommand(ctx, probe, args...)
	if err != nil {
		return nil, err
	}
	var result ffprobeResult
	if err = json.Unmarshal(data, &result); err != nil {
		return nil, err
	}
	var streams []mergerStream
	for _, raw := range result.Streams {
		if kind != "all" && raw.CodecType != kind {
			continue
		}
		t := trackOption{DVDTitle: title, Index: raw.Index, Kind: raw.CodecType, Codec: raw.CodecName, Language: raw.Tags["language"], Title: raw.Tags["title"], Width: raw.Width, Height: raw.Height, Channels: raw.Channels, ChannelLayout: raw.ChannelLayout}
		if t.Kind == "attachment" && t.Title == "" {
			t.Title = raw.Tags["filename"]
		}
		streams = append(streams, mergerStream{inputPath, t})
	}
	if kind == "all" {
		var chapters ffprobeChapterResult
		if err = json.Unmarshal(data, &chapters); err != nil {
			return nil, err
		}
		if len(chapters.Chapters) > 0 {
			streams = append(streams, mergerStream{inputPath, trackOption{DVDTitle: title, Index: -1, Kind: "chapters", Title: fmt.Sprintf("%d chapters", len(chapters.Chapters))}})
		}
	}
	if len(streams) == 0 {
		return nil, fmt.Errorf("%s contains no %s streams", dvdSourceDisplayName(inputPath), kind)
	}
	return streams, nil
}

func validateMergerChapters(ctx context.Context, probe, path string) error {
	return validateMergerChapterSource(ctx, probe, path, false)
}

func validateMergerChapterSource(ctx context.Context, probe, path string, movieSource bool) error {
	data, err := runMergerCommand(ctx, probe, "-v", "error", "-show_format", "-show_chapters", "-of", "json", path)
	if err != nil {
		return err
	}
	var format struct {
		Format struct {
			Name string `json:"format_name"`
		} `json:"format"`
	}
	if err = json.Unmarshal(data, &format); err != nil {
		return err
	}
	if !movieSource && format.Format.Name != "ffmetadata" && !strings.Contains(format.Format.Name, "matroska") {
		return errors.New("choose FFMETADATA1 metadata or an MKV containing chapters")
	}
	var result ffprobeChapterResult
	if err = json.Unmarshal(data, &result); err != nil {
		return err
	}
	if len(result.Chapters) == 0 {
		return errors.New("chapter file contains no chapters")
	}
	for _, chapter := range result.Chapters {
		start, e1 := strconv.ParseFloat(chapter.StartTime, 64)
		end, e2 := strconv.ParseFloat(chapter.EndTime, 64)
		if e1 != nil || e2 != nil || math.IsNaN(start) || math.IsNaN(end) || math.IsInf(start, 0) || math.IsInf(end, 0) || start < 0 || end <= start {
			return errors.New("chapter timestamps must have a nonnegative start and an end after the start")
		}
	}
	return nil
}

// Chapters are selectable groups, not packet streams. A dedicated source overrides
// movie chapter checkboxes; otherwise exactly one selected chapter set is used.
func resolveMergerSelection(streams []mergerStream, override string) ([]mergerStream, string, error) {
	var media []mergerStream
	chapter := override
	for _, s := range streams {
		if s.Track.Kind != "chapters" {
			media = append(media, s)
			continue
		}
		if override != "" {
			continue
		}
		if chapter != "" && chapter != s.Path {
			return nil, "", errors.New("select only one movie chapter set, or choose a chapter file to override them")
		}
		chapter = s.Path
	}
	if len(media) == 0 {
		return nil, "", errors.New("select at least one media or attachment stream")
	}
	return media, chapter, nil
}

func mergerArgs(streams []mergerStream, chapters, output string) ([]string, error) {
	titles := map[string]int{}
	for _, s := range streams {
		if n, ok := titles[s.Path]; ok && n != s.Track.DVDTitle {
			return nil, errors.New("conflicting DVD titles for one input")
		}
		titles[s.Path] = s.Track.DVDTitle
	}
	input := func(args []string, path string) []string {
		if titles[path] > 0 {
			return appendDesktopDVDInput(args, titles[path], path)
		}
		return appendDesktopMediaInput(args, path)
	}
	var err error
	streams, chapters, err = resolveMergerSelection(streams, chapters)
	if err != nil {
		return nil, err
	}
	args := []string{"-hide_banner", "-nostdin", "-y"}
	inputs := map[string]int{}
	for _, s := range streams {
		if s.Path == "" || s.Track.Index < 0 {
			return nil, errors.New("invalid selected stream")
		}
		if s.Track.Kind != "video" && s.Track.Kind != "audio" && s.Track.Kind != "subtitle" && s.Track.Kind != "attachment" && s.Track.Kind != "data" && s.Track.Kind != "unknown" {
			return nil, errors.New("unsupported selected stream type")
		}
		if _, ok := inputs[s.Path]; !ok {
			inputs[s.Path] = len(inputs)
			args = input(args, s.Path)
		}
	}
	chapterIndex := -1
	if chapters != "" {
		if index, ok := inputs[chapters]; ok {
			chapterIndex = index
		} else {
			chapterIndex = len(inputs)
			args = input(args, chapters)
		}
	}
	seen := map[string]bool{}
	for _, s := range streams {
		key := fmt.Sprintf("%d:%d", inputs[s.Path], s.Track.Index)
		if !seen[key] {
			args = append(args, "-map", key, fmt.Sprintf("-map_metadata:s:%d", len(seen)), fmt.Sprintf("%d:s:%d", inputs[s.Path], s.Track.Index))
			seen[key] = true
		}
	}
	args = append(args, "-map_metadata", "0", "-map_chapters")
	if chapters == "" {
		args = append(args, "-1")
	} else {
		args = append(args, fmt.Sprint(chapterIndex))
	}
	return append(args, "-c", "copy", "-f", "matroska", output), nil
}

func muxMerger(ctx context.Context, tools toolPaths, streams []mergerStream, chapters, output string) error {
	movieChapters := chapters == ""
	originalStreams := streams
	dvdTitles := map[string]int{}
	for _, s := range streams {
		dvdTitles[s.Path] = s.Track.DVDTitle
	}
	for _, s := range streams {
		if s.Track.DVDTitle > 0 {
			var err error
			tools, err = desktopCLITools(ctx, false)
			if err != nil {
				return err
			}
			break
		}
	}
	var selectionErr error
	streams, chapters, selectionErr = resolveMergerSelection(streams, chapters)
	if selectionErr != nil {
		return selectionErr
	}
	if chapters != "" {
		if err := validateSelectedMergerChapters(ctx, tools.ffprobe, chapters, movieChapters, dvdTitles[chapters]); err != nil {
			return err
		}
	}
	if _, err := os.Lstat(output); err == nil {
		return errors.New("output already exists; choose a different filename")
	} else if !os.IsNotExist(err) {
		return err
	}
	partial, err := reservePartialOutput(output)
	if err != nil {
		return err
	}
	args, err := mergerArgs(originalStreams, chapters, partial)
	if err != nil {
		os.Remove(partial)
		return err
	}
	if _, err = runMergerCommand(ctx, tools.ffmpeg, args...); err != nil {
		os.Remove(partial)
		return err
	}
	if err = finalizeRemuxOutput(partial, output); err != nil {
		return fmt.Errorf("%w (completed temporary file: %s)", err, partial)
	}
	return nil
}
