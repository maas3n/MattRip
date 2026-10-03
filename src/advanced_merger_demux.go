//go:build windows || linux

package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

type mergerDemuxProbe struct {
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

func appendMergerDemuxInput(args []string, path string, title int, copyTS bool) []string {
	if title > 0 {
		args = append(args, "-preindex", "1")
		return appendDesktopDVDInput(args, title, path)
	}
	if isMKVSource(path) {
		if copyTS {
			args = append(args, "-copyts")
		}
		return append(args, "-i", path)
	}
	return appendDesktopMediaInput(args, path)
}

func demuxMerger(ctx context.Context, tools toolPaths, selected []mergerStream, outputDir, dvdVideo string) (final string, err error) {
	if dvdVideo != "mpeg2" && dvdVideo != "vob" {
		return "", errors.New("choose MPEG-2 or VOB video export")
	}
	if err = validateOutputDir(outputDir); err != nil {
		return "", err
	}
	if len(selected) == 0 {
		return "", errors.New("select at least one stream to demux")
	}
	type group struct {
		path     string
		title    int
		media    []mergerStream
		chapters bool
	}
	var groups []*group
	byPath := map[string]*group{}
	mediaCount := 0
	for _, s := range selected {
		g := byPath[s.Path]
		if g == nil {
			g = &group{path: s.Path, title: s.Track.DVDTitle}
			byPath[s.Path] = g
			groups = append(groups, g)
		} else if g.title != s.Track.DVDTitle {
			return "", errors.New("conflicting DVD titles for one input")
		}
		switch s.Track.Kind {
		case "chapters":
			g.chapters = true
		case "video", "audio", "subtitle":
			if s.Track.Index < 0 {
				return "", errors.New("invalid selected stream")
			}
			g.media = append(g.media, s)
			mediaCount++
		default:
			return "", fmt.Errorf("DEMUX supports video, audio and subtitle streams; deselect %s from %s", s.Track.Kind, dvdSourceDisplayName(s.Path))
		}
	}
	if mediaCount == 0 {
		return "", errors.New("select at least one video, audio, or subtitle stream to demux")
	}
	for _, g := range groups {
		if g.chapters && len(g.media) == 0 {
			return "", fmt.Errorf("select at least one media stream from %s to demux its chapters", dvdSourceDisplayName(g.path))
		}
	}
	for _, g := range groups {
		if g.title > 0 {
			tools, err = desktopCLITools(ctx, false)
			if err != nil {
				return "", err
			}
			break
		}
	}
	final, err = os.MkdirTemp(outputDir, "Advanced-Merger-Demux-")
	if err != nil {
		return "", err
	}
	defer func() {
		if err != nil {
			_ = os.RemoveAll(final)
		}
	}()

	for groupIndex, g := range groups {
		if len(g.media) == 0 {
			continue
		}
		probeArgs := appendMergerDemuxInput([]string{"-v", "error"}, g.path, g.title, false)
		probeArgs = append(probeArgs, "-show_streams", "-show_chapters", "-show_data", "-of", "json")
		data, runErr := runMergerCommand(ctx, tools.ffprobe, probeArgs...)
		if runErr != nil {
			return final, runErr
		}
		var probe mergerDemuxProbe
		if err = json.Unmarshal(data, &probe); err != nil {
			return final, err
		}
		type streamMeta struct {
			Codec, Extra, Language string
			Width, Height           int
		}
		streamByIndex := map[int]streamMeta{}
		var dvdWidth, dvdHeight int
		for _, s := range probe.Streams {
			streamByIndex[s.Index] = streamMeta{s.Codec, s.Extra, s.Tags["language"], s.Width, s.Height}
			if g.title > 0 && s.Kind == "video" && dvdWidth == 0 && s.Width > 0 && s.Height > 0 {
				dvdWidth, dvdHeight = s.Width, s.Height
			}
		}
		stem := strings.TrimSuffix(filepath.Base(g.path), filepath.Ext(g.path))
		if g.title > 0 {
			stem = dvdSourceBaseName(g.path)
		}
		stem = sanitizeFilename(stem)
		if stem == "" {
			stem = "source"
		}
		if g.title > 0 {
			stem += fmt.Sprintf("-title-%02d", g.title)
		}
		prefix := fmt.Sprintf("%02d-%s", groupIndex+1, stem)
		type export struct {
			index           int
			format          demuxFormat
			extra, language string
			path            string
		}
		var exports []export
		for _, selectedStream := range g.media {
			meta, ok := streamByIndex[selectedStream.Track.Index]
			if !ok {
				return final, fmt.Errorf("selected stream %d is no longer available in %s", selectedStream.Track.Index, filepath.Base(g.path))
			}
			format, formatErr := streamDemuxFormat(meta.Codec, dvdVideo)
			if formatErr != nil {
				return final, formatErr
			}
			path := filepath.Join(final, fmt.Sprintf("%s-track-%02d.%s", prefix, selectedStream.Track.Index, format.Extension))
			exports = append(exports, export{selectedStream.Track.Index, format, meta.Extra, meta.Language, path})
		}
		args := []string{"-hide_banner", "-v", "error", "-nostdin", "-n"}
		args = appendMergerDemuxInput(args, g.path, g.title, true)
		for _, e := range exports {
			args = append(args, "-map", fmt.Sprintf("0:%d", e.index), "-c", "copy", "-map_metadata", "-1", "-map_chapters", "-1")
			if e.format.Filter != "" {
				args = append(args, "-bsf:v", e.format.Filter)
			}
			if e.format.Muxer == "vob" {
				args = append(args, "-muxdelay", "0", "-preload", "0")
			}
			args = append(args, "-f", e.format.Muxer, e.path)
		}
		if _, err = runMergerCommand(ctx, tools.ffmpeg, args...); err != nil {
			return final, err
		}
		for _, e := range exports {
			if err = validateAndSyncOutput(e.path); err != nil {
				return final, err
			}
			if e.format.Extension == "sub" {
				if err = writeVobSubIndex(e.path, e.extra, e.language, dvdWidth, dvdHeight); err != nil {
					return final, err
				}
			}
		}
		if g.chapters {
			if len(probe.Chapters) == 0 {
				return final, fmt.Errorf("%s no longer contains chapters", filepath.Base(g.path))
			}
			var b strings.Builder
			for i, chapter := range probe.Chapters {
				start, parseErr := secondsTextDuration(chapter.Start)
				if parseErr != nil {
					return final, parseErr
				}
				label := strings.ReplaceAll(strings.ReplaceAll(chapter.Tags["title"], "\r", " "), "\n", " ")
				if label == "" {
					label = fmt.Sprintf("Chapter %d", i+1)
				}
				fmt.Fprintf(&b, "CHAPTER%02d=%s\nCHAPTER%02dNAME=%s\n", i+1, chapterTimestamp(start.Milliseconds()), i+1, label)
			}
			chapterPath := filepath.Join(final, prefix+"-Chapters.txt")
			if err = os.WriteFile(chapterPath, []byte(b.String()), 0644); err != nil {
				return final, err
			}
			if err = validateAndSyncOutput(chapterPath); err != nil {
				return final, err
			}
		}
	}
	return final, nil
}
