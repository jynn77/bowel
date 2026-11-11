package main

// QR codes of providers: read from the camera, the screen, a picture or
// the clipboard into the add bar, and made to share one.

import (
	"context"
	"errors"
	"image"
	"image/png"
	"os"
	"runtime"
	"slices"
	"strings"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/plugins/permissionflow"
	"github.com/egoist/mygo/transfer"
	xdraw "golang.org/x/image/draw"

	"github.com/oboo/moho/internal/i18n"
	"github.com/oboo/moho/internal/provider"
	"github.com/oboo/moho/internal/qr"
)

// providerShare is what shares a provider: its subscription URL, or its
// node links.
func (a *App) providerShare(id string) (string, error) { return a.providers.Share(id) }

// fromQR turns what codes read into the add bar's text.
func fromQR(texts []string, err error) (string, error) {
	if err != nil {
		return "", err
	}
	return provider.FromQR(texts)
}

// qrScreen reads the codes on every display, and says where they were,
// for the hint that frames them. macOS gives the screen only to apps
// allowed to record it; without that it gives the wallpaper.
func (a *App) qrScreen(ctx context.Context) (string, []qrSpots, error) {
	if !permissionflow.Allowed(permissionflow.ScreenRecording) {
		_, _ = permissionflow.Request(ctx, permissionflow.ScreenRecording) // the system's prompt, once
	}
	codes, err := qr.ScanScreen()
	// Only macOS keeps the screen behind a permission of its own; on the
	// other systems a capture that failed says so itself.
	if runtime.GOOS == "darwin" && (errors.Is(err, qr.ErrScreen) || errors.Is(err, qr.ErrNone) && !permissionflow.Allowed(permissionflow.ScreenRecording)) {
		_ = permissionflow.OpenSettings(permissionflow.ScreenRecording)
		return "", nil, errors.New(i18n.T("读取屏幕需要「屏幕录制」权限：在系统设置里允许 bowel，重新打开 bowel 后再扫描", "Reading the screen needs Screen Recording: allow bowel in System Settings, reopen bowel, then scan again"))
	}
	if errors.Is(err, qr.ErrNone) {
		return "", nil, errors.New(i18n.T("屏幕上没有找到二维码", "No QR code on the screen"))
	}
	if err != nil {
		return "", nil, err
	}
	s, err := fromQR(qr.Texts(codes), nil)
	return s, screenSpots(codes, mygo.Screen.Displays()), err
}

// qrSpots are the codes read off one display: its bounds, and each code's
// square in DIPs from the display's top-left corner.
type qrSpots struct {
	Display mygo.Rectangle
	Rects   []mygo.Rectangle
}

// screenSpots puts codes found in the screen's captures on the displays
// they were on. screencapture takes the main display first, as Displays
// lists it; a capture whose shape is not its display's goes to the first
// display left that has it.
func screenSpots(codes []qr.ScreenCode, displays []mygo.Display) []qrSpots {
	shaped := func(size image.Point, d mygo.Display) bool {
		if size.X <= 0 || size.Y <= 0 || d.Bounds.Width <= 0 || d.Bounds.Height <= 0 {
			return false
		}
		r := float64(size.X) / float64(size.Y) / (float64(d.Bounds.Width) / float64(d.Bounds.Height))
		return r > .98 && r < 1.02
	}
	byCapture := map[int]int{} // capture → display
	used := map[int]bool{}
	var out []qrSpots
	for _, c := range codes {
		di, ok := byCapture[c.Display]
		if !ok {
			di = -1
			if c.Display < len(displays) && !used[c.Display] && shaped(c.Size, displays[c.Display]) {
				di = c.Display
			} else {
				for i, d := range displays {
					if !used[i] && shaped(c.Size, d) {
						di = i
						break
					}
				}
			}
			byCapture[c.Display] = di
			if di >= 0 {
				used[di] = true
				out = append(out, qrSpots{Display: displays[di].Bounds})
			}
		}
		if di < 0 || c.Rect.Empty() {
			continue
		}
		b := displays[di].Bounds
		k := float64(b.Width) / float64(c.Size.X) // pixels → DIPs
		r := mygo.Rectangle{
			X: int(float64(c.Rect.Min.X) * k), Y: int(float64(c.Rect.Min.Y) * k),
			Width: int(float64(c.Rect.Dx())*k + .5), Height: int(float64(c.Rect.Dy())*k + .5),
		}
		for i := range out {
			if out[i].Display == b {
				out[i].Rects = append(out[i].Rects, r)
			}
		}
	}
	return slices.DeleteFunc(out, func(s qrSpots) bool { return len(s.Rects) == 0 })
}

