// Package vpnmobile is the gomobile API for the separate experimental VPN APK.
package vpnmobile

import (
	"encoding/json"
	"errors"
	"net"
	"math"
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
	nextIndex int
	auto = true
	lastNetworks string
)

func init() { sched.SetAuto(true) }

type network struct {
	Name string `json:"name"`
	Handle string `json:"handle"`
	V4 bool `json:"v4"`
	V6 bool `json:"v6"`
	Weight float64 `json:"weight"`
	DownKbps float64 `json:"downKbps"`
}

// SetNetworks accepts the original Android tracker format, without credentials.
func SetNetworks(raw string) error {
	mu.Lock()
	defer mu.Unlock()
	return setNetworksLocked(raw)
}

func setNetworksLocked(raw string) error {
	var entries []network
	if err := json.Unmarshal([]byte(raw), &entries); err != nil { return errors.New("invalid network list") }
	// Validate the entire snapshot before touching any live handles.
	seen := map[string]bool{}
	handles := make([]uint64,len(entries))
	for i,n := range entries {
		h,err := strconv.ParseUint(n.Handle,10,64)
		if err != nil || h == 0 || n.Name == "" || seen[n.Name] || math.IsNaN(n.Weight) || math.IsInf(n.Weight,0) || math.IsNaN(n.DownKbps) || math.IsInf(n.DownKbps,0) {
			return errors.New("invalid network snapshot")
		}
		seen[n.Name] = true
		handles[i] = h
	}
	var maxDown float64
	for _,n := range entries { maxDown = max(maxDown,n.DownKbps) }
	list := make([]nics.Nic,0,len(entries))
	updated := make(map[string]int,len(entries))
	for i,n := range entries {
		// A newly connected network must not inherit the previous network's
		// scheduler state or physical sockets, even if the UI name is the same.
		key := n.Name + " [" + strconv.FormatUint(handles[i],10) + "]"
		idx := indexes[key]
		if idx == 0 { nextIndex++; idx = nextIndex }
		updated[key]=idx
		nics.SetHandle(idx,handles[i])
		w := n.Weight
		if w <= 0 { w = 1; if auto && maxDown > 0 && n.DownKbps > 0 { w = max(0.1,n.DownKbps/maxDown) } }
		nic := nics.Nic{Name:key, Index:idx, Weight:w}
		if n.V4 { nic.IP = net.IPv4zero }
		if n.V6 { nic.IP6 = net.IPv6unspecified }
		list = append(list,nic)
	}
	for key,idx := range indexes {
		if _,ok := updated[key]; !ok { nics.DropHandle(idx) }
	}
	indexes = updated
	lastNetworks = raw
	sched.Update(list)
	sched.CloseGone()
	return nil
}

func SetAutoWeights(on bool) {
	mu.Lock()
	auto = on
	sched.SetAuto(on)
	if lastNetworks != "" { _ = setNetworksLocked(lastNetworks) }
	mu.Unlock()
}

// ValidateProfile checks syntax only, without network I/O.
func ValidateProfile(link string) error {
	_,err := profile.ParseRealityTCP(link)
	return err
}

// ValidateEndpoint checks the actual core config, with no network I/O.
func ValidateEndpoint(link string, endpointIP string) error {
	p,err := profile.ParseRealityTCP(link)
	if err != nil { return err }
	ip,err := netip.ParseAddr(endpointIP)
	if err != nil { return errors.New("VPN endpoint requires an IP") }
	return xraybridge.ValidateConfig(link,netip.AddrPortFrom(ip.Unmap(),p.Port))
}

// Start takes ownership of a detached TUN descriptor on entry. The endpoint
// must be a trusted, already resolved IP; no system DNS fallback is used here.
// Call only from a worker thread. Startup is not proof of VPN connectivity.
func Start(fd int, mtu int, link string, endpointIP string) error {
	mu.Lock()
	defer mu.Unlock()
	if fd < 0 { return errors.New("invalid TUN descriptor") }
	if stop != nil || mtu < 1280 || mtu > 9000 {
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

// StartMergeOnly is a separate, explicitly selected unencrypted mode.
// It must never be called as a fallback from Start.
func StartMergeOnly(fd int, mtu int) error {
	mu.Lock()
	defer mu.Unlock()
	if fd < 0 { return errors.New("invalid TUN descriptor") }
	if stop != nil || mtu < 1280 || mtu > 9000 {
		_ = syscall.Close(fd)
		return errors.New("engine already running or invalid MTU")
	}
	if err := dev.Start(bal,fd,mtu); err != nil { return errors.New("merge TUN initialization failed") }
	stop = make(chan struct{})
	go sched.RunProbes(stop)
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
	if stop == nil { return }
	close(stop)
	if client != nil { _ = client.Close() }
	dev.Stop()
	client = nil
	stop = nil
}

func IsRunning() bool {
	mu.Lock()
	defer mu.Unlock()
	return stop != nil
}

func Stats() string {
	snap := sched.Snapshot()
	out := make([]map[string]any,0,len(snap))
	for _,s := range snap {
		out = append(out,map[string]any{
			"name":s.Name,"state":s.State,"state6":s.State6,"active":s.Active,
			"total":s.Total,"failed":s.Failed,"bytesIn":s.BytesIn,"bytesOut":s.BytesOut,
			"rateIn":s.RateIn,"rateOut":s.RateOut,"weight":s.Weight,"present":s.Present,
		})
	}
	b,_ := json.Marshal(out)
	return string(b)
}
