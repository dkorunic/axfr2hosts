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
	"flag"
	"fmt"
	"net"
	"os"
	"strings"
)

var (
	greedyCNAME = flag.Bool("greedy_cname", true, "Resolve out-of-zone CNAME targets (default true)")
	ignoreStar  = flag.Bool("ignore_star", true, "Ignore wildcard records (default true)")
	cidrString  = flag.String("cidr_list", "", "Use only targets from CIDR whitelist (comma separated list)")
)

const (
	endingDot     = "."
	dnsPort       = "53"
	cidrSeparator = ","
)

func parseFlags() (string, string, []string) {
	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, "Usage: %v [options] zone server[:port]\n", os.Args[0])
		flag.PrintDefaults()
		os.Exit(0)
	}

	flag.Parse()

	tail := flag.Args()
	if len(tail) != 2 {
		flag.Usage()
	}

	// make sure zone always ends with dot
	zoneName := tail[0]
	if !strings.HasSuffix(zoneName, endingDot) {
		zoneName = strings.Join([]string{zoneName, endingDot}, "")
	}

	// make sure server is in server:port format
	server := tail[1]
	if !strings.HasSuffix(server, net.JoinHostPort("", dnsPort)) {
		server = net.JoinHostPort(server, dnsPort)
	}

	// split if non-empty
	var cidrList []string
	if len(*cidrString) > 0 {
		cidrList = strings.Split(*cidrString, cidrSeparator)
	}

	return zoneName, server, cidrList
}
