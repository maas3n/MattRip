package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"debug/elf"
	"embed"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"time"
)

var appVersion = "dev"

// The release script stages the built GUI and pinned multimedia tools here
// immediately before compiling this launcher. The resulting ELF is a single
// self-extracting MattRip executable.
//
//go:embed all:payload
var payloadFS embed.FS

type payloadFile struct {
	name string
	mode fs.FileMode
}

var payload []payloadFile

func indexPayload() error {
	return fs.WalkDir(payloadFS, "payload", func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			return nil
		}
		name := strings.TrimPrefix(path, "payload/")
		mode := fs.FileMode(0o644)
		if !strings.Contains(name, "/") {
			mode = 0o755
		}
		payload = append(payload, payloadFile{name: name, mode: mode})
		return nil
	})
}

func main() {
	if runtime.GOOS != "linux" || runtime.GOARCH != "amd64" {
		fatal("this standalone build supports Linux amd64 only")
	}
	if err := indexPayload(); err != nil {
		fatal(err.Error())
	}

	root, err := extractPayload()
	if err != nil {
		fatal(err.Error())
	}
	env := runtimeEnv(root)

	if len(os.Args) > 1 && os.Args[1] == "--standalone-self-test" {
		if err := selfTest(root, env); err != nil {
			fatal(err.Error())
		}
		fmt.Printf("MattRip %s standalone self-test: OK\n", appVersion)
		return
	}

	app := filepath.Join(root, "mattrip-bin")
	userArgs := os.Args[1:]
	if len(userArgs) > 0 {
		switch userArgs[0] {
		case "--cli":
			app = filepath.Join(root, "mattrip-cli-bin")
			userArgs = userArgs[1:]
		case "scan", "metadata", "remux", "--batch", "--version", "--help", "tools", "doctor":
			app = filepath.Join(root, "mattrip-cli-bin")
		}
	}
	if app == filepath.Join(root, "mattrip-bin") && os.Getenv("DISPLAY") != "" {
		env, err = graphicsEnv(root, env)
		if err != nil {
			fatal(err.Error())
		}
	}
	args := append([]string{app}, userArgs...)
	if err := syscall.Exec(app, args, env); err != nil {
		fatal(fmt.Sprintf("could not start MattRip: %v", err))
	}
}

func softwareEnv(root string, env []string) []string {
	env = replaceEnv(env, "LD_LIBRARY_PATH", filepath.Join(root, "software")+":"+filepath.Join(root, "lib"))
	env = replaceEnv(env, "LIBGL_DRIVERS_PATH", filepath.Join(root, "software", "dri"))
	env = replaceEnv(env, "LIBGL_ALWAYS_SOFTWARE", "1")
	env = replaceEnv(env, "GALLIUM_DRIVER", "llvmpipe")
	env = replaceEnv(env, "__GLX_VENDOR_LIBRARY_NAME", "mesa")
	return env
}

func graphicsEnv(root string, env []string) ([]string, error) {
	probe := func(candidate []string) ([]byte, error) {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		cmd := exec.CommandContext(ctx, filepath.Join(root, "graphics-probe"))
		cmd.Env = candidate
		return cmd.CombinedOutput()
	}
	first, err := probe(env)
	if err == nil {
		return env, nil
	}
	fallback := softwareEnv(root, env)
	second, err := probe(fallback)
	if err != nil {
		return nil, fmt.Errorf("could not initialize OpenGL using either the system driver or bundled software renderer. Check that your desktop/WSLg display is available.\nSystem: %s\nSoftware: %s", first, second)
	}
	fmt.Fprintln(os.Stderr, "MattRip: using bundled software rendering for this display")
	return fallback, nil
}

func runtimeEnv(root string) []string {
	env := os.Environ()
	for key, dir := range map[string]string{"PATH": root, "LD_LIBRARY_PATH": filepath.Join(root, "lib")} {
		if old := os.Getenv(key); old != "" {
			dir += string(os.PathListSeparator) + old
		}
		env = replaceEnv(env, key, dir)
	}
	return env
}

