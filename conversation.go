package main

import (
	"errors"
	"image"
	"io"
	"runtime"
	"strings"
	"sync"
	"time"

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
	"github.com/fxamacker/cbor/v2"
	"github.com/hako/durafmt"
	"github.com/katzenpost/hpqc/rand"
	"golang.org/x/exp/shiny/materialdesign/icons"
)

var (
	messageList      = &layout.List{Axis: layout.Vertical, ScrollToEnd: true}
	messageField     = &widget.Editor{SingleLine: true}
	editIcon, _      = widget.NewIcon(icons.ActionSettings)
	backIcon, _      = widget.NewIcon(icons.NavigationChevronLeft)
	sendIcon, _      = widget.NewIcon(icons.NavigationChevronRight)
	queuedIcon, _    = widget.NewIcon(icons.NotificationSync)
	sentIcon, _      = widget.NewIcon(icons.ActionDone)
	deliveredIcon, _ = widget.NewIcon(icons.ActionDoneAll)
	pandaIcon, _     = widget.NewIcon(icons.ActionPets)
	attachIcon, _    = widget.NewIcon(icons.EditorAttachFile)
	transfersIcon, _ = widget.NewIcon(icons.NotificationSync)

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

type ShowTransfers struct {
}

type NewTransfer struct {
	ID uint64
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
	edit           *widget.Clickable
	transfers      *widget.Clickable
	compose        *widget.Editor
	send           *widget.Clickable
	attach         *widget.Clickable
	back           *widget.Clickable
	msgpaste       *LongPress
	msgdetails     *widget.Clickable
	messageClicked uint64
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

func (c *conversationPage) Update(item interface{}) {
	switch i := item.(type) {
	case *DownloadHeader:
		serialized, err := cbor.Marshal(i)
		if err == nil {
			msg := &Message{
				ID:           rand.NewMath().Uint64(),
				Sent:         time.Now(),
				Type:         Attachment,
				Conversation: c.id,
				Body:         serialized,
			}
			c.a.db.SendMessage(c.id, msg)
		}
	default:
	}
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
			c.Update(nil)
			return MessageSent{conversation: c.id}
		} else {
			shortNotify("Send failed", err.Error())
			return nil
		}
	}
	if c.attach.Clicked(gtx) {
		return NewTransfer{}
	}
	if c.back.Clicked(gtx) {
		return BackEvent{}
	}
	if c.msgdetails.Clicked(gtx) {
		c.messageClicked = 0 // not implemented
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

	if c.edit.Clicked(gtx) {
		return EditConversation{ID: c.id}
	}

	if c.transfers.Clicked(gtx) {
		return ShowTransfers{}
	}

	if e, ok := shortcutEvents(gtx); ok {
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
	return nil
}

func layoutAttachment(gtx C, click *widget.Clickable, msg *Message) D {
	h := &DownloadHeader{}
	_, err := cbor.UnmarshalFirst(msg.Body, h)
	if err != nil {
		return material.Caption(th, "Invalid Attachment").Layout(gtx)
	}
	return layout.Flex{Axis: layout.Horizontal, Alignment: layout.End, Spacing: layout.SpaceBetween}.Layout(gtx,
		layout.Rigid(material.Caption(th, h.Name).Layout),
		layout.Rigid(button(th, click, attachIcon).Layout),
	)
}

func (c *conversationPage) layoutMessage(gtx C, msg *Message, expires time.Duration) D {

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
		layout.Rigid(func(gtx C) D {
			switch msg.Type {
			case Text:
				return material.Body1(th, string(msg.Body)).Layout(gtx)
			case Attachment:
				return layoutAttachment(gtx, c.transfers, msg)
			}
			return D{}
		}),
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

				if gtx.Focused(msg) {
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
	conv := c.conversation
	c.l.Unlock()
	title := conv.Title
	// set focus on composition
	gtx.Execute(key.FocusCmd{Tag: c.compose})
	return layout.Flex{Axis: layout.Vertical, Spacing: layout.SpaceBetween, Alignment: layout.Middle}.Layout(gtx,
		// layout back button, title
		layout.Rigid(func(gtx C) D {
			return bgList.Layout(gtx, func(gtx C) D {
				return layout.Flex{Axis: layout.Horizontal, Spacing: layout.SpaceBetween, Alignment: layout.Middle}.Layout(gtx,
					layout.Rigid(button(th, c.back, backIcon).Layout),
					layout.Rigid(button(th, c.edit, editIcon).Layout),
					layout.Rigid(material.Caption(th, title).Layout),
					layout.Flexed(1, fill{th.Bg}.Layout),
					layout.Rigid(button(th, c.transfers, transfersIcon).Layout),
				)
			},
			)
		}),
		// layout message list
		layout.Flexed(2, func(gtx C) D {
			// style bgList
			return bgList.Layout(gtx, func(ctx C) D {
				// if there are no Messages, return empty
				if len(conv.Messages) == 0 {
					return fill{th.Bg}.Layout(ctx)
				}
				return messageList.Layout(gtx, len(conv.Messages), c.layoutConversation)
			})
		}),

		// layout the message composition editor
		layout.Rigid(func(gtx C) D {
			return bgList.Layout(gtx, func(gtx C) D {
				return layout.Flex{Axis: layout.Horizontal, Spacing: layout.SpaceBetween, Alignment: layout.Middle}.Layout(gtx,
					// padding aligned for our speech bubble
					layout.Flexed(1, fill{th.Bg}.Layout),
					// editor box
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
					// attach button
					layout.Rigid(func(gtx C) D {
						return layout.Inset{Left: unit.Dp(8), Right: unit.Dp(8)}.Layout(gtx, button(th, c.attach, attachIcon).Layout)
					}),
					// send button
					layout.Rigid(func(gtx C) D {
						return layout.Inset{Left: unit.Dp(8), Right: unit.Dp(8)}.Layout(gtx, button(th, c.send, sendIcon).Layout)
					}),
				)
			})
		}),
	)
}

func (c *conversationPage) layoutConversation(gtx C, i int) layout.Dimensions {
	c.l.Lock()
	messages := c.conversation.Messages
	expires := c.conversation.MessageExpiration
	c.l.Unlock()
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
						return c.layoutMessage(gtx, msg, expires)
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
						return c.layoutMessage(gtx, msg, expires)
					})
				})
			}),
			layout.Flexed(1, fill{th.Bg}.Layout),
		)
	}
	a := clip.Rect(image.Rectangle{Max: dims.Size})
	t := a.Push(gtx.Ops)
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
		l:            new(sync.Mutex),
		id:           conversationId,
		conversation: conv,
		compose:      ed,
		back:         &widget.Clickable{},
		msgpaste:     NewLongPress(a.w.Invalidate, 800*time.Millisecond),
		msgdetails:   &widget.Clickable{},
		send:         &widget.Clickable{},
		attach:       &widget.Clickable{},
		edit:         &widget.Clickable{},
		transfers:    &widget.Clickable{},
		updateCh:     make(chan struct{}, 1),
		//messages:      []*Message, cache messages
	}
	return p
}
