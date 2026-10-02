//go:build windows || linux

package main

import (
	"context"
	"os"
	"os/exec"
	"time"
)

func runInAppCLI(ctx context.Context, line string) (string, error) {
	args, err := splitCLICommand(line)
	if err != nil {
		return "", err
	}
	exe, err := os.Executable()
	if err != nil {
		return "", err
	}
	token, err := os.CreateTemp("", "mattrip-cli-cancel-*")
	if err != nil {
		return "", err
	}
	path := token.Name()
	token.Close()
	defer os.Remove(path)
	cmd := exec.CommandContext(ctx, exe, append([]string{"--cli"}, args...)...)
	cmd.Env = append(os.Environ(), "MATTMUX_CLI_CANCEL_FILE="+path)
	// Ask the embedded CLI to cancel its context, allowing FFmpeg shutdown and
	// owned partial-file cleanup before the subprocess exits.
	cmd.Cancel = func() error { return os.Remove(path) }
	cmd.WaitDelay = 10 * time.Second
	hideCLIWindow(cmd)
	out, err := cmd.CombinedOutput()
	return string(out), err
}
func watchCLICancellation(ctx context.Context, cancel context.CancelFunc) {
	path := os.Getenv("MATTMUX_CLI_CANCEL_FILE")
	if path == "" {
		return
	}
	go func() {
		tick := time.NewTicker(100 * time.Millisecond)
		defer tick.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-tick.C:
				if _, err := os.Stat(path); os.IsNotExist(err) {
					cancel()
					return
				}
			}
		}
	}()
}
