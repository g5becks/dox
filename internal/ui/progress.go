package ui

import "github.com/jedib0t/go-pretty/v6/progress"

func NewProgressWriter() progress.Writer {
	writer := progress.NewWriter()
	writer.SetAutoStop(true)
	writer.SetTrackerLength(30)
	writer.SetStyle(progress.StyleBlocks)
	writer.Style().Visibility.ETA = true
	writer.Style().Visibility.Speed = true
	writer.Style().Visibility.Value = true

	return writer
}
