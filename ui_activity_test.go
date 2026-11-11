package main

import (
	"fmt"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
)

// actFixtureConns is a realistic set of connections for the activity page.
func actFixtureConns() []Conn {
	now := time.Now()
	type spec struct {
		app, proc, host, ip string
		port                int
		net, rule, payload  string
		route, node         string
		chain               []string
		down, up            int64
		rate                int64
		age                 time.Duration
		status, err         string
	}
	specs := []spec{
		{"Arc", "/Applications/Arc.app/Contents/MacOS/Arc", "rr3---sn-i3b7kn.googlevideo.com", "142.250.66.78", 443, "tcp", "GEOSITE", "youtube", "media", "新加坡 01", []string{"流媒体", "新加坡 01"}, 117e6, 1.1e6, 1.05e6, 61 * time.Second, "active", ""},
		{"Arc", "/Applications/Arc.app/Contents/MacOS/Arc", "www.youtube.com", "142.250.66.46", 443, "tcp", "GEOSITE", "youtube", "media", "新加坡 01", []string{"流媒体", "新加坡 01"}, 50.6e6, 630e3, 160e3, 3 * time.Minute, "active", ""},
		{"Docker", "/Applications/Docker.app/Contents/MacOS/com.docker.backend", "registry-1.docker.io", "54.227.20.253", 443, "tcp", "DOMAIN-SUFFIX", "docker.io", "dev", "东京 01", []string{"开发", "东京 01"}, 398e6, 1.2e6, 210e3, 40 * time.Second, "active", ""},
		{"Claude", "/Applications/Claude.app/Contents/MacOS/Claude", "api.anthropic.com", "160.79.104.10", 443, "tcp", "DOMAIN-SUFFIX", "anthropic.com", "ai", "洛杉矶 VPS", []string{"AI 服务", "洛杉矶 VPS"}, 2.2e6, 860e3, 84e3, 12 * time.Second, "active", ""},
		{"Spotify", "/Applications/Spotify.app/Contents/MacOS/Spotify", "audio-ak.spotifycdn.com", "23.45.12.9", 443, "tcp", "GEOSITE", "spotify", "media", "新加坡 01", []string{"流媒体", "新加坡 01"}, 19e6, 120e3, 41e3, 7 * time.Minute, "active", ""},
		{"Slack", "/Applications/Slack.app/Contents/MacOS/Slack", "wss-primary.slack.com", "3.120.44.8", 443, "tcp", "MATCH", "", "default", "香港 03 IPLC", []string{"默认", "香港 03 IPLC"}, 430e3, 59e3, 12e3, 2 * time.Hour, "active", ""},
		{"Arc", "/Applications/Arc.app/Contents/MacOS/Arc", "x.com", "104.244.42.1", 443, "tcp", "MATCH", "", "default", "东京 02", []string{"默认", "东京 02"}, 0, 0, 0, 5 * time.Second, "failed", "dial tcp 104.244.42.1:443: i/o timeout"},
		{"WeChat", "/Applications/WeChat.app/Contents/MacOS/WeChat", "szshort.weixin.qq.com", "183.3.234.107", 443, "tcp", "GEOSITE", "cn", "direct", "", []string{"DIRECT"}, 88e3, 21e3, 900, 25 * time.Minute, "active", ""},
		{"git", "/usr/bin/git", "github.com", "20.205.243.166", 22, "tcp", "DOMAIN-SUFFIX", "github.com", "dev", "东京 01", []string{"开发", "东京 01"}, 6.4e6, 210e3, 0, 50 * time.Second, "closed", ""},
		{"Cursor", "/Applications/Cursor.app/Contents/MacOS/Cursor", "api2.cursor.sh", "35.190.27.112", 443, "tcp", "DOMAIN-SUFFIX", "cursor.sh", "ai", "洛杉矶 VPS", []string{"AI 服务", "洛杉矶 VPS"}, 1.4e6, 380e3, 9e3, 4 * time.Minute, "active", ""},
		{"Arc", "/Applications/Arc.app/Contents/MacOS/Arc", "www.google.com", "142.250.66.36", 443, "udp", "MATCH", "", "default", "香港 03 IPLC", []string{"默认", "香港 03 IPLC"}, 920e3, 48e3, 3e3, 90 * time.Second, "active", ""},
		{"mDNSResponder", "/usr/sbin/mDNSResponder", "", "8.8.8.8", 53, "udp", "IP-CIDR", "8.8.8.0/24", "default", "香港 03 IPLC", []string{"默认", "香港 03 IPLC"}, 1200, 400, 0, 2 * time.Second, "closed", ""},
		{"Telegram", "/Applications/Telegram.app/Contents/MacOS/Telegram", "149.154.167.51", "149.154.167.51", 443, "tcp", "IP-CIDR", "149.154.160.0/20", "default", "香港 02", []string{"默认", "香港 02"}, 3.1e6, 640e3, 2.2e3, 38 * time.Minute, "active", ""},
		{"Notion", "/Applications/Notion.app/Contents/MacOS/Notion", "www.notion.so", "104.18.23.110", 443, "tcp", "MATCH", "", "default", "香港 03 IPLC", []string{"默认", "香港 03 IPLC"}, 2.6e6, 190e3, 0, 3 * time.Minute, "closed", ""},
		{"Docker", "/Applications/Docker.app/Contents/MacOS/com.docker.backend", "production.cloudflare.docker.com", "104.16.98.215", 443, "tcp", "DOMAIN-SUFFIX", "docker.com", "dev", "东京 01", []string{"开发", "东京 01"}, 212e6, 400e3, 0, 2 * time.Minute, "closed", ""},
		{"Netflix", "/Applications/Netflix.app/Contents/MacOS/Netflix", "ipv4-c001-hkg001.1.oca.nflxvideo.net", "45.57.90.1", 443, "tcp", "GEOSITE", "netflix", "media", "新加坡 01", []string{"流媒体", "新加坡 01"}, 0, 0, 0, 3 * time.Second, "failed", "connect failed: connection refused"},
		{"Zoom", "/Applications/zoom.us.app/Contents/MacOS/zoom.us", "zoom.us", "170.114.52.2", 8801, "udp", "GEOSITE", "zoom", "default", "香港 02", []string{"默认", "香港 02"}, 14e6, 11e6, 180e3, 22 * time.Minute, "active", ""},
		{"Safari", "/Applications/Safari.app/Contents/MacOS/Safari", "www.bilibili.com", "119.3.70.188", 443, "tcp", "GEOSITE", "cn", "direct", "", []string{"DIRECT"}, 5.8e6, 92e3, 64e3, 6 * time.Minute, "active", ""},
		{"npm", "/opt/homebrew/bin/node", "registry.npmjs.org", "104.16.2.35", 443, "tcp", "DOMAIN-SUFFIX", "npmjs.org", "dev", "东京 01", []string{"开发", "东京 01"}, 31e6, 820e3, 0, 15 * time.Second, "closed", ""},
		{"ChatGPT", "/Applications/ChatGPT.app/Contents/MacOS/ChatGPT", "chatgpt.com", "104.18.32.47", 443, "tcp", "DOMAIN-SUFFIX", "chatgpt.com", "ai", "洛杉矶 VPS", []string{"AI 服务", "洛杉矶 VPS"}, 620e3, 140e3, 22e3, 9 * time.Minute, "active", ""},
	}
	out := make([]Conn, len(specs))
	for i, s := range specs {
		c := Conn{
			ID: fmt.Sprintf("c%02d", i+1), App: s.app, Process: s.proc, Host: s.host, IP: s.ip, Port: s.port, Network: s.net,
			Rule: s.rule, Payload: s.payload, Route: s.route, Node: s.node, Chain: s.chain,
			Down: s.down, Up: s.up, DownRate: s.rate, UpRate: s.rate / 20, Start: now.Add(-s.age), Status: s.status, Error: s.err,
		}
		if s.status != "active" {
			c.End = now.Add(-s.age / 3)
			c.DownRate, c.UpRate = 0, 0
		}
		out[i] = c
	}
	return out
}

