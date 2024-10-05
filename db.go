// db.go
package main

import (
	"bytes"
	"crypto/sha256"
	_ "embed"
	"encoding/binary"
	"errors"
	"fmt"
	"image"
	_ "image/png"
	"time"

	"github.com/dgraph-io/badger/v4"
	"github.com/fxamacker/cbor/v2"
	"github.com/katzenpost/hpqc/nike/schemes"
	"github.com/katzenpost/hpqc/rand"
	"golang.org/x/crypto/hkdf"
)

var (
	ErrConversationAlreadyExists = errors.New("Conversation already exists")
	DBVersion                    = []byte("0.0.0")
	necdh                        = schemes.ByName("x25519")
)

func (a *App) InitDB() error {
	return a.db.Update(func(txn *badger.Txn) error {
		_, err := txn.Get(versionKey())
		if err == nil {
			// TODO: here goes functions for updates
		} else {
			// initialize keys
			contactsIdx, _ := cbor.Marshal(make(map[uint64]struct{}))
			conversationIdx, _ := cbor.Marshal(make(map[uint64]struct{}))

			err = txn.Set(contactsKey(), contactsIdx)
			if err != nil {
				return err
			}
			err = txn.Set(conversationsKey(), conversationIdx)
			if err != nil {
				return err
			}
			err = txn.Set(versionKey(), DBVersion)
			if err != nil {
				return err
			}
		}
		return nil
	})
}

func versionKey() []byte {
	return []byte("katzen_version")
}

func contactsKey() []byte {
	return []byte("contacts")
}

func avatarKey(id uint64) []byte {
	return []byte(fmt.Sprintf("avatar:%d", id))
}

func contactKey(id uint64) []byte {
	return []byte(fmt.Sprintf("contact:%d", id))
}

func conversationsKey() []byte {
	return []byte("conversations")
}

func conversationKey(id uint64) []byte {
	return []byte(fmt.Sprintf("conversation:%d", id))
}

func messageKey(id uint64) []byte {
	return []byte(fmt.Sprintf("message:%d", id))
}

func outboundKey(id uint64) []byte {
	return []byte(fmt.Sprintf("outbound:%d", id))
}

// RemoveContact removes a contact from the db
func (a *App) RemoveContact(contactID uint64) error {
	panic("NotImplemented")
	return nil
}

// NewContact creates a new Contact from a shared secret (dialer)
func (a *App) NewContact(nickname string, secret []byte) (*Contact, error) {
	sK := necdh.GeneratePrivateKey(rand.Reader)
	emptyPk := necdh.NewEmptyPublicKey()
	contactID := rand.NewMath().Uint64()
	contact := &Contact{ID: contactID, Nickname: nickname, Identity: emptyPk, MyIdentity: sK, SharedSecret: secret, IsPending: true, Outbound: rand.NewMath().Uint64()}
	err := a.PutContact(contact)
	if err != nil {
		return nil, err
	}

	// if we are online, start a PANDA exchange immediately
	// if not, the exchange must be started when the client comes online and tries to send a message.
	if a.Status() == StateOnline {
		// start a panda' exchange with contact
		err := a.doPANDAExchange(contact.ID)
		if err != nil {
			panic(err)
			return nil, err
		}
	}
	return contact, nil
}

// NewConversation creates a Conversation with a Contact
func (a *App) NewConversation(contactID uint64) error {
	return a.db.Update(func(txn *badger.Txn) error {
		// Create a Contact to deserialize into
		contact := new(Contact)
		contact.MyIdentity = necdh.NewEmptyPrivateKey()
		contact.Identity = necdh.NewEmptyPublicKey()
		// verify that the contact exists, and retrieve it
		i, err := txn.Get(contactKey(contactID))
		if err != nil {
			return ErrContactNotFound
		}
		err = i.Value(func(val []byte) error {
			return cbor.Unmarshal(val, contact)
		})
		if err != nil {
			return err
		}

		// In order that contacts will tag their conversation with the same ID,
		// we derive the conversation ID from the SharedSecret between contacts
		r := hkdf.New(sha256.New, []byte(contact.SharedSecret), []byte("our first rendezvous"), nil)
		tmp := [8]byte{}
		_, err = r.Read(tmp[:])
		if err != nil {
			return err
		}
		conversationID := binary.LittleEndian.Uint64(tmp[:])

		// Make sure the Conversation doens't already exist with this contact
		i, err = txn.Get(conversationKey(conversationID))
		if err != badger.ErrKeyNotFound {
			return ErrConversationAlreadyExists
		}

		// Create the conversation and store it in the db
		conversation := &Conversation{ID: conversationID, Title: contact.Nickname, Contacts: []uint64{contact.ID}}
		serialized, err := cbor.Marshal(conversation)
		if err != nil {
			return err
		}
		err = txn.Set(conversationKey(conversationID), serialized)
		if err != nil {
			return err
		}

		conversationIDs := make(map[uint64]struct{})
		// update the list of conversations
		i, err = txn.Get(conversationsKey())
		if err == nil {
			err = i.Value(func(val []byte) error {
				return cbor.Unmarshal(val, &conversationIDs)
			})
			if err != nil {
				return err
			}
		}
		conversationIDs[conversationID] = struct{}{}
		b, err := cbor.Marshal(conversationIDs)
		if err != nil {
			return err
		}
		err = txn.Set(conversationsKey(), b)
		if err != nil {
		}
		return err
	})
}

