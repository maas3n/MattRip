//go:build windows || linux

package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

func isMKVSource(path string) bool { return strings.EqualFold(filepath.Ext(path), ".mkv") }

// The GUI accepts MKV; DVD-only batch discovery and CLI normalization stay separate.
func normalizeTabSource(path string) (string, error) {
	path = strings.TrimSpace(strings.Trim(path, "\""))
	if !isMKVSource(path) {
		return normalizeSource(path)
	}
	path = filepath.Clean(path)
	st, err := os.Stat(path)
	if err != nil {
		return "", err
	}
	if !st.Mode().IsRegular() {
		return "", errors.New("choose a regular MKV file")
	}
	return path, nil
}

func scanMKV(ctx context.Context, tools toolPaths, source string) ([]titleInfo, error) {
	if tools.mediainfo == "" {
		return nil, errors.New("MediaInfo is required to scan MKV sources")
	}
	data, err := runMergerCommand(ctx, tools.mediainfo, "--Inform=General;%Duration%", source)
	if err != nil {
		return nil, err
	}
	ms, err := strconv.ParseFloat(strings.TrimSpace(string(data)), 64)
	if err != nil || ms <= 0 {
		return nil, errors.New("MediaInfo could not read the MKV duration")
	}
	return []titleInfo{{Number: 1, Duration: time.Duration(ms * float64(time.Millisecond))}}, nil
}

func probeTabMKV(ctx context.Context, tools toolPaths, source string) (ffprobeResult, string, error) {
	var probe ffprobeResult
	data, err := runMergerCommand(ctx, tools.ffprobe, "-v", "error", "-show_streams", "-of", "json", source)
	if err != nil {
		return probe, "", err
	}
	if err = json.Unmarshal(data, &probe); err != nil {
		return probe, "", err
	}
	if tools.mediainfo == "" {
		return probe, "", errors.New("MediaInfo is required for MKV metadata")
	}
	data, err = runMergerCommand(ctx, tools.mediainfo, source)
	return probe, string(data), err
}

func tabInput(args []string, source string, title int) []string {
	if isMKVSource(source) {
		return append(args, "-i", source)
	}
	// Pre-index DVD chapters and normalize the selected title timeline.
	args = append(args, "-preindex", "1")
	return appendDesktopDVDInput(args, title, source)
}

func remuxMKV(ctx context.Context, tools toolPaths, source, output string, indexes []int, chapters bool) (string, error) {
	if err := validateOutputDir(output); err != nil {
		return "", err
	}
	final := filepath.Join(output, strings.TrimSuffix(filepath.Base(source), filepath.Ext(source))+"-remux.mkv")
	if _, err := os.Stat(final); !os.IsNotExist(err) {
		return "", fmt.Errorf("output already exists or is inaccessible: %s", final)
	}
	maps, err := ffmpegStreamMapArgs(indexes)
	if err != nil {
		return "", err
	}
	partial, err := reservePartialOutput(final)
	if err != nil {
		return "", err
	}
	defer os.Remove(partial)
	args := appendDesktopMediaInput([]string{"-hide_banner", "-v", "error", "-nostdin", "-y"}, source)
	args = append(args, maps...)
	chapterMap := "-1"
	if chapters {
		chapterMap = "0"
	}
	args = append(args, "-c", "copy", "-map_metadata", "0", "-map_chapters", chapterMap, "-f", "matroska", partial)
	if _, err = runMergerCommand(ctx, tools.ffmpeg, args...); err != nil {
		return "", err
	}
	if err = validateAndSyncOutput(partial); err != nil {
		return "", err
	}
	if err = commitOutputNoReplace(partial, final); err != nil {
		return "", err
	}
	return final, nil
}

type demuxFormat struct{ Extension, Muxer, Filter string }

func streamDemuxFormat(codec, dvdVideo string) (demuxFormat, error) {
	formats := map[string]demuxFormat{
		"mpeg2video": {"mpeg2", "mpeg2video", ""}, "mpeg1video": {"mpeg1", "mpeg1video", ""}, "mpeg4": {"m4v", "m4v", ""},
		"h264": {"h264", "h264", "h264_mp4toannexb"}, "hevc": {"h265", "hevc", "hevc_mp4toannexb"},
		"ac3": {"ac3", "ac3", ""}, "eac3": {"eac3", "eac3", ""}, "dts": {"dts", "dts", ""},
		"aac": {"aac", "adts", ""}, "mp2": {"mp2", "mp2", ""}, "mp3": {"mp3", "mp3", ""},
		"flac": {"flac", "flac", ""}, "truehd": {"thd", "truehd", ""},
		"subrip": {"srt", "srt", ""}, "ass": {"ass", "ass", ""}, "ssa": {"ssa", "ass", ""},
		"hdmv_pgs_subtitle": {"sup", "sup", ""}, "dvd_subtitle": {"sub", "vob", ""},
	}
	if codec == "pcm_s16le" || codec == "pcm_s24le" || codec == "pcm_s32le" {
		return demuxFormat{"wav", "wav", ""}, nil
	}
	f, ok := formats[codec]
	if !ok {
		return f, fmt.Errorf("lossless demux is not supported for codec %s; deselect that stream", codec)
	}
	if codec == "mpeg2video" && dvdVideo == "vob" {
		f = demuxFormat{"VOB", "vob", ""}
	}
	return f, nil
}

