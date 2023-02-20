package main

import (
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"image"
	mrand "math/rand"
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

	/*
		XXX: decide how long term identity should be constructed for this client
		and what sort of end-to-end encryption should be used for each message
		e.g. doubleratchet or else ?

		we should also consider how the stream secret may be updated by protocol
		messages - ie rekeying the stream so as to implement forward secrecy.

		// An identity for this client
		Identity sign.PublicKey
		for establishing PQ
		// KeyExchange is the serialised double ratchet key exchange we generated.
		KeyExchange []byte

		// Rratchet is the client's double ratchet for end to end encryption
		Ratchet *ratchet.Ratchet
	*/

	// Stream is the reliable channel used to communicate with Contact
	Stream *stream.Stream

	// SharedSecret is the passphrase used to add the contact.
	SharedSecret []byte

	MessageExpiration time.Duration
}

// NewContact creates a new Contact
func (a *App) NewContact(nickname string, secret []byte) (*Contact, error) {
	for {
		id := uint64(rand.NewMath().Int63())
		if _, ok := a.Contacts[id]; ok {
			continue
		}
		return &Contact{ID: id, Nickname: nickname, SharedSecret: secret}, nil
	}
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
