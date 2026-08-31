// SPDX-FileCopyrightText: 2021 Dinko Korunic
// SPDX-License-Identifier: MIT

package main

import (
	"bytes"
	"context"
	"flag"
	"fmt"
	"net"
	"net/netip"
	"os"
	"strings"
	"sync"

	"github.com/miekg/dns"
	"github.com/monoidic/cidranger/v2"
)

const (
	wildcard          = "*"
	fileZoneSeparator = "="
)

// processRemoteZone calls zoneTransfer() for AXFR and processRecords() for handling each valid RR.
func processRemoteZone(zone, server string, doCIDR bool, ranger cidranger.Ranger[struct{}], hosts chan<- HostEntry) {
	if *verbose {
		fmt.Fprintf(os.Stderr, "Info: doing AXFR for zone %q / server %q\n", zone, server)
	}

	zoneRecords := zoneTransfer(zone, server)
	processRecords(zone, doCIDR, ranger, hosts, zoneRecords)
}

// processLocalZone calls zoneParser() for local zone parse and processRecords() for handling valid RR.
func processLocalZone(zone string, doCIDR bool, ranger cidranger.Ranger[struct{}], hosts chan<- HostEntry) {
	var domain string

	file := zone

	if strings.Contains(zone, fileZoneSeparator) {
		t := strings.Split(zone, fileZoneSeparator)

		if len(t) == 2 {
			file = t[0]             // filename
			domain = dns.Fqdn(t[1]) // domain
		} else {
			fmt.Fprintf(os.Stderr, "Error: invalid file=domain option format: %q\n", zone)
			flag.Usage()
		}
	}

	if *verbose {
		fmt.Fprintf(os.Stderr, "Info: loading and parsing zone %q / domain %q\n", file, domain)
	}

	zoneRecords := zoneParser(file, domain)
	if len(zoneRecords) == 0 && domain == "" {
		fmt.Fprintf(os.Stderr, "Error: no detected records in %q file. Try next time with \"%v=domain\"\n",
			file, file)
	}

	// processRecords needs the DNS zone name, never the file path: it drives both
	// domain stripping and the CNAME in-zone test.  Passing the path silently
	// disabled -strip_domain/-strip_unstrip for every local zone file.  Fall back
	// to the SOA apex when no explicit "=domain" was given, which covers files
	// carrying their own $ORIGIN.
	zoneName := domain
	if zoneName == "" {
		zoneName = zoneNameFromRecords(zoneRecords)
	}

	processRecords(zoneName, doCIDR, ranger, hosts, zoneRecords)
}

// processRecords processes each RR and calls processHost() for each valid RR.
func processRecords(zone string, doCIDR bool, ranger cidranger.Ranger[struct{}], hosts chan<- HostEntry,
	zoneRecords []dns.RR,
) {
	var wg sync.WaitGroup

	// DNS names are case-insensitive and processHost lowercases every label, so the
	// zone must be lowercased too or the suffix comparisons silently never match.
	zone = strings.ToLower(strings.TrimSuffix(zone, endingDot))

	var r net.Resolver

	if *resolverAddress != "" {
		r = net.Resolver{
			PreferGo: true,
			Dial: func(ctx context.Context, network, _ string) (net.Conn, error) {
				d := net.Dialer{}

				return d.DialContext(ctx, network, *resolverAddress)
			},
		}
	} else {
		r = net.Resolver{PreferGo: true}
	}

	for _, rr := range zoneRecords {
		switch t := rr.(type) {
		case *dns.A:
			wg.Go(func() {
				if *ignoreStar && strings.Contains(t.Hdr.Name, wildcard) {
					return
				}

				ipAddr, ok := unmapAddrFromSlice(t.A)
				if !ok {
					return
				}

				if doCIDR && ranger != nil {
					if c, _ := ranger.Contains(ipAddr); !c {
						return
					}
				}

				processHost(t.Hdr.Name, zone, ipAddr, hosts)
			})
		case *dns.AAAA:
			wg.Go(func() {
				if *ignoreStar && strings.Contains(t.Hdr.Name, wildcard) {
					return
				}

				ipAddr6, ok := unmapAddrFromSlice(t.AAAA)
				if !ok {
					return
				}

				if doCIDR && ranger != nil {
					if c, _ := ranger.Contains(ipAddr6); !c {
						return
					}
				}

				processHost(t.Hdr.Name, zone, ipAddr6, hosts)
			})
		case *dns.CNAME:
			wg.Go(func() {
				ctx := context.Background()

				// non-greedy: drop CNAMEs whose target leaves the zone
				if !*greedyCNAME {
					cnames, err := lookup(ctx, t.Hdr.Name, dns.TypeCNAME, &r)
					if err != nil {
						return
					}

					if len(cnames) > 0 && !strings.HasSuffix(cnames[0], dns.Fqdn(zone)) {
						return
					}
				}

				addrs, err := lookup(ctx, t.Hdr.Name, dns.TypeA, &r)
				if err != nil {
					return
				}

				for _, a := range addrs {
					ipAddr, err := unmapParseAddr(a)
					if err != nil {
						continue
					}

					if doCIDR && ranger != nil {
						if c, _ := ranger.Contains(ipAddr); !c {
							continue
						}
					}

					processHost(t.Hdr.Name, zone, ipAddr, hosts)
				}
			})
		default:
		}
	}

	wg.Wait()
}

// zoneNameFromRecords returns the SOA owner name, which is the zone apex, or an
// empty string when the parsed records carry no SOA.
func zoneNameFromRecords(records []dns.RR) string {
	for _, rr := range records {
		if soa, ok := rr.(*dns.SOA); ok {
			return soa.Hdr.Name
		}
	}

	return ""
}

// zoneParser loads zones into memory and parses them, returning a slice of RRs.
func zoneParser(zone, domain string) []dns.RR {
	var records []dns.RR

	z, err := os.ReadFile(zone)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: problem reading zone file: %q: %v\n", zone, err)

		return records
	}

	// RFC 1035 zone parser
	zp := dns.NewZoneParser(bytes.NewReader(z), domain, "")

	for rr, ok := zp.Next(); ok; rr, ok = zp.Next() {
		records = append(records, rr)
	}

	// Next() reports ok=false on the first parse error, so zp.Err() only ever turns
	// non-nil once the loop has ended.  Checking it inside the loop could never
	// fire, which left a malformed line silently truncating the zone: every record
	// after the offending line is dropped, with no diagnostic at all.
	if err := zp.Err(); err != nil {
		fmt.Fprintf(os.Stderr,
			"Error: problem parsing zone %q, stopped at first error, later records skipped: %v\n",
			zone, err)
	}

	return records
}

// unmapAddrFromSlice parses 4 or 16-byte slice as IPv4 or IPv6 address and removes any IPv4-mapped IPv6 prefix.
func unmapAddrFromSlice(slice []byte) (netip.Addr, bool) {
	ipAddr, ok := netip.AddrFromSlice(slice)
	if !ok {
		return ipAddr, false
	}

	return ipAddr.Unmap(), true
}

// unmapParseAddr parses string as an IP address, returning result and removes any IPv4-mapped IPv6 prefix.
func unmapParseAddr(s string) (netip.Addr, error) {
	ipAddr, err := netip.ParseAddr(s)
	if err != nil {
		return ipAddr, err
	}

	return ipAddr.Unmap(), nil
}
