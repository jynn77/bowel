package qr

import (
	"image"
	"image/png"
	"os"
	"path/filepath"
	"testing"
)

// With no screenshot tool the scan says which to install; with one, its
// picture is what is read. The tool here is a stand-in that copies a
// picture of the test's, so no screen is needed.
func TestScanScreenTools(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	if _, err := ScanScreen(); err != ErrNoScreenTool {
		t.Fatalf("with no tool: %v", err)
	}

	dir := t.TempDir()
	code, err := Encode("vless://example")
	if err != nil {
		t.Fatal(err)
	}
	// Scaled up, as a code on a screen is.
	const scale = 8
	big := image.NewGray(image.Rect(0, 0, code.Bounds().Dx()*scale, code.Bounds().Dy()*scale))
	for y := range big.Bounds().Dy() {
		for x := range big.Bounds().Dx() {
			big.SetGray(x, y, code.GrayAt(x/scale, y/scale))
		}
	}
	shot := filepath.Join(dir, "shot.png")
	f, err := os.Create(shot)
	if err != nil {
		t.Fatal(err)
	}
	if err := png.Encode(f, big); err != nil {
		t.Fatal(err)
	}
	f.Close()
	// scrot's place in the list: called as `scrot -o FILE`.
	script := "#!/bin/sh\n[ \"$1\" = -o ] || exit 2\ncat " + shot + " > \"$2\"\n"
	if err := os.WriteFile(filepath.Join(dir, "scrot"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	// A tool earlier in the list that fails in this session is passed over.
	if err := os.WriteFile(filepath.Join(dir, "grim"), []byte("#!/bin/sh\nexit 1\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+":/usr/bin:/bin")
	codes, err := ScanScreen()
	if err != nil {
		t.Fatalf("scan: %v", err)
	}
	if got := Texts(codes); len(got) != 1 || got[0] != "vless://example" {
		t.Errorf("read %q", got)
	}
	if codes[0].Display != 0 || codes[0].Size != big.Bounds().Size() {
		t.Errorf("code %+v", codes[0])
	}
}
