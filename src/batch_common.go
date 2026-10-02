//go:build linux || windows

package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

type batchProgressFunc func(float64, string)

type batchMovie struct {
	Name   string
	Dir    string
	Source string
}

type batchOptions struct {
	InputRoot  string
	OutputRoot string
	Log        io.Writer
}

type batchFailure struct {
	Movie string
	Err   error
}

type batchResult struct {
	Total     int
	Completed int
	Outputs   []string
	Failures  []batchFailure
}

type batchDeps struct {
	ensureTools                  func(context.Context, batchProgressFunc) (toolPaths, error)
	discoverDVDTitlesViaDVDVideo func(context.Context, string, toolPaths, batchProgressFunc) ([]titleInfo, error)
	remuxTitle                   func(context.Context, string, titleInfo, string, toolPaths, batchProgressFunc) (string, error)
}

func discoverBatchMovies(root string) ([]batchMovie, error) {
	root = strings.TrimSpace(strings.Trim(root, "\""))
	if root == "" {
		return nil, errors.New("choose the folder containing the movie title folders")
	}
	abs, err := filepath.Abs(root)
	if err != nil {
		return nil, err
	}
	st, err := os.Stat(abs)
	if err != nil {
		return nil, fmt.Errorf("batch input folder does not exist: %s", abs)
	}
	if !st.IsDir() {
		return nil, errors.New("batch input must be a folder")
	}
	entries, err := os.ReadDir(abs)
	if err != nil {
		return nil, err
	}
	movies := make([]batchMovie, 0)
	for _, entry := range entries {
		if !entry.IsDir() {
			if strings.EqualFold(filepath.Ext(entry.Name()), ".iso") {
				info, err := entry.Info()
				if err == nil && info.Mode().IsRegular() {
					movies = append(movies, batchMovie{Name: strings.TrimSuffix(entry.Name(), filepath.Ext(entry.Name())), Dir: abs, Source: filepath.Join(abs, entry.Name())})
				}
			}
			continue
		}
		movieDir := filepath.Join(abs, entry.Name())
		videoTS := batchFindChildDirFold(movieDir, "VIDEO_TS")
		if videoTS == "" || !batchFileExistsFold(videoTS, "VIDEO_TS.IFO") {
			children, err := os.ReadDir(movieDir)
			if err != nil {
				return nil, fmt.Errorf("read movie folder %s: %w", movieDir, err)
			}
			for _, child := range children {
				if !child.IsDir() && strings.EqualFold(filepath.Ext(child.Name()), ".iso") {
					info, err := child.Info()
					if err == nil && info.Mode().IsRegular() {
						movies = append(movies, batchMovie{Name: strings.TrimSuffix(child.Name(), filepath.Ext(child.Name())), Dir: movieDir, Source: filepath.Join(movieDir, child.Name())})
					}
				}
			}
			continue
		}
		movies = append(movies, batchMovie{Name: entry.Name(), Dir: movieDir, Source: movieDir})
	}
	sort.Slice(movies, func(i, j int) bool {
		a, b := strings.ToLower(movies[i].Name), strings.ToLower(movies[j].Name)
		if a == b {
			return movies[i].Source < movies[j].Source
		}
		return a < b
	})
	if len(movies) == 0 {
		return nil, errors.New("no DVD movie folders containing VIDEO_TS/VIDEO_TS.IFO or unmounted ISO files were found")
	}
	return movies, nil
}

func batchFindChildDirFold(dir, name string) string {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return ""
	}
	for _, entry := range entries {
		if entry.IsDir() && strings.EqualFold(entry.Name(), name) {
			return filepath.Join(dir, entry.Name())
		}
	}
	return ""
}

func batchFileExistsFold(dir, name string) bool {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return false
	}
	for _, entry := range entries {
		if !entry.IsDir() && strings.EqualFold(entry.Name(), name) {
			return true
		}
	}
	return false
}

func batchOutputPath(movie batchMovie, outRoot string) string {
	dir := movie.Dir
	if outRoot != "" {
		dir = outRoot
	}
	base := sanitizeFilename(movie.Name)
	if base == "" {
		base = "DVD"
	}
	return filepath.Join(dir, base+".mkv")
}

func runBatch(ctx context.Context, opts batchOptions, progress batchProgressFunc) (batchResult, error) {
	return runBatchWithDeps(ctx, opts, progress, batchPlatformDeps())
}

