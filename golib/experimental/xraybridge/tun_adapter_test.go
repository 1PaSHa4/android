package xraybridge

import (
	"context"
	"net"
	"net/netip"
	"testing"
	"time"

	M "github.com/xjasonlyu/tun2socks/v2/metadata"
)

func TestPacketFlowRejectsOtherDestinationsAndLargeDatagrams(t *testing.T) {
	pc, err := net.ListenPacket("udp4", "127.0.0.1:0")
	if err != nil { t.Fatal(err) }
	defer pc.Close()
	_, cancel := context.WithCancel(context.Background())
	p := &packetFlow{PacketConn:pc, target:netip.MustParseAddrPort("127.0.0.1:53"), cancel:cancel}
	defer p.Close()
	if _, err := p.WriteTo([]byte{1}, &net.UDPAddr{IP:net.IPv4(127,0,0,1),Port:54}); err == nil { t.Fatal("wrong destination accepted") }
	if _, err := p.WriteTo(make([]byte,2049), &net.UDPAddr{IP:net.IPv4(127,0,0,1),Port:53}); err == nil { t.Fatal("oversized datagram accepted") }
	if err := p.SetReadDeadline(time.Now()); err != nil { t.Fatal(err) }
	done := make(chan error,1)
	go func(){ _,_,err := p.ReadFrom(make([]byte,64)); done <- err }()
	select {
	case err := <-done: if err == nil { t.Fatal("expired flow read succeeded") }
	case <-time.After(time.Second): t.Fatal("expired flow did not unblock")
	}
}

func TestStoppedClientRejectsTUNFlows(t *testing.T) {
	c := &Client{closed:true}
	m := &M.Metadata{DstIP:netip.MustParseAddr("192.0.2.2"),DstPort:53}
	if _, err := c.DialUDP(m); err == nil { t.Fatal("stopped UDP accepted") }
	if _, err := c.DialContext(context.Background(),m); err == nil { t.Fatal("stopped TCP accepted") }
}
