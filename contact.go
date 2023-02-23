package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"image"
	mrand "math/rand"
	"os"
	"runtime"
	"sync"
	"time"

	"gioui.org/gesture"
	"gioui.org/io/clipboard"
	"gioui.org/layout"
	"gioui.org/op/clip"
	"gioui.org/op/paint"
	"gioui.org/unit"
	"gioui.org/widget"
	"gioui.org/widget/material"
	"github.com/benc-uk/gofract/pkg/colors"
	"github.com/benc-uk/gofract/pkg/fractals"
	"github.com/fxamacker/cbor/v2"
	"github.com/katzenpost/katzenpost/core/crypto/ecdh"
	"github.com/katzenpost/katzenpost/core/crypto/nike"
	"github.com/katzenpost/katzenpost/core/crypto/rand"
	"github.com/katzenpost/katzenpost/stream"
	qrcode "github.com/skip2/go-qrcode"
	"golang.org/x/exp/shiny/materialdesign/icons"
)

// AddContactComplete is emitted when NewContact has been called
type AddContactComplete struct {
	id uint64
}

var (
	copyIcon, _   = widget.NewIcon(icons.ContentContentCopy)
	pasteIcon, _  = widget.NewIcon(icons.ContentContentPaste)
	submitIcon, _ = widget.NewIcon(icons.NavigationCheck)
	cancelIcon, _ = widget.NewIcon(icons.NavigationCancel)

	ErrContactNotFound  = errors.New("Contact not found")
	ErrContactNotDialed = errors.New("Contact not Dialed")

	week              = 7 * 24 * time.Hour
	defaultExpiration = week
)

// Contact represents a conversion party
type Contact struct {
	sync.Mutex

	// ID is the local unique contact ID.
	ID uint64

	// Nickname is also unique locally.
	Nickname string

	// IsPending is true if the key exchange has not been completed.
	IsPending bool

	// PandaKeyExchange is the serialised PANDA key exchange we generated.
	PandaKeyExchange []byte

	// PandaResult contains an error message if the PANDA exchange fails.
	PandaResult string

	// Stream is the reliable channel used to communicate with Contact
	Stream *stream.Stream

	// MyIdentity is the ecdh.PrivateKey used with PANDA
	MyIdentity nike.PrivateKey

	// SharedSecret is the passphrase used to add the contact.
	SharedSecret []byte

	// partial reads of cbor bytes get stashed here until a complete object
	// can be deserialized
	CborBuffer *bytes.Buffer
}

// NewContact creates a new Contact from a shared secret (dialer)
func (a *App) NewContact(nickname string, secret []byte) (*Contact, error) {
	for {
		id := uint64(rand.NewMath().Int63())
		if _, ok := a.Contacts[id]; ok {
			continue
		}
		// generate a new ecdh keypair as a long-term identity with this contact
		// for re-keying, etc, exchanged as part of PANDA
		sK, _ := ecdh.NewKeypair(rand.Reader)
		c := &Contact{ID: id, Nickname: nickname, MyIdentity: sK, SharedSecret: secret, IsPending: true}
		a.Contacts[id] = c

		// if we are online, start a PANDA exchange immediately
		// if not, the exchange must be started when the client comes online and tries to send a message.
		if a.Status() == StateOnline {
			// start a panda' exchange with contact
			err := a.doPANDAExchange(id)
			if err != nil {
				return nil, err
			}
		}
		return c, nil
	}
}

func (a *App) sendToContact(id uint64, msg *Message) error {
	a.Lock()
	defer a.Unlock()
	c, ok := a.Contacts[id]
	if !ok {
		return ErrContactNotFound
	}
	if a.state != StateOnline { // we hold a.Lock()
		return errors.New("Cannot send offline")
	}
	l := a.c.GetLogger("sendToContact" + c.Nickname)
	l.Debugf("sendToContact1")
	// Send a Message to Contact
	c.Lock()
	defer c.Unlock()

	if c.Stream == nil {
		return ErrContactNotDialed
	}

	// This can also block the UX thread because Write() to a Stream can block...
	// We probably want to use the SetDeadline method  (but how? does cbor.NewEncoder support
	// a context or similar for cancelling a write?
	enc := cbor.NewEncoder(c.Stream)
	c.Stream.Timeout = 42 * time.Second
	err := enc.Encode(msg)
	if err == os.ErrDeadlineExceeded {
		l.Debugf("ok %s", err)
		// set status of Message kj
	} else {
		// do stuff if the stream isn't running, etc
	}

	// XXX: how do we know when the stream has transmitted messages to the network?
	// XXX: how do we know when the message has been acknowledged?
	return err
}

