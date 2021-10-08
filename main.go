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
	"net"
	"strings"

	"github.com/miekg/dns"
)

const wildcard = "*"

func main() {
	zoneName, server, cidrList := parseFlags()

	zoneRecords := zoneTransfer(zoneName, server)

	ranger, doCIDR := rangerInit(cidrList)

	hosts := make(map[string]map[string]int, len(zoneRecords))

	keys := make([]net.IP, 0, len(zoneRecords))

	// resolve and dump A and CNAME in hosts format
	for _, rr := range zoneRecords {
		switch t := rr.(type) {
		case *dns.A:
			// ignore wildcards if ignoreStar is used
			if *ignoreStar && strings.Contains(t.Hdr.Name, wildcard) {
				continue
			}
			// if CIDR matching is true, check if IP is whitelisted
			if doCIDR && ranger != nil {
				if c, _ := ranger.Contains(t.A); !c {
					continue
				}
			}

			keys, hosts = updateHosts(t.Hdr.Name, t.A.String(), t.A, keys, hosts)
		case *dns.CNAME:
			// ignore out-of-zone targets if not using greedyCNAME
			if !*greedyCNAME {
				cname, err := net.LookupCNAME(t.Hdr.Name)
				if err != nil {
					continue
				}

				if !strings.HasSuffix(cname, zoneName) {
					continue
				}
			}

			addrs, err := net.LookupHost(t.Hdr.Name)
			if err != nil {
				continue
			}

			// loop through resolved array
			for _, addr := range addrs {
				ipAddr := net.ParseIP(addr)
				if ipAddr == nil {
					continue
				}

				// if CIDR matching is true, check if IP is whitelisted
				if doCIDR && ranger != nil {
					if c, _ := ranger.Contains(ipAddr); !c {
						continue
					}
				}

				keys, hosts = updateHosts(t.Hdr.Name, addr, ipAddr, keys, hosts)
			}
		// every other RR type is skipped over
		default:
		}
	}

	displayHosts(keys, hosts)
}

// updateHosts updates hosts map with a new label-IP pair, returning updated hosts map and updated keys slice.
func updateHosts(label, addr string, ipAddr net.IP, keys []net.IP, results map[string]map[string]int) ([]net.IP,
	map[string]map[string]int) {
	label = strings.TrimSuffix(label, ".")
	label = strings.ToLower(label)

	if _, ok := results[addr]; ok {
		results[addr][label] = 1
	} else {
		keys = append(keys, ipAddr)
		results[addr] = make(map[string]int)
		results[addr][label] = 1
	}

	return keys, results
}
