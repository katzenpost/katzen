package main

import (
	"bytes"
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
	"image/png"
	"io/ioutil"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
)

var (
	avatarPickerList = &layout.List{Axis: layout.Vertical}
	maxCacheSize     = 16 // XXX: set from platform limits?
)

type AvatarPicker struct {
	a        *App
	avatar   *gesture.Click
	nickname string
	path     string
	back     *widget.Clickable
	clear    *widget.Clickable
	up       *widget.Clickable
	clicks   map[string]*gesture.Click
	thumbs   map[os.FileInfo]*image.Image
	files    []os.FileInfo
	thsz     int
	opCh     chan *opThumb
	running  bool
	tl       *sync.Mutex
}

// Layout displays a file chooser for supported image types
func (p *AvatarPicker) Layout(gtx layout.Context) layout.Dimensions {
	bg := Background{
		Color: th.Bg,
		Inset: layout.Inset{},
	}
	return bg.Layout(gtx, func(gtx C) D {
		return layout.Flex{Axis: layout.Vertical, Spacing: layout.SpaceBetween, Alignment: layout.Middle}.Layout(gtx,
			// back to Edit Contact
			layout.Rigid(func(gtx C) D {
				return layout.Flex{Axis: layout.Horizontal, Alignment: layout.Baseline}.Layout(gtx,
					layout.Rigid(button(th, p.back, backIcon).Layout),
					layout.Flexed(1, fill{th.Bg}.Layout),
					layout.Rigid(material.H6(th, "Choose Avatar").Layout),
					layout.Flexed(1, fill{th.Bg}.Layout),
				)
			}),
			// avatar icon
			layout.Rigid(func(gtx C) D {
				dims := layout.Center.Layout(gtx, func(gtx C) D {
					return layoutAvatar(gtx, p.a.c, p.nickname)
				})
				a := clip.Rect(image.Rectangle{Max: dims.Size})
				t := a.Push(gtx.Ops)
				p.avatar.Add(gtx.Ops)
				t.Pop()
				return dims
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
				p.tl.Lock()
				if p.thsz != sz {
					p.thumbs = make(map[os.FileInfo]*image.Image)
					p.thsz = sz
				}
				p.tl.Unlock()
				//if avatarPickerList.Dragging() Up vs Down ?
				size := avatarPickerList.Position.Count * 3
				first := avatarPickerList.Position.First
				last := first + size

				if first-size < 0 {
					first = 0
				} else {
					first = first - size
				}
				if last > len(p.files) {
					last = len(p.files)
				}
				// schedule thumbnail jobs to workers outside of render loop
				go func() {
					// we'd rather that the same files do not get tasked more than once,
					// so this routine should not be scheduled again before it has completed
					p.tl.Lock()
					if p.running {
						p.tl.Unlock()
						return
					}
					p.running = true
					p.tl.Unlock()

					// prune the cache when it gets too large
					if len(p.thumbs) > maxCacheSize {
						p.tl.Lock()
						old := p.thumbs
						p.thumbs = make(map[os.FileInfo]*image.Image)
						// copy pointers to the entries we want to keep in cache
						for i := first; i < last; i++ {
							th, ok := old[p.files[i]]
							if ok {
								p.thumbs[p.files[i]] = th
							}
						}
						p.tl.Unlock()
					}

					for i := first; i < last; i++ {
						if p.files[i].IsDir() {
							continue
						}
						p.tl.Lock()
						_, ok := p.thumbs[p.files[i]]
						p.tl.Unlock()
						if !ok {
							p.opCh <- &opThumb{f: p.files[i], size: sz}
						}
					}
					p.tl.Lock()
					p.running = false
					p.tl.Unlock()
				}()

				return avatarPickerList.Layout(gtx, len(p.files), func(gtx C, i int) D {
					fn := p.files[i]
					if fn.IsDir() {
						// is a directory, attach clickable that will update the path if clicked...
						if _, ok := p.clicks[fn.Name()]; !ok {
							c := new(gesture.Click)
							p.clicks[fn.Name()] = c
						}
						in := layout.Inset{Top: unit.Dp(8), Bottom: unit.Dp(8), Left: unit.Dp(12), Right: unit.Dp(12)}
						dims := in.Layout(gtx, func(gtx C) D {
							return material.Body1(th, fn.Name()).Layout(gtx)
						})
						a := clip.Rect(image.Rectangle{Max: dims.Size})
						t := a.Push(gtx.Ops)
						p.clicks[fn.Name()].Add(gtx.Ops)
						t.Pop()
						return dims
					} else {
						p.tl.Lock()
						resized, ok := p.thumbs[fn]
						p.tl.Unlock()
						if !ok {
							// skip element
							return layout.Dimensions{Size: image.Point{X: sz, Y: sz}}
						}
						t := func(ctx C) D {
							sc := float32(sz) / float32(gtx.Dp(unit.Dp(float32(sz))))
							th := widget.Image{Scale: sc, Src: paint.NewImageOp(*resized)}
							// render thumb and attach the click handlers
							in := layout.Inset{Top: unit.Dp(4), Bottom: unit.Dp(4)}
							dims := in.Layout(gtx, func(gtx C) D {
								return th.Layout(gtx)
							})
							a := clip.Rect(image.Rectangle{Max: dims.Size})
							t := a.Push(gtx.Ops)
							if _, ok := p.clicks[fn.Name()]; !ok {
								c := new(gesture.Click)
								p.clicks[fn.Name()] = c
							}
							p.clicks[fn.Name()].Add(gtx.Ops)
							t.Pop()
							return dims
						}
						return t(gtx)
					}
					return layout.Dimensions{}
				})
			}),
		)
	})
}

