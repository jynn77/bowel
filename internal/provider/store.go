// Package provider keeps where nodes come from: subscriptions, node links
// and local files. Each provider's content is stored as-is in a file that
// mihomo reads as a `type: file` proxy-provider; mihomo understands Clash
// YAML, Base64 subscriptions and plain node links (vmess://, trojan://, …).
package provider

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/oboo/moho/internal/i18n"
)

type Kind string

const (
	Subscription Kind = "subscription" // a URL Moho downloads and updates
	Link         Kind = "link"         // one or more node links pasted in
	File         Kind = "file"         // a local file or pasted YAML
)

// Usage is what a subscription's subscription-userinfo header tells.
type Usage struct {
	Upload   int64 `json:"upload"`
	Download int64 `json:"download"`
	Total    int64 `json:"total"`
	Expire   int64 `json:"expire"` // unix seconds, 0 for none
}

type Provider struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Kind    Kind   `json:"kind"`
	Source  string `json:"source,omitempty"` // the URL, or the file it came from
	Enabled bool   `json:"enabled"`
	// AutoUpdate is hours between updates of a subscription; 0 never.
	AutoUpdate int       `json:"autoUpdate"`
	UpdatedAt  time.Time `json:"updatedAt"`
	Usage      *Usage    `json:"usage,omitempty"`
	// Error is the last update's failure, "" after a success.
	Error string `json:"error,omitempty"`
}

// Store keeps providers in a directory: index.json and one file each.
type Store struct {
	dir   string
	mu    sync.Mutex
	items []Provider
	// addrs caches Addrs by the provider file it came from: the interface
	// asks every snapshot, and the file changes only when the provider does.
	addrs map[string]addrEntry
}

// addrEntry is what parseAddrs made of a provider file, and which file.
type addrEntry struct {
	size int64
	mod  time.Time
	by   map[string]string
}

func Open(dir string) (*Store, error) {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, err
	}
	s := &Store{dir: dir}
	if b, err := os.ReadFile(s.index()); err == nil {
		_ = json.Unmarshal(b, &s.items)
	} else if !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	return s, nil
}

func (s *Store) index() string { return filepath.Join(s.dir, "index.json") }

// Path is the file mihomo reads for a provider.
func (s *Store) Path(id string) string { return filepath.Join(s.dir, id+".txt") }

func (s *Store) saveLocked() error {
	b, _ := json.MarshalIndent(s.items, "", "  ")
	return os.WriteFile(s.index(), b, 0o600)
}

func (s *Store) List() []Provider {
	s.mu.Lock()
	defer s.mu.Unlock()
	return slices.Clone(s.items)
}

func (s *Store) Get(id string) (Provider, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	i := slices.IndexFunc(s.items, func(p Provider) bool { return p.ID == id })
	if i < 0 {
		return Provider{}, false
	}
	return s.items[i], true
}

func (s *Store) update(id string, fn func(*Provider)) (Provider, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := range s.items {
		if s.items[i].ID == id {
			fn(&s.items[i])
			return s.items[i], s.saveLocked()
		}
	}
	return Provider{}, fmt.Errorf(i18n.T("没有这个 Provider: %s", "No such Provider: %s"), id)
}

var linkScheme = regexp.MustCompile(`(?i)^(vmess|vless|trojan|ss|ssr|hysteria|hysteria2|hy2|tuic|wireguard|wg|anytls|socks5?|https?|mieru|snell)://`)

// Detect tells what pasted text is: a subscription URL, node links or YAML.
func Detect(input string) (Kind, error) {
	t := strings.TrimSpace(input)
	if t == "" {
		return "", errors.New(i18n.T("内容为空", "The content is empty"))
	}
	first := strings.TrimSpace(strings.SplitN(t, "\n", 2)[0])
	if u, err := url.Parse(first); err == nil && (u.Scheme == "http" || u.Scheme == "https") && !strings.Contains(t, "\n") {
		return Subscription, nil
	}
	if linkScheme.MatchString(first) {
		return Link, nil
	}
	if strings.Contains(t, "proxies:") {
		return File, nil
	}
	return "", errors.New(i18n.T("无法识别：请粘贴订阅链接、节点链接或 Clash 配置", "Not recognized: paste a subscription link, node links or a Clash config"))
}

// Add creates a provider from pasted text. Subscriptions are downloaded.
func (s *Store) Add(ctx context.Context, client *http.Client, input string) (Provider, error) {
	kind, err := Detect(input)
	if err != nil {
		return Provider{}, err
	}
	t := strings.TrimSpace(input)
	p := Provider{Kind: kind, Enabled: true}
	var data []byte
	switch kind {
	case Subscription:
		d, usage, filename, err := fetch(ctx, client, t)
		if err != nil {
			return Provider{}, err
		}
		if err := looksLikeNodes(d); err != nil {
			return Provider{}, err
		}
		u, _ := url.Parse(t)
		p.Source, p.Usage, p.AutoUpdate, data = t, usage, 12, d
		p.Name = firstNonEmpty(filename, u.Host)
	case Link:
		data = []byte(t)
		p.Name = linkName(t)
	case File:
		data = []byte(t)
		p.Name = i18n.T("粘贴的配置", "Pasted config")
	}
	return s.create(p, data)
}

