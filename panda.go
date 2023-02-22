package main

import (
	"bytes"
	"errors"
	"fmt"
	"github.com/katzenpost/katzenpost/core/crypto/nike/ecdh"
	"github.com/katzenpost/katzenpost/core/crypto/rand"
	pclient "github.com/katzenpost/katzenpost/panda/client"
	pCommon "github.com/katzenpost/katzenpost/panda/common"
	panda "github.com/katzenpost/katzenpost/panda/crypto"
	"github.com/katzenpost/katzenpost/stream"
)

var (
	ErrNotOnline  = errors.New("Not Online")
	ErrNoDocument = errors.New("No PKI Document")
)

func (a *App) doPANDAExchange(id uint64) error {
	a.Lock()
	defer a.Unlock()

	c, ok := a.Contacts[id]
	if !ok {
		return ErrContactNotFound
	}

	s := a.c.Session()
	if s == nil {
		return ErrNotOnline
	}

	l := a.c.GetLogger("panda " + c.Nickname)

	// Use PANDA
	p, err := s.GetService(pCommon.PandaCapability)
	if err != nil {
		l.Errorf("Failed to get %s: %s", pCommon.PandaCapability, err)
		return err
	}

	pandaPayloadSize:= 1000 // XXX find out the actual payload size...
	meetingPlace := pclient.New(wtf, s, l, p.Name, p.Provider)
	// get the current document and shared random
	doc := s.CurrentDocument()

	// if no documnent for some reason
	if doc == nil {
		return ErrNoDocument
	}
	sharedRandom := doc.PriorSharedRandom[0]

	var kx *panda.KeyExchange
	pandaChan := make(chan panda.PandaUpdate)

	// our ecdh public key
	ecdhNike := ecdh.NewEcdhNike(rand.Reader)
	myPublic := ecdhNike.DerivePublicKey(c.MyIdentity)
	if c.PandaKeyExchange != nil {
		kx, err = panda.UnmarshalKeyExchange(rand.Reader, l, meetingPlace, c.PandaKeyExchange, id, pandaChan, s.HaltCh())
		if err != nil {
			return err
		}
		kx.SetSharedRandom(sharedRandom)
	} else {
		kx, err = panda.NewKeyExchange(rand.Reader, l, meetingPlace, sharedRandom, c.SharedSecret, myPublic.Bytes(), id, pandaChan, s.HaltCh())
		if err != nil {
			return err
		}
	}
	c.PandaKeyExchange = kx.Marshal()
	a.Go(kx.Run)
	a.Go(func() {
		a.pandaWorker(pandaChan)
	})
	return nil
}

func (a *App) pandaWorker(pandaChan chan panda.PandaUpdate) {
	a.Lock()
	if a.c == nil || a.c.Session() == nil {
		a.Unlock()
		return
	}

	// teardown at session close
	haltOn := a.c.Session().HaltCh()

	l := a.c.GetLogger("pandaWorker")
	a.Unlock()

	for {
		select {
		case <-haltOn:
			l.Debug("ending with Session.HaltCh")
			return
		case update, ok := <-pandaChan:
			if !ok {
				// channel was closed
				l.Debug("pandaChan closed")
				return
			}
			l.Debug("got Update")
			done, err := a.processPANDAUpdate(update)
			if err != nil {
				l.Infof("halting on err %s", err.Error())
				return
			}
			if done == true {
				l.Infof("halting on successful kx")
				return
			}
		}
	}
}

func (a *App) processPANDAUpdate(update panda.PandaUpdate) (bool, error) {
	a.Lock()
	defer a.Unlock()
	c, ok := a.Contacts[update.ID]
	if !ok {
		return false, ErrContactNotFound
	}

	l := a.c.GetLogger("pandaUpdate " + c.Nickname)

	// hold lock over contact
	c.Lock()
	defer c.Unlock()

	switch {
	case update.Err != nil:
		c.PandaResult = update.Err.Error()
		l.Infof("PANDA with %s failed: %s", c.Nickname, update.Err)
	case update.Serialised != nil:
		c.PandaKeyExchange = update.Serialised
	case update.Result != nil:
		l.Infof("PANDA with %s successfully", c.Nickname)
		c.PandaKeyExchange = nil

		// get the exchanged keys and figure out who goes first
		ecdhNike := ecdh.NewEcdhNike(rand.Reader)
		theirPublic, err := ecdhNike.UnmarshalBinaryPublicKey(update.Result)
		if err != nil {
			err = fmt.Errorf("failed to parse contact public key bytes: %s", err)
			l.Error(err.Error())
			c.PandaResult = err.Error()
			c.IsPending = false
			return false, err
		}

		// get our nike (ecdh) public key for this contact
		myPublic := ecdhNike.DerivePublicKey(c.MyIdentity)
		streamSecret := ecdhNike.DeriveSecret(c.MyIdentity, theirPublic)
		if bytes.Compare(myPublic.Bytes(), theirPublic.Bytes()) == 1 {
			// we go first, so we are the "listener"
			l.Notice("Listening with %x", streamSecret)
			st, err := stream.ListenDuplex(a.c.Session(), "", string(streamSecret))
			if err != nil {
				panic(err)
			}
			c.Stream = st
		} else {
			l.Notice("Dialing with %x", streamSecret)
			st, err := stream.DialDuplex(a.c.Session(), "", string(streamSecret))
			if err != nil {
				panic(err)
			}
			c.Stream = st
		}

		c.IsPending = false
		l.Info("Stream initialized with " + c.Nickname)
		// c.SharedSecret = nil // XXX: zero original shared secret after exchange ???
		shortNotify("PANDA Completed", "Contact "+c.Nickname)
		return true, nil
	}
	return false, nil
}
