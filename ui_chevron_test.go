package main

import (
	"image"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
	"github.com/oboo/moho/internal/routes"
)

// inkBox is the box of the pixels in r that are not the frame's first pixel.
func inkBox(img *image.RGBA, r image.Rectangle) image.Rectangle {
	bg := img.RGBAAt(0, 0)
	var box image.Rectangle
	for y := r.Min.Y; y < r.Max.Y; y++ {
		for x := r.Min.X; x < r.Max.X; x++ {
			if img.RGBAAt(x, y) != bg {
				box = box.Union(image.Rect(x, y, x+1, y+1))
			}
		}
	}
	return box
}

// TestChevronMirrors: the open and shut chevrons are one shape, the same
// size at the same height, each the other upside down (issue 68: the
// font's up triangle was smaller than its down one).
func TestChevronMirrors(t *testing.T) {
	fontsOnce.Do(registerFonts)
	white, black := ui.RGBA(255, 255, 255, 1), ui.RGBA(0, 0, 0, 1)
	tt := ui.NewTester(func(c *ui.Context) {
		ui.Row(c).Fill().Background(white).AlignItems(ui.Center).Children(func() {
			ui.Row(c).Width(40).Justify(ui.Center).Children(func() { chevron(c, black, false) })
			ui.Row(c).Width(40).Justify(ui.Center).Children(func() { chevron(c, black, true) })
		})
	}, 80, 40)
	tt.Frame()
	img := tt.Image()
	b := img.Bounds()
	k := b.Dx() / 80 // pixels a DIP
	down := inkBox(img, image.Rect(0, 0, 40*k, b.Dy()))
	up := inkBox(img, image.Rect(40*k, 0, 80*k, b.Dy()))
	if down.Empty() || up.Empty() {
		t.Fatalf("nothing drawn: down %v, up %v", down, up)
	}
	if down.Size() != up.Size() || down.Min.Y != up.Min.Y || up.Min.X-down.Min.X != 40*k {
		t.Fatalf("down %v and up %v differ in size or place", down, up)
	}
	// Upside down, pixel for pixel; and not the same way up.
	same := true
	for y := 0; y < down.Dy(); y++ {
		for x := 0; x < down.Dx(); x++ {
			d := img.RGBAAt(down.Min.X+x, down.Min.Y+y)
			if d != img.RGBAAt(up.Min.X+x, up.Max.Y-1-y) {
				t.Fatalf("pixel %d,%d of the down chevron is not the up one's mirrored", x, y)
			}
			same = same && d == img.RGBAAt(up.Min.X+x, up.Min.Y+y)
		}
	}
	if same {
		t.Error("up and down point the same way")
	}
}

// TestNoTriangleGlyphs: nothing the interface shows is a font's triangle;
// what opens and shuts is marked with chevron, in every language's table.
func TestNoTriangleGlyphs(t *testing.T) {
	var files []string
	for _, pat := range []string{"*.go", "internal/i18n/*.go"} {
		m, _ := filepath.Glob(pat)
		files = append(files, m...)
	}
	for _, f := range files {
		if strings.HasSuffix(f, "_test.go") {
			continue
		}
		b, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		for i, line := range strings.Split(string(b), "\n") {
			if strings.ContainsAny(line, "▾▴▼▲▿▵▽△") {
				t.Errorf("%s:%d: a triangle glyph, use chevron: %s", f, i+1, strings.TrimSpace(line))
			}
		}
	}
}

// TestChevronTurns: a group's 节点策略 head has no glyph for its mark
// either way, and the head stays where it is when it opens.
func TestChevronTurns(t *testing.T) {
	u := routesFixture()
	u.selectGroup("ai")
	tt := ui.NewTester(u.view, 1060, 760)
	tt.Advance(time.Second)
	shot(t, tt, "routes-chevron-shut")
	before, ok := tt.Find("节点策略")
	if !ok {
		t.Fatalf("no head: %q", tt.Texts())
	}
	if err := tt.Click("节点策略"); err != nil {
		t.Fatal(err)
	}
	tt.Advance(time.Second)
	shot(t, tt, "routes-chevron-open")
	if !u.routes.pol {
		t.Fatal("the head didn't open")
	}
	if after, _ := tt.Find("节点策略"); after != before {
		t.Errorf("the head moved when it opened: %v, then %v", before, after)
	}
	for _, s := range tt.Texts() {
		if strings.ContainsAny(s, "▾▴▼▲") {
			t.Errorf("a triangle glyph shows: %q", s)
		}
	}
}

// TestOrderRowsAlign: in 顺序备用 the nodes in the order and the ones
// that can be added are laid out alike: names, delays and the action at
// the end sit in the same columns, under the columns' names (issue 68).
func TestOrderRowsAlign(t *testing.T) {
	u := routesFixture()
	u.selectGroup("ai")
	u.routes.pol = true
	tt := ui.NewTester(u.view, 1060, 760)
	tt.Advance(time.Second)
	if err := tt.Click("顺序备用"); err != nil {
		t.Fatal(err)
	}
	tt.Advance(200 * time.Millisecond)
	// Two nodes no group uses, so each name shows once, in its row.
	u.snap.Nodes[1].Ping, u.snap.Nodes[2].Ping = 777, 999
	in, out := u.snap.Nodes[1], u.snap.Nodes[2]
	u.changePolicy("ai", func(q *routes.Policy) { q.Order = []string{in.Name} })
	tt.Frame()
	tt.Advance(200 * time.Millisecond)
	shot(t, tt, "routes-order-columns")
	find := func(s string) ui.Rect {
		t.Helper()
		r, ok := findPart(tt, s)
		if !ok {
			t.Fatalf("no %q: %q", s, tt.Texts())
		}
		return r
	}
	head, add := find("备用顺序"), find("点击加入")
	nameIn, nameOut := find(in.Display), find(out.Display)
	if !(head.Y < nameIn.Y && nameIn.Y < add.Y && add.Y < nameOut.Y) {
		t.Fatalf("%s isn't in the order with %s under 点击加入", in.Display, out.Display)
	}
	if nameIn.X != nameOut.X {
		t.Errorf("names start at %v in the order, %v under it", nameIn.X, nameOut.X)
	}
	pingIn, pingOut, pingHead := find("777"), find("999"), find("Ping")
	if pingIn.X != pingOut.X || pingIn.W != pingOut.W {
		t.Errorf("Ping is at %v in the order, %v under it", pingIn, pingOut)
	}
	if pingHead.X+pingHead.W != pingIn.X+pingIn.W {
		t.Errorf("Ping's column ends at %v, its name at %v", pingIn.X+pingIn.W, pingHead.X+pingHead.W)
	}
	rm := find("移除")
	plus, ok := tt.Find("＋")
	if !ok {
		t.Fatalf("no ＋: %q", tt.Texts())
	}
	if a, b := rm.X+rm.W/2, plus.X+plus.W/2; a != b {
		t.Errorf("× is centered at %v, ＋ at %v", a, b)
	}
}
