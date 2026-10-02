//go:build linux && !cli

package main

import (
	"context"
	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/widget"
)

func (g *linuxGUI) buildCLI() fyne.CanvasObject {
	command := widget.NewEntry()
	command.SetText("mattrip-cli --help")
	output := widget.NewMultiLineEntry()
	output.Wrapping = fyne.TextWrapWord
	var cancel context.CancelFunc
	var run *widget.Button
	stop := widget.NewButton("Cancel", func() {
		if cancel != nil {
			cancel()
		}
	})
	stop.Disable()
	run = widget.NewButton("Run", func() {
		line := command.Text
		ctx, c := context.WithCancel(context.Background())
		cancel = c
		command.Disable()
		run.Disable()
		stop.Enable()
		output.SetText("Running…")
		go func() {
			text, err := runInAppCLI(ctx, line)
			c()
			fyne.Do(func() {
				if err != nil {
					text += "\n" + err.Error()
				}
				output.SetText(text)
				command.Enable()
				run.Enable()
				stop.Disable()
				cancel = nil
			})
		}()
	})
	return container.NewBorder(container.NewVBox(widget.NewLabel("MattRip CLI — scan, metadata, remux, --batch; quote paths containing spaces."), command, container.NewHBox(run, stop)), nil, nil, nil, output)
}
