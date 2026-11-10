package core

import (
	"testing"

	"github.com/oboo/moho/internal/devnet"
)

// Without TUN, without the built-in DNS switch and with no developer
// networks, the core used to be given no resolver at all. Then a node whose
// server is a domain could not be dialled, and one with ECH — which asks DNS
// for its config before the TLS handshake — could not be dialled either,
// while every other provider was fine.
func TestBuildConfigKeepsAResolver(t *testing.T) {
	in := devInput("rule", devnet.Overlay{})
	dns, _ := build(t, in)["dns"].(map[string]any)
	if dns == nil || dns["enable"] != true {
		t.Fatalf("no resolver without TUN: %v", dns)
	}
	// Normal mode: with no tunnel there is nothing to map fake addresses
	// back from, and nothing may listen on the machine's port 53 either.
	if dns["enhanced-mode"] != "normal" || dns["listen"] != nil {
		t.Fatalf("%+v", dns)
	}
	if got := strs(dns["nameserver"]); len(got) == 0 {
		t.Fatalf("no nameserver: %v", dns)
	}

	// A settings file with none chosen still leaves a resolver that works.
	empty := devInput("rule", devnet.Overlay{})
	empty.Options.Nameservers = nil
	dns, _ = build(t, empty)["dns"].(map[string]any)
	if got := strs(dns["nameserver"]); len(got) == 0 {
		t.Fatalf("no nameserver and no default: %v", dns)
	}
}

// TUN and the gateway keep their own resolver: this did not touch them.
func TestBuildConfigResolverWithTun(t *testing.T) {
	in := devInput("rule", devnet.Overlay{})
	in.Tun = true
	dns, _ := build(t, in)["dns"].(map[string]any)
	if dns == nil || dns["listen"] == nil {
		t.Fatalf("TUN lost its resolver: %v", dns)
	}
	if dns["enhanced-mode"] != "fake-ip" {
		t.Fatalf("TUN mode changed: %v", dns["enhanced-mode"])
	}
}