// DeliverMessage adds a Message to the Conversation
func (a *App) DeliverMessage(msg *Message) error {
	msg.Received = time.Now()
	err := a.PutMessage(msg)
	if err != nil {
		return err
	}
	return a.db.Update(func(txn *badger.Txn) error {
		i, err := txn.Get(conversationKey(msg.Conversation))
		if err != nil {
			return ErrConversationNotFound
		}

		return i.Value(func(val []byte) error {
			co := new(Conversation)
			err = cbor.Unmarshal(val, co)
			if err != nil {
				return err
			}
			// add Message to Conversation
			// XXX: store the message and add the ordered ID to conversation
			// XXX: this should be the message ID
			co.Messages = append(co.Messages, msg.ID)

			// save Conversation in badger
			serialized, err := cbor.Marshal(co)
			if err != nil {
				return err
			}
			return txn.Set(conversationKey(msg.Conversation), serialized)
		})
	})
}

// SendMessage sends a Message to each Contact in a Conversation
func (a *App) SendMessage(conversation uint64, msg *Message) error {
	return a.db.Update(func(txn *badger.Txn) error {
		// store Message
		serialized, err := cbor.Marshal(msg)
		if err != nil {
			return err
		}
		err = txn.Set(messageKey(msg.ID), serialized)
		if err != nil {
			return err
		}

		// Get the Conversation
		i, err := txn.Get(conversationKey(conversation))
		if err != nil {
			return ErrConversationNotFound
		}

		return i.Value(func(val []byte) error {
			co := new(Conversation)
			err = cbor.Unmarshal(val, co)
			if err != nil {
				return err
			}
			// add MessageID to Conversation
			co.Messages = append(co.Messages, msg.ID)

			// save Conversation in badger
			serialized, err := cbor.Marshal(co)
			if err != nil {
				return err
			}
			err = txn.Set(conversationKey(conversation), serialized)
			if err != nil {
				return err
			}

			// Enqueue message to each contact in conversation
			for _, c := range co.Contacts {
				q := NewBadgerQueue(a.db, outboundKey(c))
				err := q.Push(msg)
				if err != nil {
					return err
				}
			}
			return nil
		})
	})
}

// GetContactIDs returns a slice of all Contact IDs
func (a *App) GetContactIDs() []uint64 {
	contacts := make(map[uint64]struct{})
	a.db.View(func(txn *badger.Txn) error {
		i, err := txn.Get(contactsKey())
		if err != nil {
			return err
		}
		return i.Value(func(val []byte) error {
			return cbor.Unmarshal(val, &contacts)
		})
	})
	ids := make([]uint64, 0, len(contacts))
	for k, _ := range contacts {
		ids = append(ids, k)
	}
	return ids
}

// GetContact retrieves a Contact from badger
func (a *App) GetContact(contactID uint64) (*Contact, error) {
	contact := new(Contact)
	// initialize concrete types to deserialize into
	contact.MyIdentity = necdh.NewEmptyPrivateKey()
	contact.Identity = necdh.NewEmptyPublicKey()
	err := a.db.View(func(txn *badger.Txn) error {
		i, err := txn.Get(contactKey(contactID))
		if err != nil {
			return err
		}
		return i.Value(func(val []byte) error {
			return cbor.Unmarshal(val, contact)
		})
	})
	if err != nil {
		return nil, err
	}
	return contact, nil
}

