// Package qr reads QR codes, from pictures and from the screen, and makes
// them. It is pure Go (gozxing); the screen is read with macOS's
// screencapture.
package qr

import (
	"bytes"
	"errors"
	"image"
	_ "image/gif" // the formats a QR code's picture comes in
	_ "image/jpeg"
	_ "image/png"
	"math"
	"os"
	"path/filepath"
	"runtime"
	"slices"

	"github.com/makiuchi-d/gozxing"
	multiqr "github.com/makiuchi-d/gozxing/multi/qrcode"
	"github.com/makiuchi-d/gozxing/qrcode"
	"github.com/makiuchi-d/gozxing/qrcode/decoder"
	_ "golang.org/x/image/bmp"
	_ "golang.org/x/image/webp"

	"github.com/oboo/moho/internal/i18n"
	"github.com/oboo/moho/internal/nowindow"
)

// msg is an error said in the interface's language when it is said, and
// comparable with errors.Is.
type msg struct{ zh, en string }

func (m *msg) Error() string { return i18n.T(m.zh, m.en) }

// ErrNone means the picture holds no QR code that reads.
var ErrNone error = &msg{"没有找到二维码", "No QR code found"}

// Code is a QR code found in a picture: what it says, and the square it
// fills, quiet zone excluded, in the picture's pixels.
type Code struct {
	Text string
	Rect image.Rectangle
}

// Decode reads every QR code in a picture, in the order found, without
// repeats.
func Decode(img image.Image) ([]string, error) {
	codes, err := DecodeCodes(img)
	return texts(codes), err
}

// texts is what codes say, without repeats.
func texts(codes []Code) []string {
	var out []string
	for _, c := range codes {
		if c.Text != "" && !slices.Contains(out, c.Text) {
			out = append(out, c.Text)
		}
	}
	return out
}

// DecodeCodes reads every QR code in a picture and where it is, in the
// order found; a code is there once.
func DecodeCodes(img image.Image) ([]Code, error) {
	src := gozxing.NewLuminanceSourceFromImage(img)
	hints := map[gozxing.DecodeHintType]any{gozxing.DecodeHintType_TRY_HARDER: true}
	var out []Code
	add := func(r *gozxing.Result) {
		s := r.GetText()
		if s == "" || slices.ContainsFunc(out, func(c Code) bool { return c.Text == s }) {
			return
		}
		out = append(out, Code{Text: s, Rect: codeRect(r.GetResultPoints()).Intersect(img.Bounds())})
	}
	// Dark on light, then light on dark (a code in a dark theme).
	for _, s := range []gozxing.LuminanceSource{src, src.Invert()} {
		bmp, err := gozxing.NewBinaryBitmap(gozxing.NewHybridBinarizer(s))
		if err != nil {
			continue
		}
		if rs, err := multiqr.NewQRCodeMultiReader().DecodeMultiple(bmp, hints); err == nil {
			for _, r := range rs {
				add(r)
			}
		}
		if len(out) == 0 {
			// The multi reader gives up on some pictures the single one reads.
			if r, err := qrcode.NewQRCodeReader().Decode(bmp, hints); err == nil {
				add(r)
			}
		}
		if len(out) > 0 {
			return out, nil
		}
	}
	return nil, ErrNone
}

// codeRect is the square a code fills, from the points the reader found:
// the centers of its three finder patterns (bottom left, top left, top
// right), maybe an alignment pattern after. The fourth corner is where the
// other three put it; the edge is three and a half modules out from a
// finder's center.
func codeRect(pts []gozxing.ResultPoint) image.Rectangle {
	if len(pts) == 0 {
		return image.Rectangle{}
	}
	minX, minY, maxX, maxY := math.Inf(1), math.Inf(1), math.Inf(-1), math.Inf(-1)
	grow := func(x, y float64) {
		minX, minY, maxX, maxY = min(minX, x), min(minY, y), max(maxX, x), max(maxY, y)
	}
	for _, p := range pts {
		grow(p.GetX(), p.GetY())
	}
	if len(pts) >= 3 {
		bl, tl, tr := pts[0], pts[1], pts[2]
		grow(bl.GetX()+tr.GetX()-tl.GetX(), bl.GetY()+tr.GetY()-tl.GetY())
	}
	var module float64
	n := 0
	for _, p := range pts[:min(3, len(pts))] {
		if f, ok := p.(interface{ GetEstimatedModuleSize() float64 }); ok {
			module += f.GetEstimatedModuleSize()
			n++
		}
	}
	pad := 0.0
	if n > 0 {
		pad = 3.5 * module / float64(n)
	} else {
		pad = 0.12 * max(maxX-minX, maxY-minY) // about a version 2 code's
	}
	return image.Rect(int(math.Floor(minX-pad)), int(math.Floor(minY-pad)), int(math.Ceil(maxX+pad)), int(math.Ceil(maxY+pad)))
}

// DecodeBytes reads the QR codes in a picture's data: PNG, JPEG, GIF, WebP
// or BMP, and on macOS whatever sips converts (HEIC, TIFF, …).
func DecodeBytes(data []byte) ([]string, error) {
	img, err := decodeImage(data)
	if err != nil {
		return nil, err
	}
	return Decode(img)
}

