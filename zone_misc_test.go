// SPDX-FileCopyrightText: 2026 Dinko Korunic
// SPDX-License-Identifier: MIT

package main

import (
	"net"
	"slices"
	"strings"
	"testing"

	"github.com/miekg/dns"
)

// TestProcessRemoteZone exercises the full remote path end to end: AXFR against a
// live in-process server, then record processing into the hosts channel.  The
// SOA/NS records in the stream must be skipped, leaving only address records.
func TestProcessRemoteZone(t *testing.T) {
	withMaxRetries(t, 1)

	savedStar := *ignoreStar
	savedGreedy := *greedyCNAME

	t.Cleanup(func() {
		*ignoreStar = savedStar
		*greedyCNAME = savedGreedy
	})

	*ignoreStar = false
	// keep the CNAME in the fixture from triggering a real DNS lookup
	*greedyCNAME = false

	addr := startAXFRServer(t, [][]dns.RR{axfrRecords(t)})

	hosts := make(chan HostEntry, 20)
	processRemoteZone(axfrTestZone, addr, false, nil, hosts)

	got := drain(hosts)

	// www + mail (A) + ipv6 (AAAA); SOA skipped, CNAME dropped as out-of-zone
	if len(got) != 3 {
		t.Fatalf("processRemoteZone() = %d entries, want 3; got %+v", len(got), got)
	}

	labels := make(map[string]bool, len(got))
	for _, h := range got {
		labels[h.label] = true
	}

	for _, want := range []string{"www.example.com", "mail.example.com", "ipv6.example.com"} {
		if !labels[want] {
			t.Errorf("processRemoteZone() missing label %q; got %v", want, labels)
		}
	}
}

// TestProcessRemoteZoneCIDR checks CIDR filtering is applied on the remote path.
func TestProcessRemoteZoneCIDR(t *testing.T) {
	withMaxRetries(t, 1)

	savedStar := *ignoreStar
	savedGreedy := *greedyCNAME

	t.Cleanup(func() {
		*ignoreStar = savedStar
		*greedyCNAME = savedGreedy
	})

	*ignoreStar = false
	*greedyCNAME = false

	addr := startAXFRServer(t, [][]dns.RR{axfrRecords(t)})

	// only the IPv6 record falls inside this prefix
	ranger, doCIDR := rangerInit([]string{"2001:db8::/32"})

	hosts := make(chan HostEntry, 20)
	processRemoteZone(axfrTestZone, addr, doCIDR, ranger, hosts)

	got := drain(hosts)

	if len(got) != 1 {
		t.Fatalf("processRemoteZone() with CIDR = %d entries, want 1; got %+v", len(got), got)
	}

	if got[0].label != "ipv6.example.com" {
		t.Errorf("label = %q, want %q", got[0].label, "ipv6.example.com")
	}
}

// TestProcessRemoteZoneVerbose covers the -verbose announcement on the remote path.
func TestProcessRemoteZoneVerbose(t *testing.T) {
	withMaxRetries(t, 1)

	saved := *verbose

	t.Cleanup(func() { *verbose = saved })

	*verbose = true

	addr := startAXFRServer(t, [][]dns.RR{axfrRecords(t)})

	hosts := make(chan HostEntry, 20)

	stderr := captureStderr(func() {
		processRemoteZone(axfrTestZone, addr, false, nil, hosts)
	})

	drain(hosts)

	if !strings.Contains(stderr, "doing AXFR") {
		t.Errorf("verbose processRemoteZone() stderr = %q, want an AXFR announcement", stderr)
	}
}

// TestProcessRemoteZoneUnreachable checks one dead nameserver degrades to zero
// entries rather than aborting: main runs zones concurrently, so a hard failure
// here would lose every other zone's output too.
func TestProcessRemoteZoneUnreachable(t *testing.T) {
	withMaxRetries(t, 1)

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("net.Listen: %v", err)
	}

	addr := ln.Addr().String()
	_ = ln.Close()

	hosts := make(chan HostEntry, 5)

	stderr := captureStderr(func() {
		processRemoteZone(axfrTestZone, addr, false, nil, hosts)
	})

	if got := drain(hosts); len(got) != 0 {
		t.Errorf("processRemoteZone() against a dead server = %d entries, want 0", len(got))
	}

	if !strings.Contains(stderr, "AXFR failure") {
		t.Errorf("processRemoteZone() did not report the failure; stderr=%q", stderr)
	}
}