// AddFile copies a local file in.
func (s *Store) AddFile(path string) (Provider, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Provider{}, err
	}
	if err := looksLikeNodes(data); err != nil {
		return Provider{}, err
	}
	name := strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))
	return s.create(Provider{Kind: File, Name: name, Source: path, Enabled: true}, data)
}

func (s *Store) create(p Provider, data []byte) (Provider, error) {
	p.ID = newID()
	p.UpdatedAt = time.Now()
	if err := os.WriteFile(s.Path(p.ID), data, 0o600); err != nil {
		return Provider{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.items = append(s.items, p)
	return p, s.saveLocked()
}

// Update downloads a subscription again. Other kinds are left as they are.
func (s *Store) Update(ctx context.Context, client *http.Client, id string) (Provider, error) {
	p, ok := s.Get(id)
	if !ok {
		return p, fmt.Errorf(i18n.T("没有这个 Provider: %s", "No such Provider: %s"), id)
	}
	if p.Kind != Subscription {
		return p, nil
	}
	data, usage, _, err := fetch(ctx, client, p.Source)
	if err == nil {
		err = looksLikeNodes(data)
	}
	if err != nil {
		p, _ = s.update(id, func(p *Provider) { p.Error = err.Error() })
		return p, err
	}
	if err := os.WriteFile(s.Path(id), data, 0o600); err != nil {
		return p, err
	}
	return s.update(id, func(p *Provider) {
		p.UpdatedAt, p.Usage, p.Error = time.Now(), usage, ""
	})
}

// Due lists subscriptions whose auto-update interval has passed.
func (s *Store) Due(now time.Time) []string {
	var ids []string
	for _, p := range s.List() {
		if p.Kind == Subscription && p.Enabled && p.AutoUpdate > 0 && now.Sub(p.UpdatedAt) >= time.Duration(p.AutoUpdate)*time.Hour {
			ids = append(ids, p.ID)
		}
	}
	return ids
}

func (s *Store) SetEnabled(id string, on bool) (Provider, error) {
	return s.update(id, func(p *Provider) { p.Enabled = on })
}

func (s *Store) Rename(id, name string) (Provider, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return Provider{}, errors.New(i18n.T("名称不能为空", "The name can't be empty"))
	}
	return s.update(id, func(p *Provider) { p.Name = name })
}

func (s *Store) SetAutoUpdate(id string, hours int) (Provider, error) {
	return s.update(id, func(p *Provider) { p.AutoUpdate = max(0, hours) })
}

func (s *Store) Delete(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.items = slices.DeleteFunc(s.items, func(p Provider) bool { return p.ID == id })
	_ = os.Remove(s.Path(id))
	return s.saveLocked()
}

// looksLikeNodes rejects content mihomo would read as zero nodes: an HTML
// error page, an empty answer.
func looksLikeNodes(data []byte) error {
	t := strings.TrimSpace(string(data))
	if t == "" {
		return errors.New(i18n.T("内容为空", "The content is empty"))
	}
	if strings.Contains(t, "proxies:") || linkScheme.MatchString(t) {
		return nil
	}
	if dec, err := base64.StdEncoding.DecodeString(strings.Join(strings.Fields(t), "")); err == nil && strings.Contains(string(dec), "://") {
		return nil
	}
	if dec, err := base64.RawStdEncoding.DecodeString(strings.Join(strings.Fields(t), "")); err == nil && strings.Contains(string(dec), "://") {
		return nil
	}
	if strings.HasPrefix(strings.ToLower(t), "<!doctype") || strings.HasPrefix(strings.ToLower(t), "<html") {
		return errors.New(i18n.T("订阅地址返回的是网页，不是节点列表", "The subscription link returned a web page, not a node list"))
	}
	return errors.New(i18n.T("没有找到节点：内容既不是 Clash 配置，也不是节点链接或 Base64 订阅", "No nodes found: the content is not a Clash config, node links or a Base64 subscription"))
}

func fetch(ctx context.Context, client *http.Client, u string) ([]byte, *Usage, string, error) {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, nil, "", err
	}
	// Providers answer Clash Meta clients with a Clash profile.
	req.Header.Set("User-Agent", "clash.meta/moho")
	resp, err := client.Do(req)
	if err != nil {
		return nil, nil, "", fmt.Errorf(i18n.T("下载订阅失败: %w", "Downloading the subscription failed: %w"), err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, nil, "", fmt.Errorf(i18n.T("下载订阅失败: %s", "Downloading the subscription failed: %s"), resp.Status)
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, 32<<20))
	if err != nil {
		return nil, nil, "", err
	}
	return data, parseUsage(resp.Header.Get("Subscription-Userinfo")), filenameOf(resp.Header.Get("Content-Disposition")), nil
}

