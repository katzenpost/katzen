package main

import (
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

// Event catches the widget submit events and calls catshadow.NewContact
func (p *RenameContactPage) Event(gtx layout.Context) interface{} {
	if p.back.Clicked() {
		return BackEvent{}
	}
	for _, ev := range p.newnickname.Events() {
		switch ev.(type) {
		case widget.SubmitEvent:
			p.submit.Click()
		}
	}
	if p.submit.Clicked() {
		contact, ok := p.a.Contacts[p.contactID]
		if ok {
			// XXX: SetNickname() Nickname() methods ?
			contact.Lock()
			contact.Nickname = p.newnickname.Text()
			contact.Unlock()
			return EditContactComplete{}
		}
		p.newnickname.SetText("")
	}
	return nil
}

func (p *RenameContactPage) Start(stop <-chan struct{}) {
}

func newRenameContactPage(a *App, contactID uint64) *RenameContactPage {
	p := &RenameContactPage{a: a, contactID: contactID}
	p.newnickname = &widget.Editor{SingleLine: true, Submit: true}
	p.newnickname.Focus()
	p.back = &widget.Clickable{}
	p.submit = &widget.Clickable{}
	return p
}
