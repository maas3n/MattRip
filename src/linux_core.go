//go:build linux

package main

import (
	"bufio"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
)

const appName = "MattRip"

var appVersion = "1.3.0-dev"

const (
	linuxFFmpegTag    = "autobuild-2026-09-08-23-15"
	linuxFFmpegAsset  = "ffmpeg-N-126479-g08cd8df29d-linux64-gpl.tar.xz"
	linuxFFmpegSHA256 = "635a2d74de852064852e95db5a9c475a86d36e2b6390e3c1ba5e46b2c46dfce0"
)

type titleInfo struct {
	Number   int
	Duration time.Duration
}
type ffprobeResult struct {
	Streams []struct {
		Index         int               `json:"index"`
		CodecName     string            `json:"codec_name"`
		CodecType     string            `json:"codec_type"`
		Width         int               `json:"width"`
		Height        int               `json:"height"`
		Channels      int               `json:"channels"`
		ChannelLayout string            `json:"channel_layout"`
		Tags          map[string]string `json:"tags"`
	} `json:"streams"`
}
type ffprobeChapterResult struct {
	Chapters []struct {
		ID        int               `json:"id"`
		StartTime string            `json:"start_time"`
		EndTime   string            `json:"end_time"`
		Tags      map[string]string `json:"tags"`
	} `json:"chapters"`
}
type toolPaths struct{ ffmpeg, ffprobe, mediainfo, source string }
type progressFunc func(float64, string)

func noopProgress(float64, string) {}

func inspectSystemTools(ctx context.Context) (toolPaths, error) {
	ffmpeg, err := exec.LookPath("ffmpeg")
	if err != nil {
		return toolPaths{}, errors.New("ffmpeg was not found on PATH")
	}
	ffprobe, err := exec.LookPath("ffprobe")
	if err != nil {
		return toolPaths{}, errors.New("ffprobe was not found on PATH")
	}
	if err := checkDVDDemuxer(ctx, ffmpeg); err != nil {
		return toolPaths{}, fmt.Errorf("system FFmpeg is not suitable: %w", err)
	}
	mediainfo, _ := exec.LookPath("mediainfo")
	return toolPaths{ffmpeg: ffmpeg, ffprobe: ffprobe, mediainfo: mediainfo, source: "system"}, nil
}

func ensureTools(ctx context.Context, needMediaInfo bool, progress progressFunc) (toolPaths, error) {
	if progress == nil {
		progress = noopProgress
	}
	if tools, err := inspectSystemTools(ctx); err == nil {
		if needMediaInfo && tools.mediainfo == "" {
			progress(0, "System FFmpeg is compatible; MediaInfo is not installed (optional).")
		}
		return tools, nil
	}
	progress(0, "System FFmpeg missing or incompatible; preparing MattRip-managed FFmpeg…")
	root, err := cacheRoot()
	if err != nil {
		return toolPaths{}, err
	}
	ffDir := filepath.Join(root, "tools", "ffmpeg-"+linuxFFmpegTag)
	ffmpeg, ffprobe := filepath.Join(ffDir, "ffmpeg"), filepath.Join(ffDir, "ffprobe")
	if !fileExists(ffmpeg) || !fileExists(ffprobe) {
		if err := installLinuxFFmpeg(ctx, ffDir, progress); err != nil {
			return toolPaths{}, err
		}
	}
	if err := checkDVDDemuxer(ctx, ffmpeg); err != nil {
		return toolPaths{}, fmt.Errorf("managed FFmpeg self-check failed: %w", err)
	}
	mediainfo, _ := exec.LookPath("mediainfo")
	if needMediaInfo && mediainfo == "" {
		progress(1, "Managed FFmpeg ready; MediaInfo is not installed (optional).")
	}
	return toolPaths{ffmpeg: ffmpeg, ffprobe: ffprobe, mediainfo: mediainfo, source: "managed"}, nil
}

func checkDVDDemuxer(ctx context.Context, ffmpeg string) error {
	out, err := runCommand(ctx, ffmpeg, "-hide_banner", "-demuxers")
	if err != nil {
		return err
	}
	if !strings.Contains(string(out), "dvdvideo") {
		return errors.New("FFmpeg does not expose the dvdvideo demuxer")
	}
	return nil
}