func actFixtureLogs() []LogLine {
	base := time.Date(2026, 11, 7, 19, 24, 10, 0, time.Local)
	texts := []struct{ lv, text string }{
		{"info", "[TCP] Arc → rr3---sn-i3b7kn.googlevideo.com:443 命中 GEOSITE(youtube) → 流媒体[新加坡 01]"},
		{"info", "[TCP] Claude → api.anthropic.com:443 命中 DOMAIN-SUFFIX(anthropic.com) → AI 服务[洛杉矶 VPS]"},
		{"warning", "东京 02 健康检查超时（连续第 3 次）"},
		{"error", "[TCP] Arc → x.com:443 拨号失败：东京 02 i/o timeout"},
		{"info", "默认 自动切换：东京 02 → 香港 03 IPLC（41 ms）"},
		{"debug", "[DNS] x.com → 104.244.42.1（fake-ip 198.18.0.42）"},
		{"info", "[TCP] Docker → registry-1.docker.io:443 命中 DOMAIN-SUFFIX(docker.io) → 开发[东京 01]"},
		{"info", "[UDP] Zoom → zoom.us:8801 命中 GEOSITE(zoom) → 默认[香港 02]"},
		{"debug", "[DNS] chatgpt.com → 104.18.32.47"},
		{"warning", "机场 B 流量已用 94%"},
		{"info", "[TCP] Safari → www.bilibili.com:443 命中 GEOSITE(cn) → 直连"},
		{"error", "[TCP] Netflix → ipv4-c001-hkg001.1.oca.nflxvideo.net:443 connect failed: connection refused"},
	}
	out := make([]LogLine, len(texts))
	for i, t := range texts {
		out[i] = LogLine{Seq: int64(i + 1), Time: base.Add(time.Duration(i) * 700 * time.Millisecond), Level: t.lv, Text: t.text}
	}
	return out
}