// TestProcessRecordsMalformedAddress covers the unmapAddrFromSlice failure branch.
// dns.A carries a net.IP, a plain byte slice with no length invariant, so a zone
// file or a buggy server can yield a 3-byte "address".  netip.AddrFromSlice rejects
// it and the record must be skipped rather than emitted as a garbage entry.
func TestProcessRecordsMalformedAddress(t *testing.T) {
	saved := *ignoreStar

	t.Cleanup(func() { *ignoreStar = saved })

	*ignoreStar = false

	tests := []struct {
		name string
		rr   dns.RR
	}{
		{
			name: "A record with 3-byte address",
			rr: &dns.A{
				Hdr: dns.RR_Header{Name: "bad.example.com.", Rrtype: dns.TypeA, Class: dns.ClassINET},
				A:   net.IP{192, 0, 2},
			},
		},
		{
			name: "A record with nil address",
			rr: &dns.A{
				Hdr: dns.RR_Header{Name: "nil.example.com.", Rrtype: dns.TypeA, Class: dns.ClassINET},
				A:   nil,
			},
		},
		{
			name: "AAAA record with 5-byte address",
			rr: &dns.AAAA{
				Hdr:  dns.RR_Header{Name: "bad6.example.com.", Rrtype: dns.TypeAAAA, Class: dns.ClassINET},
				AAAA: net.IP{0x20, 0x01, 0x0d, 0xb8, 0x00},
			},
		},
		{
			name: "AAAA record with nil address",
			rr: &dns.AAAA{
				Hdr:  dns.RR_Header{Name: "nil6.example.com.", Rrtype: dns.TypeAAAA, Class: dns.ClassINET},
				AAAA: nil,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			hosts := make(chan HostEntry, 5)
			processRecords("example.com", false, nil, hosts, []dns.RR{tt.rr})

			if got := drain(hosts); len(got) != 0 {
				t.Errorf("processRecords() emitted %d entries for a malformed address, want 0: %+v",
					len(got), got)
			}
		})
	}
}

// TestProcessLocalZoneInvalidFileDomainFormat covers the malformed "file=domain"
// branch.  More than one '=' is ambiguous, so the argument is rejected with a
// diagnostic instead of being silently truncated to the first two fields.
func TestProcessLocalZoneInvalidFileDomainFormat(t *testing.T) {
	hosts := make(chan HostEntry, 5)

	stderr := captureStderr(func() {
		processLocalZone("/nonexistent/zone.txt=example.com=extra", false, nil, hosts)
	})

	if got := drain(hosts); len(got) != 0 {
		t.Errorf("processLocalZone() = %d entries for a malformed argument, want 0", len(got))
	}

	if !strings.Contains(stderr, "invalid file=domain") {
		t.Errorf("processLocalZone() did not report the malformed argument; stderr=%q", stderr)
	}
}

// TestProcessLocalZoneVerbose covers the -verbose announcement on the local path.
func TestProcessLocalZoneVerbose(t *testing.T) {
	saved := *verbose

	t.Cleanup(func() { *verbose = saved })

	*verbose = true

	zoneFile := writeTempZone(t, "@ IN SOA ns1 admin 1 2 3 4 5\nhost IN A 192.0.2.1\n")

	hosts := make(chan HostEntry, 10)

	stderr := captureStderr(func() {
		processLocalZone(zoneFile+"=example.com", false, nil, hosts)
	})

	drain(hosts)

	if !strings.Contains(stderr, "loading and parsing zone") {
		t.Errorf("verbose processLocalZone() stderr = %q, want a parse announcement", stderr)
	}
}

