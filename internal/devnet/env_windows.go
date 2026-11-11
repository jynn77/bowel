package devnet

import (
	"context"

	"golang.org/x/sys/windows/registry"

	"github.com/oboo/moho/internal/netif"
)

// nrptKeys are where Windows keeps its name resolution policy table: the
// rules programs set (Tailscale's MagicDNS among them), and group
// policy's.
var nrptKeys = []string{
	`SYSTEM\CurrentControlSet\Services\Dnscache\Parameters\DnsPolicyConfig`,
	`SOFTWARE\Policies\Microsoft\Windows NT\DNSClient\DnsPolicyConfig`,
}

// osDetect reads the route table, and the DNS Windows splits by domain:
// the policy table's rules, and the suffix of each tunnel's adapter with
// that adapter's servers.
func osDetect(ctx context.Context) (routes []Route, resolvers []Resolver) {
	routes = fromSystem(netif.Routes(ctx))
	for _, path := range nrptKeys {
		resolvers = append(resolvers, nrptRules(path)...)
	}
	for _, a := range netif.Adapters() {
		if !a.Up || a.DNSSuffix == "" || !isTunnel(a.Name) {
			continue
		}
		for _, s := range a.DNS {
			resolvers = append(resolvers, splitResolvers([]string{a.DNSSuffix}, s, a.Name, proxyDNS)...)
		}
	}
	return routes, dedupeResolvers(resolvers)
}

// nrptRules reads the rules under one key: each a subkey with the names
// it is for (Name) and the servers to ask (GenericDNSServers).
func nrptRules(path string) []Resolver {
	k, err := registry.OpenKey(registry.LOCAL_MACHINE, path, registry.ENUMERATE_SUB_KEYS)
	if err != nil {
		return nil
	}
	defer k.Close()
	subs, err := k.ReadSubKeyNames(-1)
	if err != nil {
		return nil
	}
	var out []Resolver
	for _, sub := range subs {
		rk, err := registry.OpenKey(k, sub, registry.QUERY_VALUE)
		if err != nil {
			continue
		}
		names, _, err1 := rk.GetStringsValue("Name")
		servers, _, err2 := rk.GetStringValue("GenericDNSServers")
		rk.Close()
		if err1 == nil && err2 == nil {
			out = append(out, splitResolvers(names, servers, "", proxyDNS)...)
		}
	}
	return out
}
