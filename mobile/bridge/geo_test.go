package bridge

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func geoServer(t *testing.T, files map[string][]byte) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, ok := files[r.URL.Path]
		if !ok {
			http.NotFound(w, r)
			return
		}
		_, _ = w.Write(b)
	}))
	t.Cleanup(srv.Close)
	return srv
}

func geoBytes(n int) []byte { return append([]byte{0x0a}, bytes.Repeat([]byte{1}, n-1)...) }

func TestFetchGeoReplacesBothFiles(t *testing.T) {
	dir := t.TempDir()
	srv := geoServer(t, map[string][]byte{"/site": geoBytes(4096), "/ip": geoBytes(2048)})
	if err := fetchGeoFiles(context.Background(), dir, srv.URL+"/site", srv.URL+"/ip"); err != nil {
		t.Fatal(err)
	}
	for name, size := range map[string]int64{"GeoSite.dat": 4096, "GeoIP.dat": 2048} {
		st, err := os.Stat(filepath.Join(dir, name))
		if err != nil || st.Size() != size {
			t.Fatalf("%s: %v, %v", name, st, err)
		}
	}
	all, _ := os.ReadDir(dir)
	if len(all) != 2 {
		t.Fatalf("left behind: %v", all)
	}
}

// A bad second file must not leave a new first one, and the old data stays.
func TestFetchGeoKeepsOldDataOnFailure(t *testing.T) {
	huge := geoBytes(maxGeoIP + 1)
	cases := map[string]map[string][]byte{
		"too large": {"/site": geoBytes(4096), "/ip": huge},
		"web page":  {"/site": geoBytes(4096), "/ip": []byte("<html>" + strings.Repeat("x", 4096))},
		"too short": {"/site": geoBytes(4096), "/ip": geoBytes(10)},
		"missing":   {"/site": geoBytes(4096)},
	}
	for name, files := range cases {
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			old := filepath.Join(dir, "GeoSite.dat")
			if err := os.WriteFile(old, []byte("old"), 0o600); err != nil {
				t.Fatal(err)
			}
			srv := geoServer(t, files)
			if err := fetchGeoFiles(context.Background(), dir, srv.URL+"/site", srv.URL+"/ip"); err == nil {
				t.Fatal("accepted")
			}
			if b, _ := os.ReadFile(old); string(b) != "old" {
				t.Fatalf("GeoSite.dat replaced: %d bytes", len(b))
			}
			all, _ := os.ReadDir(dir)
			if len(all) != 1 {
				t.Fatalf("left behind: %v", all)
			}
		})
	}
}
