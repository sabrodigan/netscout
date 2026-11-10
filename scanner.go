package main

import (
	"context"
	"errors"
	"fmt"
	"net"
	"os"
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
	nameWorkers = 16                     // concurrent hostname lookups
	dnsTimeout  = 3 * time.Second        // per-server PTR lookup timeout
	mdnsTimeout = time.Second            // unicast mDNS query timeout

	// maxProbes caps addresses × ports per scan, bounding both memory and
	// worst-case duration (~7 minutes when every probe times out).
	maxProbes = 1 << 16
	maxPorts  = 1024
)

// errScanInProgress is returned by runScan when another scan is still running.
var errScanInProgress = errors.New("a scan is already in progress")

// runScan probes every usable address in cidr against ports and returns a
// populated Scan. A host is considered "up" if any probed port either accepts
// the connection (open) or actively refuses it (RST) — a refusal still proves
// the host is alive. Hostnames are resolved for every up host once probing ends.
func runScan(cidr string, ports []int) (*Scan, error) {
	if len(ports) == 0 {
		ports = defaultPorts
	}
	ports, err := normalizePorts(ports)
	if err != nil {
		return nil, err
	}
	if err := validateTarget(cidr, len(ports)); err != nil {
		return nil, err
	}
	addrs, err := hostsInCIDR(cidr)
	if err != nil {
		return nil, err
	}

	if !globalScanActive.CompareAndSwap(false, true) {
		return nil, errScanInProgress
	}
	defer globalScanActive.Store(false)
	globalScanTotal.Store(int32(len(addrs)))
	globalScanScanned.Store(0)
	globalScanUp.Store(0)

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
	resolveHostnames(hosts, dnsServers(os.Getenv("DNS_SERVERS")))

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
	return h, true
}

// resolveHostnames fills in Hostname for every host. It runs after probing
// with its own small pool: the system resolver may sit behind several
// forwarding hops (e.g. a VM's NAT DNS proxy) that drop answers when flooded.
func resolveHostnames(hosts []Host, servers []*net.Resolver) {
	sem := make(chan struct{}, nameWorkers)
	var wg sync.WaitGroup
	for i := range hosts {
		wg.Add(1)
		sem <- struct{}{}
		go func(h *Host) {
			defer wg.Done()
			defer func() { <-sem }()
			h.Hostname = lookupHostname(h.IP, servers)
		}(&hosts[i])
	}
	wg.Wait()
}

// lookupHostname asks the host for the name it currently announces, then falls
// back to DNS. A router keeps the DHCP name from the previous operating system
// on that address, so its answer must not hide a renamed machine.
func lookupHostname(ip string, servers []*net.Resolver) string {
	announced := reverseDNS(resolverAt(net.JoinHostPort(ip, "5353")), ip, mdnsTimeout)
	resolved := ""
	if announced == "" {
		resolved = reverseDNS(net.DefaultResolver, ip, dnsTimeout)
		if resolved == "" {
			for _, r := range servers {
				if resolved = reverseDNS(r, ip, dnsTimeout); resolved != "" {
					break
				}
			}
		}
	}
	return deviceHostname(announced, resolved)
}

// deviceHostname prefers the name the host announced. The mDNS suffix is not
// part of the hostname the machine was given.
func deviceHostname(announced, resolved string) string {
	name := strings.TrimSuffix(strings.TrimSpace(announced), ".")
	if strings.HasSuffix(strings.ToLower(name), ".local") {
		name = strings.TrimSpace(name[:len(name)-len(".local")])
	}
	if name != "" {
		return name
	}
	return strings.TrimSuffix(strings.TrimSpace(resolved), ".")
}

// dnsServers parses a comma-separated list of DNS servers ("192.168.1.1" or
// "192.168.1.1:53") into resolvers. Invalid entries are skipped.
func dnsServers(list string) []*net.Resolver {
	var out []*net.Resolver
	for _, s := range strings.Split(list, ",") {
		s = strings.TrimSpace(s)
		if s == "" {
			continue
		}
		if _, _, err := net.SplitHostPort(s); err != nil {
			s = net.JoinHostPort(s, "53")
		}
		if host, _, _ := net.SplitHostPort(s); net.ParseIP(host) == nil {
			continue
		}
		out = append(out, resolverAt(s))
	}
	return out
}

// resolverAt returns a pure-Go resolver that sends every query to addr.
func resolverAt(addr string) *net.Resolver {
	return &net.Resolver{
		PreferGo: true,
		Dial: func(ctx context.Context, network, _ string) (net.Conn, error) {
			var d net.Dialer
			return d.DialContext(ctx, network, addr)
		},
	}
}

// reverseDNS returns the first PTR name for ip via r (trailing dot trimmed), or "".
func reverseDNS(r *net.Resolver, ip string, timeout time.Duration) string {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
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

// normalizePorts rejects out-of-range ports and returns the list sorted and
// de-duplicated.
func normalizePorts(ports []int) ([]int, error) {
	seen := make(map[int]bool, len(ports))
	out := make([]int, 0, len(ports))
	for _, p := range ports {
		if p < 1 || p > 65535 {
			return nil, fmt.Errorf("invalid port %d: must be 1-65535", p)
		}
		if !seen[p] {
			seen[p] = true
			out = append(out, p)
		}
	}
	if len(out) > maxPorts {
		return nil, fmt.Errorf("too many ports (%d): at most %d per scan", len(out), maxPorts)
	}
	sort.Ints(out)
	return out, nil
}

// validateTarget restricts scans to private, loopback, or link-local IPv4
// ranges and enforces the maxProbes budget before any addresses are enumerated.
func validateTarget(cidr string, portCount int) error {
	_, ipnet, err := net.ParseCIDR(cidr)
	if err != nil {
		return fmt.Errorf("invalid CIDR %q: %w", cidr, err)
	}
	network := ipnet.IP.To4()
	if network == nil {
		return errors.New("only IPv4 CIDR ranges are supported")
	}
	ones, bits := ipnet.Mask.Size()
	// Every private block is aligned and at least a /16, so once the size is
	// capped below that, a private first address means the whole range is private.
	hosts := 1 << (bits - ones)
	if hosts*portCount > maxProbes {
		return fmt.Errorf("scan too large: %d addresses × %d ports exceeds %d probes", hosts, portCount, maxProbes)
	}
	if !network.IsPrivate() && !network.IsLoopback() && !network.IsLinkLocalUnicast() {
		return fmt.Errorf("refusing to scan %s: only private, loopback, or link-local ranges are allowed", ipnet)
	}
	return nil
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
// for use as the default scan target, narrowed to the host's own /24 when the
// interface network is wider. Returns "" if none can be determined.
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
			mask := ipnet.Mask
			if ones, _ := mask.Size(); ones < 24 {
				mask = net.CIDRMask(24, 32)
			}
			return (&net.IPNet{IP: ipnet.IP.Mask(mask), Mask: mask}).String()
		}
	}
	return ""
}