// TestZoneParserReportsParseError covers the parse-error diagnostic.
//
// dns.ZoneParser.Next returns ok=false at the first malformed line, so the error
// is only observable after the loop.  The original in-loop check could never fire,
// which meant a single bad line silently truncated the zone: records before it were
// returned, everything after was dropped, and nothing was printed.  This test pins
// both halves — the surviving records and the diagnostic.
func TestZoneParserReportsParseError(t *testing.T) {
	zoneFile := writeTempZone(t, `$TTL 3600
@ IN SOA ns1 admin 2021010101 3600 900 604800 300
before IN A 192.0.2.1
this is a totally bogus line !!!
after IN A 192.0.2.2
`)

	var records []dns.RR

	stderr := captureStderr(func() {
		records = zoneParser(zoneFile, "example.com.")
	})

	if len(records) == 0 {
		t.Fatal("zoneParser() returned no records; records before the malformed line must be kept")
	}

	var names []string
	for _, rr := range records {
		names = append(names, rr.Header().Name)
	}

	if !slices.Contains(names, "before.example.com.") {
		t.Errorf("zoneParser() dropped the record before the malformed line; got %v", names)
	}

	if !strings.Contains(stderr, "problem parsing zone") {
		t.Errorf("zoneParser() did not report the parse error; stderr=%q", stderr)
	}

	// document the truncation: parsing genuinely stops at the bad line
	if slices.Contains(names, "after.example.com.") {
		t.Logf("parser recovered past the malformed line; got %v", names)
	}
}

// TestZoneParserValidZoneIsSilent is the negative control: a well-formed zone must
// produce no diagnostic, otherwise the error path above would be meaningless.
func TestZoneParserValidZoneIsSilent(t *testing.T) {
	zoneFile := writeTempZone(t, `$TTL 3600
@ IN SOA ns1 admin 2021010101 3600 900 604800 300
one IN A 192.0.2.1
two IN A 192.0.2.2
`)

	var records []dns.RR

	stderr := captureStderr(func() {
		records = zoneParser(zoneFile, "example.com.")
	})

	if len(records) != 3 {
		t.Errorf("zoneParser() = %d records, want 3 (SOA + 2 A)", len(records))
	}

	if stderr != "" {
		t.Errorf("zoneParser() on a valid zone wrote to stderr: %q", stderr)
	}
}

// TestProcessLocalZoneStripsDomain is the unit-level oracle for the local-zone
// strip bug: processLocalZone must hand processRecords the DNS zone name, not the
// file path it was parsed from.
func TestProcessLocalZoneStripsDomain(t *testing.T) {
	savedStrip := *stripDomain
	savedUnstrip := *stripUnstrip

	t.Cleanup(func() {
		*stripDomain = savedStrip
		*stripUnstrip = savedUnstrip
	})

	content := `$TTL 3600
@ IN SOA ns1 admin 2021010101 3600 900 604800 300
host1 IN A 192.0.2.1
`

	tests := []struct {
		name     string
		arg      string
		strip    bool
		unstrip  bool
		want     []string
		zoneFile bool
	}{
		{
			name:  "explicit domain strips",
			strip: true,
			want:  []string{"host1"},
		},
		{
			name:    "strip_unstrip keeps both",
			unstrip: true,
			want:    []string{"host1", "host1.example.com"},
		},
		{
			name:  "no strip flags leaves the FQDN",
			want:  []string{"host1.example.com"},
			strip: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			*stripDomain = tt.strip
			*stripUnstrip = tt.unstrip

			zoneFile := writeTempZone(t, content)

			hosts := make(chan HostEntry, 10)
			processLocalZone(zoneFile+"=example.com", false, nil, hosts)

			got := drain(hosts)

			var labels []string
			for _, h := range got {
				labels = append(labels, h.label)
			}

			slices.Sort(labels)
			slices.Sort(tt.want)

			if !slices.Equal(labels, tt.want) {
				t.Errorf("processLocalZone() labels = %v, want %v"+
					" (the zone name, not the file path, must reach processRecords)",
					labels, tt.want)
			}
		})
	}
}

// TestZoneNameFromRecords covers the SOA fallback used when no "=domain" was
// supplied, which is how a file carrying its own $ORIGIN gets a zone name.
func TestZoneNameFromRecords(t *testing.T) {
	soa, err := dns.NewRR("example.com. 3600 IN SOA ns1.example.com. admin.example.com. 1 2 3 4 5")
	if err != nil {
		t.Fatalf("building SOA: %v", err)
	}

	a, err := dns.NewRR("host.example.com. 3600 IN A 192.0.2.1")
	if err != nil {
		t.Fatalf("building A: %v", err)
	}

	tests := []struct {
		name    string
		records []dns.RR
		want    string
	}{
		{"SOA present", []dns.RR{soa, a}, "example.com."},
		{"SOA after other records", []dns.RR{a, soa}, "example.com."},
		{"no SOA yields empty", []dns.RR{a}, ""},
		{"empty input yields empty", nil, ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := zoneNameFromRecords(tt.records); got != tt.want {
				t.Errorf("zoneNameFromRecords() = %q, want %q", got, tt.want)
			}
		})
	}
}