func toolSummary(ctx context.Context) string {
	var b strings.Builder
	if p, err := exec.LookPath("ffmpeg"); err == nil {
		fmt.Fprintf(&b, "ffmpeg: %s\n", p)
		if err := checkDVDDemuxer(ctx, p); err == nil {
			b.WriteString("  dvdvideo demuxer: yes\n")
		} else {
			fmt.Fprintf(&b, "  dvdvideo demuxer: no (%v)\n", err)
		}
	} else {
		b.WriteString("ffmpeg: not found\n")
	}
	if p, err := exec.LookPath("ffprobe"); err == nil {
		fmt.Fprintf(&b, "ffprobe: %s\n", p)
	} else {
		b.WriteString("ffprobe: not found\n")
	}
	if p, err := exec.LookPath("mediainfo"); err == nil {
		fmt.Fprintf(&b, "mediainfo: %s\n", p)
	} else {
		b.WriteString("mediainfo: not found (optional)\n")
	}
	return strings.TrimSpace(b.String())
}

func installLinuxFFmpeg(ctx context.Context, dst string, progress progressFunc) error {
	root, err := cacheRoot()
	if err != nil {
		return err
	}
	work, err := os.MkdirTemp(root, "ffmpeg-install-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(work)
	archive := filepath.Join(work, linuxFFmpegAsset)
	url := "https://github.com/BtbN/FFmpeg-Builds/releases/download/" + linuxFFmpegTag + "/" + linuxFFmpegAsset
	if err := downloadFile(ctx, url, archive, 200<<20, func(f float64) { progress(f*.82, "Downloading verified FFmpeg fallback…") }); err != nil {
		return fmt.Errorf("FFmpeg download failed: %w", err)
	}
	if err := verifySHA256(archive, linuxFFmpegSHA256); err != nil {
		return fmt.Errorf("FFmpeg verification failed: %w", err)
	}
	tarPath, err := exec.LookPath("tar")
	if err != nil {
		return errors.New("tar is required to unpack the managed FFmpeg fallback")
	}
	extract := filepath.Join(work, "extract")
	if err := os.MkdirAll(extract, 0755); err != nil {
		return err
	}
	progress(.85, "Extracting FFmpeg…")
	cmd := exec.CommandContext(ctx, tarPath, "-xJf", archive, "-C", extract)
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("FFmpeg extraction failed: %v: %s", err, strings.TrimSpace(string(out)))
	}
	ffmpeg, err := findFile(extract, "ffmpeg")
	if err != nil {
		return err
	}
	ffprobe, err := findFile(extract, "ffprobe")
	if err != nil {
		return err
	}
	tmp := dst + ".new"
	_ = os.RemoveAll(tmp)
	if err := os.MkdirAll(tmp, 0755); err != nil {
		return err
	}
	if err := copyExecutable(ffmpeg, filepath.Join(tmp, "ffmpeg")); err != nil {
		return err
	}
	if err := copyExecutable(ffprobe, filepath.Join(tmp, "ffprobe")); err != nil {
		return err
	}
	_ = os.RemoveAll(dst)
	if err := os.Rename(tmp, dst); err != nil {
		return err
	}
	progress(1, "Managed FFmpeg is ready.")
	return nil
}

func downloadFile(ctx context.Context, url, dest string, maxBytes int64, onProgress func(float64)) error {
	if !strings.HasPrefix(strings.ToLower(url), "https://") {
		return errors.New("refusing non-HTTPS download")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", appName+"/"+appVersion)
	client := &http.Client{Timeout: 30 * time.Minute, CheckRedirect: func(req *http.Request, via []*http.Request) error {
		if len(via) >= 10 {
			return errors.New("too many redirects")
		}
		if !strings.EqualFold(req.URL.Scheme, "https") {
			return errors.New("refusing non-HTTPS redirect")
		}
		return nil
	}}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("HTTP %s", resp.Status)
	}
	if maxBytes > 0 && resp.ContentLength > maxBytes {
		return fmt.Errorf("download is unexpectedly large (%d bytes)", resp.ContentLength)
	}
	f, err := os.OpenFile(dest, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0600)
	if err != nil {
		return err
	}
	defer f.Close()
	buf := make([]byte, 128*1024)
	var written int64
	for {
		n, er := resp.Body.Read(buf)
		if n > 0 {
			if _, ew := f.Write(buf[:n]); ew != nil {
				return ew
			}
			written += int64(n)
			if maxBytes > 0 && written > maxBytes {
				return errors.New("download exceeded safety limit")
			}
			if resp.ContentLength > 0 && onProgress != nil {
				onProgress(float64(written) / float64(resp.ContentLength))
			}
		}
		if er == io.EOF {
			break
		}
		if er != nil {
			return er
		}
		if err := ctx.Err(); err != nil {
			return err
		}
	}
	return f.Sync()
}

