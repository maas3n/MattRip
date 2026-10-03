//go:build windows

package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"
)

func detectChapters(ctx context.Context, ffprobe, src string, title int) ([]DVDChapter, string, error) {
	chapters, err := probeChapters(ctx, ffprobe, src, title)
	if err != nil {
		return nil, "", err
	}
	return chapters, "FFmpeg dvdvideo/libdvdread/libdvdnav pre-index", nil
}

func probeChapters(ctx context.Context, ffprobe, src string, title int) ([]DVDChapter, error) {
	var r ffprobeChapterResult
	out, err := runHidden(ctx, ffprobe, "-v", "error", "-probesize", "100M", "-analyzeduration", "100M", "-fflags", "+genpts", "-f", "dvdvideo", "-title", strconv.Itoa(title), "-preindex", "1", "-i", src, "-show_chapters", "-of", "json")
	if err != nil {
		return nil, fmt.Errorf("ffprobe chapter read failed: %w", err)
	}
	if err := json.Unmarshal(out, &r); err != nil {
		return nil, fmt.Errorf("could not parse ffprobe chapters: %w", err)
	}
	if len(r.Chapters) == 0 {
		return nil, errors.New("no DVD chapters were detected")
	}
	chapters := make([]DVDChapter, 0, len(r.Chapters))
	for i, ch := range r.Chapters {
		start, err := secondsTextDuration(ch.StartTime)
		if err != nil {
			return nil, fmt.Errorf("chapter %d has invalid start time %q", i+1, ch.StartTime)
		}
		end, err := secondsTextDuration(ch.EndTime)
		if err != nil {
			return nil, fmt.Errorf("chapter %d has invalid end time %q", i+1, ch.EndTime)
		}
		if end < start {
			return nil, fmt.Errorf("chapter %d ends before it starts", i+1)
		}
		chapters = append(chapters, DVDChapter{Number: i + 1, Start: start, Duration: end - start})
	}
	return chapters, nil
}

func secondsTextDuration(s string) (time.Duration, error) {
	f, err := strconv.ParseFloat(strings.TrimSpace(s), 64)
	if err != nil || f < 0 {
		return 0, errors.New("invalid seconds value")
	}
	return time.Duration(f * float64(time.Second)), nil
}

// readDVDVideoTitleDuration delegates title timing entirely to FFprobe's
// dvdvideo demuxer. dvdvideo is backed by libdvdread/libdvdnav in the pinned
// desktop FFmpeg build; MattRip does not parse DVD title timing itself.
func readDVDVideoTitleDuration(ctx context.Context, ffprobe, src string, title int) (time.Duration, error) {
	d, err := readDVDVideoTitleDurationAttempt(ctx, ffprobe, src, title, false)
	if err == nil {
		return d, nil
	}
	// Some discs need dvdvideo's NAV-packet pre-index pass before duration is
	// available. The retry is still the same libdvdread/libdvdnav dvdvideo path.
	return readDVDVideoTitleDurationAttempt(ctx, ffprobe, src, title, true)
}

func readDVDVideoTitleDurationAttempt(ctx context.Context, ffprobe, src string, title int, preindex bool) (time.Duration, error) {
	childCtx, cancel := context.WithTimeout(ctx, 25*time.Second)
	defer cancel()
	args := []string{"-v", "error", "-probesize", "100M", "-analyzeduration", "100M", "-fflags", "+genpts", "-f", "dvdvideo", "-title", strconv.Itoa(title)}
	if preindex {
		args = append(args, "-preindex", "1")
	}
	args = append(args, "-i", src, "-show_entries", "format=duration", "-of", "default=noprint_wrappers=1:nokey=1")
	out, err := runHidden(childCtx, ffprobe, args...)
	if err != nil {
		return 0, err
	}
	s := strings.TrimSpace(string(out))
	if s == "" || s == "N/A" {
		return 0, errors.New("no duration")
	}
	for _, line := range strings.Fields(s) {
		f, err := strconv.ParseFloat(strings.TrimSpace(line), 64)
		if err == nil && f > .1 {
			return time.Duration(f * float64(time.Second)), nil
		}
	}
	return 0, errors.New("invalid duration")
}

func probeStreams(ctx context.Context, ffprobe, src string, title int) (ffprobeResult, error) {
	var r ffprobeResult
	out, err := runHidden(ctx, ffprobe, "-v", "error", "-probesize", "100M", "-analyzeduration", "100M", "-fflags", "+genpts", "-f", "dvdvideo", "-title", strconv.Itoa(title), "-i", src, "-show_streams", "-of", "json")
	if err != nil {
		return r, fmt.Errorf("ffprobe metadata read failed: %w", err)
	}
	if err := json.Unmarshal(out, &r); err != nil {
		return r, fmt.Errorf("could not parse ffprobe metadata: %w", err)
	}
	return r, nil
}

func runHidden(ctx context.Context, path string, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, path, args...)
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	if err != nil {
		if ctx.Err() != nil {
			return stdout.Bytes(), ctx.Err()
		}
		detail := strings.TrimSpace(tail(stderr.String(), 2000))
		if detail == "" {
			detail = err.Error()
		}
		log.Printf("%s failed: %v; stderr=%s", filepath.Base(path), err, strings.TrimSpace(tail(stderr.String(), 4000)))
		return stdout.Bytes(), fmt.Errorf("%v: %s", err, detail)
	}
	if detail := strings.TrimSpace(stderr.String()); detail != "" {
		log.Printf("%s stderr: %s", filepath.Base(path), strings.TrimSpace(tail(detail, 4000)))
	}
	return stdout.Bytes(), nil
}

