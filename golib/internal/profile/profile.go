// Package profile parses a deliberately limited subset of VLESS share links.
package profile

import (
	"encoding/base64"
	"encoding/hex"
	"errors"
	"net"
	"net/url"
	"strconv"
	"strings"
)

// RealityTCP is sensitive: UUID identifies a paid VPN account. Do not log it.
// Parsing does not establish a connection or verify server authenticity.
type RealityTCP struct {
	Server, UUID, ServerName, PublicKey, ShortID, Fingerprint, Flow, SpiderX, Label string
	Port                                                                            uint16
}

// ParseRealityTCP rejects unsupported options rather than silently weakening security.
// Errors never include the input link or its credentials.
func ParseRealityTCP(raw string) (RealityTCP, error) {
	var p RealityTCP
	bad := func() (RealityTCP, error) {
		return RealityTCP{}, errors.New("invalid or unsupported VLESS REALITY TCP profile")
	}
	if len(raw) > 16384 {
		return bad()
	}
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || u.Scheme != "vless" || u.User == nil || u.Opaque != "" || (u.Path != "" && u.Path != "/") {
		return bad()
	}
	if _, ok := u.User.Password(); ok {
		return bad()
	}
	id := u.User.Username()
	if len(id) != 36 || id[8] != '-' || id[13] != '-' || id[18] != '-' || id[23] != '-' {
		return bad()
	}
	if b, err := hex.DecodeString(strings.ReplaceAll(id, "-", "")); err != nil || len(b) != 16 {
		return bad()
	}
	host := u.Hostname()
	if !validHost(host) {
		return bad()
	}
	port, err := strconv.ParseUint(u.Port(), 10, 16)
	if err != nil || port == 0 {
		return bad()
	}
	q, err := url.ParseQuery(u.RawQuery)
	if err != nil {
		return bad()
	}
	for k, v := range q {
		if len(v) != 1 {
			return bad()
		}
		switch k {
		case "type", "security", "encryption", "sni", "pbk", "sid", "fp", "flow", "spx", "headerType":
		default:
			return bad()
		}
	}
	if t := q.Get("type"); t != "" && t != "tcp" && t != "raw" {
		return bad()
	}
	if e := q.Get("encryption"); e != "" && e != "none" {
		return bad()
	}
	if h := q.Get("headerType"); h != "" && h != "none" {
		return bad()
	}
	if q.Get("security") != "reality" || !validHost(q.Get("sni")) || net.ParseIP(q.Get("sni")) != nil {
		return bad()
	}
	key, err := base64.RawURLEncoding.Strict().DecodeString(q.Get("pbk"))
	if err != nil || len(key) != 32 {
		return bad()
	}
	sid := q.Get("sid")
	if _, err := hex.DecodeString(sid); err != nil || len(sid) > 16 {
		return bad()
	}
	fp := q.Get("fp")
	switch fp {
	case "chrome", "firefox", "safari", "ios", "android", "edge":
	default:
		return bad()
	}
	flow := q.Get("flow")
	if flow != "" && flow != "xtls-rprx-vision" {
		return bad()
	}
	spx := q.Get("spx")
	if spx != "" && !strings.HasPrefix(spx, "/") {
		return bad()
	}
	p = RealityTCP{Server: host, Port: uint16(port), UUID: strings.ToLower(id), ServerName: q.Get("sni"), PublicKey: q.Get("pbk"), ShortID: strings.ToLower(sid), Fingerprint: fp, Flow: flow, SpiderX: spx, Label: u.Fragment}
	return p, nil
}

func validHost(s string) bool {
	if net.ParseIP(s) != nil {
		return true
	}
	s = strings.TrimSuffix(s, ".")
	if len(s) == 0 || len(s) > 253 {
		return false
	}
	for _, label := range strings.Split(s, ".") {
		if len(label) == 0 || len(label) > 63 || label[0] == '-' || label[len(label)-1] == '-' {
			return false
		}
		for _, c := range label {
			if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '-') {
				return false
			}
		}
	}
	return true
}
