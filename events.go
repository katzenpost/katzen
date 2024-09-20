//go:build !android

package main

import (
	"gioui.org/app"
	"gioui.org/io/key"
	"errors"
)

// handleGioEvents 
func (a *App) handleGioEvents(e interface{}) error {
	switch e := e.(type) {
	case key.FocusEvent:
		// XXX: figure out what this is useful for
	case app.DestroyEvent:
		return errors.New("system.DestroyEvent receieved")
	case app.FrameEvent:
		gtx := app.NewContext(a.ops, e)
		a.Layout(gtx)
		e.Frame(gtx.Ops)
	}
	return nil
}
