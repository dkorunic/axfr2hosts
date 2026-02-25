# axfr2hosts

[![GitHub license](https://img.shields.io/github/license/dkorunic/axfr2hosts)](https://github.com/dkorunic/axfr2hosts/blob/master/LICENSE)
[![GitHub release](https://img.shields.io/github/release/dkorunic/axfr2hosts)](https://github.com/dkorunic/axfr2hosts/releases/latest)
[![Go Report Card](https://goreportcard.com/badge/github.com/dkorunic/axfr2hosts)](https://goreportcard.com/report/github.com/dkorunic/axfr2hosts)

![](gopher.png)

## About

axfr2hosts performs a [DNS zone transfer](https://en.wikipedia.org/wiki/DNS_zone_transfer) (AXFR) against one or more zones on a single DNS server and converts the received A, AAAA, and CNAME records into a [hosts file](<https://en.wikipedia.org/wiki/Hosts_(file)>) for local use — for example, when DNS servers are [unreachable](https://blog.cloudflare.com/october-2021-facebook-outage/) or down.

Output entries are sorted by IP address. For each IP address, all associated hostnames are listed in alphabetical order.

axfr2hosts can also read and parse local RFC 1035 zone files (such as BIND 9 zone files) and process A and CNAME records into a hosts file without requiring a zone transfer.

## Requirements

One of the following:

- Permission to perform a full zone transfer (AXFR), typically granted via `allow-transfer` in [BIND 9](https://www.isc.org/bind/) or `allow-axfr-ips` in [PowerDNS](https://www.powerdns.com/),
- Read access to RFC 1035 zone files on the local filesystem.

## Installation

### Manual

Download your preferred release from the [releases page](https://github.com/dkorunic/axfr2hosts/releases) and install it manually, typically to `/usr/local/bin/axfr2hosts`.

### Using go install

```shell
go install github.com/dkorunic/axfr2hosts@latest
```

## Usage

```shell
Usage: ./axfr2hosts [options] zone [zone2 [zone3 ...]] [@server[:port]]
  -cidr_list string
    	Use only targets from CIDR whitelist (comma separated list)
  -cpu_profile string
    	CPU profile output file
  -greedy_cname
    	Resolve out-of-zone CNAME targets (default true)
  -ignore_star
    	Ignore wildcard records (default true)
  -max_retries uint
    	Maximum DNS zone transfer attempts (default 3)
  -max_transfers uint
    	Maximum parallel zone transfers (default 10)
  -mem_profile string
    	memory profile output file
  -resolver_address string
    	DNS resolver (DNS recursor) IP address
  -resolver_timeout duration
    	DNS queries timeout (should be 2-10s) (default 10s)
  -strip_domain
    	Strip domain name from FQDN hosts entries
  -strip_unstrip
    	Keep both FQDN names and domain-stripped names
  -verbose
    	Enable more verbosity
1) If server was not specified, zones will be parsed as RFC 1035 zone files on a local filesystem,
2) We also permit zone=domain argument format to infer a domain name for zone files.

For more information visit project home: https://github.com/dkorunic/axfr2hosts
```

At minimum, a single zone and a single server are required for any meaningful action.

A typical invocation looks like:

```shell
axfr2hosts dkorunic.net pkorunic.net @172.64.33.146
```

### CNAME handling

By default, the tool follows CNAME records even when they point to targets outside the transferred zone and resolves them to one or more IP addresses. This behavior can be disabled with `-greedy_cname=false`, which restricts CNAME resolution to in-zone targets only.

### Wildcard handling

By default, the tool ignores wildcard records (DNS labels containing `*`). To include wildcard records and treat them as ordinary labels, use `-ignore_star=false`.

### Filter results by CIDR

To include only records whose IP addresses fall within specific subnets, use the `-cidr_list` flag with a comma-separated list of CIDR ranges.

### Transferring many zones

When transferring a large number of zones at once, the tool works well with `xargs`. Zones that fail to transfer are skipped, and their errors are printed to stderr:

```shell
xargs axfr2hosts @nameserver < list
```

The maximum number of concurrent zone transfers is controlled by `-max_transfers`, which defaults to `10` — matching BIND 9's default `transfers-out` setting in `named.conf`.

### Strip domain name

Use `-strip_domain` to emit hosts file entries with the domain portion stripped, leaving only the short hostname label. Use `-strip_unstrip` to include both the full FQDN and the stripped short name for each entry. Note that either option produces less meaningful results when processing multiple domains simultaneously.

### Process local zone files

To process RFC 1035 zone files on the local filesystem, omit the `@server` argument. It is recommended to supply the domain name explicitly by appending `=domain` to the zone file path, because the domain inferred from the zone file itself may be incorrect (for instance, when the file lacks a top-level `$ORIGIN` directive, all records are non-FQDN, or records use the `@` macro):

```shell
axfr2hosts dkorunic.net.zone=dkorunic.net
```

### DNS error code responses

If you encounter an error such as `dns: bad xfr rcode: 9`, the following table maps DNS response codes to their meanings:

| Response Code | Return Message | Explanation          |
| :------------ | :------------- | :------------------- |
| 0             | NOERROR        | No error             |
| 1             | FORMERR        | Format error         |
| 2             | SERVFAIL       | Server failure       |
| 3             | NXDOMAIN       | Name does not exist  |
| 4             | NOTIMP         | Not implemented      |
| 5             | REFUSED        | Refused              |
| 6             | YXDOMAIN       | Name exists          |
| 7             | YRRSET         | RRset exists         |
| 8             | NXRRSET        | RRset does not exist |
| 9             | NOTAUTH        | Not authoritative    |
| 10            | NOTZONE        | Name not in zone     |
