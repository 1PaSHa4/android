// Package tun terminates the VpnService's TUN in a userland TCP/IP stack (gVisor via tun2socks)
// and re-dials every flow through the Balancer. Unlike upstream, Android creates the adapter and
// its routes (VpnService.Builder); this side only receives the file descriptor.
package tun

import (
	"context"
	"errors"
	"net"
	"strconv"
	"sync"
	"time"

	"gvisor.dev/gvisor/pkg/tcpip/stack"

	"github.com/xjasonlyu/tun2socks/v2/core"
	"github.com/xjasonlyu/tun2socks/v2/core/device"
	"github.com/xjasonlyu/tun2socks/v2/core/device/fdbased"
	"github.com/xjasonlyu/tun2socks/v2/core/option"
	tlog "github.com/xjasonlyu/tun2socks/v2/log"
	M "github.com/xjasonlyu/tun2socks/v2/metadata"
	"github.com/xjasonlyu/tun2socks/v2/proxy/reject"
	"github.com/xjasonlyu/tun2socks/v2/tunnel"

	"netmerge-android/internal/engine"
)

// udpSessionTimeout ends idle UDP sessions sooner than tun2socks' 60s (see upstream).
const udpSessionTimeout = 30 * time.Second

// FlowDialer handles application flows from the TUN. An encrypted transport must
// implement both methods (or reject unsupported flows), never fall back to tunProxy.
// Its physical sockets should be opened via the balancer, not via default routing.
type FlowDialer interface {
	DialContext(context.Context, *M.Metadata) (net.Conn, error)
	DialUDP(*M.Metadata) (net.PacketConn, error)
}

type tunProxy struct{ b *engine.Balancer }

func (p tunProxy) DialContext(ctx context.Context, m *M.Metadata) (net.Conn, error) {
	return p.b.DialPinned(ctx, m.DstIP.Unmap().String(), int(m.DstPort))
}

func (p tunProxy) DialUDP(m *M.Metadata) (net.PacketConn, error) {
	f := engine.Fam4
	if d := m.DstIP.Unmap(); d.Is6() {
		f = engine.Fam6
	}
	return p.b.ListenUDP(context.Background(), f)
}

// Tun owns the stack built on the VPN's file descriptor.
type Tun struct {
	mu  sync.Mutex
	dev device.Device
	stk *stack.Stack
}

// Start takes ownership of fd (it is closed by Stop).
func (t *Tun) Start(b *engine.Balancer, fd, mtu int) error {
	return t.StartWithDialer(tunProxy{b}, b.V6Allowed, fd, mtu)
}

// StartWithDialer installs an explicit flow transport. Invalid arguments are rejected
// before taking ownership of fd or changing the currently running stack.
// No direct fallback is installed if the chosen transport cannot reach a destination.
func (t *Tun) StartWithDialer(dialer FlowDialer, allowV6 func() bool, fd, mtu int) error {
	if dialer == nil || allowV6 == nil {
		return errors.New("TUN requires a flow dialer and IPv6 policy")
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	t.teardown()
	if lvl, e := tlog.ParseLevel("error"); e == nil {
		tlog.SetLogger(tlog.Must(tlog.NewLeveled(lvl)))
	}
	tunnel.T().SetUDPTimeout(udpSessionTimeout)
	dev, err := fdbased.Open(strconv.Itoa(fd), uint32(mtu), 0)
	if err != nil {
		return err
	}
	stk, err := core.CreateStack(&core.Config{
		LinkEndpoint:     dev,
		TransportHandler: tunnel.T(),
		Options:          []option.Option{option.WithTCPModerateReceiveBuffer(true)},
	})
	if err != nil {
		dev.Close()
		return err
	}
	installForwarders(stk, allowV6)
	tunnel.T().SetProxy(dialer)
	t.dev, t.stk = dev, stk
	return nil
}

func (t *Tun) Stop() {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.teardown()
}

func (t *Tun) teardown() {
	tunnel.T().SetProxy(&reject.Reject{})
	if t.dev != nil {
		t.dev.Close()
	}
	if t.stk != nil {
		done := make(chan struct{})
		stk := t.stk
		go func() { stk.Close(); stk.Wait(); close(done) }()
		select {
		case <-done:
		case <-time.After(5 * time.Second):
		}
	}
	t.dev, t.stk = nil, nil
}
