package main

import (
	"net/http"
	_ "net/http/pprof"
	"runtime"

	"context"
	"errors"
	"github.com/katzenpost/hpqc/bacap"
	"github.com/katzenpost/hpqc/rand"
	"github.com/katzenpost/hpqc/sign/ed25519"
	"github.com/katzenpost/katzenpost/stream"
	"github.com/stretchr/testify/require"
	"io"
	"os"
	"sync"
	"testing"
	"time"
)

func TestNewUploadHeader(t *testing.T) {
	require := require.New(t)
	tmp, err := os.CreateTemp("", "")
	require.NoError(err)

	// create temp file
	f, err := os.OpenFile(tmp.Name(), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0644)
	require.NoError(err)
	buf := make([]byte, 4200)
	_, err = io.ReadFull(rand.Reader, buf)
	require.NoError(err)
	n, err := f.Write(buf)
	require.NoError(err)
	require.Equal(n, len(buf))
	f.Close()

	// create upload header
	uh, err := NewUploadHeader(tmp.Name())
	require.NoError(err)
	require.Equal(len(uh.Sum256), 32)
	require.NotNil(uh.WriteCap)
}

func TestUploaderDownloader(t *testing.T) {
	require := require.New(t)
	bs := badgerStore(t)
	require.NoError(bs.InitDB())
	tmp, err := os.CreateTemp("", "")
	require.NoError(err)
	defer os.Remove(tmp.Name())

	// create temp file
	f, err := os.OpenFile(tmp.Name(), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0644)
	require.NoError(err)
	buf := make([]byte, 4200)
	_, err = io.ReadFull(rand.Reader, buf)
	require.NoError(err)
	n, err := f.Write(buf)
	require.NoError(err)
	require.Equal(n, len(buf))
	f.Close()

	// create upload header and upload file
	uh, err := NewUploadHeader(tmp.Name())
	require.NoError(err)
	t.Logf("made NewUploadHeader")
	ul, err := bs.NewUpload(uh, tmp.Name())
	require.NoError(err)
	t.Logf("made NewUpload")
	ulr, err := NewUploader(bs, ul)
	require.NoError(err)
	transport := NewMockTransport()
	t.Logf("made NewUploader")
	ulr.StartWithTransport(transport)
	t.Logf("Started Uploader with MockTransport")
	t.Logf("waiting for 3..2...1")
	<-time.After(4 * time.Second)
	ulr.Halt()
	t.Logf("Halted upload")
	//buf2, err := os.ReadFile(tmp.Name())
	require.NoError(err)
	require.Equal(ulr.Upload.State.Status, Completed)
	t.Logf("Upload Completed OK")

	// create download header and download file
	dh := uh.DownloadHeader()

	tmpDst, err := os.CreateTemp("", "")
	require.NoError(err)
	dl, err := bs.NewDownload(dh, tmpDst.Name())
	t.Logf("Made NewDownload")
	require.NoError(err)

	dlr, err := NewDownloader(bs, dl)
	require.NoError(err)
	t.Logf("Made NewDownloader")
	dlr.StartWithTransport(transport)
	t.Logf("Started NewDownloader with MockTransport")
	<-time.After(4 * time.Second)
	dlr.Halt()
	t.Logf("Halted Downloader")

	// read the output and input and compare
	t2, err := os.ReadFile(tmpDst.Name())
	require.NoError(err)
	t1, err := os.ReadFile(tmp.Name())
	require.NoError(err)
	require.Equal(t2, t1)
}

