package main

import (
	"errors"
	"flag"
	"fmt"
	"os"
	"runtime"

	"github.com/katzenpost/katzenpost/catshadow"
	"github.com/katzenpost/katzenpost/client"
	"github.com/katzenpost/katzenpost/client/config"
	"time"

	"gioui.org/app"
	"gioui.org/app/permission/foreground"
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
	dataDirName      = "catshadow"
	clientConfigFile = flag.String("f", "", "Path to the client config file.")
	stateFile        = flag.String("s", "catshadow_statefile", "Path to the client state file.")

	minPasswordLen = 5 // XXX pick something reasonable

	notifications = make(map[string]notify.Notification)

	// theme
	th = func() *material.Theme {
		th := material.NewTheme(gofont.Collection())
		th.Bg = rgb(0x0)
		th.Fg = rgb(0xFFFFFFFF)
		th.ContrastBg = rgb(0x22222222)
		th.ContrastFg = rgb(0x77777777)
		return th
	}()

	isConnected  bool
	isConnecting bool
)

type App struct {
	fg    chan struct{}
	w     *app.Window
	ops   *op.Ops
	c     *catshadow.Client
	stack pageStack
	focus bool
	stage system.Stage
}

func newApp(w *app.Window) *App {
	a := &App{
		w:   w,
		ops: &op.Ops{},
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
		case unlockError, restartClient:
			isConnected = false
			isConnecting = false
			a.stack.Clear(newSignInPage(a))
		case unlockSuccess:
			// validate the statefile somehow
			a.c = e.client
			a.c.Start()
			if _, err := a.c.GetBlob("AutoConnect"); err == nil {
				isConnecting = true
				go func() {
					a.c.Online()
					a.c.CreateRemoteSpool()
				}()
			}
			a.stack.Clear(newHomePage(a))
		case OfflineClick:
			go a.c.Offline()
			isConnected = false
		case OnlineClick:
			isConnecting = true
			go func() {
				a.c.Online()
				// does not replace an existing spool
				a.c.CreateRemoteSpool()
			}()
		case ShowSettingsClick:
			a.stack.Push(newSettingsPage(a))
		case AddContactClick:
			a.stack.Push(newAddContactPage(a))
		case AddContactComplete:
			a.stack.Pop()
		case ChooseContactClick:
			a.stack.Push(newConversationPage(a, e.nickname))
		case ChooseAvatar:
			a.stack.Push(newAvatarPicker(a, e.nickname))
		case RenameContact:
			a.stack.Push(newRenameContactPage(a, e.nickname))
		case EditContact:
			a.stack.Push(newEditContactPage(a, e.nickname))
		case EditContactComplete:
			a.stack.Clear(newHomePage(a))
		case MessageSent:
		}
	}
}

