package xraybridge

import (
	"context"
	"errors"
	"io"
	"net"
	"net/netip"
	"sync"
	"time"

	"github.com/xtls/xray-core/core"
	M "github.com/xjasonlyu/tun2socks/v2/metadata"

	"netmerge-android/internal/tun"
)

var _ tun.FlowDialer = (*Client)(nil)

// DialContext implements the TUN TCP transport without direct fallback.
func (c *Client) DialContext(ctx context.Context, m *M.Metadata) (net.Conn, error) {
	if m == nil || !m.DstIP.IsValid() || m.DstPort == 0 {
		return nil, errors.New("invalid TUN TCP destination")
	}
	return c.DialTCP(ctx, netip.AddrPortFrom(m.DstIP.Unmap(), m.DstPort))
}

// DialUDP creates one Xray UDP dispatcher per TUN flow. The underlying
// physical transport is still TCP to the selected VPN endpoint.
func (c *Client) DialUDP(m *M.Metadata) (net.PacketConn, error) {
	if m == nil || !m.DstIP.IsValid() || m.DstPort == 0 {
		return nil, errors.New("invalid TUN UDP destination")
	}
	lifecycle.Lock()
	defer lifecycle.Unlock()
	if c == nil || c.closed || active != c { return nil, errors.New("VPN core stopped") }
	ctx, cancel := context.WithCancel(context.WithValue(context.Background(),sessionKey{},c.endpoint))
	pc, err := core.DialUDP(ctx, c.instance)
	if err != nil { cancel(); return nil, errors.New("VPN UDP setup failed") }
	return &packetFlow{PacketConn: pc, target: netip.AddrPortFrom(m.DstIP.Unmap(), m.DstPort), cancel: cancel}, nil
}

// Xray's packet connection does not implement deadlines. TUN uses an idle
// read deadline; on expiry permanently close this flow so its blocked read
// returns. Never reuse an expired flow or silently fall back to raw UDP.
type packetFlow struct {
	net.PacketConn
	target netip.AddrPort
	cancel context.CancelFunc
	mu sync.Mutex
	timer *time.Timer
	generation uint64
	closed bool
}

func (p *packetFlow) WriteTo(b []byte, addr net.Addr) (int, error) {
	a, ok := addr.(*net.UDPAddr)
	if !ok || a.Zone != "" || a.AddrPort().Addr().Unmap() != p.target.Addr() || a.Port != int(p.target.Port()) {
		return 0, errors.New("UDP destination differs from flow")
	}
	// Xray dispatcher copies to one 2048-byte buffer; reject large packets
	// rather than silently truncating. This is experimental (DNS fits).
	if len(b) > 2048 { return 0, errors.New("UDP packet exceeds experimental limit") }
	return p.PacketConn.WriteTo(b, a)
}
func (p *packetFlow) SetReadDeadline(t time.Time) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.closed { return net.ErrClosed }
	p.generation++
	gen := p.generation
	if p.timer != nil { p.timer.Stop(); p.timer = nil }
	if !t.IsZero() {
		p.timer = time.AfterFunc(time.Until(t), func() {
			p.mu.Lock()
			if p.closed || p.generation != gen { p.mu.Unlock(); return }
			p.closed = true
			p.mu.Unlock()
			p.cancel()
			_ = p.PacketConn.Close()
		})
	}
	return nil
}
func (p *packetFlow) SetDeadline(t time.Time) error { return p.SetReadDeadline(t) }
func (p *packetFlow) SetWriteDeadline(time.Time) error { return errors.New("UDP write deadline not implemented") }
func (p *packetFlow) Close() error {
	p.mu.Lock()
	if p.closed { p.mu.Unlock(); return nil }
	p.closed = true
	if p.timer != nil { p.timer.Stop() }
	p.mu.Unlock()
	p.cancel()
	return p.PacketConn.Close()
}

func (p *packetFlow) ReadFrom(b []byte) (int, net.Addr, error) {
	n, a, err := p.PacketConn.ReadFrom(b)
	if err != nil {
		p.mu.Lock()
		closed := p.closed
		p.mu.Unlock()
		if closed { return 0, nil, io.EOF }
	}
	return n, a, err
}
