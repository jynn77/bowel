package settings

import (
	"encoding/json"
	"reflect"
	"slices"
	"sort"
	"strings"
	"testing"

	"github.com/oboo/moho/internal/routes"
)

// machineKeys are the settings an exported configuration leaves out and an
// imported one leaves alone. Every other key travels.
var machineKeys = []string{
	"systemProxy", "tun", "paused", "gateway",
	"mixedPort", "allowLan", "httpPort", "socksPort", "extraPorts", "authentication", "lanAllowedIps",
	"interface", "tunDevice",
	"devNetOff", "devNetOffScenarios", "devNetExtra",
	"extController", "extSecret",
	"mcpPolicy",
}

// portableKeys are the settings that travel. A new setting belongs in one
// of the two lists, and takeMachine must agree: TestEveryKeyIsClassified
// fails until it does.
var portableKeys = []string{
	"language", "accent", "appearance", "borders", "skin",
	"mode", "bypass", "logLevel", "testUrl",
	"locateUrl", "ipQuality", "ipInfoUrl", "stripRules",
	"routeTargets", "rules", "routing", "importedRouting", "commonRegions",
	"startHidden", "traySpeed",
	"ipv6", "unifiedDelay", "tcpConcurrent", "findProcess",
	"keepAliveInterval", "disableKeepAlive", "fingerprint",
	"urlTestInterval", "urlTestTolerance", "healthInterval",
	"dns", "dnsMode", "nameservers", "defaultNameservers", "fallback", "proxyNameservers",
	"nameserverPolicy", "fakeIpRange", "fakeIpFilter", "hosts", "respectRules", "useHosts",
	"useSystemHosts", "preferH3",
	"tunStack", "strictRoute", "tunMtu", "autoDetectInterface", "dnsHijack", "endpointIndependentNat",
	"sniff", "sniffHttp", "sniffTls", "sniffQuic", "sniffOverride", "sniffForceDnsMapping",
	"sniffParsePureIp", "sniffSkip",
	"geoAutoUpdate", "geoUpdateInterval", "geoipUrl", "geositeUrl", "mmdbUrl",
}

// fields are the settings' fields by their JSON keys, the embedded options'
// among them.
func fields(v reflect.Value, out map[string]reflect.Value) {
	for i := range v.NumField() {
		f := v.Type().Field(i)
		if f.Anonymous {
			fields(v.Field(i), out)
			continue
		}
		key, _, _ := strings.Cut(f.Tag.Get("json"), ",")
		out[key] = v.Field(i)
	}
}

// change makes v another value than it is.
func change(t *testing.T, key string, v reflect.Value) {
	switch v.Kind() {
	case reflect.String:
		v.SetString(v.String() + "x")
	case reflect.Bool:
		v.SetBool(!v.Bool())
	case reflect.Int:
		v.SetInt(v.Int() + 1)
	case reflect.Slice:
		v.Set(reflect.Append(v, reflect.Zero(v.Type().Elem())))
	case reflect.Map:
		m := reflect.MakeMap(v.Type())
		m.SetMapIndex(reflect.Zero(v.Type().Key()), reflect.Zero(v.Type().Elem()))
		v.Set(m)
	case reflect.Struct:
		change(t, key, v.Field(0))
	default:
		t.Fatalf("%s: the test cannot change a %s", key, v.Kind())
	}
}

func TestEveryKeyIsClassified(t *testing.T) {
	local, other := Defaults.Clone(), Defaults.Clone()
	lf, of := map[string]reflect.Value{}, map[string]reflect.Value{}
	fields(reflect.ValueOf(&local).Elem(), lf)
	fields(reflect.ValueOf(&other).Elem(), of)
	var all []string
	for key, v := range of {
		all = append(all, key)
		change(t, key, v)
	}
	listed := slices.Concat(machineKeys, portableKeys)
	sort.Strings(all)
	sort.Strings(listed)
	if !slices.Equal(all, listed) {
		for _, k := range all {
			if !slices.Contains(listed, k) {
				t.Errorf("%s is in neither machineKeys nor portableKeys", k)
			}
		}
		for _, k := range listed {
			if !slices.Contains(all, k) {
				t.Errorf("%s is listed but is not a setting", k)
			}
		}
		if len(all) != len(listed) {
			t.Errorf("%d settings, %d listed: one is listed twice?", len(all), len(listed))
		}
	}

	// What is imported: the machine's stay local's, the rest are other's.
	got := KeepMachine(local, other)
	gf := map[string]reflect.Value{}
	fields(reflect.ValueOf(&got).Elem(), gf)
	for key, v := range gf {
		machine := slices.Contains(machineKeys, key)
		want := of[key]
		if machine {
			want = lf[key]
		}
		if !reflect.DeepEqual(v.Interface(), want.Interface()) {
			t.Errorf("KeepMachine: %s = %v, want %v (machine: %v)", key, v.Interface(), want.Interface(), machine)
		}
	}

	// What is exported: the machine's are the defaults, the rest are kept.
	exp := Portable(other)
	ef := map[string]reflect.Value{}
	fields(reflect.ValueOf(&exp).Elem(), ef)
	for key, v := range ef {
		want := of[key]
		if slices.Contains(machineKeys, key) {
			want = lf[key]
		}
		if !reflect.DeepEqual(v.Interface(), want.Interface()) {
			t.Errorf("Portable: %s = %v, want %v", key, v.Interface(), want.Interface())
		}
	}
}

// An exported file has no word of the machine's secrets.
func TestPortableLeavesSecretsOut(t *testing.T) {
	s := Defaults.Clone()
	s.Authentication = []string{"user:hunter2"}
	s.ExtController, s.ExtSecret = "0.0.0.0:9090", "s3cret"
	s.Rules = []routes.UserRule{{Kind: "domain", Value: "example.com", Route: "direct"}}
	b, err := json.Marshal(Portable(s))
	if err != nil {
		t.Fatal(err)
	}
	for _, secret := range []string{"hunter2", "s3cret", "9090"} {
		if strings.Contains(string(b), secret) {
			t.Errorf("the export holds %q", secret)
		}
	}
	if !strings.Contains(string(b), "example.com") {
		t.Error("the export lost the rules")
	}
	if len(s.Authentication) != 1 || s.ExtSecret != "s3cret" {
		t.Error("Portable changed what it was given")
	}
}

func TestDecode(t *testing.T) {
	s, err := Decode([]byte(`{"mode":"global","ipv6":true}`))
	if err != nil {
		t.Fatal(err)
	}
	if s.Mode != "global" || s.Language != Defaults.Language || s.MixedPort != Defaults.MixedPort || string(s.IPv6) != "auto" {
		t.Fatalf("got %+v", s)
	}
	if err := s.Validate(); err != nil {
		t.Fatal(err)
	}
	if _, err := Decode([]byte(`{"mode":`)); err == nil {
		t.Fatal("half a file was read without a word")
	}
}
