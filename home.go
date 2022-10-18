package main

import (
	"bytes"
	"encoding/base64"
	"gioui.org/gesture"
	"gioui.org/io/key"
	"gioui.org/layout"
	"gioui.org/op/clip"
	"gioui.org/op/paint"
	"gioui.org/text"
	"gioui.org/unit"
	"gioui.org/widget"
	"gioui.org/widget/material"
	"github.com/hako/durafmt"
	"github.com/katzenpost/katzen/assets"
	"github.com/katzenpost/katzenpost/catshadow"
	"golang.org/x/exp/shiny/materialdesign/icons"
	"image"
	"image/png"
	"strings"
	"time"
)

var (
	contactList       = &layout.List{Axis: layout.Vertical, ScrollToEnd: false}
	selectedIdx       = 0
	kb                = false
	connectIcon, _    = widget.NewIcon(icons.DeviceSignalWiFi4Bar)
	disconnectIcon, _ = widget.NewIcon(icons.DeviceSignalWiFiOff)
	settingsIcon, _   = widget.NewIcon(icons.ActionSettings)
	addContactIcon, _ = widget.NewIcon(icons.SocialPersonAdd)
	logo              = getLogo()
	units, _          = durafmt.UnitsCoder{PluralSep: ":", UnitsSep: ","}.Decode("y:y,w:w,d:d,h:h,m:m,s:s,ms:ms,us:us")
	avatars           = make(map[string]layout.Widget)
	shortcuts         = key.Set(key.NameUpArrow + "|" +
		key.NameDownArrow + "|" +
		key.NamePageUp + "|" +
		key.NamePageDown + "|" +
		key.NameReturn + "|" +
		key.NameEscape + "|" +
		key.NameF1 + "|" + // show help page (not implemented)
		key.NameF2 + "|" + // add contact
		key.NameF3 + "|" + // show client settings
		key.NameF4 + "|" + // toggle connection status
		key.NameF5) // edit contact
)

type HomePage struct {
	a             *App
	addContact    *widget.Clickable
	connect       *widget.Clickable
	showSettings  *widget.Clickable
	av            map[string]*widget.Image
	contactClicks map[string]*gesture.Click
}

type AddContactClick struct{}
type ShowSettingsClick struct{}

