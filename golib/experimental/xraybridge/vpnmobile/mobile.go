// Package vpnmobile is the experimental gomobile API. The regular app
// does not call it yet; it must be built and linked before use.
package vpnmobile

import (
	"encoding/json"
	"errors"
	"net"
	"net/netip"
	"strconv"
	"sync"
	"syscall"
	"time"

	"netmerge-android/experimental/xraybridge"
	"netmerge-android/internal/engine"
	"netmerge-android/internal/nics"
	"netmerge-android/internal/profile"
	"netmerge-android/internal/tun"
)

var (
	mu sync.Mutex
	sched = engine.NewScheduler(nil)
	bal = engine.NewBalancer(sched)
	dev tun.Tun
	client *xraybridge.Client
	stop chan struct{}
	indexes = map[string]int{}
)

type network struct {
	Name string `json:"name"`
	Handle string `json:"handle"`
	V4 bool `json:"v4"`
	V6 bool `json:"v6"`
	Weight float64 `json:"weight"`
}

// SetNetworks accepts the original Android tracker format, without credentials.
func SetNetworks(raw string) error {
	var entries []network
	if err := json.Unmarshal([]byte(raw), &entries); err != nil { return errors.New("invalid network list") }
	mu.Lock()
	defer mu.Unlock()
	list := make([]nics.Nic,0,len(entries))
	seen := map[string]bool{}
	for _, n := range entries {
		h,err := strconv.ParseUint(n.Handle,10,64)
		if err != nil || h == 0 || n.Name == "" || seen[n.Name] { return errors.New("invalid network handle or name") }
		seen[n.Name] = true
		idx := indexes[n.Name]
		if idx == 0 { idx = len(indexes)+1; indexes[n.Name]=idx }
		nics.SetHandle(idx,h)
		w := n.Weight
		if w <= 0 { w = 1 }
		nic := nics.Nic{Name:n.Name, Index:idx, Weight:w}
		if n.V4 { nic.IP = net.IPv4zero }
		if n.V6 { nic.IP6 = net.IPv6unspecified }
		list = append(list,nic)
	}
	sched.Update(list)
	sched.CloseGone()
	return nil
}

// ValidateProfile checks syntax only, without network I/O.
func ValidateProfile(link string) error {
	_,err := profile.ParseRealityTCP(link)
	return err
}

// Start takes ownership of a detached TUN descriptor on entry. The endpoint
// must be a trusted, already resolved IP; no system DNS fallback is used here.
// Call only from a worker thread. Startup is not proof of VPN connectivity.
func Start(fd int, mtu int, link string, endpointIP string) error {
	mu.Lock()
	defer mu.Unlock()
	if fd < 0 { return errors.New("invalid TUN descriptor") }
	if client != nil || mtu < 1280 || mtu > 9000 {
		_ = syscall.Close(fd)
		return errors.New("VPN already running or invalid MTU")
	}
	p,err := profile.ParseRealityTCP(link)
	if err != nil { _ = syscall.Close(fd); return err }
	ip,err := netip.ParseAddr(endpointIP)
	if err != nil { _ = syscall.Close(fd); return errors.New("VPN endpoint requires an IP") }
	endpoint := netip.AddrPortFrom(ip.Unmap(),p.Port)
	c,err := xraybridge.Open(link,endpoint,bal)
	if err != nil { _ = syscall.Close(fd); return err }
	// The TUN owns the descriptor after this call. Application IPv6 is
	// independent of the address family used to reach the remote proxy.
	if err := dev.StartWithDialer(c,func()bool{return true},fd,mtu); err != nil {
		_ = c.Close()
		return errors.New("VPN TUN initialization failed")
	}
	client = c
	stop = make(chan struct{})
	go sched.RunEndpointProbes(stop,endpoint)
	go sample(stop)
	return nil
}

func sample(done <-chan struct{}) {
	t := time.NewTicker(time.Second)
	defer t.Stop()
	last := time.Now()
	for {
		select {
		case <-done: return
		case now := <-t.C:
			sched.Tick(now.Sub(last))
			last = now
		}
	}
}

func Stop() {
	mu.Lock()
	defer mu.Unlock()
	if client == nil { return }
	close(stop)
	_ = client.Close()
	dev.Stop()
	client = nil
	stop = nil
}

func IsRunning() bool {
	mu.Lock()
	defer mu.Unlock()
	return client != nil
}

func Stats() string {
	b,_ := json.Marshal(sched.Snapshot())
	return string(b)
}
