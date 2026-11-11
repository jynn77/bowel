// Package backup is the file a configuration is exported to and imported
// from: the settings, the providers and the rule sets, in one zip. With a
// password the zip is sealed whole, as it holds subscription links.
//
// The package knows the file, not the app: what is in the settings and
// which of them travel is the settings package's business.
package backup

import (
	"archive/zip"
	"bytes"
	"crypto/aes"
	"crypto/cipher"
	"crypto/pbkdf2"
	"crypto/rand"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/oboo/moho/internal/i18n"
)

// Format is the layout this version writes; a file of a later one is not
// read, as what it means cannot be known.
const Format = 1

// Ext is the files' extension.
const Ext = "bowel"

// Manifest says where a file comes from.
type Manifest struct {
	Format    int       `json:"format"`
	Version   string    `json:"version"` // of the bowel that wrote it
	OS        string    `json:"os"`
	CreatedAt time.Time `json:"createdAt"`
}

// Bundle is a file's contents. The JSON is as the stores keep it.
type Bundle struct {
	Manifest Manifest
	// Settings is settings.json, without what belongs to the machine.
	Settings json.RawMessage
	// Providers is the providers' index; ProviderFiles, each one's content
	// by its id.
	Providers     json.RawMessage
	ProviderFiles map[string][]byte
	// RuleSets is the rule sets' index; RuleSetFiles, each one's file by
	// its id.
	RuleSets     json.RawMessage
	RuleSetFiles map[string][]byte
}

// failure is an error said in the interface's language when it is shown.
type failure struct{ zh, en string }

func (f *failure) Error() string { return i18n.T(f.zh, f.en) }

var (
	// ErrNeedPassword: the file is sealed and no password was given.
	ErrNeedPassword = &failure{"这个文件设了密码，请输入密码", "This file has a password; enter it"}
	// ErrWrongPassword: the password does not open the file, or the file
	// was changed since it was written.
	ErrWrongPassword = &failure{"密码不对，或者文件已损坏", "Wrong password, or the file is damaged"}
	// ErrNotBackup: the file is not one of these.
	ErrNotBackup = &failure{"这不是 bowel 导出的配置文件", "This is not a configuration exported by bowel"}
	// ErrNewer: a later bowel wrote the file.
	ErrNewer = &failure{"这个文件来自更新版本的 bowel，请先更新", "This file comes from a newer bowel; update first"}
	// ErrTooBig: the file, or something in it, is larger than any
	// configuration.
	ErrTooBig = &failure{"配置文件太大了", "That configuration file is too big"}
)

const (
	maxEntry = 32 << 20  // one provider or rule set
	maxTotal = 128 << 20 // everything, unpacked
	maxFile  = 160 << 20 // the file itself
)

// A sealed file: the magic, a salt, a nonce, then the zip under AES-256-GCM
// with the magic as its additional data. The key is PBKDF2-SHA256 of the
// password.
const (
	magic    = "BOWELBK1"
	saltLen  = 16
	nonceLen = 12
	kdfIter  = 600_000
)

