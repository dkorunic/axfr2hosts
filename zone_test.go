package main

import (
	"net"
	"net/netip"
	"os"
	"testing"

	"github.com/miekg/dns"
)

func TestUnmapAddrFromSlice(t *testing.T) {
	tests := []struct {
		name     string
		slice    []byte
		expected netip.Addr
		ok       bool
	}{
		{
			name:     "IPv4 address",
			slice:    []byte{192, 0, 2, 1},
			expected: netip.MustParseAddr("192.0.2.1"),
			ok:       true,
		},
		{
			name:     "IPv6 address",
			slice:    []byte{0x20, 0x01, 0x0d, 0xb8, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 1},
			expected: netip.MustParseAddr("2001:db8::1"),
			ok:       true,
		},
		{
			name:     "IPv4-mapped IPv6 address",
			slice:    []byte{0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0xff, 0xff, 192, 0, 2, 1},
			expected: netip.MustParseAddr("192.0.2.1"),
			ok:       true,
		},
		{
			name:     "Invalid slice length",
			slice:    []byte{1, 2, 3},
			expected: netip.Addr{},
			ok:       false,
		},
		{
			name:     "Empty slice",
			slice:    []byte{},
			expected: netip.Addr{},
			ok:       false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := unmapAddrFromSlice(tt.slice)
			if ok != tt.ok {
				t.Errorf("unmapAddrFromSlice() ok = %v, want %v", ok, tt.ok)
			}
			if ok && got != tt.expected {
				t.Errorf("unmapAddrFromSlice() got = %v, want %v", got, tt.expected)
			}
		})
	}
}

func TestUnmapParseAddr(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected netip.Addr
		wantErr  bool
	}{
		{
			name:     "IPv4 address",
			input:    "192.0.2.1",
			expected: netip.MustParseAddr("192.0.2.1"),
			wantErr:  false,
		},
		{
			name:     "IPv6 address",
			input:    "2001:db8::1",
			expected: netip.MustParseAddr("2001:db8::1"),
			wantErr:  false,
		},
		{
			name:     "IPv4-mapped IPv6 address",
			input:    "::ffff:192.0.2.1",
			expected: netip.MustParseAddr("192.0.2.1"),
			wantErr:  false,
		},
		{
			name:     "Invalid address",
			input:    "invalid",
			expected: netip.Addr{},
			wantErr:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := unmapParseAddr(tt.input)
			if (err != nil) != tt.wantErr {
				t.Errorf("unmapParseAddr() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !tt.wantErr && got != tt.expected {
				t.Errorf("unmapParseAddr() got = %v, want %v", got, tt.expected)
			}
		})
	}
}

func TestZoneParser(t *testing.T) {
	// Zone file without $ORIGIN; domain is supplied via parameter.
	zoneContent := `$TTL 3600
@ IN SOA ns1 admin 2021010101 3600 900 604800 300
@ IN NS ns1
ns1   IN A    192.0.2.1
host1 IN A    192.0.2.2
host2 IN AAAA 2001:db8::1
`
	tmpFile, err := os.CreateTemp("", "test-zone-*.txt")
	if err != nil {
		t.Fatalf("CreateTemp: %v", err)
	}
	defer os.Remove(tmpFile.Name())

	if _, err := tmpFile.WriteString(zoneContent); err != nil {
		t.Fatalf("WriteString: %v", err)
	}
	tmpFile.Close()

	records := zoneParser(tmpFile.Name(), "example.com.")

	var aCount, aaaaCount, soaCount, nsCount int
	for _, rr := range records {
		switch rr.(type) {
		case *dns.A:
			aCount++
		case *dns.AAAA:
			aaaaCount++
		case *dns.SOA:
			soaCount++
		case *dns.NS:
			nsCount++
		}
	}

	if aCount != 2 {
		t.Errorf("zoneParser() A records = %d, want 2", aCount)
	}
	if aaaaCount != 1 {
		t.Errorf("zoneParser() AAAA records = %d, want 1", aaaaCount)
	}
	if soaCount != 1 {
		t.Errorf("zoneParser() SOA records = %d, want 1", soaCount)
	}
	if nsCount != 1 {
		t.Errorf("zoneParser() NS records = %d, want 1", nsCount)
	}
}

func TestZoneParserNonExistent(t *testing.T) {
	records := zoneParser("/nonexistent/zone-file-that-does-not-exist.txt", "example.com.")
	if len(records) != 0 {
		t.Errorf("zoneParser() = %d records for non-existent file, want 0", len(records))
	}
}

func TestProcessRecords(t *testing.T) {
	origIgnoreStar := *ignoreStar
	defer func() { *ignoreStar = origIgnoreStar }()
	*ignoreStar = false

	hosts := make(chan HostEntry, 20)

	rrs := []dns.RR{
		&dns.A{
			Hdr: dns.RR_Header{Name: "host1.example.com.", Rrtype: dns.TypeA, Class: dns.ClassINET, Ttl: 3600},
			A:   net.IP{192, 0, 2, 1},
		},
		&dns.AAAA{
			Hdr:  dns.RR_Header{Name: "host2.example.com.", Rrtype: dns.TypeAAAA, Class: dns.ClassINET, Ttl: 3600},
			AAAA: net.IP{0x20, 0x01, 0x0d, 0xb8, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 1},
		},
		// SOA is not A/AAAA/CNAME; must be silently ignored.
		&dns.SOA{
			Hdr: dns.RR_Header{Name: "example.com.", Rrtype: dns.TypeSOA, Class: dns.ClassINET, Ttl: 3600},
		},
	}

	processRecords("example.com", false, nil, hosts, rrs)
	close(hosts)

	var results []HostEntry
	for h := range hosts {
		results = append(results, h)
	}

	if len(results) != 2 {
		t.Errorf("processRecords() = %d entries, want 2", len(results))
	}
}

