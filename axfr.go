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
	"os"
	"strings"

	"github.com/miekg/dns"
)

// zoneTransfer prepares and executes AXFR towards a specific DNS server, returning DNS RR slice.
func zoneTransfer(zone string, server string) []dns.RR {
	// make sure zone always ends with dot
	if !strings.HasSuffix(zone, endingDot) {
		zone = strings.Join([]string{zone, endingDot}, "")
	}

	// prepare AXFR
	tr := new(dns.Transfer)
	m := new(dns.Msg)
	m.SetAxfr(zone)

	var records []dns.RR

	// execute ingoing AXFR
	c, err := tr.In(m, server)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: AXFR failure for zone %q / server %q, will skip over: %v\n", zone, server, err)

		return records
	}

	// parse messages and fetch RRs if any
	for msg := range c {
		if msg.Error != nil {
			fmt.Fprintf(os.Stderr, "Error: AXFR payload problem for zone %q / server %q, but will try to continue: %v\n",
				zone, server, msg.Error)

			continue
		}

		records = append(records, msg.RR...)
	}

	return records
}
