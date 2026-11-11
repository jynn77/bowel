package main

import (
	"testing"
	"time"
	"unicode/utf8"

	"github.com/oboo/moho/internal/settings"
)

func TestFmtRateShort(t *testing.T) {
	for b, want := range map[int64]string{
		-1:         "0B",
		0:          "0B",
		512:        "512B",
		999:        "999B",
		1000:       "1.0K",
		1536:       "1.5K",
		60 << 10:   "60K",
		999 << 10:  "999K",
		1000 << 10: "1.0M",
		1363149:    "1.3M",
		10 << 20:   "10M",
		2254857830: "2.1G",
		5 << 40:    "5.0T",
		5000 << 40: "5000T",
		10443817:   "10M", // 9.96 MB: not "10.0M"
	} {
		if got := fmtRateShort(b); got != want {
			t.Errorf("fmtRateShort(%d) = %q, want %q", b, got, want)
		}
	}
}

// The text beside the icon keeps its length whatever the rates are, so
// that the menu bar's other items don't move.
func TestTrayTextWidth(t *testing.T) {
	want := -1
	for _, b := range []int64{0, 7, 512, 1000, 60 << 10, 999 << 10, 1363149, 300 << 20, 2254857830} {
		title, tip := trayText("darwin", true, Tick{Down: b, Up: b / 3})
		if tip != trayName {
			t.Errorf("tooltip %q beside a title", tip)
		}
		n := utf8.RuneCountInString(title)
		if want < 0 {
			want = n
		}
		if n != want {
			t.Errorf("%q is %d characters, others are %d", title, n, want)
		}
	}
}

func TestTrayText(t *testing.T) {
	tick := Tick{Down: 1363149, Up: 60 << 10}
	for _, c := range []struct {
		goos       string
		show       bool
		title, tip string
	}{
		{"darwin", false, "", "bowel"},
		{"windows", false, "", "bowel"},
		{"linux", false, "", "bowel"},
		{"darwin", true, "↓1.3M ↑ 60K", "bowel"},
		{"linux", true, "↓1.3M ↑ 60K", "bowel"},
		// No text beside a tray icon on Windows: the tooltip tells.
		{"windows", true, "", "bowel · ↓ 1.30 MB/s  ↑ 60.0 KB/s"},
	} {
		title, tip := trayText(c.goos, c.show, tick)
		if title != c.title || tip != c.tip {
			t.Errorf("trayText(%s, %v) = %q, %q; want %q, %q", c.goos, c.show, title, tip, c.title, c.tip)
		}
	}
}

// The rates beside the icon are off unless asked for, are the app's own
// business, and cost polls only while they show.
// Not taken over, the tray offers the nodes only with traffic through the
// port.
func TestTrayNodes(t *testing.T) {
	for _, c := range []struct {
		on   bool
		tick Tick
		want bool
	}{
		{true, Tick{}, true},
		{false, Tick{}, false},
		{false, Tick{Active: 1}, true},
		{false, Tick{Down: 512}, true},
	} {
		if got := trayNodes(c.on, c.tick); got != c.want {
			t.Errorf("trayNodes(%v, %+v) = %v", c.on, c.tick, got)
		}
	}
}

func TestTraySpeedSetting(t *testing.T) {
	if settings.Defaults.TraySpeed {
		t.Fatal("the menu bar shows the speed by default")
	}
	old := settings.Defaults.Clone()
	next := old.Clone()
	next.TraySpeed = true
	if !appOnly(old, next) {
		t.Error("showing the speed in the menu bar reloads the core")
	}

	st, err := settings.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	a := &App{settings: st}
	set := func(fn func(*settings.Settings)) {
		t.Helper()
		if _, err := st.Update(fn); err != nil {
			t.Fatal(err)
		}
	}
	set(func(s *settings.Settings) { s.SystemProxy, s.Paused, s.TraySpeed = true, false, false })
	if got := a.pollEvery(); got != 5*time.Second {
		t.Errorf("polls every %v with nothing shown", got)
	}
	set(func(s *settings.Settings) { s.TraySpeed = true })
	if got := a.pollEvery(); got != traySpeedEvery {
		t.Errorf("polls every %v with the speed in the menu bar", got)
	}
	set(func(s *settings.Settings) { s.Paused = true })
	if got := a.pollEvery(); got != 5*time.Second {
		t.Errorf("polls every %v with the speed asked for but nothing taken over", got)
	}
	// Not taken over, an app pointed at the port still has rates to tell.
	a.lastTick = Tick{Active: 1}
	if got := a.pollEvery(); got != traySpeedEvery {
		t.Errorf("polls every %v with the speed asked for and a connection through the port", got)
	}
	a.lastTick = Tick{}
	set(func(s *settings.Settings) { s.Paused = false })
	a.winShown.Store(true)
	if got := a.pollEvery(); got != time.Second {
		t.Errorf("polls every %v with the window shown", got)
	}
}