// parseUsage reads "upload=1; download=2; total=3; expire=4".
func parseUsage(h string) *Usage {
	if h == "" {
		return nil
	}
	u := &Usage{}
	for _, part := range strings.Split(h, ";") {
		k, v, ok := strings.Cut(strings.TrimSpace(part), "=")
		if !ok {
			continue
		}
		n, _ := strconv.ParseInt(strings.TrimSpace(v), 10, 64)
		switch strings.TrimSpace(k) {
		case "upload":
			u.Upload = n
		case "download":
			u.Download = n
		case "total":
			u.Total = n
		case "expire":
			u.Expire = n
		}
	}
	return u
}

func filenameOf(cd string) string {
	for _, part := range strings.Split(cd, ";") {
		k, v, ok := strings.Cut(strings.TrimSpace(part), "=")
		if !ok {
			continue
		}
		switch strings.ToLower(k) {
		case "filename*":
			if _, enc, ok := strings.Cut(v, "''"); ok {
				if s, err := url.QueryUnescape(enc); err == nil {
					return s
				}
			}
		case "filename":
			return strings.Trim(v, `"`)
		}
	}
	return ""
}

// linkName names a provider of links after the first link's #fragment.
func linkName(t string) string {
	lines := strings.Fields(t)
	first := lines[0]
	name := ""
	if _, frag, ok := strings.Cut(first, "#"); ok {
		if s, err := url.QueryUnescape(frag); err == nil {
			name = s
		}
	}
	scheme := strings.ToLower(strings.SplitN(first, "://", 2)[0])
	if name == "" {
		name = scheme + i18n.T(" 节点", " node")
	}
	if len(lines) > 1 {
		name = fmt.Sprintf(i18n.T("%s 等 %d 个节点", "%s and %d nodes in all"), name, len(lines))
	}
	return name
}

func firstNonEmpty(v ...string) string {
	for _, s := range v {
		if s != "" {
			return s
		}
	}
	return ""
}

func newID() string {
	b := make([]byte, 3)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

// FromQR turns what QR codes read into what the add bar takes: one
// subscription URL, or node links one per line. Install links
// (clash://install-config?url=…, and those of mihomo, Clash Meta,
// Stash and Shadowrocket's sub://) give their subscription URL.
func FromQR(texts []string) (string, error) {
	var subs, links []string
	for _, t := range texts {
		t = Unwrap(strings.TrimSpace(t))
		switch k, _ := Detect(t); k {
		case Subscription:
			if !slices.Contains(subs, t) {
				subs = append(subs, t)
			}
		case Link:
			for _, l := range strings.Fields(t) {
				if linkScheme.MatchString(l) && !slices.Contains(links, l) {
					links = append(links, l)
				}
			}
		case File:
			return t, nil
		}
	}
	switch {
	case len(links) > 0:
		return strings.Join(links, "\n"), nil
	case len(subs) > 0:
		return subs[0], nil
	case len(texts) > 0:
		return "", errors.New(i18n.T("二维码里不是订阅链接或节点链接", "The QR code holds no subscription or node link"))
	}
	return "", errors.New(i18n.T("没有找到二维码", "No QR code found"))
}

// Unwrap gives the subscription URL inside a client's install link, or
// the text as it is.
func Unwrap(t string) string {
	u, err := url.Parse(t)
	if err != nil {
		return t
	}
	switch strings.ToLower(u.Scheme) {
	case "clash", "clashx", "clash-meta", "mihomo", "stash", "clashmi", "flclash":
		if v := u.Query().Get("url"); v != "" {
			return v
		}
	case "sub":
		// Shadowrocket: sub://<base64 of the URL>#name
		raw := strings.TrimPrefix(t[len(u.Scheme)+3:], "/")
		raw, _, _ = strings.Cut(raw, "#")
		for _, enc := range []*base64.Encoding{base64.StdEncoding, base64.RawStdEncoding, base64.URLEncoding, base64.RawURLEncoding} {
			if b, err := enc.DecodeString(raw); err == nil {
				if s := strings.TrimSpace(string(b)); strings.HasPrefix(s, "http://") || strings.HasPrefix(s, "https://") {
					return s
				}
			}
		}
	}
	return t
}

// Share is what shares a provider: a subscription's URL, or the node
// links pasted in. Files have nothing to share.
func (s *Store) Share(id string) (string, error) {
	p, ok := s.Get(id)
	if !ok {
		return "", fmt.Errorf(i18n.T("没有这个 Provider: %s", "No such Provider: %s"), id)
	}
	switch p.Kind {
	case Subscription:
		return p.Source, nil
	case Link:
		b, err := os.ReadFile(s.Path(id))
		if err != nil {
			return "", err
		}
		return strings.TrimSpace(string(b)), nil
	}
	return "", errors.New(i18n.T("本地文件没有可分享的链接", "A local file has no link to share"))
}
