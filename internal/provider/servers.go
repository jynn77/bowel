package provider

import (
	"encoding/base64"
	"encoding/json"
	"net"
	"net/url"
	"os"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"
)

// Servers maps a provider's node names, as the provider's content has
// them, to their servers' hosts. Nodes it can't read are absent.
func (s *Store) Servers(id string) map[string]string {
	b, err := os.ReadFile(s.Path(id))
	if err != nil {
		return nil
	}
	return parseServers(b)
}

// Addrs maps a provider's node names to their servers' addresses, host:port
// as the content gives them: what people paste into another client, and what
// names a node's server when its name doesn't. Nodes it can't read are
// absent. The parse is kept by the file's identity, since the interface asks
// for it every snapshot.
func (s *Store) Addrs(id string) map[string]string {
	p := s.Path(id)
	fi, err := os.Stat(p)
	if err != nil {
		return nil
	}
	s.mu.Lock()
	e, cached := s.addrs[id]
	s.mu.Unlock()
	if cached && e.size == fi.Size() && e.mod.Equal(fi.ModTime()) {
		return e.by
	}
	b, err := os.ReadFile(p)
	if err != nil {
		return nil
	}
	by := parseAddrs(b)
	s.mu.Lock()
	if s.addrs == nil {
		s.addrs = map[string]addrEntry{}
	}
	s.addrs[id] = addrEntry{size: fi.Size(), mod: fi.ModTime(), by: by}
	s.mu.Unlock()
	return by
}

// Names are the names of provider id's nodes as its file has them, those
// without a server of their own (direct, WireGuard's peers) too.
func (s *Store) Names(id string) []string {
	b, err := os.ReadFile(s.Path(id))
	if err != nil {
		return nil
	}
	return parseNames(b)
}

func parseNames(data []byte) []string {
	var doc struct {
		Proxies []struct {
			Name string `yaml:"name"`
		} `yaml:"proxies"`
	}
	var out []string
	if yaml.Unmarshal(data, &doc) == nil && len(doc.Proxies) > 0 {
		for _, p := range doc.Proxies {
			if p.Name != "" {
				out = append(out, p.Name)
			}
		}
		return out
	}
	for name := range parseServers(data) {
		out = append(out, name)
	}
	return out
}

// parseServers reads what mihomo reads: Clash YAML, or node links, perhaps
// in Base64. Only the hosts: a test pings them.
func parseServers(data []byte) map[string]string {
	var doc struct {
		Proxies []struct {
			Name   string `yaml:"name"`
			Server string `yaml:"server"`
		} `yaml:"proxies"`
	}
	out := map[string]string{}
	if yaml.Unmarshal(data, &doc) == nil && len(doc.Proxies) > 0 {
		for _, p := range doc.Proxies {
			if p.Server != "" {
				out[p.Name] = p.Server
			}
		}
		return out
	}
	text := strings.TrimSpace(string(data))
	if !strings.Contains(text, "://") {
		if b := decodeB64(text); b != nil {
			text = string(b)
		}
	}
	for _, line := range strings.Split(text, "\n") {
		if name, host, _, ok := parseLink(strings.TrimSpace(line)); ok {
			out[name] = host
		}
	}
	return out
}

// parseAddrs is parseServers with the ports kept.
func parseAddrs(data []byte) map[string]string {
	var doc struct {
		Proxies []struct {
			Name   string `yaml:"name"`
			Server string `yaml:"server"`
			// Port is a number, or a string when it is a hopping range.
			Port any `yaml:"port"`
		} `yaml:"proxies"`
	}
	out := map[string]string{}
	if yaml.Unmarshal(data, &doc) == nil && len(doc.Proxies) > 0 {
		for _, p := range doc.Proxies {
			if p.Server != "" {
				out[p.Name] = hostPort(p.Server, portOf(p.Port))
			}
		}
		return out
	}
	text := strings.TrimSpace(string(data))
	if !strings.Contains(text, "://") {
		if b := decodeB64(text); b != nil {
			text = string(b)
		}
	}
	for _, line := range strings.Split(text, "\n") {
		if name, host, port, ok := parseLink(strings.TrimSpace(line)); ok {
			out[name] = hostPort(host, port)
		}
	}
	return out
}

// portOf is a YAML port as an address' port.
func portOf(v any) string {
	switch t := v.(type) {
	case int:
		return strconv.Itoa(t)
	case int64:
		return strconv.FormatInt(t, 10)
	case float64:
		return strconv.Itoa(int(t))
	case string:
		return strings.TrimSpace(t)
	}
	return ""
}

// hostPort joins a host and its port as an address: [v6]:port, host:port, or
// the host alone when the content left the port to the node's type.
func hostPort(host, port string) string {
	if port == "" {
		return host
	}
	if strings.Contains(host, ":") && !strings.HasPrefix(host, "[") {
		return "[" + host + "]:" + port
	}
	return host + ":" + port
}

func decodeB64(s string) []byte {
	s = strings.Join(strings.Fields(s), "")
	for _, enc := range []*base64.Encoding{base64.StdEncoding, base64.RawStdEncoding, base64.URLEncoding, base64.RawURLEncoding} {
		if b, err := enc.DecodeString(s); err == nil {
			return b
		}
	}
	return nil
}

func parseLink(line string) (string, string, string, bool) {
	scheme, rest, ok := strings.Cut(line, "://")
	if !ok {
		return "", "", "", false
	}
	if strings.EqualFold(scheme, "vmess") {
		return parseVmess(rest)
	}
	rest, frag, _ := strings.Cut(rest, "#")
	name, err := url.PathUnescape(frag)
	if err != nil {
		name = frag
	}
	rest, _, _ = strings.Cut(rest, "?")
	rest, _, _ = strings.Cut(rest, "/")
	if strings.EqualFold(scheme, "ss") && !strings.Contains(rest, "@") {
		// ss://<base64 of method:password@host:port>
		b := decodeB64(rest)
		if b == nil {
			return "", "", "", false
		}
		rest = string(b)
	}
	host, port, ok := authority(rest[strings.LastIndex(rest, "@")+1:])
	return name, host, port, ok
}

func parseVmess(b64 string) (string, string, string, bool) {
	var v struct {
		Name string `json:"ps"`
		Host string `json:"add"`
		Port any    `json:"port"`
	}
	if b := decodeB64(b64); b == nil || json.Unmarshal(b, &v) != nil {
		return "", "", "", false
	}
	return v.Name, v.Host, portOf(v.Port), v.Host != ""
}

// authority is the host and port of host:port, ok if that isn't what s is.
// The port may be a hopping range such as "443,20000-30000".
func authority(s string) (string, string, bool) {
	i := strings.LastIndex(s, ":")
	if i < 0 || !validPort(s[i+1:]) {
		return "", "", false
	}
	host := strings.Trim(s[:i], "[]")
	if net.ParseIP(host) == nil && strings.Contains(host, ":") {
		return "", "", false
	}
	return host, strings.TrimSpace(s[i+1:]), true
}

func validPort(s string) bool {
	s, _, _ = strings.Cut(s, ",")
	s, _, _ = strings.Cut(s, "-")
	p, err := strconv.Atoi(strings.TrimSpace(s))
	return err == nil && p > 0 && p <= 65535
}
