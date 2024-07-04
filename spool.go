package main

import (
	"gioui.org/gesture"
	"gioui.org/layout"
	"gioui.org/op/clip"
	"gioui.org/unit"
	"gioui.org/widget"
	"gioui.org/widget/material"
	"gioui.org/x/notify"
	"github.com/katzenpost/katzenpost/catshadow"
	"image"
	"sync"
	"time"
	//"gioui.org/widget/material"
)

type SpoolPage struct {
	a              *App
	provider       *layout.List
	providerClicks map[string]*gesture.Click
	connect        *widget.Clickable
	settings       *widget.Clickable
	back           *widget.Clickable
	submit         *widget.Clickable
	once           *sync.Once
	errCh          chan error
}

func (p *SpoolPage) Start(stop <-chan struct{}) {
	// start a goroutine that redraws the page every second
	go func() {
		for {
			select {
			case <-stop:
				return
			case <-time.After(time.Second):
				p.a.w.Invalidate()
			}
		}
	}()
}

func (p *SpoolPage) Layout(gtx layout.Context) layout.Dimensions {
	bg := Background{
		Color: th.Bg,
		Inset: layout.Inset{},
	}

	providers, err := p.a.c.GetSpoolProviders()
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
					layout.Rigid(button(th, p.settings, settingsIcon).Layout),
					//layout.Rigid(button(th, p.addContact, addContactIcon).Layout),
				)
			}),
			// Add a caption
			layout.Rigid(func(gtx C) D {
				if err == catshadow.ErrNotOnline {
					return material.Body2(th, "Welcome to Katzen. Please connect to choose a message storage provider").Layout(gtx)
				}
				if isConnecting {
					return material.Body2(th, "Connecting...").Layout(gtx)
				}
				return material.Body2(th, "Please choose a message storage provider").Layout(gtx)
			}),

			// show list of providers
			layout.Flexed(1, func(gtx C) D {
				gtx.Constraints.Min.X = gtx.Dp(unit.Dp(300))
				// skip the provider list if there was an error
				if err != nil {
					return layout.Dimensions{}
				}
				return p.provider.Layout(gtx, len(providers), func(gtx C, i int) layout.Dimensions {
					in := layout.Inset{Top: unit.Dp(8), Bottom: unit.Dp(8), Left: unit.Dp(12), Right: unit.Dp(12)}

					// if the layout is selected, change background color
					bg := Background{Inset: in}
					if kb && i == selectedIdx {
						bg.Color = th.ContrastBg
					} else {
						bg.Color = th.Bg
					}

					// create a click handler for this provider
					if _, ok := p.providerClicks[providers[i]]; !ok {
						c := new(gesture.Click)
						p.providerClicks[providers[i]] = c
					}

					// attach click handler to this element
					dims := bg.Layout(gtx, func(gtx C) D {
						return material.Body2(th, providers[i]).Layout(gtx)
					})
					a := clip.Rect(image.Rectangle{Max: dims.Size})
					t := a.Push(gtx.Ops)
					p.providerClicks[providers[i]].Add(gtx.Ops)
					t.Pop()
					return dims
				})
			}),
		)
	})
}

func (p *SpoolPage) Event(gtx layout.Context) interface{} {
	if p.back.Clicked() {
		return BackEvent{}
	}
	if p.connect.Clicked() {
		if !isConnected && !isConnecting {
			return OnlineClick{}
		}
		return OfflineClick{}
	}
	if p.settings.Clicked() {
		return ShowSettingsClick{}
	}
	for provider, click := range p.providerClicks {
		for _, e := range click.Events(gtx.Queue) {
			if e.Type == gesture.TypeClick {
				provider := provider // copy reference to provider
				go p.once.Do(func() {
					select {
					case p.errCh <- p.a.c.CreateRemoteSpoolOn(provider):
					case <-p.a.c.HaltCh():
						return
					}
				})
			}
		}
	}
	select {
	case e := <-p.errCh:
		if e == nil {
			notify.Push("Success", "Katzen created a spool")
			return BackEvent{}
		} else {
			notify.Push("Failure", e.Error())
			p.once = new(sync.Once)
		}
	default:
	}

	return nil
}

func newSpoolPage(a *App) *SpoolPage {
	p := &SpoolPage{}
	p.provider = &layout.List{Axis: layout.Vertical}
	p.back = &widget.Clickable{}
	p.connect = &widget.Clickable{}
	p.settings = &widget.Clickable{}
	p.once = new(sync.Once)
	p.errCh = make(chan error)
	p.submit = &widget.Clickable{}
	p.providerClicks = make(map[string]*gesture.Click)
	p.a = a
	return p
}
