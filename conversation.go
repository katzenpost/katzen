package main

import (
	"github.com/hako/durafmt"
	"github.com/katzenpost/katzenpost/catshadow"
	"golang.org/x/exp/shiny/materialdesign/icons"
	"image"
	"io"
	"runtime"
	"strings"
	"time"

	"gioui.org/gesture"
	"gioui.org/io/clipboard"
	"gioui.org/io/event"
	"gioui.org/io/key"
	"gioui.org/io/pointer"
	"gioui.org/io/transfer"
	"gioui.org/layout"
	"gioui.org/op/clip"
	"gioui.org/unit"
	"gioui.org/widget"
	"gioui.org/widget/material"
)

var (
	messageList      = &layout.List{Axis: layout.Vertical, ScrollToEnd: true}
	messageField     = &widget.Editor{SingleLine: true}
	backIcon, _      = widget.NewIcon(icons.NavigationChevronLeft)
	sendIcon, _      = widget.NewIcon(icons.NavigationChevronRight)
	queuedIcon, _    = widget.NewIcon(icons.NotificationSync)
	sentIcon, _      = widget.NewIcon(icons.ActionDone)
	deliveredIcon, _ = widget.NewIcon(icons.ActionDoneAll)
	pandaIcon, _     = widget.NewIcon(icons.ActionPets)
)

type conversationPage struct {
	a              *App
	nickname       string
	avatar         *widget.Image
	edit           *gesture.Click
	compose        *widget.Editor
	send           *widget.Clickable
	back           *widget.Clickable
	cancel         *gesture.Click
	msgcopy        *widget.Clickable
	msgpaste       *LongPress
	msgdetails     *widget.Clickable
	messageClicked *catshadow.Message
	messageClicks  map[*catshadow.Message]*gesture.Click
}

func (c *conversationPage) Start(stop <-chan struct{}) {
}

type MessageSent struct {
	nickname string
	msgId    catshadow.MessageID
}

type EditContact struct {
	nickname string
}

func (c *conversationPage) Event(gtx layout.Context) interface{} {
	// check for editor SubmitEvents
	if e, ok := c.compose.Update(gtx); ok {
		switch e.(type) {
		case widget.SubmitEvent:
			c.send.Click()
		case widget.ChangeEvent:
		}
	}

	// catch clicks on send button, update list view position to bottom
	if c.send.Clicked(gtx) {
		messageList.ScrollToEnd = true
		// XXX: could do this in Layout where we know the # of messages
		messageList.ScrollTo(0x1 << 32)

		msg := []byte(c.compose.Text())
		c.compose.SetText("")
		if len(msg) == 0 {
			return nil
		}
		// truncate messages
		// TODO: this should split messages and return the set of message IDs sent
		if len(msg)+4 > c.a.c.DoubleRatchetPayloadLength() {
			msg = msg[:c.a.c.DoubleRatchetPayloadLength()-4]
		}
		msgId := c.a.c.SendMessage(c.nickname, msg)
		return MessageSent{nickname: c.nickname, msgId: msgId}
	}

	// check for long press
	if e, ok := c.msgpaste.Update(gtx); ok {
		if e.Type == LongPressed {
			gtx.Source.Execute(clipboard.ReadCmd{Tag: c.msgpaste})
		} else { // LongPressCancelled
			gtx.Execute(key.FocusCmd{Tag: c.compose})
		}
	}

	// catch clipboard transfer triggered by long press and update composition
	if ev, ok := gtx.Event(transfer.TargetFilter{Target: c.msgpaste, Type: "application/text"}); ok {
		switch e := ev.(type) {
		case transfer.DataEvent:
			f := e.Open()
			defer f.Close()
			if b, err := io.ReadAll(f); err == nil {
				if c.compose.SelectionLen() > 0 {
					c.compose.Delete(1) // deletes the selection as a single rune
				}
				start, _ := c.compose.Selection()
				txt := c.compose.Text()
				c.compose.SetText(txt[:start] + string(b) + txt[start:])
				gtx.Execute(key.FocusCmd{Tag: c.compose})
			}
		}
	}

	if e, ok := c.edit.Update(gtx.Source); ok {
		if e.Kind == gesture.KindClick {
			return EditContact{nickname: c.nickname}
		}
	}
	if c.back.Clicked(gtx) {
		return BackEvent{}
	}
	if c.msgcopy.Clicked(gtx) {
		gtx.Source.Execute(clipboard.WriteCmd{
			Data: io.NopCloser(strings.NewReader(string(c.messageClicked.Plaintext))),
		})
		c.messageClicked = nil
	}
	if c.msgdetails.Clicked(gtx) {
		c.messageClicked = nil // not implemented
	}

	for msg, click := range c.messageClicks {
		if _, ok := click.Update(gtx.Source); ok {
			c.messageClicked = msg
		}
	}

	if _, ok := c.cancel.Update(gtx.Source); ok {
		c.messageClicked = nil
	}

	/* XXX: doesn't seem to work yet
	if e, ok := readMenuKeys(c, gtx); ok {
		if e.Name == key.NameEscape && e.State == key.Release {
			return BackEvent{}
		}
		if e.Name == key.NameF5 && e.State == key.Release {
			return EditContact{nickname: c.nickname}
		}
		if e.Name == key.NameUpArrow && e.State == key.Release {
			messageList.ScrollToEnd = false
			if messageList.Position.First > 0 {
				messageList.Position.First = messageList.Position.First - 1
			}
		}
		if e.Name == key.NameDownArrow && e.State == key.Release {
			messageList.ScrollToEnd = true
			messageList.Position.First = messageList.Position.First + 1
		}
		if e.Name == key.NamePageUp && e.State == key.Release {
			messageList.ScrollToEnd = false
			if messageList.Position.First-messageList.Position.Count > 0 {
				messageList.Position.First = messageList.Position.First - messageList.Position.Count
			}
		}
		if e.Name == key.NamePageDown && e.State == key.Release {
			messageList.ScrollToEnd = true
			messageList.Position.First = messageList.Position.First + messageList.Position.Count
		}
		return RedrawEvent{}
	}
	*/

	return nil
}

