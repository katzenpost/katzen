package main

import (
	"fmt"
	"github.com/katzenpost/hpqc/bacap"
	"github.com/katzenpost/hpqc/rand"
	"github.com/stretchr/testify/require"
	"testing"
)

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
	r1ownerCap, r2readCap, err := r1.CompleteExchange(rcem1)
	require.NoError(err)

	// complete exchange for r2
	err = rcem2.UnmarshalBinary(r1Ex)
	require.NoError(err)
	r2ownerCap, r1readCap, err := r2.CompleteExchange(rcem2)
	require.NoError(err)

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
