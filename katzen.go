package main

import (
	_ "embed"
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sync"

	"github.com/dgraph-io/badger/v4"
	"github.com/fxamacker/cbor/v2"
	"github.com/katzenpost/katzenpost/client"
	"github.com/katzenpost/katzenpost/core/worker"
	"time"

	"gioui.org/app"
	_ "gioui.org/app/permission/foreground"
	_ "gioui.org/app/permission/storage"
	"gioui.org/font/gofont"
	"gioui.org/io/key"
	"gioui.org/io/system"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/unit"
	"gioui.org/widget/material"
	"gioui.org/x/notify"
	"net/http"
	_ "net/http/pprof"
)

const (
	initialPKIConsensusTimeout = 120 * time.Second
	notificationTimeout        = 30 * time.Second
	keySize                    = 32 // symmetric key size for badger db encryption (AES-256)
)

var (
	dataDirName = "katzen"

	// obtain the default data location
	dir, _ = app.DataDir()

	// path to default profile
	dataDir = filepath.Join(dir, dataDirName, "default")

	profilePath      = flag.String("p", dataDir, "Path to application profile")
	clientConfigFile = flag.String("f", "", "Path to the client config file.")
	debug            = flag.Int("d", 0, "Port for net/http/pprof listener")

	minPasswordLen = 5                // XXX pick something reasonable
	updateInterval = 1 * time.Second // try to read from contacts every updateInterval

	notifications = make(map[string]notify.Notification)

	//go:embed default_config_without_tor.toml
	cfgWithoutTor []byte
	//go:embed default_config_with_tor.toml
	cfgWithTor []byte

	// theme
	th = func() *material.Theme {
		th := material.NewTheme(gofont.Collection())
		th.Bg = rgb(0x0)
		th.Fg = rgb(0xFFFFFFFF)
		th.ContrastBg = rgb(0x22222222)
		th.ContrastFg = rgb(0x77777777)
		return th
	}()
)

type App struct {
	sync.Mutex
	worker.Worker

	endBg func()
	w     *app.Window
	ops   *op.Ops
	c     *client.Client

	db *badger.DB

	cancelConn    func()
	state         ConnectedState
	sessions      map[uint64]*client.Session
	Contacts      map[uint64]*Contact
	Conversations map[uint64]*Conversation
	messageChans  map[uint64]chan *Message

	stack pageStack
	focus bool
	stage system.Stage

	connectOnce *sync.Once

	cmdCh chan streamCmd
}

type Command uint8

const (
	Start Command = iota
	Stop
)

// contacts have streams, commands act on contacts
type streamCmd struct {
	Command   Command
	ContactID uint64
}

func (a *App) startTransport(id uint64) error {
	c, ok := a.Contacts[id]
	if !ok {
		return ErrContactNotFound
	}
	if c.Transport == nil {
		return ErrContactNotDialed
	}

	if _, ok := a.messageChans[id]; ok {
		return ErrAlreadyReading
	}
	a.messageChans[id] = c.Messages(a.HaltCh())
	return nil
}

func (a *App) stopTransport(id uint64) error {
	c, ok := a.Contacts[id]
	if !ok {
		return ErrContactNotFound
	}
	if c.Transport == nil {
		return ErrContactNotDialed
	}
	c.Transport.Halt()
	return nil
}

func (a *App) haltAllTransports() {
	for _, c := range a.Contacts {
		c.Lock()
		if !c.IsPending {
			c.Transport.Halt()
		}
		c.Unlock()
	}
}

func (a *App) streamWorker(s *client.Session) {
	// add active streams to active list
	// read messages from each contact
	// write to the appropriate conversation
	// streamWorker returns when session halts

	for {
		select {
		case <-s.HaltCh():
			a.haltAllTransports()
			return
		case cmd := <-a.cmdCh:
			switch cmd.Command {
			case Start:
				a.startTransport(cmd.ContactID)
			case Stop:
				a.stopTransport(cmd.ContactID)
			default:
				panic(cmd)
			}
		case <-time.After(updateInterval):
		}

		// send and receive messages from each contact
		for id, msgCh := range a.messageChans {
			// send messages if contact has pending
			// XXX: refactor
			a.Lock()
			c, ok := a.Contacts[id]
			a.Unlock()
			if !ok {
				panic("wtf")
				a.stopTransport(id)
			}

			c.Lock()
			msg, err := c.Outbound.Peek()
			if err == nil {
				enc := cbor.NewEncoder(c.Transport)
				err := enc.Encode(msg)
				if err == nil {
					c.Outbound.Pop()
				}
			}
			c.Unlock()

			select {
			case m, ok := <-msgCh:
				// apply our ID to the Message
				m.Sender = id
				if !ok {
					a.stopTransport(id)
				}
				a.deliverMessage(m)
			default:
				// skip
			}
		}

	}
}

func (a *App) deliverMessage(m *Message) {
	if c, ok := a.Conversations[m.Conversation]; ok {
		m.Received = time.Now()
		c.Messages = append(c.Messages, m)
	}
}

func (a *App) eventSinkWorker(s *client.Session) {
	for {
		select {
		case <-s.HaltCh(): // session is halted
			return
		case e := <-s.EventSink:
			err := a.handleClientEvent(e)
			if err != nil {
				return
			}
		}
	}
}

