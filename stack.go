// SPDX-License-Identifier: Unlicense OR MIT
/*
these methods were implemented by Elias Naur <mail@eliasnaur.com>
for the scatter.im project https://git.sr.ht/~eliasnaur/scatter
*/
package main

import (
	"image"

	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/op/clip"
	"gioui.org/op/paint"
	"gioui.org/unit"
	"image/color"
)

type pageStack struct {
	pages    []Page
	stopChan chan<- struct{}
}

type Page interface {
	Start(stop <-chan struct{})
	Event(gtx layout.Context) interface{}
	Layout(gtx layout.Context) layout.Dimensions
}

type Background struct {
	Color  color.NRGBA
	Radius unit.Dp
	Inset  layout.Inset
}

type clipCircle struct {
}

func (cc *clipCircle) Layout(gtx layout.Context, w layout.Widget) layout.Dimensions {
	macro := op.Record(gtx.Ops)
	dims := w(gtx)
	call := macro.Stop()
	max := dims.Size.X
	if dy := dims.Size.Y; dy > max {
		max = dy
	}
	rr := max >> 1
	t := clip.RRect{
		Rect: image.Rectangle{Max: image.Point{X: max, Y: max}},
		NE:   rr, NW: rr, SE: rr, SW: rr,
	}.Push(gtx.Ops)
	call.Add(gtx.Ops)
	t.Pop()
	return dims
}

func (b *Background) Layout(gtx layout.Context, w layout.Widget) layout.Dimensions {
	macro := op.Record(gtx.Ops)
	dims := b.Inset.Layout(gtx, w)
	call := macro.Stop()
	size := dims.Size
	if r := gtx.Dp(b.Radius); r > 0 {
		if r > size.X/2 {
			r = size.X >> 1
		}
		if r > size.Y/2 {
			r = size.Y >> 1
		}
		t := clip.RRect{
			Rect: image.Rectangle{Max: image.Point{
				X: size.X, Y: size.Y,
			}}, NW: r, NE: r, SW: r, SE: r,
		}.Push(gtx.Ops)
		defer t.Pop()
	}
	paint.FillShape(gtx.Ops, b.Color, clip.Rect(image.Rectangle{Max: size}).Op())
	call.Add(gtx.Ops)
	return dims
}

type RedrawEvent struct{}
type BackEvent struct{}

type fill struct {
	color color.NRGBA
}

type icon struct {
	src  []byte
	size unit.Dp

	// Cached values.
	op      paint.ImageOp
	imgSize int
}

func rgb(c uint32) color.NRGBA {
	return argb((0xff << 24) | c)
}

func argb(c uint32) color.NRGBA {
	return color.NRGBA{A: uint8(c >> 24), R: uint8(c >> 16), G: uint8(c >> 8), B: uint8(c)}
}

func (f fill) Layout(gtx layout.Context) layout.Dimensions {
	cs := gtx.Constraints
	d := cs.Min
	paint.FillShape(gtx.Ops, f.color, clip.Rect(image.Rectangle{Max: d}).Op())
	return layout.Dimensions{Size: d, Baseline: d.Y}
}

func (s *pageStack) Len() int {
	return len(s.pages)
}

func (s *pageStack) Current() Page {
	return s.pages[len(s.pages)-1]
}

func (s *pageStack) Pop() {
	if len(s.pages) > 0 {
		if s.stopChan != nil {
			s.stop()
		}
		i := len(s.pages) - 1
		s.pages[i] = nil
		s.pages = s.pages[:i]
	}
	if len(s.pages) > 0 {
		s.start() // start new top of stack
	}
}

func (s *pageStack) start() {
	stop := make(chan struct{})
	s.stopChan = stop
	s.Current().Start(stop)
}

func (s *pageStack) Push(p Page) {
	if s.stopChan != nil {
		s.stop()
	}
	s.pages = append(s.pages, p)
	s.start()
}

func (s *pageStack) stop() {
	close(s.stopChan)
	s.stopChan = nil
}

func (s *pageStack) Clear(p Page) {
	for len(s.pages) > 0 {
		s.Pop()
	}
	s.Push(p)
}
