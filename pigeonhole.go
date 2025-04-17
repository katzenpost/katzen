package main

import (
	"context"
	"fmt"
	"gioui.org/layout"
	"github.com/katzenpost/hpqc/bacap"
	"github.com/katzenpost/hpqc/rand"
	"github.com/katzenpost/katzenpost/client"
	"github.com/katzenpost/katzenpost/core/worker"
	mClient "github.com/katzenpost/katzenpost/pigeonhole/client"
	"golang.org/x/crypto/blake2b"
	"io"
	"os"
	"sync"
	"time"

	//"gioui.org/x/explorer"
	"gioui.org/widget"
)

// Downloader
type Downloader struct {
	worker.Worker
	transport mClient.ReadWriteClient
	db        *BadgerStore

	// State is the State of the Downloader
	State DownloadState

	// Header contains metadata Name, Sum256, Size
	Header *DownloadHeader

	// Sum256 is the current hash.Hash.Sum256 of the bytes received
	Sum256 []byte

	// io.Writer is where received bytes get written
	dest io.Writer

	// each upload or download consists of a number of chunks
	// which are stored with a uint64 key in a db
	Chunks []uint64

	// Number of read Chunks
	BytesRead uint64

	// We need a structure to hold a reference to each payload response
	// that we received from pigeonhole in our database
	// so that

	// UI buttons associated with each Downloader
	startBtn  *widget.Clickable
	cancelBtn *widget.Clickable
	deleteBtn *widget.Clickable

	cmdCh chan Command
}

// NewUploadHeader returns an UploadHeader created for the File
func NewUploadHeader(path string) (*UploadHeader, error) {
	fileInfo, err := os.Stat(path)
	if err != nil {
		return nil, err
	}
	ownerCap, err := bacap.NewBoxOwnerCap(rand.Reader)
	if err != nil {
		return nil, err
	}
	if fileInfo.Size() <= 0 {
		return nil, fmt.Errorf("Empty or System file: %s", fileInfo.Name())
	}
	sum, err := sum256file(path)
	if err != nil {
		return nil, err
	}
	header := &UploadHeader{WriteCap: ownerCap,
		Name:   fileInfo.Name(),
		Length: uint64(fileInfo.Size()),
		Sum256: sum,
	}

	return header, nil
}

// NewDownloader returns a new Downloader with the
func NewUploader(transport *mClient.Client, db *BadgerStore, h *UploadHeader, source io.Reader) (*Uploader, error) {
	// XXX: set db
	ul := &Uploader{startOnce: new(sync.Once), Header: h, db: db, source: source, startBtn: new(widget.Clickable), cancelBtn: new(widget.Clickable), deleteBtn: new(widget.Clickable)}
	return ul, nil
}

func NewDownloader(session *client.Session, h *DownloadHeader, dest io.Writer) *Downloader {
	dloader := &Downloader{State: Proposed,
		Header: h, dest: dest, cmdCh: make(chan Command),
		startBtn:  &widget.Clickable{},
		cancelBtn: &widget.Clickable{},
		deleteBtn: &widget.Clickable{},
	}
	return dloader
}

// bacapworker receives command to start/stop reading and storing bytes
func (d *Downloader) worker() {
	reader, err := bacap.NewStatefulReader(d.Header.ReadCap, d.Header.Sum256)
	if err != nil {
		panic(err)
	}

	for {
		select {
		case <-d.HaltCh():
			return
		case cmd := <-d.cmdCh:
			switch cmd {
			case Start:
			case Stop:
				return
			default:
			}
		default:

		}
		// is already read
		if d.BytesRead == d.Header.Length {
			// set state to complted
			return
		}
		boxID, err := reader.NextBoxID()
		if err != nil {
			return
		}
		ctx, cancelFn := context.WithTimeout(context.Background(), time.Minute)

		// fetch the box
		// XXX: FIXME: obtain signature along with payload fromt he client; requires api change
		sig := [64]byte{'f', 'm', 'l'}
		ciphertext /*sig,*/, err := d.transport.GetWithContext(ctx, boxID.Bytes())
		cancelFn()
		if err != nil {
			// retry
			continue
		}
		// obtain plaintext. a decryption failure is a fatal error
		plaintext, err := reader.DecryptNext(d.Header.Sum256, boxID.ByteArray(), ciphertext, sig)
		if err != nil {
			// XXX: log or handle this error
			return
		}

		// write bytes to dest. failure to write is a fatal error
		n, err := d.dest.Write(plaintext)
		if err != nil || n != len(plaintext) {
			// XXX: log or handle this error
			panic(err)
			return
		}

		// update how many bytes have been read
		d.BytesRead += uint64(len(plaintext))
	}
}