// readFromContact is called by the streamWorker
func (a *App) readFromContact(id uint64) error {
	a.Lock()
	c, ok := a.Contacts[id]
	if !ok {
		a.Unlock()
		return ErrContactNotFound
	}
	l := a.c.GetLogger("readFromContact")
	a.Unlock()
	// try to read any buffered data
	// XXX: unclear what happens here if a cbor object
	// spans multiple messages and we get a partial read
	// maybe we need a buffered reader that accumulates
	// things read from Stream so that we dont throw away
	// any data on a timeout that doesn't deserialize into
	// a struct
	//c.Stream.Timeout = 2 * time.Second

	c.Lock()
	if c.Stream == nil {
		c.Unlock()
		return ErrContactNotDialed
	}

	// XXX: we need to double buffer the raw reads from Stream so that
	// no bytes are lost on a timeout while deserializing

	if c.CborBuffer == nil {
		c.CborBuffer = new(bytes.Buffer)
	}
	newBuf := new(bytes.Buffer)
	var dec *cbor.Decoder
	if c.CborBuffer.Len() > 0 {
		l.Debugf("io.MultiReader(c.CborBuffer, c.Stream), newBuf): CborBuffer.Len() %d", c.CborBuffer.Len())
		dec = cbor.NewDecoder(io.TeeReader(io.MultiReader(c.CborBuffer, c.Stream), newBuf))
	} else {
		dec = cbor.NewDecoder(io.TeeReader(c.Stream, newBuf))
	}
	c.Unlock()
	m := new(Message)
	err := dec.Decode(m)
	if err != nil {
		// timed out, most likely
		if newBuf.Len() > 0 {
			// partial read of stream
			c.Lock()
			c.CborBuffer = newBuf
			c.Unlock()
		}
		return err
	} else {
		// how many bytes were read? ...
		l.Debugf("newBuf.Len(): %d", newBuf.Len())
		if  c.CborBuffer.Len() > 0 {
			l.Debugf("short read of CborBuffer: %d", c.CborBuffer.Len())
			if newBuf.Len() > 0 {
				// Decoded an object from CborBuffer, but also read Stream,:
				n, err := io.Copy(c.CborBuffer, newBuf)
				l.Debugf("short read of buf, %d %v io.Copy(c.CborBuffer, newBuf)", n, err)
				panic("does this happen?")
			}
		}
	}

	m.Sender = c.ID
	m.Received = time.Now()
	// if we have a conversation from this contact
	co, ok := a.Conversations[m.Conversation]
	if !ok {
		// Create a new conversation with this ID
		co = &Conversation{ID: m.Conversation,
			Title:    "Conversation with " + c.Nickname,
			Contacts: []*Contact{c}, Messages: []*Message{},
			MessageExpiration: defaultExpiration,
		}
		a.Conversations[m.Conversation] = co
	}
	co.Lock()
	defer co.Unlock()
	co.Messages = append(co.Messages, m)
	return nil
}

// A contactal is a fractal and secret that represents a user identity
type Contactal struct {
	SharedSecret string
}

// NewContactal returns a new randomized Contactal
func NewContactal() *Contactal {
	secret := make([]byte, 32)
	rand.Reader.Read(secret)
	return &Contactal{SharedSecret: base64.StdEncoding.EncodeToString(secret)}
}

// Render returns a visual representation of the Contactal's SharedSecret, by
// using a DeterministicRandReader to derive parameters for fractals.Fractal
// and fractals.Fractal.Render
func (c *Contactal) Render(sz image.Point) image.Image {
	img := image.NewRGBA(image.Rectangle{Max: sz})
	g := colors.GradientTable{}
	var b [6]byte
	// ensure the secret is 32b
	s := sha256.Sum256([]byte(c.SharedSecret))
	r, _ := rand.NewDeterministicRandReader(s[:])

	// generate a random gradient table. 42 colors is arbitrary, but looks nice.
	for i := 0; i < 42; i++ {
		r.Read(b[:])
		clrString := fmt.Sprintf("#%02x%02x%02x", b[:2], b[2:4], b[4:])
		g.AddToTable(clrString, float64(i)/42.0)
	}

	// initialize a deterministic math/rand so
	m := mrand.New(r)
	f := &fractals.Fractal{FractType: "julia",
		Center:       fractals.ComplexPair{m.Float64(), m.Float64()},
		MagFactor:    1.0,
		MaxIter:      90,
		W:            1.0,
		H:            1.0,
		ImgWidth:     sz.X,
		JuliaSeed:    fractals.ComplexPair{m.Float64(), m.Float64()},
		InnerColor:   "#000000",
		FullScreen:   false,
		ColorRepeats: 2.0}

	f.Render(img, g)
	return img
}