// GetAvatar retrieves the Contact Avatar image.Image
func (a *App) GetAvatar(contactID uint64, sz image.Point) (image.Image, error) {
	var img image.Image
	err := a.db.View(func(txn *badger.Txn) error {
		i, err := txn.Get(avatarKey(contactID))
		if err != nil {
			return err
		}
		return i.Value(func(val []byte) error {
			m, _, err := image.Decode(bytes.NewReader(val))
			if err != nil {
				return err
			}
			//avatarSz := image.Rect(0, 0, sz.X, sz.Y)
			img = m //scale(m, avatarSz, draw.ApproxBiLinear)
			return nil
		})
	})
	if err != nil {
		return nil, err
	}
	return img, nil
}

// PutContact stores a Contact in badger
func (a *App) PutContact(contact *Contact) error {
	return a.db.Update(func(txn *badger.Txn) error {
		b, err := cbor.Marshal(contact)
		if err != nil {
			return err
		}
		err = txn.Set(contactKey(contact.ID), b)
		if err != nil {
			return err
		}
		i, err := txn.Get(contactsKey())
		return i.Value(func(val []byte) error {

			contactsIdx := make(map[uint64]struct{})
			err := cbor.Unmarshal(val, &contactsIdx)
			if err != nil {
				return err
			}
			contactsIdx[contact.ID] = struct{}{}
			serialized, err := cbor.Marshal(contactsIdx)
			if err != nil {
				return err
			}
			return txn.Set(contactsKey(), serialized)
		})
	})
}

// GetConversationIDs returns a slice of all Conversation IDs
func (a *App) GetConversationIDs() []uint64 {
	var conversationIDs map[uint64]struct{}
	a.db.View(func(txn *badger.Txn) error {
		i, err := txn.Get(conversationsKey())
		if err != nil {
			return err
		}
		return i.Value(func(val []byte) error {
			return cbor.Unmarshal(val, &conversationIDs)
		})
	})
	ids := make([]uint64, 0, len(conversationIDs))
	for k, _ := range conversationIDs {
		ids = append(ids, k)
	}
	return ids
}

// GetConversation retrieves Conversation from badger
func (a *App) GetConversation(id uint64) (*Conversation, error) {
	conv := new(Conversation)
	err := a.db.View(func(txn *badger.Txn) error {
		i, err := txn.Get(conversationKey(id))
		if err != nil {
			return err
		}
		return i.Value(func(val []byte) error {
			return cbor.Unmarshal(val, conv)
		})
	})
	if err != nil {
		return nil, err
	}
	return conv, nil
}

// PutConversation stores Conversation in badger
func (a *App) PutConversation(conversation *Conversation) error {
	fmt.Println("PutConversation")
	return a.db.Update(func(txn *badger.Txn) error {
		// serialize the conversation
		serialized, err := cbor.Marshal(conversation)
		if err != nil {
			return err
		}
		// store the serialized conversation
		err = txn.Set(conversationKey(conversation.ID), serialized)
		if err != nil {
			return err
		}

		// fetch the index of all conversations
		i, err := txn.Get(conversationsKey())
		if err != nil {
			return err
		}
		return i.Value(func(val []byte) error {
			conversationsIdx := make(map[uint64]struct{})
			err := cbor.Unmarshal(val, conversationsIdx)
			if err != nil {
				return err
			}

			// add conversation to index
			conversationsIdx[conversation.ID] = struct{}{}
			serialized, err := cbor.Marshal(conversationsIdx)
			if err != nil {
				return err
			}
			return txn.Set(conversationsKey(), serialized)
		})
	})
}

// GetMessage returns Message
func (a *App) GetMessage(msgId uint64) (*Message, error) {
	msg := new(Message)
	err := a.db.View(func(txn *badger.Txn) error {
		i, err := txn.Get(messageKey(msgId))
		if err != nil {
			return err
		}
		return i.Value(func(val []byte) error {
			return cbor.Unmarshal(val, msg)
		})
	})
	if err != nil {
		return nil, err
	}
	return msg, nil
}

// PutMessage places Message in db
func (a *App) PutMessage(msg *Message) error {
	fmt.Println("PutMessage(", msg.ID, ")")
	return a.db.Update(func(txn *badger.Txn) error {
		serialized, err := cbor.Marshal(msg)
		if err != nil {
			return err
		}
		return txn.Set(messageKey(msg.ID), serialized)
	})
}
