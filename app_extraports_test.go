package main

import (
	"encoding/json"
	"fmt"
	"net"
	"strings"
	"testing"

	"github.com/oboo/moho/internal/core"
	"github.com/oboo/moho/internal/settings"
)

// TestExtraPortsOnCore: an extra port set as the CLI sets it (a settings
// patch) is open on this machine, bound to its group. A port another program
// holds, bowel's own, and a group that doesn't exist are refused, with the
// settings left as they were. A port whose group is deleted afterwards
// closes, and deleting the group is not held up by it.
func TestExtraPortsOnCore(t *testing.T) {
	a, _ := routingApp(t, nil)
	port := freePortFor(t)
	set := func(v string) error {
		patch, err := a.settingsPatch(map[string]json.RawMessage{"extraPorts": json.RawMessage(v)}, true)
		if err != nil {
			return err
		}
		return a.updateSettings(func(s *settings.Settings) { _ = json.Unmarshal(patch, s) })
	}
	if !strings.Contains(strings.Join(editable(a.settings.Get()), " "), "extraPorts") {
		t.Fatal("extraPorts is not a setting the CLI can change")
	}
	if err := set(fmt.Sprintf(`[{"port":%d,"route":"ai"}]`, port)); err != nil {
		t.Fatal(err)
	}
	listener := fmt.Sprintf("name: %s", core.ExtraPortListener(port))
	if cfg := coreConfig(t, a); !strings.Contains(cfg, listener) || !strings.Contains(cfg, "proxy: AI 服务") {
		t.Fatalf("the listener is not at the core:\n%s", cfg)
	}
	c, err := net.Dial("tcp", fmt.Sprintf("127.0.0.1:%d", port))
	if err != nil {
		t.Fatalf("nothing listens on the extra port: %v", err)
	}
	c.Close()

	held, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer held.Close()
	for name, v := range map[string]string{
		"held by another program": fmt.Sprintf(`[{"port":%d,"route":"ai"},{"port":%d,"route":"media"}]`, port, held.Addr().(*net.TCPAddr).Port),
		"bowel's own":             fmt.Sprintf(`[{"port":%d,"route":"ai"},{"port":%d,"route":"media"}]`, port, a.probePort),
		"the mixed port":          fmt.Sprintf(`[{"port":%d,"route":"ai"}]`, a.settings.Get().MixedPort),
		"no such group":           fmt.Sprintf(`[{"port":%d,"route":"nope"}]`, port),
		"not a list of ports":     `["7901"]`,
	} {
		if err := set(v); err == nil {
			t.Errorf("%s: should be refused", name)
		}
		if got := a.settings.Get().ExtraPorts; len(got) != 1 || got[0] != (core.ExtraPort{Port: port, Route: "ai"}) {
			t.Fatalf("%s: settings changed to %+v", name, got)
		}
	}

	// A group of the user's, a port for it, and the group deleted.
	var gid string
	if err := a.updateSettings(func(s *settings.Settings) { gid = s.Routing.AddGroup(s.Rules, "工作") }); err != nil {
		t.Fatal(err)
	}
	if err := set(fmt.Sprintf(`[{"port":%d,"route":%q}]`, port, gid)); err != nil {
		t.Fatal(err)
	}
	if cfg := coreConfig(t, a); !strings.Contains(cfg, "proxy: "+gid) {
		t.Fatalf("the port did not follow to the new group:\n%s", cfg)
	}
	if err := a.updateSettings(func(s *settings.Settings) { s.Routing.DeleteGroup(s.Rules, gid, "media") }); err != nil {
		t.Fatalf("deleting a group with a port: %v", err)
	}
	if cfg := coreConfig(t, a); strings.Contains(cfg, listener) {
		t.Errorf("the port of a deleted group is still open:\n%s", cfg)
	}
	if err := set(`[]`); err != nil {
		t.Fatal(err)
	}
}

func freePortFor(t *testing.T) int {
	t.Helper()
	p, err := core.FreePort()
	if err != nil {
		t.Fatal(err)
	}
	return p
}
