package main

import (
	"fmt"
	"gioui.org/io/key"
	"gioui.org/layout"
	"gioui.org/widget"
	"gioui.org/widget/material"
	"runtime"
)

type signInPage struct {
	a          *App
	password   *widget.Editor
	submit     *widget.Clickable
	result     chan interface{}
	errMsg     string
	connecting bool
}

func (p *signInPage) Start(stop <-chan struct{}) {
}

func (signInPage) Update() {}

func (p *signInPage) Layout(gtx layout.Context) layout.Dimensions {
	gtx.Execute(key.FocusCmd{Tag: p.password})
	bg := Background{
		Color: th.Bg,
		Inset: layout.Inset{},
	}
	return bg.Layout(gtx, func(gtx C) D {
		return layout.Flex{Axis: layout.Vertical, Spacing: layout.SpaceBetween, Alignment: layout.End}.Layout(gtx,
			layout.Flexed(1, func(gtx C) D {
				if p.errMsg != "" {
					return layout.Center.Layout(gtx, material.Editor(th, p.password, p.errMsg).Layout)
				}
				return layout.Center.Layout(gtx, material.Editor(th, p.password, "Enter your password").Layout)
			}),
			layout.Rigid(func(gtx C) D {
				return material.Button(th, p.submit, "MEOW").Layout(gtx)
			}),
		)
	})
}

type signInStarted struct {
	result chan interface{}
}

func (p *signInPage) Event(gtx layout.Context) interface{} {
	if ev, ok := p.password.Update(gtx); ok {
		switch ev.(type) {
		case widget.SubmitEvent:
			p.submit.Click()
		}
	}

	if p.submit.Clicked(gtx) {
		p.connecting = true
		pw := p.password.Text()
		p.password.SetText("")
		if len(pw) != 0 && len(pw) < minPasswordLen {
			p.errMsg = fmt.Sprintf("Password must be minimum %d characters long", minPasswordLen)
		} else {
			go func() {
				setupClient(p.a, []byte(pw), p.result)
				p.a.w.Invalidate()
			}()
			return signInStarted{result: p.result}
		}
	}
	return nil
}

func newSignInPage(a *App) *signInPage {
	pw := &widget.Editor{SingleLine: true, Mask: '*', Submit: true}

	if runtime.GOOS == "android" {
		pw.Submit = false
	}

	return &signInPage{
		a:        a,
		password: pw,
		submit:   &widget.Clickable{},
		result:   make(chan interface{}, 1),
	}
}