// validID is what a provider's or a rule set's id may be: it becomes a file
// name, so nothing that leaves the directory.
var validID = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.-]{0,63}$`)

// ValidID says whether id can name a file of a store.
func ValidID(id string) bool { return validID.MatchString(id) && !strings.Contains(id, "..") }

// Sealed says whether data is a file with a password.
func Sealed(data []byte) bool { return bytes.HasPrefix(data, []byte(magic)) }

// Encode is b as a file, sealed with password unless it is "".
func Encode(b Bundle, password string) ([]byte, error) {
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	add := func(name string, data []byte) error {
		w, err := zw.CreateHeader(&zip.FileHeader{Name: name, Method: zip.Deflate, Modified: b.Manifest.CreatedAt})
		if err != nil {
			return err
		}
		_, err = w.Write(data)
		return err
	}
	b.Manifest.Format = Format
	man, err := json.MarshalIndent(b.Manifest, "", "  ")
	if err != nil {
		return nil, err
	}
	entries := []struct {
		name string
		data []byte
	}{
		{"manifest.json", man},
		{"settings.json", b.Settings},
		{"providers/index.json", orJSON(b.Providers, "[]")},
		{"rulesets/index.json", orJSON(b.RuleSets, "{}")},
	}
	for _, id := range sorted(b.ProviderFiles) {
		if !ValidID(id) {
			return nil, fmt.Errorf("backup: bad provider id %q", id)
		}
		entries = append(entries, struct {
			name string
			data []byte
		}{"providers/" + id + ".txt", b.ProviderFiles[id]})
	}
	for _, id := range sorted(b.RuleSetFiles) {
		if !ValidID(id) {
			return nil, fmt.Errorf("backup: bad rule set id %q", id)
		}
		entries = append(entries, struct {
			name string
			data []byte
		}{"rulesets/" + id + ".yaml", b.RuleSetFiles[id]})
	}
	for _, e := range entries {
		if err := add(e.name, e.data); err != nil {
			return nil, err
		}
	}
	if err := zw.Close(); err != nil {
		return nil, err
	}
	if password == "" {
		return buf.Bytes(), nil
	}
	salt, nonce := make([]byte, saltLen), make([]byte, nonceLen)
	_, _ = rand.Read(salt)
	_, _ = rand.Read(nonce)
	aead, err := sealer(password, salt)
	if err != nil {
		return nil, err
	}
	out := append([]byte(magic), salt...)
	out = append(out, nonce...)
	return aead.Seal(out, nonce, buf.Bytes(), []byte(magic)), nil
}

func sealer(password string, salt []byte) (cipher.AEAD, error) {
	key, err := pbkdf2.Key(sha256.New, password, salt, kdfIter, 32)
	if err != nil {
		return nil, err
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	return cipher.NewGCM(block)
}

func orJSON(b json.RawMessage, empty string) []byte {
	if len(bytes.TrimSpace(b)) == 0 {
		return []byte(empty)
	}
	return b
}

func sorted(m map[string][]byte) []string {
	ids := make([]string, 0, len(m))
	for id := range m {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	return ids
}

// Decode reads a file. A sealed one needs its password: ErrNeedPassword
// without, ErrWrongPassword with another.
func Decode(data []byte, password string) (Bundle, error) {
	var b Bundle
	if len(data) > maxFile {
		return b, ErrTooBig
	}
	if Sealed(data) {
		if password == "" {
			return b, ErrNeedPassword
		}
		head := len(magic) + saltLen + nonceLen
		if len(data) < head {
			return b, ErrNotBackup
		}
		salt, nonce := data[len(magic):len(magic)+saltLen], data[len(magic)+saltLen:head]
		aead, err := sealer(password, salt)
		if err != nil {
			return b, err
		}
		plain, err := aead.Open(nil, nonce, data[head:], []byte(magic))
		if err != nil {
			return b, ErrWrongPassword
		}
		data = plain
	}
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return b, ErrNotBackup
	}
	b.ProviderFiles, b.RuleSetFiles = map[string][]byte{}, map[string][]byte{}
	total, seen := 0, map[string]bool{}
	var man []byte
	for _, f := range zr.File {
		if f.FileInfo().IsDir() {
			continue
		}
		if seen[f.Name] { // which of the two would be meant?
			return b, ErrNotBackup
		}
		seen[f.Name] = true
		body, err := readEntry(f, maxTotal-total)
		if err != nil {
			return b, err
		}
		total += len(body)
		switch f.Name {
		case "manifest.json":
			man = body
		case "settings.json":
			b.Settings = body
		case "providers/index.json":
			b.Providers = body
		case "rulesets/index.json":
			b.RuleSets = body
		default:
			if id, ok := entryID(f.Name, "providers/", ".txt"); ok {
				b.ProviderFiles[id] = body
			} else if id, ok := entryID(f.Name, "rulesets/", ".yaml"); ok {
				b.RuleSetFiles[id] = body
			}
			// Anything else is not ours to read: a later version's, or
			// something a person added. It is left alone.
		}
	}
	if man == nil || json.Unmarshal(man, &b.Manifest) != nil || b.Manifest.Format < 1 {
		return b, ErrNotBackup
	}
	if b.Manifest.Format > Format {
		return b, ErrNewer
	}
	if b.Settings == nil {
		return b, ErrNotBackup
	}
	return b, nil
}

// entryID is the id in a name such as providers/a1b2c3.txt, if the name is
// exactly that: no further directories, and an id that is safe as a file's.
func entryID(name, dir, ext string) (string, bool) {
	rest, ok := strings.CutPrefix(name, dir)
	if !ok {
		return "", false
	}
	id, ok := strings.CutSuffix(rest, ext)
	if !ok || !ValidID(id) {
		return "", false
	}
	return id, true
}

func readEntry(f *zip.File, left int) ([]byte, error) {
	limit := min(maxEntry, left)
	if f.UncompressedSize64 > uint64(limit) {
		return nil, ErrTooBig
	}
	r, err := f.Open()
	if err != nil {
		return nil, ErrNotBackup
	}
	defer r.Close()
	// The header's size is the writer's word; the limit is what counts.
	body, err := io.ReadAll(io.LimitReader(r, int64(limit)+1))
	if err != nil {
		return nil, ErrNotBackup
	}
	if len(body) > limit {
		return nil, ErrTooBig
	}
	return body, nil
}

// ReadFile reads the file at path.
func ReadFile(path, password string) (Bundle, error) {
	f, err := os.Open(path)
	if err != nil {
		return Bundle{}, err
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, maxFile+1))
	if err != nil {
		return Bundle{}, err
	}
	return Decode(data, password)
}

// WriteFile writes b to path, whole or not at all, for the user alone to
// read.
func WriteFile(path string, b Bundle, password string) error {
	data, err := Encode(b, password)
	if err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return err
	}
	if err := os.Rename(tmp, path); err != nil {
		os.Remove(tmp)
		return err
	}
	return nil
}

// IsPassword says whether err is about the password: none given, or the
// wrong one.
func IsPassword(err error) bool {
	return errors.Is(err, ErrNeedPassword) || errors.Is(err, ErrWrongPassword)
}
