package ruleset

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"gopkg.in/yaml.v3"

	"github.com/oboo/moho/internal/i18n"
	"github.com/oboo/moho/internal/routes"
)

// UpdateEvery is how often a set from a link is downloaded again.
const UpdateEvery = 24 * time.Hour

const (
	maxSize      = 32 << 20
	fetchTimeout = 30 * time.Second
)

// Meta is what is known of a saved set, apart from its rules.
type Meta struct {
	Behavior  string    `json:"behavior"`
	Count     int       `json:"count"`
	Skipped   int       `json:"skipped,omitempty"`
	UpdatedAt time.Time `json:"updatedAt"`
	Error     string    `json:"error,omitempty"` // of the last update; the file is the last good one
}

// Checked is a set that was fetched and read, not yet saved.
type Checked struct {
	Source string
	Set    Set
}

// Name is what to call a set from its source: the file's name.
func (c *Checked) Name() string {
	base := c.Source
	if u, err := url.Parse(c.Source); err == nil && u.Path != "" {
		base = u.Path
	}
	base = strings.TrimSuffix(base, "/")
	base = base[strings.LastIndexAny(base, `/\`)+1:]
	base = strings.TrimSuffix(base, filepath.Ext(base))
	if base == "" {
		return i18n.T("规则集", "Rule set")
	}
	return base
}

// Store keeps the sets' files, and what is known of them, in a directory.
// The services in settings.json say which sets there are and where they
// come from; a set's id is its service's.
type Store struct {
	mu       sync.Mutex
	dir      string
	meta     map[string]Meta
	matchers map[string]*matcher
}

// Open reads the directory, making it if need be.
func Open(dir string) (*Store, error) {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, err
	}
	s := &Store{dir: dir, meta: map[string]Meta{}, matchers: map[string]*matcher{}}
	if b, err := os.ReadFile(s.index()); err == nil {
		_ = json.Unmarshal(b, &s.meta)
	}
	return s, nil
}

func (s *Store) index() string { return filepath.Join(s.dir, "index.json") }

// Dir is where the files are, for the core's list of safe paths.
func (s *Store) Dir() string { return s.dir }

// Path is the file the core reads for set id.
func (s *Store) Path(id string) string { return filepath.Join(s.dir, id+".yaml") }

func (s *Store) saveLocked() error {
	b, err := json.MarshalIndent(s.meta, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(s.index(), b, 0o600)
}

// Get is what is known of set id. A set whose file is gone counts as none.
func (s *Store) Get(id string) (Meta, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	m, ok := s.meta[id]
	if ok {
		if _, err := os.Stat(s.Path(id)); err != nil {
			return Meta{}, false
		}
	}
	return m, ok
}

// Local says whether a source is a file on this machine.
func Local(source string) bool {
	return !strings.HasPrefix(source, "http://") && !strings.HasPrefix(source, "https://")
}

func expand(path string) string {
	if rest, ok := strings.CutPrefix(path, "~/"); ok {
		if home, err := os.UserHomeDir(); err == nil {
			return filepath.Join(home, rest)
		}
	}
	return path
}

// Check says what is wrong with a source as typed, if anything can be told
// without fetching it.
func Check(source string) error {
	if Local(source) {
		if strings.HasPrefix(source, "~/") || filepath.IsAbs(source) {
			return nil
		}
	} else if u, err := url.Parse(source); err == nil && u.Hostname() != "" {
		return nil
	}
	return errors.New(i18n.T("不是链接或文件路径", "Not a link or a file path"))
}

// Fetch gets a source and reads it. Nothing is saved.
func Fetch(ctx context.Context, client *http.Client, source string) (*Checked, error) {
	if err := Check(source); err != nil {
		return nil, err
	}
	data, err := read(ctx, client, source)
	if err != nil {
		return nil, err
	}
	set, err := Parse(data)
	if err != nil {
		return nil, err
	}
	return &Checked{Source: source, Set: set}, nil
}

func read(ctx context.Context, client *http.Client, source string) ([]byte, error) {
	tooBig := errors.New(i18n.T("规则集太大了", "That rule set is too big"))
	var r io.Reader
	if Local(source) {
		f, err := os.Open(expand(source))
		if err != nil {
			return nil, err
		}
		defer f.Close()
		r = f
	} else {
		ctx, cancel := context.WithTimeout(ctx, fetchTimeout)
		defer cancel()
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, source, nil)
		if err != nil {
			return nil, err
		}
		resp, err := client.Do(req)
		if err != nil {
			return nil, err
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			return nil, fmt.Errorf("HTTP %d", resp.StatusCode)
		}
		r = resp.Body
	}
	data, err := io.ReadAll(io.LimitReader(r, maxSize+1))
	if err != nil {
		return nil, err
	}
	if len(data) > maxSize {
		return nil, tooBig
	}
	return data, nil
}

// Save writes a checked set as id. The file is replaced whole: the core
// watches it and must never read half of one.
func (s *Store) Save(id string, c *Checked) error {
	b, err := yaml.Marshal(map[string]any{"payload": c.Set.Rules})
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(s.dir, id+".*.tmp")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.Write(b); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Rename(tmp.Name(), s.Path(id)); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.meta[id] = Meta{Behavior: c.Set.Behavior, Count: len(c.Set.Rules), Skipped: c.Set.Skipped, UpdatedAt: time.Now()}
	s.matchers[id] = newMatcher(c.Set)
	return s.saveLocked()
}

// Update fetches set id again. On failure the last good file stays in use
// and the error is kept to show.
func (s *Store) Update(ctx context.Context, client *http.Client, id, source string) error {
	c, err := Fetch(ctx, client, source)
	if err != nil {
		s.mu.Lock()
		defer s.mu.Unlock()
		m := s.meta[id]
		m.Error = err.Error()
		s.meta[id] = m
		_ = s.saveLocked()
		return err
	}
	return s.Save(id, c)
}

// Stale says whether a set from a link is due for downloading.
func (s *Store) Stale(id string, now time.Time) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return now.Sub(s.meta[id].UpdatedAt) >= UpdateEvery
}

// Delete forgets set id.
func (s *Store) Delete(id string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.meta, id)
	delete(s.matchers, id)
	_ = os.Remove(s.Path(id))
	_ = s.saveLocked()
}

// Keep forgets every set but those of ids: what a crash, or a settings file
// restored from elsewhere, left behind.
func (s *Store) Keep(ids map[string]bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	entries, _ := os.ReadDir(s.dir)
	for _, e := range entries {
		id, ok := strings.CutSuffix(e.Name(), ".yaml")
		if ok && !ids[id] {
			_ = os.Remove(filepath.Join(s.dir, e.Name()))
		}
	}
	for id := range s.meta {
		if !ids[id] {
			delete(s.meta, id)
			delete(s.matchers, id)
		}
	}
	_ = s.saveLocked()
}

// matcher is set id's matcher, read from its file the first time.
func (s *Store) matcher(id string) *matcher {
	s.mu.Lock()
	defer s.mu.Unlock()
	if m := s.matchers[id]; m != nil {
		return m
	}
	meta, ok := s.meta[id]
	if !ok {
		return nil
	}
	b, err := os.ReadFile(s.Path(id))
	if err != nil {
		return nil
	}
	var y struct {
		Payload []string `yaml:"payload"`
	}
	if yaml.Unmarshal(b, &y) != nil {
		return nil
	}
	m := newMatcher(Set{Behavior: meta.Behavior, Rules: y.Payload})
	s.matchers[id] = m
	return m
}

// Match is the rule of set id that catches a connection to host at ip.
func (s *Store) Match(id, host, ip string) (string, bool) {
	if m := s.matcher(id); m != nil {
		return m.match(host, ip)
	}
	return "", false
}

// Preview is the first few rules of set id that people can read.
func (s *Store) Preview(id string) []routes.Rule {
	if m := s.matcher(id); m != nil {
		return m.preview
	}
	return nil
}
