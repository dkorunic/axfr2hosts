package main

import (
	"net/netip"
	"testing"
)

func TestProcessHost(t *testing.T) {
	// Save original flag values
	origStripDomain := *stripDomain
	origStripUnstrip := *stripUnstrip
	defer func() {
		*stripDomain = origStripDomain
		*stripUnstrip = origStripUnstrip
	}()

	tests := []struct {
		name         string
		label        string
		zone         string
		ipAddr       netip.Addr
		stripDomain  bool
		stripUnstrip bool
		expected     []HostEntry
	}{
		{
			name:   "Basic processing",
			label:  "host.example.com.",
			zone:   "example.com",
			ipAddr: netip.MustParseAddr("192.0.2.1"),
			expected: []HostEntry{
				{label: "host.example.com", ipAddr: netip.MustParseAddr("192.0.2.1")},
			},
		},
		{
			name:        "Strip domain",
			label:       "host.example.com.",
			zone:        "example.com",
			ipAddr:      netip.MustParseAddr("192.0.2.1"),
			stripDomain: true,
			expected: []HostEntry{
				{label: "host", ipAddr: netip.MustParseAddr("192.0.2.1")},
			},
		},
		{
			name:         "Strip and Unstrip",
			label:        "host.example.com.",
			zone:         "example.com",
			ipAddr:       netip.MustParseAddr("192.0.2.1"),
			stripUnstrip: true,
			expected: []HostEntry{
				{label: "host", ipAddr: netip.MustParseAddr("192.0.2.1")},
				{label: "host.example.com", ipAddr: netip.MustParseAddr("192.0.2.1")},
			},
		},
		{
			name:        "Strip domain no match",
			label:       "host.other.com.",
			zone:        "example.com",
			ipAddr:      netip.MustParseAddr("192.0.2.1"),
			stripDomain: true,
			expected: []HostEntry{
				{label: "host.other.com", ipAddr: netip.MustParseAddr("192.0.2.1")},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			*stripDomain = tt.stripDomain
			*stripUnstrip = tt.stripUnstrip

			hosts := make(chan HostEntry, 10)
			processHost(tt.label, tt.zone, tt.ipAddr, hosts)
			close(hosts)

			var results []HostEntry
			for h := range hosts {
				results = append(results, h)
			}

			if len(results) != len(tt.expected) {
				t.Errorf("processHost() generated %d entries, want %d", len(results), len(tt.expected))
				return
			}

			for i, res := range results {
				if res != tt.expected[i] {
					t.Errorf("processHost() result[%d] = %v, want %v", i, res, tt.expected[i])
				}
			}
		})
	}
}

func TestWriteHostEntries(t *testing.T) {
	hosts := make(chan HostEntry, 3)
	ip1 := netip.MustParseAddr("192.0.2.1")
	ip2 := netip.MustParseAddr("192.0.2.2")

	hosts <- HostEntry{label: "host1", ipAddr: ip1}
	hosts <- HostEntry{label: "host2", ipAddr: ip1}
	hosts <- HostEntry{label: "host3", ipAddr: ip2}
	close(hosts)

	keys := []netip.Addr{}
	entries := make(HostMap)

	writeHostEntries(hosts, &keys, entries)

	if len(keys) != 2 {
		t.Errorf("writeHostEntries() keys length = %d, want 2", len(keys))
	}

	if len(entries) != 2 {
		t.Errorf("writeHostEntries() entries length = %d, want 2", len(entries))
	}

	if _, ok := entries[ip1]["host1"]; !ok {
		t.Errorf("writeHostEntries() missing host1 for ip1")
	}
	if _, ok := entries[ip1]["host2"]; !ok {
		t.Errorf("writeHostEntries() missing host2 for ip1")
	}
	if _, ok := entries[ip2]["host3"]; !ok {
		t.Errorf("writeHostEntries() missing host3 for ip2")
	}
}
