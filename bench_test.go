// SPDX-FileCopyrightText: 2026 Dinko Korunic
// SPDX-License-Identifier: MIT

package main

import (
	"fmt"
	"net/netip"
	"strings"
	"testing"
)

// benchHostMap builds a HostMap with addrs addresses carrying labels names each.
func benchHostMap(addrs, labels int) ([]netip.Addr, HostMap) {
	keys := make([]netip.Addr, 0, addrs)
	entries := make(HostMap, addrs)

	for i := range addrs {
		ip := netip.AddrFrom4([4]byte{192, 0, byte(i / 254), byte(i%254 + 1)})
		keys = append(keys, ip)

		m := make(map[string]struct{}, labels)
		for j := range labels {
			m[fmt.Sprintf("host%d-%d.example.com", i, j)] = struct{}{}
		}

		entries[ip] = m
	}

	return keys, entries
}

// BenchmarkProcessRecords measures the per-RR fan-out, which dominates a large
// zone transfer.  Only A/AAAA are used: CNAME would measure the DNS resolver.
func BenchmarkProcessRecords(b *testing.B) {
	saved := *ignoreStar
	*ignoreStar = false

	b.Cleanup(func() { *ignoreStar = saved })

	for _, n := range []int{100, 1000, 10000} {
		b.Run(fmt.Sprintf("records=%d", n), func(b *testing.B) {
			rrs := manyRecords(n)

			b.ReportAllocs()
			b.ResetTimer()

			for b.Loop() {
				hosts := make(chan HostEntry, n+1)
				processRecords("example.com", false, nil, hosts, rrs)
				close(hosts)

				for range hosts {
				}
			}
		})
	}
}

// BenchmarkProcessHost isolates the label pipeline across the strip modes, which
// runs once per emitted record.
func BenchmarkProcessHost(b *testing.B) {
	savedStrip, savedUnstrip := *stripDomain, *stripUnstrip

	b.Cleanup(func() {
		*stripDomain, *stripUnstrip = savedStrip, savedUnstrip
	})

	ip := netip.MustParseAddr("192.0.2.1")

	modes := []struct {
		name    string
		strip   bool
		unstrip bool
	}{
		{"plain", false, false},
		{"strip_domain", true, false},
		{"strip_unstrip", false, true},
	}

	for _, m := range modes {
		b.Run(m.name, func(b *testing.B) {
			*stripDomain, *stripUnstrip = m.strip, m.unstrip

			hosts := make(chan HostEntry, 4)

			b.ReportAllocs()
			b.ResetTimer()

			for b.Loop() {
				processHost("Host1.Example.COM.", "example.com", ip, hosts)

				for len(hosts) > 0 {
					<-hosts
				}
			}
		})
	}
}

// BenchmarkWriteHostEntries measures the monitor goroutine's merge loop, the one
// place every record funnels through.
func BenchmarkWriteHostEntries(b *testing.B) {
	const n = 10000

	b.ReportAllocs()

	for b.Loop() {
		b.StopTimer()

		hosts := make(chan HostEntry, n)
		for i := range n {
			hosts <- HostEntry{
				label:  fmt.Sprintf("host%d.example.com", i),
				ipAddr: netip.AddrFrom4([4]byte{192, 0, byte(i / 254), byte(i%254 + 1)}),
			}
		}

		close(hosts)

		var keys []netip.Addr

		entries := make(HostMap, n)

		b.StartTimer()
		writeHostEntries(hosts, &keys, entries)
	}
}

// BenchmarkDisplayHostEntries measures the final sort-and-render pass, including
// the per-address label sort.  Redirecting stdout keeps the benchmark off the
// terminal without changing what is measured.
func BenchmarkDisplayHostEntries(b *testing.B) {
	for _, shape := range []struct {
		name          string
		addrs, labels int
	}{
		{"1000x1", 1000, 1},
		{"1000x8", 1000, 8},
		{"100x64", 100, 64},
	} {
		b.Run(shape.name, func(b *testing.B) {
			keys, entries := benchHostMap(shape.addrs, shape.labels)

			b.ReportAllocs()
			b.ResetTimer()

			for b.Loop() {
				captureStdout(func() {
					displayHostEntries(keys, entries)
				})
			}
		})
	}
}

// BenchmarkZoneParser measures RFC 1035 parsing, the cost paid per local zone file.
func BenchmarkZoneParser(b *testing.B) {
	for _, n := range []int{100, 5000} {
		b.Run(fmt.Sprintf("records=%d", n), func(b *testing.B) {
			var sb strings.Builder

			sb.WriteString("$TTL 3600\n@ IN SOA ns1 admin 1 2 3 4 5\n")

			for i := range n {
				fmt.Fprintf(&sb, "host%d IN A 192.0.2.%d\n", i, i%254+1)
			}

			path := benchTempZone(b, sb.String())

			b.ReportAllocs()
			b.ResetTimer()

			for b.Loop() {
				if got := zoneParser(path, "example.com."); len(got) == 0 {
					b.Fatal("parser returned no records")
				}
			}
		})
	}
}

// BenchmarkNormalizeAddrPort covers the address normaliser across families.
func BenchmarkNormalizeAddrPort(b *testing.B) {
	for _, addr := range []string{"192.0.2.1", "192.0.2.1:53", "2001:db8::1", "[2001:db8::1]:53"} {
		b.Run(addr, func(b *testing.B) {
			b.ReportAllocs()

			for b.Loop() {
				_ = normalizeAddrPort(addr)
			}
		})
	}
}