// Layout the Contactal
func (c *Contactal) Layout(gtx C) D {
	return layout.Center.Layout(gtx, func(gtx C) D {
		cc := clipCircle{}
		return cc.Layout(gtx, func(gtx C) D {
			x := gtx.Constraints.Max.X
			y := gtx.Constraints.Max.Y
			if x > y {
				x = y
			}
			sz := image.Point{X: x, Y: x}

			gtx.Constraints = layout.Exact(gtx.Constraints.Constrain(sz))
			i := c.Render(sz)
			return widget.Image{Fit: widget.Contain, Src: paint.NewImageOp(i)}.Layout(gtx)
		})
	})
}

// Generate a QR code for the serialised Contactal
func (c *Contactal) QR() (*qrcode.QRCode, error) {
	return qrcode.New(c.SharedSecret, qrcode.High)
}

// Reset Re-Initializes the shared secret.
func (c *Contactal) Reset() {
	var b [32]byte
	rand.Reader.Read(b[:])
	c.SharedSecret = base64.StdEncoding.EncodeToString(b[:])
}

// AddContactPage is the page for adding a new contact
type AddContactPage struct {
	a         *App
	nickname  *widget.Editor
	avatar    *widget.Image
	contactal *Contactal
	copy      *widget.Clickable
	paste     *widget.Clickable
	back      *widget.Clickable
	newAvatar *gesture.Click
	newQr     *gesture.Click
	secret    *widget.Editor
	submit    *widget.Clickable
	cancel    *widget.Clickable
}

// Layout returns a simple centered layout prompting user for contact nickname and secret
func (p *AddContactPage) Layout(gtx layout.Context) layout.Dimensions {
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
					layout.Rigid(material.H6(th, "Add Contact").Layout),
					layout.Flexed(1, fill{th.Bg}.Layout))
			}),
			// Nickname and Avatar image
			layout.Flexed(1, func(gtx C) D {
				return layout.Flex{Axis: layout.Horizontal}.Layout(gtx,
					layout.Flexed(1, func(gtx C) D {
						return layout.Center.Layout(gtx, material.Editor(th, p.nickname, "Nickname").Layout)
					}),
					layout.Flexed(1, func(gtx C) D {
						dims := p.contactal.Layout(gtx)
						a := clip.Rect(image.Rectangle{Max: dims.Size})
						t := a.Push(gtx.Ops)
						p.newAvatar.Add(gtx.Ops)
						t.Pop()
						return dims
					}),
				)
			}),
			// secret entry and QR image
			layout.Flexed(1, func(gtx C) D {
				return layout.Flex{Axis: layout.Horizontal}.Layout(gtx,
					layout.Flexed(1, func(gtx C) D {
						// vertical secret and copy/paste controls beneath
						return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
							// secret string
							layout.Flexed(1, func(gtx C) D {
								in := layout.Inset{Left: unit.Dp(8), Right: unit.Dp(8), Top: unit.Dp(8), Bottom: unit.Dp(8)}
								return in.Layout(gtx, func(gtx C) D {
									return layout.Center.Layout(gtx, material.Editor(th, p.secret, "Secret").Layout)
								})
							}),
							// copy/paste
							layout.Rigid(func(gtx C) D {
								return layout.Center.Layout(gtx, func(gtx C) D {
									return layout.Flex{Axis: layout.Horizontal, Spacing: layout.SpaceBetween, Alignment: layout.End}.Layout(gtx,
										layout.Flexed(1, button(th, p.copy, copyIcon).Layout),
										layout.Flexed(1, button(th, p.paste, pasteIcon).Layout),
										layout.Flexed(1, button(th, p.submit, submitIcon).Layout),
										layout.Flexed(1, button(th, p.cancel, cancelIcon).Layout),
									)
								})
							}),
						)
					}),
					// the image widget of qrcode
					layout.Flexed(1, func(gtx C) D {
						return layout.Center.Layout(gtx, p.layoutQr)
					}),
				)
			}),
		)
	})
}

