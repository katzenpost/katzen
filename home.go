package main

import (
	"bytes"
	"encoding/base64"
	"gioui.org/font"
	"gioui.org/gesture"
	"gioui.org/io/key"
	"gioui.org/layout"
	"gioui.org/op/clip"
	"gioui.org/op/paint"
	"gioui.org/unit"
	"gioui.org/widget"
	"gioui.org/widget/material"
	"github.com/hako/durafmt"
	"github.com/katzenpost/katzen/assets"
	"golang.org/x/exp/shiny/materialdesign/icons"
	"image"
	_ "image/png"
	"strings"
	"sync"
	"time"
)

var (
	convoList         = &layout.List{Axis: layout.Vertical, ScrollToEnd: false}
	selectedIdx       = 0
	kb                = false
	settingsIcon, _   = widget.NewIcon(icons.ActionSettings)
	addContactIcon, _ = widget.NewIcon(icons.SocialPersonAdd)
	logo              = getLogo()
	units, _          = durafmt.UnitsCoder{PluralSep: ":", UnitsSep: ","}.Decode("y:y,w:w,d:d,h:h,m:m,s:s,ms:ms,us:us")
	avatars           = make(map[uint64]layout.Widget)
)

type HomePage struct {
	l                *sync.Mutex
	a                *App
	addContact       *widget.Clickable
	connect          *widget.Clickable
	connectIcon      *connectIcon
	showSettings     *widget.Clickable
	convoClicks      map[uint64]*gesture.Click
	contacts         []*Contact
	conversations    []*Conversation
	updateContactsCh chan interface{}
	updateConvCh     chan interface{}
}

type AddContactClick struct{}
type ShowSettingsClick struct{}

