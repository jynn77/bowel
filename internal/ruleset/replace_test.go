package ruleset

import (
	"os"
	"testing"
)

func TestSnapshotReplace(t *testing.T) {
	a, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"mine", "unused"} {
		set, err := Parse([]byte("payload:\n  - DOMAIN-SUFFIX," + id + ".example\n"))
		if err != nil {
			t.Fatal(err)
		}
		if err := a.Save(id, &Checked{Source: "/tmp/" + id + ".yaml", Set: set}); err != nil {
			t.Fatal(err)
		}
	}
	meta, files := a.Snapshot(map[string]bool{"mine": true, "gone": true})
	if len(meta) != 1 || len(files) != 1 || meta["mine"].Count != 1 {
		t.Fatalf("snapshot: %+v, %d files", meta, len(files))
	}

	dir := t.TempDir()
	b, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	set, _ := Parse([]byte("payload:\n  - DOMAIN-SUFFIX,old.example\n"))
	if err := b.Save("old", &Checked{Set: set}); err != nil {
		t.Fatal(err)
	}
	if _, ok := b.Match("old", "a.old.example", ""); !ok {
		t.Fatal("the old set does not match")
	}
	meta["nofile"] = Meta{Behavior: "domain"}                // no file: left out
	files["../evil"] = []byte("payload: []")                 // no meta: never written
	meta["../evil2"], files["../evil2"] = Meta{}, []byte("") // not a file name: left out
	if err := b.Replace(meta, files); err != nil {
		t.Fatal(err)
	}
	if _, ok := b.Get("old"); ok {
		t.Error("the old set is still known")
	}
	if _, err := os.Stat(b.Path("old")); err == nil {
		t.Error("the old set left its file")
	}
	if _, ok := b.Match("old", "a.old.example", ""); ok {
		t.Error("the old set still matches")
	}
	if _, ok := b.Get("nofile"); ok {
		t.Error("a set without a file is known")
	}
	if m, ok := b.Get("mine"); !ok || m.Count != 1 {
		t.Fatalf("mine: %+v %v", m, ok)
	}
	if _, ok := b.Match("mine", "a.mine.example", ""); !ok {
		t.Error("the new set does not match")
	}
	again, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := again.Get("mine"); !ok {
		t.Error("reopened: the set is gone")
	}
	entries, _ := os.ReadDir(dir)
	if len(entries) != 2 { // index.json and mine.yaml
		t.Errorf("the directory holds %v", entries)
	}
}
