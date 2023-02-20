package main

import (
	"github.com/hako/durafmt"
	"golang.org/x/exp/shiny/materialdesign/icons"
	"image"
	"runtime"
	"strings"
	"time"

	"gioui.org/gesture"
	"gioui.org/io/clipboard"
	"gioui.org/io/key"
	"gioui.org/io/pointer"
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
	conversation   *Conversation
	avatar         *widget.Image
	edit           *gesture.Click
	compose        *widget.Editor
	send           *widget.Clickable
	back           *widget.Clickable
	cancel         *gesture.Click
	msgcopy        *widget.Clickable
	msgpaste       *LongPress
	msgdetails     *widget.Clickable
	messageClicked *Message
	messageClicks  map[*Message]*gesture.Click
}

func (c *conversationPage) Start(stop <-chan struct{}) {
}

type MessageSent struct {
	conversation uint64
}

func (c *conversationPage) Event(gtx layout.Context) interface{} {
	// receive keystroke to editor panel
	for _, ev := range c.compose.Events() {
		switch ev.(type) {
		case widget.SubmitEvent:
			c.send.Click()
		}
	}
	for _, ev := range c.msgpaste.Events(gtx.Queue) {
		switch ev.Type {
		case LongPressed:
			clipboard.ReadOp{Tag: c}.Add(gtx.Ops)
			return RedrawEvent{}
		default:
			// return focus to the editor
			c.compose.Focus()
		}
	}
	key.InputOp{Tag: c, Keys: shortcuts}.Add(gtx.Ops)
	for _, e := range gtx.Events(c) {
		switch e := e.(type) {
		case key.Event:
			if e.Name == key.NameEscape && e.State == key.Release {
				return BackEvent{}
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

		case clipboard.Event:
			if c.compose.SelectionLen() > 0 {
				c.compose.Delete(1) // deletes the selection as a single rune
			}
			start, _ := c.compose.Selection()
			txt := c.compose.Text()
			c.compose.SetText(txt[:start] + e.Text + txt[start:])
			c.compose.Focus()
		}
	}

	if c.send.Clicked() {
		msg := []byte(c.compose.Text())
		c.compose.SetText("")
		if len(msg) == 0 {
			return nil
		}
		//msgId := c.a.c.SendMessage(c.nickname, msg)
		return MessageSent{conversation: c.conversation.ID}
	}
	if c.back.Clicked() {
		return BackEvent{}
	}
	if c.msgcopy.Clicked() {
		clipboard.WriteOp{Text: string(c.messageClicked.Body)}.Add(gtx.Ops)
		c.messageClicked = nil
		return nil
	}
	if c.msgdetails.Clicked() {
		c.messageClicked = nil // not implemented
	}

	for msg, click := range c.messageClicks {
		for _, e := range click.Events(gtx.Queue) {
			if e.Type == gesture.TypeClick {
				c.messageClicked = msg
			}
		}
	}

	for _, e := range c.cancel.Events(gtx.Queue) {
		if e.Type == gesture.TypeClick {
			c.messageClicked = nil
		}
	}

	return nil
}

func layoutMessage(gtx C, msg *Message, isSelected bool, expires time.Duration) D {

	var statusIcon *widget.Icon
	if msg.Sender == 0 { // self
		statusIcon = queuedIcon
		switch {
		case msg.Sent.IsZero():
			statusIcon = queuedIcon
		case !msg.Sent.IsZero() && !msg.Acked.IsZero():
			statusIcon = sentIcon
		case !msg.Acked.IsZero():
			statusIcon = deliveredIcon
		default:
		}
	}

	return layout.Flex{Axis: layout.Vertical, Alignment: layout.End, Spacing: layout.SpaceBetween}.Layout(gtx,
		layout.Rigid(material.Body1(th, string(msg.Body)).Layout),
		layout.Rigid(func(gtx C) D {
			in := layout.Inset{Top: unit.Dp(8), Bottom: unit.Dp(0), Left: unit.Dp(8), Right: unit.Dp(8)}
			return in.Layout(gtx, func(gtx C) D {
				var ts time.Time
				if msg.Sender == 0 {
					ts = msg.Sent
				} else {
					ts = msg.Received
				}
				timeLabel := strings.Replace(durafmt.ParseShort(time.Now().Round(0).Sub(ts).Truncate(time.Minute)).Format(units), "0 s", "now", 1)
				var whenExpires string
				if expires == 0 {
					whenExpires = ""
				} else {
					whenExpires = durafmt.ParseShort(ts.Add(expires).Sub(time.Now().Round(0).Truncate(time.Minute))).Format(units) + " remaining"
				}
				if isSelected {
					timeLabel = ts.Truncate(time.Minute).Format(time.RFC822)
					if msg.Sender == 0 {
						timeLabel = "Sent: " + timeLabel
					} else {
						timeLabel = "Received: " + timeLabel
					}
				}
				if msg.Sender == 0 {
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
	messages := c.conversation.Messages
	expires := c.conversation.MessageExpiration
	bgl := Background{
		Color: th.Bg,
		Inset: layout.Inset{Top: unit.Dp(0), Bottom: unit.Dp(0), Left: unit.Dp(0), Right: unit.Dp(0)},
	}

	return layout.Flex{Axis: layout.Vertical, Spacing: layout.SpaceBetween, Alignment: layout.Middle}.Layout(gtx,
		layout.Rigid(func(gtx C) D {
			return bgl.Layout(gtx, func(gtx C) D {
				return layout.Flex{Axis: layout.Horizontal, Spacing: layout.SpaceBetween, Alignment: layout.Middle}.Layout(gtx,
					layout.Rigid(button(th, c.back, backIcon).Layout),
					layout.Rigid(material.Caption(th, c.conversation.Title).Layout),
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
						sent1 := messages[i-1].Sender == 0
						sent2 := messages[i].Sender == 0
						if sent1 != sent2 {
							inbetween = layout.Inset{Top: unit.Dp(8)}
						}
					}
					var dims D
					isSelected := messages[i] == c.messageClicked
					if messages[i].Sender == 0 {
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
						c.msgpaste.Add(gtx.Ops)
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

func newConversationPage(a *App, conversationId uint64) *conversationPage {
	ed := &widget.Editor{SingleLine: false, Submit: true}
	if runtime.GOOS == "android" {
		ed.Submit = false
	}

	var conv *Conversation
	var ok bool
	conv, ok = a.Conversations[conversationId]
	if !ok {
		conv = new(Conversation)
		a.Conversations[conversationId] = conv
	}

	p := &conversationPage{a: a, conversation: conv,
		compose:       ed,
		messageClicks: make(map[*Message]*gesture.Click),
		back:          &widget.Clickable{},
		msgcopy:       &widget.Clickable{},
		msgpaste:      NewLongPress(a.w.Invalidate, 800*time.Millisecond),
		msgdetails:    &widget.Clickable{},
		cancel:        new(gesture.Click),
		send:          &widget.Clickable{},
		edit:          new(gesture.Click),
	}
	p.compose.Focus()
	return p
}