func TestPutGetStartStream(t *testing.T) {
	require := require.New(t)

	bs := badgerStore(t)
	require.NoError(bs.InitDB())

	// create a stream
	local, remote := newStreams()

	// save it in db
	id := uint64(42)
	err := bs.PutStream(id, &stream.BufferedStream{Stream: local})
	require.NoError(err)

	// get stream from db
	buf, err := bs.GetStream(id)
	require.NoError(err)
	dblocal := buf.Stream

	require.Equal(dblocal.Context, local.Context)

	// compare readcap
	a, _ := dblocal.ReadCap.MarshalBinary()
	b, _ := local.ReadCap.MarshalBinary()
	require.Equal(a, b)

	// compare writecap
	a, _ = dblocal.WriteCap.MarshalBinary()
	b, _ = local.WriteCap.MarshalBinary()
	require.Equal(a, b)

	// initialize a scratch client with session and make it the transport
	mockTransport := NewMockTransport()
	require.NoError(err)
	dblocal.SetTransport(mockTransport)
	dblocal.Start()

	remote.SetTransport(mockTransport)
	remote.Start()
	payload := make([]byte, 4200)
	io.ReadFull(rand.Reader, payload)
	dblocal.Write(payload)
	recv := make([]byte, len(payload))
	n, err := io.ReadFull(remote, recv)
	require.Equal(n, len(recv))
	require.NoError(err)
}

func newStreamsFromExchange() (*stream.Stream, *stream.Stream) {
	r1, _ := NewReadCapExchange()
	r2, _ := NewReadCapExchange()
	r1Ex, _ := r1.ExchangeBytes()
	r2Ex, _ := r2.ExchangeBytes()
	rcem1 := &ReadCapExchangeMessage{}
	rcem2 := &ReadCapExchangeMessage{}

	// complete exchange for r1
	rcem1.UnmarshalBinary(r2Ex)
	r1ownerCap, r2readCap, _ := r1.CompleteExchange(rcem1)

	// complete exchange for r2
	rcem2.UnmarshalBinary(r1Ex)
	r2ownerCap, r1readCap, _ := r2.CompleteExchange(rcem2)

	streamCtx := []byte("failure")
	streamCtx = make([]byte, 32)
	// initialize streams
	a := stream.NewStream(r1ownerCap, r2readCap, streamCtx[:])
	b := stream.NewStream(r2ownerCap, r1readCap, streamCtx[:])
	return a, b
}

// newStreams returns an initialized pair of Streams without transport
func newStreams() (*stream.Stream, *stream.Stream) {
	// create a pair of Streams that correspond to each end
	w1, err := bacap.NewBoxOwnerCap(rand.Reader)
	if err != nil {
		panic(err)
	}

	w2, err := bacap.NewBoxOwnerCap(rand.Reader)
	if err != nil {
		panic(err)
	}

	r1 := w1.UniversalReadCap()
	r2 := w2.UniversalReadCap()

	streamCtx := [32]byte{}
	_, err = io.ReadFull(rand.Reader, streamCtx[:])
	if err != nil {
		panic(err)
	}

	a := stream.NewStream(w1, r2, streamCtx[:])
	b := stream.NewStream(w2, r1, streamCtx[:])
	return a, b
}

type message struct {
	payload   []byte
	signature [64]byte
}

type mockTransport struct {
	l    *sync.Mutex
	data map[[32]byte]message
}

func NewMockTransport() stream.Transport {
	m := &mockTransport{}
	m.l = new(sync.Mutex)
	m.l.Lock()
	defer m.l.Unlock()
	m.data = make(map[[32]byte]message)
	return m
}

func (m mockTransport) Put(ctx context.Context, addr [32]byte, sig [64]byte, payload []byte) error {
	m.l.Lock()
	defer m.l.Unlock()
	var boxPk ed25519.PublicKey
	if err := boxPk.FromBytes(addr[:]); err != nil {
		return err
	}
	if ok := boxPk.Verify(sig[:], payload); !ok {
		return errors.New("failed to verify")
	}

	m.data[addr] = message{signature: sig, payload: payload}
	return nil
}

func (m mockTransport) Get(ctx context.Context, addr [32]byte) ([]byte, [64]byte, error) {
	m.l.Lock()
	d, ok := m.data[addr]
	m.l.Unlock()
	if !ok {
		return nil, [64]byte{}, errors.New("NotFound")
	}
	return d.payload, d.signature, nil
}

func (m mockTransport) PayloadSize() int {
	return 1024
}

func init() {
	go func() {
		http.ListenAndServe("localhost:1234", nil)
	}()
	runtime.SetMutexProfileFraction(1)
	runtime.SetBlockProfileRate(1)
}
