package profile

import (
	"strings"
	"testing"
)

const testID = "12345678-1234-4234-8234-123456789abc"
const testKey = "AQEBAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQE"
const testLink = "vless://" + testID + "@vpn.example:443?type=tcp&security=reality&encryption=none&sni=example.org&pbk=" + testKey + "&sid=abcd&fp=chrome&flow=xtls-rprx-vision&spx=%2F#Test%20node"

func TestParseRealityTCP(t *testing.T) {
	p, err := ParseRealityTCP(testLink)
	if err != nil {
		t.Fatal(err)
	}
	if p.UUID != testID || p.Server != "vpn.example" || p.Port != 443 || p.ServerName != "example.org" || p.PublicKey != testKey || p.ShortID != "abcd" || p.Fingerprint != "chrome" || p.Flow != "xtls-rprx-vision" || p.SpiderX != "/" || p.Label != "Test node" {
		t.Fatal("incorrect parsed profile")
	}
	if _, err := ParseRealityTCP(strings.Replace(testLink, "vpn.example", "[2001:db8::1]", 1)); err != nil {
		t.Fatal("IPv6 endpoint rejected")
	}
}

func TestRejectUnsupportedOrMalformed(t *testing.T) {
	for _, pair := range [][2]string{{"security=reality", "security=none"}, {"type=tcp", "type=ws"}, {"encryption=none", "encryption=custom"}, {"sid=abcd", "sid=abc"}, {testKey, "broken"}, {"fp=chrome", "fp=unknown"}, {"sni=example.org", "sni="}, {":443?", ":0?"}, {testID, "not-a-uuid"}, {"&sid=abcd", "&sid=abcd&sid=ef"}, {"&sid=abcd", "&sid=abcd&allowInsecure=1"}, {"vpn.example", "bad_host"}, {"spx=%2F", "spx=bad"}, {"#Test%20node", "%ZZ#Test"}} {
		_, err := ParseRealityTCP(strings.Replace(testLink, pair[0], pair[1], 1))
		if err == nil {
			t.Fatal("malformed variant accepted: " + pair[0])
		}
		if strings.Contains(err.Error(), testID) {
			t.Fatal("error leaks credentials")
		}
	}
}
