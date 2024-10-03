package main

import (
	_ "embed"
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
	"github.com/katzenpost/katzenpost/stream"
	"time"

	"gioui.org/app"
	_ "gioui.org/app/permission/foreground"
	_ "gioui.org/app/permission/storage"
	"gioui.org/font/gofont"
	"gioui.org/io/event"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/text"
	"gioui.org/unit"
	"gioui.org/widget/material"
	"net/http"
	_ "net/http/pprof"
)

const (
	initialPKIConsensusTimeout = 120 * time.Second
	notificationTimeout        = 30 * time.Second
	keySize                    = 32 // symmetric key size for badger db encryption (AES-256)
)

var (
	dataDirName      = "katzen"
	clientConfigFile = flag.String("f", "", "Path to the client config file.")
	stateFile        = flag.String("s", "catshadow_statefile", "Path to the client state file.")
	debug            = flag.Int("d", 0, "Enable golang debug service.")

	th *material.Theme

	// obtain the default data location
	dir, _ = app.DataDir()

	// path to default profile
	dataDir = filepath.Join(dir, dataDirName, "default")

	profilePath = flag.String("p", dataDir, "Path to application profile")

	minPasswordLen = 5               // XXX pick something reasonable
	updateInterval = 1 * time.Second // try to read from contacts every updateInterval

	//go:embed default_config_without_tor.toml
	cfgWithoutTor []byte
	//go:embed default_config_with_tor.toml
	cfgWithTor []byte

	isConnected  bool
	isConnecting bool
)

type App struct {
	sync.Mutex
	worker.Worker

	w   *app.Window
	ops *op.Ops
	c   *client.Client

	db *badger.DB

	cancelConn func()
	state      ConnectedState
	sessions   map[uint64]*client.Session

	transports   map[uint64]*stream.BufferedStream
	messageChans map[uint64]chan *Message

	stack pageStack
	focus bool

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
	_, err := a.GetContact(id)
	if err != nil {
		return ErrContactNotFound
	}
	a.Lock()
	if _, ok := a.messageChans[id]; ok {
		a.Unlock()
		return ErrAlreadyReading
	}
	a.Unlock()
	m := a.Messages(id, a.HaltCh())
	a.Lock()
	a.messageChans[id] = m
	a.Unlock()
	return nil
}

func (a *App) getTransport(id uint64) (*stream.BufferedStream, error) {
	_, err := a.GetContact(id)
	if err != nil {
		return nil, ErrContactNotFound
	}
	a.Lock()
	transport, ok := a.transports[id]
	a.Unlock()
	if !ok {
		return nil, ErrNotReading
	}
	return transport, nil
}

func (a *App) stopTransport(id uint64) error {
	_, err := a.GetContact(id)
	if err != nil {
		return ErrContactNotFound
	}
	a.Lock()
	defer a.Unlock()
	transport, ok := a.transports[id]
	if !ok {
		return ErrNotReading
	}
	transport.Halt()
	delete(a.transports, id)
	return nil
}

func (a *App) haltAllTransports() {
	a.Lock()
	defer a.Unlock()
	for _, transport := range a.transports {
		transport.Halt()
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
			// XXX: get outbound queue associated with contact
			_, err := a.GetContact(id)
			if err != nil {
				a.stopTransport(id)
				delete(a.messageChans, id)
				continue
			}

			// XXX: figure out how to manipulate transport inside
			// of a badger transaction
			// ideally we'd save the state of transport as well as
			bq := NewBadgerQueue(a.db, []byte(fmt.Sprintf("contact:%d:queue", id)))
			msg, err := bq.Peek()
			if err == nil {
				transport, err := a.getTransport(id)
				if err == nil {
					enc := cbor.NewEncoder(transport)
					err := enc.Encode(msg)
					if err == nil {
						// XXX: wait for the writeBuf to flush
						// then remove from Outbound Queue
						// wait for the transport to flush?
						transport.Stream.Sync()
						_, err = bq.Pop()
					}
				}
			}

			select {
			case m, ok := <-msgCh:
				if !ok {
					a.stopTransport(id)
				}
				// apply our ID to the Message
				m.Sender = id
				a.DeliverMessage(m)
			default:
				// skip
			}
		}

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
		sessions:     make(map[uint64]*client.Session),
		messageChans: make(map[uint64]chan *Message),
		w:            w,
		ops:          &op.Ops{},
		transports:   make(map[uint64]*stream.BufferedStream),
		connectOnce:  new(sync.Once),
	}
	return a
}

func (a *App) Layout(gtx layout.Context) {
	a.update(gtx)
	a.stack.Current().Layout(gtx)
}

func (a *App) update(gtx layout.Context) {
	// handle global shortcuts
	if backEvent(gtx) {
		// XXX: this means that after signin, the top level page is homescreen
		// and therefore pressing back won't logout
		if a.stack.Len() > 1 {
			a.stack.Pop()
			return
		}
	}

	if a.stack.Len() == 0 {
		a.stack.Push(newSignInPage(a))
		return
	}

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
		case EditConversation:
			//a.stack.Push(newEditConversationPage(a, e.ID))
		case EditConversationComplete:
			a.stack.Pop()
		case MessageSent:
		}
	}
}

func (a *App) run() error {
	// on Android, this will start a foreground service, and does nothing on other platforms
	cancelForeground, err := app.Start("Background Connection", "")
	if err != nil {
		return err
	}
	defer cancelForeground()

	evCh := make(chan event.Event)
	ackCh := make(chan struct{})
	go func() {
		for {
			ev := a.w.Event()
			evCh <- ev
			<-ackCh
			if _, ok := ev.(app.DestroyEvent); ok {
				return
			}
		}
	}()

	// select from all event sources
	for {
		select {
		case e := <-evCh:
			if err := a.handleGioEvents(e); err != nil {
				ackCh <- struct{}{}
				return err
			}
			ackCh <- struct{}{}
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
		w := new(app.Window)
		w.Option(app.Size(unit.Dp(400), unit.Dp(400)),
			app.Title("Katzen"),
			app.NavigationColor(rgb(0x0)),
			app.StatusColor(rgb(0x0)),
		)

		// theme must be declared AFTER NewWindow on android
		th = func() *material.Theme {
			th := material.NewTheme()
			th.Shaper = text.NewShaper(text.WithCollection(gofont.Collection()))
			th.Bg = rgb(0x0)
			th.Fg = rgb(0xFFFFFFFF)
			th.ContrastBg = rgb(0x22222222)
			th.ContrastFg = rgb(0x77777777)
			return th
		}()

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
