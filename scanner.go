package main

import (
	"context"
	"errors"
	"fmt"
	"net"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

var (
	globalScanActive  atomic.Bool
	globalScanTotal   atomic.Int32
	globalScanScanned atomic.Int32
	globalScanUp      atomic.Int32
)

// defaultPorts is the set of TCP ports probed when a scan request omits its own.
var defaultPorts = []int{21, 22, 23, 25, 80, 139, 443, 445, 3306, 3389, 5432, 8080}

const (
	dialTimeout = 800 * time.Millisecond // per-port connect timeout
	scanWorkers = 128                    // concurrent host probes
)

// runScan probes every usable address in cidr against ports and returns a
// populated Scan. A host is considered "up" if any probed port either accepts
// the connection (open) or actively refuses it (RST) — a refusal still proves
// the host is alive. Reverse DNS is attempted for every up host.
func runScan(cidr string, ports []int) (*Scan, error) {
	if len(ports) == 0 {
		ports = defaultPorts
	}
	addrs, err := hostsInCIDR(cidr)
	if err != nil {
		return nil, err
	}

	globalScanTotal.Store(int32(len(addrs)))
	globalScanScanned.Store(0)
	globalScanUp.Store(0)
	globalScanActive.Store(true)
	defer globalScanActive.Store(false)

	started := time.Now()

	type job struct{ ip string }
	jobs := make(chan job)
	results := make(chan Host)

	var wg sync.WaitGroup
	for i := 0; i < scanWorkers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := range jobs {
				if h, up := probeHost(j.ip, ports); up {
					globalScanUp.Add(1)
					results <- h
				}
				globalScanScanned.Add(1)
			}
		}()
	}

	go func() {
		for _, ip := range addrs {
			jobs <- job{ip: ip}
		}
		close(jobs)
	}()

	go func() {
		wg.Wait()
		close(results)
	}()

	var hosts []Host
	for h := range results {
		hosts = append(hosts, h)
	}
	sort.Slice(hosts, func(i, j int) bool {
		return ipLess(hosts[i].IP, hosts[j].IP)
	})

	scan := &Scan{
		CIDR:       cidr,
		Ports:      ports,
		StartedAt:  started,
		FinishedAt: time.Now(),
		HostCount:  len(hosts),
		Hosts:      hosts,
	}
	return scan, nil
}

// probeHost attempts a TCP connect to each port. It returns the Host and true
// if the host appears alive (any open port or connection-refused response).
func probeHost(ip string, ports []int) (Host, bool) {
	var open []int
	alive := false

	for _, p := range ports {
		addr := net.JoinHostPort(ip, fmt.Sprintf("%d", p))
		conn, err := net.DialTimeout("tcp", addr, dialTimeout)
		if err == nil {
			conn.Close()
			open = append(open, p)
			alive = true
			continue
		}
		// A refused connection means the host is up but the port is closed.
		if isConnRefused(err) {
			alive = true
		}
	}

	if !alive {
		return Host{}, false
	}

	sort.Ints(open)
	h := Host{IP: ip, Status: "up", OpenPorts: open}
	if h.OpenPorts == nil {
		h.OpenPorts = []int{}
	}
	h.Hostname = reverseDNS(ip)
	return h, true
}

// reverseDNS returns the first PTR name for ip (trailing dot trimmed), or "".
func reverseDNS(ip string) string {
	ctx, cancel := context.WithTimeout(context.Background(), 700*time.Millisecond)
	defer cancel()
	var r net.Resolver
	names, err := r.LookupAddr(ctx, ip)
	if err != nil || len(names) == 0 {
		return ""
	}
	return strings.TrimSuffix(names[0], ".")
}

// isConnRefused reports whether err is a TCP connection-refused error.
func isConnRefused(err error) bool {
	var opErr *net.OpError
	if errors.As(err, &opErr) {
		return strings.Contains(strings.ToLower(opErr.Err.Error()), "refused")
	}
	return strings.Contains(strings.ToLower(err.Error()), "refused")
}

// hostsInCIDR returns every usable host address in cidr, skipping the network
// and broadcast addresses for IPv4 blocks smaller than /31.
func hostsInCIDR(cidr string) ([]string, error) {
	ip, ipnet, err := net.ParseCIDR(cidr)
	if err != nil {
		return nil, fmt.Errorf("invalid CIDR %q: %w", cidr, err)
	}
	if ip.To4() == nil {
		return nil, errors.New("only IPv4 CIDR ranges are supported")
	}

	var addrs []string
	for cur := ip.Mask(ipnet.Mask); ipnet.Contains(cur); cur = nextIP(cur) {
		addrs = append(addrs, cur.String())
	}

	// Drop network + broadcast for blocks with more than 2 addresses.
	if ones, bits := ipnet.Mask.Size(); bits-ones >= 2 && len(addrs) >= 2 {
		addrs = addrs[1 : len(addrs)-1]
	}
	return addrs, nil
}

// nextIP returns a copy of ip incremented by one.
func nextIP(ip net.IP) net.IP {
	out := make(net.IP, len(ip))
	copy(out, ip)
	for i := len(out) - 1; i >= 0; i-- {
		out[i]++
		if out[i] != 0 {
			break
		}
	}
	return out
}

// ipLess orders two dotted-quad IPv4 strings numerically.
func ipLess(a, b string) bool {
	ea, eb := net.ParseIP(a).To4(), net.ParseIP(b).To4()
	if ea == nil || eb == nil {
		return a < b
	}
	for i := 0; i < 4; i++ {
		if ea[i] != eb[i] {
			return ea[i] < eb[i]
		}
	}
	return false
}

// localCIDR derives the primary non-loopback IPv4 network (e.g. 192.168.1.0/24)
// for use as the default scan target. Returns "" if none can be determined.
func localCIDR() string {
	ifaces, err := net.Interfaces()
	if err != nil {
		return ""
	}
	for _, iface := range ifaces {
		if iface.Flags&net.FlagUp == 0 || iface.Flags&net.FlagLoopback != 0 {
			continue
		}
		addrs, err := iface.Addrs()
		if err != nil {
			continue
		}
		for _, a := range addrs {
			ipnet, ok := a.(*net.IPNet)
			if !ok || ipnet.IP.To4() == nil {
				continue
			}
			return (&net.IPNet{IP: ipnet.IP.Mask(ipnet.Mask), Mask: ipnet.Mask}).String()
		}
	}
	return ""
}
