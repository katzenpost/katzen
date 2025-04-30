package main

import (
	"context"
	"github.com/katzenpost/hpqc/bacap"
	"github.com/katzenpost/hpqc/rand"
	"github.com/katzenpost/katzenpost/core/worker"
	"golang.org/x/crypto/blake2b"
	"io"
	"os"
	"sync"
	"time"

	//"gioui.org/x/explorer"
	"gioui.org/widget"
)

// Transport describes the interface to Get or Put Frames
type Transport interface {
	Get(ctx context.Context, addr [32]byte) (ciphertext []byte, signature [64]byte, err error)
	Put(ctx context.Context, addr [32]byte, signature [64]byte, payload []byte) error
	PayloadSize() int
}

// Downloader
type Downloader struct {
	worker.Worker
	startOnce *sync.Once
	transport Transport
	db        *BadgerStore

	// Download holds the TransferState and DownloadHeader
	Download *Download

	// io.Writer is where received bytes get written
	dest io.Writer

	// UI buttons associated with each Downloader
	startBtn  *widget.Clickable
	shareBtn  *widget.Clickable
	cancelBtn *widget.Clickable
	deleteBtn *widget.Clickable
}

// NewDownloader initializes and returns a Downloader
func NewDownloader(db *BadgerStore, dl *Download) (*Downloader, error) {
	dest, err := os.OpenFile(dl.Path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0644)
	if err != nil {
		return nil, err
	}
	dloader := &Downloader{Download: dl,
		startOnce: new(sync.Once),
		dest:      dest,
		db:        db,
		startBtn:  &widget.Clickable{},
		shareBtn:  &widget.Clickable{},
		cancelBtn: &widget.Clickable{},
		deleteBtn: &widget.Clickable{},
	}
	return dloader, nil
}

func (d *Downloader) StartWithTransport(t Transport) {
	d.transport = t
	d.Start()
}

func (d *Downloader) Start() {
	if d.transport == nil {
		panic("no transport")
	}
	d.startOnce.Do(func() {
		d.Go(d.worker)
		d.Go(func() {
			<-d.HaltCh()
			d.startOnce = new(sync.Once)
		})
	})
}

// bacapworker receives command to start/stop reading and storing bytes
func (d *Downloader) worker() {
	reader, err := bacap.NewStatefulReader(d.Download.Header.ReadCap, d.Download.Header.Sum256)
	if err != nil {
		panic(err)
	}

	for d.Download.State.Length < d.Download.Header.Length {
		// is failed
		if d.Download.State.Status == Failed {
			return
		}
		if d.Download.State.Length == d.Download.Header.Length {
			d.Download.State.Status = Completed
			return
		}
		boxID, err := reader.NextBoxID()
		if err != nil {
			d.Download.State.Status = Failed
			return
		}
		ctx, cancelFn := context.WithTimeout(context.Background(), time.Minute)
		d.Go(func() {
			select {
			case <-ctx.Done():
			case <-d.HaltCh():
				cancelFn()
			}
		})

		// fetch the box
		ciphertext, sig, err := d.transport.Get(ctx, boxID.ByteArray())
		cancelFn()
		if err != nil {
			// retry
			continue
		}
		// obtain plaintext. a decryption failure is a fatal error
		plaintext, err := reader.DecryptNext(d.Download.Header.Sum256, boxID.ByteArray(), ciphertext, sig)
		if err != nil {
			d.Download.State.Status = Failed
			return
		}

		// write bytes to dest. failure to write is a fatal error
		n, err := d.dest.Write(plaintext)
		if err != nil || n != len(plaintext) {
			d.Download.State.Status = Failed
			return
		}

		// update how many bytes have been read
		d.Download.State.Length += uint64(n)
	}
}

// Uploader manages sending an attachment to scratch service
type Uploader struct {
	startOnce *sync.Once
	worker.Worker
	Upload *Upload

	db        *BadgerStore
	source    io.ReadSeeker
	transport Transport

	startBtn  *widget.Clickable
	shareBtn  *widget.Clickable
	cancelBtn *widget.Clickable
	deleteBtn *widget.Clickable
}

// NewUploader returns an Uploader with db and Upload reference
func NewUploader(db *BadgerStore, ul *Upload) (*Uploader, error) {
	source, err := os.Open(ul.Path)
	if err != nil {
		return nil, err
	}
	uploader := &Uploader{Upload: ul,
		db:        db,
		source:    source,
		startOnce: new(sync.Once),
		startBtn:  &widget.Clickable{},
		shareBtn:  &widget.Clickable{},
		cancelBtn: &widget.Clickable{},
		deleteBtn: &widget.Clickable{},
	}
	return uploader, nil
}

func (u *Uploader) StartWithTransport(t Transport) {
	u.transport = t
	u.Start()
}

func (u *Uploader) Start() {
	if u.transport == nil {
		panic("no transport")
	}
	u.startOnce.Do(func() {
		u.Go(u.worker)
		u.Go(func() {
			<-u.HaltCh()
			u.startOnce = new(sync.Once)
		})
	})
}

func (u *Uploader) worker() {
	// advance the writeCap by the previous state
	numChunks := uint64(len(u.Upload.State.Chunks))
	writeCap, err := AdvanceOwnerCapBy(u.Upload.Header.WriteCap, numChunks)
	if err != nil {
		panic(err)
	}
	writer, err := bacap.NewStatefulWriter(writeCap, u.Upload.Header.Sum256)
	if err != nil {
		panic(err)
	}
	payloadSize := u.transport.PayloadSize()

	buf := make([]byte, payloadSize)
	for u.Upload.State.Length < u.Upload.Header.Length {
		// seek reader to current upload offset
		_, err := u.source.Seek(int64(u.Upload.State.Length), 0)
		if err != nil {
			panic(err)
		}
		// read one payload of data
		n, err := io.ReadFull(u.source, buf)
		switch err {
		case nil, io.ErrUnexpectedEOF:
			// payloadLen is transported bytes not including ciphertext overhead
			payloadLen := len(buf[:n])
			box_id, ciphertext, sig, err := writer.EncryptNext(buf[:n])
			if err != nil {
				panic(err)
			}
			// create Chunk
			ch := &Chunk{ID: rand.NewMath().Uint64(), Key: box_id, Payload: ciphertext}
			copy(ch.Signature[:], sig)

			// watch for cancellation signal
			ctx, cancelFn := context.WithTimeout(context.Background(), time.Minute)
			u.Go(func() {
				select {
				case <-ctx.Done():
				case <-u.HaltCh():
				}
				cancelFn()
			})
			var sig64 [64]byte
			copy(sig64[:], sig)
			err = u.transport.Put(ctx, box_id, sig64, ciphertext)
			if err != nil {
				return
			}

			// save state of upload if chunk was successfully uploaded
			err = u.db.PutChunk(ch)
			if err != nil {
				panic(err)
			}
			u.Upload.State.Chunks = append(u.Upload.State.Chunks, ch.ID)
			u.Upload.State.Length += uint64(payloadLen)

			// save the state of uploader
			err = u.db.PutUpload(u.Upload)
			if err != nil {
				panic(err)
			}
		default:
			panic(err)
		}
	}
	u.Upload.State.Status = Completed
	err = u.db.PutUpload(u.Upload)
	if err != nil {
		panic(err)
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