func extractPayload() (string, error) {
	cache, err := os.UserCacheDir()
	if err != nil {
		return "", fmt.Errorf("could not find user cache directory: %w", err)
	}
	if strings.TrimSpace(cache) == "" {
		return "", errors.New("user cache directory is empty")
	}

	fingerprint, err := payloadFingerprint()
	if err != nil {
		return "", err
	}
	root := filepath.Join(cache, "mattrip", "standalone", appVersion+"-"+fingerprint[:16])
	if err := os.MkdirAll(root, 0o700); err != nil {
		return "", fmt.Errorf("could not create standalone cache: %w", err)
	}

	for _, p := range payload {
		data, err := payloadFS.ReadFile("payload/" + p.name)
		if err != nil {
			return "", fmt.Errorf("embedded %s is missing: %w", p.name, err)
		}
		dst := filepath.Join(root, p.name)
		if err := os.MkdirAll(filepath.Dir(dst), 0o700); err != nil {
			return "", err
		}
		if ok, err := fileMatches(dst, data, p.mode); err == nil && ok {
			continue
		}
		if err := writeAtomic(dst, data, p.mode); err != nil {
			return "", fmt.Errorf("could not extract %s: %w", p.name, err)
		}
	}
	return root, nil
}

func payloadFingerprint() (string, error) {
	h := sha256.New()
	for _, p := range payload {
		data, err := payloadFS.ReadFile("payload/" + p.name)
		if err != nil {
			return "", err
		}
		_, _ = io.WriteString(h, p.name)
		_, _ = h.Write([]byte{0})
		_, _ = h.Write(data)
		_, _ = h.Write([]byte{0})
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

func fileMatches(path string, want []byte, mode fs.FileMode) (bool, error) {
	st, err := os.Stat(path)
	if err != nil {
		return false, err
	}
	if !st.Mode().IsRegular() || st.Size() != int64(len(want)) || st.Mode().Perm() != mode.Perm() {
		return false, nil
	}
	got, err := os.ReadFile(path)
	if err != nil {
		return false, err
	}
	return bytes.Equal(got, want), nil
}

func writeAtomic(path string, data []byte, mode fs.FileMode) error {
	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, ".extract-*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)
	if err := tmp.Chmod(mode); err != nil {
		tmp.Close()
		return err
	}
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmpName, path)
}

func replaceEnv(env []string, key, value string) []string {
	prefix := key + "="
	out := make([]string, 0, len(env)+1)
	for _, item := range env {
		if strings.HasPrefix(item, prefix) {
			continue
		}
		out = append(out, item)
	}
	return append(out, prefix+value)
}

func selfTest(root string, env []string) error {
	// Ask the host ELF loader to resolve the GUI and bundled libraries. This
	// catches missing GUI dependencies without requiring a running display or ldd.
	app := filepath.Join(root, "mattrip-bin")
	binary, err := elf.Open(app)
	if err != nil {
		return fmt.Errorf("invalid GUI ELF executable: %w", err)
	}
	defer binary.Close()
	var loader string
	for _, prog := range binary.Progs {
		if prog.Type == elf.PT_INTERP {
			data, err := io.ReadAll(prog.Open())
			if err != nil {
				return err
			}
			loader = strings.TrimRight(string(data), "\x00")
		}
	}
	if loader != "" {
		for _, p := range payload {
			if p.name != "mattrip-bin" && p.name != "graphics-probe" && !strings.HasPrefix(p.name, "lib/") && !strings.HasPrefix(p.name, "software/") {
				continue
			}
			cmd := exec.Command(loader, "--list", filepath.Join(root, p.name))
			cmd.Env = env
			if strings.HasPrefix(p.name, "software/") {
				cmd.Env = softwareEnv(root, env)
			}
			out, err := cmd.CombinedOutput()
			if err != nil || bytes.Contains(out, []byte("not found")) {
				return fmt.Errorf("%s dependency check failed: %v: %s", p.name, err, strings.TrimSpace(string(out)))
			}
		}
	}
	checks := [][]string{
		{filepath.Join(root, "mattrip-cli-bin"), "--help"},
		{filepath.Join(root, "ffmpeg"), "-hide_banner", "-version"},
		{filepath.Join(root, "ffprobe"), "-hide_banner", "-version"},
		{filepath.Join(root, "mediainfo"), "--Version"},
	}
	for _, args := range checks {
		cmd := exec.Command(args[0], args[1:]...)
		cmd.Env = env
		out, err := cmd.CombinedOutput()
		if err != nil {
			return fmt.Errorf("%s self-test failed: %v: %s", filepath.Base(args[0]), err, strings.TrimSpace(string(out)))
		}
	}
	return nil
}

func fatal(msg string) {
	fmt.Fprintln(os.Stderr, "MattRip standalone:", msg)
	os.Exit(1)
}
