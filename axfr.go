// SPDX-FileCopyrightText: 2021 Dinko Korunic
// SPDX-License-Identifier: MIT

package main

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/avast/retry-go/v5"
	"github.com/miekg/dns"
)

const (
	dialTimeout  = 30 * time.Second
	readTimeout  = 30 * time.Second
	writeTimeout = 30 * time.Second
)

// zoneTransfer prepares and executes AXFR towards a specific DNS server, returning DNS RR slice.
func zoneTransfer(zone, server string) []dns.RR {
	ctx := context.Background()

	zone = dns.Fqdn(zone)

	tr := new(dns.Transfer)
	m := new(dns.Msg)
	m.SetAxfr(zone)

	tr.DialTimeout = dialTimeout
	tr.ReadTimeout = readTimeout
	tr.WriteTimeout = writeTimeout

	records := make([]dns.RR, 0, 1024)

	var c chan *dns.Envelope

	err := retry.New(
		retry.Attempts(*maxRetries),
		retry.Context(ctx),
	).Do(
		func() error {
			var err error

			c, err = tr.In(m, server)
			if err != nil {
				return fmt.Errorf("error performing zone transfer: %w", err)
			}

			return nil
		},
	)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: AXFR failure for zone %q / server %q, will skip over: %v\n", zone, server, err)

		return records
	}

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
