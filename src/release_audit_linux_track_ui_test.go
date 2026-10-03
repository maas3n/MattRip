//go:build linux

package main

import (
	"os"
	"strings"
	"testing"
)

// This source-level release audit deliberately runs under -tags cli, where the
// Fyne desktop entry point itself is excluded. Linux release packaging invokes
// go test -tags cli before building the GUI, so this prevents a future release
// from silently reverting the track selector to a detached top-level window or
// rendering metadata with Fyne's low-contrast disabled-entry foreground.
func TestLinuxReleaseKeepsModalTrackSelector(t *testing.T) {
	b, err := os.ReadFile("main_linux.go")
	if err != nil {
		t.Fatal(err)
	}
	src := string(b)
	for _, required := range []string{
		`widget.NewButton("Show Metadata"`,
		`dialog.NewCustomWithoutButtons(`,
		`g.window`,
		`Close & use selection`,
		`selectedTrackIndexes(`,
		`Tracks: %d selected for title %d`,
		`widget.NewLabel(detailsText)`,
		`container.NewVScroll(container.NewPadded(details))`,
	} {
		if !strings.Contains(src, required) {
			t.Fatalf("Linux release track-selection UI is missing %q", required)
		}
	}
	if strings.Contains(src, `fyne.CurrentApp().NewWindow(fmt.Sprintf("MattRip — Title`) {
		t.Fatal("Linux track selector must remain parented to the main window, not a detached OS window")
	}
	if strings.Contains(src, `details.Disable()`) {
		t.Fatal("Linux metadata must not use a disabled Entry because disabled theme text can be unreadable")
	}
}
