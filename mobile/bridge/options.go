package bridge

// The 设置 pages, Provider sharing, and reading QR codes from pictures.

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"time"

	"github.com/oboo/moho/internal/i18n"
	"github.com/oboo/moho/internal/qr"
	"github.com/oboo/moho/internal/routes"
	"github.com/oboo/moho/internal/settings"
)

const oldRulesFile = "user-rules.json"

// openSettings opens settings.json, starting it, the first time, from the
// rules an older build kept in a file of their own.
func openSettings(dir string) (*settings.Store, error) {
	st, err := settings.Open(dir)
	if err != nil {
		return nil, err
	}
	old := filepath.Join(dir, oldRulesFile)
	data, err := os.ReadFile(old)
	if err != nil {
		return st, nil
	}
	var rules []routes.UserRule
	if json.Unmarshal(data, &rules) == nil && len(rules) > 0 {
		if _, err := st.Update(func(s *settings.Settings) { s.Rules = append(rules, s.Rules...) }); err != nil {
			return st, nil
		}
	}
	_ = os.Remove(old)
	return st, nil
}

// settingsData is the settings as saved, plus what the page can do here.
type settingsData struct {
	settings.Settings
	GeoUpdate bool `json:"canUpdateGeo"`
}

func (x *bridge) settingsData() settingsData {
	return settingsData{Settings: x.set.Get(), GeoUpdate: canUpdateGeo}
}

// setLanguage changes the language of what the bridge writes. It touches
// nothing the core reads, so the config isn't reloaded.
func (x *bridge) setLanguage(name string) error {
	if !i18n.Known(name) {
		return fmt.Errorf(i18n.T("未知语言: %s", "Unknown language: %s"), name)
	}
	if _, err := x.set.Update(func(s *settings.Settings) { s.Language = name }); err != nil {
		return err
	}
	i18n.Set(name)
	return nil
}

// setSettings applies a JSON object of settings' own keys over the settings.
func (x *bridge) setSettings(ctx context.Context, patch json.RawMessage) error {
	if len(patch) == 0 {
		return nil
	}
	var bad error
	err := x.change(ctx, func(s *settings.Settings) { bad = json.Unmarshal(patch, s) })
	if bad != nil {
		return bad
	}
	return err
}

func (x *bridge) resetCore(ctx context.Context) error {
	return x.change(ctx, func(s *settings.Settings) { s.ResetCore() })
}

func (x *bridge) flushDNS(ctx context.Context) error {
	c, err := x.core()
	if err != nil {
		return err
	}
	if err := c.FlushDNS(ctx); err != nil {
		return err
	}
	return c.FlushFakeIP(ctx)
}

func (x *bridge) updateGeo() error {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	if runtime.GOOS == "ios" {
		o := x.set.Get()
		return fetchGeoFiles(ctx, x.dir, o.GeoSiteURL, o.GeoIPURL)
	}
	c, err := x.core()
	if err != nil {
		return err
	}
	return c.UpdateGeo(ctx)
}

// fetchGeoFiles downloads the rule data into dir. The core runs in the iOS
// extension, which can't spare the memory to download it, so the app does; the
// core reads the new files when it next starts. Nothing is replaced unless
// both files came down whole.
func fetchGeoFiles(ctx context.Context, dir, siteURL, ipURL string) error {
	files := []struct {
		url, name string
		limit     int64
	}{{siteURL, "GeoSite.dat", maxGeoSite}, {ipURL, "GeoIP.dat", maxGeoIP}}
	temps := make([]string, 0, len(files))
	defer func() {
		for _, t := range temps {
			_ = os.Remove(t)
		}
	}()
	for _, f := range files {
		t, err := fetchGeoFile(ctx, dir, f.url, f.name, f.limit)
		if err != nil {
			return err
		}
		temps = append(temps, t)
	}
	for i, f := range files {
		if err := os.Rename(temps[i], filepath.Join(dir, f.name)); err != nil {
			return err
		}
	}
	return nil
}

func fetchGeoFile(ctx context.Context, dir, from, name string, limit int64) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, from, nil)
	if err != nil {
		return "", err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("%s: HTTP %d", name, resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, limit+1))
	if err != nil {
		return "", err
	}
	if int64(len(body)) > limit {
		return "", fmt.Errorf("%s: %s", name, i18n.T("文件太大，VPN 的内存放不下，请换精简版的地址", "too large for the VPN's memory, use a smaller list"))
	}
	// Both are protobuf lists: a field 1 of bytes comes first. A web page doesn't.
	if len(body) < 1024 || body[0] != 0x0a {
		return "", fmt.Errorf("%s: %s", name, i18n.T("下载到的不是 Geo 数据", "what came down isn't Geo data"))
	}
	t, err := os.CreateTemp(dir, name+".*")
	if err != nil {
		return "", err
	}
	defer t.Close()
	if _, err := t.Write(body); err != nil {
		_ = os.Remove(t.Name())
		return "", err
	}
	return t.Name(), nil
}

// shareData is what shares a Provider: its text, and that text's QR code as
// rows of 0 and 1, one character a module.
type shareData struct {
	Text  string   `json:"text"`
	Rows  []string `json:"rows,omitempty"`
	Error string   `json:"error,omitempty"`
}

func (x *bridge) shareProvider(id string) shareData {
	text, err := x.store.Share(id)
	if err != nil {
		return shareData{Error: err.Error()}
	}
	img, err := qr.Encode(text)
	if err != nil { // too long for a code; the text can still be copied
		return shareData{Text: text}
	}
	b := img.Bounds()
	out := shareData{Text: text, Rows: make([]string, 0, b.Dy())}
	row := make([]byte, b.Dx())
	for y := b.Min.Y; y < b.Max.Y; y++ {
		for x := b.Min.X; x < b.Max.X; x++ {
			row[x-b.Min.X] = '0'
			if img.GrayAt(x, y).Y < 128 {
				row[x-b.Min.X] = '1'
			}
		}
		out.Rows = append(out.Rows, string(row))
	}
	return out
}

// decodedData is a picture's QR code, or why there is none.
type decodedData struct {
	Text  string `json:"text,omitempty"`
	Error string `json:"error,omitempty"`
}

func decodeQR(path string) decodedData {
	texts, err := qr.DecodeFile(path)
	if err != nil {
		return decodedData{Error: err.Error()}
	}
	if len(texts) == 0 {
		return decodedData{Error: i18n.T("图片里没有二维码", "No QR code in that picture")}
	}
	return decodedData{Text: texts[0]}
}

// canUpdateGeo: every platform can now; iOS downloads in the app.
const canUpdateGeo = true

// What the iOS extension can afford to read, about twice what it ships with.
const (
	maxGeoSite = 9 << 20
	maxGeoIP   = 1 << 20
)