// Event catches the widget submit events and calls NewContact
func (p *AddContactPage) Event(gtx layout.Context) interface{} {
	if p.back.Clicked() {
		return BackEvent{}
	}
	for _, ev := range p.nickname.Events() {
		switch ev.(type) {
		case widget.SubmitEvent:
			p.secret.Focus()
		}
	}

	for _, e := range p.newQr.Events(gtx.Queue) {
		if e.Type == gesture.TypeClick {
			p.contactal.Reset()
			p.secret.SetText(p.contactal.SharedSecret)
		}
	}

	if p.copy.Clicked() {
		clipboard.WriteOp{Text: p.secret.Text()}.Add(gtx.Ops)
		return nil
	}

	if p.paste.Clicked() {
		clipboard.ReadOp{Tag: p}.Add(gtx.Ops)
	}

	for _, e := range gtx.Events(p) {
		ce := e.(clipboard.Event)
		p.secret.SetText(ce.Text)
		p.contactal.SharedSecret = ce.Text
		return RedrawEvent{}
	}

	for _, e := range p.newAvatar.Events(gtx.Queue) {
		if e.Type == gesture.TypeClick {
			p.contactal = NewContactal()
			p.secret.SetText(p.contactal.SharedSecret)
			return RedrawEvent{}
		}
	}

	for _, ev := range p.secret.Events() {
		switch ev.(type) {
		case widget.SubmitEvent:
			p.submit.Click()
		case widget.ChangeEvent:
			p.contactal.SharedSecret = p.secret.Text()
			return RedrawEvent{}
		}
	}
	if p.cancel.Clicked() {
		return BackEvent{}
	}
	if p.submit.Clicked() {
		if len(p.secret.Text()) < minPasswordLen {
			p.secret.SetText("")
			p.secret.Focus()
			return nil
		}

		if len(p.nickname.Text()) == 0 {
			p.nickname.Focus()
			return nil
		}

		contact, err := p.a.NewContact(p.nickname.Text(), []byte(p.secret.Text()))
		if err != nil {
			p.nickname.SetText("")
			p.secret.SetText("")
			return nil
		}

		sz := image.Point{X: gtx.Dp(unit.Dp(96)), Y: gtx.Dp(unit.Dp(96))}
		i := p.contactal.Render(sz)
		w := func(gtx C) D {
			return widget.Image{Fit: widget.Contain, Src: paint.NewImageOp(i)}.Layout(gtx)
		}

		p.a.Contacts[contact.ID] = contact
		avatars[contact.ID] = w
		conv, err := p.a.NewConversation(contact.ID)
		if err == nil {
			// create a new conversation with this contact
			p.a.Conversations[conv.ID] = conv
			return AddContactComplete{id: contact.ID}
		}
	}
	return nil
}

func (p *AddContactPage) Start(stop <-chan struct{}) {
}

func newAddContactPage(a *App) *AddContactPage {
	p := &AddContactPage{}
	p.a = a
	p.nickname = &widget.Editor{SingleLine: true, Submit: true}
	p.secret = &widget.Editor{SingleLine: false, Submit: true}
	if runtime.GOOS == "android" {
		p.secret.Submit = false
	}

	p.newAvatar = new(gesture.Click)
	p.newQr = new(gesture.Click)
	p.back = &widget.Clickable{}
	p.copy = &widget.Clickable{}
	p.paste = &widget.Clickable{}
	p.submit = &widget.Clickable{}
	p.cancel = &widget.Clickable{}

	// generate random avatar parameters
	p.contactal = NewContactal()
	p.secret.SetText(p.contactal.SharedSecret)
	p.nickname.Focus()
	return p
}

func (p *AddContactPage) layoutQr(gtx C) D {
	in := layout.Inset{}
	dims := in.Layout(gtx, func(gtx C) D {
		x := gtx.Constraints.Max.X
		y := gtx.Constraints.Max.Y
		if x > y {
			x = y
		}

		sz := image.Point{X: x, Y: x}
		gtx.Constraints = layout.Exact(gtx.Constraints.Constrain(sz))
		qr, err := p.contactal.QR()
		if err != nil {
			return layout.Center.Layout(gtx, material.Caption(th, "QR").Layout)
		}
		qr.BackgroundColor = th.Bg
		qr.ForegroundColor = th.Fg

		i := qr.Image(x)
		return widget.Image{Fit: widget.ScaleDown, Src: paint.NewImageOp(i)}.Layout(gtx)

	})
	a := clip.Rect(image.Rectangle{Max: dims.Size})
	t := a.Push(gtx.Ops)
	p.newQr.Add(gtx.Ops)
	t.Pop()
	return dims

}

func button(th *material.Theme, button *widget.Clickable, icon *widget.Icon) material.IconButtonStyle {
	return material.IconButtonStyle{
		Background: th.Palette.Bg,
		Color:      th.Palette.ContrastFg,
		Icon:       icon,
		Size:       unit.Dp(20),
		Inset:      layout.UniformInset(unit.Dp(8)),
		Button:     button,
	}
}
