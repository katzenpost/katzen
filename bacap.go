package main

import (
	"errors"
	"fmt"
	"hash"
	"io"

	"github.com/katzenpost/hpqc/bacap"
	"github.com/katzenpost/hpqc/nike"
	"github.com/katzenpost/hpqc/nike/schemes"
	"github.com/katzenpost/hpqc/sign"
	"github.com/katzenpost/hpqc/sign/ed25519"
	sClient "github.com/katzenpost/katzenpost/scratch/client"
	"golang.org/x/crypto/blake2b"
	"golang.org/x/crypto/hkdf"
)

var nikeScheme = schemes.ByName("X25519")
var signScheme = ed25519.Scheme()

// ReadCapExchangeMessage contains public keys for exchanging bacap.UniversalReadCap with a peer using a NIKE
type ReadCapExchangeMessage struct {
	edPubKey sign.PublicKey
	nkPubKey nike.PublicKey
}

// ReadCapExchange contains key material used to initiate and complete the exchange
type ReadCapExchange struct {
	edPrivKey sign.PrivateKey
	edPubKey  sign.PublicKey
	nkPrivKey nike.PrivateKey
}

// MarshalBinary implmements encoding.BinaryMarshaler
func (r *ReadCapExchange) MarshalBinary() ([]byte, error) {
	edPubKeyBytes, err := r.edPubKey.MarshalBinary()
	if err != nil {
		return nil, err
	}
	edPrivKeyBytes, err := r.edPrivKey.MarshalBinary()
	if err != nil {
		return nil, err
	}
	nkPrivKeyBytes, err := r.nkPrivKey.MarshalBinary()
	if err != nil {
		return nil, err
	}
	resp := []byte{}
	resp = append(resp, edPubKeyBytes...)
	resp = append(resp, edPrivKeyBytes...)
	resp = append(resp, nkPrivKeyBytes...)
	return resp, nil
}

// UnmarshalBinary implements encoding.BinaryUnmarshaler
func (r *ReadCapExchange) UnmarshalBinary(ex []byte) error {
	if len(ex) != nikeScheme.PrivateKeySize()+signScheme.PrivateKeySize()+signScheme.PublicKeySize() {
		return errors.New("Invalid size")
	}
	edPubKeyBytes := ex[:signScheme.PublicKeySize()]
	edPrivKeyBytes := ex[signScheme.PublicKeySize() : signScheme.PublicKeySize()+signScheme.PrivateKeySize()]
	nkPrivKeyBytes := ex[signScheme.PublicKeySize()+signScheme.PrivateKeySize():]
	nkPrivKey, err := nikeScheme.UnmarshalBinaryPrivateKey(nkPrivKeyBytes)
	if err != nil {
		return err
	}
	edPrivKey, err := signScheme.UnmarshalBinaryPrivateKey(edPrivKeyBytes)
	if err != nil {
		return err
	}

	edPubKey, err := signScheme.UnmarshalBinaryPublicKey(edPubKeyBytes)
	if err != nil {
		return err
	}
	r.edPubKey = edPubKey
	r.edPrivKey = edPrivKey
	r.nkPrivKey = nkPrivKey
	return nil
}

// ExchangeBytes returns serialized exchnage
func (r *ReadCapExchange) ExchangeBytes() ([]byte, error) {
	m := &ReadCapExchangeMessage{
		edPubKey: r.edPubKey,
		nkPubKey: r.nkPrivKey.Public(),
	}
	return m.MarshalBinary()
}

func (r *ReadCapExchangeMessage) MarshalBinary() ([]byte, error) {
	if r.edPubKey == nil || r.nkPubKey == nil {
		panic("Not Initialized")
	}
	b1, err := r.edPubKey.MarshalBinary()
	if err != nil {
		return nil, err
	}
	b2, err := r.nkPubKey.MarshalBinary()
	if err != nil {
		return nil, err
	}

	return append(b1, b2...), nil
}

func (r *ReadCapExchangeMessage) UnmarshalBinary(ex []byte) error {
	if len(ex) != signScheme.PublicKeySize()+nikeScheme.PublicKeySize() {
		return errors.New("Failed to parse peer readcap exchange")
	}
	verifierKey, err := signScheme.UnmarshalBinaryPublicKey(ex[:signScheme.PublicKeySize()])
	if err != nil {
		return err
	}
	peerDHKey, err := nikeScheme.UnmarshalBinaryPublicKey(ex[signScheme.PublicKeySize():])
	if err != nil {
		return err
	}
	r.edPubKey = verifierKey
	r.nkPubKey = peerDHKey
	return nil
}

