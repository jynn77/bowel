package bridge

import (
	"encoding/json"
	"os"
	"path/filepath"
	"time"
)

const (
	routeTrafficFile = "route-traffic.json"
	// keepDays is how far back the per-route totals go; the longest range is 30.
	keepDays = 32
	// trafficSaveEvery bounds what a killed app can lose.
	trafficSaveEvery = 20 * time.Second
	// rangesEvery is how often the 7 and 30 day totals go to the page.
	rangesEvery = 5 * time.Second
)

// routeTraffic is what each route carried, by day, kept in a small file. The
// core only counts since it started and the monitor only keeps today and this
// month in total, so nothing else can say what a route carried last week.
type routeTraffic struct {
	path  string
	days  map[string]map[string]int64
	dirty bool
	saved time.Time
}

func loadRouteTraffic(path string) *routeTraffic {
	t := &routeTraffic{path: path, days: map[string]map[string]int64{}}
	if b, err := os.ReadFile(path); err == nil {
		if json.Unmarshal(b, &t.days) != nil || t.days == nil {
			t.days = map[string]map[string]int64{}
		}
	}
	return t
}

func (t *routeTraffic) add(day, route string, bytes int64) {
	m := t.days[day]
	if m == nil {
		m = map[string]int64{}
		t.days[day] = m
	}
	m[route] += bytes
	t.dirty = true
}

// day returns a copy of one day's totals.
func (t *routeTraffic) day(day string) map[string]int64 {
	out := make(map[string]int64, len(t.days[day]))
	for k, v := range t.days[day] {
		out[k] = v
	}
	return out
}

// sum adds up the last n calendar days, today included.
func (t *routeTraffic) sum(n int, now time.Time) map[string]int64 {
	out := map[string]int64{}
	for i := 0; i < n; i++ {
		for id, v := range t.days[now.AddDate(0, 0, -i).Format(time.DateOnly)] {
			out[id] += v
		}
	}
	return out
}

func (t *routeTraffic) saveIfDue(now time.Time) {
	if !t.dirty || now.Sub(t.saved) < trafficSaveEvery {
		return
	}
	oldest := now.AddDate(0, 0, -keepDays).Format(time.DateOnly)
	for d := range t.days {
		if d < oldest {
			delete(t.days, d)
		}
	}
	t.saved = now
	b, err := json.Marshal(t.days)
	if err != nil {
		return
	}
	tmp := t.path + ".tmp"
	if os.WriteFile(tmp, b, 0o600) == nil && os.Rename(tmp, t.path) == nil {
		t.dirty = false
	}
}

// trafficDays loads the totals the first time they're needed. x.mu is held.
func (x *bridge) trafficDays() *routeTraffic {
	if x.traffic == nil {
		x.traffic = loadRouteTraffic(filepath.Join(x.dir, routeTrafficFile))
	}
	return x.traffic
}

// trafficRanges is the totals to send with this tick, nil between sends.
func (x *bridge) trafficRanges(now time.Time) *rangesData {
	x.mu.Lock()
	defer x.mu.Unlock()
	if now.Sub(x.rangesSent) < rangesEvery {
		return nil
	}
	x.rangesSent = now
	t := x.trafficDays()
	return &rangesData{Week: t.sum(7, now), Month: t.sum(30, now)}
}
