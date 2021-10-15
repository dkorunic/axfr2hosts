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
	"flag"
	"fmt"
	"github.com/miekg/dns"
	"github.com/yl2chen/cidranger"
	"io/ioutil"
	"net"
	"os"
	"strings"
)

const (
	wildcard          = "*"
	fileZoneSeparator = "="
)

// processRemoteZone is calling zoneTransfer() for AXFR and processRecords() for handling each valid RR.
func processRemoteZone(zone string, server string, doCIDR bool, ranger cidranger.Ranger, keys []net.IP, hosts HostMap) ([]net.IP, HostMap) {
	if *verbose {
		fmt.Fprintf(os.Stderr, "Info: doing AXFR for zone %q / server %q\n", zone, server)
	}

	zoneRecords := zoneTransfer(zone, server)

	return processRecords(zone, doCIDR, ranger, keys, hosts, zoneRecords)
}

// processLocalZone is calling zoneParser() for local zone parse and processRecords() for handling valid RR.
func processLocalZone(zone string, doCIDR bool, ranger cidranger.Ranger, keys []net.IP, hosts HostMap) ([]net.IP,
	HostMap) {
	var domain string

	if strings.Contains(zone, fileZoneSeparator) {
		t := strings.Split(zone, fileZoneSeparator)

		if len(t) == 2 {
			zone = t[0]   // filename
			domain = t[1] // domain

			// make sure domain always ends with dot
			if !strings.HasSuffix(domain, endingDot) {
				domain = strings.Join([]string{domain, endingDot}, "")
			}
		} else {
			fmt.Fprintf(os.Stderr, "Error: invalid file=domain option format: %q\n", zone)
			flag.Usage()
		}
	}

	if *verbose {
		fmt.Fprintf(os.Stderr, "Info: loading and parsing zone %q / domain %q\n", zone, domain)
	}

	zoneRecords := zoneParser(zone, domain)
	if len(zoneRecords) == 0 && domain == "" {
		fmt.Fprintf(os.Stderr, "Error: no detected records in %q file. Try next time with \"%v=domain\"\n",
			zone, zone)
	}

	return processRecords(zone, doCIDR, ranger, keys, hosts, zoneRecords)
}

// processRecords is processing each RR and calling updateHosts() for each valid RR.
func processRecords(zone string, doCIDR bool, ranger cidranger.Ranger, keys []net.IP, hosts HostMap,
	zoneRecords []dns.RR) ([]net.IP, HostMap) {
	for _, rr := range zoneRecords {
		switch t := rr.(type) {
		case *dns.A:
			// ignore wildcards if ignoreStar is used
			if *ignoreStar && strings.Contains(t.Hdr.Name, wildcard) {
				continue
			}
			// if CIDR matching is true, check if IP is whitelisted
			if doCIDR && ranger != nil {
				if c, _ := ranger.Contains(t.A); !c {
					continue
				}
			}

			keys, hosts = updateHosts(t.Hdr.Name, t.A.String(), zone, t.A, keys, hosts)
		case *dns.CNAME:
			// ignore out-of-zone targets if not using greedyCNAME
			if !*greedyCNAME {
				cname, err := net.LookupCNAME(t.Hdr.Name)
				if err != nil {
					continue
				}

				if !strings.HasSuffix(cname, zone) {
					continue
				}
			}

			addrs, err := net.LookupHost(t.Hdr.Name)
			if err != nil {
				continue
			}

			// loop through resolved array
			for _, addr := range addrs {
				ipAddr := net.ParseIP(addr)
				if ipAddr == nil {
					continue
				}

				// if CIDR matching is true, check if IP is whitelisted
				if doCIDR && ranger != nil {
					if c, _ := ranger.Contains(ipAddr); !c {
						continue
					}
				}

				keys, hosts = updateHosts(t.Hdr.Name, addr, zone, ipAddr, keys, hosts)
			}
		// every other RR type is skipped over
		default:
		}
	}

	return keys, hosts
}

// zoneParser is parsing loading zones into memory and parsing them, returning slice of RRs.
func zoneParser(zone, domain string) []dns.RR {
	var records []dns.RR

	// read a whole zone into memory
	z, err := ioutil.ReadFile(zone)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: problem reading zone file: %q: %v\n", zone, err)

		return records
	}

	// initialize RFC 1035 zone parser
	zp := dns.NewZoneParser(strings.NewReader(string(z)), domain, "")

	// fetch all RRs
	for rr, ok := zp.Next(); ok; rr, ok = zp.Next() {
		if err := zp.Err(); err != nil {
			fmt.Fprintf(os.Stderr, "Error: problem parsing zone %q but will try to continue: %v\n", zone, err)

			continue
		}

		records = append(records, rr)
	}

	return records
}
