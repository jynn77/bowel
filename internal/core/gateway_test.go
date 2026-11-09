package core

import (
	"testing"

	"gopkg.in/yaml.v3"

	"github.com/oboo/moho/internal/routes"
)

// dnsOf builds the config of in and returns its dns section.
func dnsOf(t *testing.T, in Input) map[string]any {
	t.Helper()
	out, err := BuildConfig(in)
	if err != nil {
		t.Fatal(err)
	}
	var cfg map[string]any
	if err := yaml.Unmarshal(out, &cfg); err != nil {
		t.Fatal(err)
	}
	dns, _ := cfg["dns"].(map[string]any)
	return dns
}

// With the gateway on, the resolver answers the LAN on port 53, with fake
// IPs whatever the DNS mode chosen; without it, only TUN's own port.
func TestBuildConfigGateway(t *testing.T) {
	in := Input{Routes: routes.Template(), Mode: "rule", MixedPort: 7899, ProbePort: 7900, TestURL: "x", Tun: true}
	in.Options = DefaultOptions.Clone()
	in.DNSMode = "redir-host"

	dns := dnsOf(t, in)
	if dns["listen"] != "127.0.0.1:1053" || dns["enhanced-mode"] != "redir-host" {
		t.Fatalf("TUN alone: listen %v, mode %v", dns["listen"], dns["enhanced-mode"])
	}

	in.Gateway = true
	dns = dnsOf(t, in)
	if dns["enable"] != true || dns["listen"] != "0.0.0.0:53" || dns["enhanced-mode"] != "fake-ip" || dns["fake-ip-range"] == nil {
		t.Fatalf("gateway: %v", dns)
	}

	// The gateway needs TUN: without it, nothing listens on the LAN. The
	// core keeps a resolver of its own all the same — nodes need one for
	// their own names, ECH included — answering with real addresses, since
	// no tunnel would map fake ones back.
	in.Tun = false
	if dns := dnsOf(t, in); dns == nil || dns["enable"] != true || dns["listen"] != nil || dns["enhanced-mode"] != "redir-host" {
		t.Fatalf("gateway without TUN: %v", dns)
	}
	// The built-in DNS switch, without TUN, gives that resolver a loopback
	// address for apps to point at.
	in.DNS = true
	if dns := dnsOf(t, in); dns["listen"] != "127.0.0.1:1053" || dns["enhanced-mode"] != "redir-host" {
		t.Fatalf("gateway without TUN, DNS on: %v", dns)
	}
}

// Without TUN and without the DNS switch, the core still has a resolver: a
// node whose server is a domain, or one with ECH, cannot be dialled without
// one, and nothing else gave it that.
func TestBuildConfigKeepsAResolver(t *testing.T) {
	in := Input{Routes: routes.Template(), Mode: "rule", MixedPort: 7899, ProbePort: 7900, TestURL: "x"}
	in.Options = DefaultOptions.Clone()
	dns := dnsOf(t, in)
	if dns == nil || dns["enable"] != true {
		t.Fatalf("no resolver without TUN: %v", dns)
	}
	if dns["listen"] != nil {
		t.Fatalf("a resolver listens without TUN or the switch: %v", dns)
	}
	if dns["enhanced-mode"] != "redir-host" {
		t.Fatalf("fake IPs without a tunnel: %v", dns["enhanced-mode"])
	}
	if dns["nameserver"] == nil || dns["default-nameserver"] == nil {
		t.Fatalf("a resolver with nothing to ask: %v", dns)
	}
}
