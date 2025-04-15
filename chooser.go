package main

import (
	"gioui.org/app"
	"gioui.org/gesture"
	"gioui.org/layout"
	"gioui.org/op/clip"
	"gioui.org/op/paint"
	"gioui.org/unit"
	"gioui.org/widget"
	"gioui.org/widget/material"
	"golang.org/x/image/draw"
	"image"
	_ "image/jpeg"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
)

var (
	dirList = &layout.List{Axis: layout.Vertical}
)

type Chooser struct {
	a       *App
	path    string
	back    *widget.Clickable
	up      *widget.Clickable
	entries []*ClickDirEntry
	opCh    chan *opMakeThumb
	tl      *sync.Mutex // mutex to protect modification to thumbs
	chosen  fs.File
}

func (c *Chooser) Chosen() fs.File {
	return c.chosen
}

// ClickDirEntry widget displays a thumbnail if possible or filename and has a gesture.Click associated
type ClickDirEntry struct {
	Path     string
	Click    *gesture.Click
	DirEntry fs.DirEntry
	Thumb    image.Image
	tl       *sync.Mutex // mutex to protect Thumb as it must update when the window is resized
}

// guess if a given filename will decode as png or jpeg
func isLikelyImage(name string) bool {
	s := strings.ToLower(name)
	for _, suffix := range []string{"jpg", "jpeg", "png"} {
		if strings.HasSuffix(s, suffix) {
			return true
		}
	}
	return false
}

func (d *ClickDirEntry) Layout(gtx layout.Context) layout.Dimensions {
	if isLikelyImage(d.DirEntry.Name()) {
		d.tl.Lock()
		defer d.tl.Unlock()
		var dims layout.Dimensions
		sx := gtx.Constraints.Max.X
		if d.Thumb == nil || d.Thumb.Bounds().Size().X != sx {
			// No thumbnail exists yet
			dims = layout.Dimensions{Size: image.Point{X: sx, Y: sx}}
		} else {
			th := widget.Image{Src: paint.NewImageOp(d.Thumb)}
			// render thumb and attach the click handlers
			dims = th.Layout(gtx)
		}
		//a := clip.Rect(image.Rectangle{Max: dims.Size})
		a := clip.Rect(image.Rectangle{Max: dims.Size})
		t := a.Push(gtx.Ops)
		d.Click.Add(gtx.Ops)
		t.Pop()
		return dims
	} else {
		in := layout.Inset{Top: unit.Dp(8), Bottom: unit.Dp(8), Left: unit.Dp(12), Right: unit.Dp(12)}
		dims := in.Layout(gtx, func(gtx C) D {
			return material.Body1(th, d.DirEntry.Name()).Layout(gtx)
		})
		a := clip.Rect(image.Rectangle{Max: dims.Size})
		t := a.Push(gtx.Ops)
		d.Click.Add(gtx.Ops)
		t.Pop()
		return dims
	}
}

// Layout displays a file chooser
func (p *Chooser) Layout(gtx layout.Context) layout.Dimensions {
	return bg.Layout(gtx, func(gtx C) D {
		return layout.Flex{Axis: layout.Vertical, Spacing: layout.SpaceBetween, Alignment: layout.Middle}.Layout(gtx,
			// back button and title
			layout.Rigid(func(gtx C) D {
				return layout.Flex{Axis: layout.Horizontal, Alignment: layout.Baseline}.Layout(gtx,
					layout.Rigid(button(th, p.back, backIcon).Layout),
					layout.Flexed(1, fill{th.Bg}.Layout),
					layout.Rigid(material.H6(th, "Choose File").Layout),
					layout.Flexed(1, fill{th.Bg}.Layout),
				)
			}),
			// cwd and buttons
			layout.Rigid(func(gtx C) D {
				return layout.Flex{Axis: layout.Horizontal, Alignment: layout.Baseline}.Layout(gtx,
					layout.Rigid(material.Button(th, p.up, "..").Layout),
					layout.Flexed(1, material.Body1(th, p.path).Layout),
				)
			}),
			// list contents
			layout.Flexed(1, func(gtx C) D {
				// file item layout
				sz := gtx.Constraints.Max.X
				//if dirList.Dragging() Up vs Down ?
				size := dirList.Position.Count * 3
				first := dirList.Position.First
				last := first + size

				if first-size < 0 {
					first = 0
				} else {
					first = first - size
				}
				if last > len(p.entries) {
					last = len(p.entries)
				}
				// schedule thumbnail jobs to workers outside of render loop
				go func() {
					for i := first; i < last; i++ {
						f := p.entries[i]
						if f.DirEntry.IsDir() {
							continue
						}
						f.tl.Lock()
						doMakeThumb := (isLikelyImage(f.DirEntry.Name()) && (f.Thumb == nil || f.Thumb.Bounds().Size().X != sz))
						f.tl.Unlock()
						if doMakeThumb {
							p.opCh <- &opMakeThumb{entry: p.entries[i], size: sz}
						}
					}
				}()

				return dirList.Layout(gtx, len(p.entries), func(gtx C, i int) D {
					return p.entries[i].Layout(gtx)
				})
			}),
		)
	})
}

