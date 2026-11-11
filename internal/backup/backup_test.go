package backup

import (
	"archive/zip"
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"
)

func sample() Bundle {
	return Bundle{
		Manifest:      Manifest{Version: "0.3.0", OS: "darwin", CreatedAt: time.Date(2026, 11, 10, 8, 0, 0, 0, time.UTC)},
		Settings:      []byte(`{"mode":"rule"}`),
		Providers:     []byte(`[{"id":"a1b2c3","name":"n"}]`),
		ProviderFiles: map[string][]byte{"a1b2c3": []byte("trojan://x@example.com:443#n")},
		RuleSets:      []byte(`{"s1":{"behavior":"domain"}}`),
		RuleSetFiles:  map[string][]byte{"s1": []byte("payload:\n  - example.com\n")},
	}
}

func same(t *testing.T, got, want Bundle) {
	t.Helper()
	want.Manifest.Format = Format
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %+v\nwant %+v", got, want)
	}
}

func TestRoundTrip(t *testing.T) {
	for _, pw := range []string{"", "correct horse"} {
		data, err := Encode(sample(), pw)
		if err != nil {
			t.Fatal(err)
		}
		if Sealed(data) != (pw != "") {
			t.Fatalf("password %q: sealed = %v", pw, Sealed(data))
		}
		if pw != "" && bytes.Contains(data, []byte("example.com")) {
			t.Fatal("a sealed file shows its contents")
		}
		got, err := Decode(data, pw)
		if err != nil {
			t.Fatal(err)
		}
		same(t, got, sample())
	}
}

func TestPassword(t *testing.T) {
	data, err := Encode(sample(), "one")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Decode(data, ""); !errors.Is(err, ErrNeedPassword) {
		t.Fatalf("no password: %v", err)
	}
	if _, err := Decode(data, "two"); !errors.Is(err, ErrWrongPassword) {
		t.Fatalf("wrong password: %v", err)
	}
	// A password for a file without one does no harm.
	plain, _ := Encode(sample(), "")
	if _, err := Decode(plain, "one"); err != nil {
		t.Fatalf("plain file with a password: %v", err)
	}
}

func TestTampered(t *testing.T) {
	data, _ := Encode(sample(), "one")
	for _, at := range []int{len(magic) + 2, len(magic) + saltLen + 2, len(data) - 20, len(data) - 1} {
		bad := bytes.Clone(data)
		bad[at] ^= 1
		if _, err := Decode(bad, "one"); !errors.Is(err, ErrWrongPassword) {
			t.Fatalf("byte %d changed: %v", at, err)
		}
	}
	if _, err := Decode(data[:len(magic)+4], "one"); !errors.Is(err, ErrNotBackup) {
		t.Fatalf("cut short: %v", err)
	}
}

// zipOf is a zip of the entries, for files this package would not write.
func zipOf(t *testing.T, entries map[string]string) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for name, body := range entries {
		w, err := zw.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		w.Write([]byte(body))
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func TestNotBackup(t *testing.T) {
	for name, data := range map[string][]byte{
		"text":        []byte("mixed-port: 7890\n"),
		"empty":       nil,
		"other zip":   zipOf(t, map[string]string{"a.txt": "a"}),
		"no settings": zipOf(t, map[string]string{"manifest.json": `{"format":1}`}),
		"format 0":    zipOf(t, map[string]string{"manifest.json": `{}`, "settings.json": `{}`}),
	} {
		if _, err := Decode(data, ""); !errors.Is(err, ErrNotBackup) {
			t.Errorf("%s: %v", name, err)
		}
	}
}

func TestNewerFormat(t *testing.T) {
	data := zipOf(t, map[string]string{"manifest.json": `{"format":2}`, "settings.json": `{}`})
	if _, err := Decode(data, ""); !errors.Is(err, ErrNewer) {
		t.Fatalf("got %v", err)
	}
}

// Names that would leave the stores' directories, or are not a store's
// files, are never taken for one.
func TestEntryNames(t *testing.T) {
	data := zipOf(t, map[string]string{
		"manifest.json":              `{"format":1}`,
		"settings.json":              `{}`,
		"providers/../../evil.txt":   "x",
		"providers/sub/a.txt":        "x",
		"providers/..txt":            "x",
		"providers/.txt":             "x",
		`providers/a\b.txt`:          "x",
		"providers/ok.txt":           "kept",
		"rulesets/../settings.yaml":  "x",
		"rulesets/ok.yaml":           "kept",
		"rulesets/ok.txt":            "x",
		"/etc/passwd":                "x",
		"core/config.yaml":           "x",
		"providers/a/../../b/c.yaml": "x",
	})
	b, err := Decode(data, "")
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(b.ProviderFiles, map[string][]byte{"ok": []byte("kept")}) {
		t.Errorf("providers: %q", b.ProviderFiles)
	}
	if !reflect.DeepEqual(b.RuleSetFiles, map[string][]byte{"ok": []byte("kept")}) {
		t.Errorf("rule sets: %q", b.RuleSetFiles)
	}
	bad := sample()
	bad.ProviderFiles = map[string][]byte{"../x": nil}
	if _, err := Encode(bad, ""); err == nil {
		t.Error("an id that leaves the directory was written")
	}
}

func TestTooBig(t *testing.T) {
	big := sample()
	big.ProviderFiles["a1b2c3"] = make([]byte, maxEntry+1)
	data, err := Encode(big, "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Decode(data, ""); !errors.Is(err, ErrTooBig) {
		t.Fatalf("got %v", err)
	}
}

func TestFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "c."+Ext)
	if err := WriteFile(path, sample(), "pw"); err != nil {
		t.Fatal(err)
	}
	if fi, err := os.Stat(path); err != nil || fi.Mode().Perm()&0o077 != 0 && os.PathSeparator == '/' {
		t.Fatalf("mode %v, %v", fi.Mode(), err)
	}
	if _, err := os.Stat(path + ".tmp"); err == nil {
		t.Fatal("the temporary file was left")
	}
	got, err := ReadFile(path, "pw")
	if err != nil {
		t.Fatal(err)
	}
	same(t, got, sample())
	if _, err := ReadFile(path, ""); !IsPassword(err) {
		t.Fatalf("got %v", err)
	}
}
