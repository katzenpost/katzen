package main

import (
	"runtime"

	"gioui.org/io/event"
	"gioui.org/io/key"
	"gioui.org/layout"
)

// helper to capture back / escape presses from any page
func backEvent(gtx layout.Context) bool {
	filters := []event.Filter{
		key.Filter{Name: key.NameEscape},
		key.Filter{Name: key.NameBack},
	}
	if ke, ok := gtx.Event(filters...); ok {
		switch ke := ke.(type) {
		case key.Event:
			if runtime.GOOS == "android" || ke.State == key.Release {
				return true
			}
		}
	}
	return false
}

// helper to capture hotkeys used in various pages
func shortcutEvents(gtx layout.Context) (key.Event, bool) {
	// hotkeys
	filters := []event.Filter{
		key.Filter{Name: key.NameF1},
		key.Filter{Name: key.NameF2},
		key.Filter{Name: key.NameF3},
		key.Filter{Name: key.NameF4},
		key.Filter{Name: key.NameF5},
		key.Filter{Name: key.NameUpArrow},
		key.Filter{Name: key.NameDownArrow},
		key.Filter{Name: key.NamePageUp},
		key.Filter{Name: key.NamePageDown},
		key.Filter{Name: key.NameReturn},
	}
	if ke, ok := gtx.Event(filters...); ok {
		switch ke := ke.(type) {
		case key.Event:
			// if on android key.Release isn't implemented
			if runtime.GOOS == "android" || ke.State == key.Press {
				return ke, true
			}
		}
	}
	return key.Event{}, false
}
