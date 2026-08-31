// SPDX-FileCopyrightText: 2026 Dinko Korunic
// SPDX-License-Identifier: MIT

package main

import (
	"context"
	"errors"
	"net"
	"net/netip"
	"sync/atomic"
	"testing"
	"time"

	"github.com/miekg/dns"
)

// TestLookupSingleflightKeyIncludesType is the behavioural oracle for bug #15:
// the singleflight key must distinguish query types for the same hostname.
//
// This replaces a version that defined local makeCorrectKey/makeBuggyKey helpers
// and asserted on those.  It never called lookup(), so mutating the production key
// builder changed nothing it observed — it tested a copy of the code.
//
// The invariant matters because processRecords looks up the SAME name twice, first
// as CNAME then as A (zone.go, non-greedy CNAME handling).  If the key omits the
// type, the second call joins the first still-in-flight call and receives a
// hostname where an address is expected.
//
// Note on the key encoding: the production scheme is decimal(type) ++ hostname.
// The suffix form (hostname ++ decimal(type)) cannot actually produce a collision
// across the three types lookupFunc supports — A(1), CNAME(5), AAAA(28) — because
// their decimal forms differ in the final digit.  The prefix encoding is still the
// right choice, since adding a type such as MX(15) would make "a"+"15" collide
// with "a1"+"5"; that is a guarantee about future types, not a currently reachable
// bug, so this test targets the reachable defect: dropping the type entirely.
func TestLookupSingleflightKeyIncludesType(t *testing.T) {
	const host = "alias.example.com."

	// A deliberately slow resolver makes the two calls overlap, which is the only
	// window in which singleflight can collapse them.  The gate is a fixed delay
	// rather than a channel handshake because net.Resolver.LookupCNAME issues AAAA
	// and A probes before its CNAME query, so any handshake keyed on seeing a
	// particular query type races with the resolver's internal query sequence.
	const serverDelay = 300 * time.Millisecond

	pc, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("net.ListenPacket: %v", err)
	}

	handler := dns.HandlerFunc(func(w dns.ResponseWriter, req *dns.Msg) {
		if len(req.Question) == 0 {
			return
		}

		time.Sleep(serverDelay)

		q := req.Question[0]

		m := new(dns.Msg)
		m.SetReply(req)
		m.Authoritative = true

		var rr dns.RR

		switch q.Qtype {
		case dns.TypeCNAME:
			rr, _ = dns.NewRR(host + " 3600 IN CNAME www.example.com.")
		case dns.TypeA:
			rr, _ = dns.NewRR(host + " 3600 IN A 192.0.2.10")
		}

		if rr != nil {
			m.Answer = []dns.RR{rr}
		}

		_ = w.WriteMsg(m)
	})

	srv := &dns.Server{PacketConn: pc, Net: "udp", Handler: handler}

	go func() {
		_ = srv.ActivateAndServe()
	}()

	t.Cleanup(func() { _ = srv.Shutdown() })

	savedTimeout := *resolverTimeout

	t.Cleanup(func() { *resolverTimeout = savedTimeout })

	*resolverTimeout = 15 * time.Second

	addr := pc.LocalAddr().String()
	r := &net.Resolver{
		PreferGo: true,
		Dial: func(ctx context.Context, network, _ string) (net.Conn, error) {
			return (&net.Dialer{}).DialContext(ctx, network, addr)
		},
	}

	type result struct {
		vals []string
		err  error
	}

	cnameCh := make(chan result, 1)
	aCh := make(chan result, 1)

	go func() {
		v, e := lookup(context.Background(), host, dns.TypeCNAME, r)
		cnameCh <- result{v, e}
	}()

	go func() {
		v, e := lookup(context.Background(), host, dns.TypeA, r)
		aCh <- result{v, e}
	}()

	cn := <-cnameCh
	av := <-aCh

	if cn.err != nil {
		t.Fatalf("CNAME lookup: %v", cn.err)
	}

	if av.err != nil {
		t.Fatalf("A lookup: %v", av.err)
	}

	if len(av.vals) == 0 || len(cn.vals) == 0 {
		t.Fatalf("lookups returned no values: A=%v CNAME=%v", av.vals, cn.vals)
	}

	// Both assertions are needed.  With a type-blind key the two overlapping calls
	// collapse into one and both callers receive the same value; which of them wins
	// the race is arbitrary, so exactly one of these two checks catches it either
	// way.  Asserting only on the A side would pass whenever the A call happened to
	// register first.
	if _, err := netip.ParseAddr(av.vals[0]); err != nil {
		t.Errorf("A lookup returned %q, which is not an IP address: the concurrent "+
			"CNAME answer leaked through a shared singleflight key, so the key does "+
			"not distinguish query types", av.vals[0])
	}

	if _, err := netip.ParseAddr(cn.vals[0]); err == nil {
		t.Errorf("CNAME lookup returned the IP address %q: the concurrent A answer "+
			"leaked through a shared singleflight key, so the key does not "+
			"distinguish query types", cn.vals[0])
	}
}

