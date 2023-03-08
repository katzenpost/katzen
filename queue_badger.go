package main

import (
	"encoding/binary"
	"github.com/dgraph-io/badger/v4"
	"github.com/fxamacker/cbor/v2"
)

type BadgerQueue struct {
	Prefix []byte // Only iterate over this given prefix
	db     *badger.DB
}

func (b *BadgerQueue) meta() []byte {
	return append(b.Prefix, []byte("queue_metadata")...)
}

// prefix e.g. "contact:userid:queue"
func NewBadgerQueue(db *badger.DB, prefix []byte) *BadgerQueue {
	q := &BadgerQueue{db: db, Prefix: prefix}
	// check queue exists or create it
	err := q.db.Update(func(txn *badger.Txn) error {
		_, err := txn.Get(q.meta())
		if err == badger.ErrKeyNotFound {
			ptrb := make([]byte, 16) // 2 uint64
			return txn.Set(q.meta(), ptrb)
		}
		return nil
	})
	if err != nil {
		panic(err)
	}
	return q
}

// Push increments the head pointer and writes to the queue
func (q *BadgerQueue) Push(e *Message) error {
	return q.db.Update(func(txn *badger.Txn) error {
		serialized, err := cbor.Marshal(e)
		if err != nil {
			return err
		}
		// read the queue metadata
		i, err := txn.Get(q.meta())
		if err != nil {
			return err
		}
		return i.Value(func(metadata []byte) error {
			// store value at head
			err := txn.Set(append(q.Prefix, metadata[:8]...), serialized)
			if err != nil {
				return err
			}

			// increment queue index
			qhead := binary.BigEndian.Uint64(metadata[:8])
			qhead += 1
			binary.BigEndian.PutUint64(metadata[:8], qhead)
			return txn.Set(q.meta(), metadata)
		})
	})
}

// Peek displays the head of queue
func (q *BadgerQueue) Peek() (*Message, error) {
	msg := new(Message)
	err := q.db.View(func(txn *badger.Txn) error {
		i, err := txn.Get(q.meta())
		if err != nil {
			return err
		}
		return i.Value(func(metadata []byte) error {
			head := binary.BigEndian.Uint64(metadata[:8])
			tail := binary.BigEndian.Uint64(metadata[8:])
			if head == tail {
				return ErrQueueEmpty
			}
			i, err := txn.Get(append(q.Prefix, metadata[8:]...))
			if err != nil {
				return err
			}
			return i.Value(func(msgb []byte) error {
				return cbor.Unmarshal(msgb, msg)
			})
		})
	})
	if err != nil {
		return nil, err
	}
	return msg, nil
}

func (q *BadgerQueue) Pop() (*Message, error) {
	msg := new(Message)
	err := q.db.Update(func(txn *badger.Txn) error {
		i, err := txn.Get(q.meta())
		if err != nil {
			return err
		}
		return i.Value(func(metadata []byte) error {
			head := binary.BigEndian.Uint64(metadata[:8])
			tail := binary.BigEndian.Uint64(metadata[8:])
			if head == tail {
				return ErrQueueEmpty
			}

			itemKey := append(q.Prefix, metadata[8:]...)
			i, err := txn.Get(itemKey)
			if err != nil {
				return err
			}
			// delete item from queue
			err = txn.Delete(itemKey)
			if err != nil {
				return err
			}

			// increment tail pointer
			tail += 1
			binary.BigEndian.PutUint64(metadata[8:], tail)
			err = txn.Set(q.meta(), metadata)
			if err != nil {
				return err
			}

			// deserialize value
			return i.Value(func(msgb []byte) error {
				return cbor.Unmarshal(msgb, msg)
			})
		})
	})
	if err != nil {
		return nil, err
	}
	return msg, nil
}
