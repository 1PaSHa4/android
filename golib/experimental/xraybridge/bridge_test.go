package xraybridge

import (
	"context"
	"errors"
	"net"
	"net/netip"
	"testing"

	xnet "github.com/xtls/xray-core/common/net"
)

type offlineDialer struct{}
func (offlineDialer) DialPinned(context.Context, string, int) (net.Conn, error) {
	return nil, errors.New("offline smoke test")
}

func TestCoreLifecycle(t *testing.T) {
	const link = "vless://12345678-1234-4234-8234-123456789abc@192.0.2.1:443?type=tcp&security=reality&encryption=none&sni=example.org&pbk=AQEBAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQE&sid=abcd&fp=chrome"
	endpoint := netip.MustParseAddrPort("192.0.2.1:443")
	c, err := Open(link, endpoint, offlineDialer{})
	if err != nil { t.Fatal(err) }
	defer c.Close()
	if _, err := Open(link, endpoint, offlineDialer{}); err == nil { t.Fatal("second core accepted") }
	if err := c.Close(); err != nil { t.Fatal(err) }
	if _, err := c.DialTCP(context.Background(), endpoint); err == nil { t.Fatal("closed core accepted flow") }
	if _, err := gate.Dial(context.Background(), nil, xnet.TCPDestination(xnet.ParseAddress("192.0.2.1"), 443), nil); err == nil { t.Fatal("closed gate accepted physical connection") }
}
