package settings

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/oboo/moho/internal/geo"
)

// StripRules is the zero-width rule by default, kept across opening the
// store again, and a list the user emptied stays empty.
func TestStripRulesSaved(t *testing.T) {
	dir := t.TempDir()
	st, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	if got := st.Get().StripRules; len(got) != 1 || got[0] != geo.InvisibleRule {
		t.Fatalf("default %q, want %q", got, geo.InvisibleRule)
	}

	// A settings.json written before the field says nothing about it: the
	// default is taken, as the automatic stripping did before it became a
	// setting.
	if err := os.WriteFile(filepath.Join(dir, "settings.json"), []byte(`{"language":"zh"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	st, err = Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	if got := st.Get().StripRules; len(got) != 1 || got[0] != geo.InvisibleRule {
		t.Fatalf("an old file's default %q, want %q", got, geo.InvisibleRule)
	}

	// Emptied by the user: written as an empty list, and it stays empty.
	if _, err := st.Update(func(s *Settings) { s.StripRules = []string{} }); err != nil {
		t.Fatal(err)
	}
	st, err = Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	if got := st.Get().StripRules; len(got) != 0 {
		t.Fatalf("an emptied list %q, want none", got)
	}
}

// CompileRules turns plain text into a rule matching itself and /…/ into the
// expression between the slashes; one that does not compile is reported.
func TestCompileRules(t *testing.T) {
	res, bad := CompileRules([]string{"IPLC", "/\\[.*\\]/", "C++", "/(/", ""})
	if len(res) != 5 {
		t.Fatalf("%d results, want 5", len(res))
	}
	if got := res[0].FindString("HK IPLC 01"); got != "IPLC" {
		t.Errorf("plain text matches itself: %q", got)
	}
	if got := res[1].FindString("[Vless] HK"); got != "[Vless]" {
		t.Errorf("a slash rule is the expression between: %q", got)
	}
	if got := res[2].FindString("Node C++ IEPL"); got != "C++" {
		t.Errorf("a plain + is not a quantifier: %q", got)
	}
	if len(bad) != 1 || bad[0] != 3 || res[3] != nil {
		t.Errorf("the malformed expression: bad %v, res %v", bad, res)
	}
	if res[4] == nil {
		t.Error("an empty rule is plain text, which always compiles")
	}
}

// Validate refuses an empty rule and a malformed expression, in the words
// the interface says.
func TestStripRulesValidate(t *testing.T) {
	s := Defaults.Clone()
	s.StripRules = []string{"/(/"}
	err := s.Validate()
	if err == nil || !strings.Contains(err.Error(), "/(/") {
		t.Errorf("a malformed expression passes: %v", err)
	}
	s.StripRules = []string{"  "}
	if err := s.Validate(); err == nil {
		t.Error("an empty rule passes")
	}
	s.StripRules = []string{"IPLC"}
	if err := s.Validate(); err != nil {
		t.Errorf("a plain rule fails: %v", err)
	}
}
