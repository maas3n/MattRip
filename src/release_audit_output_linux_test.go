//go:build linux

package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"golang.org/x/sys/unix"
)

func auditRemux(t *testing.T, body string) (string, error, string) {
	t.Helper()
	root := t.TempDir()
	src := filepath.Join(root, "disc.iso")
	if err := os.WriteFile(src, []byte("test fixture"), 0600); err != nil {
		t.Fatal(err)
	}
	out := filepath.Join(root, "out")
	if err := os.Mkdir(out, 0700); err != nil {
		t.Fatal(err)
	}
	final := filepath.Join(out, "disc.mkv")
	t.Setenv("MATTRIP_AUDIT_FINAL", final)
	tool := filepath.Join(root, "ffmpeg-fixture")
	if err := os.WriteFile(tool, []byte("#!/bin/sh\nfor arg do last=\"$arg\"; done\n"+body), 0700); err != nil {
		t.Fatal(err)
	}
	f, err := remuxTitle(context.Background(), src, titleInfo{Number: 1, Duration: time.Second}, out, true, nil, toolPaths{ffmpeg: tool}, nil)
	return f, err, out
}

func TestRemuxRejectsEmptyOutput(t *testing.T) {
	f, err, out := auditRemux(t, ": > \"$last\"\n")
	if err == nil {
		t.Fatalf("reported success for zero-byte output: %s", f)
	}
	if _, statErr := os.Stat(filepath.Join(out, "disc.mkv")); !os.IsNotExist(statErr) {
		t.Fatalf("empty final output remains: %v", statErr)
	}
}

func TestRemuxPreservesNewlyAppearingDestination(t *testing.T) {
	f, err, out := auditRemux(t, "printf 'new output' > \"$last\"\nprintf 'other writer' > \"$MATTRIP_AUDIT_FINAL\"\n")
	b, readErr := os.ReadFile(filepath.Join(out, "disc.mkv"))
	if readErr != nil {
		t.Fatal(readErr)
	}
	if err == nil || string(b) != "other writer" {
		t.Fatalf("destination collision: final=%s err=%v content=%q", f, err, b)
	}
	entries, readErr := os.ReadDir(out)
	if readErr != nil {
		t.Fatal(readErr)
	}
	var recovered []byte
	for _, entry := range entries {
		if strings.HasSuffix(entry.Name(), ".partial.mkv") {
			recovered, readErr = os.ReadFile(filepath.Join(out, entry.Name()))
			if readErr != nil {
				t.Fatal(readErr)
			}
		}
	}
	if string(recovered) != "new output" {
		t.Fatalf("completed remux was not retained after publication collision: %q", recovered)
	}
}

func TestCommitFallsBackWhenRenameAndLinkAreUnsupported(t *testing.T) {
	root := t.TempDir()
	partial := filepath.Join(root, ".completed.partial.mkv")
	final := filepath.Join(root, "completed.mkv")
	if err := os.WriteFile(partial, []byte("completed output"), 0600); err != nil {
		t.Fatal(err)
	}
	oldRename, oldLink := renameOutputNoReplace, linkOutputNoReplace
	renameOutputNoReplace = func(string, string) error { return unix.EOPNOTSUPP }
	linkOutputNoReplace = func(string, string) error { return unix.EOPNOTSUPP }
	t.Cleanup(func() { renameOutputNoReplace, linkOutputNoReplace = oldRename, oldLink })

	if err := commitOutputNoReplace(partial, final); err != nil {
		t.Fatalf("fallback commit failed: %v", err)
	}
	got, err := os.ReadFile(final)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "completed output" {
		t.Fatalf("fallback output = %q", got)
	}
	if _, err := os.Stat(partial); !os.IsNotExist(err) {
		t.Fatalf("partial still exists after successful fallback: %v", err)
	}
}

func TestOutputPathDistinguishesAdditionalTitles(t *testing.T) {
	root := t.TempDir()
	src := filepath.Join(root, "disc.iso")
	if got := outputPath(src, root, 1); got != filepath.Join(root, "disc.mkv") {
		t.Fatalf("title 1 path = %q", got)
	}
	if got := outputPath(src, root, 2); got != filepath.Join(root, "disc-title-02.mkv") {
		t.Fatalf("title 2 path = %q", got)
	}
}

func TestRemuxFailureCleansOwnedPartial(t *testing.T) {
	_, err, out := auditRemux(t, "printf 'incomplete' > \"$last\"\nprintf 'disk full' >&2\nexit 1\n")
	if err == nil || !strings.Contains(err.Error(), "disk full") {
		t.Fatalf("expected useful error, got %v", err)
	}
	entries, readErr := os.ReadDir(out)
	if readErr != nil {
		t.Fatal(readErr)
	}
	if len(entries) != 0 {
		t.Fatalf("failed remux left output: %v", entries)
	}
}

func TestRemuxCancellationCleansOwnedPartial(t *testing.T) {
	root := t.TempDir()
	src := filepath.Join(root, "disc.iso")
	if err := os.WriteFile(src, []byte("fixture"), 0600); err != nil {
		t.Fatal(err)
	}
	out := filepath.Join(root, "out")
	if err := os.Mkdir(out, 0700); err != nil {
		t.Fatal(err)
	}
	tool := filepath.Join(root, "ffmpeg-fixture")
	if err := os.WriteFile(tool, []byte("#!/bin/sh\nfor arg do last=\"$arg\"; done\nprintf 'partial' > \"$last\"\nexec sleep 10\n"), 0700); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	_, err := remuxTitle(ctx, src, titleInfo{Number: 1}, out, true, nil, toolPaths{ffmpeg: tool}, nil)
	if err != context.DeadlineExceeded {
		t.Fatalf("expected deadline, got %v", err)
	}
	entries, readErr := os.ReadDir(out)
	if readErr != nil {
		t.Fatal(readErr)
	}
	if len(entries) != 0 {
		t.Fatalf("cancelled output remains: %v", entries)
	}
}
