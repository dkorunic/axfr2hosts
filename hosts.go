// SPDX-FileCopyrightText: 2021 Dinko Korunic
// SPDX-License-Identifier: MIT

package main

import (
	"net/netip"
	"strings"
)

// HostEntry contains label, addr and ipAddr for sending through a channel.
type HostEntry struct {
	ipAddr netip.Addr
	label  string
}

// HostMap contains map of addresses and labels.
type HostMap map[netip.Addr]map[string]struct{}

// processHost cleans FQDN and optionally shortens it, sending the resulting HostEntry
// to the hosts channel.
func processHost(label, zone string, ipAddr netip.Addr, hosts chan<- HostEntry) {
	label = strings.TrimSuffix(label, endingDot)
	label = strings.ToLower(label)

	if *stripDomain || *stripUnstrip {
		labelStripped := strings.TrimSuffix(label, endingDot+zone)
		if labelStripped != "" {
			hosts <- HostEntry{label: labelStripped, ipAddr: ipAddr}

			if !*stripUnstrip {
				return
			}
		}
	}

	hosts <- HostEntry{label: label, ipAddr: ipAddr}
}

// writeHostEntries consumes HostEntry items from the hosts channel and updates
// the provided hosts map and keys slice with new label-IP pairs.
func writeHostEntries(hosts <-chan HostEntry, keys *[]netip.Addr, entries HostMap) {
	for x := range hosts {
		label, ipAddr := x.label, x.ipAddr
		if _, ok := entries[ipAddr]; ok {
			entries[ipAddr][label] = struct{}{}
		} else {
			*keys = append(*keys, ipAddr)
			entries[ipAddr] = make(map[string]struct{}, subMapSize)
			entries[ipAddr][label] = struct{}{}
		}
	}
}