func layoutMessage(gtx C, msg *catshadow.Message, isSelected bool, expires time.Duration) D {

	var statusIcon *widget.Icon
	if msg.Outbound == true {
		statusIcon = queuedIcon
		switch {
		case !msg.Sent:
			statusIcon = queuedIcon
		case msg.Sent && !msg.Delivered:
			statusIcon = sentIcon
		case msg.Delivered:
			statusIcon = deliveredIcon
		default:
		}
	}

	return layout.Flex{Axis: layout.Vertical, Alignment: layout.End, Spacing: layout.SpaceBetween}.Layout(gtx,
		layout.Rigid(material.Body1(th, string(msg.Plaintext)).Layout),
		layout.Rigid(func(gtx C) D {
			in := layout.Inset{Top: unit.Dp(8), Bottom: unit.Dp(0), Left: unit.Dp(8), Right: unit.Dp(8)}
			return in.Layout(gtx, func(gtx C) D {
				timeLabel := strings.Replace(durafmt.ParseShort(time.Now().Round(0).Sub(msg.Timestamp).Truncate(time.Minute)).Format(units), "0 s", "now", 1)
				var whenExpires string
				if expires == 0 {
					whenExpires = ""
				} else {
					whenExpires = durafmt.ParseShort(msg.Timestamp.Add(expires).Sub(time.Now().Round(0).Truncate(time.Minute))).Format(units) + " remaining"
				}
				if isSelected {
					timeLabel = msg.Timestamp.Truncate(time.Minute).Format(time.RFC822)
					if msg.Outbound {
						timeLabel = "Sent: " + timeLabel
					} else {
						timeLabel = "Received: " + timeLabel
					}
				}
				if msg.Outbound {
					return layout.Flex{Axis: layout.Horizontal, Alignment: layout.End, Spacing: layout.SpaceBetween}.Layout(gtx,
						layout.Rigid(material.Caption(th, timeLabel).Layout),
						layout.Rigid(material.Caption(th, whenExpires).Layout),
						layout.Rigid(func(gtx C) D {
							return statusIcon.Layout(gtx, th.Palette.ContrastFg)
						}),
					)
					// do not show delivery status for received messages, instead show received timestamp
				} else {
					return layout.Flex{Axis: layout.Horizontal, Alignment: layout.End, Spacing: layout.SpaceBetween}.Layout(gtx,
						layout.Rigid(material.Caption(th, timeLabel).Layout),
						layout.Rigid(material.Caption(th, whenExpires).Layout),
					)
				}
			})
		}),
	)
}

