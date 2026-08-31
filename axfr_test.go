// SPDX-FileCopyrightText: 2026 Dinko Korunic
// SPDX-License-Identifier: MIT

package main

import (
	"net"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/miekg/dns"
)

// axfrTestZone is the zone every AXFR test server answers for.
const axfrTestZone = "example.com."

// axfrRecords builds a well-formed AXFR record sequence.  RFC 5936 requires the
// stream to open and close with the same SOA, and miekg/dns enforces this on the
// client side: a stream not starting with SOA is rejected outright, and the second
// SOA is what terminates the transfer.
func axfrRecords(t *testing.T) []dns.RR {
	t.Helper()

	soa, err := dns.NewRR(axfrTestZone + " 3600 IN SOA ns1.example.com. hostmaster.example.com. 1 7200 3600 1209600 3600")
	if err != nil {
		t.Fatalf("building SOA: %v", err)
	}

	rrs := []dns.RR{soa}

	for _, s := range []string{
		"www.example.com. 3600 IN A 192.0.2.1",
		"mail.example.com. 3600 IN A 192.0.2.2",
		"ipv6.example.com. 3600 IN AAAA 2001:db8::1",
		"alias.example.com. 3600 IN CNAME www.example.com.",
	} {
		rr, err := dns.NewRR(s)
		if err != nil {
			t.Fatalf("building RR %q: %v", s, err)
		}

		rrs = append(rrs, rr)
	}

	return append(rrs, soa)
}

// startAXFRServer runs an in-process TCP DNS server that serves the given records
// as a single AXFR envelope, and returns its "host:port" address.  A per-server
// ServeMux is used rather than dns.HandleFunc so tests never share global state.
func startAXFRServer(t *testing.T, envelopes [][]dns.RR) string {
	t.Helper()

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("net.Listen: %v", err)
	}

	mux := dns.NewServeMux()
	mux.HandleFunc(axfrTestZone, func(w dns.ResponseWriter, req *dns.Msg) {
		tr := new(dns.Transfer)
		ch := make(chan *dns.Envelope)

		var wg sync.WaitGroup

		wg.Add(1)

		go func() {
			defer wg.Done()

			_ = tr.Out(w, req, ch)
		}()

		for _, e := range envelopes {
			ch <- &dns.Envelope{RR: e}
		}

		close(ch)
		wg.Wait()
		_ = w.Close()
	})

	srv := &dns.Server{Listener: ln, Net: "tcp", Handler: mux}

	go func() {
		_ = srv.ActivateAndServe()
	}()

	t.Cleanup(func() {
		_ = srv.Shutdown()
	})

	return ln.Addr().String()
}

// withMaxRetries pins the global retry count for the duration of a test.  Failure
// tests use 1 so they do not pay retry-go's backoff delay.
func withMaxRetries(t *testing.T, n uint) {
	t.Helper()

	saved := *maxRetries

	t.Cleanup(func() { *maxRetries = saved })

	*maxRetries = n
}

func TestZoneTransferSuccess(t *testing.T) {
	withMaxRetries(t, 1)

	want := axfrRecords(t)
	addr := startAXFRServer(t, [][]dns.RR{want})

	got := zoneTransfer(axfrTestZone, addr)

	if len(got) != len(want) {
		t.Fatalf("zoneTransfer() returned %d records, want %d", len(got), len(want))
	}

	for i := range want {
		if got[i].String() != want[i].String() {
			t.Errorf("record[%d] = %q, want %q", i, got[i].String(), want[i].String())
		}
	}
}

// TestZoneTransferFqdnNormalization checks that a zone given without the trailing
// dot still matches the server's canonical zone name.  zoneTransfer applies
// dns.Fqdn before building the AXFR question; without it the query name would be
// "example.com" and the server would answer REFUSED.
func TestZoneTransferFqdnNormalization(t *testing.T) {
	withMaxRetries(t, 1)

	want := axfrRecords(t)
	addr := startAXFRServer(t, [][]dns.RR{want})

	got := zoneTransfer(strings.TrimSuffix(axfrTestZone, "."), addr)

	if len(got) != len(want) {
		t.Fatalf("zoneTransfer(%q) returned %d records, want %d — zone name was not FQDN-normalised",
			strings.TrimSuffix(axfrTestZone, "."), len(got), len(want))
	}
}

