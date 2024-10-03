//go:build android

package main

import (
	"errors"
	"gioui.org/app"
	"gioui.org/io/key"
)

// handleGioEvents starts and stops the android foreground service when
// AndroidViewEvents indicate that the application has a view or not.
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
	case app.AndroidViewEvent:
		if e.View == 0 {
			return errors.New("app.AndroidViewEvent nil received")
		}
	}
	return nil
}
