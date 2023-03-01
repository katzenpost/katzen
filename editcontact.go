package main

import (
	"gioui.org/gesture"
	"gioui.org/layout"
	"gioui.org/op/clip"
	"gioui.org/unit"
	"gioui.org/widget"
	"gioui.org/widget/material"
	"image"
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
	minExpiration = 0.0  // never delete messages
	maxExpiration = 14.0 // 2 weeks
)

// Layout returns the contact options menu
func (p *EditContactPage) Layout(gtx layout.Context) layout.Dimensions {
	bg := Background{
		Color: th.Bg,
		Inset: layout.Inset{},
	}

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
func (p *EditContactPage) Event(gtx layout.Context) interface{} {
	if p.back.Clicked() {
		return BackEvent{}
	}
	for _, e := range p.avatar.Events(gtx.Queue) {
		if e.Type == gesture.TypeClick {
			return ChooseAvatar{id: p.id}
		}
	}
	// update duration
	p.duration = time.Duration(int64(p.expiry.Value)) * time.Minute * 60 * 24
	if p.rename.Clicked() {
		return RenameContact{id: p.id}
	}
	if p.remove.Clicked() {
		// TODO: confirmation dialog
		c, ok := p.a.Contacts[p.id]
		if ok {
			if c.Transport != nil {
				// if has a stream, halt it
				c.Transport.Close()
				c.Transport.Halt()
				c.Transport = nil
			}
			delete(p.a.Contacts, p.id)
			delete(avatars, p.id)
			return EditContactComplete{id: p.id}
		}
	}
	return nil
}

func (p *EditContactPage) Start(stop <-chan struct{}) {
}

func newEditContactPage(a *App, id uint64) *EditContactPage {
	p := &EditContactPage{a: a, id: id, back: &widget.Clickable{},
		avatar: &gesture.Click{}, clear: &widget.Clickable{},
		rename: &widget.Clickable{},
		remove: &widget.Clickable{}, apply: &widget.Clickable{},
		settings: &layout.List{Axis: layout.Vertical},
	}
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
		material.Button(th, p.rename, "Rename Contact").Layout,
		layout.Spacer{Height: unit.Dp(8)}.Layout,
		material.Button(th, p.remove, "Delete Contact").Layout,
	}
	return p
}