// TestZoneTransferMultipleEnvelopes verifies records are accumulated across
// envelopes.  Real servers chunk large zones into many messages, so a transfer
// that only kept the last envelope would silently truncate big zones.
func TestZoneTransferMultipleEnvelopes(t *testing.T) {
	withMaxRetries(t, 1)

	all := axfrRecords(t)
	// split: [SOA, A] then [A, AAAA, CNAME, SOA]
	addr := startAXFRServer(t, [][]dns.RR{all[:2], all[2:]})

	got := zoneTransfer(axfrTestZone, addr)

	if len(got) != len(all) {
		t.Fatalf("zoneTransfer() across 2 envelopes returned %d records, want %d", len(got), len(all))
	}
}

// TestZoneTransferUnreachableServer covers the dial-failure path: zoneTransfer
// must return an empty (non-nil) slice and report the failure on stderr rather
// than aborting the whole run, so one dead nameserver cannot kill every zone.
func TestZoneTransferUnreachableServer(t *testing.T) {
	withMaxRetries(t, 1)

	// bind then immediately release to get a port nothing is listening on
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("net.Listen: %v", err)
	}

	addr := ln.Addr().String()
	_ = ln.Close()

	var got []dns.RR

	stderr := captureStderr(func() {
		got = zoneTransfer(axfrTestZone, addr)
	})

	if got == nil {
		t.Error("zoneTransfer() returned nil slice on failure, want empty non-nil slice")
	}

	if len(got) != 0 {
		t.Errorf("zoneTransfer() returned %d records from an unreachable server, want 0", len(got))
	}

	if !strings.Contains(stderr, "AXFR failure") {
		t.Errorf("zoneTransfer() did not report the failure on stderr; stderr=%q", stderr)
	}
}

// TestZoneTransferRefused covers a server that answers but refuses the transfer.
// miekg/dns surfaces this as an envelope error, which zoneTransfer must log and
// skip rather than treating the refusal as an empty-but-valid zone.
func TestZoneTransferRefused(t *testing.T) {
	withMaxRetries(t, 1)

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("net.Listen: %v", err)
	}

	t.Cleanup(func() { _ = ln.Close() })

	// accept the connection, then hang up without a valid AXFR stream
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}

			_ = conn.Close()
		}
	}()

	var got []dns.RR

	stderr := captureStderr(func() {
		got = zoneTransfer(axfrTestZone, ln.Addr().String())
	})

	if len(got) != 0 {
		t.Errorf("zoneTransfer() returned %d records from a refusing server, want 0", len(got))
	}

	if !strings.Contains(stderr, "AXFR") {
		t.Errorf("zoneTransfer() did not report the refusal on stderr; stderr=%q", stderr)
	}
}

// TestZoneTransferTimeouts documents the transfer deadlines.  They are package
// constants rather than flags, so a change here is a deliberate decision: values
// of zero would mean "no deadline" and let a wedged server hang the run forever.
func TestZoneTransferTimeouts(t *testing.T) {
	for _, tt := range []struct {
		name string
		got  time.Duration
	}{
		{"dialTimeout", dialTimeout},
		{"readTimeout", readTimeout},
		{"writeTimeout", writeTimeout},
	} {
		t.Run(tt.name, func(t *testing.T) {
			if tt.got <= 0 {
				t.Errorf("%s = %v, want a positive deadline (zero disables the timeout)", tt.name, tt.got)
			}
		})
	}
}

