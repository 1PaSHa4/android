// Package xraybridge is an experimental integration, not yet used by the APK.
package xraybridge

import (
	"context"
	"errors"
	"net"
	"net/netip"
	"strconv"
	"sync"

	xnet "github.com/xtls/xray-core/common/net"
	"github.com/xtls/xray-core/core"
	"github.com/xtls/xray-core/transport/internet"

	"netmerge-android/internal/vpntransport"
	"netmerge-android/internal/profile"
)

// Xray's system dialer is global. Install a permanent guarded adapter once;
// clearing the active endpoint rejects new physical dials instead of resetting
// Xray to its default (direct) system dialer.
type systemGate struct {
	mu sync.RWMutex
	endpoint *vpntransport.EndpointDialer
	ctx context.Context
	cancel context.CancelFunc
	conns map[*physicalConn]struct{}
}

type physicalConn struct {
	net.Conn
	g *systemGate
	once sync.Once
}
func (c *physicalConn) Close() error {
	var err error
	c.once.Do(func(){
		err = c.Conn.Close()
		c.g.mu.Lock()
		delete(c.g.conns,c)
		c.g.mu.Unlock()
	})
	return err
}

type sessionKey struct{}
var gate systemGate
var lifecycle sync.Mutex
var active *Client

func init() { internet.UseAlternativeSystemDialer(&gate) }

var _ internet.SystemDialer = (*systemGate)(nil)

func (g *systemGate) Dial(ctx context.Context, _ xnet.Address, dest xnet.Destination, _ *internet.SocketConfig) (net.Conn, error) {
	g.mu.RLock()
	d, lifetime := g.endpoint, g.ctx
	g.mu.RUnlock()
	if d == nil || ctx.Value(sessionKey{}) != d || dest.Network != xnet.Network_TCP {
		return nil, errors.New("VPN system dial rejected")
	}
	if dest.Address == nil || !dest.Address.Family().IsIP() {
		return nil, errors.New("VPN physical destination must be an IP")
	}
	dialCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	stop := context.AfterFunc(lifetime,cancel)
	defer stop()
	raw, err := d.DialContext(dialCtx, "tcp", net.JoinHostPort(dest.Address.IP().String(), strconv.Itoa(int(dest.Port))))
	if err != nil { return nil, err }
	g.mu.Lock()
	if g.endpoint != d || lifetime.Err() != nil || ctx.Err() != nil {
		g.mu.Unlock()
		_ = raw.Close()
		return nil, errors.New("VPN session ended")
	}
	c := &physicalConn{Conn:raw,g:g}
	g.conns[c] = struct{}{}
	g.mu.Unlock()
	return c,nil
}
func (g *systemGate) DestIpAddress() xnet.IP { return nil }

func (g *systemGate) set(d *vpntransport.EndpointDialer) {
	g.mu.Lock()
	if g.cancel != nil { g.cancel() }
	old := g.conns
	g.conns = make(map[*physicalConn]struct{})
	g.endpoint = d
	if d != nil {
		g.ctx,g.cancel = context.WithCancel(context.Background())
	} else {
		g.ctx,g.cancel = nil,nil
	}
	g.mu.Unlock()
	for c := range old { _ = c.Close() }
}

type Client struct {
	instance *core.Instance
	endpoint *vpntransport.EndpointDialer
	closed bool
}

// Open starts the core with no listeners and a single encrypted outbound.
// endpoint must come from a trusted bootstrap resolution.
// Successful startup does not prove remote authentication or connectivity.
func Open(link string, endpoint netip.AddrPort, pinned vpntransport.PinnedTCP) (*Client, error) {
	lifecycle.Lock()
	defer lifecycle.Unlock()
	if active != nil {
		return nil, errors.New("VPN core already running")
	}
	p, err := profile.ParseRealityTCP(link)
	if err != nil { return nil, err }
	if endpoint.Port() != p.Port { return nil, errors.New("VPN endpoint port differs from profile") }
	d, err := vpntransport.NewEndpointDialer(pinned, endpoint)
	if err != nil { return nil, err }
	cfg, err := buildCoreConfig(p, endpoint)
	if err != nil { return nil, errors.New("Xray configuration rejected") }
	gate.set(d)
	instance, err := core.New(cfg)
	if err != nil {
		gate.set(nil)
		return nil, errors.New("Xray initialization failed")
	}
	if err := instance.Start(); err != nil {
		gate.set(nil)
		_ = instance.Close()
		return nil, errors.New("Xray startup failed")
	}
	c := &Client{instance: instance, endpoint:d}
	active = c
	return c, nil
}

// DialTCP dispatches an application connection through the encrypted outbound.
// Xray may return before the remote handshake is complete.
func (c *Client) DialTCP(ctx context.Context, target netip.AddrPort) (net.Conn, error) {
	lifecycle.Lock()
	defer lifecycle.Unlock()
	if c == nil || c.closed || active != c {
		return nil, errors.New("VPN core stopped")
	}
	if !target.IsValid() || target.Port() == 0 || target.Addr().Zone() != "" {
		return nil, errors.New("invalid application destination")
	}
	return core.Dial(context.WithValue(ctx,sessionKey{},c.endpoint), c.instance, xnet.TCPDestination(xnet.ParseAddress(target.Addr().Unmap().String()), xnet.Port(target.Port())))
}

// Close disables new flows and shuts down the core.
func (c *Client) Close() error {
	lifecycle.Lock()
	defer lifecycle.Unlock()
	if c == nil || c.closed { return nil }
	c.closed = true
	gate.set(nil)
	err := c.instance.Close()
	active = nil
	if err != nil { return errors.New("Xray shutdown failed") }
	return nil
}