// decodeImage reads a picture's data.
func decodeImage(data []byte) (image.Image, error) {
	img, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		img, err = convert(data)
		if err != nil {
			return nil, errors.New(i18n.T("无法读取这张图片", "Cannot read this picture"))
		}
	}
	return img, nil
}

// DecodeFile reads the QR codes in a picture file.
func DecodeFile(path string) ([]string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return DecodeBytes(data)
}

// convert turns a picture Go cannot decode into PNG with sips (macOS).
func convert(data []byte) (image.Image, error) {
	if runtime.GOOS != "darwin" {
		return nil, errors.ErrUnsupported
	}
	dir, err := os.MkdirTemp("", "moho-qr-")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(dir)
	in, out := filepath.Join(dir, "in"), filepath.Join(dir, "out.png")
	if err := os.WriteFile(in, data, 0o600); err != nil {
		return nil, err
	}
	if err := nowindow.Command("/usr/bin/sips", "-s", "format", "png", in, "--out", out).Run(); err != nil {
		return nil, err
	}
	b, err := os.ReadFile(out)
	if err != nil {
		return nil, err
	}
	img, _, err := image.Decode(bytes.NewReader(b))
	return img, err
}

// ErrScreen means the screen could not be captured: on macOS, Screen
// Recording has not been allowed.
var ErrScreen error = &msg{"无法读取屏幕", "Cannot read the screen"}

// ErrNoScreenTool means Linux has no program installed that can take a
// picture of the screen.
var ErrNoScreenTool error = &msg{"没有找到截屏工具：请安装 grim、spectacle、gnome-screenshot、maim、scrot 或 ImageMagick 之一", "No screenshot tool was found: install one of grim, spectacle, gnome-screenshot, maim, scrot or ImageMagick"}

// maxDisplays is how many displays ScanScreen reads.
const maxDisplays = 8

// ScreenCode is a QR code on a display: Display is which of the screen's
// captures it is in (the main display first, as screencapture takes them),
// Size that capture's size in pixels, and Rect is in its pixels.
type ScreenCode struct {
	Code
	Display int
	Size    image.Point
}

// Texts is what codes on the screen say, without repeats.
func Texts(codes []ScreenCode) []string {
	cs := make([]Code, len(codes))
	for i, c := range codes {
		cs[i] = c.Code
	}
	return texts(cs)
}

// ScanScreen reads the QR codes on every display, and where they are.
// How the screen is captured is each system's own (screen_*.go): one
// picture per display where the system gives that, or one of them all.
func ScanScreen() ([]ScreenCode, error) {
	dir, err := os.MkdirTemp("", "moho-qr-")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(dir)
	files, runErr := capture(dir)
	if errors.Is(runErr, errors.ErrUnsupported) || runErr == ErrNoScreenTool {
		return nil, runErr
	}
	var out []ScreenCode
	read := 0
	for i, f := range files {
		data, err := os.ReadFile(f)
		if err != nil {
			continue
		}
		read++
		img, err := decodeImage(data)
		if err != nil {
			continue
		}
		codes, _ := DecodeCodes(img)
		for _, c := range codes {
			out = append(out, ScreenCode{Code: c, Display: i, Size: img.Bounds().Size()})
		}
	}
	switch {
	case len(out) > 0:
		return out, nil
	case read == 0 && runErr != nil:
		return nil, ErrScreen
	}
	return nil, ErrNone
}

// ErrTooLong means the text does not fit in a QR code.
var ErrTooLong error = &msg{"内容太长，放不进二维码", "Too long to fit in a QR code"}

// Encode makes a QR code of text: one pixel per module, black on white,
// with the standard quiet zone. Show it scaled up, pixelated.
func Encode(text string) (*image.Gray, error) {
	if text == "" {
		return nil, errors.New(i18n.T("内容为空", "Nothing to encode"))
	}
	w := qrcode.NewQRCodeWriter()
	var m *gozxing.BitMatrix
	var err error
	// Medium correction where the text leaves room for it, low where not.
	for _, level := range []decoder.ErrorCorrectionLevel{decoder.ErrorCorrectionLevel_M, decoder.ErrorCorrectionLevel_L} {
		m, err = w.Encode(text, gozxing.BarcodeFormat_QR_CODE, 0, 0, map[gozxing.EncodeHintType]any{
			gozxing.EncodeHintType_CHARACTER_SET:    "UTF-8",
			gozxing.EncodeHintType_ERROR_CORRECTION: level,
			gozxing.EncodeHintType_MARGIN:           4,
		})
		if err == nil {
			break
		}
	}
	if err != nil {
		return nil, ErrTooLong
	}
	img := image.NewGray(image.Rect(0, 0, m.GetWidth(), m.GetHeight()))
	for y := range m.GetHeight() {
		for x := range m.GetWidth() {
			if !m.Get(x, y) {
				img.Pix[y*img.Stride+x] = 0xff
			}
		}
	}
	return img, nil
}
