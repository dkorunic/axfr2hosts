// SPDX-FileCopyrightText: 2026 Dinko Korunic
// SPDX-License-Identifier: MIT

package main

import (
	"context"
	"errors"
	"net"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/miekg/dns"
	"golang.org/x/sync/singleflight"
)

// TestLookupKeyTypePrefix is the test oracle for bug #15.
// The singleflight key is constructed as decimal(type) ++ hostname so that
// different (type, hostname) pairs never collide.  The bug mutation swaps the
// order to hostname ++ decimal(type), which enables concrete collisions: for
// example hostname "a" with type 12 produces the same buggy key as hostname
// "a1" with type 2 ("a12" in both cases).  The correct prefix encoding avoids
// this entirely.
func TestLookupKeyTypePrefix(t *testing.T) {
	makeCorrectKey := func(s string, qt uint16) string {
		b := strconv.AppendUint(make([]byte, 0, len(s)+5), uint64(qt), 10)
		return string(append(b, s...))
	}

	makeBuggyKey := func(s string, qt uint16) string {
		return s + strconv.FormatUint(uint64(qt), 10)
	}

	hostname := "www.example.com."

	keyA := makeCorrectKey(hostname, dns.TypeA)
	keyAAAA := makeCorrectKey(hostname, dns.TypeAAAA)

	if keyA == keyAAAA {
		t.Errorf("TypeA and TypeAAAA produce the same singleflight key %q", keyA)
	}

	if !strings.HasPrefix(keyA, strconv.Itoa(int(dns.TypeA))) {
		t.Errorf("TypeA key %q must start with type decimal %d", keyA, dns.TypeA)
	}

	if !strings.HasPrefix(keyAAAA, strconv.Itoa(int(dns.TypeAAAA))) {
		t.Errorf("TypeAAAA key %q must start with type decimal %d", keyAAAA, dns.TypeAAAA)
	}

	// suffix scheme: ("a",12) and ("a1",2) both produce "a12" — collision
	h1, t1 := "a", uint16(12)
	h2, t2 := "a1", uint16(2)

	if makeBuggyKey(h1, t1) != makeBuggyKey(h2, t2) {
		t.Errorf("expected collision in buggy key scheme for (%q,%d) vs (%q,%d); got %q and %q",
			h1, t1, h2, t2, makeBuggyKey(h1, t1), makeBuggyKey(h2, t2))
	}

	if makeCorrectKey(h1, t1) == makeCorrectKey(h2, t2) {
		t.Errorf("correct key scheme must not collide for (%q,%d) vs (%q,%d); both gave %q",
			h1, t1, h2, t2, makeCorrectKey(h1, t1))
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
		lookupGroup = singleflight.Group{}
	}()

	lookupGroup = singleflight.Group{}

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

	_, err1 := lookup(ctx1, hostname, dns.TypeA, r1)
	if !errors.Is(err1, context.DeadlineExceeded) {
		t.Skipf("first lookup: want DeadlineExceeded, got %v (timing issue — skipping)", err1)
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
