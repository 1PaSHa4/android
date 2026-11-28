package vpntransport

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"net/netip"
	"strings"
	"testing"
)

type fakePinned struct {
	calls int
	host string
	port int
}
func (f *fakePinned) DialPinned(_ context.Context, host string, port int) (net.Conn, error) {
	f.calls++
	f.host, f.port = host, port
	return nil, errors.New("test transport unavailable")
}

func TestEndpointDialerRestrictsPhysicalTraffic(t *testing.T) {
	f := &fakePinned{}
	d, err := NewEndpointDialer(f, netip.MustParseAddrPort("192.0.2.1:443"))
	if err != nil { t.Fatal(err) }
	for _, input := range [][2]string{{"udp","192.0.2.1:443"},{"tcp","example.org:443"},{"tcp","192.0.2.2:443"},{"tcp","192.0.2.1:80"},{"tcp6","192.0.2.1:443"}} {
		if _, err := d.DialContext(context.Background(), input[0], input[1]); err == nil { t.Fatal("unexpected success") }
	}
	if f.calls != 0 { t.Fatal("forbidden physical connection was attempted") }
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := d.DialContext(ctx,"tcp",d.Address()); err == nil || f.calls != 0 { t.Fatal("canceled dial was attempted") }
	if _, err := d.DialContext(context.Background(),"tcp",d.Address()); err == nil { t.Fatal("unexpected success") }
	if f.calls != 1 || f.host != "192.0.2.1" || f.port != 443 { t.Fatal("did not delegate to pinned dialer") }
}

func TestBuildXrayConfig(t *testing.T) {
	const link = "vless://12345678-1234-4234-8234-123456789abc@vpn.example:443?type=tcp&security=reality&encryption=none&sni=example.org&pbk=AQEBAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQE&sid=abcd&fp=chrome"
	ip := netip.MustParseAddr("192.0.2.1")
	b, err := BuildXrayConfig(link, ip)
	if err != nil { t.Fatal(err) }
	var cfg struct {
		Inbounds []any
		Outbounds []struct {
			Protocol string
			Mux struct{ Enabled bool }
			Settings struct{ Vnext []struct{ Address string } }
			StreamSettings struct{ Security string }
		}
	}
	if err := json.Unmarshal(b, &cfg); err != nil { t.Fatal(err) }
	if len(cfg.Inbounds) != 0 || len(cfg.Outbounds) != 1 { t.Fatal("unexpected routing configuration") }
	o := cfg.Outbounds[0]
	if o.Protocol != "vless" || o.Mux.Enabled || o.StreamSettings.Security != "reality" || len(o.Settings.Vnext) != 1 || o.Settings.Vnext[0].Address != ip.String() { t.Fatal("unexpected VPN outbound") }
	if strings.Contains(string(b), "vpn.example") { t.Fatal("physical endpoint must not require DNS") }
	if _, err := BuildXrayConfig(strings.Replace(link,"vpn.example","192.0.2.2",1), ip); err == nil { t.Fatal("literal endpoint mismatch accepted") }
	if _, err := BuildXrayConfig(link,netip.Addr{}); err == nil { t.Fatal("missing endpoint accepted") }
}
