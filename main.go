// Copyright (C) 2021  Dinko Korunic
//
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU General Public License as published by
// the Free Software Foundation, version 3.
//
// This program is distributed in the hope that it will be useful, but
// WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the GNU
// General Public License for more details.
//
// You should have received a copy of the GNU General Public License
// along with this program. If not, see <http://www.gnu.org/licenses/>.
package main

import (
	"fmt"
	"github.com/yl2chen/cidranger"
	"net"
	"os"
	"strings"
	"time"

	"github.com/miekg/dns"
)

const (
	wildcard = "*"
)

func main() {
	zoneName, server, cidrList := parseFlags()

	zoneRecords := zoneTransfer(zoneName, server)

	ranger, doCIDR := rangerInit(cidrList)

	hosts := make(map[string]map[string]int, len(zoneRecords))

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

			hosts = updateHosts(t.Hdr.Name, t.A.String(), hosts)
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
				// if CIDR matching is true, check if IP is whitelisted
				if doCIDR && ranger != nil {
					ip := net.ParseIP(addr)
					if ip == nil {
						continue
					}

					if c, _ := ranger.Contains(ip); !c {
						continue
					}
				}

				hosts = updateHosts(t.Hdr.Name, addr, hosts)
			}
		// every other RR type is skipped over
		default:
		}
	}

	displayHosts(hosts)
}

// rangerInit initializes and loads CIDR Ranger and sets doCIDR flag to true if list of networks is non-empty.
func rangerInit(cidrList []string) (cidranger.Ranger, bool) {
	var (
		ranger cidranger.Ranger
		doCIDR bool
	)

	// prepare CIDR matching
	if len(cidrList) > 0 {
		doCIDR = true
		ranger = cidranger.NewPCTrieRanger()

		// parse and insert individual networks
		for _, s := range cidrList {
			_, n, err := net.ParseCIDR(s)
			if err != nil {
				fmt.Fprintf(os.Stderr, "Error: problem parsing CIDR: %v\n", err)
			}

			_ = ranger.Insert(cidranger.NewBasicRangerEntry(*n))
		}
	}

	return ranger, doCIDR
}

// zoneTransfer prepares and executes AXFR towards a specific DNS server, returning DNS RR slice.
func zoneTransfer(zone string, server string) []dns.RR {
	// prepare AXFR
	tr := new(dns.Transfer)
	m := new(dns.Msg)
	m.SetAxfr(zone)

	// execute ingoing AXFR
	c, err := tr.In(m, server)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: AXFR failure: %v\n", err)
		os.Exit(1)
	}

	// fetch RRs
	var records []dns.RR

	for msg := range c {
		if msg.Error != nil {
			fmt.Fprintf(os.Stderr, "Error: AXFR payload problem: %v\n", msg.Error)
			os.Exit(1)
		}

		records = append(records, msg.RR...)
	}

	return records
}

// updateHosts updates hosts map with a new label-IP pair, returning updated hosts map.
func updateHosts(label, addr string, results map[string]map[string]int) map[string]map[string]int {
	label = strings.TrimSuffix(label, ".")

	if _, ok := results[addr]; ok {
		results[addr][label] = 1
	} else {
		results[addr] = make(map[string]int)
		results[addr][label] = 1
	}

	return results
}

// displayHosts does a final Unix hosts file output with a list of unique IPs and labels.
func displayHosts(results map[string]map[string]int) {
	var (
		x, last int
		sb      strings.Builder
	)

	t := time.Now().Format(time.RFC1123)
	fmt.Printf("# axfr2hosts generated list at %v\n", t)

	for ip, labelMap := range results {
		sb.Reset()
		sb.WriteString(ip)
		sb.WriteString("\t")

		x = 0
		last = len(labelMap)
		for label, _ := range labelMap {
			sb.WriteString(label)
			x++

			if x != last {
				sb.WriteString(" ")
			}
		}

		sb.WriteString("\n")

		fmt.Print(sb.String())
	}
}
