// @license
// Copyright (C) 2021  Dinko Korunic
//
// Permission is hereby granted, free of charge, to any person obtaining a copy
// of this software and associated documentation files (the "Software"), to deal
// in the Software without restriction, including without limitation the rights
// to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
// copies of the Software, and to permit persons to whom the Software is
// furnished to do so, subject to the following conditions:
//
// The above copyright notice and this permission notice shall be included in all
// copies or substantial portions of the Software.
//
// THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
// IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
// FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
// AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
// LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
// OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE
// SOFTWARE.

package main

import (
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

// processRemoteZone is calling zoneTransfer() for AXFR and processRecords() for handling each valid RR.
func processRemoteZone(zone, server string, doCIDR bool, ranger cidranger.Ranger[struct{}], hosts chan<- HostEntry) {
	if *verbose {
		fmt.Fprintf(os.Stderr, "Info: doing AXFR for zone %q / server %q\n", zone, server)
	}

	zoneRecords := zoneTransfer(zone, server)
	processRecords(zone, doCIDR, ranger, hosts, zoneRecords)
}

// processLocalZone is calling zoneParser() for local zone parse and processRecords() for handling valid RR.
func processLocalZone(zone string, doCIDR bool, ranger cidranger.Ranger[struct{}], hosts chan<- HostEntry) {
	var domain string

	if strings.Contains(zone, fileZoneSeparator) {
		t := strings.Split(zone, fileZoneSeparator)

		if len(t) == 2 {
			zone = t[0]             // filename
			domain = dns.Fqdn(t[1]) // domain
		} else {
			fmt.Fprintf(os.Stderr, "Error: invalid file=domain option format: %q\n", zone)
			flag.Usage()
		}
	}

	if *verbose {
		fmt.Fprintf(os.Stderr, "Info: loading and parsing zone %q / domain %q\n", zone, domain)
	}

	zoneRecords := zoneParser(zone, domain)
	if len(zoneRecords) == 0 && domain == "" {
		fmt.Fprintf(os.Stderr, "Error: no detected records in %q file. Try next time with \"%v=domain\"\n",
			zone, zone)
	}

	processRecords(zone, doCIDR, ranger, hosts, zoneRecords)
}

// processRecords is processing each RR and calling processHost() for each valid RR.
func processRecords(zone string, doCIDR bool, ranger cidranger.Ranger[struct{}], hosts chan<- HostEntry,
	zoneRecords []dns.RR,
) {
	var wg sync.WaitGroup

	var r net.Resolver

	if *resolverAddress != "" {
		// custom DNS resolver
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

	// process each RR
	for _, rr := range zoneRecords {
		switch t := rr.(type) {
		case *dns.A:

			wg.Go(func() {
				// ignore wildcards if ignoreStar is used
				if *ignoreStar && strings.Contains(t.Hdr.Name, wildcard) {
					return
				}

				ipAddr, ok := unmapAddrFromSlice(t.A)
				if !ok {
					return
				}

				// if CIDR matching is true, check if IP is whitelisted
				if doCIDR && ranger != nil {
					if c, _ := ranger.Contains(ipAddr); !c {
						return
					}
				}

				processHost(t.Hdr.Name, zone, ipAddr, hosts)
			})
		case *dns.AAAA:

			wg.Go(func() {
				// ignore wildcards if ignoreStar is used
				if *ignoreStar && strings.Contains(t.Hdr.Name, wildcard) {
					return
				}

				ipAddr6, ok := unmapAddrFromSlice(t.AAAA)
				if !ok {
					return
				}

				// if CIDR matching is true, check if IP is whitelisted
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

				// ignore out-of-zone targets if not using greedyCNAME
				if !*greedyCNAME {
					cnames, err := lookup(ctx, t.Hdr.Name, dns.TypeCNAME, &r)
					if err != nil {
						return
					}

					if len(cnames) > 0 && !strings.HasSuffix(cnames[0], zone) {
						return
					}
				}

				addrs, err := lookup(ctx, t.Hdr.Name, dns.TypeA, &r)
				if err != nil {
					return
				}

				// loop through resolved array
				for _, a := range addrs {
					ipAddr, err := unmapParseAddr(a)
					if err != nil {
						continue
					}

					// if CIDR matching is true, check if IP is whitelisted
					if doCIDR && ranger != nil {
						if c, _ := ranger.Contains(ipAddr); !c {
							continue
						}
					}

					processHost(t.Hdr.Name, zone, ipAddr, hosts)
				}
			})
		// every other RR type is skipped over
		default:
		}
	}

	wg.Wait()
}

// zoneParser is parsing loading zones into memory and parsing them, returning slice of RRs.
func zoneParser(zone, domain string) []dns.RR {
	var records []dns.RR

	// read a whole zone into memory
	z, err := os.ReadFile(zone)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: problem reading zone file: %q: %v\n", zone, err)

		return records
	}

	// initialize RFC 1035 zone parser
	zp := dns.NewZoneParser(strings.NewReader(string(z)), domain, "")

	// fetch all RRs
	for rr, ok := zp.Next(); ok; rr, ok = zp.Next() {
		if err := zp.Err(); err != nil {
			fmt.Fprintf(os.Stderr, "Error: problem parsing zone %q but will try to continue: %v\n", zone, err)

			continue
		}

		records = append(records, rr)
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
