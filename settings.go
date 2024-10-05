package main

import (
	"time"

	"gioui.org/layout"
	"gioui.org/unit"
	"gioui.org/widget"
	"gioui.org/widget/material"
	"gioui.org/x/notify"
	"github.com/dgraph-io/badger/v4"
)

// SettingsPage is for user settings
type SettingsPage struct {
	a                 *App
	back              *widget.Clickable
	submit            *widget.Clickable
	switchUseTor      *widget.Bool
	switchAutoConnect *widget.Bool
}

var (
	inset = layout.UniformInset(unit.Dp(8))
)

const (
	settingNameColumnWidth    = .3
	settingDetailsColumnWidth = 1 - settingNameColumnWidth
)

// Layout returns a simple centered layout prompting to update settings
func (p *SettingsPage) Layout(gtx layout.Context) layout.Dimensions {
	bg := Background{
		Color: th.Bg,
		Inset: layout.Inset{},
	}

	return bg.Layout(gtx, func(gtx C) D {
		return layout.Flex{Axis: layout.Vertical, Alignment: layout.End}.Layout(gtx,
			// topbar
			layout.Rigid(func(gtx C) D {
				return layout.Flex{Axis: layout.Horizontal, Spacing: layout.SpaceBetween, Alignment: layout.Baseline}.Layout(gtx,
					layout.Rigid(button(th, p.back, backIcon).Layout),
					layout.Flexed(1, fill{th.Bg}.Layout),
					layout.Rigid(material.H6(th, "Settings").Layout),
					layout.Flexed(1, fill{th.Bg}.Layout))
			}),
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				return layout.Flex{Alignment: layout.Middle}.Layout(gtx,
					layout.Flexed(settingNameColumnWidth, func(gtx C) D {
						return inset.Layout(gtx, material.Body1(th, "Use Tor").Layout)
					}),
					layout.Flexed(settingDetailsColumnWidth, func(gtx C) D {
						return inset.Layout(gtx, material.Switch(th, p.switchUseTor, "Use Tor").Layout)
					}),
				)
			}),
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				return layout.Flex{Alignment: layout.Middle}.Layout(gtx,
					layout.Flexed(settingNameColumnWidth, func(gtx C) D {
						return inset.Layout(gtx, material.Body1(th, "Connect Automatically").Layout)
					}),
					layout.Flexed(settingDetailsColumnWidth, func(gtx C) D {
						return inset.Layout(gtx, material.Switch(th, p.switchAutoConnect, "Connect Automatically").Layout)
					}),
				)
			}),
			layout.Rigid(func(gtx C) D {
				return material.Button(th, p.submit, "Apply Settings").Layout(gtx)
			}),
		)
	})
}

type restartClient struct{}

// Event catches the widget submit events and calls Settings
func (p *SettingsPage) Event(gtx layout.Context) interface{} {
	if p.back.Clicked(gtx) {
		return BackEvent{}
	}
	if p.switchUseTor.Update(gtx) {
		if p.switchUseTor.Value && !hasDefaultTor() {
			p.switchUseTor.Value = false

			// set UseTor to false
			p.a.db.Update(func(txn *badger.Txn) error {
				return txn.Set([]byte("UseTor"), []byte{0x0})
			})
			warnNoTor()
			return nil
		}
		if p.switchUseTor.Value {
			p.a.db.Update(func(txn *badger.Txn) error {
				return txn.Set([]byte("UseTor"), []byte{0xFF})
			})
		} else {
			p.a.db.Update(func(txn *badger.Txn) error {
				return txn.Set([]byte("UseTor"), []byte{0x00})
			})
		}
	}
	if p.switchAutoConnect.Update(gtx) {
		if p.switchAutoConnect.Value {
			p.a.db.Update(func(txn *badger.Txn) error {
				return txn.Set([]byte("AutoConnect"), []byte{0xFF})
			})
		} else {
			p.a.db.Update(func(txn *badger.Txn) error {
				return txn.Set([]byte("AutoConnect"), []byte{0x00})
			})
		}
	}
	if p.submit.Clicked(gtx) {
		go func() {
			if n, err := notify.Push("Restarting", "Katzen is restarting"); err == nil {
				<-time.After(notificationTimeout)
				n.Cancel()
			}
		}()
		p.a.c.Shutdown()
		return restartClient{}
	}
	return nil
}

func (p *SettingsPage) Start(stop <-chan struct{}) {
}

func (SettingsPage) Update() {}

func newSettingsPage(a *App) *SettingsPage {
	p := &SettingsPage{a: a}
	p.back = &widget.Clickable{}
	p.submit = &widget.Clickable{}

	// read database for Tor setting (set at startup
	err := a.db.View(func(txn *badger.Txn) error {
		i, err := txn.Get([]byte("UseTor"))
		if err != nil {
			return err
		}
		return i.Value(func(val []byte) error {
			if val[0] == 0xFF {
				p.switchUseTor = &widget.Bool{Value: true}
			} else {
				p.switchUseTor = &widget.Bool{Value: false}
			}
			return nil
		})
	})
	if err != nil {
		// but not if you specified your own cfg file
		p.switchUseTor = &widget.Bool{Value: false}
	}

	// read database for autoConnect setting
	err = a.db.View(func(txn *badger.Txn) error {
		i, err := txn.Get([]byte("AutoConnect"))
		if err != nil {
			return err
		}
		return i.Value(func(val []byte) error {
			if val[0] == 0xFF {
				p.switchAutoConnect = &widget.Bool{Value: true}
			} else {
				p.switchAutoConnect = &widget.Bool{Value: false}
			}
			return nil
		})
	})
	if err != nil {
		p.switchAutoConnect = &widget.Bool{Value: false}
	}
	return p
}

func warnNoTor() {
	go func() {
		if n, err := notify.Push("Failure", "Tor requested, but not available on port 9050. Disable in settings to connect."); err == nil {
			<-time.After(notificationTimeout)
			n.Cancel()
		}
	}()
}
