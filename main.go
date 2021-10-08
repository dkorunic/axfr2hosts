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

const (
	wildcard       = "*"
	defaultMapSize = 2048
)

func main() {
	zones, server, cidrList := parseFlags()

	ranger, doCIDR := rangerInit(cidrList)
	hosts := make(map[string]map[string]int, defaultMapSize)
	keys := make([]net.IP, 0, defaultMapSize)

	for _, zone := range zones {
		zoneRecords := zoneTransfer(zone, server)

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

				keys, hosts = updateHosts(t.Hdr.Name, t.A.String(), zone, t.A, keys, hosts)
			case *dns.CNAME:
				// ignore out-of-zone targets if not using greedyCNAME
				if !*greedyCNAME {
					cname, err := net.LookupCNAME(t.Hdr.Name)
					if err != nil {
						continue
					}

					if !strings.HasSuffix(cname, zone) {
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

					keys, hosts = updateHosts(t.Hdr.Name, addr, zone, ipAddr, keys, hosts)
				}
			// every other RR type is skipped over
			default:
			}
		}
	}

	displayHosts(keys, hosts)
}

// updateHosts cleans FQDN and optionally shortens it, calling low-level writeMap and returning updated hosts map and
// keys slice.
func updateHosts(label, addr, zone string, ipAddr net.IP, keys []net.IP,
	hosts map[string]map[string]int) ([]net.IP, map[string]map[string]int) {
	label = strings.TrimSuffix(label, endingDot)
	label = strings.ToLower(label)

	// strip domain if needed
	if *stripDomain || *stripUnstrip {
		labelStripped := strings.TrimSuffix(label, strings.Join([]string{endingDot, zone}, ""))
		if labelStripped != "" {
			if *stripUnstrip {
				keys, hosts = writeMap(labelStripped, addr, ipAddr, keys, hosts)
			} else {
				return writeMap(labelStripped, addr, ipAddr, keys, hosts)
			}
		}
	}

	return writeMap(label, addr, ipAddr, keys, hosts)
}

// writeMap updates hosts map with a new label-IP pair, returning updated hosts map and keys slice.
func writeMap(label, addr string, ipAddr net.IP, keys []net.IP, hosts map[string]map[string]int) ([]net.IP,
	map[string]map[string]int) {
	if _, ok := hosts[addr]; ok {
		hosts[addr][label] = 1
	} else {
		keys = append(keys, ipAddr)
		hosts[addr] = make(map[string]int)
		hosts[addr][label] = 1
	}

	return keys, hosts
}
