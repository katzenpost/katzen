// make a new badger db
// make a new queue
// push message to the queue

package main

import (
	"github.com/dgraph-io/badger/v4"
	"github.com/stretchr/testify/require"
	"testing"
)

func TestBadgerQueue(t *testing.T) {
	require := require.New(t)
	opt := badger.DefaultOptions("").WithInMemory(true)
	db, err := badger.Open(opt)
	require.NoError(err)
	bq := NewBadgerQueue(db, []byte("foo"))

	m := new(Message)
	m.Body = []byte("hello")
	err = bq.Push(m)
	require.NoError(err)

	m2, err := bq.Peek()
	require.NoError(err)

	require.Equal(m2.Body, m.Body)

	m3, err := bq.Pop()
	require.NoError(err)
	require.Equal(m2.Body, m3.Body)
	_, err = bq.Pop()
	require.Error(err, ErrQueueEmpty)
}