var (
	downloadList = &layout.List{Axis: layout.Vertical}
	uploadList   = &layout.List{Axis: layout.Vertical}
)

// DownloadState indicates the state of the Downloader
// it has state proposed, rejected, inprogress, completed
// a new Downlaod starts in state Proposed, and proceeds to Rejected or InProgress state
// on starting, and reaches state Completed or Failed
type DownloadState uint8

const (
	Proposed DownloadState = iota
	Rejected
	InProgress
	Completed
	Failed
)

type Uploader struct {
	startOnce *sync.Once
	worker.Worker

	Header    *UploadHeader
	db        *BadgerStore
	source    io.Reader
	transport mClient.ReadWriteClient

	// Total bytes written
	BytesWritten uint64

	// Chunk IDs of the Chunks created
	Chunks []uint64

	startBtn  *widget.Clickable
	cancelBtn *widget.Clickable
	deleteBtn *widget.Clickable
}

func (u *Uploader) Start() {
	u.startOnce.Do(func() {
		u.Go(u.worker)
		<-u.HaltCh()
		u.startOnce = new(sync.Once)
	})
}

func (u *Uploader) worker() {
	writer, err := bacap.NewStatefulWriter(u.Header.WriteCap, u.Header.Sum256)
	if err != nil {
		panic(err)
	}
	payloadSize := u.transport.PayloadSize()

	for u.BytesWritten < u.Header.Length {
		buf := make([]byte, payloadSize)
		n, err := io.ReadFull(u.source, buf)
		switch err {
		case nil, io.ErrUnexpectedEOF:
			// XXX: does EncryptNext do padding?
			box_id, ciphertext, sig, err := writer.EncryptNext(buf[:n])
			if err != nil {
				panic(err)
			}
			// create and save the Chunk locally
			ch := &Chunk{ID: rand.NewMath().Uint64(), Key: box_id, Payload: ciphertext}
			copy(ch.Signature[:], sig)
			err = u.db.PutChunk(ch)
			if err != nil {
				panic(err)
			}
			u.Chunks = append(u.Chunks, ch.ID)
			u.BytesWritten += uint64(len(ciphertext))

			ctx, cancelFn := context.WithTimeout(context.Background(), time.Minute)
			//err = u.transport.PutWithContext(ctx, box_id[:], sig[:], ciphertext)
			err = u.transport.PutWithContext(ctx, box_id[:], ciphertext)
			cancelFn()
			if err != nil {
				panic(err)
				// Chunk should have a flag on whether it has been uploaded
			}
		default:
			panic(err)
		}
	}
}

func sum256file(path string) ([]byte, error) {
	r, err := os.Open(path)
	if err != nil {
		return nil, err
	}

	h, err := blake2b.New256(nil)
	if err != nil {
		return nil, err
	}

	// Get the block size and make a buffer a multiple thereof
	blockSize := h.BlockSize()
	buf := make([]byte, blockSize*1024)

	// Read from the file and write to the hash
	for {
		n, err := r.Read(buf)
		if err != nil {
			if err == io.EOF {
				// Write the remaining bytes to the hash
				h.Write(buf[:n])
				break
			}
			return nil, err
		}

		// Write the buffer to the hash
		h.Write(buf[:n])
	}

	// Get the hash sum
	return h.Sum(nil), nil
}
