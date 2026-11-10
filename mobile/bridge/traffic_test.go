package bridge

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/oboo/moho/internal/locate"
)

func TestRouteTrafficRanges(t *testing.T) {
	path := filepath.Join(t.TempDir(), routeTrafficFile)
	now := time.Date(2026, 3, 31, 12, 0, 0, 0, time.Local)
	day := func(back int) string { return now.AddDate(0, 0, -back).Format(time.DateOnly) }

	a := loadRouteTraffic(path)
	a.add(day(0), "ai", 1)
	a.add(day(6), "ai", 10)
	a.add(day(7), "ai", 100)    // in the 30 days only
	a.add(day(29), "dev", 1000) // the 30th day
	a.add(day(30), "dev", 9999) // one too many
	a.add(day(40), "ai", 77)    // old enough to be dropped
	a.saveIfDue(now)

	b := loadRouteTraffic(path)
	if got := b.sum(7, now); got["ai"] != 11 || got["dev"] != 0 {
		t.Fatalf("7 days: %v", got)
	}
	if got := b.sum(30, now); got["ai"] != 111 || got["dev"] != 1000 {
		t.Fatalf("30 days: %v", got)
	}
	if _, kept := b.days[day(40)]; kept {
		t.Fatal("a day past the longest range was kept")
	}
}

func TestRouteTrafficSurvivesRestart(t *testing.T) {
	x := testBridge(t)
	today := time.Now().Format(time.DateOnly)
	x.mu.Lock()
	days := x.trafficDays()
	days.add(today, "ai", 300)
	days.add(today, "dev", 100)
	days.saveIfDue(time.Now())
	x.traffic, x.shareDay = nil, ""
	x.mu.Unlock()

	got := x.todayShare(nil)
	if got["ai"] != .75 || got["dev"] != .25 {
		t.Fatalf("today after a restart: %v", got)
	}
}

func TestYouIsTheMeasuredPlaceOnly(t *testing.T) {
	x := testBridge(t)
	if p := x.youPlace(); p != nil {
		t.Fatalf("known before any measurement: %+v", p)
	}
	path := filepath.Join(t.TempDir(), "locations.json")
	b, _ := json.Marshal(map[string]any{directNode: map[string]any{
		"source": locate.SourceProbe, "at": time.Now(),
		"place": map[string]any{"country": "JP", "city": "东京", "flag": "🇯🇵", "lat": 35.7, "lon": 139.7},
	}})
	if err := os.WriteFile(path, b, 0o600); err != nil {
		t.Fatal(err)
	}
	x.loc = locate.New(path)
	if p := x.youPlace(); p == nil || p.Lat != 35.7 || p.Lon != 139.7 {
		t.Fatalf("after a measurement: %+v", p)
	}
}

// With the tunnel not known to be down, a request from the app could leave
// through a node and put the phone at that node's exit.
func TestLocateYouWaitsForTheTunnel(t *testing.T) {
	x := testBridge(t)
	x.mu.Lock()
	x.vpn = "connecting"
	x.mu.Unlock()
	x.locateYou()
	x.mu.Lock()
	defer x.mu.Unlock()
	if !x.youProbed.IsZero() {
		t.Fatal("measured the phone while the tunnel was coming up")
	}
}
