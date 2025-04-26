package main

import (
	"github.com/stretchr/testify/require"
	"testing"
)

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

	rcem := &ReadCapExchangeMessage{}

	// complete exchange for r1
	err = rcem.UnmarshalBinary(r2Ex)
	require.NoError(err)
	r1ownerCap, _, err := r1.CompleteExchange(rcem)
	require.NoError(err)

	// complete exchange for r2
	err = rcem.UnmarshalBinary(r1Ex)
	require.NoError(err)
	_, r2readCap, err := r2.CompleteExchange(rcem)

	r1ReadCapForr2, err := r1ownerCap.UniversalReadCap().MarshalBinary()
	require.NoError(err)

	// show that r1's BoxOwnerCap produces r2's UniversalReadCap
	r2readCapBytes, err := r2readCap.MarshalBinary()
	require.NoError(err)
	require.Equal(r2readCapBytes, r1ReadCapForr2)
}
