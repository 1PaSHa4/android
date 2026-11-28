package tun

import (
	"testing"
)

var _ FlowDialer = tunProxy{}

func TestStartWithDialerRequiresTransport(t *testing.T) {
	var dev Tun
	if err := dev.StartWithDialer(nil, func() bool { return true }, -1, 1500); err == nil {
		t.Fatal("missing transport must be rejected")
	}
	if dev.dev != nil || dev.stk != nil {
		t.Fatal("invalid transport must not initialize the stack")
	}
}

func TestStartWithDialerRequiresIPv6Policy(t *testing.T) {
	var dev Tun
	if err := dev.StartWithDialer(tunProxy{}, nil, -1, 1500); err == nil {
		t.Fatal("missing IPv6 policy must be rejected")
	}
	if dev.dev != nil || dev.stk != nil {
		t.Fatal("invalid policy must not initialize the stack")
	}
}
