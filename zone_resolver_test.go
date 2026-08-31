// SPDX-FileCopyrightText: 2026 Dinko Korunic
// SPDX-License-Identifier: MIT

package main

import (
	"net"
	"net/netip"
	"testing"
	"time"

	"github.com/miekg/dns"
)

// resolverEntry is one name in the test recursor's zone: either an alias (cname
// set) or a terminal address (a set).
type resolverEntry struct {
	cname string
	a     string
}

// testResolverZone is the fixture served by startTestResolver.  alias.* stays
// inside example.com; external.* points out of it.
var testResolverZone = map[string]resolverEntry{
	"www.example.com.":      {a: "192.0.2.10"},
	"target.other.com.":     {a: "198.51.100.5"},
	"alias.example.com.":    {cname: "www.example.com."},
	"external.example.com.": {cname: "target.other.com."},
}

// answerFor builds an RFC-correct answer section: a query for a CNAME'd name
// returns the CNAME chain followed by the target's address record.
//
// This matters more than it looks.  Go's pure resolver implements LookupCNAME by
// firing A, AAAA and CNAME queries concurrently and taking the canonical name from
// whichever answer it processes first.  A fixture that returned a bare A record
// with no CNAME in the answer section made LookupCNAME intermittently fall back to
// returning the queried name — which trivially carries the zone suffix and silently
// defeated the out-of-zone filter under test.
func answerFor(name string, qtype uint16) []dns.RR {
	var out []dns.RR

	// follow the CNAME chain, emitting each hop
	for range len(testResolverZone) {
		e, ok := testResolverZone[name]
		if !ok || e.cname == "" {
			break
		}

		if rr, err := dns.NewRR(name + " 60 IN CNAME " + e.cname); err == nil {
			out = append(out, rr)
		}

		name = e.cname
	}

	if qtype == dns.TypeA {
		if e, ok := testResolverZone[name]; ok && e.a != "" {
			if rr, err := dns.NewRR(name + " 60 IN A " + e.a); err == nil {
				out = append(out, rr)
			}
		}
	}

	return out
}

// startTestResolver runs an in-process UDP DNS recursor over testResolverZone and
// returns its "host:port" address.  Pointing -resolver_address at it makes the
// CNAME paths in processRecords hermetic: the pre-existing CNAME tests instead
// relied on real internet lookups failing, so they exercised only the error return
// and asserted nothing about filtering.
func startTestResolver(t *testing.T) string {
	t.Helper()

	pc, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("net.ListenPacket: %v", err)
	}

	handler := dns.HandlerFunc(func(w dns.ResponseWriter, req *dns.Msg) {
		m := new(dns.Msg)
		m.SetReply(req)
		m.Authoritative = true

		if len(req.Question) > 0 {
			q := req.Question[0]
			// AAAA deliberately yields only the CNAME chain: the fixture is IPv4-only
			m.Answer = answerFor(q.Name, q.Qtype)
		}

		_ = w.WriteMsg(m)
	})

	srv := &dns.Server{PacketConn: pc, Net: "udp", Handler: handler}

	go func() {
		_ = srv.ActivateAndServe()
	}()

	t.Cleanup(func() {
		_ = srv.Shutdown()
	})

	return pc.LocalAddr().String()
}

// useTestResolver points the global resolver flag at a local test recursor and
// restores every global it touches afterwards.
//
// It deliberately does NOT reset lookupGroup: singleflight retains no state once a
// call completes, so there is nothing to clear between tests, and assigning a fresh
// Group over the global races with the bookkeeping goroutine that DoChan leaves
// running past lookup()'s return.
func useTestResolver(t *testing.T) {
	t.Helper()

	savedAddr := *resolverAddress
	savedTimeout := *resolverTimeout
	savedGreedy := *greedyCNAME

	t.Cleanup(func() {
		*resolverAddress = savedAddr
		*resolverTimeout = savedTimeout
		*greedyCNAME = savedGreedy
	})

	*resolverAddress = startTestResolver(t)
	*resolverTimeout = 2 * time.Second
}

func cnameRR(name, target string) dns.RR {
	return &dns.CNAME{
		Hdr:    dns.RR_Header{Name: name, Rrtype: dns.TypeCNAME, Class: dns.ClassINET, Ttl: 3600},
		Target: target,
	}
}

func drain(hosts chan HostEntry) []HostEntry {
	close(hosts)

	var out []HostEntry
	for h := range hosts {
		out = append(out, h)
	}

	return out
}

