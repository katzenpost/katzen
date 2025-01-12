package main

import (
	"errors"
	"image"
	"io"
	"runtime"
	"strings"
	"sync"
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
	"github.com/dgraph-io/badger/v4"
	"github.com/hako/durafmt"
	"github.com/katzenpost/hpqc/rand"
	"golang.org/x/exp/shiny/materialdesign/icons"
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

	ErrConversationNotFound = errors.New("Conversation not found")
	ErrHalted               = errors.New("Halted")
)

// Conversation holds a multiparty conversation
type Conversation struct {
	sync.Mutex

	// Title is the string set to display at header of conversation
	Title string

	// ID is the group identifier for this conversation to tag messages to/from
	ID uint64

	// Contacts are the contacts present in this conversation
	Contacts []uint64

	// Messages are the messages in this conversation
	Messages []uint64

	// MessageExpiration is the duration after which conversation history is cleared
	MessageExpiration time.Duration

	// LastMessage is the timestamp of the last sent or received message
	LastMessage time.Time
}

func (c *Conversation) Add(contactID uint64) error {
	panic("NotImplemented")
	return nil
}

func (c *Conversation) Remove(contactID uint64) error {
	panic("NotImplemented")
	return nil
}

func (c *Conversation) Destroy() error {
	panic("NotImplemented")
	return nil
}

type EditConversation struct {
	ID uint64
}

type EditConversationComplete struct{}

type conversationPage struct {
	l              *sync.Mutex
	a              *App
	id             uint64
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
	messageClicked uint64
	messageClicks  map[uint64]*gesture.Click
	updateCh       chan struct{}
}

func (c *conversationPage) Start(stop <-chan struct{}) {
	go func() {
		for {
			select {
			case <-stop:
				return
			case <-c.updateCh:
				c.updateConversation()
			}
		}
	}()
}

func (c *conversationPage) Update() {
	select {
	case c.updateCh <- struct{}{}:
	default:
	}
}

func (c *conversationPage) updateConversation() {
	updated, err := c.a.db.GetConversation(c.id)
	if err == nil {
		c.l.Lock()
		c.conversation = updated
		c.l.Unlock()
	}
}

type MessageSent struct {
	conversation uint64
}