func runBatchWithDeps(ctx context.Context, opts batchOptions, progress batchProgressFunc, deps batchDeps) (batchResult, error) {
	if progress == nil {
		progress = func(float64, string) {}
	}
	movies, err := discoverBatchMovies(opts.InputRoot)
	if err != nil {
		return batchResult{}, err
	}
	outRoot := strings.TrimSpace(strings.Trim(opts.OutputRoot, "\""))
	if outRoot != "" {
		outRoot, err = filepath.Abs(outRoot)
		if err != nil {
			return batchResult{}, err
		}
		if err := validateOutputDir(outRoot); err != nil {
			return batchResult{}, err
		}
	}
	writer := opts.Log
	if writer == nil {
		writer = io.Discard
	}
	logger := log.New(writer, "", log.LstdFlags)
	result := batchResult{Total: len(movies)}
	logger.Printf("MattRip batch start: input=%q output=%q movies=%d", opts.InputRoot, outRoot, len(movies))

	tools, err := deps.ensureTools(ctx, func(frac float64, status string) {
		progress(frac*.02, status)
	})
	if err != nil {
		return result, err
	}

	for index, movie := range movies {
		if err := ctx.Err(); err != nil {
			return result, err
		}
		base := float64(index) / float64(len(movies))
		span := 1.0 / float64(len(movies))
		logger.Printf("[%d/%d] scanning %s", index+1, len(movies), movie.Name)
		titles, scanErr := deps.discoverDVDTitlesViaDVDVideo(ctx, movie.Source, tools, func(frac float64, status string) {
			progress(base+span*(frac*.30), fmt.Sprintf("[%d/%d] %s — %s", index+1, len(movies), movie.Name, status))
		})
		if scanErr != nil {
			result.Failures = append(result.Failures, batchFailure{Movie: movie.Name, Err: scanErr})
			logger.Printf("[%d/%d] scan failed %s: %v", index+1, len(movies), movie.Name, scanErr)
			continue
		}
		best, bestErr := longestTitle(titles)
		if bestErr != nil {
			result.Failures = append(result.Failures, batchFailure{Movie: movie.Name, Err: bestErr})
			logger.Printf("[%d/%d] no usable title %s: %v", index+1, len(movies), movie.Name, bestErr)
			continue
		}
		logger.Printf("[%d/%d] selected longest title %d (%s) for %s", index+1, len(movies), best.Number, formatDuration(best.Duration), movie.Name)
		outDir := movie.Dir
		if outRoot != "" {
			outDir = outRoot
		}
		desired := batchOutputPath(movie, outRoot)
		if _, statErr := os.Stat(desired); statErr == nil {
			existsErr := fmt.Errorf("output already exists: %s", desired)
			result.Failures = append(result.Failures, batchFailure{Movie: movie.Name, Err: existsErr})
			logger.Printf("[%d/%d] skipped %s: %v", index+1, len(movies), movie.Name, existsErr)
			continue
		} else if !errors.Is(statErr, os.ErrNotExist) {
			result.Failures = append(result.Failures, batchFailure{Movie: movie.Name, Err: statErr})
			logger.Printf("[%d/%d] output check failed %s: %v", index+1, len(movies), movie.Name, statErr)
			continue
		}
		final, muxErr := deps.remuxTitle(ctx, movie.Source, best, outDir, tools, func(frac float64, status string) {
			progress(base+span*(.30+frac*.70), fmt.Sprintf("[%d/%d] %s — %s", index+1, len(movies), movie.Name, status))
		})
		if muxErr != nil {
			result.Failures = append(result.Failures, batchFailure{Movie: movie.Name, Err: muxErr})
			logger.Printf("[%d/%d] remux failed %s: %v", index+1, len(movies), movie.Name, muxErr)
			continue
		}
		if filepath.Clean(final) != filepath.Clean(desired) {
			if moveErr := commitOutputNoReplace(final, desired); moveErr != nil {
				moveErr = fmt.Errorf("could not place completed MKV at %s: %w; completed MKV retained at %s", desired, moveErr, final)
				result.Failures = append(result.Failures, batchFailure{Movie: movie.Name, Err: moveErr})
				logger.Printf("[%d/%d] final placement failed %s: %v", index+1, len(movies), movie.Name, moveErr)
				continue
			}
			final = desired
		}
		result.Completed++
		result.Outputs = append(result.Outputs, final)
		logger.Printf("[%d/%d] completed %s -> %s", index+1, len(movies), movie.Name, final)
		progress(float64(index+1)/float64(len(movies)), fmt.Sprintf("Completed %d of %d movie(s).", index+1, len(movies)))
	}

	logger.Printf("MattRip batch complete: completed=%d failed=%d total=%d", result.Completed, len(result.Failures), result.Total)
	if len(result.Failures) > 0 {
		parts := make([]string, 0, len(result.Failures))
		for _, failure := range result.Failures {
			parts = append(parts, fmt.Sprintf("%s: %v", failure.Movie, failure.Err))
		}
		return result, fmt.Errorf("batch completed with %d failure(s): %s", len(result.Failures), strings.Join(parts, "; "))
	}
	progress(1, fmt.Sprintf("Batch complete: %d movie(s) remuxed.", result.Completed))
	return result, nil
}