func verifySHA256(path, expected string) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return err
	}
	got := hex.EncodeToString(h.Sum(nil))
	if !strings.EqualFold(got, strings.TrimSpace(expected)) {
		return fmt.Errorf("SHA-256 mismatch (got %s)", got)
	}
	return nil
}
func copyExecutable(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0755)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		return err
	}
	if err := out.Close(); err != nil {
		return err
	}
	return os.Chmod(dst, 0755)
}
func findFile(root, name string) (string, error) {
	var found string
	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() && d.Name() == name {
			found = path
			return filepath.SkipAll
		}
		return nil
	})
	if err != nil {
		return "", err
	}
	if found == "" {
		return "", fmt.Errorf("%s was not found in archive", name)
	}
	return found, nil
}
func fileExists(p string) bool { st, err := os.Stat(p); return err == nil && !st.IsDir() }
func cacheRoot() (string, error) {
	p, err := os.UserCacheDir()
	if err != nil {
		return "", err
	}
	p = filepath.Join(p, "mattrip")
	if err := os.MkdirAll(p, 0700); err != nil {
		return "", err
	}
	return p, nil
}

func normalizeSource(p string) (string, error) {
	src, err := resolveDVDSource(p)
	if err != nil {
		return "", err
	}
	return src.Input, nil
}
func findChildDirFold(dir, name string) string {
	es, err := os.ReadDir(dir)
	if err != nil {
		return ""
	}
	for _, e := range es {
		if e.IsDir() && strings.EqualFold(e.Name(), name) {
			return filepath.Join(dir, e.Name())
		}
	}
	return ""
}
func fileExistsFold(dir, name string) bool {
	es, err := os.ReadDir(dir)
	if err != nil {
		return false
	}
	for _, e := range es {
		if !e.IsDir() && strings.EqualFold(e.Name(), name) {
			return true
		}
	}
	return false
}

func discoverDVDTitlesViaDVDVideo(ctx context.Context, src string, tools toolPaths, progress progressFunc) ([]titleInfo, error) {
	src, err := normalizeSource(src)
	if err != nil {
		return nil, err
	}
	if progress == nil {
		progress = noopProgress
	}

	// FFmpeg's dvdvideo demuxer accepts title numbers 1..99 and uses
	// libdvdread/libdvdnav as its source of truth. Deliberately probe the full
	// title-number range instead of parsing VIDEO_TS.IFO in MattRip. This keeps
	// folder and ISO title discovery on the same libdvdread/libdvdnav path.
	maxTitle := 99

	var titles []titleInfo
	for n := 1; n <= maxTitle; n++ {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		progress(float64(n-1)/float64(maxTitle), fmt.Sprintf("Scanning DVD title %d of %d…", n, maxTitle))
		d, err := readDVDVideoTitleDuration(ctx, tools.ffprobe, src, n)
		if err != nil {
			continue
		}
		titles = append(titles, titleInfo{Number: n, Duration: d})
	}
	if len(titles) == 0 {
		return nil, errors.New("no readable DVD titles were found; the disc may be encrypted, damaged, or unsupported")
	}
	sort.Slice(titles, func(i, j int) bool { return titles[i].Number < titles[j].Number })
	progress(1, fmt.Sprintf("Found %d title(s).", len(titles)))
	return titles, nil
}

func longestTitle(titles []titleInfo) (titleInfo, error) {
	if len(titles) == 0 {
		return titleInfo{}, errors.New("no titles")
	}
	best := titles[0]
	for _, t := range titles[1:] {
		if t.Duration > best.Duration {
			best = t
		}
	}
	return best, nil
}
func readDVDVideoTitleDuration(ctx context.Context, ffprobe, src string, title int) (time.Duration, error) {
	d, err := readDVDVideoTitleDurationAttempt(ctx, ffprobe, src, title, false)
	if err == nil {
		return d, nil
	}
	// Some DVD titles do not expose a reliable duration until dvdvideo performs
	// its NAV-packet pre-index pass. Retry immediately in this same scan action;
	// users never need to press Scan Titles a second time.
	return readDVDVideoTitleDurationAttempt(ctx, ffprobe, src, title, true)
}

