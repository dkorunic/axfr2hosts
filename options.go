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

	if len(flag.Args()) == 0 {
		fmt.Fprintf(os.Stderr, "Error: no arguments were given\n")
		flag.Usage()
	}

	zones, server := parseZoneArgs(flag.Args())

	if len(zones) == 0 {
		fmt.Fprintf(os.Stderr, "Error: no zones to transfer or parse\n")
		flag.Usage()
	}

	var cidrList []string
	if len(*cidrString) > 0 {
		cidrList = strings.Split(*cidrString, cidrSeparator)
	}

	if *resolverAddress != "" {
		*resolverAddress = normalizeAddrPort(*resolverAddress)
	}

	return zones, server, cidrList
}

// normalizeAddrPort appends the default DNS port to addr unless it already carries
// one.  SplitHostPort beats strings.Contains: a bare IPv6 address contains ':' too,
// so a Contains-based check would leave it portless and unusable as a dial target.
func normalizeAddrPort(addr string) string {
	if _, _, err := net.SplitHostPort(addr); err == nil {
		return addr
	}

	// A bracketed IPv6 literal carrying no port ("[::1]") is a natural thing to
	// type, but JoinHostPort re-brackets any host containing ':' and would emit
	// "[[::1]]:53".  Unwrap it first so the brackets are applied exactly once.
	if inner, ok := strings.CutPrefix(addr, "["); ok {
		if inner, ok := strings.CutSuffix(inner, "]"); ok {
			addr = inner
		}
	}

	return net.JoinHostPort(addr, dnsPort)
}

// parseZoneArgs splits positional arguments into a deduplicated zone list and an
// optional server address.  Arguments prefixed with '@' name the server; the rest
// are zones, trailing dot trimmed so "example.com." and "example.com" collapse.
func parseZoneArgs(args []string) ([]string, string) {
	var server string

	zones := make([]string, 0, len(args))
	zoneMap := make(map[string]struct{}, len(args))

	for _, arg := range args {
		// nameserver argument starts with '@'
		if after, ok := strings.CutPrefix(arg, dnsPrefix); ok {
			server = normalizeAddrPort(after)

			continue
		}

		// TrimRight, not TrimSuffix: "example.com.." would otherwise survive as a
		// distinct zone from "example.com" and defeat the deduplication below.
		arg = strings.TrimRight(arg, endingDot)

		// a bare "." (or "..") trims to nothing; an empty zone name is never
		// useful and parseFlags reports it cleanly as "no zones"
		if arg == "" {
			continue
		}

		if _, ok := zoneMap[arg]; !ok {
			zones = append(zones, arg)
			zoneMap[arg] = struct{}{}
		}
	}

	return zones, server
}

// atLeastOne clamps a user-supplied limit to a minimum of one.
//
// Zero is a plausible thing to type but pathological for both limits it guards:
// a zero-capacity semaphore makes main's send block with no receiver yet started
// ("all goroutines are asleep - deadlock!"), and retry-go reads Attempts(0) as
// "retry until the call succeeds", which hangs forever against a dead nameserver.
func atLeastOne(n uint) uint {
	if n < 1 {
		return 1
	}

	return n
}
