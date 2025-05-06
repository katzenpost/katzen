package main

import (
	"fmt"
	"github.com/katzenpost/hpqc/bacap"
	"github.com/katzenpost/hpqc/rand"
	"github.com/stretchr/testify/require"
	"testing"
)

func TestNewReadCapExchange(t *testing.T) {
	require := require.New(t)
	ex, err := NewReadCapExchange()
	require.NoError(err)
	require.NotNil(ex.edPubKey)
	require.NotNil(ex.edPrivKey)
	require.NotNil(ex.nkPrivKey)
}

func TestMarshalUnmarshalReadCapExchange(t *testing.T) {
	require := require.New(t)
	ex, err := NewReadCapExchange()
	require.NoError(err)
	b, err := ex.MarshalBinary()
	require.NoError(err)

	ex2 := new(ReadCapExchange)
	err = ex2.UnmarshalBinary(b)
	require.NoError(err)

	epk1, err := ex.edPubKey.MarshalBinary()
	require.NoError(err)
	esk1, err := ex.edPrivKey.MarshalBinary()
	require.NoError(err)
	nsk1, err := ex.nkPrivKey.MarshalBinary()
	require.NoError(err)

	epk2, err := ex2.edPubKey.MarshalBinary()
	require.NoError(err)
	esk2, err := ex2.edPrivKey.MarshalBinary()
	require.NoError(err)
	nsk2, err := ex2.nkPrivKey.MarshalBinary()
	require.NoError(err)

	require.Equal(epk1, epk2)
	require.Equal(esk1, esk2)
	require.Equal(nsk1, nsk2)
}

func TestMarshalUnmarshalReadCapExchangeMessage(t *testing.T) {
	require := require.New(t)

	// create new exchange
	ex, err := NewReadCapExchange()
	require.NoError(err)

	// create reference message from exchange
	// verify that ReadCapExchange.ExchangeBytes() is ReadCapExchangeMessage{}.MarshalBinary()
	m := &ReadCapExchangeMessage{edPubKey: ex.edPubKey, nkPubKey: ex.nkPrivKey.Public()}
	b, err := m.MarshalBinary()
	require.NoError(err)
	b2, err := ex.ExchangeBytes()
	require.NoError(err)
	require.Equal(b, b2)

	// deserialize and verify vs reference
	m2 := &ReadCapExchangeMessage{}
	err = m2.UnmarshalBinary(b)
	require.NoError(err)

	epk1, err := m.edPubKey.MarshalBinary()
	require.NoError(err)
	npk1, err := ex.nkPrivKey.Public().MarshalBinary()
	require.NoError(err)

	epk2, err := m2.edPubKey.MarshalBinary()
	require.NoError(err)
	npk2, err := m2.nkPubKey.MarshalBinary()
	require.NoError(err)

	require.Equal(epk1, epk2)
	require.Equal(npk1, npk2)
}

func TestAdvanceCaps(t *testing.T) {
	// ctx agreedout of band
	ctx := []byte("TestAdvanceCaps")
	// write three entries of data
	require := require.New(t)
	ownerCap, err := bacap.NewBoxOwnerCap(rand.Reader)
	require.NoError(err)
	readCap := ownerCap.UniversalReadCap()

	sw, err := bacap.NewStatefulWriter(ownerCap, ctx)
	require.NoError(err)
	type ctsig struct {
		ct  []byte
		sig []byte
	}
	ctmap := make(map[[32]byte](*ctsig))

	msg := func(i int) []byte {
		return []byte(fmt.Sprintf("msg #%d", i))
	}
	for i := 0; i < 43; i++ {
		boxID, ct, sig, err := sw.EncryptNext(msg(i))
		ctmap[boxID] = &ctsig{ct: ct, sig: sig}
		require.NoError(err)
	}

	readCap42, err := AdvanceReadCapBy(readCap, 42)
	require.NoError(err)

	sr, err := bacap.NewStatefulReader(readCap42, ctx)
	require.NoError(err)
	boxID, err := sr.NextBoxID()
	require.NoError(err)
	c, ok := ctmap[boxID.ByteArray()]
	require.True(ok)
	var sig64 [64]byte
	copy(sig64[:], c.sig)

	m, err := sr.DecryptNext(ctx, boxID.ByteArray(), c.ct, sig64)
	require.NoError(err)
	require.Equal(m, msg(42))
}

func TestCompleteExchange(t *testing.T) {
	require := require.New(t)
	r1, err := NewReadCapExchange()
	require.NoError(err)

	r2, err := NewReadCapExchange()
	require.NoError(err)

	r1Ex, err := r1.ExchangeBytes()
	require.NoError(err)

	r2Ex, err := r2.ExchangeBytes()
	require.NoError(err)

	rcem1 := &ReadCapExchangeMessage{}
	rcem2 := &ReadCapExchangeMessage{}

	// complete exchange for r1
	err = rcem1.UnmarshalBinary(r2Ex)
	require.NoError(err)
	ctx, r1ownerCap, r2readCap, err := r1.CompleteExchange(rcem1)
	require.NoError(err)

	// complete exchange for r2
	err = rcem2.UnmarshalBinary(r1Ex)
	require.NoError(err)
	ctx2, r2ownerCap, r1readCap, err := r2.CompleteExchange(rcem2)
	require.NoError(err)

	require.Equal(ctx, ctx2)

	// show that readcap held by r1 equals readcap created by r2
	r2readCapBytesByOwner, err := r2ownerCap.UniversalReadCap().MarshalBinary()
	require.NoError(err)
	r2readCapBytes, err := r2readCap.MarshalBinary()
	require.NoError(err)
	require.Equal(r2readCapBytes, r2readCapBytesByOwner)

	// show that readcap held by r2 equals readcap created by r1
	r1readCapBytesByOwner, err := r1ownerCap.UniversalReadCap().MarshalBinary()
	require.NoError(err)
	r1readCapBytes, err := r1readCap.MarshalBinary()
	require.NoError(err)
	require.Equal(r1readCapBytes, r1readCapBytesByOwner)
}
