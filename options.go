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
	"flag"
	"fmt"
	"net"
	"os"
	"strings"
)

var (
	greedyCNAME  = flag.Bool("greedy_cname", true, "Resolve out-of-zone CNAME targets")
	ignoreStar   = flag.Bool("ignore_star", true, "Ignore wildcard records")
	cidrString   = flag.String("cidr_list", "", "Use only targets from CIDR whitelist (comma separated list)")
	stripDomain  = flag.Bool("strip_domain", false, "Strip domain name from FQDN hosts entries")
	stripUnstrip = flag.Bool("strip_unstrip", false, "Keep both FQDN names and domain-stripped names")
	verbose      = flag.Bool("verbose", false, "Enable more verbosity")
	maxTransfers = flag.Uint("max_transfers", 10, "Maximum parallel zone transfers")
	maxRetries   = flag.Uint("max_retries", 3, "Maximum zone transfer attempts")
)

const (
	endingDot     = "."
	dnsPort       = "53"
	dnsPrefix     = "@"
	cidrSeparator = ","
	portSeparator = ":"
	projectHome   = "https://github.com/dkorunic/axfr2hosts"
)

func parseFlags() ([]string, string, []string) {
	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, "Usage: %v [options] zone [zone2 [zone3 ...]] [@server[:port]]\n", os.Args[0])
		flag.PrintDefaults()
		fmt.Fprintf(os.Stderr, "1) If server was not specified, zones will be parsed as RFC 1035 zone files on a local filesystem,\n")
		fmt.Fprintf(os.Stderr, "2) We also permit zone=domain argument format to infer a domain name for zone files.\n")
		fmt.Fprintf(os.Stderr, "\nFor more information visit project home: %v\n", projectHome)
		os.Exit(0)
	}

	flag.Parse()

	var server string

	zones := make([]string, 0, len(flag.Args()))
	zoneMap := make(map[string]struct{}, len(flag.Args()))

	if len(flag.Args()) == 0 {
		fmt.Fprintf(os.Stderr, "Error: no arguments were given\n")
		flag.Usage()
	}

	for _, arg := range flag.Args() {
		// nameserver starts with '@'
		if strings.HasPrefix(arg, dnsPrefix) {
			server = strings.TrimPrefix(arg, dnsPrefix)

			// make sure server is in server:port format
			if !strings.Contains(server, portSeparator) {
				server = net.JoinHostPort(server, dnsPort)
			}

			continue
		}

		// otherwise it is a zone name; make sure to strip ending dot
		arg = strings.TrimSuffix(arg, endingDot)

		// add zone only if unique
		if _, ok := zoneMap[arg]; !ok {
			zones = append(zones, arg)
			zoneMap[arg] = struct{}{}
		}
	}

	// check if zones are empty
	if len(zones) == 0 {
		fmt.Fprintf(os.Stderr, "Error: no zones to transfer or parse\n")
		flag.Usage()
	}

	// split if non-empty
	var cidrList []string
	if len(*cidrString) > 0 {
		cidrList = strings.Split(*cidrString, cidrSeparator)
	}

	return zones, server, cidrList
}
