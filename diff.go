package main

import "sort"

// diffScans compares two scans and reports hosts added, removed, or changed
// going from "from" to "to", keyed by IP address.
func diffScans(from, to *Scan) ScanDiff {
	fromByIP := indexHosts(from.Hosts)
	toByIP := indexHosts(to.Hosts)

	diff := ScanDiff{
		FromID:  from.ID,
		ToID:    to.ID,
		Added:   []Host{},
		Removed: []Host{},
		Changed: []HostChange{},
	}

	// Added + changed: walk the "to" hosts.
	for ip, th := range toByIP {
		fh, existed := fromByIP[ip]
		if !existed {
			diff.Added = append(diff.Added, th)
			continue
		}
		added, removed := portDelta(fh.OpenPorts, th.OpenPorts)
		if len(added) > 0 || len(removed) > 0 || fh.Status != th.Status {
			diff.Changed = append(diff.Changed, HostChange{
				IP:           ip,
				Hostname:     th.Hostname,
				FromStatus:   fh.Status,
				ToStatus:     th.Status,
				AddedPorts:   added,
				RemovedPorts: removed,
			})
		}
	}

	// Removed: hosts present in "from" but not "to".
	for ip, fh := range fromByIP {
		if _, ok := toByIP[ip]; !ok {
			diff.Removed = append(diff.Removed, fh)
		}
	}

	sort.Slice(diff.Added, func(i, j int) bool { return ipLess(diff.Added[i].IP, diff.Added[j].IP) })
	sort.Slice(diff.Removed, func(i, j int) bool { return ipLess(diff.Removed[i].IP, diff.Removed[j].IP) })
	sort.Slice(diff.Changed, func(i, j int) bool { return ipLess(diff.Changed[i].IP, diff.Changed[j].IP) })
	return diff
}

func indexHosts(hosts []Host) map[string]Host {
	m := make(map[string]Host, len(hosts))
	for _, h := range hosts {
		m[h.IP] = h
	}
	return m
}

// portDelta returns ports present in "to" but not "from" (added) and vice
// versa (removed).
func portDelta(from, to []int) (added, removed []int) {
	fromSet := make(map[int]bool, len(from))
	for _, p := range from {
		fromSet[p] = true
	}
	toSet := make(map[int]bool, len(to))
	for _, p := range to {
		toSet[p] = true
	}
	added, removed = []int{}, []int{}
	for _, p := range to {
		if !fromSet[p] {
			added = append(added, p)
		}
	}
	for _, p := range from {
		if !toSet[p] {
			removed = append(removed, p)
		}
	}
	sort.Ints(added)
	sort.Ints(removed)
	return added, removed
}
