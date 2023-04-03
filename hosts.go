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

// processHost cleans FQDN and optionally shortens it, calling low-level writeHostEntries and returning updated hosts
// map and keys slice.
func processHost(label, zone string, ipAddr netip.Addr, hosts chan<- HostEntry) {
	label = strings.TrimSuffix(label, endingDot)
	label = strings.ToLower(label)

	// strip domain if needed
	if *stripDomain || *stripUnstrip {
		labelStripped := strings.TrimSuffix(label, strings.Join([]string{endingDot, zone}, ""))
		if labelStripped != "" {
			hosts <- HostEntry{label: labelStripped, ipAddr: ipAddr}

			if !*stripUnstrip {
				return
			}
		}
	}

	hosts <- HostEntry{label: label, ipAddr: ipAddr}
}

// writeHostEntries updates hosts map with a new label-IP pair, returning updated hosts map and keys slice.
func writeHostEntries(hosts <-chan HostEntry, keys *[]netip.Addr, entries HostMap) {
	for x := range hosts {
		label, ipAddr := x.label, x.ipAddr
		if _, ok := entries[ipAddr]; ok {
			entries[ipAddr][label] = struct{}{}
		} else {
			*keys = append(*keys, ipAddr)
			entries[ipAddr] = make(map[string]struct{})
			entries[ipAddr][label] = struct{}{}
		}
	}
}