func (p *Chooser) Event(gtx C) interface{} {
	if p.back.Clicked(gtx) {
		return BackEvent{}
	}

	for _, clickable := range p.entries {
		if e, ok := clickable.Click.Update(gtx.Source); ok {
			if e.Kind == gesture.KindClick {
				if u, err := filepath.Abs(filepath.Join(p.path, clickable.DirEntry.Name())); err == nil {
					if clickable.DirEntry.IsDir() {
						return ChooserChoseDir{Path: u}
					}

					if _, err := os.Stat(u); err == nil {
						return ChooserChoseFile{Path: u}
					}
				}
			}
		}
	}
	return nil
}

type NewChooser struct {
	Path string
}

type ChooserChoseDir struct {
	Path string
}

type ChooserChoseFile struct {
	Path string
}

type opMakeThumb struct {
	entry *ClickDirEntry
	size  int
}

func (p *Chooser) Start(stop <-chan struct{}) {
	// start the thumbnail workers
	n := runtime.NumCPU()
	p.opCh = make(chan *opMakeThumb, 2*n)
	for i := 0; i < n; i++ {
		go func() {
			for {
				select {
				case o := <-p.opCh:
					th := makeThumb(o.entry, o.size)
					o.entry.tl.Lock()
					o.entry.Thumb = th
					o.entry.tl.Unlock()
				case <-stop:
					return
				}
			}
		}()
	}
}

func (c *Chooser) Update(item interface{}) {
	f, ok := item.(fs.File)
	if ok {
		c.chosen = f
	}
}

func makeThumb(d *ClickDirEntry, sz int) image.Image {
	f, err := os.Open(filepath.Join(d.Path, d.DirEntry.Name()))
	if err != nil {
		return nil
	}
	m, _, err := image.Decode(f)
	if err == nil {
		sx, sy := m.Bounds().Max.X, m.Bounds().Max.Y
		aspect := float32(sy) / float32(sx)
		rz := image.Rectangle{Max: image.Point{X: sz, Y: int(float32(sz) * aspect)}}
		return scale(m, rz, draw.NearestNeighbor)
	}
	return nil
}

func scan(path string) []*ClickDirEntry {
	// get contents of directory at cwd
	dirEntries, err := os.ReadDir(path)
	if err != nil {
		return nil
	}

	paths := make([]*ClickDirEntry, len(dirEntries))
	for i, de := range dirEntries {
		paths[i] = &ClickDirEntry{Path: path, DirEntry: de, Click: new(gesture.Click), tl: new(sync.Mutex)}
	}

	newpath := filepath.Join(path, "/..")
	fi, _ := os.Stat(newpath)
	updir := &ClickDirEntry{Path: path, DirEntry: fs.FileInfoToDirEntry(fi), Click: new(gesture.Click), tl: new(sync.Mutex)}
	return append([]*ClickDirEntry{updir}, paths...)
}

func newChooser(a *App, path string) *Chooser {
	if path == "" {
		path, _ = app.DataDir()
		if runtime.GOOS == "android" {
			path = "/sdcard/"
		}
	}

	ap := &Chooser{up: &widget.Clickable{},
		a:       a,
		back:    &widget.Clickable{},
		entries: scan(path),
		path:    path}
	return ap
}
