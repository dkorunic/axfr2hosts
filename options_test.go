// SPDX-FileCopyrightText: 2026 Dinko Korunic
// SPDX-License-Identifier: MIT

package main

import (
	"flag"
	"os"
	"strings"
	"testing"
)

// TestNormalizeAddrPort is the test oracle for bug #13 and its resolver-side twin.
// Addresses are normalised to "host:port" form.  A strings.Contains(":") check
// misfires on bare IPv6 addresses, which already contain ":", leaving them without
// a port and therefore unusable as a dial target.  normalizeAddrPort uses
// net.SplitHostPort instead, which distinguishes "no port" from "IPv6 colons".
func TestNormalizeAddrPort(t *testing.T) {
	tests := []struct {
		name string
		addr string
		want string
	}{
		{
			name: "IPv4 bare address gets port appended",
			addr: "192.0.2.1",
			want: "192.0.2.1:53",
		},
		{
			name: "IPv4 address already with port is unchanged",
			addr: "192.0.2.1:53",
			want: "192.0.2.1:53",
		},
		{
			name: "IPv4 address with non-default port is unchanged",
			addr: "192.0.2.1:5353",
			want: "192.0.2.1:5353",
		},
		{
			name: "IPv6 with brackets and port is unchanged",
			addr: "[2001:db8::1]:53",
			want: "[2001:db8::1]:53",
		},
		{
			// bug #13: bare IPv6 contains ":" — a !Contains check skipped JoinHostPort
			name: "bare IPv6 address must get port appended",
			addr: "2001:db8::1",
			want: "[2001:db8::1]:53",
		},
		{
			name: "bare IPv6 loopback must get port appended",
			addr: "::1",
			want: "[::1]:53",
		},
		{
			name: "fully expanded bare IPv6 must get port appended",
			addr: "2001:4860:4860:0000:0000:0000:0000:8888",
			want: "[2001:4860:4860:0000:0000:0000:0000:8888]:53",
		},
		{
			name: "hostname without port gets port appended",
			addr: "ns1.example.com",
			want: "ns1.example.com:53",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := normalizeAddrPort(tt.addr); got != tt.want {
				t.Errorf("normalizeAddrPort(%q) = %q, want %q"+
					" (bare IPv6 addresses contain \":\" and bypass a Contains-based port check)",
					tt.addr, got, tt.want)
			}
		})
	}
}

// TestResolverAddressNormalization guards the -resolver_address path specifically.
// It shared the bug class with the server path but was fixed later: a bare IPv6
// recursor silently lost its port, every DialContext failed with "missing port in
// address", and because processRecords discards lookup errors the visible symptom
// was CNAME records disappearing from the output with no diagnostic at all.
func TestResolverAddressNormalization(t *testing.T) {
	saved := *resolverAddress
	t.Cleanup(func() { *resolverAddress = saved })

	tests := []struct {
		name string
		in   string
		want string
	}{
		{"bare IPv4 recursor", "192.0.2.53", "192.0.2.53:53"},
		{"bare IPv6 recursor", "2001:4860:4860::8888", "[2001:4860:4860::8888]:53"},
		{"IPv6 recursor with explicit port", "[2001:4860:4860::8888]:5353", "[2001:4860:4860::8888]:5353"},
		{"IPv4 recursor with explicit port", "192.0.2.53:5353", "192.0.2.53:5353"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			*resolverAddress = tt.in

			// NOTE: this reimplements parseFlags' guard rather than exercising it,
			// so it constrains normalizeAddrPort only.  That parseFlags actually
			// applies the normalisation is covered by
			// TestParseFlagsAppliesResolverNormalization below.
			if *resolverAddress != "" {
				*resolverAddress = normalizeAddrPort(*resolverAddress)
			}

			if *resolverAddress != tt.want {
				t.Errorf("resolver address %q normalised to %q, want %q",
					tt.in, *resolverAddress, tt.want)
			}

			if _, _, err := splitHostPortCheck(*resolverAddress); err != nil {
				t.Errorf("normalised resolver address %q is not a valid dial target: %v",
					*resolverAddress, err)
			}
		})
	}
}

