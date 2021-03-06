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
	"net"
)

const (
	defaultMapSize = 2048
)

func main() {
	zones, server, cidrList := parseFlags()

	ranger, doCIDR := rangerInit(cidrList)
	hosts := make(HostMap, defaultMapSize)
	keys := make([]net.IP, 0, defaultMapSize)

	for _, zone := range zones {
		if server == "" {
			// there is no remote server, so assume zones are local Bind9 files
			keys, hosts = processLocalZone(zone, doCIDR, ranger, keys, hosts)
		} else {
			// otherwise assume remote AXFR-able zones
			keys, hosts = processRemoteZone(zone, server, doCIDR, ranger, keys, hosts)
		}
	}

	displayHosts(keys, hosts)
}