func readDVDVideoTitleDurationAttempt(ctx context.Context, ffprobe, src string, title int, preindex bool) (time.Duration, error) {
	child, cancel := context.WithTimeout(ctx, 25*time.Second)
	defer cancel()
	args := []string{"-v", "error", "-probesize", "100M", "-analyzeduration", "100M", "-fflags", "+genpts", "-f", "dvdvideo", "-title", strconv.Itoa(title)}
	if preindex {
		args = append(args, "-preindex", "1")
	}
	args = append(args, "-i", src, "-show_entries", "format=duration", "-of", "default=noprint_wrappers=1:nokey=1")
	out, err := runCommand(child, ffprobe, args...)
	if err != nil {
		return 0, err
	}
	for _, v := range strings.Fields(strings.TrimSpace(string(out))) {
		f, err := strconv.ParseFloat(v, 64)
		if err == nil && f > .1 {
			return time.Duration(f * float64(time.Second)), nil
		}
	}
	return 0, errors.New("no duration")
}

func probeStreams(ctx context.Context, ffprobe, src string, title int) (ffprobeResult, error) {
	var r ffprobeResult
	out, err := runCommand(ctx, ffprobe, "-v", "error", "-probesize", "100M", "-analyzeduration", "100M", "-fflags", "+genpts", "-f", "dvdvideo", "-title", strconv.Itoa(title), "-i", src, "-show_streams", "-of", "json")
	if err != nil {
		return r, fmt.Errorf("ffprobe metadata read failed: %w", err)
	}
	if err := json.Unmarshal(out, &r); err != nil {
		return r, fmt.Errorf("could not parse ffprobe metadata: %w", err)
	}
	return r, nil
}
func probeChapters(ctx context.Context, ffprobe, src string, title int) ([]DVDChapter, error) {
	var r ffprobeChapterResult
	out, err := runCommand(ctx, ffprobe, "-v", "error", "-probesize", "100M", "-analyzeduration", "100M", "-fflags", "+genpts", "-f", "dvdvideo", "-title", strconv.Itoa(title), "-preindex", "1", "-i", src, "-show_chapters", "-of", "json")
	if err != nil {
		return nil, fmt.Errorf("ffprobe chapter read failed: %w", err)
	}
	if err := json.Unmarshal(out, &r); err != nil {
		return nil, fmt.Errorf("could not parse ffprobe chapters: %w", err)
	}
	if len(r.Chapters) == 0 {
		return nil, errors.New("no DVD chapters were detected")
	}
	chs := make([]DVDChapter, 0, len(r.Chapters))
	for i, ch := range r.Chapters {
		start, err := secondsTextDuration(ch.StartTime)
		if err != nil {
			return nil, err
		}
		end, err := secondsTextDuration(ch.EndTime)
		if err != nil || end < start {
			return nil, fmt.Errorf("invalid chapter %d", i+1)
		}
		chs = append(chs, DVDChapter{Number: i + 1, Start: start, Duration: end - start})
	}
	return chs, nil
}
func detectChapters(ctx context.Context, ffprobe, src string, title int) ([]DVDChapter, string, error) {
	chs, err := probeChapters(ctx, ffprobe, src, title)
	if err != nil {
		return nil, "", err
	}
	return chs, "FFmpeg dvdvideo/libdvdread/libdvdnav pre-index", nil
}
func secondsTextDuration(s string) (time.Duration, error) {
	f, err := strconv.ParseFloat(strings.TrimSpace(s), 64)
	if err != nil || f < 0 {
		return 0, errors.New("invalid seconds value")
	}
	return time.Duration(f * float64(time.Second)), nil
}

