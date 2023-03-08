package main

import (
	"bytes"
	"errors"
	"fmt"
	"github.com/katzenpost/katzenpost/core/crypto/ecdh"
	necdh "github.com/katzenpost/katzenpost/core/crypto/nike/ecdh"
	"github.com/katzenpost/katzenpost/core/crypto/rand"
	pclient "github.com/katzenpost/katzenpost/panda/client"
	pCommon "github.com/katzenpost/katzenpost/panda/common"
	panda "github.com/katzenpost/katzenpost/panda/crypto"
	"github.com/katzenpost/katzenpost/stream"
	"golang.org/x/crypto/nacl/secretbox"
)

var (
	ErrNotOnline  = errors.New("Not Online")
	ErrNoDocument = errors.New("No PKI Document")
)

func (a *App) restartPandaExchanges() {
	l := a.c.GetLogger("restartPandaExchanges")
	for _, id := range a.GetContactIDs() {
		c, err := a.GetContact(id)
		if !c.IsPending {
			continue
		}
		err = a.doPANDAExchange(id)
		if err != nil {
			l.Debug("error restarting exchange for %s: %s", c.Nickname, err.Error())
		}
	}
}

func (a *App) doPANDAExchange(id uint64) error {
	s := a.Session()
	if s == nil {
		return ErrNotOnline
	}

	c, err := a.GetContact(id)
	if err != nil {
		return ErrContactNotFound
	}

	l := a.c.GetLogger("panda " + c.Nickname)

	// Use PANDA
	p, err := s.GetService(pCommon.PandaCapability)
	if err != nil {
		l.Errorf("Failed to get %s: %s", pCommon.PandaCapability, err)
		return err
	}
	// minimum blob size to exchange a ecdh.PublicKey
	blobSize := 24 /* nonce */ + 4 /* length */ + ecdh.PublicKeySize + secretbox.Overhead

	meetingPlace := pclient.New(blobSize, s, l, p.Name, p.Provider)
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
	ecdhNike := necdh.NewEcdhNike(rand.Reader)
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
	if a.Session() == nil {
		return
	}
	// teardown at session close
	haltOn := a.Session().HaltCh()

	l := a.c.GetLogger("pandaWorker")
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
	c, err := a.GetContact(update.ID)
	if err != nil {
		a.Unlock()
		return false, ErrContactNotFound
	}
	a.Unlock()

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
		ecdhNike := necdh.NewEcdhNike(rand.Reader)
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

			// XXX: need to create and store the transport in badger
			st, err := stream.ListenDuplex(a.Session(), "", string(streamSecret))
			if err != nil {
				panic(err)
			}
			// XXX: need to create and store the transport in badger
			a.transports[c.ID] = &stream.BufferedStream{Stream: st}
		} else {
			l.Notice("Dialing with %x", streamSecret)
			st, err := stream.DialDuplex(a.Session(), "", string(streamSecret))
			if err != nil {
				panic(err)
			}
			a.transports[c.ID] = &stream.BufferedStream{Stream: st}
		}

		c.IsPending = false
		l.Info("Stream initialized with " + c.Nickname)
		// c.SharedSecret = nil // XXX: zero original shared secret after exchange ???
		shortNotify("PANDA Completed", "Contact "+c.Nickname)

		// by default, of course we want to start chatting, right?
		err = a.startTransport(c.ID)
		if err != nil {
			return true, err
		}
		return true, nil
	}
	return false, nil
}