func actUI() *UI {
	u := fixtureUI()
	u.page = "activity"
	u.act.apply(actFixtureConns(), actFixtureLogs())
	return u
}

func TestActivityList(t *testing.T) {
	u := actUI()
	tt := ui.NewTester(u.view, 1060, 760)
	tt.Advance(time.Second)
	shot(t, tt, "activity")
	for _, want := range []string{"13 个连接", "活跃 13", "已关闭 5", "失败 2", "全部", "registry-1.docker.io", "所有路由", "AI 服务"} {
		if !hasTextPart(tt, want) {
			t.Errorf("activity lacks %q: %q", want, tt.Texts())
		}
	}
	// Only active rows by default.
	if hasTextPart(tt, "github.com") {
		t.Errorf("closed github.com shows under 活跃")
	}
	// Sorted by rate, highest first: googlevideo above docker above youtube.
	order := []string{"rr3---sn-i3b7kn.googlevideo.com", "registry-1.docker.io", "www.youtube.com"}
	if !above(tt, order...) {
		t.Errorf("rows not sorted by rate: %v", ys(tt, order...))
	}
	// Ascending after a click on 速率.
	if err := tt.Click("速率 ↓"); err != nil {
		t.Fatal(err)
	}
	if !u.act.asc || !above(tt, "www.youtube.com", "registry-1.docker.io") {
		t.Errorf("速率 did not reverse: asc %v %v", u.act.asc, ys(tt, order...))
	}
	// By duration: Slack's two hours first.
	if err := tt.Click("时长"); err != nil {
		t.Fatal(err)
	}
	rows := u.actFiltered()
	if u.act.sort != "dur" || rows[0].App != "Slack" {
		t.Errorf("时长 sort: %q first is %s", u.act.sort, rows[0].App)
	}
	// All, then failed.
	if err := tt.Click("全部"); err != nil {
		t.Fatal(err)
	}
	if !hasTextPart(tt, "github.com") {
		t.Errorf("全部 lacks a closed connection")
	}
	if err := tt.Click("失败 2"); err != nil {
		t.Fatal(err)
	}
	if n := len(u.actFiltered()); n != 2 || !hasTextPart(tt, "i/o timeout") {
		t.Errorf("失败: %d rows, %q", n, tt.Texts())
	}
	shot(t, tt, "activity-failed")
}

// TestActivityBlocked: what a rule refused has its own filter and says so
// where the others give a duration.
func TestActivityBlocked(t *testing.T) {
	u := actUI()
	now := time.Now()
	u.act.apply(append(actFixtureConns(), Conn{ID: "blocked-1", App: "Arc", Host: "ads.example.com", Port: 443, Network: "tcp", Rule: "DomainSuffix",
		Payload: "ads.example.com", Route: "reject", Chain: []string{"拦截"}, Start: now, End: now, Status: "blocked"}), actFixtureLogs())
	tt := ui.NewTester(u.view, 1060, 760)
	tt.Advance(time.Second)
	if hasTextPart(tt, "ads.example.com") {
		t.Errorf("a blocked connection shows under 活跃")
	}
	if err := tt.Click("已拦截 1"); err != nil {
		t.Fatal(err)
	}
	tt.Advance(time.Second)
	shot(t, tt, "activity-blocked")
	for _, want := range []string{"ads.example.com", "DomainSuffix", "拦截", "已拦截"} {
		if !hasTextPart(tt, want) {
			t.Errorf("list lacks %q: %q", want, tt.Texts())
		}
	}
	if hasTextPart(tt, "registry-1.docker.io") {
		t.Errorf("other connections show under 已拦截")
	}
}

