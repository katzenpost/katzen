package main

import (
	"gioui.org/io/key"
	"gioui.org/layout"
	"gioui.org/widget"
	"gioui.org/widget/material"
)

// RenameContactPage is the page for renaming a contact
type RenameContactPage struct {
	a           *App
	contactID   uint64
	newnickname *widget.Editor
	back        *widget.Clickable
	submit      *widget.Clickable
}

// Layout returns a simple centered layout prompting user for new contact nickname
func (p *RenameContactPage) Layout(gtx layout.Context) layout.Dimensions {
	bg := Background{
		Color: th.Bg,
		Inset: layout.Inset{},
	}

	gtx.Execute(key.FocusCmd{Tag: p.newnickname})
	return bg.Layout(gtx, func(gtx C) D {
		return layout.Flex{Axis: layout.Vertical, Alignment: layout.End}.Layout(gtx,
			layout.Rigid(func(gtx C) D {
				return layout.Flex{Axis: layout.Horizontal, Spacing: layout.SpaceBetween, Alignment: layout.Baseline}.Layout(gtx,
					layout.Rigid(button(th, p.back, backIcon).Layout),
					layout.Flexed(1, fill{th.Bg}.Layout),
					layout.Rigid(material.H6(th, "Rename Contact").Layout),
					layout.Flexed(1, fill{th.Bg}.Layout))
			}),
			layout.Flexed(1, func(gtx C) D {
				return layout.Center.Layout(gtx, material.Editor(th, p.newnickname, "new nickname").Layout)
			}),
			layout.Rigid(func(gtx C) D { return material.Button(th, p.submit, "MEOW").Layout(gtx) }),
		)
	})
}

// Event catches the widget submit events and calls NewContact
func (p *RenameContactPage) Event(gtx layout.Context) interface{} {
	if p.back.Clicked(gtx) {
		return BackEvent{}
	}
	if ev, ok := p.newnickname.Update(gtx); ok {
		switch ev.(type) {
		case widget.SubmitEvent:
			p.submit.Click()
		}
	}
	if p.submit.Clicked(gtx) {
		contact, err := p.a.GetContact(p.contactID)
		if err == nil {
			// XXX: SetNickname() Nickname() methods ?
			contact.Nickname = p.newnickname.Text()
			err = p.a.PutContact(contact)
			if err == nil {
				return EditContactComplete{}
			}
		}
		p.newnickname.SetText("")
	}
	return nil
}

func (p *RenameContactPage) Start(stop <-chan struct{}) {
}

func (RenameContactPage) Update() {}

func newRenameContactPage(a *App, contactID uint64) *RenameContactPage {
	p := &RenameContactPage{a: a, contactID: contactID}
	p.newnickname = &widget.Editor{SingleLine: true, Submit: true}
	p.back = &widget.Clickable{}
	p.submit = &widget.Clickable{}
	return p
}
