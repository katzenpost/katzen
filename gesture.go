package main

import (
	"time"

	"gioui.org/io/pointer"
	"gioui.org/layout"
)

// LongPressType represents a successful or cancelled LongPress action.
type LongPressType uint8

const (
	LongPressed LongPressType = iota
	LongPressCancelled
)

// LongPressEvent represent a long press action
type LongPressEvent struct {
	Type LongPressType
}

// LongPress detects a press-and-hold in the form of LongPress events
type LongPress struct {
	pressedAt time.Time
	// releasedAt tracks the pointer
	releasedAt time.Time
	// pressedFor tracks how long the press has been held so far.
	pressedFor time.Duration
	// detectAt tracks how long the press must be held.
	detectAt time.Duration
	// pressing tracks whether a press is occurring
	pressed bool
	timer   *time.Timer
	// callback is called when the timer fires
	callback func()
}

// Update returns the next click event, if any.
func (l *LongPress) Update(gtx layout.Context) (*LongPressEvent, bool) {
	// consume pointer events and start or stop a timer
	filt := pointer.Filter{Target: l, Kinds: pointer.Press | pointer.Release | pointer.Leave}
	if e, ok := gtx.Event(filt); ok {
		if e, ok := e.(pointer.Event); ok {
			switch e.Kind {
			case pointer.Press:
				l.pressedAt = time.Now()
				l.timer = time.NewTimer(l.detectAt)
				time.AfterFunc(l.detectAt, l.callback)
				l.pressed = true
			case pointer.Cancel, pointer.Release, pointer.Leave:
				if l.pressed {
					l.pressed = false
					l.pressedFor = time.Now().Sub(l.pressedAt)
					if !l.timer.Stop() {
						<-l.timer.C
					}
					l.timer = nil
					return &LongPressEvent{Type: LongPressCancelled}, true
				}
			}
		}
	}

	// check if the timer has fired return a LongPressEvent
	if l.timer != nil {
		select {
		case t := <-l.timer.C:
			l.pressedFor = t.Sub(l.pressedAt)
			if l.pressed {
				l.pressed = false
				l.pressedFor = time.Now().Sub(l.pressedAt)
				return &LongPressEvent{Type: LongPressed}, true
			}
		default:
		}
	}

	return &LongPressEvent{}, false
}

// NewLongPress returns a LongPress that triggers after the duration
func NewLongPress(cb func(), duration time.Duration) *LongPress {
	return &LongPress{detectAt: duration, callback: cb}
}
