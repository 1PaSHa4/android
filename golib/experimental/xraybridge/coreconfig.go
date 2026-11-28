package xraybridge

import (
	"encoding/base64"
	"encoding/hex"
	"errors"
	"net/netip"
	"net/url"

	"github.com/xtls/xray-core/app/dispatcher"
	xlog "github.com/xtls/xray-core/app/log"
	"github.com/xtls/xray-core/app/proxyman"
	_ "github.com/xtls/xray-core/app/proxyman/inbound"
	"github.com/xtls/xray-core/common/net"
	"github.com/xtls/xray-core/common/protocol"
	"github.com/xtls/xray-core/common/serial"
	"github.com/xtls/xray-core/core"
	"github.com/xtls/xray-core/proxy/vless"
	vout "github.com/xtls/xray-core/proxy/vless/outbound"
	"github.com/xtls/xray-core/transport/internet"
	"github.com/xtls/xray-core/transport/internet/reality"
	_ "github.com/xtls/xray-core/transport/internet/tcp"
	"github.com/xtls/xray-core/transport/internet/tls"
	_ "github.com/xtls/xray-core/transport/internet/tagged/taggedimpl"

	"netmerge-android/internal/profile"
)

// Only the needed protobuf configuration is registered. Do not import the
// general-purpose JSON loader or all-protocol distro into this Android client.
func buildCoreConfig(p profile.RealityTCP, endpoint netip.AddrPort) (*core.Config, error) {
	ip := endpoint.Addr().Unmap()
	if !endpoint.IsValid() || endpoint.Port() != p.Port || ip.IsUnspecified() || ip.IsMulticast() || ip.Zone() != "" {
		return nil, errors.New("invalid VPN endpoint")
	}
	if literal, err := netip.ParseAddr(p.Server); err == nil && literal.Unmap() != ip {
		return nil, errors.New("VPN endpoint differs from profile")
	}
	pub, err := base64.RawURLEncoding.Strict().DecodeString(p.PublicKey)
	if err != nil || len(pub) != 32 { return nil, errors.New("invalid REALITY key") }
	sid := make([]byte,8)
	if len(p.ShortID)>16 { return nil, errors.New("invalid REALITY short ID") }
	if _,err := hex.Decode(sid,[]byte(p.ShortID));err != nil { return nil,errors.New("invalid REALITY short ID") }
	if tls.GetFingerprint(p.Fingerprint)==nil {return nil,errors.New("unsupported REALITY fingerprint")}
	spider := p.SpiderX
	if spider=="" {spider="/"}
	u,err:=url.Parse(spider)
	if err!=nil || u.Host!="" || u.Scheme!="" || u.RawQuery!="" || u.Fragment!="" {
		return nil,errors.New("unsupported REALITY spider path")
	}
	security := serial.ToTypedMessage(&reality.Config{
		ServerName:p.ServerName, PublicKey:pub, ShortId:sid, Fingerprint:p.Fingerprint,
		SpiderX:spider, SpiderY:make([]int64,10),
	})
	return &core.Config{
		App:[]*serial.TypedMessage{
			serial.ToTypedMessage(&xlog.Config{AccessLogType:xlog.LogType_None, ErrorLogType:xlog.LogType_None}),
			serial.ToTypedMessage(&dispatcher.Config{}),
			serial.ToTypedMessage(&proxyman.InboundConfig{}),
			serial.ToTypedMessage(&proxyman.OutboundConfig{}),
		},
		Outbound:[]*core.OutboundHandlerConfig{{
			Tag:"vpn",
			SenderSettings:serial.ToTypedMessage(&proxyman.SenderConfig{
				StreamSettings:&internet.StreamConfig{ProtocolName:"tcp",SecurityType:security.Type,SecuritySettings:[]*serial.TypedMessage{security}},
				MultiplexSettings:&proxyman.MultiplexingConfig{Enabled:false},
			}),
			ProxySettings:serial.ToTypedMessage(&vout.Config{Vnext:&protocol.ServerEndpoint{
				Address:net.NewIPOrDomain(net.IPAddress(ip.AsSlice())),
				Port:uint32(p.Port),
				User:&protocol.User{Account:serial.ToTypedMessage(&vless.Account{Id:p.UUID,Flow:p.Flow,Encryption:"none"})},
			}}),
		}},
	},nil
}
