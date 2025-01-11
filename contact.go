package main

import (
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"image"
	mrand "math/rand"
	"runtime"
	"strings"
	"sync"
	"time"

	"gioui.org/gesture"
	"gioui.org/io/clipboard"
	"gioui.org/io/key"
	"gioui.org/io/transfer"
	"gioui.org/layout"
	"gioui.org/op/clip"
	"gioui.org/op/paint"
	"gioui.org/unit"
	"gioui.org/widget"
	"gioui.org/widget/material"
	"github.com/benc-uk/gofract/pkg/colors"
	"github.com/benc-uk/gofract/pkg/fractals"
	"github.com/katzenpost/hpqc/nike"
	"github.com/katzenpost/hpqc/rand"
	qrcode "github.com/skip2/go-qrcode"
	"golang.org/x/exp/shiny/materialdesign/icons"
	"io"
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

	ErrAlreadyReading   = errors.New("Already Reading Contact")
	ErrNotReading       = errors.New("Not Reading Contact")
	ErrContactNotFound  = errors.New("Contact not found")
	ErrContactNotDialed = errors.New("Contact not Dialed")
	ErrOffline          = errors.New("Cannot send offline")

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

	// Outbound identifies the queue of messages
	// which allows for offline composition to contacts
	Outbound uint64

	// MyIdentity is the nike.PrivateKey used with PANDA
	MyIdentity nike.PrivateKey

	// Identity is the nike.PublicKey learned from PANDA
	Identity nike.PublicKey

	// SharedSecret is the passphrase used to add the contact.
	SharedSecret []byte
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
	initOnce  *sync.Once
}

// Layout returns a simple centered layout prompting user for contact nickname and secret
func (p *AddContactPage) Layout(gtx layout.Context) layout.Dimensions {
	// set the default window focus to nickname entry on first layout
	p.initOnce.Do(func() {
		if len(p.nickname.Text()) == 0 {
			gtx.Execute(key.FocusCmd{Tag: p.nickname})
		}
	})

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
	if p.back.Clicked(gtx) {
		return BackEvent{}
	}
	if ev, ok := p.nickname.Update(gtx); ok {
		switch ev.(type) {
		case widget.SubmitEvent:
			gtx.Execute(key.FocusCmd{Tag: p.secret})
		}
	}

	if ev, ok := p.newQr.Update(gtx.Source); ok {
		if ev.Kind == gesture.KindClick {
			p.contactal.Reset()
			p.secret.SetText(p.contactal.SharedSecret)
		}
	}

	if p.copy.Clicked(gtx) {
		gtx.Execute(clipboard.WriteCmd{
			Data: io.NopCloser(strings.NewReader(p.secret.Text())),
		})
	}

	if p.paste.Clicked(gtx) {
		gtx.Execute(clipboard.ReadCmd{Tag: p})
	}

	if ev, ok := gtx.Event(transfer.TargetFilter{Target: p, Type: "application/text"}); ok {
		switch e := ev.(type) {
		case transfer.DataEvent:
			f := e.Open()
			defer f.Close()
			if b, err := io.ReadAll(f); err == nil {
				p.secret.SetText(string(b))
				p.contactal.SharedSecret = string(b)
			}
		}
	}

	if ev, ok := p.newAvatar.Update(gtx.Source); ok {
		if ev.Kind == gesture.KindClick {
			p.contactal = NewContactal()
			p.secret.SetText(p.contactal.SharedSecret)
		}
	}

	if ev, ok := p.secret.Update(gtx); ok {
		switch ev.(type) {
		case widget.SubmitEvent:
			p.submit.Click()
		case widget.ChangeEvent:
			p.contactal.SharedSecret = p.secret.Text()
		}
	}
	if p.cancel.Clicked(gtx) {
		return BackEvent{}
	}
	if p.submit.Clicked(gtx) {
		if len(p.secret.Text()) < minPasswordLen {
			p.secret.SetText("")
			gtx.Execute(key.FocusCmd{Tag: p.secret})
			return nil
		}

		if len(p.nickname.Text()) == 0 {
			gtx.Execute(key.FocusCmd{Tag: p.nickname})
			return nil
		}

		contact, err := p.a.db.NewContact(p.nickname.Text(), []byte(p.secret.Text()))
		if err != nil {
			p.nickname.SetText("")
			p.secret.SetText("")
			return nil
		}

		// if we are online, start a PANDA exchange immediately
		// if not, the exchange must be started when the client comes online and tries to send a message.
		if p.a.Status() == StateOnline {
			// start a panda' exchange with contact
			err := p.a.doPANDAExchange(contact.ID)
			if err != nil {
				shortNotify("DoPANDAExchange", err.Error())
			}
		}

		sz := image.Point{X: gtx.Dp(unit.Dp(96)), Y: gtx.Dp(unit.Dp(96))}
		i := p.contactal.Render(sz)
		w := func(gtx C) D {
			return widget.Image{Fit: widget.Contain, Src: paint.NewImageOp(i)}.Layout(gtx)
		}

		avatars[contact.ID] = w
		_, err = p.a.db.NewConversation(contact.ID)
		if err == nil {
			// create a new conversation with this contact
			return AddContactComplete{id: contact.ID}
		}
		shortNotify("ConnectionStatusEvent", err.Error())
	}
	return nil
}

func (p *AddContactPage) Start(stop <-chan struct{}) {
}

func (AddContactPage) Update() {}

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

	p.initOnce = new(sync.Once)
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
