package sysproxy

import (
	"context"
	"net"
	"reflect"
	"testing"
)

func TestForget(t *testing.T) {
	saved := Saved{Services: []Service{
		{Name: "Wi-Fi", Web: Endpoint{true, "127.0.0.1", 7899}, Secure: Endpoint{true, "127.0.0.1", 7899}, Socks: Endpoint{true, "localhost", 7899}, Bypass: []string{"10.0.0.0/8"}},
		{Name: "Ethernet", Web: Endpoint{true, "proxy.corp", 8080}, Secure: Endpoint{false, "127.0.0.1", 7000}, Bypass: []string{"corp"}},
		{Name: "Bridge"},
	}}
	saved.Forget(7899, 7000)
	want := Saved{Services: []Service{
		{Name: "Wi-Fi", Bypass: DefaultBypass},
		{Name: "Ethernet", Web: Endpoint{true, "proxy.corp", 8080}, Bypass: []string{"corp"}},
		{Name: "Bridge"},
	}}
	if !reflect.DeepEqual(saved, want) {
		t.Errorf("got %+v\nwant %+v", saved, want)
	}
}

func TestTurnOffDead(t *testing.T) {
	saved := Saved{Services: []Service{
		{Name: "A", Web: Endpoint{true, "127.0.0.1", 7890}, Secure: Endpoint{true, "127.0.0.1", 7890}, Socks: Endpoint{true, "localhost", 1080}},
		{Name: "B", Web: Endpoint{true, "127.0.0.1", 7890}, Secure: Endpoint{true, "proxy.corp", 8080}, Socks: Endpoint{false, "127.0.0.1", 9000}},
	}}
	asked := map[int]int{}
	saved.TurnOffDead(func(_ string, port int) bool { asked[port]++; return port == 1080 })
	want := Saved{Services: []Service{
		{Name: "A", Web: Endpoint{false, "127.0.0.1", 7890}, Secure: Endpoint{false, "127.0.0.1", 7890}, Socks: Endpoint{true, "localhost", 1080}},
		{Name: "B", Web: Endpoint{false, "127.0.0.1", 7890}, Secure: Endpoint{true, "proxy.corp", 8080}, Socks: Endpoint{false, "127.0.0.1", 9000}},
	}}
	if !reflect.DeepEqual(saved, want) {
		t.Errorf("got %+v\nwant %+v", saved, want)
	}
	if !reflect.DeepEqual(asked, map[int]int{7890: 1, 1080: 1}) {
		t.Errorf("asked %v: each local address once, none for a proxy that is off or remote", asked)
	}
}

func TestListening(t *testing.T) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := l.Addr().(*net.TCPAddr).Port
	if !Listening(context.Background(), "127.0.0.1", port) {
		t.Error("a listener was not found")
	}
	l.Close()
	if Listening(context.Background(), "127.0.0.1", port) {
		t.Error("a closed port was found listening")
	}
}
