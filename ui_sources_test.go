package main

import (
	"testing"
	"time"

	"github.com/egoist/mygo/ui"

	"github.com/oboo/moho/internal/devtools"
)

// sourcesFixture: npm on a mirror that answers, the rest official; with
// down, pip is on a mirror that does not answer while another one does.
func sourcesFixture(down bool) []devtools.Source {
	list := devtools.Sources(devtools.Options{Home: "/nonexistent"})
	for i := range list {
		s := &list[i]
		s.Probes = []devtools.Probe{{Mirror: "", Ms: 142, OK: true}}
		for j, m := range s.Mirrors {
			s.Probes = append(s.Probes, devtools.Probe{Mirror: m, Ms: 31 + 9*j, OK: true})
		}
		switch {
		case s.ID == devtools.SourceNPM:
			s.Mirror = s.Mirrors[0]
		case s.ID == devtools.SourcePyPI && down:
			s.Mirror = s.Mirrors[0]
			s.Probes[1] = devtools.Probe{Mirror: s.Mirrors[0]}
		}
	}
	return list
}

// When every source answers the section is one quiet sentence, and the
// cell says nothing.
func TestSourcesQuiet(t *testing.T) {
	u := fixtureUI()
	u.snap.Sources = sourcesFixture(false)
	tt := ui.NewTesterWith(u.view, 1060, 760, ui.TesterOptions{})
	tt.Advance(time.Second)
	if tt.HasText("连不上") {
		t.Error("the cell warns with nothing down")
	}
	u.home.x = "mode"
	tt.Frame()
	tt.Advance(2 * time.Second)
	toolsShot(t, tt, "sources-quiet")
	for _, s := range []string{"依赖源", "4 个下载源都通", "查看"} {
		if !tt.HasText(s) {
			t.Errorf("no %q", s)
		}
	}
	for _, s := range []string{"一键修复", "换源", "registry.npmmirror.com"} {
		if tt.HasText(s) {
			t.Errorf("%q shows before it is asked for", s)
		}
	}
}

// A source that is down is said on the cell, and the section offers the
// fix without opening anything.
func TestSourcesDown(t *testing.T) {
	u := fixtureUI()
	u.snap.Sources = sourcesFixture(true)
	tt := ui.NewTesterWith(u.view, 1060, 760, ui.TesterOptions{})
	tt.Advance(time.Second)
	toolsShot(t, tt, "sources-card")
	if !tt.HasText("pip 的下载源连不上 ›") {
		t.Fatal("the cell doesn't say a source is down")
	}
	if err := tt.Click("pip 的下载源连不上 ›"); err != nil {
		t.Fatal(err)
	}
	tt.Advance(2 * time.Second)
	if u.home.x != "mode" || !u.home.srcList {
		t.Fatalf("the line opened %q, list %v", u.home.x, u.home.srcList)
	}
	toolsShot(t, tt, "sources-down")
	for _, s := range []string{"pip 的下载源连不上", "一键修复", "镜像 pypi.tuna.tsinghua.edu.cn · 连不上", "修复", "镜像 registry.npmmirror.com · 31 ms", "官方源 · 走「开发」 · 142 ms"} {
		if !tt.HasText(s) {
			t.Errorf("no %q", s)
		}
	}
	// The fix is the fastest mirror that answers.
	src := u.snap.Sources[1]
	if kind, m := u.sourceFix(src); kind != devtools.FixMirror || m != src.Mirrors[1] {
		t.Errorf("fix: %q %q", kind, m)
	}
}

// The other ways show only for the source asked about.
func TestSourceWays(t *testing.T) {
	u := fixtureUI()
	u.snap.Sources = sourcesFixture(false)
	u.home.x, u.home.srcList = "mode", true
	tt := ui.NewTesterWith(u.view, 1060, 760, ui.TesterOptions{})
	tt.Advance(2 * time.Second)
	if tt.HasText("mirrors.cloud.tencent.com") {
		t.Error("the ways show before they are asked for")
	}
	u.home.srcOpen = devtools.SourceNPM
	tt.Frame()
	tt.Advance(time.Second)
	toolsShot(t, tt, "sources-ways")
	for _, s := range []string{"官方源 · 走「开发」", "registry.npmmirror.com", "mirrors.cloud.tencent.com", "repo.huaweicloud.com", "使用中", "40 ms"} {
		if !tt.HasText(s) {
			t.Errorf("no %q", s)
		}
	}
	if tt.HasText("pypi.tuna.tsinghua.edu.cn") {
		t.Error("another source's mirrors show")
	}
}

// An official source that is down because its service was sent directly
// is put right by its route, not by a mirror the user did not choose.
func TestSourceFixRoute(t *testing.T) {
	u := fixtureUI()
	u.snap.Sources = sourcesFixture(false)
	s := u.snap.Settings
	s.Routing.MoveService(s.Rules, devtools.SourceGo, "direct")
	u.snap.Settings = s
	for i := range u.snap.Sources {
		if u.snap.Sources[i].ID == devtools.SourceGo {
			u.snap.Sources[i].Probes[0] = devtools.Probe{}
			if kind, _ := u.sourceFix(u.snap.Sources[i]); kind != devtools.FixRoute {
				t.Errorf("fix: %q", kind)
			}
		}
	}
}
