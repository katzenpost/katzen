package main

import (
	_ "embed"
	"errors"
	"flag"
	"fmt"
	"os"
	"runtime"
	"sync"

	"github.com/katzenpost/katzenpost/client"
	"github.com/katzenpost/katzenpost/core/crypto/rand"
	"github.com/katzenpost/katzenpost/core/worker"
	"github.com/katzenpost/katzenpost/stream"
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
	initialPKIConsensusTimeout = 45 * time.Second
	notificationTimeout        = 30 * time.Second
)

var (
	dataDirName      = "katzen"
	clientConfigFile = flag.String("f", "", "Path to the client config file.")
	stateFile        = flag.String("s", "statefile", "Path to the client state file.")
	debug            = flag.Bool("d", false, "Enable golang debug service.")

	minPasswordLen = 5 // XXX pick something reasonable

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

// Store is the interface for reading and writing saved state
type Store interface {
	Get([]byte) ([]byte, error)
	Put([]byte, []byte) error
}

type App struct {
	sync.Mutex
	worker.Worker

	endBg func()
	w     *app.Window
	ops   *op.Ops
	c     *client.Client

	cancelConn func()
	state      ConnectedState

	Contacts      map[uint64]*Contact
	Conversations map[uint64]*Conversation
	Settings      map[string]interface{}

	stack pageStack
	focus bool
	stage system.Stage

	connectOnce *sync.Once

	db Store
}

func (a *App) eventSinkWorker(s *client.Session) {
	for {
		select {
		case <-s.HaltCh(): // session is halted
			return
		case <-a.HaltCh(): // client is halted
			return
		case e := <-s.EventSink:
			err := a.handleClientEvent(e)
			if err != nil {
				return
			}
		}
	}
}

// Reunion holds the informtaion for a Reunion protocol
type Reunion struct {
	// TODO other parts of state we must serialize / deserialize
	Secret []byte
	Epoch  []byte
}

// TODO :Methods on Reunion that we want

// MessageType indicates what type of Body is encoded by Message
type MessageType uint8

const (
	Text MessageType = iota
	Audio
	Image
	Attachment
)

// Message holds information
type Message struct {
	sync.Mutex

	// Type is the type of Message
	Type MessageType

	// Conversation tag
	Conversation uint64

	// ID of the Message
	ID uint64

	// sender Contact.ID for this client
	Sender uint64 `cbor:"-"`

	// Sent is the sender timestamp
	Sent time.Time

	// Received is the reciver timestamp
	Received time.Time `cbor:"-"`

	// Acked is when the receiver acknowledged this message
	Acked time.Time

	// Body is the message body
	Body []byte
}

// Conversation holds a multiparty conversation
type Conversation struct {
	sync.Mutex

	// Title is the string set to dispaly at header of conversation
	Title string

	// ID is the group identifier for this conversation to tag messages to/from
	ID uint64

	// Contacts are the contacts present in this conversation
	Contacts []*Contact

	// Messages are the messages in this conversation
	Messages []*Message

	// MessageExpiration is the duration after which conversation history is cleared
	MessageExpiration time.Duration
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

func (c *Conversation) Send(msg *Message) error {
	c.Lock()
	for _, c := range c.Contacts {
		c.Send(msg)
	}
	panic("NotImplemented")
	return nil
}

// Methods on Conversation that we want
// Remove(contactID uint64)
// Add(contactID uint64)
// Destroy() // purge *Message sent to this Conversation

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

func (a *App) NewConversation(id uint64) (*Conversation, error) {
	contact, ok := a.Contacts[id]
	if !ok {
		return nil, errors.New("No such contact")
	}
	for {
		id = uint64(rand.NewMath().Int63())
		if _, ok := a.Conversations[id]; ok {
			continue
		}
	}

	conv := &Conversation{ID: id, Title: "Chat with" + contact.Nickname, Contacts: []*Contact{contact}}
	return conv, nil
}

// Send a Message to Contact
func (c *Contact) Send(msg *Message) error {
	panic("NotImplemented")
	return nil
}

func newApp(w *app.Window) *App {
	a := &App{
		Contacts:      make(map[uint64]*Contact),
		Conversations: make(map[uint64]*Conversation),
		Settings:      make(map[string]interface{}),
		w:             w,
		ops:           &op.Ops{},
		connectOnce:   new(sync.Once),
	}
	// XXX we dont serialize anything to disk yet
	if hasTor() {
		a.Settings["UseTor"] = true
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
			a.stack.Clear(newSignInPage(a))
		case unlockSuccess:
			// validate the statefile somehow
			c := e.client
			a.c = c
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

	if *debug {
		go func() {
			http.ListenAndServe("localhost:8080", nil)
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
	case client.ConnectionStatusEvent:
	case client.MessageReplyEvent:
	case client.MessageSentEvent:
	case client.MessageIDGarbageCollected:
	case client.NewDocumentEvent:
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
