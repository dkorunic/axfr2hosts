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

	"github.com/miekg/dns"
)

// zoneTransfer prepares and executes AXFR towards a specific DNS server, returning DNS RR slice.
func zoneTransfer(zone string, server string) []dns.RR {
	// prepare AXFR
	tr := new(dns.Transfer)
	m := new(dns.Msg)
	m.SetAxfr(zone)

	// execute ingoing AXFR
	c, err := tr.In(m, server)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: AXFR failure: %v\n", err)
		os.Exit(1)
	}

	// fetch RRs
	var records []dns.RR

	for msg := range c {
		if msg.Error != nil {
			fmt.Fprintf(os.Stderr, "Error: AXFR payload problem: %v\n", msg.Error)
			os.Exit(1)
		}

		records = append(records, msg.RR...)
	}

	return records
}