func (p *HomePage) Layout(gtx layout.Context) layout.Dimensions {
	// xxx do not request this every frame...
	bg := Background{
		Color: th.Bg,
		Inset: layout.Inset{},
	}

	if len(p.conversations) == 0 {
		selectedIdx = 0
	} else if selectedIdx < 0 {
		selectedIdx = len(p.conversations) - 1
	} else {
		selectedIdx = selectedIdx % len(p.conversations)
	}

	// re-center list view for keyboard contact selection
	if selectedIdx < convoList.Position.First || selectedIdx >= convoList.Position.First+convoList.Position.Count {
		// list doesn't wrap around view to end, so do not give negative value for First
		if selectedIdx < convoList.Position.Count-1 {
			convoList.Position.First = 0
		} else {
			convoList.Position.First = (selectedIdx - convoList.Position.Count + 1)
		}
	}

	return bg.Layout(gtx, func(gtx C) D {
		// returns a flex consisting of the conversation list and add contact button
		return layout.Flex{Axis: layout.Vertical, Alignment: layout.End}.Layout(gtx,
			// topbar: Name, Add Contact, Settings
			layout.Rigid(func(gtx C) D {
				return layout.Flex{Axis: layout.Horizontal, Spacing: layout.SpaceBetween, Alignment: layout.Middle}.Layout(
					gtx,
					layout.Rigid(layoutLogo),
					layout.Flexed(1, fill{th.Bg}.Layout),
					layout.Rigid(p.connectIcon.Layout),
					layout.Rigid(button(th, p.showSettings, settingsIcon).Layout),
					layout.Rigid(button(th, p.addContact, addContactIcon).Layout),
				)
			}),

			// show list of conversations
			layout.Flexed(1, func(gtx C) D {
				gtx.Constraints.Min.X = gtx.Dp(unit.Dp(300))
				// the convoList layout function evaluates for each element of conversations
				return convoList.Layout(gtx, len(p.conversations), func(gtx C, i int) layout.Dimensions {
					if _, ok := p.convoClicks[p.conversations[i].ID]; !ok {
						p.convoClicks[p.conversations[i].ID] = new(gesture.Click)
					}

					// update the last message received
					var err error
					var lastMsgId uint64
					var lastMsg *Message
					if len(p.conversations[i].Messages) > 0 {
						lastMsgId = p.conversations[i].Messages[len(p.conversations[i].Messages)-1]
						lastMsg, err = p.a.GetMessage(lastMsgId)
						if err != nil {
							panic(err)
						}
					}

					// inset each contact Flex
					in := layout.Inset{Top: unit.Dp(8), Bottom: unit.Dp(8), Left: unit.Dp(12), Right: unit.Dp(12)}

					// if the layout is selected, change background color
					bg := Background{Inset: in}
					if i == selectedIdx {
						bg.Color = th.ContrastBg
					} else {
						bg.Color = th.Bg
					}

					return bg.Layout(gtx, func(gtx C) D {
						// returns Horizontal Flex of contact icon, contact name, and last message received or sent
						dims := layout.Flex{Axis: layout.Horizontal, Spacing: layout.SpaceEvenly}.Layout(gtx,
							// conversation name and last message
							layout.Flexed(1, func(gtx C) D {
								gtx.Constraints.Max.Y = gtx.Dp(unit.Dp(96))
								return layout.Flex{Axis: layout.Vertical, Alignment: layout.Start, Spacing: layout.SpaceBetween}.Layout(gtx,
									layout.Rigid(func(gtx C) D {
										return layout.Flex{Axis: layout.Horizontal, Alignment: layout.Start, Spacing: layout.SpaceBetween}.Layout(gtx,
											// conversation Title
											layout.Rigid(func(gtx C) D {
												in := layout.Inset{Top: unit.Dp(4), Bottom: unit.Dp(4), Left: unit.Dp(12), Right: unit.Dp(12)}
												return in.Layout(gtx, ConvoStyle(th, p.conversations[i].Title).Layout)
											}),
											layout.Rigid(func(gtx C) D {
												return layout.Flex{Axis: layout.Vertical, Alignment: layout.Start, Spacing: layout.SpaceEnd}.Layout(gtx,
													layout.Rigid(func(gtx C) D {
														// XXX: how does this apply to multiparty conversations
														//if contacts[i].IsPending {
														//	return pandaIcon.Layout(gtx, th.Palette.ContrastBg)
														//}
														return fill{th.Bg}.Layout(gtx)
													}),
													layout.Rigid(func(gtx C) D {
														// timestamp
														lastMsgTs := p.conversations[i].LastMessage
														if !lastMsgTs.IsZero() {
															messageAge := strings.Replace(durafmt.ParseShort(time.Now().Round(0).Sub(lastMsgTs).Truncate(time.Minute)).Format(units), "0 s", "now", 1)
															return material.Caption(th, messageAge).Layout(gtx)
														}
														return fill{th.Bg}.Layout(gtx)
													}),
												)
											}),
										)
									}),
									// last message
									layout.Rigid(func(gtx C) D {
										in := layout.Inset{Top: unit.Dp(4), Bottom: unit.Dp(4), Left: unit.Dp(12), Right: unit.Dp(12)}
										if lastMsg != nil {
											return in.Layout(gtx, func(gtx C) D {
												// TODO: set the color based on sent or received
												return material.Body2(th, string(lastMsg.Body)).Layout(gtx)
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
						p.convoClicks[p.conversations[i].ID].Add(gtx.Ops)
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

// ChooseConvoClick is the event that indicates which conversation was selected
type ChooseConvoClick struct {
	id uint64
}

// ConnectClick is the event that indicates connection button was clicked
type ConnectClick struct {
}

// Event returns a ChooseConvoClick event when a contact is chosen
func (p *HomePage) Event(gtx layout.Context) interface{} {
	if p.connect.Clicked(gtx) {
		return ConnectClick{}
	}
	// listen for pointer right click events on the addContact widget
	if p.addContact.Clicked(gtx) {
		return AddContactClick{}
	}
	if p.showSettings.Clicked(gtx) {
		return ShowSettingsClick{}
	}
	for id, click := range p.convoClicks {
		if e, ok := click.Update(gtx.Source); ok {
			if e.Kind == gesture.KindClick {
				return ChooseConvoClick{id: id}
			}
		}
	}
	// check for keypress events
	if e, ok := shortcutEvents(gtx); ok {
		if e.Name == key.NameF2 {
			return AddContactClick{}
		}
		if e.Name == key.NameF3 {
			return ShowSettingsClick{}
		}
		if e.Name == key.NameF4 {
			return ConnectClick{}
		}
		if e.Name == key.NameUpArrow {
			selectedIdx = selectedIdx - 1
		}
		if e.Name == key.NameDownArrow {
			selectedIdx = selectedIdx + 1
		}
		if e.Name == key.NameReturn {
			p.l.Lock()
			defer p.l.Unlock()
			if len(p.contacts) < selectedIdx+1 {
				return nil
			}
			return ChooseConvoClick{id: p.conversations[selectedIdx].ID}
		}
	}

	return nil
}

func (p *HomePage) Start(stop <-chan struct{}) {
	go p.connectIcon.Start(stop)
	// receive commands to update the contact list, e.g. from KeyExchangeCompleted events
	go func() {
		for {
			select {
			case <-stop:
				return
			case <-p.updateContactsCh:
				p.updateContacts()
			case <-p.updateConvCh:
				p.updateConversations()
			}
		}
	}()
	p.UpdateConversations()
	p.UpdateContacts()
}

func (h *HomePage) UpdateConversations() {
	select {
	case h.updateConvCh <- struct{}{}:
	default:
	}
}

func (h *HomePage) updateConversations() {
	h.l.Lock()
	defer h.l.Unlock()
	if h.a == nil {
		return
	}
	if h.a.c == nil {
		return
	}
	h.conversations = h.a.getSortedConvos()
}

func (h *HomePage) UpdateContacts() {
	select {
	case h.updateContactsCh <- struct{}{}:
	default:
	}
}

func (h *HomePage) updateContacts() {
	h.l.Lock()
	defer h.l.Unlock()
	if h.a == nil {
		return
	}
	if h.a.c == nil {
		return
	}
	h.contacts = h.a.getSortedContacts()
	h.a.w.Invalidate()
}

func newHomePage(a *App) *HomePage {
	connectButton := &widget.Clickable{}
	p := &HomePage{
		a:                a,
		l:                new(sync.Mutex),
		updateConvCh:     make(chan interface{}, 1),
		updateContactsCh: make(chan interface{}, 1),
		addContact:       &widget.Clickable{},
		connect:          connectButton,
		connectIcon:      NewConnectIcon(a, th, connectButton),
		showSettings:     &widget.Clickable{},
		convoClicks:      make(map[uint64]*gesture.Click),
	}
	p.UpdateConversations()
	return p
}

func ConvoStyle(th *material.Theme, txt string) material.LabelStyle {
	l := material.Label(th, th.TextSize, txt)
	l.Font.Weight = font.Bold
	return l
}