func TestActivitySearch(t *testing.T) {
	u := actUI()
	u.act.status = slices.Index(actStatuses, "all")
	tt := ui.NewTester(u.view, 1060, 760)
	tt.Advance(time.Second)
	u.act.q = "app:Arc"
	tt.Frame()
	rows := u.actFiltered()
	if len(rows) != 4 {
		t.Errorf("app:Arc keeps %d rows", len(rows))
	}
	for _, r := range rows {
		if r.App != "Arc" {
			t.Errorf("app:Arc keeps %s", r.App)
		}
	}
	if hasTextPart(tt, "registry-1.docker.io") || !hasTextPart(tt, "x.com") {
		t.Errorf("app:Arc shows %q", tt.Texts())
	}
	shot(t, tt, "activity-search")
	for q, want := range map[string]int{
		"route:ai":              3,
		"route:流媒体":             4,
		"node:东京":               5,
		"host:docker":           2,
		"直连":                    2,
		"anthropic":             1,
		"app:arc route:其他流量":      2,
		"youtube 142.250.66.46": 1,
		"nothing-like-this":     0,
	} {
		u.act.q = q
		if n := len(u.actFiltered()); n != want {
			t.Errorf("%q keeps %d rows, want %d", q, n, want)
		}
	}
	u.act.q = "nothing-like-this"
	tt.Frame()
	if !tt.HasText("没有符合条件的连接") {
		t.Errorf("no empty state: %q", tt.Texts())
	}
	// Filters: route chip, network, app.
	u.act.q = ""
	tt.Frame()
	if err := tt.Click("开发"); err != nil {
		t.Fatal(err)
	}
	if n := len(u.actFiltered()); n != 4 || u.act.route != "dev" {
		t.Errorf("开发 keeps %d rows (route %q)", n, u.act.route)
	}
	u.act.route = ""
	u.act.net = "UDP"
	if n := len(u.actFiltered()); n != 3 {
		t.Errorf("UDP keeps %d rows", n)
	}
	u.act.net = actAllNet
	u.act.filterApp("Docker")
	tt.Frame()
	if n := len(u.actFiltered()); n != 2 || !tt.HasText("Docker") {
		t.Errorf("filterApp(Docker) keeps %d rows", n)
	}
}

func TestActivityDetail(t *testing.T) {
	u := actUI()
	tt := ui.NewTester(u.view, 1060, 760)
	tt.Advance(time.Second)
	if err := tt.Click("api.anthropic.com:443"); err != nil {
		t.Fatal(err)
	}
	tt.Advance(time.Second)
	if u.act.open != "c04" {
		t.Fatalf("open %q", u.act.open)
	}
	for _, want := range []string{"/Applications/Claude.app/Contents/MacOS/Claude", "160.79.104.10", "DOMAIN-SUFFIX,anthropic.com", "洛杉矶 · Hysteria2", "复制域名", "相关日志", "断开"} {
		if !hasTextPart(tt, want) {
			t.Errorf("detail lacks %q: %q", want, tt.Texts())
		}
	}
	shot(t, tt, "activity-open")
	if err := tt.Click("复制域名"); err != nil {
		t.Fatal(err)
	}
	if got := tt.Clipboard(); got != "api.anthropic.com" {
		t.Errorf("clipboard %q", got)
	}
	// The failed one shows its error.
	u.act.status, u.act.open = 2, "c07"
	tt.Frame()
	tt.Advance(time.Second)
	if !hasTextPart(tt, "dial tcp 104.244.42.1:443: i/o timeout") {
		t.Errorf("failed detail lacks the error: %q", tt.Texts())
	}
	shot(t, tt, "activity-open-failed")
	// 相关日志 goes to the logs, filtered by the host.
	u.act.status, u.act.open = 0, "c04"
	tt.Frame()
	if err := tt.Click("相关日志"); err != nil {
		t.Fatal(err)
	}
	tt.Advance(time.Second)
	if !u.act.logTab || u.act.logQ != "api.anthropic.com" || len(u.act.filteredLogs()) != 1 {
		t.Errorf("相关日志: tab %v q %q, %d lines", u.act.logTab, u.act.logQ, len(u.act.filteredLogs()))
	}
}

