package settings

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/oboo/moho/internal/core"
)

// TestExtraPorts: an extra port is a listener like the others, on a port of
// its own; it is this machine's, so resetting the core's options and
// carrying the configuration elsewhere both leave it out of their way.
func TestExtraPorts(t *testing.T) {
	s := Defaults.Clone()
	s.ExtraPorts = []core.ExtraPort{{Port: 7901, Route: "ai"}, {Port: 7902, Route: "media"}}
	if err := s.Validate(); err != nil {
		t.Fatal(err)
	}
	for name, change := range map[string]func(*Settings){
		"the mixed port's": func(s *Settings) { s.ExtraPorts[0].Port = s.MixedPort },
		"the HTTP port's":  func(s *Settings) { s.HTTPPort = 7901 },
		"the controller's": func(s *Settings) { s.ExtController = "127.0.0.1:7902" },
		"another's":        func(s *Settings) { s.ExtraPorts[1].Port = 7901 },
		"none":             func(s *Settings) { s.ExtraPorts[0].Port = 0 },
		"no route":         func(s *Settings) { s.ExtraPorts[0].Route = "" },
	} {
		bad := s.Clone()
		change(&bad)
		if err := bad.Validate(); err == nil {
			t.Errorf("%s: should not pass", name)
		}
	}

	var back Settings
	b, _ := json.Marshal(s)
	if err := json.Unmarshal(b, &back); err != nil || len(back.ExtraPorts) != 2 || back.ExtraPorts[1] != s.ExtraPorts[1] {
		t.Fatalf("saved and read: %v %+v", err, back.ExtraPorts)
	}
	if !strings.Contains(string(b), `"extraPorts":[{"port":7901,"route":"ai"}`) {
		t.Errorf("saved as %s", b)
	}

	reset := s.Clone()
	reset.ResetCore()
	if len(reset.ExtraPorts) != 2 {
		t.Error("ResetCore closed the extra ports")
	}
	if p := Portable(s); len(p.ExtraPorts) != 0 {
		t.Errorf("exported: %+v", p.ExtraPorts)
	}
	if got := KeepMachine(s, Defaults.Clone()); len(got.ExtraPorts) != 2 {
		t.Errorf("after an import: %+v", got.ExtraPorts)
	}
}
