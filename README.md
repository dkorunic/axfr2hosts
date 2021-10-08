# axfr2hosts

[![GitHub license](https://img.shields.io/github/license/dkorunic/axfr2hosts)](https://github.com/dkorunic/axfr2hosts/blob/master/LICENSE)
[![GitHub release](https://img.shields.io/github/release/dkorunic/axfr2hosts)](https://github.com/dkorunic/axfr2hosts/releases/latest)
[![codebeat badge](https://codebeat.co/badges/b535ef48-ba10-413e-81f0-dcb5a17e01c4)](https://codebeat.co/projects/github-com-dkorunic-axfr2hosts-main)
[![Go Report Card](https://goreportcard.com/badge/github.com/dkorunic/axfr2hosts)](https://goreportcard.com/report/github.com/dkorunic/axfr2hosts)

## About

axfr2hosts is a tool meant to do a [DNS zone transfer](https://en.wikipedia.org/wiki/DNS_zone_transfer) in a form of AXFR transaction of a single zone towards a single DNS server and convert received A and CNAME records from a requested zone into a Unix [hosts file](<https://en.wikipedia.org/wiki/Hosts_(file)>) for a sysops use, for instance when DNS server is [otherwise unreachable](https://blog.cloudflare.com/october-2021-facebook-outage/) and/or down.

## Requirements

Ability to do AXFR, usually permitted with `allow-transfer` in Bind 9 or with `allow-axfr-ips` in PowerDNS.

## Installation

There are two ways of installing axfr2hosts:

### Manual

Download your preferred flavor from [the releases](https://github.com/dkorunic/axfr2hosts/releases) page and install manually, typically to `/usr/local/bin/axfr2hosts`.

### Using go get

```shell
go get github.com/dkorunic/axfr2hosts
```

## Usage

```shell
Usage: ./axfr2hosts [options] zone [zone2 [zone3 ...]] @server[:port]
  -cidr_list string
    	Use only targets from CIDR whitelist (comma separated list)
  -greedy_cname
    	Resolve out-of-zone CNAME targets (default true)
  -ignore_star
    	Ignore wildcard records (default true)
```

Typical use case would be:

```shell
axfr2hosts dkorunic.net pkorunic.net @172.64.33.146
```

### CNAME handling

However the tool by default follows CNAMEs even if they are out-of-zone and resolves to one or more IP addresses if possible and lists all of them. That behaviour can be changed with `-greedy_cname=false` option.

### Wildcard handling

Also, by default tool lists wildcard (DNS labels containing `*`) like they are ordinary labels and that can be changed with `-ignore_star=true` option, which simply skips over those records.

### Filter results by CIDR

Finally if there is a need to list only a subset of records matching one or more CIDR ranges, option `-cidr_list` can be used.

### Many zones transfer

If there is a lot of zones that need to be fetched at once, tool works well with `xargs`. Individual zone errors will be displayed and such zones will be skipped over:

```shell
xargs axfr2hosts @nameserver < list
```
