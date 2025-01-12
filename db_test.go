package main

import (
	"fmt"
	"github.com/dgraph-io/badger/v4"
	"github.com/katzenpost/hpqc/rand"
	"github.com/stretchr/testify/require"
	"testing"
)

func badgerStore(t *testing.T) *BadgerStore {
	require := require.New(t)
	opt := badger.DefaultOptions("").WithInMemory(true)
	db, err := badger.Open(opt)
	require.NoError(err)
	bs := &BadgerStore{db: db}
	return bs
}

func TestBadgerInitDB(t *testing.T) {
	require := require.New(t)
	bs := badgerStore(t)
	err := bs.InitDB()
	require.NoError(err)
}

func TestBadgerNewContact(t *testing.T) {
	require := require.New(t)
	bs := badgerStore(t)
	require.NoError(bs.InitDB())
	secret := []byte("alicesecret")
	contact, err := bs.NewContact("alice", secret)
	require.NoError(err)
	require.Equal(contact.SharedSecret, secret)
	require.NotEqual(contact.Outbound, 0)
	require.True(contact.IsPending)
	require.Equal(contact.PandaResult, "")
}

func TestBadgerGetContact(t *testing.T) {
	require := require.New(t)
	bs := badgerStore(t)
	require.NoError(bs.InitDB())
	secret := []byte("alicesecret")
	contact, err := bs.NewContact("alice", secret)
	require.NoError(err)

	contact2, err := bs.GetContact(contact.ID)
	require.Equal(contact.ID, contact2.ID)
	require.Equal(contact.Nickname, contact2.Nickname)
	require.Equal(contact.SharedSecret, contact2.SharedSecret)
}

func TestBadgerGetContactIDs(t *testing.T) {
	require := require.New(t)
	bs := badgerStore(t)
	require.NoError(bs.InitDB())
	secret := []byte("alicesecret")
	for _, name := range []string{"alice", "bob", "eve", "mallory"} {
		_, err := bs.NewContact(name, secret)
		require.NoError(err)
	}

	ids := bs.GetContactIDs()
	require.Equal(len(ids), 4)
	lastId := uint64(0)
	for _, id := range ids {
		contact, err := bs.GetContact(id)
		require.NoError(err)
		require.Equal(contact.ID, id)
		require.NotEqual(contact.Nickname, "")
		require.NotEqual(contact.ID, lastId)
		lastId = contact.ID
	}
}

func TestBadgerDeleteContact(t *testing.T) {
	require := require.New(t)
	bs := badgerStore(t)
	require.NoError(bs.InitDB())
	secret := []byte("alicesecret")
	contact, err := bs.NewContact("alice", secret)
	require.NoError(err)
	err = bs.RemoveContact(contact.ID)
	require.NoError(err)
	_, err = bs.GetContact(contact.ID)
	require.Error(err, badger.ErrKeyNotFound)
	ids := bs.GetContactIDs()
	require.NotContains(ids, contact.ID)
}

func TestBadgerCreateConversation(t *testing.T) {
	require := require.New(t)
	bs := badgerStore(t)
	require.NoError(bs.InitDB())

	secret := []byte("somesecret")
	contact, err := bs.NewContact("alice", secret)
	require.NoError(err)
	conv, err := bs.NewConversation(contact.ID)
	require.NoError(err)
	require.Equal(len(conv.Contacts), 1)
	require.Equal(conv.Contacts[0], contact.ID)
}

func TestBadgerPutGetConversation(t *testing.T) {
	require := require.New(t)
	bs := badgerStore(t)
	require.NoError(bs.InitDB())

	// create 42 conversations and make sure the index is created and correctly fetches each conversation
	for i := 0; i < 42; i++ {
		conv := &Conversation{Title: fmt.Sprintf("A group conversation %d with the usual suspects", i), ID: rand.NewMath().Uint64(), Contacts: []uint64{1, 2, 3, 4}}
		err := bs.PutConversation(conv)
		require.NoError(err)
	}
	ids := bs.GetConversationIDs()
	require.Equal(len(ids), 42)
	for _, id := range ids {
		conv, err := bs.GetConversation(id)
		require.NoError(err)
		require.Equal(conv.ID, id)
	}
}

func TestBadgerDeleteConversation(t *testing.T) {
	require := require.New(t)
	bs := badgerStore(t)
	require.NoError(bs.InitDB())

	secret := []byte("somesecret")
	contact, err := bs.NewContact("alice", secret)
	require.NoError(err)
	conv, err := bs.NewConversation(contact.ID)
	require.NoError(err)
	err = bs.RemoveConversation(conv.ID)
	require.NoError(err)
	_, err = bs.GetConversation(conv.ID)
	require.Error(err, badger.ErrKeyNotFound)
	ids := bs.GetConversationIDs()
	require.NotContains(ids, conv.ID)
}

