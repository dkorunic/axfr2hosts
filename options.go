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
