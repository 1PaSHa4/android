package xraybridge

import (
	"context"
	"io"
	"net"
	"net/netip"
	"testing"
	"time"
)

type observedDial struct {
	host string
	port int
	first byte
	err error
}
type recordingDialer struct { events chan observedDial }
func (r recordingDialer) DialPinned(_ context.Context, host string, port int) (net.Conn,error) {
	local,remote:=net.Pipe()
	go func(){
		defer remote.Close()
		_ = remote.SetReadDeadline(time.Now().Add(4*time.Second))
		var b [5]byte
		_,err:=io.ReadFull(remote,b[:])
		select {case r.events<-observedDial{host,port,b[0],err}:default:}
	}()
	return local,nil
}

// This is an offline path test: it proves Xray initiates a TLS record over
// the pinned physical endpoint, not that a real REALITY server authenticates.
func TestApplicationTCPReachesPinnedRealityTransport(t *testing.T) {
	const link = "vless://12345678-1234-4234-8234-123456789abc@192.0.2.1:443?type=tcp&security=reality&encryption=none&sni=example.org&pbk=AQEBAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQE&sid=abcd&fp=chrome"
	events:=make(chan observedDial,4)
	c,err:=Open(link,netip.MustParseAddrPort("192.0.2.1:443"),recordingDialer{events})
	if err!=nil {t.Fatal(err)}
	defer c.Close()
	ctx,cancel:=context.WithTimeout(context.Background(),6*time.Second)
	defer cancel()
	app,err:=c.DialTCP(ctx,netip.MustParseAddrPort("198.51.100.2:80"))
	if err!=nil {t.Fatal(err)}
	defer app.Close()
	go func(){_,_=app.Write([]byte("test"))}()
	select {
	case e:=<-events:
		if e.host!="192.0.2.1" || e.port!=443 {t.Fatal("wrong physical endpoint")}
		if e.err!=nil || e.first!=0x16 {t.Fatal("expected initial TLS handshake record on pinned socket")}
	case <-ctx.Done(): t.Fatal("Xray did not reach the pinned transport")
	}
}
