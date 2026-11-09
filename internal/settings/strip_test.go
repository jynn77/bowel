package settings

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/oboo/moho/internal/geo"
)

// StripChars is the zero-width set by default, kept across opening the store
// again, and a field the user cleared stays cleared.
func TestStripCharsSaved(t *testing.T) {
	dir := t.TempDir()
	st, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	if got := st.Get().StripChars; got != geo.InvisibleChars {
		t.Fatalf("default %q, want %q", got, geo.InvisibleChars)
	}

	// A settings.json from before the field: nothing about it is said, so
	// the default is taken, as the automatic stripping did before it became
	// a setting.
	if err := os.WriteFile(filepath.Join(dir, "settings.json"), []byte(`{"language":"zh"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	st, err = Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	if got := st.Get().StripChars; got != geo.InvisibleChars {
		t.Fatalf("an old file's default %q, want %q", got, geo.InvisibleChars)
	}

	// Cleared by the user: written as an empty field, and it stays empty.
	if _, err := st.Update(func(s *Settings) { s.StripChars = "" }); err != nil {
		t.Fatal(err)
	}
	st, err = Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	if got := st.Get().StripChars; got != "" {
		t.Fatalf("a cleared field %q, want \"\"", got)
	}
}