func TestProcessRecordsIgnoreStar(t *testing.T) {
	origIgnoreStar := *ignoreStar
	defer func() { *ignoreStar = origIgnoreStar }()
	*ignoreStar = true

	hosts := make(chan HostEntry, 20)

	rrs := []dns.RR{
		&dns.A{
			Hdr: dns.RR_Header{Name: "*.example.com.", Rrtype: dns.TypeA, Class: dns.ClassINET, Ttl: 3600},
			A:   net.IP{192, 0, 2, 1},
		},
		&dns.A{
			Hdr: dns.RR_Header{Name: "host1.example.com.", Rrtype: dns.TypeA, Class: dns.ClassINET, Ttl: 3600},
			A:   net.IP{192, 0, 2, 2},
		},
		&dns.AAAA{
			Hdr:  dns.RR_Header{Name: "*.v6.example.com.", Rrtype: dns.TypeAAAA, Class: dns.ClassINET, Ttl: 3600},
			AAAA: net.IP{0x20, 0x01, 0x0d, 0xb8, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 1},
		},
	}

	processRecords("example.com", false, nil, hosts, rrs)
	close(hosts)

	var results []HostEntry
	for h := range hosts {
		results = append(results, h)
	}

	if len(results) != 1 {
		t.Errorf("processRecords() = %d entries, want 1 (wildcards ignored)", len(results))
	}

	if len(results) == 1 && results[0].label != "host1.example.com" {
		t.Errorf("processRecords() label = %q, want %q", results[0].label, "host1.example.com")
	}
}

func TestProcessRecordsCIDR(t *testing.T) {
	origIgnoreStar := *ignoreStar
	defer func() { *ignoreStar = origIgnoreStar }()
	*ignoreStar = false

	ranger, doCIDR := rangerInit([]string{"192.0.2.0/24"})

	hosts := make(chan HostEntry, 20)

	rrs := []dns.RR{
		&dns.A{
			Hdr: dns.RR_Header{Name: "host1.example.com.", Rrtype: dns.TypeA, Class: dns.ClassINET, Ttl: 3600},
			A:   net.IP{192, 0, 2, 1}, // inside CIDR
		},
		&dns.A{
			Hdr: dns.RR_Header{Name: "host2.example.com.", Rrtype: dns.TypeA, Class: dns.ClassINET, Ttl: 3600},
			A:   net.IP{10, 0, 0, 1}, // outside CIDR
		},
		&dns.AAAA{
			Hdr:  dns.RR_Header{Name: "host3.example.com.", Rrtype: dns.TypeAAAA, Class: dns.ClassINET, Ttl: 3600},
			AAAA: net.IP{0x20, 0x01, 0x0d, 0xb8, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 1}, // outside CIDR
		},
	}

	processRecords("example.com", doCIDR, ranger, hosts, rrs)
	close(hosts)

	var results []HostEntry
	for h := range hosts {
		results = append(results, h)
	}

	if len(results) != 1 {
		t.Errorf("processRecords() = %d entries, want 1 (CIDR filtered)", len(results))
	}

	if len(results) == 1 && results[0].label != "host1.example.com" {
		t.Errorf("processRecords() label = %q, want %q", results[0].label, "host1.example.com")
	}
}

func TestProcessLocalZone(t *testing.T) {
	// Zone file without $ORIGIN; domain supplied via "file=domain" argument.
	zoneContent := `$TTL 3600
@ IN SOA ns1 admin 2021010101 3600 900 604800 300
@ IN NS ns1
ns1   IN A    192.0.2.1
host1 IN A    192.0.2.2
host2 IN AAAA 2001:db8::1
`
	tmpFile, err := os.CreateTemp("", "test-local-zone-*.txt")
	if err != nil {
		t.Fatalf("CreateTemp: %v", err)
	}
	defer os.Remove(tmpFile.Name())

	if _, err := tmpFile.WriteString(zoneContent); err != nil {
		t.Fatalf("WriteString: %v", err)
	}
	tmpFile.Close()

	hosts := make(chan HostEntry, 20)
	processLocalZone(tmpFile.Name()+"=example.com", false, nil, hosts)
	close(hosts)

	var results []HostEntry
	for h := range hosts {
		results = append(results, h)
	}

	// Expect at least 3 host entries: ns1, host1, host2 (SOA/NS are skipped)
	if len(results) < 3 {
		t.Errorf("processLocalZone() = %d entries, want at least 3", len(results))
	}
}

func TestProcessLocalZoneNoSeparator(t *testing.T) {
	// Without "=domain", zoneParser is called with domain=""; non-$ORIGIN file yields no records
	// and a warning is printed (but no panic or crash).
	tmpFile, err := os.CreateTemp("", "test-empty-zone-*.txt")
	if err != nil {
		t.Fatalf("CreateTemp: %v", err)
	}
	defer os.Remove(tmpFile.Name())
	tmpFile.Close()

	hosts := make(chan HostEntry, 5)
	processLocalZone(tmpFile.Name(), false, nil, hosts)
	close(hosts)

	var results []HostEntry
	for h := range hosts {
		results = append(results, h)
	}

	if len(results) != 0 {
		t.Errorf("processLocalZone() empty zone = %d entries, want 0", len(results))
	}
}
