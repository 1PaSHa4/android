// Package vpntransport prepares the restricted Xray transport boundary.
// The engine is not started by this package.
package vpntransport

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"net/netip"
	"strconv"

	"netmerge-android/internal/profile"
)

// PinnedTCP is implemented by the existing net-merge Balancer.DialPinned.
type PinnedTCP interface {
	DialPinned(context.Context, string, int) (net.Conn, error)
}

// EndpointDialer permits the core to open physical sockets only to the selected
// VPN endpoint. It cannot resolve names, connect directly to user destinations,
// or silently select the system default network.
type EndpointDialer struct {
	pinned PinnedTCP
	endpoint netip.AddrPort
}

// NewEndpointDialer requires an already resolved, trusted endpoint address.
func NewEndpointDialer(pinned PinnedTCP, endpoint netip.AddrPort) (*EndpointDialer, error) {
	if pinned == nil || !endpoint.IsValid() || endpoint.Port() == 0 || endpoint.Addr().IsUnspecified() || endpoint.Addr().IsMulticast() || endpoint.Addr().Zone() != "" {
		return nil, errors.New("invalid VPN transport endpoint")
	}
	return &EndpointDialer{pinned: pinned, endpoint: netip.AddrPortFrom(endpoint.Addr().Unmap(), endpoint.Port())}, nil
}

func (d *EndpointDialer) DialContext(ctx context.Context, network, address string) (net.Conn, error) {
	if network != "tcp" && network != "tcp4" && network != "tcp6" {
		return nil, errors.New("VPN physical transport requires TCP")
	}
	dest, err := netip.ParseAddrPort(address)
	if err != nil || netip.AddrPortFrom(dest.Addr().Unmap(), dest.Port()) != d.endpoint {
		return nil, errors.New("VPN physical destination not permitted")
	}
	ip := d.endpoint.Addr()
	if (network == "tcp4" && !ip.Is4()) || (network == "tcp6" && !ip.Is6()) {
		return nil, errors.New("VPN transport address family mismatch")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	conn, err := d.pinned.DialPinned(ctx, ip.String(), int(d.endpoint.Port()))
	if err != nil {
		return nil, errors.New("VPN physical connection failed")
	}
	return conn, nil
}

// BuildXrayConfig accepts only a validated share link plus an explicitly
// resolved endpoint IP. The caller is responsible for trusted DNS bootstrap.
// The returned JSON contains credentials: never log it or write it to Git.
func BuildXrayConfig(link string, ip netip.Addr) ([]byte, error) {
	p, err := profile.ParseRealityTCP(link)
	if err != nil {
		return nil, err
	}
	ip = ip.Unmap()
	if !ip.IsValid() || ip.IsUnspecified() || ip.IsMulticast() || ip.Zone() != "" {
		return nil, errors.New("VPN endpoint must be a resolved IP")
	}
	if literal, err := netip.ParseAddr(p.Server); err == nil && literal.Unmap() != ip {
		return nil, errors.New("VPN endpoint differs from profile")
	}
	config := map[string]any{
		"log": map[string]any{"loglevel": "none", "access": "none", "error": "none"},
		"inbounds": []any{},
		"outbounds": []any{map[string]any{
			"tag": "vpn",
			"protocol": "vless",
			"settings": map[string]any{"vnext": []any{map[string]any{
				"address": ip.String(),
				"port": p.Port,
				"users": []any{map[string]any{"id": p.UUID, "encryption": "none", "flow": p.Flow}},
			}}},
			"streamSettings": map[string]any{
				"network": "tcp",
				"security": "reality",
				"realitySettings": map[string]any{
					"serverName": p.ServerName,
					"publicKey": p.PublicKey,
					"shortId": p.ShortID,
					"fingerprint": p.Fingerprint,
					"spiderX": p.SpiderX,
				},
			},
			"mux": map[string]any{"enabled": false},
		}},
	}
	return json.Marshal(config)
}

// Address returns the only allowed physical destination, without account data.
func (d *EndpointDialer) Address() string {
	return net.JoinHostPort(d.endpoint.Addr().String(), strconv.Itoa(int(d.endpoint.Port())))
}
