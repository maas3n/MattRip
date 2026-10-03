//go:build linux

package main

import "testing"

func TestLinuxDVDSourceClassifiesOpticalDevice(t *testing.T) {
	old := linuxOpticalDevice
	linuxOpticalDevice = func(path string) bool {
		return path == "/dev/sr9"
	}
	t.Cleanup(func() { linuxOpticalDevice = old })

	source, err := resolveDVDSource("/dev/sr9")
	if err != nil {
		t.Fatal(err)
	}
	if source.Kind != dvdSourcePhysicalDrive || source.Input != "/dev/sr9" || source.BaseName != "DVD-sr9" {
		t.Fatalf("unexpected physical source: %#v", source)
	}
	if got := dvdSourceBaseName("/dev/sr9"); got != "DVD-sr9" {
		t.Fatalf("physical DVD basename=%q", got)
	}
	if _, err := defaultDVDOutputDir("/dev/sr9"); err == nil {
		t.Fatal("physical DVD should require an explicit CLI output directory")
	}
}
