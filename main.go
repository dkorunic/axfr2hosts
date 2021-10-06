package main

import (
	"fmt"
	"net"
	"os"
	"strings"

	"github.com/miekg/dns"
)

func main() {
	if len(os.Args) != 3 {
		fmt.Printf("%s zone server:port\n", os.Args[0])
		os.Exit(1)
	}

	// make sure we have server:port for destination
	server := os.Args[2]
	if !strings.HasSuffix(server, ":53") {
		server = net.JoinHostPort(server, "53")
	}

	// make sure zone ends with dot
	zone := os.Args[1]
	if !strings.HasSuffix(zone, ".") {
		zone = strings.Join([]string{zone, "."}, "")
	}

	// prepare AXFR
	tr := new(dns.Transfer)
	m := new(dns.Msg)
	m.SetAxfr(zone)

	// execute ingoing AXFR
	c, err := tr.In(m, server)
	if err != nil {
		fmt.Printf("Error: failed to AXFR %v\n", err)
		os.Exit(1)
	}

	// fetch RRs
	var records []dns.RR
	for msg := range c {
		if msg.Error != nil {
			fmt.Printf("Error: %v\n", msg.Error)
			os.Exit(1)
		}
		records = append(records, msg.RR...)
	}

	// resolve and dump A and CNAME in hosts format
	for _, rr := range records {
		switch t := rr.(type) {
		case *dns.A:
			fmt.Printf("%v\t%v\n", t.A, strings.TrimSuffix(t.Hdr.Name, "."))
		case *dns.CNAME:
			addrs, err := net.LookupHost(t.Hdr.Name)
			if err != nil {
				continue
			}
			for _, addr := range addrs {
				fmt.Printf("%v\t%v\n", addr, strings.TrimSuffix(t.Hdr.Name, "."))
			}
		default:
		}
	}
}
