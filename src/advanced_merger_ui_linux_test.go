//go:build linux && !cli

package main

import (
	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/test"
	"fyne.io/fyne/v2/widget"
	"image/png"
	"os"
	"testing"
)

func TestAdvancedMergerAndBatchTabs(t *testing.T) {
	a := test.NewApp()
	defer a.Quit()
	w := a.NewWindow("test")
	defer w.Close()
	g := &linuxGUI{window: w}
	g.build()
	tabs, ok := w.Content().(*container.AppTabs)
	if !ok {
		t.Fatal("missing tabs")
	}
	if len(tabs.Items) != 4 {
		t.Fatalf("tabs = %d; want 4", len(tabs.Items))
	}
	if tabs.Items[0].Text != "REMUX/DEMUX" {
		t.Fatal("missing REMUX/DEMUX tab")
	}
	if tabs.Items[1].Text != "ADVANCED" {
		t.Fatal("missing ADVANCED tab")
	}
	if tabs.Items[2].Text != "BATCH" {
		t.Fatal("missing BATCH tab")
	}

	var scanSelect, remux, mainDemux bool
	walkLinuxCanvas(tabs.Items[0].Content, func(obj fyne.CanvasObject) {
		if button, ok := obj.(*widget.Button); ok {
			switch button.Text {
			case "SCAN/SELECT STREAMS":
				scanSelect = true
			case "REMUX":
				remux = true
			case "DEMUX":
				mainDemux = true
			}
		}
	})
	if !scanSelect || !remux || !mainDemux {
		t.Fatal("REMUX/DEMUX action buttons are incomplete")
	}
	batchRemux := false
	walkLinuxCanvas(tabs.Items[2].Content, func(obj fyne.CanvasObject) {
		if button, ok := obj.(*widget.Button); ok && button.Text == "BATCH REMUX" {
			batchRemux = true
		}
	})
	if !batchRemux {
		t.Fatal("BATCH tab is missing BATCH REMUX")
	}

	var media, dvdDrive, audioStreams, subtitleStreams, chapterFile, demux, mux bool
	var activity *widget.ProgressBarInfinite
	walkLinuxCanvas(tabs.Items[1].Content, func(obj fyne.CanvasObject) {
		switch o := obj.(type) {
		case *widget.Button:
			switch o.Text {
			case "MEDIA(All streams included)":
				media = true
			case "DVD DRIVE":
				dvdDrive = true
			case "ADD AUDIO(Only audio streams will be included)":
				audioStreams = true
			case "ADD SUBTITLE(Only subtitle streams will be Included)":
				subtitleStreams = true
			case "ADD CHAPTER .txt FILE(FFMETADATA1 Format)":
				chapterFile = true
			case "DEMUX":
				demux = true
			case "MUX":
				mux = true
			}
		case *widget.ProgressBarInfinite:
			activity = o
		}
	})
	if !media || !dvdDrive || !audioStreams || !subtitleStreams || !chapterFile {
		t.Fatal("Advanced Merger source buttons are incomplete")
	}
	if !demux || !mux {
		t.Fatal("Advanced Merger is missing its MUX/DEMUX buttons")
	}
	if activity == nil {
		t.Fatal("Advanced Merger is missing its activity progress bar")
	}
	if activity.Visible() {
		t.Fatal("Advanced Merger activity progress bar should be hidden while idle")
	}

	w.Resize(fyne.NewSize(840, 620))
	tabs.SelectIndex(2)
	if path := os.Getenv("MATTRIP_UI_CAPTURE"); path != "" {
		f, err := os.Create(path)
		if err != nil {
			t.Fatal(err)
		}
		defer f.Close()
		if err = png.Encode(f, w.Canvas().Capture()); err != nil {
			t.Fatal(err)
		}
	}
	if tabs.SelectedIndex() != 2 {
		t.Fatal("cannot select BATCH tab")
	}
}

func walkLinuxCanvas(obj fyne.CanvasObject, visit func(fyne.CanvasObject)) {
	if obj == nil {
		return
	}
	visit(obj)
	if c, ok := obj.(*fyne.Container); ok {
		for _, child := range c.Objects {
			walkLinuxCanvas(child, visit)
		}
	}
}
