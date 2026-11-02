package main

import (
	"errors"
	"reflect"
	"testing"
)

func TestValidateTarget(t *testing.T) {
	tests := []struct {
		cidr    string
		ports   int
		wantErr bool
	}{
		{"192.168.1.0/24", len(defaultPorts), false},
		{"10.0.0.0/16", 1, false},
		{"172.16.5.0/24", 12, false},
		{"127.0.0.0/24", 12, false},
		{"169.254.0.0/24", 12, false},
		{"8.8.8.0/24", 12, true},      // public range
		{"0.0.0.0/0", 1, true},        // far over the probe budget
		{"10.0.0.0/8", 1, true},       // private but over budget
		{"10.0.0.0/16", 2, true},      // budget is addresses × ports
		{"192.168.1.0/24", 257, true}, // 256 × 257 > maxProbes
		{"fd00::/64", 1, true},        // IPv6
		{"not-a-cidr", 1, true},
	}
	for _, tt := range tests {
		err := validateTarget(tt.cidr, tt.ports)
		if (err != nil) != tt.wantErr {
			t.Errorf("validateTarget(%q, %d) error = %v, wantErr %v", tt.cidr, tt.ports, err, tt.wantErr)
		}
	}
}

func TestNormalizePorts(t *testing.T) {
	got, err := normalizePorts([]int{443, 22, 80, 22})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if want := []int{22, 80, 443}; !reflect.DeepEqual(got, want) {
		t.Errorf("normalizePorts = %v, want %v", got, want)
	}

	for _, bad := range [][]int{{0}, {-1}, {65536}} {
		if _, err := normalizePorts(bad); err == nil {
			t.Errorf("normalizePorts(%v) accepted an invalid port", bad)
		}
	}

	many := make([]int, maxPorts+1)
	for i := range many {
		many[i] = i + 1
	}
	if _, err := normalizePorts(many); err == nil {
		t.Errorf("normalizePorts accepted %d ports", len(many))
	}
}

func TestHostsInCIDR(t *testing.T) {
	addrs, err := hostsInCIDR("192.168.1.0/30")
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"192.168.1.1", "192.168.1.2"}; !reflect.DeepEqual(addrs, want) {
		t.Errorf("hostsInCIDR(/30) = %v, want %v", addrs, want)
	}

	addrs, err = hostsInCIDR("192.168.1.0/24")
	if err != nil {
		t.Fatal(err)
	}
	if len(addrs) != 254 {
		t.Errorf("hostsInCIDR(/24) returned %d addresses, want 254", len(addrs))
	}
}

func TestRunScanRejectsConcurrentScans(t *testing.T) {
	globalScanActive.Store(true)
	defer globalScanActive.Store(false)

	_, err := runScan("127.0.0.0/30", []int{1})
	if !errors.Is(err, errScanInProgress) {
		t.Errorf("runScan during an active scan: err = %v, want errScanInProgress", err)
	}
}