func metadataText(ctx context.Context, src string, title titleInfo, tools toolPaths, preserve bool) (string, error) {
	src, err := normalizeSource(src)
	if err != nil {
		return "", err
	}
	probe, err := probeStreams(ctx, tools.ffprobe, src, title.Number)
	if err != nil {
		return "", err
	}
	chapters, source, chapterErr := detectChapters(ctx, tools.ffprobe, src, title.Number)
	var b strings.Builder
	fmt.Fprintf(&b, "MattRip — DVD title %d\nDuration: %s\n\n", title.Number, formatDuration(title.Duration))
	vn, an, sn := 0, 0, 0
	for _, s := range probe.Streams {
		lang := strings.TrimSpace(s.Tags["language"])
		name := strings.TrimSpace(s.Tags["title"])
		suffix := ""
		if lang != "" {
			suffix += "  [" + lang + "]"
		}
		if name != "" {
			suffix += "  " + name
		}
		switch s.CodecType {
		case "video":
			vn++
			fmt.Fprintf(&b, "Video %d: %s", vn, friendlyCodec(s.CodecName))
			if s.Width > 0 && s.Height > 0 {
				fmt.Fprintf(&b, "  %dx%d", s.Width, s.Height)
			}
			fmt.Fprintf(&b, "%s\n", suffix)
		case "audio":
			an++
			fmt.Fprintf(&b, "Audio %d: %s", an, friendlyCodec(s.CodecName))
			if s.Channels > 0 {
				fmt.Fprintf(&b, "  %d ch", s.Channels)
			}
			if s.ChannelLayout != "" {
				fmt.Fprintf(&b, " (%s)", s.ChannelLayout)
			}
			fmt.Fprintf(&b, "%s\n", suffix)
		case "subtitle":
			sn++
			fmt.Fprintf(&b, "Subtitle %d: %s%s\n", sn, friendlyCodec(s.CodecName), suffix)
		}
	}
	b.WriteString("\nChapters\n--------\n")
	if chapterErr != nil {
		fmt.Fprintf(&b, "Chapter timestamps unavailable: %v\n", chapterErr)
	} else {
		fmt.Fprintf(&b, "%d chapter(s) via %s; preserve=%t\n\n", len(chapters), source, preserve)
		for _, ch := range chapters {
			fmt.Fprintf(&b, "%2d  %s  %s\n", ch.Number, formatChapterTimestamp(ch.Start), formatChapterTimestamp(ch.Duration))
		}
	}
	if tools.mediainfo != "" {
		if target := mediaInfoTarget(src); target != "" {
			b.WriteString("\nMediaInfo source details\n------------------------\n")
			out, miErr := runCommand(ctx, tools.mediainfo, target)
			if miErr != nil {
				fmt.Fprintf(&b, "MediaInfo could not read source details: %s\n", strings.SplitN(miErr.Error(), "\n", 2)[0])
			} else if len(bytes.TrimSpace(out)) == 0 {
				b.WriteString("MediaInfo returned no source details.\n")
			} else {
				b.Write(out)
			}
		}
	}
	return b.String(), nil
}