// qrImageFile reads the codes in a picture the user picks; "" when they
// cancel.
func (a *App) qrImageFile(ctx context.Context) (string, error) {
	paths, err := mygo.Dialog.Open(mygo.OpenDialogOptions{
		Parent: mygo.CallerWindow(ctx), Title: i18n.T("从图片读取二维码", "Read a QR code from a picture"),
		Filters: []mygo.FileFilter{{Name: i18n.T("图片", "Pictures"), Extensions: []string{"png", "jpg", "jpeg", "gif", "webp", "bmp", "heic", "heif", "tiff", "tif"}}},
	})
	if err != nil || len(paths) == 0 {
		return "", err
	}
	texts, err := qr.DecodeFile(paths[0])
	if errors.Is(err, qr.ErrNone) {
		return "", errors.New(i18n.T("这张图片里没有找到二维码", "No QR code in this picture"))
	}
	return fromQR(texts, err)
}

// qrClipboard reads the codes in a picture on the clipboard; ok is false
// when there is none.
func (a *App) qrClipboard() (s string, ok bool, err error) {
	data, err := mygo.Clipboard.ReadFormat(transfer.PNG)
	if err != nil || len(data) == 0 {
		return "", false, nil
	}
	texts, err := qr.DecodeBytes(data)
	if errors.Is(err, qr.ErrNone) {
		return "", true, errors.New(i18n.T("剪贴板里的图片没有二维码", "No QR code in the picture on the clipboard"))
	}
	s, err = fromQR(texts, err)
	return s, true, err
}

// openCamera asks for the camera the first time, then opens it.
func (a *App) openCamera(ctx context.Context) (*qr.Camera, error) {
	st, err := permissionflow.Check(permissionflow.Camera)
	if err == nil && st == permissionflow.NotDetermined {
		st, err = permissionflow.Request(ctx, permissionflow.Camera)
	}
	if err != nil && !errors.Is(err, errors.ErrUnsupported) {
		return nil, err
	}
	if err == nil && st == permissionflow.Denied {
		_ = permissionflow.OpenSettings(permissionflow.Camera)
		return nil, errors.New(i18n.T("使用摄像头需要权限：在系统设置里允许 bowel 使用摄像头", "bowel needs the camera: allow it in System Settings"))
	}
	return qr.OpenCamera()
}

// saveQR writes a provider's code as a PNG the user places, 8 pixels a
// module so that it prints and scans.
func (a *App) saveQR(ctx context.Context, name string, code *image.Gray) error {
	name = strings.NewReplacer("/", "-", ":", "-").Replace(name)
	path, err := mygo.Dialog.Save(mygo.SaveDialogOptions{
		Parent: mygo.CallerWindow(ctx), Title: i18n.T("存储二维码", "Save QR code"), DefaultPath: name + i18n.T(" 二维码.png", " QR code.png"),
	})
	if err != nil || path == "" {
		return err
	}
	const k = 8
	b := code.Bounds()
	big := image.NewGray(image.Rect(0, 0, b.Dx()*k, b.Dy()*k))
	xdraw.NearestNeighbor.Scale(big, big.Bounds(), code, b, xdraw.Src, nil)
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	if err := png.Encode(f, big); err != nil {
		f.Close()
		return err
	}
	return f.Close()
}