func (p *HomePage) Layout(gtx layout.Context) layout.Dimensions {
	contacts := getSortedContacts(p.a)
	// xxx do not request this every frame...
	bg := Background{
		Color: th.Bg,
		Inset: layout.Inset{},
	}

	// bounds check to current contact list
	if selectedIdx < 0 {
		selectedIdx = len(contacts) - 1
	} else {
		selectedIdx = selectedIdx % len(contacts)
	}

	// re-center list view for keyboard contact selection
	if kb {
		if selectedIdx < contactList.Position.First || selectedIdx >= contactList.Position.First+contactList.Position.Count {
			// list doesn't wrap around view to end, so do not give negative value for First
			if selectedIdx < contactList.Position.Count-1 {
				contactList.Position.First = 0
			} else {
				contactList.Position.First = (selectedIdx - contactList.Position.Count + 1)
			}
		}
	}

	return bg.Layout(gtx, func(gtx C) D {
		// returns a flex consisting of the contacts list and add contact button
		return layout.Flex{Axis: layout.Vertical, Alignment: layout.End}.Layout(gtx,
			// topbar: Name, Add Contact, Settings
			layout.Rigid(func(gtx C) D {
				return layout.Flex{Axis: layout.Horizontal, Spacing: layout.SpaceBetween, Alignment: layout.Middle}.Layout(
					gtx,
					layout.Rigid(layoutLogo),
					layout.Flexed(1, fill{th.Bg}.Layout),
					func() layout.FlexChild {
						if isConnected {
							return layout.Rigid(button(th, p.connect, connectIcon).Layout)
						}
						return layout.Rigid(button(th, p.connect, disconnectIcon).Layout)
					}(),
					layout.Rigid(button(th, p.showSettings, settingsIcon).Layout),
					layout.Rigid(button(th, p.addContact, addContactIcon).Layout),
				)
			}),

			// show list of conversations
			layout.Flexed(1, func(gtx C) D {
				gtx.Constraints.Min.X = gtx.Dp(unit.Dp(300))
				// the contactList
				return contactList.Layout(gtx, len(contacts), func(gtx C, i int) layout.Dimensions {
					lastMsg := contacts[i].LastMessage

					// inset each contact Flex
					in := layout.Inset{Top: unit.Dp(8), Bottom: unit.Dp(8), Left: unit.Dp(12), Right: unit.Dp(12)}

					// if the layout is selected, change background color
					bg := Background{Inset: in}
					if kb && i == selectedIdx {
						bg.Color = th.ContrastBg
					} else {
						bg.Color = th.Bg
					}

					return bg.Layout(gtx, func(gtx C) D {
						// returns Flex of contact icon, contact name, and last message received or sent
						if _, ok := p.contactClicks[contacts[i].Nickname]; !ok {
							c := new(gesture.Click)
							p.contactClicks[contacts[i].Nickname] = c
						}

						dims := layout.Flex{Axis: layout.Horizontal, Spacing: layout.SpaceEvenly}.Layout(gtx,
							// contact avatar
							layout.Rigid(func(gtx C) D {
								return layoutAvatar(gtx, p.a.c, contacts[i].Nickname)
							}),
							// contact name and last message
							layout.Flexed(1, func(gtx C) D {
								gtx.Constraints.Max.Y = gtx.Dp(unit.Dp(96))
								return layout.Flex{Axis: layout.Vertical, Alignment: layout.Start, Spacing: layout.SpaceBetween}.Layout(gtx,
									layout.Rigid(func(gtx C) D {
										return layout.Flex{Axis: layout.Horizontal, Alignment: layout.Start, Spacing: layout.SpaceBetween}.Layout(gtx,
											// contact name
											layout.Rigid(func(gtx C) D {
												in := layout.Inset{Top: unit.Dp(4), Bottom: unit.Dp(4), Left: unit.Dp(12), Right: unit.Dp(12)}
												return in.Layout(gtx, ContactStyle(th, contacts[i].Nickname).Layout)
											}),
											layout.Rigid(func(gtx C) D {
												// timestamp
												if lastMsg != nil {
													messageAge := strings.Replace(durafmt.ParseShort(time.Now().Round(0).Sub(lastMsg.Timestamp).Truncate(time.Minute)).Format(units), "0 s", "now", 1)
													return material.Caption(th, messageAge).Layout(gtx)
												}
												return fill{th.Bg}.Layout(gtx)
											}),
										)
									}),
									// last message
									layout.Rigid(func(gtx C) D {
										in := layout.Inset{Top: unit.Dp(4), Bottom: unit.Dp(4), Left: unit.Dp(12), Right: unit.Dp(12)}
										if lastMsg != nil {
											return in.Layout(gtx, func(gtx C) D {
												// TODO: set the color based on sent or received
												return material.Body2(th, string(lastMsg.Plaintext)).Layout(gtx)
											})
										} else {
											return fill{th.Bg}.Layout(gtx)
										}
									}),
								)
							}),
						)
						a := clip.Rect(image.Rectangle{Max: dims.Size})
						t := a.Push(gtx.Ops)
						p.contactClicks[contacts[i].Nickname].Add(gtx.Ops)
						t.Pop()
						return dims
					})
				})
			}),
		)
	})
}

func getLogo() *widget.Image {
	d, err := base64.StdEncoding.DecodeString(assets.Logob64)
	if err != nil {
		return nil
	}
	if m, _, err := image.Decode(bytes.NewReader(d)); err == nil {
		return &widget.Image{Src: paint.NewImageOp(m)}
	}
	return nil
}

func layoutLogo(gtx C) D {
	in := layout.Inset{Left: unit.Dp(12), Right: unit.Dp(12)}
	return in.Layout(gtx, func(gtx C) D {
		sz := gtx.Constraints.Max.X
		logo.Scale = float32(sz) / float32(gtx.Dp(unit.Dp(float32(sz))))
		return logo.Layout(gtx)
	})
}

