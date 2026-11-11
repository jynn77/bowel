package provider

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestSnapshotReplace(t *testing.T) {
	a, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	p1, err := a.create(Provider{Kind: Link, Name: "one", Enabled: true}, []byte("trojan://x@one.example:443#one"))
	if err != nil {
		t.Fatal(err)
	}
	p2, err := a.create(Provider{Kind: Subscription, Name: "two", Source: "https://sub.example/t", AutoUpdate: 12}, []byte("trojan://x@two.example:443#two"))
	if err != nil {
		t.Fatal(err)
	}
	items, files := a.Snapshot()
	if len(items) != 2 || len(files) != 2 {
		t.Fatalf("snapshot: %d items, %d files", len(items), len(files))
	}

	// Another store, with a provider of its own, takes the snapshot.
	dir := t.TempDir()
	b, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	old, err := b.create(Provider{Kind: Link, Name: "old", Enabled: true}, []byte("trojan://x@old.example:443#old"))
	if err != nil {
		t.Fatal(err)
	}
	if err := b.Replace(items, files); err != nil {
		t.Fatal(err)
	}
	if got := b.List(); len(got) != 2 || got[0].ID != p1.ID || got[1].ID != p2.ID || got[1].Source != p2.Source || got[1].AutoUpdate != 12 {
		t.Fatalf("after Replace: %+v", got)
	}
	if _, err := os.Stat(b.Path(old.ID)); err == nil {
		t.Error("the provider that is gone left its file")
	}
	if c, _ := b.Content(p2.ID); string(c) != "trojan://x@two.example:443#two" {
		t.Errorf("content: %q", c)
	}
	// And it is what a restart finds.
	again, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	ids := func(ps []Provider) (out []string) {
		for _, p := range ps {
			out = append(out, p.ID+" "+p.Name+" "+p.Source)
		}
		return out
	}
	if !reflect.DeepEqual(ids(again.List()), ids(b.List())) {
		t.Errorf("reopened: %+v", again.List())
	}
	if left, _ := filepath.Glob(filepath.Join(dir, "*.tmp")); len(left) > 0 {
		t.Errorf("temporary files left: %v", left)
	}
}

// A provider without content, or with an id that is no file name, changes
// nothing.
func TestReplaceRefuses(t *testing.T) {
	s, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	kept, err := s.create(Provider{Kind: Link, Name: "kept"}, []byte("trojan://x@kept.example:443#kept"))
	if err != nil {
		t.Fatal(err)
	}
	for name, c := range map[string]struct {
		items []Provider
		files map[string][]byte
	}{
		"no content": {[]Provider{{ID: "aaaaaa"}}, nil},
		"path":       {[]Provider{{ID: "../x"}}, map[string][]byte{"../x": []byte("x")}},
		"empty id":   {[]Provider{{}}, map[string][]byte{"": []byte("x")}},
		"twice":      {[]Provider{{ID: "a"}, {ID: "a"}}, map[string][]byte{"a": []byte("x")}},
	} {
		if err := s.Replace(c.items, c.files); err == nil {
			t.Errorf("%s: replaced", name)
		}
		if got := s.List(); len(got) != 1 || got[0].ID != kept.ID {
			t.Fatalf("%s: the store changed: %+v", name, got)
		}
		if _, err := s.Content(kept.ID); err != nil {
			t.Fatalf("%s: %v", name, err)
		}
	}
}