func (c *conversationPage) Event(gtx layout.Context) interface{} {
	// check for editor SubmitEvents
	if e, ok := c.compose.Update(gtx); ok {
		switch e.(type) {
		case widget.SubmitEvent:
			c.send.Click()
		}
	}
	if ev, ok := c.msgpaste.Update(gtx); ok {
		switch ev.Type {
		case LongPressed:
			gtx.Execute(clipboard.ReadCmd{Tag: c})
			return RedrawEvent{}
		default:
			// return focus to the editor
			gtx.Execute(key.FocusCmd{Tag: c.compose})
		}
	}
	if c.send.Clicked(gtx) {
		if len(c.compose.Text()) == 0 {
			return nil
		}
		msg := &Message{
			// XXX: truncate sender timestamps to some lower resolution
			ID:           rand.NewMath().Uint64(),
			Sent:         time.Now(),
			Type:         Text,
			Conversation: c.id,
			Body:         []byte(c.compose.Text()),
		}
		err := c.a.db.SendMessage(c.id, msg)
		if err == nil {
			c.compose.SetText("")
			c.Update()
			return MessageSent{conversation: c.id}
		} else {
			shortNotify("Send failed", err.Error())
			return nil
		}
	}
	if c.back.Clicked(gtx) {
		return BackEvent{}
	}
	if c.msgdetails.Clicked(gtx) {
		c.messageClicked = 0 // not implemented
	}
	if c.msgcopy.Clicked(gtx) {
		msg, err := c.a.db.GetMessage(c.messageClicked)
		if err == nil {
			gtx.Source.Execute(clipboard.WriteCmd{
				Data: io.NopCloser(strings.NewReader(string(msg.Body))),
			})
			c.messageClicked = 0
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
			return EditConversation{ID: c.id}
		}
	}
	for msg, click := range c.messageClicks {
		if _, ok := click.Update(gtx.Source); ok {
			c.messageClicked = msg
		}
	}

	if _, ok := c.cancel.Update(gtx.Source); ok {
		c.messageClicked = 0
	}

	if e, ok := shortcutEvents(gtx); ok {
		if e.State == key.Release {
			switch e.Name {
			case key.NameEscape:
				return BackEvent{}
			case key.NameUpArrow:
				messageList.ScrollToEnd = false
				if messageList.Position.First > 0 {
					messageList.Position.First = messageList.Position.First - 1
				}
			case key.NameDownArrow:
				messageList.ScrollToEnd = true
				messageList.Position.First = messageList.Position.First + 1
			case key.NamePageUp:
				messageList.ScrollToEnd = false
				if messageList.Position.First-messageList.Position.Count > 0 {
					messageList.Position.First = messageList.Position.First - messageList.Position.Count
				}
			case key.NamePageDown:
				messageList.ScrollToEnd = true
				messageList.Position.First = messageList.Position.First + messageList.Position.Count
			}
			return RedrawEvent{}
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
	c.l.Lock() // protect modification of c.conversation throughout Layout
	defer c.l.Unlock()
	title := c.conversation.Title
	// set focus on composition
	gtx.Execute(key.FocusCmd{Tag: c.compose})
	return layout.Flex{Axis: layout.Vertical, Spacing: layout.SpaceBetween, Alignment: layout.Middle}.Layout(gtx,
		// layout back button, title
		layout.Rigid(func(gtx C) D {
			return bgList.Layout(gtx, func(gtx C) D {
				return layout.Flex{Axis: layout.Horizontal, Spacing: layout.SpaceBetween, Alignment: layout.Middle}.Layout(gtx,
					layout.Rigid(button(th, c.back, backIcon).Layout),
					layout.Rigid(material.Caption(th, title).Layout),
					layout.Flexed(1, fill{th.Bg}.Layout),
				)
			},
			)
		}),
		// layout message list
		layout.Flexed(2, func(gtx C) D {
			return bgList.Layout(gtx, func(ctx C) D {
				if len(c.conversation.Messages) == 0 {
					return fill{th.Bg}.Layout(ctx)
				}
				dims := messageList.Layout(gtx, len(c.conversation.Messages), c.layoutConversation)
				if c.messageClicked != 0 {
					a := clip.Rect(image.Rectangle{Max: dims.Size})
					t := a.Push(gtx.Ops)
					c.cancel.Add(gtx.Ops)
					t.Pop()
				}
				return dims
			})
		}),
		layout.Rigid(func(gtx C) D {
			// return the menu laid out for message actions
			if c.messageClicked != 0 {
				return bg.Layout(gtx, func(gtx C) D {
					return layout.Flex{Axis: layout.Horizontal, Spacing: layout.SpaceBetween, Alignment: layout.Baseline}.Layout(gtx,
						layout.Rigid(material.Button(th, c.msgcopy, "copy").Layout),
						layout.Flexed(1, fill{th.Bg}.Layout),
						layout.Rigid(material.Button(th, c.msgdetails, "details").Layout),
					)
				})
			}
			return bgList.Layout(gtx, func(gtx C) D {
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

func (c *conversationPage) layoutConversation(gtx C, i int) layout.Dimensions {
	messages := c.conversation.Messages
	expires := c.conversation.MessageExpiration
	if _, ok := c.messageClicks[messages[i]]; !ok {
		c.messageClicks[messages[i]] = new(gesture.Click)
	}

	// make message bubbles separated when different people speak
	if i > 0 {
		msg1, err1 := c.a.db.GetMessage(messages[i-1])
		msg2, err2 := c.a.db.GetMessage(messages[i])
		if err1 == nil && err2 == nil {
			sent1 := msg1.Sender == 0
			sent2 := msg2.Sender == 0
			if sent1 != sent2 {
				inbetween = layout.Inset{Top: unit.Dp(8)}
			}
		}
	}
	var dims D
	isSelected := messages[i] == c.messageClicked
	msg, err := c.a.db.GetMessage(messages[i])
	if err != nil {
		panic(err)
	}
	// if this is a message sent by us
	if msg.Sender == 0 {
		dims = layout.Flex{Axis: layout.Horizontal, Alignment: layout.Baseline, Spacing: layout.SpaceAround}.Layout(gtx,
			layout.Flexed(1, fill{th.Bg}.Layout),
			layout.Flexed(5, func(gtx C) D {
				return inbetween.Layout(gtx, func(gtx C) D {
					return bgSender.Layout(gtx, func(gtx C) D {
						return layoutMessage(gtx, msg, isSelected, expires)
					})
				})
			}),
		)
		// or sent by someone else
	} else {
		dims = layout.Flex{Axis: layout.Horizontal, Alignment: layout.Baseline, Spacing: layout.SpaceAround}.Layout(gtx,
			layout.Flexed(5, func(gtx C) D {
				return inbetween.Layout(gtx, func(gtx C) D {
					return bgReceiver.Layout(gtx, func(gtx C) D {
						return layoutMessage(gtx, msg, isSelected, expires)
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
}

func newConversationPage(a *App, conversationId uint64) *conversationPage {
	ed := &widget.Editor{SingleLine: false, Submit: true}
	if runtime.GOOS == "android" {
		ed.Submit = false
	}

	conv, err := a.db.GetConversation(conversationId)
	if err == badger.ErrKeyNotFound {
		conv = new(Conversation)
		conv.ID = conversationId
		a.db.PutConversation(conv)
	}

	p := &conversationPage{a: a,
		l:             new(sync.Mutex),
		id:            conversationId,
		conversation:  conv,
		compose:       ed,
		messageClicks: make(map[uint64]*gesture.Click),
		back:          &widget.Clickable{},
		msgcopy:       &widget.Clickable{},
		msgpaste:      NewLongPress(a.w.Invalidate, 800*time.Millisecond),
		msgdetails:    &widget.Clickable{},
		cancel:        new(gesture.Click),
		send:          &widget.Clickable{},
		edit:          new(gesture.Click),
		updateCh:      make(chan struct{}, 1),
		//messages:      []*Message, cache messages
	}
	return p
}
