// SPDX-FileCopyrightText: 2026 Dinko Korunic
// SPDX-License-Identifier: MIT

package main

import (
	"fmt"
	"net"
	"net/netip"
	"runtime"
	"sync"
	"testing"
	"time"

	"github.com/miekg/dns"
)

// manyRecords builds n distinct A records spread over a /24.
func manyRecords(n int) []dns.RR {
	rrs := make([]dns.RR, 0, n)

	for i := range n {
		rrs = append(rrs, &dns.A{
			Hdr: dns.RR_Header{
				Name:   fmt.Sprintf("host%d.example.com.", i),
				Rrtype: dns.TypeA,
				Class:  dns.ClassINET,
			},
			A: net.IP{192, 0, 2, byte(i % 254)},
		})
	}

	return rrs
}

// TestProcessRecordsLosesNothing is the central correctness property of the
// fan-out design: processRecords spawns a goroutine per RR and only returns after
// wg.Wait, so every record must reach the channel exactly once.  A dropped Add or
// an early return would silently truncate the hosts file.
func TestProcessRecordsLosesNothing(t *testing.T) {
	savedStar := *ignoreStar

	t.Cleanup(func() { *ignoreStar = savedStar })

	*ignoreStar = false

	const n = 5000

	rrs := manyRecords(n)

	// unbuffered-ish channel with a live consumer, mirroring main's monitor
	hosts := make(chan HostEntry, 64)

	var (
		wg     sync.WaitGroup
		got    int
		labels = make(map[string]struct{}, n)
	)

	wg.Go(func() {
		for h := range hosts {
			got++

			labels[h.label] = struct{}{}
		}
	})

	processRecords("example.com", false, nil, hosts, rrs)
	close(hosts)
	wg.Wait()

	if got != n {
		t.Errorf("processRecords() delivered %d entries, want %d", got, n)
	}

	if len(labels) != n {
		t.Errorf("processRecords() delivered %d distinct labels, want %d", len(labels), n)
	}
}

// TestProcessRecordsReturnsAfterAllSends guards the ordering contract main relies
// on: main closes hostChan once every worker has returned.  If processRecords
// returned before its goroutines finished sending, that close would race and panic
// with "send on closed channel".
func TestProcessRecordsReturnsAfterAllSends(t *testing.T) {
	savedStar := *ignoreStar

	t.Cleanup(func() { *ignoreStar = savedStar })

	*ignoreStar = false

	for range 20 {
		rrs := manyRecords(200)
		hosts := make(chan HostEntry, 4096)

		processRecords("example.com", false, nil, hosts, rrs)

		// safe only because processRecords waited for every sender
		close(hosts)

		n := 0
		for range hosts {
			n++
		}

		if n != len(rrs) {
			t.Fatalf("delivered %d entries, want %d", n, len(rrs))
		}
	}
}

// TestWriteHostEntriesMergesLabels covers the monitor's merge behaviour: the same
// address arriving under several names collapses to one key with all labels, which
// is what produces multi-name hosts lines.
func TestWriteHostEntriesMergesLabels(t *testing.T) {
	ip := netip.MustParseAddr("192.0.2.1")
	other := netip.MustParseAddr("192.0.2.2")

	hosts := make(chan HostEntry, 16)
	for _, l := range []string{"a", "b", "a", "c"} {
		hosts <- HostEntry{label: l, ipAddr: ip}
	}

	hosts <- HostEntry{label: "d", ipAddr: other}
	close(hosts)

	var keys []netip.Addr

	entries := make(HostMap)
	writeHostEntries(hosts, &keys, entries)

	if len(keys) != 2 {
		t.Errorf("keys = %v, want 2 unique addresses", keys)
	}

	if got := len(entries[ip]); got != 3 {
		t.Errorf("entries[%v] has %d labels, want 3 (duplicates must collapse)", ip, got)
	}

	// a repeated address must not be appended to keys twice, or it would print twice
	seen := make(map[netip.Addr]int, len(keys))
	for _, k := range keys {
		seen[k]++
		if seen[k] > 1 {
			t.Errorf("address %v appended to keys %d times", k, seen[k])
		}
	}
}

// TestNoGoroutineLeak checks the fan-out retires cleanly.  processRecords is
// called repeatedly and the goroutine count must settle back near the baseline;
// a leaked worker per record would grow without bound over a long run.
func TestNoGoroutineLeak(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping goroutine settling check in short mode")
	}

	savedStar := *ignoreStar

	t.Cleanup(func() { *ignoreStar = savedStar })

	*ignoreStar = false

	settle := func() int {
		// give retiring goroutines a chance to be reaped before sampling
		for range 20 {
			runtime.Gosched()
			time.Sleep(5 * time.Millisecond)
		}

		return runtime.NumGoroutine()
	}

	base := settle()

	for range 10 {
		hosts := make(chan HostEntry, 8192)
		processRecords("example.com", false, nil, hosts, manyRecords(1000))
		close(hosts)

		for range hosts {
		}
	}

	after := settle()

	// a small drift is normal (test server goroutines, GC workers); a leak of one
	// goroutine per record would be 10000
	if after > base+20 {
		t.Errorf("goroutine count grew from %d to %d across 10 runs of 1000 records — likely a leak",
			base, after)
	}
}
