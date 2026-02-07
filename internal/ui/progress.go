package ui

import "github.com/jedib0t/go-pretty/v6/progress"

const trackerLength = 30

func NewProgressWriter() progress.Writer {
	writer := progress.NewWriter()
	writer.SetAutoStop(true)
	writer.SetTrackerLength(trackerLength)
	writer.SetStyle(progress.StyleBlocks)
	writer.Style().Visibility.ETA = true
	writer.Style().Visibility.Speed = true
	writer.Style().Visibility.Value = true

	return writer
}
