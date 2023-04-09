package main

import (
	"bytes"
	"encoding/base64"
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
	"sort"
	"strings"
	"time"
)

var (
	convoList         = &layout.List{Axis: layout.Vertical, ScrollToEnd: false}
	sorted            = []*Conversation{}
	selectedIdx       = 0
	kb                = false
	settingsIcon, _   = widget.NewIcon(icons.ActionSettings)
	addContactIcon, _ = widget.NewIcon(icons.SocialPersonAdd)
	logo              = getLogo()
	units, _          = durafmt.UnitsCoder{PluralSep: ":", UnitsSep: ","}.Decode("y:y,w:w,d:d,h:h,m:m,s:s,ms:ms,us:us")
	avatars           = make(map[uint64]layout.Widget)
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

type sortedConvos []*Conversation

func (s sortedConvos) Less(i, j int) bool {
	// sorts conversations by most-recent-first, followed by conversations
	// without messages by title alphabetically

	li := len(s[i].Messages)
	lj := len(s[j].Messages)

	if li == 0 && lj == 0 {
		return s[i].Title < s[j].Title
	} else if li == 0 {
		return false
	} else if lj == 0 {
		return true
	} else {
		var its, jts time.Time
		its = s[i].LastMessage
		jts = s[j].LastMessage
		return its.After(jts)
	}
}

func (s sortedConvos) Swap(i, j int) {
	s[i], s[j] = s[j], s[i]
}
func (s sortedConvos) Len() int {
	return len(s)
}

func getSortedConvos(a *App) (convos sortedConvos) {
	conversationIDs := a.GetConversationIDs()
	for cid, _ := range conversationIDs {
		convo, err := a.GetConversation(cid)
		if err == nil {
			convos = append(convos, convo)
		}
	}
	sort.Sort(convos)
	return
}

type HomePage struct {
	a            *App
	addContact   *widget.Clickable
	connect      *widget.Clickable
	connectIcon  *connectIcon
	showSettings *widget.Clickable
	convoClicks  map[uint64]*gesture.Click
}

type AddContactClick struct{}
type ShowSettingsClick struct{}

func (p *HomePage) Layout(gtx layout.Context) layout.Dimensions {
	sorted = getSortedConvos(p.a)
	// xxx do not request this every frame...
	bg := Background{
		Color: th.Bg,
		Inset: layout.Inset{},
	}

	if len(sorted) == 0 {
		selectedIdx = 0
	} else if selectedIdx < 0 {
		selectedIdx = len(sorted) - 1
	} else {
		selectedIdx = selectedIdx % len(sorted)
	}

	// re-center list view for keyboard contact selection
	if kb {
		if selectedIdx < convoList.Position.First || selectedIdx >= convoList.Position.First+convoList.Position.Count {
			// list doesn't wrap around view to end, so do not give negative value for First
			if selectedIdx < convoList.Position.Count-1 {
				convoList.Position.First = 0
			} else {
				convoList.Position.First = (selectedIdx - convoList.Position.Count + 1)
			}
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
				// the convoList
				return convoList.Layout(gtx, len(sorted), func(gtx C, i int) layout.Dimensions {
					// update the last message received
					var lastMsg uint64
					var lastTs time.Time
					if len(sorted[i].Messages) > 0 {
						lastMsg = sorted[i].Messages[len(sorted[i].Messages)-1]
					}

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
						if _, ok := p.convoClicks[sorted[i].ID]; !ok {
							c := new(gesture.Click)
							p.convoClicks[sorted[i].ID] = c
						}

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
												return in.Layout(gtx, ConvoStyle(th, sorted[i].Title).Layout)
											}),
											layout.Rigid(func(gtx C) D {
												return layout.Flex{Axis: layout.Vertical, Alignment: layout.Start, Spacing: layout.SpaceEnd}.Layout(gtx,
													layout.Rigid(func(gtx C) D {
														// timestamp
														if lastMsg != 0 {
															messageAge := strings.Replace(durafmt.ParseShort(time.Now().Round(0).Sub(lastTs).Truncate(time.Minute)).Format(units), "0 s", "now", 1)
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
										if lastMsg != 0 {
											return in.Layout(gtx, func(gtx C) D {
												// TODO: set the color based on sent or received
												msg, _ := p.a.GetMessage(lastMsg)
												return material.Body2(th, string(msg.Body)).Layout(gtx)
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
						p.convoClicks[sorted[i].ID].Add(gtx.Ops)
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
	if p.connect.Clicked() {
		return ConnectClick{}
	}
	// listen for pointer right click events on the addContact widget
	if p.addContact.Clicked() {
		return AddContactClick{}
	}
	if p.showSettings.Clicked() {
		return ShowSettingsClick{}
	}
	for id, click := range p.convoClicks {
		for _, e := range click.Events(gtx.Queue) {
			if e.Type == gesture.TypeClick {
				return ChooseConvoClick{id: id}
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
				return ConnectClick{}
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
				kb = false
				// editor event isn't consuming the kb input upon addcontact return.
				if len(sorted) <= selectedIdx {
					return nil
				}
				return ChooseConvoClick{id: sorted[selectedIdx].ID}
			}
		}
	}

	return nil
}

func (p *HomePage) Start(stop <-chan struct{}) {
	p.connectIcon.Start(stop)
}

func newHomePage(a *App) *HomePage {
	cl := &widget.Clickable{}
	return &HomePage{
		a:            a,
		addContact:   &widget.Clickable{},
		connect:      cl,
		connectIcon:  NewConnectIcon(a, th, cl),
		showSettings: &widget.Clickable{},
		convoClicks:  make(map[uint64]*gesture.Click),
	}
}

func ConvoStyle(th *material.Theme, txt string) material.LabelStyle {
	l := material.Label(th, th.TextSize, txt)
	return l
}
