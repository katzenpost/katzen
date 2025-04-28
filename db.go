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
	"image/png"
	"time"

	"github.com/dgraph-io/badger/v4"
	"github.com/fxamacker/cbor/v2"
	"github.com/katzenpost/hpqc/rand"
	"github.com/katzenpost/katzenpost/stream"
	"golang.org/x/crypto/hkdf"
)

var (
	ErrConversationAlreadyExists = errors.New("Conversation already exists")
	DBVersion                    = []byte("0.0.0")
)

// BadgerStore holds katzen data and wraps a BadgerDB instance
type BadgerStore struct {
	db *badger.DB
}

// InitDB initializes default values of the BadgerStore
func (a *BadgerStore) InitDB() error {
	return a.db.Update(func(txn *badger.Txn) error {
		_, err := txn.Get(versionKey())
		if err == nil {
			// TODO: here goes functions for updates
		} else {
			// initialize keys
			contactsIdx, _ := cbor.Marshal(make(map[uint64]struct{}))
			conversationIdx, _ := cbor.Marshal(make(map[uint64]struct{}))
			downloadIdx, _ := cbor.Marshal(make(map[uint64]struct{}))
			uploadIdx, _ := cbor.Marshal(make(map[uint64]struct{}))

			err = txn.Set(contactsKey(), contactsIdx)
			if err != nil {
				return err
			}
			err = txn.Set(conversationsKey(), conversationIdx)
			if err != nil {
				return err
			}
			err = txn.Set(downloadsKey(), downloadIdx)
			if err != nil {
				return err
			}
			err = txn.Set(uploadsKey(), uploadIdx)
			if err != nil {
				return err
			}
			err = txn.Set(versionKey(), DBVersion)
			if err != nil {
				return err
			}

			if hasDefaultTor() {
				err = txn.Set([]byte("UseTor"), []byte{0xFF})
				if err != nil {
					return err
				}
			} else {
				err = txn.Set([]byte("UseTor"), []byte{0x00})
				if err != nil {
					return err
				}
			}
		}
		return nil
	})
}

func versionKey() []byte {
	return []byte("katzen_version")
}

