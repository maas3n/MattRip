//go:build linux && !cli

package main

import (
	"context"
	"fmt"
	"strings"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/layout"
	"fyne.io/fyne/v2/widget"
)

func (g *linuxGUI) buildBatch() fyne.CanvasObject {
	input := widget.NewEntry()
	input.SetPlaceHolder("Folder containing movie title folders with VIDEO_TS subfolders")
	output := widget.NewEntry()
	output.SetPlaceHolder("Optional — blank writes each MKV into its movie title folder")
	progress := widget.NewProgressBar()
	status := widget.NewLabel("Choose the movie collection folder, then click BATCH REMUX.")
	status.Wrapping = fyne.TextWrapWord

	chooseInput := widget.NewButton("CHOOSE MOVIE FOLDER", func() {
		d := dialog.NewFolderOpen(func(uri fyne.ListableURI, err error) {
			if err != nil {
				dialog.ShowError(err, g.window)
				return
			}
			if uri != nil {
				input.SetText(uri.Path())
			}
		}, g.window)
		d.Show()
	})
	chooseOutput := widget.NewButton("CHOOSE OUTPUT FOLDER", func() {
		d := dialog.NewFolderOpen(func(uri fyne.ListableURI, err error) {
			if err != nil {
				dialog.ShowError(err, g.window)
				return
			}
			if uri != nil {
				output.SetText(uri.Path())
			}
		}, g.window)
		d.Show()
	})

	cancel := widget.NewButton("Cancel", g.cancelCurrent)
	oneClick := widget.NewButton("BATCH REMUX", func() {
		root := strings.TrimSpace(input.Text)
		if root == "" {
			dialog.ShowInformation("Choose a movie folder", "Choose the folder containing the movie title folders first.", g.window)
			return
		}
		outRoot := strings.TrimSpace(output.Text)
		g.startAsync("Running batch remux…", func(ctx context.Context) error {
			result, err := runBatch(ctx, batchOptions{InputRoot: root, OutputRoot: outRoot}, func(frac float64, text string) {
				fyne.Do(func() {
					if frac < 0 {
						frac = 0
					}
					if frac > 1 {
						frac = 1
					}
					progress.SetValue(frac)
					if text != "" {
						status.SetText(text)
					}
				})
			})
			fyne.Do(func() {
				if err == nil {
					status.SetText(fmt.Sprintf("Batch complete: %d movie(s) remuxed.", result.Completed))
					dialog.ShowInformation("Batch complete", fmt.Sprintf("Remuxed %d movie(s).", result.Completed), g.window)
				} else if ctx.Err() == nil {
					status.SetText(fmt.Sprintf("Batch finished with errors: %d completed, %d failed.", result.Completed, len(result.Failures)))
				}
			})
			return err
		})
	})
	oneClick.Importance = widget.HighImportance

	help := widget.NewLabel("BATCH REMUX scans each VIDEO_TS movie or unmounted ISO through FFmpeg dvdvideo (libdvdread/libdvdnav), automatically selects the longest title, includes all streams, preserves chapters, and remuxes with -analyzeduration 100M -probesize 100M -fflags +genpts. Leave Output Folder blank to place each completed MKV beside its ISO or VIDEO_TS folder.")
	help.Wrapping = fyne.TextWrapWord

	inputRow := container.NewBorder(nil, nil, nil, chooseInput, input)
	outputRow := container.NewBorder(nil, nil, nil, chooseOutput, output)
	actions := container.NewHBox(layout.NewSpacer(), oneClick, cancel)
	return container.NewPadded(container.NewVBox(
		widget.NewLabelWithStyle("BATCH", fyne.TextAlignLeading, fyne.TextStyle{Bold: true}),
		help,
		widget.NewSeparator(),
		widget.NewLabelWithStyle("Movie collection folder", fyne.TextAlignLeading, fyne.TextStyle{Bold: true}),
		inputRow,
		widget.NewLabel("Expected layout: selected folder / Movie Title / VIDEO_TS / VIDEO_TS.IFO"),
		widget.NewSeparator(),
		widget.NewLabelWithStyle("Output folder", fyne.TextAlignLeading, fyne.TextStyle{Bold: true}),
		outputRow,
		widget.NewLabel("Optional. Leave blank to write each MKV into its corresponding movie title folder."),
		widget.NewSeparator(),
		progress,
		status,
		layout.NewSpacer(),
		actions,
	))
}
