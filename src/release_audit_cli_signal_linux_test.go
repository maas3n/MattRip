//go:build linux && !cli

package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

func TestCLIInterruptCleansOwnedPartial(t *testing.T) {
	root := t.TempDir()
	cli := filepath.Join(root, "mattrip-cli")
	build := exec.Command("go", "build", "-tags", "cli", "-o", cli, ".")
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build CLI: %v\n%s", err, out)
	}

	bin := filepath.Join(root, "bin")
	if err := os.Mkdir(bin, 0700); err != nil {
		t.Fatal(err)
	}
	ffmpeg := `#!/bin/sh
for arg in "$@"; do
    if [ "$arg" = "-demuxers" ]; then
        printf ' D  dvdvideo\n'
        exit 0
    fi
    last="$arg"
done
printf 'partial' > "$last"
exec sleep 30
`
	if err := os.WriteFile(filepath.Join(bin, "ffmpeg"), []byte(ffmpeg), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(bin, "ffprobe"), []byte("#!/bin/sh\nprintf '60\\n'\n"), 0700); err != nil {
		t.Fatal(err)
	}

	src := filepath.Join(root, "disc.iso")
	if err := os.WriteFile(src, []byte("fixture"), 0600); err != nil {
		t.Fatal(err)
	}
	outDir := filepath.Join(root, "out")
	if err := os.Mkdir(outDir, 0700); err != nil {
		t.Fatal(err)
	}

	cmd := exec.Command(cli, "remux", "--title", "1", "--output", outDir, src)
	cmd.Env = append(os.Environ(), "PATH="+bin+":"+os.Getenv("PATH"))
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}

	deadline := time.Now().Add(8 * time.Second)
	found := false
	for time.Now().Before(deadline) {
		matches, _ := filepath.Glob(filepath.Join(outDir, ".mattrip-*.partial.mkv"))
		if len(matches) > 0 {
			found = true
			break
		}
		time.Sleep(25 * time.Millisecond)
	}
	if !found {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
		t.Fatal("CLI did not create a partial output before interrupt")
	}

	if err := cmd.Process.Signal(os.Interrupt); err != nil {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
		t.Fatal(err)
	}
	if err := cmd.Wait(); err == nil {
		t.Fatal("interrupted CLI unexpectedly exited successfully")
	}
	entries, err := os.ReadDir(outDir)
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if filepath.Ext(entry.Name()) == ".mkv" || filepath.Base(entry.Name()) != "" && len(entry.Name()) > 0 && filepath.Clean(entry.Name()) == entry.Name() {
			if matched, _ := filepath.Match(".mattrip-*.partial.mkv", entry.Name()); matched {
				t.Fatalf("Ctrl+C left partial output %s", entry.Name())
			}
		}
	}
}