func remuxTitle(ctx context.Context, src string, title titleInfo, outDir string, preserve bool, streamIndexes []int, tools toolPaths, progress progressFunc) (string, error) {
	src, err := normalizeSource(src)
	if err != nil {
		return "", err
	}
	if err := validateOutputDir(outDir); err != nil {
		return "", err
	}
	final := outputPath(src, outDir, title.Number)
	if _, err := os.Stat(final); err == nil {
		return "", fmt.Errorf("output already exists: %s", final)
	} else if !errors.Is(err, os.ErrNotExist) {
		return "", fmt.Errorf("check output path: %w", err)
	}
	partial, err := reservePartialOutput(final)
	if err != nil {
		return "", err
	}
	cleanupPartial := true
	defer func() {
		if cleanupPartial {
			_ = os.Remove(partial)
		}
	}()
	if progress == nil {
		progress = noopProgress
	}
	progress(0, fmt.Sprintf("Remuxing title %d with fixed timestamps…", title.Number))
	args := []string{"-hide_banner", "-nostdin", "-y"}
	args = appendDesktopDVDInput(args, title.Number, src)
	mapArgs, mapErr := ffmpegStreamMapArgs(streamIndexes)
	if mapErr != nil {
		return "", mapErr
	}
	args = append(args, mapArgs...)
	args = append(args, "-c", "copy", "-map_metadata", "0")
	if preserve {
		args = append(args, "-map_chapters", "0")
	} else {
		args = append(args, "-map_chapters", "-1")
	}
	args = append(args, "-progress", "pipe:1", "-nostats", partial)
	cmd := exec.CommandContext(ctx, tools.ffmpeg, args...)
	configureDVDLibrarySearch(cmd, tools.ffmpeg)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return "", err
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return "", err
	}
	if err := cmd.Start(); err != nil {
		return "", err
	}
	var errBuf bytes.Buffer
	done := make(chan struct{})
	go func() { _, _ = io.Copy(&errBuf, stderr); close(done) }()
	scanner := bufio.NewScanner(stdout)
	for scanner.Scan() {
		if ctx.Err() != nil {
			break
		}
		line := scanner.Text()
		if strings.HasPrefix(line, "out_time_us=") {
			us, _ := strconv.ParseInt(strings.TrimPrefix(line, "out_time_us="), 10, 64)
			if title.Duration > 0 && us > 0 {
				ratio := float64(time.Duration(us)*time.Microsecond) / float64(title.Duration)
				if ratio > .995 {
					ratio = .995
				}
				progress(ratio, fmt.Sprintf("Remuxing title %d with fixed timestamps… %d%%", title.Number, int(ratio*100)))
			}
		}
	}
	waitErr := cmd.Wait()
	<-done
	if ctx.Err() != nil {
		return "", ctx.Err()
	}
	if waitErr != nil {
		return "", fmt.Errorf("FFmpeg remux failed: %s", tail(errBuf.String(), 5000))
	}
	if err := validateAndSyncOutput(partial); err != nil {
		return "", err
	}
	if err := commitOutputNoReplace(partial, final); err != nil {
		cleanupPartial = false
		return "", fmt.Errorf("%w; completed MKV retained at %s", err, partial)
	}
	if err := syncDirectory(filepath.Dir(final)); err != nil {
		return "", fmt.Errorf("output committed to %s but directory sync failed: %w", final, err)
	}
	progress(1, "Completed: "+final)
	return final, nil
}

func runCommand(ctx context.Context, path string, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, path, args...)
	configureDVDLibrarySearch(cmd, path)
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
		return stdout.Bytes(), fmt.Errorf("%v: %s", err, detail)
	}
	return stdout.Bytes(), nil
}
func validateOutputDir(dir string) error {
	dir = filepath.Clean(strings.TrimSpace(dir))
	if dir == "." || dir == "" {
		return errors.New("choose an output folder")
	}
	abs, err := filepath.Abs(dir)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(abs, 0755); err != nil {
		return err
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
func outputPath(src, outDir string, title int) string {
	base := sanitizeFilename(dvdSourceBaseName(src))
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
	invalid := `<>:"/\\|?*`
	s = strings.Map(func(r rune) rune {
		if r < 32 || strings.ContainsRune(invalid, r) {
			return '_'
		}
		return r
	}, s)
	s = strings.TrimRight(s, ". ")
	return s
}
func mediaInfoTarget(src string) string {
	return dvdSourceMediaInfoTarget(src)
}
func friendlyCodec(s string) string {
	m := map[string]string{"mpeg2video": "MPEG-2 Video", "ac3": "Dolby Digital (AC-3)", "eac3": "Dolby Digital Plus (E-AC-3)", "dts": "DTS", "pcm_dvd": "PCM", "mp2": "MPEG Audio Layer II", "dvd_subtitle": "DVD Subtitle"}
	if v, ok := m[s]; ok {
		return v
	}
	if s == "" {
		return "Unknown"
	}
	return strings.ToUpper(s)
}
func formatDuration(d time.Duration) string {
	if d < 0 {
		d = 0
	}
	total := int64(d.Round(time.Second) / time.Second)
	h := total / 3600
	m := (total % 3600) / 60
	s := total % 60
	if h > 0 {
		return fmt.Sprintf("%d:%02d:%02d", h, m, s)
	}
	return fmt.Sprintf("%d:%02d", m, s)
}
func formatChapterTimestamp(d time.Duration) string {
	if d < 0 {
		d = 0
	}
	ms := d.Round(time.Millisecond).Milliseconds()
	h := ms / 3600000
	m := (ms % 3600000) / 60000
	s := (ms % 60000) / 1000
	return fmt.Sprintf("%02d:%02d:%02d.%03d", h, m, s, ms%1000)
}
func tail(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[len(s)-n:]
}
