package main

import (
	"encoding/base64"
	"gioui.org/io/key"
	"gioui.org/layout"
	"github.com/katzenpost/hpqc/bacap"
	"github.com/katzenpost/hpqc/sign/ed25519"
	mClient "github.com/katzenpost/katzenpost/pigeonhole/client"
	"golang.org/x/exp/shiny/materialdesign/icons"
	"os"

	"gioui.org/widget"
	"gioui.org/widget/material"
)

var (
	startIcon, _  = widget.NewIcon(icons.AVPlayArrow)
	stopIcon, _   = widget.NewIcon(icons.NavigationCancel)
	deleteIcon, _ = widget.NewIcon(icons.ActionDelete)
)

// UploadHeader contains the metadata for uploading a file and producing a DownloadHeader
type UploadHeader struct {
	WriteCap *bacap.BoxOwnerCap
	Name     string
	Length   uint64
	Sum256   []byte
}

// DownloadHeader holds the Read capability and file metadata
type DownloadHeader struct {
	ReadCap *bacap.UniversalReadCap // the read capability
	Name    string                  // the name of the Payload
	Length  uint64                  // the length of the Payload
	Sum256  []byte                  // the hash of the Payload
}

// TransferState holds state needed to resume a transfer, the chunks sent or receive and the bytes so far
type TransferState struct {
	// Chunks is the ordered Chunk ids comprising the transfer
	Chunks []uint64

	// Number of bytes transferred in these chunks
	Length uint64
}

// DownloadHeader returns the Header used to start a download
func (u *UploadHeader) DownloadHeader() *DownloadHeader {
	d := new(DownloadHeader)
	d.ReadCap = u.WriteCap.UniversalReadCap()
	d.Name = u.Name
	d.Length = u.Length
	d.Sum256 = u.Sum256
	return d
}

// Upload holds the State of a Transfer of the content in Header
type Upload struct {
	ID uint64
	Header *UploadHeader
	State *TransferState
}

// Download holds the State of a Transfer of the content in Header
type Download struct {
	ID uint64
	Header *DownloadHeader
	State *TransferState
}

// Chunk holds each bacap message
type Chunk struct {
	ID        uint64
	Key       [ed25519.PublicKeySize]byte
	Signature [ed25519.SignatureSize]byte
	Payload   []byte
}

// TransferPage tracks the uploads and downloads in progress and provides UI to stop/start/cancel transfers
// It consists of the navbar and two lists, one for uploads and one for downloads.
// Each element of these lists present controls for (re) starting / stopping / removing a transfer
type TransferPage struct {
	a         *App
	downloads []*Downloader
	uploads   []*Uploader
	add       *widget.Clickable
	back      *widget.Clickable
	updateCh  chan struct{}
}

type TransferCancelled struct{}
type TransferCompleted struct{}
type TransferRemoved struct{}
type TransferStarted struct{}
type TransferFailure struct {
	Error error
}

func (t *TransferPage) Start(stop <-chan struct{}) {
	go func() {
		for {
			select {
			case <-stop:
				return
			case <-t.updateCh:
			}
		}
	}()
}

// Update is called by the Chooser with the path chosen
func (t *TransferPage) Update(item interface{}) {
	switch i := item.(type) {
	case string:
		uh, err := NewUploadHeader(i)
		if err != nil {
			return
		}
		f, err := os.Open(i)
		if err != nil {
			return
		}
		// XXX: requires a non nil session (ie, be online)
		// in order to create an Uploader, which is lame.
		transport, err := mClient.NewClient(t.a.Session())
		if err != nil {
			return
		}
		dl, err := NewUploader(transport, t.a.db, uh, f)
		if err != nil {
			return
		}
		t.uploads = append(t.uploads, dl)
	}
}

func newTransferPage(a *App) *TransferPage {
	t := &TransferPage{a: a,
		uploads:   []*Uploader{},
		downloads: []*Downloader{},
		add:       &widget.Clickable{},
		back:      &widget.Clickable{},
		updateCh:  make(chan struct{}, 1),
	}
	return t
}

// Layout the view for a Downloader
func (d *Downloader) Layout(gtx layout.Context) layout.Dimensions {
	progress := float32(d.BytesRead) / float32(d.Header.Length)
	return layout.Flex{Axis: layout.Vertical, Spacing: layout.SpaceBetween, Alignment: layout.Middle}.Layout(gtx,
		layout.Rigid(material.Caption(th, d.Header.Name).Layout),
		layout.Rigid(material.ProgressBar(th, progress).Layout),

		layout.Rigid(func(gtx C) D {
			return layout.Flex{Axis: layout.Horizontal, Spacing: layout.SpaceBetween, Alignment: layout.Middle}.Layout(
				gtx,
				layout.Rigid(button(th, d.startBtn, startIcon).Layout),
				layout.Rigid(button(th, d.cancelBtn, cancelIcon).Layout),
				layout.Rigid(button(th, d.deleteBtn, deleteIcon).Layout),
			)
		}),
	)
}

