# axfr2hosts

[![GitHub license](https://img.shields.io/github/license/dkorunic/axfr2hosts)](https://github.com/dkorunic/axfr2hosts/blob/master/LICENSE)
[![GitHub release](https://img.shields.io/github/release/dkorunic/axfr2hosts)](https://github.com/dkorunic/axfr2hosts/releases/latest)
[![codebeat badge](https://codebeat.co/badges/b535ef48-ba10-413e-81f0-dcb5a17e01c4)](https://codebeat.co/projects/github-com-dkorunic-axfr2hosts-main)
[![Go Report Card](https://goreportcard.com/badge/github.com/dkorunic/axfr2hosts)](https://goreportcard.com/report/github.com/dkorunic/axfr2hosts)

![](gopher.png)

## About

axfr2hosts is a tool meant to do a [DNS zone transfer](https://en.wikipedia.org/wiki/DNS_zone_transfer) in a form of AXFR transaction of one or more zones towards a single DNS server and convert received A, AAAA and CNAME records from a DNS responses into a [hosts file](<https://en.wikipedia.org/wiki/Hosts_(file)>) for a local use, for instance when DNS servers are [unreachable](https://blog.cloudflare.com/october-2021-facebook-outage/) and/or down.

By default hosts entries will be sorted its IP as a key and under each entry individual FQDNs will be sorted alphabetically.

If needed, axfr2hosts can also read and parse local RFC 1035 zones (for instance BIND 9 zone files) and process A and CNAME records into a hosts file as described above so that a zone transfer is not needed.

## Requirements

Either of:

- Ability to do a full zone transfer (AXFR), usually permitted with `allow-transfer` in [BIND 9](https://www.isc.org/bind/) or with `allow-axfr-ips` in [PowerDNS](https://www.powerdns.com/),
- Permissions to read RFC 1035 zone files locally.

## Installation

There are two ways of installing axfr2hosts:

### Manual

Download your preferred flavor from [the releases](https://github.com/dkorunic/axfr2hosts/releases) page and install manually, typically to `/usr/local/bin/axfr2hosts`.

### Using go get

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
    	Maximum DNS zone transfer attempts and/or query retries (default 3)
  -max_transfers uint
    	Maximum parallel zone transfers (default 10)
  -mem_profile string
    	memory profile output file
  -resolver_address string
    	DNS resolver (DNS recursor) IP address
  -resolver_timeout duration
    	DNS resolver timeout (default 5s)
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

At minimum, a single zone and a single server are needed for any meaningful action.

Typical use case would be:

```shell
axfr2hosts dkorunic.net pkorunic.net @172.64.33.146
```

### CNAME handling

However the tool by default follows CNAMEs even if they are out-of-zone and resolves to one or more IP addresses if possible and lists all of them. That behaviour can be changed with `-greedy_cname=false` flag.

### Wildcard handling

Also, by default tool lists wildcard (DNS labels containing `*`) like they are ordinary labels and that can be changed with `-ignore_star=true` flag, which simply skips over those records.

### Filter results by CIDR

Finally if there is a need to list only a subset of records matching one or more CIDR ranges, `-cidr_list` flag can be used.

### Many zones transfer

If there is a lot of zones that need to be fetched at once, tool works well with `xargs`. Individual zone errors will be displayed and such zones will be skipped over:

```shell
xargs axfr2hosts @nameserver < list
```

Maximum of concurrent zone transfers is limited by `-max_transfers` flag and defaults to `10`, aligned with BIND 9 default (`transfers-out` in BIND 9 `named.conf`).

### Strip domain name

It is also possible to output hosts file with domain names stripped by using `-strip_domain=true` flag. It is also possible to keep both domain-stripped labels and FQDNs at the same time by using `-strip_unstrip=true` flag. When using many domains at once, either of these options do not make much sense.

### Process local zone files

It is also possible to directly process RFC 1035 zone files on a local filesystem when a nameserver is not been specified. We would typically recommend specifying a domain name manually by suffixing the zone file with `=` and domain name as shown below, as one inferred from a zone can possibly be invalid (due to lack of top-level `$ORIGIN` and/or all records being non-FQDN and/or being suffixed with `@` macro):

```shell
axfr2hosts dkorunic.net.zone=dkorunic.net
```

### DNS error code responses

In case you are wondering what `dns: bad xfr rcode: 9` means, here is a list of DNS response codes:

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
