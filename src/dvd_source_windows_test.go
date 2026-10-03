//go:build windows

package main

import "testing"

func TestWindowsDriveLetterParsing(t *testing.T) {
	for _, input := range []string{"D:", "d:", "D:\\", "d:/", "\"E:\\\""} {
		letter, ok := windowsDriveLetter(input)
		if !ok {
			t.Fatalf("expected %q to parse", input)
		}
		if input[0] == 'E' && letter != 'E' {
			t.Fatalf("got %c for %q", letter, input)
		}
	}
	for _, input := range []string{"", "DVD", "D:\\folder", "1:", "/dev/sr0"} {
		if _, ok := windowsDriveLetter(input); ok {
			t.Fatalf("did not expect %q to parse as a Windows drive root", input)
		}
	}
}

func TestWindowsDVDSourceClassifiesOpticalDrive(t *testing.T) {
	old := windowsDriveType
	windowsDriveType = func(letter byte) uintptr {
		if letter == 'D' {
			return driveCDROM
		}
		return 3 // DRIVE_FIXED
	}
	t.Cleanup(func() { windowsDriveType = old })

	source, err := resolveDVDSource("D:")
	if err != nil {
		t.Fatal(err)
	}
	if source.Kind != dvdSourcePhysicalDrive || source.Input != "D:" || source.BaseName != "DVD-D" {
		t.Fatalf("unexpected physical source: %#v", source)
	}
	if got := dvdSourceBaseName("D:"); got != "DVD-D" {
		t.Fatalf("physical DVD basename=%q", got)
	}
	if _, err := defaultDVDOutputDir("D:"); err == nil {
		t.Fatal("physical DVD should require an explicit CLI output directory")
	}
}

func TestWindowsDVDDriveEnumerationUsesOpticalTypesOnly(t *testing.T) {
	old := windowsDriveType
	windowsDriveType = func(letter byte) uintptr {
		switch letter {
		case 'E', 'G':
			return driveCDROM
		default:
			return 3
		}
	}
	t.Cleanup(func() { windowsDriveType = old })

	drives, err := platformDVDDrives()
	if err != nil {
		t.Fatal(err)
	}
	if len(drives) != 2 || drives[0].Input != "E:" || drives[1].Input != "G:" {
		t.Fatalf("unexpected drives: %#v", drives)
	}
}
