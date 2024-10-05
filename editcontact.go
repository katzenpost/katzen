package main

import (
	"gioui.org/gesture"
	"gioui.org/layout"
	"gioui.org/op/clip"
	"gioui.org/unit"
	"gioui.org/widget"
	"gioui.org/widget/material"
	"github.com/hako/durafmt"
	"image"
	"math"
	"time"
)

// EditContactPage is the page for adding a new contact
type EditContactPage struct {
	a        *App
	id       uint64
	back     *widget.Clickable
	apply    *widget.Clickable
	avatar   *gesture.Click
	clear    *widget.Clickable
	expiry   *widget.Float
	rename   *widget.Clickable
	remove   *widget.Clickable
	settings *layout.List
	widgets  []layout.Widget
	duration time.Duration
}

const (
	minExpiration = float32(0.0)  // never delete messages
	maxExpiration = float32(14.0) // 2 weeks
)

// Layout returns the contact options menu
func (p *EditContactPage) Layout(gtx layout.Context) layout.Dimensions {
	return bg.Layout(gtx, func(gtx C) D {
		return layout.Flex{Axis: layout.Vertical, Spacing: layout.SpaceEnd, Alignment: layout.Start}.Layout(gtx,
			// topbar
			layout.Rigid(func(gtx C) D {
				return layout.Flex{Axis: layout.Horizontal, Spacing: layout.SpaceBetween, Alignment: layout.Middle}.Layout(gtx,
					layout.Rigid(button(th, p.back, backIcon).Layout),
					layout.Flexed(1, fill{th.Bg}.Layout),
					layout.Rigid(material.H6(th, "Edit Contact").Layout),
					layout.Flexed(1, fill{th.Bg}.Layout))
			}),
			// settings list
			layout.Flexed(1, func(gtx C) D {
				in := layout.Inset{Top: unit.Dp(8), Bottom: unit.Dp(8), Left: unit.Dp(12), Right: unit.Dp(12)}
				return in.Layout(gtx, func(gtx C) D {
					return p.settings.Layout(gtx, len(p.widgets), func(gtx C, i int) layout.Dimensions {
						// Layout the widget in the list. can wrap and inset, etc, here...
						return p.widgets[i](gtx)
					})
				})
			}),
		)
	})
}

type EditContact struct {
	id uint64
}

type EditContactComplete struct {
	id uint64
}

type ChooseAvatar struct {
	id uint64
}

type RenameContact struct {
	id uint64
}

// Event catches the widget submit events and calls NewContact
func valueToDuration(val float32) time.Duration {
	// multiply by the maximum range, in days
	duration := val * maxExpiration
	// round to a multiple of days
	duration = float32(math.Round(float64(duration)))
	// update the slider to a rounded value
	return time.Duration(int64(duration) * int64(time.Hour) * 24)
}

func durationToValue(dur time.Duration) float32 {
	// convert duration to days
	fdur := float64(int64(dur) / (int64(time.Hour) * 24))
	// round to a multiple of days and return the scaled slider value
	return float32(math.Round(fdur)) / maxExpiration
}

// Event catches the widget submit events and calls catshadow.NewContact
func (p *EditContactPage) Event(gtx layout.Context) interface{} {
	if p.back.Clicked(gtx) {
		return BackEvent{}
	}
	if _, ok := p.avatar.Update(gtx.Source); ok {
		return ChooseAvatar{id: p.id}
	}
	if p.clear.Clicked(gtx) {
		// TODO: confirmation dialog
		// XXX: p.a.c.WipeConversation(p.id)
		return EditContactComplete{id: p.id}
	}
	if p.expiry.Update(gtx) {
		p.duration = valueToDuration(p.expiry.Value)
	}
	if p.rename.Clicked(gtx) {
		return RenameContact{id: p.id}
	}
	if p.remove.Clicked(gtx) {
		// TODO: confirmation dialog
		_, err := p.a.GetContact(p.id)
		if err != nil {
			p.a.Lock()
			defer p.a.Unlock()
			transport, ok := p.a.transports[p.id]
			if ok {
				// if has a stream, halt it
				transport.Close()
				transport.Halt()
				delete(p.a.transports, p.id)
			}
			err = p.a.RemoveContact(p.id)
			if err == nil {
				delete(avatars, p.id)
				return EditContactComplete{id: p.id}
			}
		}
	}
	if p.apply.Clicked(gtx) {
		//p.a.c.ChangeExpiration(p.di, p.duration)
		return BackEvent{}
	}
	return nil
}

func (p *EditContactPage) Start(stop <-chan struct{}) {
}

func (EditContactPage) Update() {}

func newEditContactPage(a *App, id uint64) *EditContactPage {
	p := &EditContactPage{a: a, id: id, back: &widget.Clickable{},
		avatar: &gesture.Click{}, clear: &widget.Clickable{},
		rename: &widget.Clickable{},
		remove: &widget.Clickable{}, apply: &widget.Clickable{},
		settings: &layout.List{Axis: layout.Vertical},
	}
	//p.duration, _ = a.c.GetExpiration(id)
	p.expiry.Value = durationToValue(p.duration)
	p.widgets = []layout.Widget{
		func(gtx C) D {
			dims := layout.Center.Layout(gtx, func(gtx C) D {
				return p.a.layoutAvatar(gtx, p.id)
			})
			a := clip.Rect(image.Rectangle{Max: dims.Size})
			t := a.Push(gtx.Ops)
			p.avatar.Add(gtx.Ops)
			t.Pop()
			return dims
		},
		layout.Spacer{Height: unit.Dp(8)}.Layout,
		func(gtx C) D {
			var label string
			if p.expiry.Value < 1.0/maxExpiration {
				label = "Delete after: never"
			} else {
				label = "Delete after: " + durafmt.Parse(p.duration).Format(units)
			}
			return layout.Flex{Axis: layout.Vertical, Alignment: layout.Middle, Spacing: layout.SpaceBetween}.Layout(gtx,
				layout.Rigid(material.Body2(th, "Message deletion").Layout),
				layout.Rigid(material.Slider(th, p.expiry).Layout),
				layout.Rigid(material.Caption(th, label).Layout),
			)
		},
		layout.Spacer{Height: unit.Dp(8)}.Layout,
		material.Button(th, p.clear, "Clear History").Layout,
		layout.Spacer{Height: unit.Dp(8)}.Layout,
		material.Button(th, p.rename, "Rename Contact").Layout,
		layout.Spacer{Height: unit.Dp(8)}.Layout,
		material.Button(th, p.remove, "Delete Contact").Layout,
	}
	return p
}
