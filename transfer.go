package main

import (
	"encoding/base64"
	"gioui.org/io/key"
	"gioui.org/layout"
	"github.com/katzenpost/hpqc/bacap"
	"github.com/katzenpost/hpqc/sign/ed25519"
	sClient "github.com/katzenpost/katzenpost/scratch/client"
	"golang.org/x/exp/shiny/materialdesign/icons"

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
	ID     uint64
	Header *UploadHeader
	State  *TransferState
	Path   string
}

// Download holds the State of a Transfer of the content in Header
type Download struct {
	ID     uint64
	Header *DownloadHeader
	State  *TransferState
	Path   string
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
	switch path := item.(type) {
	case string:
		uh, err := NewUploadHeader(path)
		if err != nil {
			return
		}
		ul, err := t.a.db.NewUpload(uh, path)
		if err != nil {
			return
		}
		dl, err := NewUploader(t.a.db, ul)
		if err == nil {
			t.uploads = append(t.uploads, dl)
		}
	}
}

func newTransferPage(a *App) *TransferPage {
	ulIDs := a.db.GetUploadIDs()
	dlIDs := a.db.GetDownloadIDs()
	uploads := make([]*Uploader, 0, len(ulIDs))
	downloads := make([]*Downloader, 0, len(dlIDs))

	for _, ulID := range ulIDs {
		if ul, err := a.db.GetUpload(ulID); err == nil {
			if ulr, err := NewUploader(a.db, ul); err == nil {
				uploads = append(uploads, ulr)
			}
		}
	}
	for _, dlID := range dlIDs {
		if dl, err := a.db.GetDownload(dlID); err == nil {
			if dlr, err := NewDownloader(a.db, dl); err == nil {
				downloads = append(downloads, dlr)
			}
		}
	}

	t := &TransferPage{a: a,
		uploads:   uploads,
		downloads: downloads,
		add:       &widget.Clickable{},
		back:      &widget.Clickable{},
		updateCh:  make(chan struct{}, 1),
	}
	return t
}

// Layout the view for a Downloader
func (d *Downloader) Layout(gtx layout.Context) layout.Dimensions {
	progress := float32(d.Download.State.Length) / float32(d.Download.Header.Length)
	return layout.Flex{Axis: layout.Vertical, Spacing: layout.SpaceBetween, Alignment: layout.Middle}.Layout(gtx,
		layout.Rigid(material.Caption(th, d.Download.Header.Name).Layout),
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
	progress := float32(u.Upload.State.Length) / float32(u.Upload.Header.Length)
	return layout.Flex{Axis: layout.Vertical, Spacing: layout.SpaceBetween, Alignment: layout.Middle}.Layout(gtx,
		layout.Rigid(material.Caption(th, u.Upload.Header.Name).Layout),
		layout.Rigid(material.Caption(th, base64.StdEncoding.EncodeToString(u.Upload.Header.Sum256)).Layout),
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
			transport, err := sClient.NewClient(t.a.Session())
			if err != nil {
				return TransferFailure{Error: err}
			}
			ul.StartWithTransport(transport)
			return TransferStarted{}
		}
		if ul.cancelBtn.Clicked(gtx) {
			ul.Halt()
			return TransferCancelled{}
		}
		if ul.deleteBtn.Clicked(gtx) {
			ul.Halt()
			t.uploads = append(t.uploads[:i], t.uploads[i+1:]...)
			err := t.a.db.RemoveUpload(ul.Upload.ID)
			if err != nil {
				return TransferFailure{Error: err}
			}
			return TransferRemoved{}
		}
	}
	// check if any downloader buttons are clicked
	for i, dl := range t.downloads {
		if dl.startBtn.Clicked(gtx) {
			transport, err := sClient.NewClient(t.a.Session())
			if err != nil {
				return TransferFailure{Error: err}
			}
			dl.StartWithTransport(transport)
			return TransferStarted{}
		}

		if dl.cancelBtn.Clicked(gtx) {
			dl.Halt()
			return TransferCancelled{}
		}
		if dl.deleteBtn.Clicked(gtx) {
			dl.Halt()
			t.downloads = append(t.downloads[:i], t.downloads[i+1:]...)
			err := t.a.db.RemoveDownload(dl.Download.ID)
			if err != nil {
				return TransferFailure{Error: err}
			}
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