// Layout the view for a Uploader
// Shown is the file name and size, a progress bar, and start/stop
func (u *Uploader) Layout(gtx layout.Context) layout.Dimensions {
	progress := float32(u.BytesWritten) / float32(u.Header.Length)
	return layout.Flex{Axis: layout.Vertical, Spacing: layout.SpaceBetween, Alignment: layout.Middle}.Layout(gtx,
		layout.Rigid(material.Caption(th, u.Header.Name).Layout),
		layout.Rigid(material.Caption(th, base64.StdEncoding.EncodeToString(u.Header.Sum256)).Layout),
		layout.Rigid(material.ProgressBar(th, progress).Layout),
		layout.Rigid(func(gtx C) D {
			return layout.Flex{Axis: layout.Horizontal, Spacing: layout.SpaceBetween, Alignment: layout.Middle}.Layout(
				gtx,
				layout.Rigid(button(th, u.startBtn, startIcon).Layout),
				layout.Rigid(button(th, u.cancelBtn, cancelIcon).Layout),
				layout.Rigid(button(th, u.deleteBtn, deleteIcon).Layout),
			)
		}),
	)
}

// after the download is complete the buttons change to close
//
// each upload list item laid out has the file name and size, a progress bar, and cancel / repair / delete buttons
// clicking delete removes the attachment from the list
// Event checks to see if any of the UX elements have changed and does the corresponding actions
func (t *TransferPage) Event(gtx layout.Context) interface{} {
	if t.add.Clicked(gtx) {
		return NewChooser{}
	}

	if t.back.Clicked(gtx) {
		return BackEvent{}
	}

	// handle keyboard shortcuts
	if e, ok := shortcutEvents(gtx); ok {
		switch e.Name {
		case key.NameEscape:
			return BackEvent{}
		}

	}

	// check if uploader buttons are clicked and do the action
	for i, ul := range t.uploads {
		if ul.startBtn.Clicked(gtx) {
			ul.Start()
			return TransferStarted{}
		}
		if ul.cancelBtn.Clicked(gtx) {
			ul.Halt()
			return TransferCancelled{}
		}
		if ul.deleteBtn.Clicked(gtx) {
			ul.Halt()
			t.uploads = append(t.uploads[:i], t.uploads[i+1:]...)
			return TransferRemoved{}
		}
	}
	// check if any downloader buttons are clicked
	for i, dl := range t.downloads {
		if dl.startBtn.Clicked(gtx) {
			dl.Start()
			return TransferStarted{}
		}

		if dl.cancelBtn.Clicked(gtx) {
			dl.Halt()
			return TransferCancelled{}
		}
		if dl.deleteBtn.Clicked(gtx) {
			dl.Halt()
			t.downloads = append(t.downloads[:i], t.downloads[i+1:]...)
			return TransferRemoved{}
		}
	}
	return nil
}

func (t *TransferPage) Layout(gtx layout.Context) layout.Dimensions {
	return layout.Flex{Axis: layout.Vertical, Spacing: layout.SpaceBetween, Alignment: layout.Middle}.Layout(gtx,
		// layout back button, title
		layout.Rigid(func(gtx C) D {
			return bgList.Layout(gtx, func(gtx C) D {
				return layout.Flex{Axis: layout.Horizontal, Spacing: layout.SpaceBetween, Alignment: layout.Middle}.Layout(gtx,
					layout.Rigid(button(th, t.back, backIcon).Layout),
					layout.Rigid(material.Caption(th, "Transfer Page").Layout),
					layout.Flexed(1, fill{th.Bg}.Layout),
				)
			},
			)
		}),
		// layout upload list
		layout.Flexed(2, func(gtx C) D {
			return bgList.Layout(gtx, func(ctx C) D {
				// return empty if there are no messages
				if len(t.uploads) == 0 {
					return fill{th.Bg}.Layout(ctx)
				}
				return uploadList.Layout(gtx, len(t.uploads), func(gtx C, i int) D {
					return t.uploads[i].Layout(gtx)
				})
			})
		}),

		// layout download list
		layout.Flexed(2, func(gtx C) D {
			return bgList.Layout(gtx, func(ctx C) D {
				// return empty if there are no messages
				if len(t.downloads) == 0 {
					return fill{th.Bg}.Layout(ctx)
				}
				return downloadList.Layout(gtx, len(t.downloads), func(gtx C, i int) D {
					return t.downloads[i].Layout(gtx)
				})
			})
		}),
	)
}
