// SPDX-FileCopyrightText: 2021 Dinko Korunic
// SPDX-License-Identifier: MIT

package main

import (
	"fmt"
	"net/netip"
	"os"

	"github.com/monoidic/cidranger/v2"
)

// rangerInit initializes and loads CIDR Ranger and sets doCIDR flag to true if list of networks is non-empty.
func rangerInit(cidrList []string) (cidranger.Ranger[struct{}], bool) {
	var (
		ranger cidranger.Ranger[struct{}]
		doCIDR bool
	)

	if len(cidrList) > 0 {
		doCIDR = true
		ranger = cidranger.NewPCTrieRanger[struct{}]()

		for _, s := range cidrList {
			n, err := netip.ParsePrefix(s)
			if err != nil {
				fmt.Fprintf(os.Stderr, "Error: problem parsing CIDR: %v\n", err)

				continue
			}

			_ = ranger.Insert(n, struct{}{})
		}
	}

	return ranger, doCIDR
}
