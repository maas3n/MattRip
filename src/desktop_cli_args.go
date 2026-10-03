//go:build linux || windows

package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

func parseCLIStreams(value string) ([]int, error) {
	if value == "" {
		return nil, nil
	}
	indexes := []int{}
	for _, s := range strings.Split(value, ",") {
		n, err := strconv.Atoi(strings.TrimSpace(s))
		if err != nil || n < 0 {
			return nil, fmt.Errorf("--streams requires comma-separated nonnegative stream indexes")
		}
		indexes = append(indexes, n)
	}
	return indexes, nil
}
func cliFlagsFirst(args []string) []string {
	var flags, positional []string
	for i := 0; i < len(args); i++ {
		a := args[i]
		if a == "--" {
			positional = append(positional, args[i+1:]...)
			break
		}
		if strings.HasPrefix(a, "-") {
			flags = append(flags, a)
			if a == "--title" || a == "-title" || a == "--output" || a == "-output" || a == "--streams" || a == "-streams" || a == "--log" || a == "-log" {
				if i+1 < len(args) {
					i++
					flags = append(flags, args[i])
				}
			}
		} else {
			positional = append(positional, a)
		}
	}
	return append(append(flags, "--"), positional...)
}
func defaultDVDOutputDir(src string) (string, error) {
	src, err := normalizeSource(src)
	if err != nil {
		return "", err
	}
	abs, err := filepath.Abs(src)
	if err != nil {
		return "", err
	}
	st, err := os.Stat(abs)
	if err != nil {
		return "", err
	}
	if !st.IsDir() || strings.EqualFold(filepath.Base(abs), "VIDEO_TS") {
		return filepath.Dir(abs), nil
	}
	return abs, nil
}

// Tokenize without a shell. Preserve Windows path backslashes literally.
func splitCLICommand(line string) ([]string, error) {
	var args []string
	var b strings.Builder
	var quote rune
	active := false
	for _, c := range line {
		if quote != 0 {
			if c == quote {
				quote = 0
			} else {
				b.WriteRune(c)
			}
			continue
		}
		if c == '"' || c == '\'' {
			quote = c
			active = true
			continue
		}
		if c == ' ' || c == '\t' || c == '\n' {
			if active {
				args = append(args, b.String())
				b.Reset()
				active = false
			}
			continue
		}
		active = true
		b.WriteRune(c)
	}
	if quote != 0 {
		return nil, fmt.Errorf("unclosed quote in command")
	}
	if active {
		args = append(args, b.String())
	}
	if len(args) > 0 && (args[0] == "mattrip-cli" || args[0] == "mattrip-cli.exe") {
		args = args[1:]
	}
	return args, nil
}