func newApp(w *app.Window) *App {
	a := &App{
		Contacts:      make(map[uint64]*Contact),
		Conversations: make(map[uint64]*Conversation),
		sessions:      make(map[uint64]*client.Session),
		messageChans:  make(map[uint64]chan *Message),
		w:             w,
		ops:           &op.Ops{},
		connectOnce:   new(sync.Once),
	}
	return a
}

func (a *App) Layout(gtx layout.Context) {
	a.update(gtx)
	a.stack.Current().Layout(gtx)
}

func (a *App) update(gtx layout.Context) {
	page := a.stack.Current()
	if e := page.Event(gtx); e != nil {
		switch e := e.(type) {
		case RedrawEvent:
			a.w.Invalidate()
		case BackEvent:
			a.stack.Pop()
		case signInStarted:
			p := newUnlockPage(e.result)
			a.stack.Clear(p)
		case unlockError:
			a.stack.Clear(newSignInPage(a))
		case restartClient:
			a.db.Close()
			a.stack.Clear(newSignInPage(a))
		case unlockSuccess:
			// validate the statefile somehow
			c := e.client
			a.c = c
			go a.maybeAutoConnect()
			a.stack.Clear(newHomePage(a))
		case ConnectClick:
			go a.doConnectClick()
		case ShowSettingsClick:
			a.stack.Push(newSettingsPage(a))
		case AddContactClick:
			a.stack.Push(newAddContactPage(a))
		case AddContactComplete:
			a.stack.Pop()
		case ChooseConvoClick:
			a.stack.Push(newConversationPage(a, e.id))
		case ChooseAvatar:
			a.stack.Push(newAvatarPicker(a, e.id, ""))
		case ChooseAvatarPath:
			a.stack.Pop()
			a.stack.Push(newAvatarPicker(a, e.id, e.path))
		case RenameContact:
			a.stack.Push(newRenameContactPage(a, e.id))
		case EditContact:
			a.stack.Push(newEditContactPage(a, e.id))
		case EditContactComplete:
			a.stack.Clear(newHomePage(a))
		case MessageSent:
		}
	}
}

func (a *App) run() error {
	// teardown client at exit for any reason
	defer func() {
		if a.c != nil {
			a.c.Shutdown()
			a.c.Wait()
		}
	}()

	for {
		select {
		case e := <-a.w.Events():
			if err := a.handleGioEvents(e); err != nil {
				return err
			}
		case <-time.After(1 * time.Minute):
			// redraw the screen to update the message timestamps once per minute
			a.w.Invalidate()
		}
	}
}

func main() {
	flag.Parse()
	fmt.Println("Katzenpost is still pre-alpha.  DO NOT DEPEND ON IT FOR STRONG SECURITY OR ANONYMITY.")

	if *debug != 0 {
		go func() {
			http.ListenAndServe(fmt.Sprintf("localhost:%d", *debug), nil)
		}()
		runtime.SetMutexProfileFraction(1)
		runtime.SetBlockProfileRate(1)
	}

	// Start graphical user interface.
	uiMain()
}

func uiMain() {
	go func() {
		w := app.NewWindow(
			app.Size(unit.Dp(400), unit.Dp(400)),
			app.Title("Katzen"),
			app.NavigationColor(rgb(0x0)),
			app.StatusColor(rgb(0x0)),
		)
		if err := newApp(w).run(); err != nil {
			fmt.Fprintf(os.Stderr, "Failed: %v\n", err)
		}
		os.Exit(0)
	}()
	app.Main()
}

type (
	C = layout.Context
	D = layout.Dimensions
)

func (a *App) handleClientEvent(e interface{}) error {
	switch e := e.(type) {
	case *client.ConnectionStatusEvent:
		shortNotify("ConnectionStatusEvent", e.String())
	case *client.MessageReplyEvent:
		//shortNotify("MessageReplyEvent", e.String())
	case *client.MessageSentEvent:
		//shortNotify("MessageSentEvent", e.String())
	case *client.MessageIDGarbageCollected:
		shortNotify("MessageIDGarbageCollected", e.String())
	case *client.NewDocumentEvent:
		shortNotify("NewDocument", e.String())
	default:
		panic(e)
	}
	return nil
}

func (a *App) handleGioEvents(e interface{}) error {
	switch e := e.(type) {
	case key.FocusEvent:
		a.focus = e.Focus
	case system.DestroyEvent:
		return errors.New("system.DestroyEvent receieved")
	case system.FrameEvent:
		gtx := layout.NewContext(a.ops, e)
		key.InputOp{Tag: a.w, Keys: key.NameEscape + "|" + key.NameBack}.Add(a.ops)
		for _, e := range gtx.Events(a.w) {
			switch e := e.(type) {
			case key.Event:
				if (e.Name == key.NameEscape && e.State == key.Release) || e.Name == key.NameBack {
					if a.stack.Len() > 1 {
						a.stack.Pop()
						a.w.Invalidate()
					}
				}
			}
		}
		a.Layout(gtx)
		e.Frame(gtx.Ops)
	case system.StageEvent:
		fmt.Printf("StageEvent %s received\n", e.Stage)
		a.stage = e.Stage
		if e.Stage >= system.StageRunning {
			if a.stack.Len() == 0 {
				a.stack.Push(newSignInPage(a))
			}
		}
		if e.Stage == system.StagePaused {
			var err error
			a.endBg, err = app.Start("Is running in the background", "")
			if err != nil {
				return err
			}
		} else {
			if a.endBg != nil {
				a.endBg()
			}
		}
	}
	return nil
}
