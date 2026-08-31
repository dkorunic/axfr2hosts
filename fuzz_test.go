// SPDX-FileCopyrightText: 2026 Dinko Korunic
// SPDX-License-Identifier: MIT

package main

import (
	"net"
	"net/netip"
	"regexp"
	"strings"
	"testing"
)

// FuzzNormalizeAddrPort checks the contract on the inputs the contract covers.
//
// The function normalises, it does not validate: no transformation turns "]" into
// a dialable address, so "any bytes become dialable" is not a property it has.
// What must hold is that a *recognisable* address — a bare IP, a bracketed IPv6
// literal, a plain hostname, or an already-complete host:port — always comes out
// dialable, and that an already-complete one comes out untouched.
func FuzzNormalizeAddrPort(f *testing.F) {
	for _, seed := range []string{
		"192.0.2.1", "192.0.2.1:53", "2001:db8::1", "[2001:db8::1]:53",
		"::1", "[::1]", "ns1.example.com", "1:2:3:4:5:6:7:8", "]", "", ":",
	} {
		f.Add(seed)
	}

	plainHost := regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9.-]*$`)

	f.Fuzz(func(t *testing.T, addr string) {
		got := normalizeAddrPort(addr)

		// an address that already carries a port must survive verbatim
		if _, _, err := net.SplitHostPort(addr); err == nil {
			if got != addr {
				t.Errorf("normalizeAddrPort(%q) = %q, want it unchanged (it already has a port)",
					addr, got)
			}

			return
		}

		// otherwise only recognisable hosts are in scope
		_, ipErr := netip.ParseAddr(addr)
		bracketed := strings.HasPrefix(addr, "[") && strings.HasSuffix(addr, "]")

		if bracketed {
			// strip exactly one bracket pair, as normalizeAddrPort does; Trim would
			// also accept unbalanced garbage like "[::]]"
			inner := addr[1 : len(addr)-1]
			if _, err := netip.ParseAddr(inner); err != nil || strings.ContainsAny(inner, "[]%") {
				return
			}
		} else if ipErr != nil || strings.ContainsAny(addr, "[]%") {
			// netip accepts IPv6 zone IDs ("::%eth0") including ones containing
			// bracket characters, which no real interface name can have
			if !plainHost.MatchString(addr) {
				return
			}
		}

		if _, _, err := net.SplitHostPort(got); err != nil {
			t.Errorf("normalizeAddrPort(%q) = %q, which net.SplitHostPort rejects: %v",
				addr, got, err)
		}
	})
}

// FuzzParseZoneArgs asserts the invariants callers rely on: zones are unique, no
// zone retains a trailing dot, an '@' argument never leaks into the zone list, and
// a server address is always dialable.
func FuzzParseZoneArgs(f *testing.F) {
	for _, seed := range []string{
		"example.com", "example.com.", "@192.0.2.1", "@2001:db8::1",
		"a=b", "", "@", ".", "..", "@:", "@[::1]:53",
	} {
		f.Add(seed, "example.org", "@192.0.2.2")
	}

	f.Fuzz(func(t *testing.T, a, b, c string) {
		args := []string{a, b, c}
		zones, server := parseZoneArgs(args)

		seen := make(map[string]struct{}, len(zones))

		for _, z := range zones {
			if _, dup := seen[z]; dup {
				t.Errorf("parseZoneArgs(%q) returned duplicate zone %q", args, z)
			}

			seen[z] = struct{}{}

			if strings.HasPrefix(z, dnsPrefix) {
				t.Errorf("parseZoneArgs(%q) leaked server argument %q into the zone list", args, z)
			}

			if strings.HasSuffix(z, endingDot) {
				t.Errorf("parseZoneArgs(%q) left a trailing dot on zone %q", args, z)
			}

			if z == "" {
				t.Errorf("parseZoneArgs(%q) produced an empty zone name", args)
			}
		}

		// the server invariant holds for recognisable addresses; parseZoneArgs
		// normalises rather than validates, so garbage stays garbage
		if server != "" {
			raw := strings.TrimPrefix(lastServerArg(args), dnsPrefix)
			if _, err := netip.ParseAddr(raw); err == nil && !strings.ContainsAny(raw, "[]%") {
				if _, _, err := net.SplitHostPort(server); err != nil {
					t.Errorf("parseZoneArgs(%q) turned IP %q into undialable %q: %v",
						args, raw, server, err)
				}
			}
		}
	})
}

// FuzzZoneParser feeds arbitrary bytes through the RFC 1035 parser.  The contract
// is that it never panics and never yields a nil RR, since processRecords type-
// switches on every element without a nil check.
func FuzzZoneParser(f *testing.F) {
	for _, seed := range []string{
		"",
		"$TTL 3600\n@ IN SOA ns1 admin 1 2 3 4 5\nhost IN A 192.0.2.1\n",
		"$ORIGIN example.com.\nhost IN A 192.0.2.1\n",
		"garbage !!! not a zone\n",
		"@ IN A\n",
		"$INCLUDE /etc/passwd\n",
		"host IN A 999.999.999.999\n",
	} {
		f.Add(seed)
	}

	f.Fuzz(func(t *testing.T, content string) {
		path := writeTempZone(t, content)

		// stderr is noisy for malformed input and irrelevant to the invariants
		captureStderr(func() {
			for _, rr := range zoneParser(path, "example.com.") {
				if rr == nil {
					t.Error("zoneParser() returned a nil RR; processRecords type-switches without a nil check")
				}
			}
		})
	})
}

// FuzzProcessHost checks the label pipeline never emits an empty hostname, which
// would produce a syntactically broken hosts-file line.
func FuzzProcessHost(f *testing.F) {
	f.Add("host.example.com.", "example.com")
	f.Add("example.com.", "example.com")
	f.Add(".", "")
	f.Add("", "example.com")
	f.Add("HOST.EXAMPLE.COM.", "EXAMPLE.COM")

	f.Fuzz(func(t *testing.T, label, zone string) {
		saved := *stripUnstrip
		*stripUnstrip = true

		defer func() { *stripUnstrip = saved }()

		hosts := make(chan HostEntry, 8)
		processHost(label, zone, netip.MustParseAddr("192.0.2.1"), hosts)
		close(hosts)

		for h := range hosts {
			if h.label == "" {
				t.Errorf("processHost(%q, %q) emitted an empty label, which would write a"+
					" hosts line with no hostname", label, zone)
			}
		}
	})
}

// lastServerArg returns the final '@'-prefixed argument, mirroring parseZoneArgs'
// last-one-wins rule.
func lastServerArg(args []string) string {
	var last string

	for _, a := range args {
		if strings.HasPrefix(a, dnsPrefix) {
			last = a
		}
	}

	return last
}
