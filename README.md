# axfr2hosts

[![GitHub license](https://img.shields.io/github/license/dkorunic/axfr2hosts.svg)](https://github.com/dkorunic/axfr2hosts/blob/master/LICENSE.txt)
[![GitHub release](https://img.shields.io/github/release/dkorunic/axfr2hosts.svg)](https://github.com/dkorunic/axfr2hosts/releases/latest)

## About

axfr2hosts is a tool meant to do a [DNS zone transfer](https://en.wikipedia.org/wiki/DNS_zone_transfer) in a form of AXFR transaction of a single zone towards a single DNS server and convert received A and CNAME records from a requested zone into a Unix [hosts file](https://en.wikipedia.org/wiki/Hosts_(file)) for a sysops use, for instance when DNS server is [otherwise unreachable](https://blog.cloudflare.com/october-2021-facebook-outage/) and/or down.

## Requirements

Ability to do AXFR, usually permitted with `allow-transfer` in Bind 9 or with `allow-axfr-ips` in PowerDNS.

## Installation

There are two ways of installing axfr2hosts


### Manual

Download your preferred flavor from [the releases](https://github.com/dkorunic/axfr2hosts/releases) page and install manually, typically to `/usr/local/bin/axfr2hosts`.

### Using go get

```shell
go get github.com/dkorunic/axfr2hosts
```

## Usage

```shell
Usage: ./axfr2hosts [options] zone server[:port]
  -cidr_list string
    	Use only targets from CIDR whitelist (comma separated list)
  -greedy_cname
    	Resolve out-of-zone CNAME targets (default true) (default true)
  -ignore_star
    	Ignore wildcard records (default true) (default true)
```

Typical use case would be:

```shell
axfr2hosts dkorunic.net 172.64.33.146
```

However the tool by default follows CNAMEs even if they are out-of-zone and resolves to one or more IP addresses if possible and lists all of them. That behaviour can be changed with `-greedy_cname=false` option.

Also, by default tool lists wildcard (DNS labels containing `*`) like they are ordinary labels and that can be changed with `-ignore_star=true` option, which simply skips over those records.

Finally if there is a need to list only a subset of records matching one or more CIDR ranges, option `-cidr_list` can be used.