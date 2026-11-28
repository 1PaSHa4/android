package xraybridge

import (
	"context"
	"net"
	"net/netip"
	"testing"
	"time"

	xnet "github.com/xtls/xray-core/common/net"
	"netmerge-android/internal/vpntransport"
)

type pipeDialer struct { peer net.Conn }
func (p *pipeDialer) DialPinned(context.Context,string,int)(net.Conn,error) {
	a,b := net.Pipe()
	p.peer=b
	return a,nil
}

func TestSystemGateClosesPhysicalSocketsAndRejectsStaleSessions(t *testing.T) {
	var g systemGate
	p := &pipeDialer{}
	endpoint:=netip.MustParseAddrPort("192.0.2.1:443")
	d,err:=vpntransport.NewEndpointDialer(p,endpoint)
	if err != nil {t.Fatal(err)}
	g.set(d)
	defer g.set(nil)
	dest:=xnet.TCPDestination(xnet.ParseAddress("192.0.2.1"),443)
	if _,err:=g.Dial(context.Background(),nil,dest,nil);err==nil {t.Fatal("unscoped connection accepted")}
	ctx:=context.WithValue(context.Background(),sessionKey{},d)
	c,err:=g.Dial(ctx,nil,dest,nil)
	if err!=nil {t.Fatal(err)}
	defer c.Close()
	defer p.peer.Close()
	g.set(nil)
	_ = p.peer.SetReadDeadline(time.Now().Add(time.Second))
	if _,err:=p.peer.Read(make([]byte,1));err==nil {t.Fatal("old physical socket remained open")}
	d2,err:=vpntransport.NewEndpointDialer(p,endpoint)
	if err!=nil {t.Fatal(err)}
	g.set(d2)
	if _,err:=g.Dial(ctx,nil,dest,nil);err==nil {t.Fatal("stale session accepted")}
}
