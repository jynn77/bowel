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

	// The gateway needs TUN: without it, nothing listens on the LAN.
	in.Tun = false
	if dns := dnsOf(t, in); dns != nil && dns["listen"] != nil {
		t.Fatalf("gateway without TUN: %v", dns)
	}
	in.DNS = true
	if dns := dnsOf(t, in); dns["listen"] != nil || dns["enhanced-mode"] != "redir-host" {
		t.Fatalf("gateway without TUN, DNS on: %v", dns)
	}
}
