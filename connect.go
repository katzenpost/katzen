package main

import (
	"context"
	"sync"
	"time"

	"gioui.org/layout"
	"gioui.org/unit"
	"gioui.org/widget"
	"gioui.org/widget/material"
	"github.com/dgraph-io/badger/v4"
	"github.com/katzenpost/katzenpost/client"
	"github.com/katzenpost/katzenpost/core/crypto/rand"
	"golang.org/x/exp/shiny/materialdesign/icons"
)

type ConnectedState uint8

const (
	StateOffline ConnectedState = iota
	StateConnecting
	StateOnline
)

func (a *App) Status() ConnectedState {
	a.Lock()
	defer a.Unlock()
	return a.state
}

// Session returns the active Session
func (a *App) Session() *client.Session {
	a.Lock()
	defer a.Unlock()

	// XXX: this wont work because eventworker isn't per session
	if len(a.sessions) == 0 {
		return nil
	}

	// kludge
	n := rand.NewMath().Intn(len(a.sessions))
	i := 0
	for _, s := range a.sessions {
		if i == n {
			return s
		}
		i++
	}
	return nil
}

// start connecting automtaically if enabled
func (a *App) maybeAutoConnect() {
	doAutoConnect := false
	a.db.View(func(txn *badger.Txn) error {
		i, err := txn.Get([]byte("AutoConnect"))
		if err != nil {
			return err
		}
		return i.Value(func(val []byte) error {
			if val[0] == 0xFF {
				doAutoConnect = true
			}
			return nil
		})
	})

	if doAutoConnect {
		a.doConnectClick()
	}
}

func (a *App) doConnectClick() {
	a.Lock()
	switch a.state {
	case StateOffline:
	case StateConnecting:
		a.cancelConn()
		a.Unlock()
		return
	case StateOnline:
		// iterate over all sessions held by client and teardown
		// Note, we currently only make one session
		// but client will support making multiple sessions to different entry nodes
		a.state = StateOffline
		// XXX: we need to remove session from client... but how?
		wg := new(sync.WaitGroup)
		wg.Add(len(a.sessions))
		for _, s := range a.sessions {
			go func() {
				s.Shutdown()
				wg.Done()
			}()
		}
		a.Unlock()
		wg.Wait()
		a.Lock()
		a.sessions = make(map[uint64]*client.Session)
		a.connectOnce = new(sync.Once)
		a.Unlock()
		return
	}
	a.Unlock()

	a.connectOnce.Do(func() {
		// set state connecting ??
		ctx, cancel := context.WithTimeout(context.Background(), initialPKIConsensusTimeout)
		defer cancel()
		a.Lock()
		a.state = StateConnecting
		a.cancelConn = cancel
		a.Unlock()
		s, err := a.c.NewTOFUSession(ctx)
		if err != nil {
			a.Lock()
			a.state = StateOffline
			a.connectOnce = new(sync.Once)
			a.cancelConn = nil
			a.Unlock()
			shortNotify("Disconnected", err.Error())
			return
		}
		a.Lock()
		a.sessions[rand.NewMath().Uint64()] = s
		a.state = StateOnline
		a.Unlock()

		// start worker routine to consume events from this session
		a.Go(func() { a.eventSinkWorker(s) })

		// start worker routine to read from streams
		a.Go(func() { a.streamWorker(s) })

		// restart any unfinished key exchanges
		a.Go(a.restartPandaExchanges)
	})
}

type connectIcon struct {
	sync.Mutex

	th            *material.Theme
	clickable     *widget.Clickable
	current       *widget.Icon
	connected     *widget.Icon
	interval      time.Duration
	disconnected  *widget.Icon
	connecting    []*widget.Icon
	connectingIdx int
	a             *App
}

func NewConnectIcon(a *App, th *material.Theme, cl *widget.Clickable) *connectIcon {
	disconnected, _ := widget.NewIcon(icons.DeviceSignalWiFiOff)
	w1, _ := widget.NewIcon(icons.DeviceSignalWiFi1Bar)
	w2, _ := widget.NewIcon(icons.DeviceSignalWiFi2Bar)
	w3, _ := widget.NewIcon(icons.DeviceSignalWiFi3Bar)
	connected, _ := widget.NewIcon(icons.DeviceSignalWiFi4Bar)
	i := &connectIcon{
		a:            a,
		th:           th,
		clickable:    cl,
		current:      disconnected,
		interval:     time.Second, // animation update interval
		disconnected: disconnected,
		connecting:   []*widget.Icon{w1, w2, w3},
		connected:    connected,
	}
	return i
}

func (i *connectIcon) Start(stop <-chan struct{}) {
	go func() {
		numIcons := len(i.connecting)
		for {
			select {
			case <-stop:
				return
			case <-time.After(i.interval):
				i.Lock()
				switch i.a.Status() {
				case StateOffline:
					i.current = i.disconnected
				case StateConnecting:
					i.current = i.connecting[i.connectingIdx]
					i.connectingIdx = (i.connectingIdx + 1) % numIcons
				case StateOnline:
					i.current = i.connected
				}
				i.Unlock()
				i.a.w.Invalidate() // redraw
			}
		}
	}()
}

func (i *connectIcon) Layout(gtx layout.Context) layout.Dimensions {
	i.Lock()
	defer i.Unlock()
	return material.IconButtonStyle{
		Background: th.Palette.Bg,
		Color:      th.Palette.ContrastFg,
		Icon:       i.current,
		Size:       unit.Dp(20),
		Inset:      layout.UniformInset(unit.Dp(8)),
		Button:     i.clickable,
	}.Layout(gtx)
}

type connectingPage struct {
	result chan interface{}
}

func (p *connectingPage) Layout(gtx layout.Context) layout.Dimensions {
	bg := Background{
		Color: th.Bg,
		Inset: layout.Inset{},
	}

	return bg.Layout(gtx, func(gtx C) D { return layout.Center.Layout(gtx, material.Caption(th, "Stand by... connecting").Layout) })
}

func (p *connectingPage) Start(stop <-chan struct{}) {
}

type connectError struct {
	err error
}

type connectSuccess struct {
	client *client.Client
}

func (p *connectingPage) Event(gtx layout.Context) interface{} {
	select {
	case r := <-p.result:
		switch r := r.(type) {
		case error:
			return connectError{err: r}
		case *client.Client:
			return connectSuccess{client: r}
		}
	default:
	}
	return nil
}

func newConnectingPage(result chan interface{}) *connectingPage {
	p := new(connectingPage)
	p.result = result
	return p
}
