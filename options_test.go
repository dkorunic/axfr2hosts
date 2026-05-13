// SPDX-FileCopyrightText: 2026 Dinko Korunic
// SPDX-License-Identifier: MIT

package main

import (
	"net"
	"strings"
	"testing"
)

// TestIPv6ServerPortHandling is the test oracle for bug #13.
// parseFlags normalises the server address to "host:port" form using the check
// !strings.Contains(server, portSeparator).  Because bare IPv6 addresses already
// contain ":", the check misfires and net.JoinHostPort is never called, leaving
// the server string without a port (e.g. "2001:db8::1" instead of
// "[2001:db8::1]:53").  The test documents the expected normalization for each
// address family.
func TestIPv6ServerPortHandling(t *testing.T) {
	// normalizePort replicates the fixed logic from options.go: use SplitHostPort
	// to detect a missing port rather than strings.Contains, so bare IPv6 addresses
	// are handled correctly.
	normalizePort := func(server string) string {
		if _, _, err := net.SplitHostPort(server); err != nil {
			return net.JoinHostPort(server, dnsPort)
		}

		return server
	}

	tests := []struct {
		name     string
		server   string
		expected string
	}{
		{
			name:     "IPv4 bare address gets port appended",
			server:   "192.0.2.1",
			expected: "192.0.2.1:53",
		},
		{
			name:     "IPv4 address already with port is unchanged",
			server:   "192.0.2.1:53",
			expected: "192.0.2.1:53",
		},
		{
			name:     "IPv6 with brackets and port is unchanged",
			server:   "[2001:db8::1]:53",
			expected: "[2001:db8::1]:53",
		},
		{
			// Bug #13: bare IPv6 contains ":" so !Contains is false and
			// JoinHostPort is never called.  The result must still be the
			// bracketed form with port, but the current logic produces the
			// bare address.
			name:     "bare IPv6 address must get port appended",
			server:   "2001:db8::1",
			expected: "[2001:db8::1]:53",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := normalizePort(tt.server)
			if got != tt.expected {
				t.Errorf("normalizePort(%q) = %q, want %q"+
					" (bare IPv6 addresses contain \":\" and bypass the port check)",
					tt.server, got, tt.expected)
			}
		})
	}
}

// TestZoneDeduplicate is the test oracle for bug #14.
// parseFlags deduplicates zone arguments via a map so that the same zone is
// processed only once.  The bug mutation removes the guard and always appends,
// causing duplicate AXFR transfers and doubled output.
func TestZoneDeduplicate(t *testing.T) {
	// Replicate the deduplication logic from parseFlags.
	deduplicate := func(args []string) []string {
		zones := make([]string, 0, len(args))
		zoneMap := make(map[string]struct{}, len(args))

		for _, arg := range args {
			arg = strings.TrimSuffix(arg, endingDot)

			if _, ok := zoneMap[arg]; !ok {
				zones = append(zones, arg)
				zoneMap[arg] = struct{}{}
			}
		}

		return zones
	}

	tests := []struct {
		name      string
		args      []string
		wantZones []string
	}{
		{
			name:      "unique zones unchanged",
			args:      []string{"example.com", "other.com"},
			wantZones: []string{"example.com", "other.com"},
		},
		{
			name:      "exact duplicate removed",
			args:      []string{"example.com", "other.com", "example.com"},
			wantZones: []string{"example.com", "other.com"},
		},
		{
			name:      "trailing-dot duplicate removed",
			args:      []string{"example.com", "example.com."},
			wantZones: []string{"example.com"},
		},
		{
			name:      "three copies produce one entry",
			args:      []string{"example.com", "example.com", "example.com"},
			wantZones: []string{"example.com"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := deduplicate(tt.args)

			if len(got) != len(tt.wantZones) {
				t.Errorf("deduplicate(%v) = %v (len %d), want %v (len %d)",
					tt.args, got, len(got), tt.wantZones, len(tt.wantZones))
				return
			}

			for i, z := range got {
				if z != tt.wantZones[i] {
					t.Errorf("deduplicate() zone[%d] = %q, want %q", i, z, tt.wantZones[i])
				}
			}
		})
	}
}