// TestLookupForgetOnDeadlineExceeded is the test oracle for bug #16.
// When the outer context deadline fires before the DNS response arrives,
// lookup() must call lookupGroup.Forget(key) so that the next caller for the
// same key starts a fresh DNS query rather than joining the still-running
// (timed-out) in-flight call.  The bug mutation calls Forget only on
// context.Canceled, leaving the key in the group after a deadline timeout.
//
// We verify the Forget by using two different resolvers: the first call uses
// resolver r1 (no dial counter); if Forget was called, the second DoChan for
// the same key starts a new goroutine using resolver r2 (with a dial counter).
// If Forget was NOT called, DoChan ignores r2's function and joins the old
// r1-based goroutine — the r2 Dial counter stays zero.
func TestLookupForgetOnDeadlineExceeded(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping timing-sensitive singleflight test in short mode")
	}

	// black-hole TCP server: accepts but never responds
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("net.Listen: %v", err)
	}
	defer ln.Close()

	go func() {
		for {
			_, err := ln.Accept()
			if err != nil {
				return
			}
		}
	}()

	srvAddr := ln.Addr().String()

	savedTimeout := *resolverTimeout

	defer func() {
		*resolverTimeout = savedTimeout
	}()

	// lookupGroup is deliberately NOT reassigned.  This test leaves a singleflight
	// goroutine in flight on purpose (the inner timeout outlives the outer
	// deadline), and assigning a fresh Group over the global races with that
	// goroutine's bookkeeping — `go test -race` reports it as a data race.
	// Isolation comes from the unique hostname below instead.

	// inner timeout > outer deadline keeps the goroutine in-flight
	*resolverTimeout = 500 * time.Millisecond

	hostname := "deadline-forget-test.example.invalid."

	// outer deadline fires first → DeadlineExceeded
	r1 := &net.Resolver{
		PreferGo: true,
		Dial: func(ctx context.Context, network, _ string) (net.Conn, error) {
			return (&net.Dialer{}).DialContext(ctx, "tcp", srvAddr)
		},
	}

	ctx1, cancel1 := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel1()

	// This must be fatal, never a skip.  The server above is a black hole, so the
	// outer deadline always fires and DeadlineExceeded is deterministic on correct
	// code.  A t.Skipf here silently disables the whole test whenever lookup stops
	// reporting the deadline — which is exactly what the bug this test guards does,
	// so the skip made the test unable to fail on its own subject.
	_, err1 := lookup(ctx1, hostname, dns.TypeA, r1)
	if !errors.Is(err1, context.DeadlineExceeded) {
		t.Fatalf("first lookup: err = %v, want context.DeadlineExceeded — lookup must "+
			"surface the outer deadline (and Forget the singleflight key) rather than "+
			"returning a non-deadline error", err1)
	}

	// r2.Dial fires only if Forget freed the key; otherwise DoChan reuses call 1
	var r2DialCalled atomic.Bool

	r2 := &net.Resolver{
		PreferGo: true,
		Dial: func(ctx context.Context, network, _ string) (net.Conn, error) {
			r2DialCalled.Store(true)
			return (&net.Dialer{}).DialContext(ctx, "tcp", srvAddr)
		},
	}

	ctx2, cancel2 := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel2()

	_, _ = lookup(ctx2, hostname, dns.TypeA, r2)

	if !r2DialCalled.Load() {
		t.Error("after DeadlineExceeded, subsequent lookup did not start a new DNS query — " +
			"Forget may not have been called on the singleflight key")
	}
}

func TestLookupFuncTypes(t *testing.T) {
	ctx := context.Background()
	r := &net.Resolver{PreferGo: true}

	tests := []struct {
		name    string
		t       uint16
		wantNil bool
	}{
		{"TypeCNAME", dns.TypeCNAME, false},
		{"TypeA", dns.TypeA, false},
		{"TypeAAAA", dns.TypeAAAA, false},
		{"TypeMX (unsupported)", dns.TypeMX, true},
		{"TypeNS (unsupported)", dns.TypeNS, true},
		{"TypeSOA (unsupported)", dns.TypeSOA, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fn := lookupFunc(ctx, "host.example.com.", tt.t, r)
			if tt.wantNil && fn != nil {
				t.Errorf("lookupFunc(%d) = non-nil, want nil", tt.t)
			}
			if !tt.wantNil && fn == nil {
				t.Errorf("lookupFunc(%d) = nil, want non-nil closure", tt.t)
			}
		})
	}
}

// TestLookupUnsupportedType covers the guard in front of singleflight.DoChan.
// lookupFunc returns nil for any qtype it cannot build a closure for; handing that
// nil to DoChan panics, and singleflight re-panics it in every waiting caller, so
// a single stray qtype would crash the process instead of failing one record.
func TestLookupUnsupportedType(t *testing.T) {
	r := &net.Resolver{PreferGo: true}

	for _, qt := range []uint16{dns.TypeMX, dns.TypeNS, dns.TypeSOA, dns.TypeTXT} {
		t.Run(dns.Type(qt).String(), func(t *testing.T) {
			got, err := lookup(t.Context(), "host.example.com.", qt, r)

			if !errors.Is(err, ErrUnsupportedType) {
				t.Errorf("lookup(%v) error = %v, want ErrUnsupportedType", dns.Type(qt), err)
			}

			if got != nil {
				t.Errorf("lookup(%v) = %v, want nil", dns.Type(qt), got)
			}
		})
	}
}