// TestParseZoneArgs covers zone deduplication (bug #14) plus server extraction.
// The dedup guard keeps the same zone from being transferred twice; dropping it
// would cause duplicate AXFRs and doubled output.
func TestParseZoneArgs(t *testing.T) {
	tests := []struct {
		name       string
		args       []string
		wantZones  []string
		wantServer string
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
		{
			name:      "input order is preserved",
			args:      []string{"zebra.com", "alpha.com", "zebra.com", "mid.com"},
			wantZones: []string{"zebra.com", "alpha.com", "mid.com"},
		},
		{
			name:       "server argument extracted and not treated as a zone",
			args:       []string{"example.com", "@192.0.2.1"},
			wantZones:  []string{"example.com"},
			wantServer: "192.0.2.1:53",
		},
		{
			name:       "bare IPv6 server argument gets bracketed port",
			args:       []string{"example.com", "@2001:db8::1"},
			wantZones:  []string{"example.com"},
			wantServer: "[2001:db8::1]:53",
		},
		{
			name:       "server with explicit port is preserved",
			args:       []string{"example.com", "@192.0.2.1:5353"},
			wantZones:  []string{"example.com"},
			wantServer: "192.0.2.1:5353",
		},
		{
			name:       "last server argument wins",
			args:       []string{"@192.0.2.1", "example.com", "@192.0.2.2"},
			wantZones:  []string{"example.com"},
			wantServer: "192.0.2.2:53",
		},
		{
			name:       "server only yields no zones",
			args:       []string{"@192.0.2.1"},
			wantZones:  []string{},
			wantServer: "192.0.2.1:53",
		},
		{
			name:      "file=domain argument is kept verbatim",
			args:      []string{"/tmp/db.example.com=example.com"},
			wantZones: []string{"/tmp/db.example.com=example.com"},
		},
		{
			name:      "no arguments yields no zones",
			args:      []string{},
			wantZones: []string{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotZones, gotServer := parseZoneArgs(tt.args)

			if gotServer != tt.wantServer {
				t.Errorf("parseZoneArgs(%v) server = %q, want %q", tt.args, gotServer, tt.wantServer)
			}

			if len(gotZones) != len(tt.wantZones) {
				t.Fatalf("parseZoneArgs(%v) zones = %v (len %d), want %v (len %d)",
					tt.args, gotZones, len(gotZones), tt.wantZones, len(tt.wantZones))
			}

			for i, z := range gotZones {
				if z != tt.wantZones[i] {
					t.Errorf("parseZoneArgs() zone[%d] = %q, want %q", i, z, tt.wantZones[i])
				}
			}
		})
	}
}

// TestCIDRListSplit covers the -cidr_list parsing branch of parseFlags, which
// splits on commas and must yield nil (not an empty slice) for an empty string so
// that rangerInit leaves doCIDR false and filtering stays disabled.
func TestCIDRListSplit(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want []string
	}{
		{"empty string disables filtering", "", nil},
		{"single CIDR", "192.0.2.0/24", []string{"192.0.2.0/24"}},
		{"two CIDRs", "192.0.2.0/24,2001:db8::/32", []string{"192.0.2.0/24", "2001:db8::/32"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// mirrors the cidrList branch in parseFlags
			var got []string
			if len(tt.in) > 0 {
				got = strings.Split(tt.in, cidrSeparator)
			}

			if len(got) != len(tt.want) {
				t.Fatalf("split(%q) = %v, want %v", tt.in, got, tt.want)
			}

			for i := range got {
				if got[i] != tt.want[i] {
					t.Errorf("split(%q)[%d] = %q, want %q", tt.in, i, got[i], tt.want[i])
				}
			}

			// an empty list must leave CIDR filtering switched off
			ranger, doCIDR := rangerInit(got)
			if tt.in == "" && (doCIDR || ranger != nil) {
				t.Errorf("empty cidr_list produced doCIDR=%v ranger!=nil=%v, want filtering disabled",
					doCIDR, ranger != nil)
			}
		})
	}
}