func chunkKey(id uint64) []byte {
	return []byte(fmt.Sprintf("chunk:%d", id))
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

func streamKey(id uint64) []byte {
	return []byte(fmt.Sprintf("stream:%d", id))
}

func transferKey(id uint64) []byte {
	return []byte(fmt.Sprintf("transfer:%d", id))
}

func uploadKey(id uint64) []byte {
	return []byte(fmt.Sprintf("upload:%d", id))
}

func downloadsKey() []byte {
	return []byte("downloads")
}

func uploadsKey() []byte {
	return []byte("uploads")
}

func downloadKey(id uint64) []byte {
	return []byte(fmt.Sprintf("download:%d", id))
}

// RemoveContact removes a contact from the db
func (a *BadgerStore) RemoveContact(contactID uint64) error {
	return a.db.Update(func(txn *badger.Txn) error {
		i, err := txn.Get(contactsKey())
		if err != nil {
			return err
		}
		return i.Value(func(val []byte) error {

			contactsIdx := make(map[uint64]struct{})
			err := cbor.Unmarshal(val, &contactsIdx)
			if err != nil {
				return err
			}
			delete(contactsIdx, contactID)
			serialized, err := cbor.Marshal(contactsIdx)
			if err != nil {
				return err
			}
			err = txn.Set(contactsKey(), serialized)
			if err != nil {
				return err
			}

			return txn.Delete(contactKey(contactID))
		})
	})
}

// NewContact creates a new Contact from a shared secret (dialer)
func (a *BadgerStore) NewContact(nickname string, secret []byte) (*Contact, error) {
	// create a new ReadCapExchange
	rc, err := NewReadCapExchange()
	if err != nil {
		return nil, err
	}
	contactID := rand.NewMath().Uint64()
	contact := &Contact{ID: contactID, Nickname: nickname, ReadCapExchange: rc, SharedSecret: secret, IsPending: true, Outbound: rand.NewMath().Uint64()}
	err = a.PutContact(contact)
	if err != nil {
		return nil, err
	}

	return contact, nil
}

// NewConversation creates a Conversation with a Contact
func (a *BadgerStore) NewConversation(contactID uint64) (*Conversation, error) {
	conversation := new(Conversation)
	err := a.db.Update(func(txn *badger.Txn) error {
		// Create a Contact to deserialize into
		contact := new(Contact)
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

		// store conversation in the db
		conversation.ID = conversationID
		conversation.Title = contact.Nickname
		conversation.Contacts = []uint64{contact.ID}

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
	if err != nil {
		return nil, err
	}
	return conversation, nil
}

// RemoveConversation removes a conversation from the db
func (a *BadgerStore) RemoveConversation(conversationID uint64) error {
	return a.db.Update(func(txn *badger.Txn) error {
		i, err := txn.Get(conversationsKey())
		if err != nil {
			return err
		}
		return i.Value(func(val []byte) error {

			conversationIDs := make(map[uint64]struct{})
			err := cbor.Unmarshal(val, &conversationIDs)
			if err != nil {
				return err
			}
			delete(conversationIDs, conversationID)
			serialized, err := cbor.Marshal(conversationIDs)
			if err != nil {
				return err
			}
			err = txn.Set(conversationsKey(), serialized)
			if err != nil {
				return err
			}

			return txn.Delete(conversationKey(conversationID))
		})
	})
}

// DeliverMessage adds a Message to the Conversation
func (a *BadgerStore) DeliverMessage(msg *Message) error {
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
func (a *BadgerStore) SendMessage(conversation uint64, msg *Message) error {
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
func (a *BadgerStore) GetContactIDs() []uint64 {
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
func (a *BadgerStore) GetContact(contactID uint64) (*Contact, error) {
	contact := new(Contact)
	// initialize concrete types to deserialize into
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

// PutAvatar stores the Contact Avatar image.Image {
func (a *BadgerStore) PutAvatar(contactID uint64, img image.Image) error {
	// png encode avatar image
	buf := new(bytes.Buffer)
	if err := png.Encode(buf, img); err != nil {
		return err
	}
	return a.db.Update(func(txn *badger.Txn) error {
		return txn.Set(avatarKey(contactID), buf.Bytes())
	})
}

// GetAvatar retrieves the Contact Avatar image.Image
func (a *BadgerStore) GetAvatar(contactID uint64, sz image.Point) (image.Image, error) {
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
func (a *BadgerStore) PutContact(contact *Contact) error {
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
		if err != nil {
			return err
		}
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
func (a *BadgerStore) GetConversationIDs() []uint64 {
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
func (a *BadgerStore) GetConversation(id uint64) (*Conversation, error) {
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
func (a *BadgerStore) PutConversation(conversation *Conversation) error {
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
			err := cbor.Unmarshal(val, &conversationsIdx)
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
func (a *BadgerStore) GetMessage(msgId uint64) (*Message, error) {
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
func (a *BadgerStore) PutMessage(msg *Message) error {
	return a.db.Update(func(txn *badger.Txn) error {
		serialized, err := cbor.Marshal(msg)
		if err != nil {
			return err
		}
		return txn.Set(messageKey(msg.ID), serialized)
	})
}

// RemoveMessage removes a Message from the db
func (a *BadgerStore) RemoveMessage(msgId uint64) error {
	return a.db.Update(func(txn *badger.Txn) error {
		return txn.Delete(messageKey(msgId))
	})
}

// GetStream returns Stream
func (a *BadgerStore) GetStream(streamId uint64) (*stream.BufferedStream, error) {
	// XXX: Stream doesn't unmarshal nicely
	st := new(stream.BufferedStream)
	err := a.db.View(func(txn *badger.Txn) error {
		i, err := txn.Get(streamKey(streamId))
		if err != nil {
			return err
		}
		return i.Value(func(val []byte) error {
			if s, err := stream.LoadStream(val); err == nil {
				st.Stream = s
			} else {
				return err
			}
			return nil
		})
	})
	if err != nil {
		return nil, err
	}
	return st, nil
}

// PutStream places a Halted Stream in db
func (a *BadgerStore) PutStream(streamID uint64, stream *stream.BufferedStream) error {
	return a.db.Update(func(txn *badger.Txn) error {
		serialized, err := stream.Stream.Save()
		if err != nil {
			return err
		}
		return txn.Set(streamKey(streamID), serialized)
	})
}

// SetAutoConnect(status) controls whether katzen should connect automatically or not
func (a *BadgerStore) SetAutoConnect(status bool) {
	var val []byte
	if status {
		val = []byte{0xFF}
	} else {
		val = []byte{0x00}
	}
	err := a.db.Update(func(txn *badger.Txn) error {
		return txn.Set([]byte("AutoConnect"), val)
	})
	if err != nil {
		panic(err)
	}
}

// AutoConnect returns true if AutoConnect is enabled
func (a *BadgerStore) AutoConnect() bool {
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
	return doAutoConnect
}

// SetUseTor(status) controls whether Tor usage is enabled or not
func (a *BadgerStore) SetUseTor(status bool) {
	var val []byte
	if status {
		val = []byte{0xFF}
	} else {
		val = []byte{0x00}
	}
	a.db.Update(func(txn *badger.Txn) error {
		return txn.Set([]byte("UseTor"), val)
	})
}

// UseTor returns true if Tor usage is enabled
func (a *BadgerStore) UseTor() bool {
	useTor := false
	// read database for Tor setting (set at startup
	err := a.db.View(func(txn *badger.Txn) error {
		i, err := txn.Get([]byte("UseTor"))
		if err != nil {
			return err
		}
		return i.Value(func(val []byte) error {
			if val[0] == 0xFF {
				useTor = true
			} else {
				useTor = false
			}
			return nil
		})
	})
	if err != nil {
		// but not if you specified your own cfg file
		useTor = false
	}
	return useTor
}

// PutChunk places Chunk in db
func (a *BadgerStore) PutChunk(chunk *Chunk) error {
	return a.db.Update(func(txn *badger.Txn) error {
		serialized, err := cbor.Marshal(chunk)
		if err != nil {
			return err
		}
		return txn.Set(chunkKey(chunk.ID), serialized)
	})
}

// RemoveChunk removes a Chunk from the db
func (a *BadgerStore) RemoveChunk(chunkId uint64) error {
	return a.db.Update(func(txn *badger.Txn) error {
		return txn.Delete(chunkKey(chunkId))
	})
}

// GetChunk returns Chunk
func (a *BadgerStore) GetChunk(chunkId uint64) (*Chunk, error) {
	chunk := new(Chunk)
	err := a.db.View(func(txn *badger.Txn) error {
		i, err := txn.Get(chunkKey(chunkId))
		if err != nil {
			return err
		}
		return i.Value(func(val []byte) error {
			return cbor.Unmarshal(val, chunk)
		})
	})
	if err != nil {
		return nil, err
	}
	return chunk, nil
}

// NewUpload returns an Upload with TransferState and using the specified UploadHeader
func (a *BadgerStore) NewUpload(header *UploadHeader, path string) (*Upload, error) {
	id := rand.NewMath().Uint64()
	ts := &TransferState{Chunks: []uint64{}, Length: 0}
	ul := &Upload{ID: id, Header: header, State: ts, Path: path}
	err := a.PutUpload(ul)
	if err != nil {
		return nil, err
	}
	return ul, nil
}

// PutUpload stores an Upload in the db
func (a *BadgerStore) PutUpload(ul *Upload) error {
	return a.db.Update(func(txn *badger.Txn) error {
		// save the transferstate
		serialized, err := cbor.Marshal(ul)
		if err != nil {
			return err
		}
		err = txn.Set(uploadKey(ul.ID), serialized)
		if err != nil {
			return err
		}
		// fetch the index of all uploads
		i, err := txn.Get(uploadsKey())
		if err != nil {
			return err
		}
		return i.Value(func(val []byte) error {
			uploadsIdx := make(map[uint64]struct{})
			err := cbor.Unmarshal(val, &uploadsIdx)
			if err != nil {
				return err
			}

			// add upload to index
			uploadsIdx[ul.ID] = struct{}{}
			serialized, err := cbor.Marshal(uploadsIdx)
			if err != nil {
				return err
			}
			return txn.Set(uploadsKey(), serialized)
		})

		return nil
	})
}

// GetUploadIDs returns a slice of all Upload IDs
func (a *BadgerStore) GetUploadIDs() []uint64 {
	var uploadIDs map[uint64]struct{}
	a.db.View(func(txn *badger.Txn) error {
		i, err := txn.Get(uploadsKey())
		if err != nil {
			return err
		}
		return i.Value(func(val []byte) error {
			return cbor.Unmarshal(val, &uploadIDs)
		})
	})
	ids := make([]uint64, 0, len(uploadIDs))
	for k, _ := range uploadIDs {
		ids = append(ids, k)
	}
	return ids
}

// GetUpload returns the upload state and header
func (a *BadgerStore) GetUpload(ulId uint64) (*Upload, error) {
	ul := new(Upload)
	// initialize concrete types to deserialize into
	// TODO: TransferState could be stored/retrieved under a different key and updated independently
	ul.State = new(TransferState)
	ul.Header = new(UploadHeader)
	err := a.db.View(func(txn *badger.Txn) error {
		i, err := txn.Get(uploadKey(ulId))
		if err != nil {
			return err
		}
		return i.Value(func(val []byte) error {
			return cbor.Unmarshal(val, ul)
		})
	})
	if err != nil {
		return nil, err
	}
	return ul, nil
}

// RemoveUpload removes the Upload state and header from db
func (a *BadgerStore) RemoveUpload(ulId uint64) error {
	return a.db.Update(func(txn *badger.Txn) error {
		i, err := txn.Get(uploadsKey())
		if err != nil {
			return err
		}
		return i.Value(func(val []byte) error {

			uploadIDs := make(map[uint64]struct{})
			err := cbor.Unmarshal(val, &uploadIDs)
			if err != nil {
				return err
			}
			delete(uploadIDs, ulId)
			serialized, err := cbor.Marshal(uploadIDs)
			if err != nil {
				return err
			}
			err = txn.Set(uploadsKey(), serialized)
			if err != nil {
				return err
			}

			return txn.Delete(uploadKey(ulId))
		})
	})
}

// NewDownload returns a new Download using the specified DownloadHeader
func (a *BadgerStore) NewDownload(header *DownloadHeader, path string) (*Download, error) {
	id := rand.NewMath().Uint64()
	ts := &TransferState{Chunks: []uint64{}, Length: 0}
	dl := &Download{ID: id, Header: header, State: ts, Path: path}
	err := a.PutDownload(dl)
	if err != nil {
		return nil, err
	}
	return dl, nil
}

// GetDownloadIDs returns a slice of all Download IDs
func (a *BadgerStore) GetDownloadIDs() []uint64 {
	var downloadIDs map[uint64]struct{}
	a.db.View(func(txn *badger.Txn) error {
		i, err := txn.Get(downloadsKey())
		if err != nil {
			return err
		}
		return i.Value(func(val []byte) error {
			return cbor.Unmarshal(val, &downloadIDs)
		})
	})
	ids := make([]uint64, 0, len(downloadIDs))
	for k, _ := range downloadIDs {
		ids = append(ids, k)
	}
	return ids
}

// PutDownload stores a Download in the db
func (a *BadgerStore) PutDownload(dl *Download) error {
	return a.db.Update(func(txn *badger.Txn) error {
		// save the transferstate
		serialized, err := cbor.Marshal(dl)
		if err != nil {
			return err
		}
		err = txn.Set(downloadKey(dl.ID), serialized)
		if err != nil {
			return err
		}

		// fetch the index of all downloads
		i, err := txn.Get(downloadsKey())
		if err != nil {
			return err
		}
		return i.Value(func(val []byte) error {
			downloadsIdx := make(map[uint64]struct{})
			err := cbor.Unmarshal(val, &downloadsIdx)
			if err != nil {
				return err
			}

			// add download to index
			downloadsIdx[dl.ID] = struct{}{}
			serialized, err := cbor.Marshal(downloadsIdx)
			if err != nil {
				return err
			}
			return txn.Set(downloadsKey(), serialized)
		})
		return nil
	})
}

// GetDownload returns the Download state and header from db
func (a *BadgerStore) GetDownload(dlId uint64) (*Download, error) {
	dl := new(Download)
	// initialize concrete types to deserialize into
	dl.State = new(TransferState)
	dl.Header = new(DownloadHeader)
	err := a.db.View(func(txn *badger.Txn) error {
		i, err := txn.Get(downloadKey(dlId))
		if err != nil {
			return err
		}
		return i.Value(func(val []byte) error {
			return cbor.Unmarshal(val, dl)
		})
	})
	if err != nil {
		return nil, err
	}
	return dl, nil
}

// RemoveDownload removes the Download state and header from db
func (a *BadgerStore) RemoveDownload(dlId uint64) error {
	return a.db.Update(func(txn *badger.Txn) error {
		return txn.Delete(downloadKey(dlId))
	})
}

func (a *BadgerStore) Close() {
	a.db.Close()
}
