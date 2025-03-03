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
	"os"

	"github.com/monoidic/cidranger/v2"
)

// rangerInit initializes and loads CIDR Ranger and sets doCIDR flag to true if list of networks is non-empty.
func rangerInit(cidrList []string) (cidranger.Ranger[struct{}], bool) {
	var (
		ranger cidranger.Ranger[struct{}]
		doCIDR bool
	)

	// prepare CIDR matching
	if len(cidrList) > 0 {
		doCIDR = true
		ranger = cidranger.NewPCTrieRanger[struct{}]()

		// parse and insert individual networks
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