func (a *App) run() error {
	// only read window events until client is established
	for {
		if a.c != nil {
			break
		}
		e := <-a.w.Events()
		if err := a.handleGioEvents(e); err != nil {
			return err
		}
	}
	defer func() {
		if a.c != nil {
			a.c.Shutdown()
			a.c.Wait()
		}
	}()

	// select from all event sources
	for {
		select {
		case e := <-a.c.EventSink:
			if err := a.handleCatshadowEvent(e); err != nil {
				return err
			}
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

func getConfigNoTor() (*config.Config, error) {
	cfgString := `
[UpstreamProxy]
  Type = "none"

[Logging]
  Disable = false
  Level = "DEBUG"
  File = ""

[VotingAuthority]
[[VotingAuthority.Peers]]
  Addresses = ["37.218.241.202:30000"]
  IdentityPublicKey = "EmUWxb6ocBBXhxlrAKgxVd/6tyIDVK/8pIY/nZrqSDQ="
  LinkPublicKey = "Mcfs706pyzBIvEj+k5t2L9t9x+LplOR4wz3RiVrgoVU="

[[VotingAuthority.Peers]]
  Addresses = ["37.218.245.95:30000"]
  IdentityPublicKey = "vdOAeoRtWKFDw+W4k3sNN1EMT9ZsaHHmuCHOEKSg1aA="
  LinkPublicKey = "VNmU4g1hXBS7BQ1RJYMGNjNg4fIZbCimppeJ1XwrqX4="

[[VotingAuthority.Peers]]
  Addresses = ["37.218.245.228:30000"]
  IdentityPublicKey = "bFgvws69dJrc3ACKXN5aCJKLHjkN7D8DA2HDKkhSNIk="
  LinkPublicKey = "p1JekMh8uCPDsRSP5Uc59DJvEGMmA/B0mcMCXx1WEkk="

[Debug]
  DisableDecoyTraffic = false
 `
	return config.Load([]byte(cfgString))
}

func getDefaultConfig() (*config.Config, error) {
	cfgString := `
[UpstreamProxy]
  Type = "socks5"
  Network = "tcp"
  Address = "127.0.0.1:9050"

[Logging]
  Disable = false
  Level = "DEBUG"
  File = ""

[VotingAuthority]
[[VotingAuthority.Peers]]
  Addresses = ["n5axysudjvjjkpy4r7hur7qfgybfaiwrfz2mqwkvnyylqxinldtao2ad.onion:30000"]
  IdentityPublicKey = "EmUWxb6ocBBXhxlrAKgxVd/6tyIDVK/8pIY/nZrqSDQ="
  LinkPublicKey = "Mcfs706pyzBIvEj+k5t2L9t9x+LplOR4wz3RiVrgoVU="

[[VotingAuthority.Peers]]
  Addresses = ["mj5ouhyjvokgvbcp56lh56plxvzh4wcrq3fadpqf6ewdqmuy7pr3n6qd.onion:30000"]
  IdentityPublicKey = "vdOAeoRtWKFDw+W4k3sNN1EMT9ZsaHHmuCHOEKSg1aA="
  LinkPublicKey = "VNmU4g1hXBS7BQ1RJYMGNjNg4fIZbCimppeJ1XwrqX4="

[[VotingAuthority.Peers]]
  Addresses = ["pz6obnsyh7vmpmtmrsam443jh4gkei77q3y66ty3fd6h6wjdvcmu6pid.onion:30000"]
  IdentityPublicKey = "bFgvws69dJrc3ACKXN5aCJKLHjkN7D8DA2HDKkhSNIk="
  LinkPublicKey = "p1JekMh8uCPDsRSP5Uc59DJvEGMmA/B0mcMCXx1WEkk="

[Debug]
  DisableDecoyTraffic = false
  PollingInterval = 500
  PreferedTransports = ["onion"]
`
	return config.Load([]byte(cfgString))
}

func (a *App) handleCatshadowEvent(e interface{}) error {
	switch event := e.(type) {
	case *client.ConnectionStatusEvent:
		isConnecting = false
		if event.IsConnected {
			isConnected = true
			go func() {
				if n, err := notify.Push("Connected", "Katzen has connected"); err == nil {
					<-time.After(notificationTimeout)
					n.Cancel()
				}
			}()
		} else {
			isConnected = false
			go func() {
				if n, err := notify.Push("Disconnected", "Katzen has disconnected"); err == nil {
					<-time.After(notificationTimeout)
					n.Cancel()
				}
			}()
		}
		if event.Err != nil {
			go func() {
				if n, err := notify.Push("Error", fmt.Sprintf("Katzen error: %s", event.Err)); err == nil {
					<-time.After(notificationTimeout)
					n.Cancel()
				}
			}()
		}
	case *catshadow.KeyExchangeCompletedEvent:
		if event.Err != nil {
			if n, err := notify.Push("Key Exchange", fmt.Sprintf("Failed: %s", event.Err)); err == nil {
				go func() { <-time.After(notificationTimeout); n.Cancel() }()
			}
		} else {
			if n, err := notify.Push("Key Exchange", fmt.Sprintf("Completed: %s", event.Nickname)); err == nil {
				go func() { <-time.After(notificationTimeout); n.Cancel() }()
			}
		}
	case *catshadow.MessageNotSentEvent:
		if n, err := notify.Push("Message Not Sent", fmt.Sprintf("Failed to send message to %s", event.Nickname)); err == nil {
			go func() { <-time.After(notificationTimeout); n.Cancel() }()
		}
	case *catshadow.MessageReceivedEvent:
		// do not notify for the focused conversation
		p := a.stack.Current()
		switch p := p.(type) {
		case *conversationPage:
			// XXX: on android, input focus is not lost when the application does not have foreground
			// but system.Stage is changed. On desktop linux, the stage does not change, but window focus is lost.
			if p.nickname == event.Nickname && a.stage == system.StageRunning && a.focus {
				a.w.Invalidate()
				return nil
			}
		}
		// emit a notification in all other cases
		if n, err := notify.Push("Message Received", fmt.Sprintf("Message Received from %s", event.Nickname)); err == nil {
			if o, ok := notifications[event.Nickname]; ok {
				// cancel old notification before replacing with a new one
				o.Cancel()
			}
			notifications[event.Nickname] = n
		}
	case *catshadow.MessageSentEvent:
	case *catshadow.MessageDeliveredEvent:
	default:
		// do not invalidate window for events we do not care about
		return nil
	}
	// redraw the screen when an event we care about is received
	a.w.Invalidate()
	return nil
}

func (a *App) handleGioEvents(e interface{}) error {
	switch e := e.(type) {
	case key.Event:
		switch e.Name {
		case key.NameEscape:
			if a.stack.Len() > 1 {
				a.stack.Pop()
				a.w.Invalidate()
			}
		}
	case key.FocusEvent:
		a.focus = e.Focus
	case *system.CommandEvent:
		switch e.Type {
		case system.CommandBack:
			if a.stack.Len() > 1 {
				a.stack.Pop()
				e.Cancel = true
				a.w.Invalidate()
			}
		}
	case system.DestroyEvent:
		return errors.New("system.DestroyEvent receieved")
	case system.FrameEvent:
		gtx := layout.NewContext(a.ops, e)
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
			foreground.Start("Is running in the background", "")
		} else {
			foreground.Stop()
		}
	}
	return nil
}

func init() {
	go func() {
		http.ListenAndServe("localhost:8080", nil)
	}()
	runtime.SetMutexProfileFraction(1)
	runtime.SetBlockProfileRate(1)
}