func TestActivityClose(t *testing.T) {
	u := actUI()
	tt := ui.NewTester(u.view, 1060, 760)
	tt.Advance(time.Second)
	// One row's ×.
	before := u.act.count("active")
	if err := tt.Click("断开"); err != nil {
		t.Fatal(err)
	}
	if u.act.count("active") != before-1 || u.act.open != "" {
		t.Errorf("× closed %d, opened %q", before-u.act.count("active"), u.act.open)
	}
	// 断开筛选结果 waits for a second click, then closes what the filter keeps.
	u.act.q = "route:媒体"
	tt.Frame()
	if err := tt.Click("断开筛选结果"); err != nil {
		t.Fatal(err)
	}
	if !tt.HasText("确认断开 2 个？") {
		t.Fatalf("not armed: %q", tt.Texts())
	}
	shot(t, tt, "activity-armed")
	tt.Advance(3500 * time.Millisecond)
	if !tt.HasText("断开筛选结果") {
		t.Errorf("did not disarm after 3 s: %q", tt.Texts())
	}
	tt.Click("断开筛选结果")
	if err := tt.Click("确认断开 2 个？"); err != nil {
		t.Fatal(err)
	}
	for _, c := range u.act.conns {
		if c.Route == "media" && c.Status == "active" {
			t.Errorf("%s still active", c.Host)
		}
	}
	if u.act.count("active") != before-3 {
		t.Errorf("closed %d in all", before-u.act.count("active"))
	}
}

func TestActivityLogs(t *testing.T) {
	for _, dark := range []bool{false, true} {
		u := actUI()
		u.act.showLogs()
		tt := ui.NewTesterWith(u.view, 1060, 760, ui.TesterOptions{Dark: dark})
		tt.Advance(time.Second)
		suffix := map[bool]string{false: "", true: "-dark"}[dark]
		shot(t, tt, "activity-logs"+suffix)
		if !hasTextPart(tt, "东京 02 健康检查超时") || !tt.HasText("WARN") || !tt.HasText("ERR") {
			t.Errorf("logs: %q", tt.Texts())
		}
		if err := tt.Click("错误"); err != nil {
			t.Fatal(err)
		}
		if n := len(u.act.filteredLogs()); n != 2 || hasTextPart(tt, "健康检查") {
			t.Errorf("错误 keeps %d lines", n)
		}
		u.act.level, u.act.logQ = 0, "dns"
		if n := len(u.act.filteredLogs()); n != 2 {
			t.Errorf("dns keeps %d lines", n)
		}
		if dark {
			u.act.logTab, u.act.logQ = false, ""
			u.act.open = "c01"
			tt.Frame()
			tt.Advance(time.Second)
			shot(t, tt, "activity-dark")
		}
	}
}

func TestActivityOff(t *testing.T) {
	u := actUI()
	u.snap.Takeover.On, u.snap.State = false, StateOff
	// Nothing observed: no connection, no event, no history.
	u.act.conns, u.tick, u.home.hist = nil, Tick{}, nil
	tt := ui.NewTester(u.view, 1060, 760)
	tt.Advance(time.Second)
	if !hasTextPart(tt, "流量现在不经过 bowel") || !tt.HasText("0 个连接") {
		t.Errorf("off lacks its note: %q", tt.Texts())
	}
	shot(t, tt, "activity-off")
}

func TestActivityNarrow(t *testing.T) {
	u := actUI()
	u.act.open = "c01"
	tt := ui.NewTester(u.view, 960, 660)
	tt.Advance(time.Second)
	shot(t, tt, "activity-narrow")
}

func TestActivityApplyKeepsLast(t *testing.T) {
	var a activityUI
	var lines []LogLine
	for i := 1; i <= 2500; i++ {
		lines = append(lines, LogLine{Seq: int64(i), Text: "x"})
	}
	a.apply(nil, lines[:1500])
	a.apply(nil, lines[1400:]) // overlapping lines come once
	if len(a.logs) != actMaxLogs || a.logs[0].Seq != 501 || a.lastSeq != 2500 {
		t.Errorf("%d lines from %d, last %d", len(a.logs), a.logs[0].Seq, a.lastSeq)
	}
}