// TestProcessRecordsCNAMEInZoneKept covers the non-greedy accept path: the CNAME
// target stays inside the zone, so the record is resolved and emitted.
func TestProcessRecordsCNAMEInZoneKept(t *testing.T) {
	useTestResolver(t)

	*greedyCNAME = false

	hosts := make(chan HostEntry, 10)
	processRecords("example.com", false, nil, hosts, []dns.RR{
		cnameRR("alias.example.com.", "www.example.com."),
	})

	got := drain(hosts)

	if len(got) != 1 {
		t.Fatalf("processRecords() = %d entries, want 1 (in-zone CNAME must be kept)", len(got))
	}

	if got[0].label != "alias.example.com" {
		t.Errorf("label = %q, want %q", got[0].label, "alias.example.com")
	}

	if got[0].ipAddr != netip.MustParseAddr("192.0.2.10") {
		t.Errorf("ipAddr = %v, want 192.0.2.10", got[0].ipAddr)
	}
}

// TestProcessRecordsCNAMEOutOfZoneDropped covers the non-greedy reject path.
// external.example.com is a CNAME to target.other.com, outside the zone, so with
// -greedy_cname=false it must be dropped even though it resolves fine.
func TestProcessRecordsCNAMEOutOfZoneDropped(t *testing.T) {
	useTestResolver(t)

	*greedyCNAME = false

	hosts := make(chan HostEntry, 10)
	processRecords("example.com", false, nil, hosts, []dns.RR{
		cnameRR("external.example.com.", "target.other.com."),
	})

	if got := drain(hosts); len(got) != 0 {
		t.Errorf("processRecords() = %d entries, want 0 (out-of-zone CNAME must be dropped when non-greedy)", len(got))
	}
}

// TestProcessRecordsCNAMEGreedyKeepsOutOfZone is the counterpart: with the default
// -greedy_cname=true the zone-membership check is skipped entirely and the same
// out-of-zone CNAME is resolved and kept.
func TestProcessRecordsCNAMEGreedyKeepsOutOfZone(t *testing.T) {
	useTestResolver(t)

	*greedyCNAME = true

	hosts := make(chan HostEntry, 10)
	processRecords("example.com", false, nil, hosts, []dns.RR{
		cnameRR("external.example.com.", "target.other.com."),
	})

	got := drain(hosts)

	if len(got) != 1 {
		t.Fatalf("processRecords() = %d entries, want 1 (greedy mode keeps out-of-zone CNAMEs)", len(got))
	}

	if got[0].ipAddr != netip.MustParseAddr("198.51.100.5") {
		t.Errorf("ipAddr = %v, want 198.51.100.5", got[0].ipAddr)
	}
}

// TestProcessRecordsCNAMECIDRFiltered covers CIDR filtering on the CNAME branch,
// which is applied per resolved address rather than per record.
func TestProcessRecordsCNAMECIDRFiltered(t *testing.T) {
	useTestResolver(t)

	*greedyCNAME = true

	tests := []struct {
		name  string
		cidr  []string
		rr    dns.RR
		count int
	}{
		{
			name:  "resolved address inside CIDR is kept",
			cidr:  []string{"192.0.2.0/24"},
			rr:    cnameRR("alias.example.com.", "www.example.com."),
			count: 1,
		},
		{
			name:  "resolved address outside CIDR is dropped",
			cidr:  []string{"192.0.2.0/24"},
			rr:    cnameRR("external.example.com.", "target.other.com."),
			count: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ranger, doCIDR := rangerInit(tt.cidr)

			hosts := make(chan HostEntry, 10)
			processRecords("example.com", doCIDR, ranger, hosts, []dns.RR{tt.rr})

			if got := drain(hosts); len(got) != tt.count {
				t.Errorf("processRecords() = %d entries, want %d", len(got), tt.count)
			}
		})
	}
}

// TestProcessRecordsCustomResolverAddress asserts the -resolver_address branch is
// actually taken: the test recursor is the only server that knows these names, so
// a non-empty result proves the custom Dial closure was used instead of the
// system resolver.
func TestProcessRecordsCustomResolverAddress(t *testing.T) {
	useTestResolver(t)

	*greedyCNAME = true

	if _, _, err := net.SplitHostPort(*resolverAddress); err != nil {
		t.Fatalf("test resolver address %q is not dialable: %v", *resolverAddress, err)
	}

	hosts := make(chan HostEntry, 10)
	processRecords("example.com", false, nil, hosts, []dns.RR{
		cnameRR("alias.example.com.", "www.example.com."),
	})

	if got := drain(hosts); len(got) != 1 {
		t.Errorf("processRecords() with -resolver_address = %d entries, want 1", len(got))
	}
}

// TestProcessRecordsCNAMEUnresolvable covers the lookup-error path: the recursor
// answers with an empty A set, so nothing is emitted and no goroutine panics.
func TestProcessRecordsCNAMEUnresolvable(t *testing.T) {
	useTestResolver(t)

	*greedyCNAME = true

	hosts := make(chan HostEntry, 10)
	processRecords("example.com", false, nil, hosts, []dns.RR{
		cnameRR("unknown.example.com.", "nowhere.example.com."),
	})

	if got := drain(hosts); len(got) != 0 {
		t.Errorf("processRecords() = %d entries, want 0 for an unresolvable CNAME", len(got))
	}
}