func (c *conversationPage) Layout(gtx layout.Context) layout.Dimensions {
	// set focus on composition
	gtx.Execute(key.FocusCmd{Tag: c.compose})
	contact := c.a.c.GetContacts()[c.nickname]
	if n, ok := notifications[c.nickname]; ok {
		if c.a.focus {
			n.Cancel()
			delete(notifications, c.nickname)
		}
	}
	messages := c.a.c.GetSortedConversation(c.nickname)
	expires, _ := c.a.c.GetExpiration(c.nickname)
	bgl := Background{
		Color: th.Bg,
		Inset: layout.Inset{Top: unit.Dp(0), Bottom: unit.Dp(0), Left: unit.Dp(0), Right: unit.Dp(0)},
	}

	return layout.Flex{Axis: layout.Vertical, Spacing: layout.SpaceBetween, Alignment: layout.Middle}.Layout(gtx,
		layout.Rigid(func(gtx C) D {
			return bgl.Layout(gtx, func(gtx C) D {
				return layout.Flex{Axis: layout.Horizontal, Spacing: layout.SpaceBetween, Alignment: layout.Middle}.Layout(gtx,
					layout.Rigid(button(th, c.back, backIcon).Layout),
					layout.Rigid(func(gtx C) D {
						dims := layoutAvatar(gtx, c.a.c, c.nickname)
						a := clip.Rect(image.Rectangle{Max: dims.Size})
						t := a.Push(gtx.Ops)
						c.edit.Add(gtx.Ops)
						t.Pop()
						return dims
					}),
					layout.Rigid(material.Caption(th, c.nickname).Layout),
					layout.Rigid(func(gtx C) D {
						if contact.IsPending {
							return pandaIcon.Layout(gtx, th.Palette.ContrastFg)
						}
						return layout.Dimensions{}
					}),
					layout.Flexed(1, fill{th.Bg}.Layout),
				)
			},
			)
		}),
		layout.Flexed(2, func(gtx C) D {
			return bgl.Layout(gtx, func(ctx C) D {
				if len(messages) == 0 {
					return fill{th.Bg}.Layout(ctx)
				}

				dims := messageList.Layout(gtx, len(messages), func(gtx C, i int) layout.Dimensions {
					if _, ok := c.messageClicks[messages[i]]; !ok {
						c.messageClicks[messages[i]] = new(gesture.Click)
					}

					bgSender := Background{
						Color:  th.ContrastBg,
						Inset:  layout.Inset{Top: unit.Dp(8), Bottom: unit.Dp(8), Left: unit.Dp(8), Right: unit.Dp(12)},
						Radius: unit.Dp(10),
					}
					bgReceiver := Background{
						Color:  th.ContrastFg,
						Inset:  layout.Inset{Top: unit.Dp(8), Bottom: unit.Dp(8), Left: unit.Dp(12), Right: unit.Dp(8)},
						Radius: unit.Dp(10),
					}
					inbetween := layout.Inset{Top: unit.Dp(2)}
					if i > 0 {
						if messages[i-1].Outbound != messages[i].Outbound {
							inbetween = layout.Inset{Top: unit.Dp(8)}
						}
					}
					var dims D
					isSelected := messages[i] == c.messageClicked
					if messages[i].Outbound {
						dims = layout.Flex{Axis: layout.Horizontal, Alignment: layout.Baseline, Spacing: layout.SpaceAround}.Layout(gtx,
							layout.Flexed(1, fill{th.Bg}.Layout),
							layout.Flexed(5, func(gtx C) D {
								return inbetween.Layout(gtx, func(gtx C) D {
									return bgSender.Layout(gtx, func(gtx C) D {
										return layoutMessage(gtx, messages[i], isSelected, expires)
									})
								})
							}),
						)
					} else {
						dims = layout.Flex{Axis: layout.Horizontal, Alignment: layout.Baseline, Spacing: layout.SpaceAround}.Layout(gtx,
							layout.Flexed(5, func(gtx C) D {
								return inbetween.Layout(gtx, func(gtx C) D {
									return bgReceiver.Layout(gtx, func(gtx C) D {
										return layoutMessage(gtx, messages[i], isSelected, expires)
									})
								})
							}),
							layout.Flexed(1, fill{th.Bg}.Layout),
						)
					}
					a := clip.Rect(image.Rectangle{Max: dims.Size})
					t := a.Push(gtx.Ops)
					c.messageClicks[messages[i]].Add(gtx.Ops)
					t.Pop()
					return dims

				})
				if c.messageClicked != nil {
					a := clip.Rect(image.Rectangle{Max: dims.Size})
					t := a.Push(gtx.Ops)
					c.cancel.Add(gtx.Ops)
					t.Pop()
				}
				return dims
			})
		}),
		layout.Rigid(func(gtx C) D {
			bg := Background{
				Color: th.ContrastBg,
				Inset: layout.Inset{Top: unit.Dp(8), Bottom: unit.Dp(0), Left: unit.Dp(12), Right: unit.Dp(12)},
			}
			// return the menu laid out for message actions
			if c.messageClicked != nil {
				return bg.Layout(gtx, func(gtx C) D {
					return layout.Flex{Axis: layout.Horizontal, Spacing: layout.SpaceBetween, Alignment: layout.Baseline}.Layout(gtx,
						layout.Rigid(material.Button(th, c.msgcopy, "copy").Layout),
						layout.Flexed(1, fill{th.Bg}.Layout),
						layout.Rigid(material.Button(th, c.msgdetails, "details").Layout),
					)
				})
			}
			bgSender := Background{
				Color:  th.ContrastBg,
				Inset:  layout.Inset{Top: unit.Dp(8), Bottom: unit.Dp(8), Left: unit.Dp(12), Right: unit.Dp(12)},
				Radius: unit.Dp(10),
			}
			bgl := Background{
				Color: th.Bg,
				Inset: layout.Inset{Top: unit.Dp(8), Bottom: unit.Dp(0), Left: unit.Dp(0), Right: unit.Dp(0)},
			}

			return bgl.Layout(gtx, func(gtx C) D {
				return layout.Flex{Axis: layout.Horizontal, Spacing: layout.SpaceBetween, Alignment: layout.Middle}.Layout(gtx,
					layout.Flexed(1, fill{th.Bg}.Layout),
					layout.Flexed(5, func(gtx C) D {
						dims := bgSender.Layout(gtx, material.Editor(th, c.compose, "").Layout)
						t := pointer.PassOp{}.Push(gtx.Ops)
						defer t.Pop()
						a := clip.Rect(image.Rectangle{Max: dims.Size})
						x := a.Push(gtx.Ops)
						defer x.Pop()
						event.Op(gtx.Ops, c.msgpaste)
						return dims
					}),
					layout.Rigid(func(gtx C) D {
						return layout.Inset{Left: unit.Dp(8), Right: unit.Dp(8)}.Layout(gtx, button(th, c.send, sendIcon).Layout)
					}),
				)
			})
		}),
	)
}

func newConversationPage(a *App, nickname string) *conversationPage {
	ed := &widget.Editor{SingleLine: false, Submit: true}
	if runtime.GOOS == "android" {
		ed.Submit = false
	}

	p := &conversationPage{a: a, nickname: nickname,
		compose:       ed,
		messageClicks: make(map[*catshadow.Message]*gesture.Click),
		back:          &widget.Clickable{},
		msgcopy:       &widget.Clickable{},
		msgpaste:      NewLongPress(a.w.Invalidate, 800*time.Millisecond),
		msgdetails:    &widget.Clickable{},
		cancel:        new(gesture.Click),
		send:          &widget.Clickable{},
		edit:          new(gesture.Click),
	}
	return p
}
