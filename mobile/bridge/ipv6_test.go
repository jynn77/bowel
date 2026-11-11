package bridge

import (
	"encoding/json"
	"testing"

	"github.com/oboo/moho/internal/core"
	"github.com/oboo/moho/internal/settings"
)

// The app's switch reads a boolean, and Auto is on here.
func TestSettingsDataIPv6(t *testing.T) {
	x := testBridge(t)
	for mode, want := range map[core.IPv6Mode]bool{core.IPv6Auto: true, core.IPv6On: true, core.IPv6Off: false} {
		if _, err := x.set.Update(func(s *settings.Settings) { s.IPv6 = mode }); err != nil {
			t.Fatal(err)
		}
		b, err := json.Marshal(x.settingsData())
		if err != nil {
			t.Fatal(err)
		}
		var got struct {
			IPv6 any `json:"ipv6"`
		}
		if err := json.Unmarshal(b, &got); err != nil {
			t.Fatal(err)
		}
		if got.IPv6 != want {
			t.Errorf("%s shows as %v", mode, got.IPv6)
		}
	}
}