// NewReadCapExchange returns ReadCapExchange that is used to initialize a BoxOwnerCap, UniversalReadCap with another peer
func NewReadCapExchange() (*ReadCapExchange, error) {
	edPubKey, edPrivKey, err := signScheme.GenerateKey()
	if err != nil {
		return nil, err
	}
	_, nkPrivKey, err := nikeScheme.GenerateKeyPair()
	if err != nil {
		return nil, err
	}
	return &ReadCapExchange{
		edPubKey:  edPubKey,
		edPrivKey: edPrivKey,
		nkPrivKey: nkPrivKey,
	}, nil
}

// CompleteExchange returns BowOwnerCap and UniversalReadCap for writing and reading to peer
func (k ReadCapExchange) CompleteExchange(kx *ReadCapExchangeMessage) (*bacap.BoxOwnerCap, *bacap.UniversalReadCap, error) {
	// derive secret from nike and create a UniversalReadCap
	sharedSecret := nikeScheme.DeriveSecret(k.nkPrivKey, kx.nkPubKey)
	hash := func() hash.Hash {
		h, _ := blake2b.New512(nil)
		return h
	}
	h := hkdf.New(hash, sharedSecret, nil, nil)
	boxIndexSeed := make([]byte, 32)
	_, err := io.ReadFull(h, boxIndexSeed)
	if err != nil {
		return nil, nil, err
	}

	blindingFactor := make([]byte, 32)
	_, err = io.ReadFull(h, blindingFactor)
	if err != nil {
		return nil, nil, err
	}
	var ownerCap *bacap.BoxOwnerCap
	switch k := k.edPrivKey.(type) {
	case *ed25519.PrivateKey:
		// ed25519.PrivateKey.Blind(fac []byte) returns ed25519.BlindedPrivateKey
		// ... so marshal the key and unmarshal to get the right type
		blindedPrivateKey := k.Blind(blindingFactor)
		b, _ := blindedPrivateKey.MarshalBinary()
		converted := append(b, blindedPrivateKey.PublicKey().Bytes()...)
		ownerCapPrivateKey := ed25519.NewEmptyPrivateKey()
		err := ownerCapPrivateKey.UnmarshalBinary(converted)
		if err != nil {
			return nil, nil, err
		}
		ownerCap = sClient.NewOwnerCapFromSeed(ownerCapPrivateKey, boxIndexSeed)
	default:
		return nil, nil, errors.New("Unsupported sign.Scheme")
	}

	// Blind is not in sign.Scheme, but ed25519.PublicKey has it.
	switch k := kx.edPubKey.(type) {
	case *ed25519.PublicKey:
		// ed25519.PublicKey.Blind conveniently returns the blinded ed25519.PublicKey
		readCap := sClient.NewUniversalReadCapFromSeed(k.Blind(blindingFactor), boxIndexSeed)
		return ownerCap, readCap, nil
	default:
		return nil, nil, errors.New("Unsupported sign.Scheme")
	}
}

func AdvanceOwnerCapBy(c *bacap.BoxOwnerCap, count uint64) (*bacap.BoxOwnerCap, error) {
	rawBytes, _ := c.MarshalBinary()
	mbiBytes := rawBytes[ed25519.PrivateKeySize:]
	mbi := &bacap.MessageBoxIndex{}
	err := mbi.UnmarshalBinary(mbiBytes)
	if err != nil {
		return nil, err
	}

	newmbi, err := mbi.AdvanceIndexTo(mbi.Idx64 + count)
	if err != nil {
		return nil, err
	}
	newmbiBytes, err := newmbi.MarshalBinary()
	if err != nil {
		return nil, err
	}
	newCap := &bacap.BoxOwnerCap{}
	err = newCap.UnmarshalBinary(append(rawBytes[:ed25519.PrivateKeySize], newmbiBytes...))
	if err != nil {
		return nil, err
	}
	return newCap, nil

}

func AdvanceReadCapBy(c *bacap.UniversalReadCap, count uint64) (*bacap.UniversalReadCap, error) {
	rawBytes, _ := c.MarshalBinary()
	mbiBytes := rawBytes[ed25519.PublicKeySize:]
	mbi := &bacap.MessageBoxIndex{}
	err := mbi.UnmarshalBinary(mbiBytes)
	if err != nil {
		return nil, err
	}

	newmbi, err := mbi.AdvanceIndexTo(mbi.Idx64 + count)
	if err != nil {
		return nil, err
	}
	newmbiBytes, err := newmbi.MarshalBinary()
	if err != nil {
		return nil, err
	}
	newCap := &bacap.UniversalReadCap{}
	newCap.UnmarshalBinary(append(rawBytes[:ed25519.PublicKeySize], newmbiBytes...))
	return newCap, nil
}