// startCountingAXFRServer runs an in-process TCP DNS server that answers an AXFR
// for any zone it is asked about, holding each transfer open for hold so that
// concurrent transfers actually overlap.  The returned func reports the highest
// number of transfers that were ever in flight simultaneously, which is what makes
// -max_transfers observable: the previous bounds test asserted only on the exit
// code and so could not tell a working semaphore from a missing one.
func startCountingAXFRServer(t *testing.T, hold time.Duration) (string, func() int) {
	t.Helper()

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("net.Listen: %v", err)
	}

	var (
		mu       sync.Mutex
		cur, max int
	)

	handler := dns.HandlerFunc(func(w dns.ResponseWriter, req *dns.Msg) {
		if len(req.Question) == 0 {
			return
		}

		mu.Lock()
		cur++

		if cur > max {
			max = cur
		}
		mu.Unlock()

		defer func() {
			mu.Lock()
			cur--
			mu.Unlock()
		}()

		time.Sleep(hold)

		zone := req.Question[0].Name

		soa, err := dns.NewRR(zone + " 3600 IN SOA ns1." + zone + " hostmaster." + zone +
			" 1 7200 3600 1209600 3600")
		if err != nil {
			return
		}

		a, err := dns.NewRR("host." + zone + " 3600 IN A 192.0.2.1")
		if err != nil {
			return
		}

		tr := new(dns.Transfer)
		ch := make(chan *dns.Envelope)

		var wg sync.WaitGroup

		wg.Add(1)

		go func() {
			defer wg.Done()

			_ = tr.Out(w, req, ch)
		}()

		ch <- &dns.Envelope{RR: []dns.RR{soa, a, soa}}

		close(ch)
		wg.Wait()
		_ = w.Close()
	})

	srv := &dns.Server{Listener: ln, Net: "tcp", Handler: handler}

	go func() {
		_ = srv.ActivateAndServe()
	}()

	t.Cleanup(func() {
		_ = srv.Shutdown()
	})

	return ln.Addr().String(), func() int {
		mu.Lock()
		defer mu.Unlock()

		return max
	}
}

// TestZoneTransferHonoursMaxRetries checks that -max_retries controls how many
// times a failed transfer is attempted.  Nothing else in the suite observed the
// attempt count, so pinning it to a constant went unnoticed.
//
// Attempts are measured by elapsed time rather than by counting connections:
// retry-go only retries when tr.In itself fails, which happens on dial failure, and
// a dead port has nothing to count connections with.  retry-go's exponential
// backoff makes the signal large — a single attempt returns in about a
// millisecond, three attempts take a few hundred — so the thresholds below sit
// orders of magnitude apart rather than relying on fine timing.
func TestZoneTransferHonoursMaxRetries(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping retry-backoff timing test in short mode")
	}

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("net.Listen: %v", err)
	}

	addr := ln.Addr().String()
	// close immediately: a dead port makes the dial fail, which is the only
	// failure mode retry-go actually retries
	_ = ln.Close()

	elapsedFor := func(t *testing.T, n uint) time.Duration {
		t.Helper()

		withMaxRetries(t, n)

		start := time.Now()

		_ = captureStderr(func() { zoneTransfer(axfrTestZone, addr) })

		return time.Since(start)
	}

	// anti-vacuity: a single attempt must be effectively instant, proving the
	// measurement below reflects retries and not fixed overhead
	if single := elapsedFor(t, 1); single > 100*time.Millisecond {
		t.Fatalf("one attempt took %v, want it to be near-instant: the timing signal "+
			"is dominated by something other than retry backoff", single)
	}

	if many := elapsedFor(t, 3); many < 250*time.Millisecond {
		t.Errorf("three attempts took %v, want at least the retry backoff (~475ms): "+
			"-max_retries is not controlling the attempt count", many)
	}
}

// TestZoneTransferAppliesReadTimeout checks that readTimeout is actually assigned
// to the dns.Transfer, not merely declared.
//
// TestZoneTransferTimeouts asserts the constants are positive, which says nothing
// about whether they reach the Transfer.  Leaving ReadTimeout at zero does not hang
// as one might expect: miekg/dns substitutes its own 2s default (xfr.go), so the
// deadline silently tightens from 30s to 2s and a merely slow server starts
// failing.  The server here answers after 3s: fine under the real timeout, too slow
// under the library default.
func TestZoneTransferAppliesReadTimeout(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping slow-server read-deadline test in short mode")
	}

	withMaxRetries(t, 1)

	addr, _ := startCountingAXFRServer(t, 3*time.Second)

	got := zoneTransfer(axfrTestZone, addr)

	if len(got) == 0 {
		t.Error("zoneTransfer() returned no records from a server that answers after 3s: " +
			"the configured read timeout was not applied, leaving the library's " +
			"shorter default in force")
	}
}