// TestProcessRecordsZoneCaseInsensitive pins DNS case-insensitivity: labels are
// lowercased, so the zone must be too or every suffix comparison silently misses.
func TestProcessRecordsZoneCaseInsensitive(t *testing.T) {
	savedStrip := *stripDomain
	savedStar := *ignoreStar

	t.Cleanup(func() {
		*stripDomain = savedStrip
		*ignoreStar = savedStar
	})

	*stripDomain = true
	*ignoreStar = false

	for _, zone := range []string{"example.com", "EXAMPLE.COM", "Example.Com", "example.com."} {
		t.Run(zone, func(t *testing.T) {
			hosts := make(chan HostEntry, 5)
			processRecords(zone, false, nil, hosts, []dns.RR{
				&dns.A{
					Hdr: dns.RR_Header{Name: "HOST1.Example.COM.", Rrtype: dns.TypeA, Class: dns.ClassINET},
					A:   net.IP{192, 0, 2, 1},
				},
			})

			got := drain(hosts)

			if len(got) != 1 {
				t.Fatalf("processRecords() = %d entries, want 1", len(got))
			}

			if got[0].label != "host1" {
				t.Errorf("label = %q, want %q (zone %q must match case-insensitively)",
					got[0].label, "host1", zone)
			}
		})
	}
}

// TestProcessLocalZoneQualifiesRelativeNames covers the dns.Fqdn applied to the
// "=domain" argument.  The zone file below uses relative names and carries no
// $ORIGIN, so the origin handed to the parser is the only thing that can qualify
// them.  A non-FQDN origin is not a valid parser origin, so dropping the Fqdn
// silently yields an unqualified (or empty) zone.
func TestProcessLocalZoneQualifiesRelativeNames(t *testing.T) {
	zoneFile := writeTempZone(t, `$TTL 3600
@ IN SOA ns1 admin 2021010101 3600 900 604800 300
host1 IN A 192.0.2.1
`)

	hosts := make(chan HostEntry, 10)

	// note: "example.com" without a trailing dot, which is how a user types it
	_ = captureStderr(func() {
		processLocalZone(zoneFile+"=example.com", false, nil, hosts)
	})

	got := drain(hosts)

	var labels []string
	for _, h := range got {
		labels = append(labels, h.label)
	}

	if !slices.Contains(labels, "host1.example.com") {
		t.Errorf("labels = %v, want host1.example.com: the relative name was not "+
			"qualified against the =domain argument", labels)
	}
}

// TestProcessRecordsIgnoresWildcardAnywhere covers -ignore_star for a wildcard that
// is not the leading label.  Wildcards conventionally appear first, which makes
// tightening the check to a prefix match look harmless; it is not, because a name
// such as foo.*.example.com would then reach the output.
func TestProcessRecordsIgnoresWildcardAnywhere(t *testing.T) {
	saved := *ignoreStar

	t.Cleanup(func() { *ignoreStar = saved })

	*ignoreStar = true

	for _, tt := range []struct {
		name   string
		record string
	}{
		{"leading wildcard label", "*.example.com. 3600 IN A 192.0.2.9"},
		{"wildcard in a middle label", "foo.*.example.com. 3600 IN A 192.0.2.9"},
		{"wildcard in an AAAA record", "foo.*.example.com. 3600 IN AAAA 2001:db8::9"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			star, err := dns.NewRR(tt.record)
			if err != nil {
				t.Fatalf("building RR %q: %v", tt.record, err)
			}

			keep, err := dns.NewRR("host1.example.com. 3600 IN A 192.0.2.1")
			if err != nil {
				t.Fatalf("building RR: %v", err)
			}

			hosts := make(chan HostEntry, 10)
			processRecords("example.com", false, nil, hosts, []dns.RR{star, keep})

			for _, h := range drain(hosts) {
				if strings.Contains(h.label, "*") {
					t.Errorf("wildcard record %q reached the output as %q despite -ignore_star",
						tt.record, h.label)
				}
			}
		})
	}
}