// TestAtLeastOne pins the clamp guarding two limits where zero is pathological:
// a zero-capacity semaphore deadlocks main, and retry-go reads Attempts(0) as
// "retry forever".
func TestAtLeastOne(t *testing.T) {
	tests := []struct {
		name string
		in   uint
		want uint
	}{
		{"zero clamps to one", 0, 1},
		{"one is unchanged", 1, 1},
		{"typical value unchanged", 10, 10},
		{"large value unchanged", 1 << 20, 1 << 20},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := atLeastOne(tt.in); got != tt.want {
				t.Errorf("atLeastOne(%d) = %d, want %d", tt.in, got, tt.want)
			}
		})
	}
}

// TestParseFlagsAppliesResolverNormalization drives the real parseFlags rather
// than reimplementing its guard.
//
// TestResolverAddressNormalization above constrains normalizeAddrPort in
// isolation, and the CLI-level test asserts only an exit code and a line count on
// A/AAAA-only data, where the resolver flag cannot affect the output.  Nothing
// checked that parseFlags applies the normalisation at all, so inverting the guard
// went unnoticed: a supplied address would be left undialable, and — the case that
// makes this cheap to detect — an *unset* address would be rewritten to ":53",
// silently pointing every CNAME lookup at localhost.
func TestParseFlagsAppliesResolverNormalization(t *testing.T) {
	savedArgs := os.Args
	savedAddr := *resolverAddress
	// parseFlags installs a flag.Usage that calls os.Exit(0).  Left in place it
	// terminates the whole test binary the next time any other test drives a code
	// path that reaches flag.Usage, so it must be restored too.
	savedUsage := flag.Usage

	t.Cleanup(func() {
		os.Args = savedArgs
		*resolverAddress = savedAddr
		flag.Usage = savedUsage
	})

	for _, tt := range []struct {
		name string
		args []string
		want string
	}{
		{
			name: "bare IPv4 gains the default port",
			args: []string{"-resolver_address", "192.0.2.53"},
			want: "192.0.2.53:53",
		},
		{
			name: "bare IPv6 is bracketed and gains the default port",
			args: []string{"-resolver_address", "2001:db8::53"},
			want: "[2001:db8::53]:53",
		},
		{
			name: "explicit port is preserved",
			args: []string{"-resolver_address", "192.0.2.53:5353"},
			want: "192.0.2.53:5353",
		},
		{
			name: "unset address stays empty and is not turned into :53",
			args: nil,
			want: "",
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			// flag.Parse leaves unmentioned flags at their previous value, so the
			// global must be cleared explicitly between subtests
			*resolverAddress = ""

			os.Args = append(append([]string{"axfr2hosts"}, tt.args...), "example.com")

			zones, _, _ := parseFlags()
			if len(zones) != 1 {
				t.Fatalf("parseFlags() zones = %v, want exactly one", zones)
			}

			if *resolverAddress != tt.want {
				t.Errorf("after parseFlags(), resolver address = %q, want %q",
					*resolverAddress, tt.want)
			}

			if *resolverAddress == "" {
				return
			}

			if _, _, err := splitHostPortCheck(*resolverAddress); err != nil {
				t.Errorf("resolver address %q is not a valid dial target: %v",
					*resolverAddress, err)
			}
		})
	}
}

// TestBehaviouralFlagDefaults pins the flag defaults that change what ends up in
// the generated hosts file.  Flipping either boolean silently alters output for
// every user who does not pass the flag, and nothing else in the suite asserts the
// declared default.
//
// Only the semantic booleans are pinned.  Tuning knobs (-max_transfers,
// -max_retries, -resolver_timeout) are deliberately excluded: their values are
// incidental, and asserting them would turn ordinary tuning into a test failure.
func TestBehaviouralFlagDefaults(t *testing.T) {
	for _, tt := range []struct {
		name string
		flag string
		want string
	}{
		{"out-of-zone CNAME targets are resolved by default", "greedy_cname", "true"},
		{"wildcard records are dropped by default", "ignore_star", "true"},
		{"domain stripping is off by default", "strip_domain", "false"},
		{"keeping both forms is off by default", "strip_unstrip", "false"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			f := flag.Lookup(tt.flag)
			if f == nil {
				t.Fatalf("flag -%s is not registered", tt.flag)
			}

			if f.DefValue != tt.want {
				t.Errorf("-%s default = %q, want %q", tt.flag, f.DefValue, tt.want)
			}
		})
	}
}