func layoutAvatar(gtx C, c *catshadow.Client, nickname string) D {
	return layout.Center.Layout(gtx, func(gtx C) D {
		cc := clipCircle{}
		return cc.Layout(gtx, func(gtx C) D {
			sz := image.Point{X: gtx.Dp(42), Y: gtx.Dp(42)}
			gtx.Constraints = layout.Exact(gtx.Constraints.Constrain(sz))
			if w, ok := avatars[nickname]; ok {
				return w(gtx)
			} else {
				if b, err := c.GetBlob("avatar://" + nickname); err == nil {
					if m, _, err := image.Decode(bytes.NewReader(b)); err == nil {
						w = func(gtx C) D {
							return widget.Image{Fit: widget.Contain, Src: paint.NewImageOp(m)}.Layout(gtx)
						}
					}
				} else {
					co := Contactal{SharedSecret: nickname}
					i := co.Render(sz)
					b := &bytes.Buffer{}
					if err := png.Encode(b, i); err == nil {
						c.AddBlob("avatar://"+nickname, b.Bytes())
					}
					w = func(gtx C) D {
						return widget.Image{Fit: widget.Contain, Src: paint.NewImageOp(i)}.Layout(gtx)
					}
				}
				avatars[nickname] = w
				return w(gtx)
			}
		})
	})
}

// ChooseContactClick is the event that indicates which contact was selected
type ChooseContactClick struct {
	nickname string
}

// Connect is the event that indicates client online mode is requested
type OnlineClick struct {
}

// OfflineClick is the event that indicates client offline mode is requested
type OfflineClick struct {
	Err error
}

// Event returns a ChooseContactClick event when a contact is chosen
func (p *HomePage) Event(gtx layout.Context) interface{} {
	if p.connect.Clicked() {
		if !isConnected && !isConnecting {
			return OnlineClick{}
		}
		return OfflineClick{}
	}
	// listen for pointer right click events on the addContact widget
	if p.addContact.Clicked() {
		return AddContactClick{}
	}
	if p.showSettings.Clicked() {
		return ShowSettingsClick{}
	}
	for nickname, click := range p.contactClicks {
		for _, e := range click.Events(gtx.Queue) {
			if e.Type == gesture.TypeClick {
				return ChooseContactClick{nickname: nickname}
			}
		}
	}
	// check for keypress events
	key.InputOp{Tag: p, Keys: shortcuts}.Add(gtx.Ops)
	for _, e := range gtx.Events(p) {
		switch e := e.(type) {
		case key.Event:
			if e.Name == key.NameF1 && e.State == key.Release {
			}
			if e.Name == key.NameF2 && e.State == key.Release {
				return AddContactClick{}
			}
			if e.Name == key.NameF3 && e.State == key.Release {
				return ShowSettingsClick{}
			}
			if e.Name == key.NameF4 && e.State == key.Release {
				if !isConnected {
					return OnlineClick{}
				}
				return OfflineClick{}
			}
			if e.Name == key.NameUpArrow && e.State == key.Release {
				kb = true
				selectedIdx = selectedIdx - 1
			}
			if e.Name == key.NameDownArrow && e.State == key.Release {
				kb = true
				selectedIdx = selectedIdx + 1
			}
			if e.Name == key.NameEscape && e.State == key.Release {
				kb = false
			}
			if e.Name == key.NameReturn && e.State == key.Release {
				contacts := getSortedContacts(p.a)
				kb = false
				return ChooseContactClick{nickname: contacts[selectedIdx].Nickname}
			}
		}
	}

	return nil
}

func (p *HomePage) Start(stop <-chan struct{}) {
}

func newHomePage(a *App) *HomePage {
	return &HomePage{
		a:             a,
		addContact:    &widget.Clickable{},
		connect:       &widget.Clickable{},
		showSettings:  &widget.Clickable{},
		contactClicks: make(map[string]*gesture.Click),
		av:            make(map[string]*widget.Image),
	}
}

func ContactStyle(th *material.Theme, txt string) material.LabelStyle {
	l := material.Label(th, th.TextSize, txt)
	l.Font.Weight = text.Bold
	return l
}