func normalizeSource(p string) (string, error) {
	p = filepath.Clean(strings.TrimSpace(strings.Trim(p, "\"")))
	if p == "." || p == "" {
		return "", errors.New("choose a DVD folder, VIDEO_TS folder, or ISO file")
	}
	info, err := os.Stat(p)
	if err != nil {
		return "", fmt.Errorf("source does not exist: %s", p)
	}
	if !info.IsDir() {
		if strings.EqualFold(filepath.Ext(p), ".iso") {
			return p, nil
		}
		return "", errors.New("source file must be a DVD ISO (.iso)")
	}
	if strings.EqualFold(filepath.Base(p), "VIDEO_TS") {
		if fileExists(filepath.Join(p, "VIDEO_TS.IFO")) {
			return filepath.Dir(p), nil
		}
	}
	if fileExists(filepath.Join(p, "VIDEO_TS", "VIDEO_TS.IFO")) {
		return p, nil
	}
	if fileExists(filepath.Join(p, "VIDEO_TS.IFO")) {
		return filepath.Dir(p), nil
	}
	return "", errors.New("the selected folder does not contain a VIDEO_TS DVD structure")
}

func currentSource() (string, error) { return normalizeTabSource(getText(app.sourceEdit)) }
func selectedTitle() (titleInfo, error) {
	idx, _, _ := procSendMessageW.Call(app.titleCombo, CB_GETCURSEL, 0, 0)
	if int32(idx) < 0 {
		return titleInfo{}, errors.New("scan the DVD titles and choose a title first")
	}
	current, err := normalizeSource(getText(app.sourceEdit))
	if err != nil {
		return titleInfo{}, err
	}
	app.titlesMu.RLock()
	defer app.titlesMu.RUnlock()
	if !strings.EqualFold(filepath.Clean(current), filepath.Clean(app.titlesSource)) {
		return titleInfo{}, errors.New("the source changed after the last title scan; scan titles again")
	}
	if int(idx) >= len(app.titles) {
		return titleInfo{}, errors.New("scan the DVD titles again")
	}
	return app.titles[int(idx)], nil
}

func validateOutputDir(dir string) error {
	dir = filepath.Clean(dir)
	if dir == "." || dir == "" {
		return errors.New("choose an output folder")
	}
	protected := []string{os.Getenv("ProgramFiles"), os.Getenv("ProgramW6432"), os.Getenv("ProgramFiles(x86)"), os.Getenv("WINDIR")}
	abs, err := filepath.Abs(dir)
	if err != nil {
		return err
	}
	for _, p := range protected {
		if p == "" {
			continue
		}
		pa, _ := filepath.Abs(p)
		if isWithin(abs, pa) {
			return fmt.Errorf("output folder must not be inside a protected Windows folder:\n%s", pa)
		}
	}
	if err := os.MkdirAll(abs, 0755); err != nil {
		return fmt.Errorf("cannot create output folder: %w", err)
	}
	f, err := os.CreateTemp(abs, ".mattrip-write-test-")
	if err != nil {
		return fmt.Errorf("output folder is not writable: %w", err)
	}
	name := f.Name()
	_ = f.Close()
	_ = os.Remove(name)
	return nil
}
func isWithin(path, root string) bool {
	p := strings.ToLower(filepath.Clean(path))
	r := strings.ToLower(filepath.Clean(root))
	return p == r || strings.HasPrefix(p, r+string(os.PathSeparator))
}
func outputPath(src, outDir string, title int) string {
	base := filepath.Base(src)
	if strings.EqualFold(filepath.Ext(base), ".iso") {
		base = strings.TrimSuffix(base, filepath.Ext(base))
	}
	if strings.EqualFold(base, "VIDEO_TS") {
		base = filepath.Base(filepath.Dir(src))
	}
	base = sanitizeFilename(base)
	if base == "" {
		base = "DVD"
	}
	if title > 1 {
		base = fmt.Sprintf("%s-title-%02d", base, title)
	}
	return filepath.Join(outDir, base+".mkv")
}
func sanitizeFilename(s string) string {
	s = strings.TrimSpace(s)
	invalid := `<>:"/\|?*`
	s = strings.Map(func(r rune) rune {
		if r < 32 || strings.ContainsRune(invalid, r) {
			return '_'
		}
		return r
	}, s)
	s = strings.TrimRight(s, ". ")
	reserved := map[string]bool{"CON": true, "PRN": true, "AUX": true, "NUL": true, "COM1": true, "COM2": true, "COM3": true, "COM4": true, "COM5": true, "COM6": true, "COM7": true, "COM8": true, "COM9": true, "LPT1": true, "LPT2": true, "LPT3": true, "LPT4": true, "LPT5": true, "LPT6": true, "LPT7": true, "LPT8": true, "LPT9": true}
	if reserved[strings.ToUpper(s)] {
		s = "_" + s
	}
	return s
}
func mediaInfoTarget(src string) string {
	info, err := os.Stat(src)
	if err != nil {
		return ""
	}
	if !info.IsDir() {
		return src
	}
	p := filepath.Join(src, "VIDEO_TS", "VIDEO_TS.IFO")
	if fileExists(p) {
		return p
	}
	return ""
}