// TestLookupCanceledContext covers the ctx.Done() branch for a plain cancellation
// (as opposed to a deadline).  Cancellation must not Forget the singleflight key,
// because the in-flight call may still be serving other waiters; lookup returns an
// empty result and leaves the group untouched.
func TestLookupCanceledContext(t *testing.T) {
	// lookupGroup is intentionally left alone; see useTestResolver for why
	// reassigning it races with singleflight's bookkeeping goroutine.

	// black-hole server so the lookup never completes on its own
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("net.Listen: %v", err)
	}

	t.Cleanup(func() { _ = ln.Close() })

	go func() {
		for {
			if _, err := ln.Accept(); err != nil {
				return
			}
		}
	}()

	r := &net.Resolver{
		PreferGo: true,
		Dial: func(ctx context.Context, _, _ string) (net.Conn, error) {
			return (&net.Dialer{}).DialContext(ctx, "tcp", ln.Addr().String())
		},
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // already canceled before the call

	got, err := lookup(ctx, "canceled.example.invalid.", dns.TypeA, r)

	if errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("lookup() on a canceled context returned DeadlineExceeded, want Canceled or empty result")
	}

	if got == nil && err == nil {
		t.Error("lookup() returned nil result and nil error, want an empty slice")
	}
}

// TestLookupPropagatesResolverError checks that a failed lookup is reported as a
// failure.  Returning (nil, nil) instead would make a resolver outage
// indistinguishable from a name that legitimately has no addresses, so
// processRecords would silently drop records instead of skipping a broken lookup.
func TestLookupPropagatesResolverError(t *testing.T) {
	savedTimeout := *resolverTimeout

	t.Cleanup(func() { *resolverTimeout = savedTimeout })

	*resolverTimeout = 2 * time.Second

	dialErr := errors.New("dial refused by test resolver")
	r := &net.Resolver{
		PreferGo: true,
		Dial: func(_ context.Context, _, _ string) (net.Conn, error) {
			return nil, dialErr
		},
	}

	for _, tt := range []struct {
		name  string
		qtype uint16
	}{
		{"A lookup", dns.TypeA},
		{"CNAME lookup", dns.TypeCNAME},
	} {
		t.Run(tt.name, func(t *testing.T) {
			vals, err := lookup(context.Background(), "propagate-error.example.invalid.", tt.qtype, r)
			if err == nil {
				t.Errorf("lookup() = (%v, nil) with an unreachable resolver, want a non-nil error: "+
					"callers cannot tell a resolver failure from an empty answer", vals)
			}
		})
	}
}

// TestLookupHonoursResolverTimeout checks that -resolver_timeout is the deadline
// actually applied to the DNS query, rather than a hard-coded constant.  The
// server never answers, so the only thing that can end the call is the timeout.
func TestLookupHonoursResolverTimeout(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping timing-sensitive resolver timeout test in short mode")
	}

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("net.Listen: %v", err)
	}

	t.Cleanup(func() { _ = ln.Close() })

	// black hole: accept and never respond
	go func() {
		for {
			if _, err := ln.Accept(); err != nil {
				return
			}
		}
	}()

	savedTimeout := *resolverTimeout

	t.Cleanup(func() { *resolverTimeout = savedTimeout })

	// Must stay well below any plausible hard-coded fallback (the defect this
	// guards against pinned the deadline at 1s), or the assertion below cannot
	// tell the configured timeout from the constant.
	const short = 200 * time.Millisecond

	*resolverTimeout = short

	addr := ln.Addr().String()
	r := &net.Resolver{
		PreferGo: true,
		Dial: func(ctx context.Context, _, _ string) (net.Conn, error) {
			return (&net.Dialer{}).DialContext(ctx, "tcp", addr)
		},
	}

	start := time.Now()
	// no deadline on the outer context: the resolver timeout is the only limit
	_, _ = lookup(context.Background(), "timeout-honoured.example.invalid.", dns.TypeA, r)
	elapsed := time.Since(start)

	// 3.5x headroom over `short` (700ms), still comfortably under a 1s constant:
	// discriminating without being timing-fragile on a loaded machine
	if elapsed > 700*time.Millisecond {
		t.Errorf("lookup took %v with -resolver_timeout=%v, want it to return near the "+
			"configured timeout: the flag is not being applied to the query", elapsed, short)
	}
}

// NOTE on the dropped `defer cancel()` in lookupFunc: there is deliberately no
// unit test for it.  The context handed to net.Resolver's Dial is cancelled by the
// resolver itself once the query completes, so it cannot distinguish a leaked
// parent from a cancelled one, and the leak produces no observable wrong answer —
// only a context and timer held until the resolver timeout.  `go vet`'s lostcancel
// does not flag `_ = cancel` either.  The project's golangci-lint config does catch
// it (gosec G118, "context cancellation function ... is not called"), so the guard
// lives in `task lint` rather than here.  A test that cannot fail on its subject
// would be worse than none.
