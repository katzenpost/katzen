package main

import (
	"errors"
	"fmt"
	"github.com/katzenpost/hpqc/rand"
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
	for _, id := range a.db.GetContactIDs() {
		c, err := a.db.GetContact(id)
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

	c, err := a.db.GetContact(id)
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
	// minimum blob size to exchange necdh.PublicKey and eddsa.Pub
	blobSize := 24 /* nonce */ + 4 /* length */ + signScheme.PublicKeySize() + nikeScheme.PublicKeySize() + secretbox.Overhead

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

	ex, err := NewReadCapExchange()
	if err != nil {
		return err
	}

	kxBytes, err := ex.ExchangeBytes()
	if err != nil {
		return err
	}

	if c.PandaKeyExchange != nil {
		kx, err = panda.UnmarshalKeyExchange(rand.Reader, l, meetingPlace, c.PandaKeyExchange, id, pandaChan, s.HaltCh())
		if err != nil {
			return err
		}
		kx.SetSharedRandom(sharedRandom)
	} else {
		kx, err = panda.NewKeyExchange(rand.Reader, l, meetingPlace, sharedRandom, c.SharedSecret, kxBytes, id, pandaChan, s.HaltCh())
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
	c, err := a.db.GetContact(update.ID)
	if err != nil {
		return false, ErrContactNotFound
	}
	defer a.db.PutContact(c) // save updated contact

	l := a.c.GetLogger("pandaUpdate " + c.Nickname)

	// hold lock over contact
	switch {
	case update.Err != nil:
		c.PandaResult = update.Err.Error()
		l.Infof("PANDA with %s failed: %s", c.Nickname, update.Err)
	case update.Serialised != nil:
		c.PandaKeyExchange = update.Serialised
	case update.Result != nil:
		l.Infof("PANDA with %s successfully", c.Nickname)
		c.PandaKeyExchange = nil
		c.IsPending = false

		msg := &ReadCapExchangeMessage{}
		err = msg.UnmarshalBinary(update.Result)
		if err != nil {
			err = fmt.Errorf("failed to parse exchange bytes: %s", err)
			l.Error(err.Error())
			c.PandaResult = err.Error()
			return false, err
		}
		ctx, ownerCap, readCap, err := c.ReadCapExchange.CompleteExchange(msg)
		if err != nil {
			err = fmt.Errorf("failed to complete exchange: %s", err)
			l.Error(err.Error())
			c.PandaResult = err.Error()
			return false, err
		}

		// save these initial capabilities
		c.ReadCap = readCap
		c.WriteCap = ownerCap

		// create a stream to exchange messages with this contact
		st := stream.NewStream(ownerCap, readCap, ctx)
		transport := &stream.BufferedStream{Stream: st}
		err = a.db.PutStream(c.ID, transport)
		if err != nil {
			panic(err)
		}

		l.Info("Stream (%x) initialized between [%v] [%v] (%s)", ctx, ownerCapStr(c.WriteCap), readCapStr(c.ReadCap), c.Nickname)
		// c.SharedSecret = nil // XXX: zero original shared secret after exchange ???
		shortNotify("PANDA Completed", "Contact "+c.Nickname)

		// by default, of course we want to start chatting, right?
		err = a.startTransport(a.Session(), c.ID)
		if err != nil {
			return true, err
		}
		return true, nil
	}
	return false, nil
}