func TestBadgerPutGetMessage(t *testing.T) {
	require := require.New(t)
	bs := badgerStore(t)
	require.NoError(bs.InitDB())

	// create 42 messages and make sure the index is created and correctly fetches each message
	ids := make([]uint64, 42)
	for i := 0; i < 42; i++ {
		id := rand.NewMath().Uint64()
		ids[i] = id
		msg := &Message{ID: id, Conversation: 4242, Sender: rand.NewMath().Uint64(), Body: []byte(fmt.Sprintf("A test message: %d", i))}

		err := bs.PutMessage(msg)
		require.NoError(err)
	}

	require.Equal(len(ids), 42)
	for i, id := range ids {
		msg, err := bs.GetMessage(id)
		require.NoError(err)
		require.Equal(msg.Conversation, uint64(4242))
		require.Equal([]byte(fmt.Sprintf("A test message: %d", i)), msg.Body)
	}

}

func TestBadgerPutRemoveMessage(t *testing.T) {
	require := require.New(t)
	bs := badgerStore(t)
	require.NoError(bs.InitDB())
	ids := make([]uint64, 42)
	for i := 0; i < 42; i++ {
		id := rand.NewMath().Uint64()
		ids[i] = id
		msg := &Message{ID: id, Conversation: 4242, Sender: rand.NewMath().Uint64(), Body: []byte(fmt.Sprintf("A test message: %d", i))}

		err := bs.PutMessage(msg)
		require.NoError(err)
	}

	require.Equal(len(ids), 42)
	for _, id := range ids {
		err := bs.RemoveMessage(id)
		require.NoError(err)
		msg, err := bs.GetMessage(id)
		require.Error(err, badger.ErrKeyNotFound)
		require.Nil(msg)
	}
}

func TestBadgerDeliverMessage(t *testing.T) {
	require := require.New(t)
	bs := badgerStore(t)
	require.NoError(bs.InitDB())

	secret := []byte("somesecret")
	// create a new contact and a conversation
	contact, err := bs.NewContact("alice", secret)
	require.NoError(err)

	conv, err := bs.NewConversation(contact.ID)
	require.NoError(err)
	require.Equal(len(conv.Contacts), 1)
	require.Equal(conv.Contacts[0], contact.ID)

	// deliver messages into the conversation from the user
	for i := 0; i < 42; i++ {
		m := &Message{Conversation: conv.ID, ID: rand.NewMath().Uint64(), Sender: contact.ID, Body: []byte(fmt.Sprintf("test message %d", i))}
		err = bs.DeliverMessage(m)
		require.NoError(err)
	}
	// fetch conversation from db and verify that all of the messages were written
	conv2, err := bs.GetConversation(conv.ID)
	require.NoError(err)
	require.Equal(len(conv2.Messages), 42)
	for _, msgId := range conv2.Messages {
		msg, err := bs.GetMessage(msgId)
		require.NoError(err)
		require.Equal(conv.ID, msg.Conversation)
		require.Equal(contact.ID, msg.Sender)
	}
}

func TestBadgerSendMessage(t *testing.T) {
	require := require.New(t)
	bs := badgerStore(t)
	require.NoError(bs.InitDB())

	secret := []byte("somesecret")
	// create a set of users to place in a conversation
	for _, name := range []string{"alice", "bob", "eve", "mallory"} {
		_, err := bs.NewContact(name, secret)
		require.NoError(err)
	}

	// create a conversation with all of the contacts and save it in db
	ids := bs.GetContactIDs()
	conv := &Conversation{Title: "A group conversation with the usual suspects", ID: 1234, Contacts: ids}
	err := bs.PutConversation(conv)
	require.NoError(err)

	// write messages to the conversation, which will enqueue each message for each contact in the conversation
	msgs := make([]*Message, 42)
	for i := 0; i < 42; i++ {
		m := &Message{Conversation: conv.ID, ID: rand.NewMath().Uint64(), Sender: 4242, Body: []byte(fmt.Sprintf("test message %d", i))}
		err := bs.SendMessage(conv.ID, m)
		require.NoError(err)
		msgs[i] = m
	}

	// test that all the messages exist for the outbound queue for each user in the conversation
	for _, id := range ids {
		q := NewBadgerQueue(bs.db, outboundKey(id))
		for _, m2 := range msgs {
			m, err := q.Pop()
			require.NoError(err)
			require.Equal(m2.Body, m.Body)
		}
		_, err := q.Pop()
		require.Error(err, ErrQueueEmpty)
	}
}