func TestActivityClearLogs(t *testing.T) {
	var a activityUI
	a.apply(nil, []LogLine{{Seq: 1, Text: "a"}, {Seq: 2, Text: "b"}})
	a.clearLogs()
	if len(a.logs) != 0 || a.lastSeq != 2 {
		t.Fatalf("after clear: %d lines, last %d", len(a.logs), a.lastSeq)
	}
	// The poller asks for what comes after lastSeq, so a fetch that still
	// carries the cleared lines must not put them back.
	a.apply(nil, []LogLine{{Seq: 2, Text: "b"}})
	if len(a.logs) != 0 {
		t.Errorf("cleared lines came back: %+v", a.logs)
	}
	a.apply(nil, []LogLine{{Seq: 3, Text: "c"}})
	if len(a.logs) != 1 || a.logs[0].Seq != 3 {
		t.Errorf("new lines after a clear: %+v", a.logs)
	}
}

func TestActivityPoller(t *testing.T) {
	var mu sync.Mutex // the main thread
	var a activityUI
	wanted := true
	fetches := 0
	update := func(fn func()) { mu.Lock(); defer mu.Unlock(); fn() }
	a.polling = true
	done := make(chan struct{})
	go func() {
		a.pollWith(func(after int64) ([]Conn, []LogLine) {
			mu.Lock()
			fetches++
			n := fetches
			if n == 3 {
				wanted = false
			}
			mu.Unlock()
			return actFixtureConns()[:n], []LogLine{{Seq: after + 1, Text: "line"}}
		}, update, func() bool { return wanted }, time.Millisecond)
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("the poller did not stop")
	}
	if a.polling || len(a.conns) != 2 || len(a.logs) != 2 || a.lastSeq != 2 {
		t.Errorf("polling %v, %d conns, %d logs, last %d", a.polling, len(a.conns), len(a.logs), a.lastSeq)
	}
}

// ---- Helpers ----

func hasTextPart(tt *ui.Tester, s string) bool {
	for _, t := range tt.Texts() {
		if strings.Contains(t, s) {
			return true
		}
	}
	return false
}

func ys(tt *ui.Tester, texts ...string) []float32 {
	var out []float32
	for _, s := range texts {
		r, ok := findPart(tt, s)
		if !ok {
			out = append(out, -1)
			continue
		}
		out = append(out, r.Y)
	}
	return out
}

func above(tt *ui.Tester, texts ...string) bool {
	y := ys(tt, texts...)
	for i := range y {
		if y[i] < 0 || i > 0 && y[i] <= y[i-1] {
			return false
		}
	}
	return true
}

func findPart(tt *ui.Tester, s string) (ui.Rect, bool) {
	for _, t := range tt.Texts() {
		if strings.Contains(t, s) {
			r, ok := tt.Find(t)
			return r, ok
		}
	}
	return ui.Rect{}, false
}

// The bin drops what the page lists. What has ended goes at once; what is
// still running needs a second click, since hiding it hides something live.
func TestActivityClearList(t *testing.T) {
	u := actUI()
	a := &u.act

	// 失败: gone on the first click.
	a.status = slices.Index(actStatuses, "failed")
	if len(u.actFiltered()) == 0 {
		t.Fatal("no failed connections to clear")
	}
	u.tapClear()
	if n := len(u.actFiltered()); n != 0 {
		t.Fatalf("%d failed connections left after one click", n)
	}
	// And they stay gone when the poller brings them back.
	u.act.apply(actFixtureConns(), nil)
	if n := len(u.actFiltered()); n != 0 {
		t.Fatalf("%d failed connections came back with the next poll", n)
	}

	// 活跃: the first click only arms.
	a.status = slices.Index(actStatuses, "active")
	live := len(u.actFiltered())
	if live == 0 {
		t.Fatal("no active connections to clear")
	}
	u.tapClear()
	if !a.clearArmed() {
		t.Fatal("clearing live connections was not armed")
	}
	if n := len(u.actFiltered()); n != live {
		t.Fatalf("%d of %d active connections went without a confirmation", live-n, live)
	}
	u.tapClear()
	if a.clearArmed() {
		t.Fatal("the second click left it armed")
	}
	if n := len(u.actFiltered()); n != 0 {
		t.Fatalf("%d active connections left after the confirmation", n)
	}

	// The log tab still clears lines, and asks nothing.
	a.status = slices.Index(actStatuses, "active")
	a.logTab = true
	u.tapClear()
	if len(a.logs) != 0 {
		t.Fatalf("%d log lines left after clearing", len(a.logs))
	}
}