func (p *AvatarPicker) Event(gtx C) interface{} {
	if p.up.Clicked(gtx) {
		if u, err := filepath.Abs(filepath.Join(p.path, "..")); err == nil {
			return ChooseAvatarPath{nickname: p.nickname, path: u}
		}
	}
	if p.back.Clicked(gtx) {
		return BackEvent{}
	}

	if e, ok := p.avatar.Update(gtx.Source); ok {
		if e.Kind == gesture.KindClick {
			ct := Contactal{}
			ct.Reset()
			sz := image.Point{X: gtx.Dp(96), Y: gtx.Dp(96)}
			i := ct.Render(sz)
			b := new(bytes.Buffer)
			if err := png.Encode(b, i); err == nil {
				p.a.c.AddBlob("avatar://"+p.nickname, b.Bytes())
				delete(avatars, p.nickname)
				return RedrawEvent{}
			}
		}
	}

	for filename, click := range p.clicks {
		if e, ok := click.Update(gtx.Source); ok {
			if e.Kind == gesture.KindClick {
				// if it is a directory path - change the path
				// if it is a file path, return the file selection event
				if u, err := filepath.Abs(filepath.Join(p.path, filename)); err == nil {
					if f, err := os.Stat(u); err == nil {
						if f.IsDir() {
							return ChooseAvatarPath{nickname: p.nickname, path: u}
							p.path = u
						} else {
							p.a.setAvatar(p.nickname, u)
						}
					}
				}
			}
		}
	}
	return nil
}

type ChooseAvatarPath struct {
	nickname string
	path     string
}

type opThumb struct {
	f    os.FileInfo
	size int
}

func (p *AvatarPicker) Start(stop <-chan struct{}) {
	// start the thumbnail workers
	n := runtime.NumCPU()
	p.opCh = make(chan *opThumb, n)
	for i := 0; i < n; i++ {
		go func() {
			for {
				select {
				case o := <-p.opCh:
					p.makeThumb(o.f, o.size)
				case <-stop:
					return
				}
			}
		}()
	}
}

func (p *AvatarPicker) makeThumb(fn os.FileInfo, sz int) {
	f, err := os.Open(filepath.Join(p.path, fn.Name()))
	if err != nil {
		return
	}
	if m, _, err := image.Decode(f); err == nil {
		sx, sy := m.Bounds().Max.X, m.Bounds().Max.Y
		aspect := float32(sy) / float32(sx)
		rz := image.Rectangle{Max: image.Point{X: sz, Y: int(float32(sz) * aspect)}}
		resized := scale(m, rz, draw.NearestNeighbor)
		p.tl.Lock()
		p.thumbs[fn] = &resized
		p.tl.Unlock()
		p.a.w.Invalidate()
	}
}

func (p *AvatarPicker) scan() {
	// get contents of directory at cwd
	files, err := ioutil.ReadDir(p.path)
	if err != nil {
		return
	}

	ff := make([]os.FileInfo, 0, len(files))
	// filter non image filesnames and hidden directories
	for _, fn := range files {
		n := strings.ToLower(fn.Name())
		// skip .paths
		if strings.HasPrefix(n, ".") {
			continue
		}
		if fn.IsDir() || strings.HasSuffix(n, ".jpg") || strings.HasSuffix(n, ".png") || strings.HasSuffix(n, ".jpeg") {
			ff = append(ff, fn)
		}
	}
	p.tl.Lock()
	p.files = ff
	p.thumbs = make(map[os.FileInfo]*image.Image)
	p.tl.Unlock()
}

func newAvatarPicker(a *App, nickname string, path string) *AvatarPicker {
	if path == "" {
		path, _ = app.DataDir()
		if runtime.GOOS == "android" {
			path = "/sdcard/"
		}
	}

	ap := &AvatarPicker{up: &widget.Clickable{},
		a:        a,
		avatar:   &gesture.Click{},
		nickname: nickname,
		back:     &widget.Clickable{},
		clear:    &widget.Clickable{},
		clicks:   make(map[string]*gesture.Click),
		thumbs:   make(map[os.FileInfo]*image.Image),
		files:    make([]os.FileInfo, 0),
		tl:       new(sync.Mutex),
		path:     path}
	ap.scan()
	return ap
}

func scale(src image.Image, rect image.Rectangle, scale draw.Scaler) image.Image {
	dst := image.NewRGBA(rect)
	scale.Scale(dst, rect, src, src.Bounds(), draw.Over, nil)
	return dst
}

func (a *App) setAvatar(nickname, path string) {
	if b, err := ioutil.ReadFile(path); err == nil {
		// scale file
		if m, _, err := image.Decode(bytes.NewReader(b)); err == nil {
			avatarSz := image.Rect(0, 0, 96, 96)
			resized := scale(m, avatarSz, draw.ApproxBiLinear)
			b := &bytes.Buffer{}
			if err := png.Encode(b, resized); err == nil {
				a.c.AddBlob("avatar://"+nickname, b.Bytes())
				delete(avatars, nickname)
			}
		}
	} else {
		panic(err)
	}
}