func demuxTab(ctx context.Context, tools toolPaths, source string, title int, output string, indexes []int, chapters bool, dvdVideo string) (final string, err error) {
	if err = validateOutputDir(output); err != nil {
		return "", err
	}
	args := tabInput([]string{"-v", "error"}, source, title)
	args = append(args, "-show_streams", "-show_chapters", "-show_data", "-of", "json")
	data, err := runMergerCommand(ctx, tools.ffprobe, args...)
	if err != nil {
		return "", err
	}
	var probe struct {
		Streams []struct {
			Index  int               `json:"index"`
			Codec  string            `json:"codec_name"`
			Kind   string            `json:"codec_type"`
			Extra  string            `json:"extradata"`
			Width  int               `json:"width"`
			Height int               `json:"height"`
			Tags   map[string]string `json:"tags"`
		} `json:"streams"`
		Chapters []struct {
			Start string            `json:"start_time"`
			Tags  map[string]string `json:"tags"`
		} `json:"chapters"`
	}
	if err = json.Unmarshal(data, &probe); err != nil {
		return "", err
	}
	// dvdvideo supplies the IFO palette but may omit VobSub canvas extradata.
	// Only use the DVD picture dimensions here; an arbitrary MKV video may
	// have been resized independently of its subtitle canvas.
	var dvdWidth, dvdHeight int
	if !isMKVSource(source) {
		for _, stream := range probe.Streams {
			if stream.Kind == "video" && stream.Width > 0 && stream.Height > 0 {
				dvdWidth, dvdHeight = stream.Width, stream.Height
				break
			}
		}
	}
	selected := map[int]bool{}
	for _, i := range indexes {
		selected[i] = true
	}
	type export struct {
		index           int
		format          demuxFormat
		extra, language string
	}
	var exports []export
	for _, s := range probe.Streams {
		if indexes != nil && !selected[s.Index] {
			continue
		}
		if s.Kind != "video" && s.Kind != "audio" && s.Kind != "subtitle" {
			continue
		}
		f, e := streamDemuxFormat(s.Codec, dvdVideo)
		if e != nil {
			return "", e
		}
		exports = append(exports, export{s.Index, f, s.Extra, s.Tags["language"]})
		delete(selected, s.Index)
	}
	if len(selected) > 0 {
		return "", errors.New("selected stream is no longer available; scan again")
	}
	if len(exports) == 0 {
		return "", errors.New("select at least one track")
	}
	base := strings.TrimSuffix(filepath.Base(source), filepath.Ext(source))
	if !isMKVSource(source) {
		base = dvdSourceBaseName(source) + fmt.Sprintf("-title-%02d", title)
	}
	// Reserve a new output directory; never overwrite a previous export.
	final, err = os.MkdirTemp(output, base+"-demux-")
	if err != nil {
		return "", err
	}
	defer func() {
		if err != nil {
			os.RemoveAll(final)
		}
	}()
	args = []string{"-hide_banner", "-v", "error", "-nostdin", "-n"}
	if isMKVSource(source) {
		args = append(args, "-copyts")
	}
	args = tabInput(args, source, title)
	for _, e := range exports {
		name := filepath.Join(final, fmt.Sprintf("track-%02d.%s", e.index, e.format.Extension))
		args = append(args, "-map", fmt.Sprintf("0:%d", e.index), "-c", "copy", "-map_metadata", "-1", "-map_chapters", "-1")
		if e.format.Filter != "" {
			args = append(args, "-bsf:v", e.format.Filter)
		}
		if e.format.Muxer == "vob" {
			args = append(args, "-muxdelay", "0", "-preload", "0")
		}
		args = append(args, "-f", e.format.Muxer, name)
	}
	if _, err = runMergerCommand(ctx, tools.ffmpeg, args...); err != nil {
		return final, err
	}
	for _, e := range exports {
		name := filepath.Join(final, fmt.Sprintf("track-%02d.%s", e.index, e.format.Extension))
		if err = validateAndSyncOutput(name); err != nil {
			return final, err
		}
		if e.format.Extension == "sub" {
			if err = writeVobSubIndex(name, e.extra, e.language, dvdWidth, dvdHeight); err != nil {
				return final, err
			}
		}
	}
	if chapters && len(probe.Chapters) > 0 {
		var b strings.Builder
		for i, ch := range probe.Chapters {
			var start time.Duration
			start, err = secondsTextDuration(ch.Start)
			if err != nil {
				return final, err
			}
			label := strings.ReplaceAll(strings.ReplaceAll(ch.Tags["title"], "\r", " "), "\n", " ")
			if label == "" {
				label = fmt.Sprintf("Chapter %d", i+1)
			}
			fmt.Fprintf(&b, "CHAPTER%02d=%s\nCHAPTER%02dNAME=%s\n", i+1, chapterTimestamp(start.Milliseconds()), i+1, label)
		}
		err = os.WriteFile(filepath.Join(final, "Chapters.txt"), []byte(b.String()), 0644)
	}
	return final, err
}

func chapterTimestamp(ms int64) string {
	return fmt.Sprintf("%02d:%02d:%02d.%03d", ms/3600000, ms/60000%60, ms/1000%60, ms%1000)
}
