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
	"fmt"
	"net/netip"
	"sort"
	"strings"
	"time"
)

// displayHostEntries does a final Unix hosts file output with a list of unique IPs and labels.
func displayHostEntries(keysAddr []netip.Addr, results HostMap) {
	var (
		x, last int
		sb      strings.Builder
		ipAddr  netip.Addr
	)

	keysHost := make([]string, 0, subMapSize)

	t := time.Now().Format(time.RFC1123)
	fmt.Printf("# axfr2hosts generated list at %v\n", t)

	// sorting by IP
	sort.Slice(keysAddr, func(i, j int) bool {
		return keysAddr[i].Compare(keysAddr[j]) < 0
	})

	for i := range keysAddr {
		ipAddr = keysAddr[i]
		labelMap := results[ipAddr]

		sb.Reset()
		sb.WriteString(ipAddr.String())
		sb.WriteString("\t")

		last = len(labelMap)

		keysHost = keysHost[:0]

		for k := range labelMap {
			keysHost = append(keysHost, k)
		}

		// sorting by hostname
		sort.Strings(keysHost)

		x = 0

		for _, k := range keysHost {
			sb.WriteString(k)

			x++

			if x != last {
				sb.WriteString(" ")
			}
		}

		sb.WriteString("\n")

		fmt.Print(sb.String())
	}
}
