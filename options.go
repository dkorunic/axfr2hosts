// SPDX-FileCopyrightText: 2021 Dinko Korunic
// SPDX-License-Identifier: MIT

package main

import (
	"flag"
	"fmt"
	"net"
	"os"
	"runtime"
	"strings"
	"time"
)

const (
	endingDot              = "."
	dnsPort                = "53"
	dnsPrefix              = "@"
	cidrSeparator          = ","
	portSeparator          = ":"
	projectHome            = "https://github.com/dkorunic/axfr2hosts"
	maxTransfersDefault    = 10
	maxRetriesDefault      = 3
	defaultResolverTimeout = 10 * time.Second
)

var (
	greedyCNAME     = flag.Bool("greedy_cname", true, "Resolve out-of-zone CNAME targets")
	ignoreStar      = flag.Bool("ignore_star", true, "Ignore wildcard records")
	cidrString      = flag.String("cidr_list", "", "Use only targets from CIDR whitelist (comma separated list)")
	stripDomain     = flag.Bool("strip_domain", false, "Strip domain name from FQDN hosts entries")
	stripUnstrip    = flag.Bool("strip_unstrip", false, "Keep both FQDN names and domain-stripped names")
	verbose         = flag.Bool("verbose", false, "Enable more verbosity")
	version         = flag.Bool("version", false, "Show version and exit")
	maxTransfers    = flag.Uint("max_transfers", maxTransfersDefault, "Maximum parallel zone transfers")
	maxRetries      = flag.Uint("max_retries", maxRetriesDefault, "Maximum DNS zone transfer attempts")
	cpuProfile      = flag.String("cpu_profile", "", "CPU profile output file")
	memProfile      = flag.String("mem_profile", "", "memory profile output file")
	resolverAddress = flag.String("resolver_address", "", "DNS resolver (DNS recursor) IP address")
	resolverTimeout = flag.Duration("resolver_timeout", defaultResolverTimeout, "DNS queries timeout (should be 2-10s)")
)

// parseFlags parses the command line flags and arguments, returning a slice of zones, the server address,
// and a slice of CIDR blocks.
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

	if *version {
		fmt.Printf("axfr2hosts %v %v%v, built on %v, with %v\n", GitTag, GitCommit, GitDirty,
			BuildTime, runtime.Version())

		os.Exit(0)
	}

	var server string

	zones := make([]string, 0, len(flag.Args()))
	zoneMap := make(map[string]struct{}, len(flag.Args()))

	if len(flag.Args()) == 0 {
		fmt.Fprintf(os.Stderr, "Error: no arguments were given\n")
		flag.Usage()
	}

	for _, arg := range flag.Args() {
		// nameserver starts with '@'
		if after, ok := strings.CutPrefix(arg, dnsPrefix); ok {
			server = after

			// make sure server is in host:port format; use SplitHostPort instead of
			// strings.Contains so that bare IPv6 addresses (which contain ":") are
			// also wrapped correctly by net.JoinHostPort.
			if _, _, err := net.SplitHostPort(server); err != nil {
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

	// check if resolverIP is in server:port format
	if *resolverAddress != "" && !strings.Contains(*resolverAddress, portSeparator) {
		*resolverAddress = net.JoinHostPort(*resolverAddress, dnsPort)
	}

	return zones, server, cidrList
}
