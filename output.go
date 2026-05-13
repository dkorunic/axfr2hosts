// SPDX-FileCopyrightText: 2021 Dinko Korunic
// SPDX-License-Identifier: MIT

package main

import (
	"bufio"
	"fmt"
	"net/netip"
	"os"
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

	w := bufio.NewWriter(os.Stdout)
	defer w.Flush()

	t := time.Now().Format(time.RFC1123)
	fmt.Fprintf(w, "# axfr2hosts generated list at %v\n", t)

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

		_, _ = w.WriteString(sb.String())
	}
}
