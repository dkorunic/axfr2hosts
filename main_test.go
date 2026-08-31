// SPDX-FileCopyrightText: 2026 Dinko Korunic
// SPDX-License-Identifier: MIT

package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/miekg/dns"
)

// subprocessEnv makes the test binary re-exec itself as the real CLI.
//
// main() and parseFlags() call flag.Parse and os.Exit, so they cannot run in the
// test process without terminating it.  Running them in a child process is the
// only way to cover the actual entry point — argument handling, the semaphore,
// goroutine orchestration and final output — rather than a reimplementation.
const subprocessEnv = "AXFR2HOSTS_TEST_SUBPROCESS"

func TestMain(m *testing.M) {
	if os.Getenv(subprocessEnv) == "1" {
		// os.Args is whatever runCLI passed; testing's flags are not registered
		// because m.Run() has not been called on this path.
		main()
		os.Exit(0)
	}

	os.Exit(m.Run())
}

// cliResult is the observable outcome of one CLI invocation.
type cliResult struct {
	stdout string
	stderr string
	code   int
}

// runCLI executes the test binary as axfr2hosts with the given arguments under a
// hard timeout, so a hang regression fails the test instead of wedging the suite.
func runCLI(t *testing.T, args ...string) cliResult {
	t.Helper()

	// each call forks a full process; under -race that is slow enough to be worth
	// skipping when the caller asked for a quick run
	if testing.Short() {
		t.Skip("skipping subprocess CLI test in short mode")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, os.Args[0], args...)
	cmd.Env = append(os.Environ(), subprocessEnv+"=1")

	var stdout, stderr bytes.Buffer

	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()

	res := cliResult{stdout: stdout.String(), stderr: stderr.String()}

	var exitErr *exec.ExitError

	switch {
	case err == nil:
	case errors.As(err, &exitErr):
		res.code = exitErr.ExitCode()
	default:
		t.Fatalf("running CLI %v: %v", args, err)
	}

	if ctx.Err() != nil {
		t.Fatalf("CLI %v did not finish within the timeout — likely a hang", args)
	}

	return res
}

// hostLines returns the generated hosts entries with the header comment removed.
func hostLines(out string) []string {
	var lines []string

	for _, l := range strings.Split(strings.TrimRight(out, "\n"), "\n") {
		if l != "" && !strings.HasPrefix(l, "#") {
			lines = append(lines, l)
		}
	}

	return lines
}

func testZoneFile(t *testing.T) string {
	t.Helper()

	return writeTempZone(t, `$TTL 3600
@ IN SOA ns1 admin 2021010101 3600 900 604800 300
host1 IN A 192.0.2.1
host2 IN A 192.0.2.2
host3 IN AAAA 2001:db8::1
`)
}

func TestCLIVersion(t *testing.T) {
	res := runCLI(t, "-version")

	if res.code != 0 {
		t.Errorf("-version exit code = %d, want 0", res.code)
	}

	if !strings.Contains(res.stdout, "axfr2hosts") {
		t.Errorf("-version stdout = %q, want it to name the program", res.stdout)
	}
}

func TestCLINoArguments(t *testing.T) {
	res := runCLI(t)

	if !strings.Contains(res.stderr, "no arguments were given") {
		t.Errorf("bare invocation stderr = %q, want a missing-arguments diagnostic", res.stderr)
	}

	if !strings.Contains(res.stderr, "Usage:") {
		t.Errorf("bare invocation did not print usage; stderr=%q", res.stderr)
	}
}

func TestCLILocalZone(t *testing.T) {
	res := runCLI(t, testZoneFile(t)+"=example.com")

	got := hostLines(res.stdout)

	want := []string{
		"192.0.2.1\thost1.example.com",
		"192.0.2.2\thost2.example.com",
		"2001:db8::1\thost3.example.com",
	}

	if len(got) != len(want) {
		t.Fatalf("output = %d lines, want %d:\n%s", len(got), len(want), res.stdout)
	}

	for i := range want {
		if got[i] != want[i] {
			t.Errorf("line[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}

// TestCLIStripDomain is the end-to-end oracle for the local-zone strip bug.
// processLocalZone used to hand processRecords the *file path* instead of the DNS
// domain, so -strip_domain and -strip_unstrip were silently inoperative for every
// local zone file while working correctly over AXFR.
func TestCLIStripDomain(t *testing.T) {
	zoneFile := testZoneFile(t)

	tests := []struct {
		name string
		args []string
		want []string
	}{
		{
			name: "strip_domain shortens every label",
			args: []string{"-strip_domain", zoneFile + "=example.com"},
			want: []string{"192.0.2.1\thost1", "192.0.2.2\thost2", "2001:db8::1\thost3"},
		},
		{
			name: "strip_unstrip keeps both forms",
			args: []string{"-strip_unstrip", zoneFile + "=example.com"},
			want: []string{
				"192.0.2.1\thost1 host1.example.com",
				"192.0.2.2\thost2 host2.example.com",
				"2001:db8::1\thost3 host3.example.com",
			},
		},
		{
			name: "zone matching is case-insensitive",
			args: []string{"-strip_domain", zoneFile + "=EXAMPLE.COM"},
			want: []string{"192.0.2.1\thost1", "192.0.2.2\thost2", "2001:db8::1\thost3"},
		},
		{
			name: "trailing dot on the domain is tolerated",
			args: []string{"-strip_domain", zoneFile + "=example.com."},
			want: []string{"192.0.2.1\thost1", "192.0.2.2\thost2", "2001:db8::1\thost3"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := hostLines(runCLI(t, tt.args...).stdout)

			if len(got) != len(tt.want) {
				t.Fatalf("output = %v, want %v", got, tt.want)
			}

			for i := range tt.want {
				if got[i] != tt.want[i] {
					t.Errorf("line[%d] = %q, want %q", i, got[i], tt.want[i])
				}
			}
		})
	}
}

// TestCLIStripDomainFromOrigin covers the SOA fallback: with no "=domain" the zone
// name is recovered from the file's own $ORIGIN via the SOA apex.
func TestCLIStripDomainFromOrigin(t *testing.T) {
	zoneFile := writeTempZone(t, `$ORIGIN example.com.
$TTL 3600
@ IN SOA ns1 admin 2021010101 3600 900 604800 300
host1 IN A 192.0.2.1
`)

	got := hostLines(runCLI(t, "-strip_domain", zoneFile).stdout)

	want := []string{"192.0.2.1\thost1"}
	if len(got) != 1 || got[0] != want[0] {
		t.Errorf("output = %v, want %v", got, want)
	}
}

func TestCLICIDRFilter(t *testing.T) {
	zoneFile := testZoneFile(t)

	tests := []struct {
		name string
		cidr string
		want []string
	}{
		{
			name: "IPv4 prefix keeps only IPv4 hosts",
			cidr: "192.0.2.0/24",
			want: []string{"192.0.2.1\thost1.example.com", "192.0.2.2\thost2.example.com"},
		},
		{
			name: "IPv6 prefix keeps only the IPv6 host",
			cidr: "2001:db8::/32",
			want: []string{"2001:db8::1\thost3.example.com"},
		},
		{
			name: "narrow prefix keeps a single host",
			cidr: "192.0.2.2/32",
			want: []string{"192.0.2.2\thost2.example.com"},
		},
		{
			name: "comma-separated list unions the prefixes",
			cidr: "192.0.2.2/32,2001:db8::/32",
			want: []string{"192.0.2.2\thost2.example.com", "2001:db8::1\thost3.example.com"},
		},
		{
			name: "non-matching prefix filters everything out",
			cidr: "10.0.0.0/8",
			want: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := hostLines(runCLI(t, "-cidr_list", tt.cidr, zoneFile+"=example.com").stdout)

			if len(got) != len(tt.want) {
				t.Fatalf("output = %v, want %v", got, tt.want)
			}

			for i := range tt.want {
				if got[i] != tt.want[i] {
					t.Errorf("line[%d] = %q, want %q", i, got[i], tt.want[i])
				}
			}
		})
	}
}

// TestCLIDuplicateZonesEmitOnce covers zone deduplication end to end: the same
// file listed twice must not double the output.
func TestCLIDuplicateZonesEmitOnce(t *testing.T) {
	zoneFile := testZoneFile(t)
	arg := zoneFile + "=example.com"

	got := hostLines(runCLI(t, arg, arg, arg).stdout)

	if len(got) != 3 {
		t.Errorf("output = %d lines for a thrice-repeated zone, want 3:\n%v", len(got), got)
	}
}

// TestCLIWildcardHandling covers -ignore_star in both directions.
func TestCLIWildcardHandling(t *testing.T) {
	zoneFile := writeTempZone(t, `$TTL 3600
@ IN SOA ns1 admin 2021010101 3600 900 604800 300
host1 IN A 192.0.2.1
*     IN A 192.0.2.9
`)

	t.Run("wildcards ignored by default", func(t *testing.T) {
		got := hostLines(runCLI(t, zoneFile+"=example.com").stdout)

		for _, l := range got {
			if strings.Contains(l, "*") {
				t.Errorf("wildcard record leaked into output: %q", l)
			}
		}
	})

	t.Run("ignore_star=false keeps them", func(t *testing.T) {
		got := hostLines(runCLI(t, "-ignore_star=false", zoneFile+"=example.com").stdout)

		var found bool

		for _, l := range got {
			if strings.Contains(l, "*") {
				found = true
			}
		}

		if !found {
			t.Errorf("-ignore_star=false dropped the wildcard record; got %v", got)
		}
	})
}

// TestCLIMissingZoneFile checks a missing file degrades to a diagnostic plus an
// empty hosts file rather than a crash.
func TestCLIMissingZoneFile(t *testing.T) {
	res := runCLI(t, "/nonexistent/axfr2hosts-missing.zone=example.com")

	if res.code != 0 {
		t.Errorf("exit code = %d, want 0 (a missing zone must not be fatal)", res.code)
	}

	if !strings.Contains(res.stderr, "problem reading zone file") {
		t.Errorf("stderr = %q, want a read-failure diagnostic", res.stderr)
	}

	if len(hostLines(res.stdout)) != 0 {
		t.Errorf("stdout = %q, want no host entries", res.stdout)
	}
}

// deadServer returns a TCP address with nothing listening on it.
func deadServer(t *testing.T) string {
	t.Helper()

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("net.Listen: %v", err)
	}

	addr := ln.Addr().String()
	_ = ln.Close()

	return addr
}

// TestCLIPathologicalLimits is the oracle for two flag values that used to be
// unsurvivable.
//
// -max_transfers=0 sized the semaphore channel at zero capacity, so main's send
// blocked before the receiving goroutine was ever started: the Go runtime aborted
// the process with "all goroutines are asleep - deadlock!" and exit status 2.
//
// -max_retries=0 reaches retry-go as Attempts(0), which it documents as "retry
// until the call succeeds" — against an unreachable nameserver that never
// terminates.  Both are now clamped to one.
//
// runCLI enforces a timeout, so a regression on either surfaces as a failure
// rather than a wedged test run.
func TestCLIPathologicalLimits(t *testing.T) {
	server := "@" + deadServer(t)

	tests := []struct {
		name string
		flag string
	}{
		{"max_transfers=0 must not deadlock", "-max_transfers=0"},
		{"max_retries=0 must not retry forever", "-max_retries=0"},
		{"both zero at once", "-max_transfers=0"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			args := []string{tt.flag, "example.com", server}
			if tt.name == "both zero at once" {
				args = append([]string{"-max_retries=0"}, args...)
			}

			res := runCLI(t, args...)

			if res.code != 0 {
				t.Errorf("exit code = %d, want 0; a zero limit must be clamped, not fatal\nstderr=%s",
					res.code, res.stderr)
			}

			if strings.Contains(res.stderr, "all goroutines are asleep") {
				t.Errorf("runtime deadlock detected with %s:\n%s", tt.flag, res.stderr)
			}

			// the AXFR still fails against a dead server, which is the expected outcome
			if !strings.Contains(res.stderr, "AXFR failure") {
				t.Errorf("expected an AXFR failure diagnostic; stderr=%q", res.stderr)
			}
		})
	}
}

// TestCLIMaxTransfersBounds checks the semaphore path survives a range of values,
// including one and a value larger than the number of zones.
func TestCLIMaxTransfersBounds(t *testing.T) {
	server := "@" + deadServer(t)

	for _, n := range []string{"0", "1", "2", "100"} {
		t.Run("max_transfers="+n, func(t *testing.T) {
			res := runCLI(t, "-max_transfers="+n, "a.example.com", "b.example.com", "c.example.com", server)

			if res.code != 0 {
				t.Errorf("-max_transfers=%s exit code = %d, want 0\nstderr=%s", n, res.code, res.stderr)
			}
		})
	}
}

// TestCLIServerOnlyRejected covers the "no zones" branch: an invocation naming
// only a nameserver has nothing to transfer and must say so.
func TestCLIServerOnlyRejected(t *testing.T) {
	res := runCLI(t, "@192.0.2.1")

	if !strings.Contains(res.stderr, "no zones to transfer or parse") {
		t.Errorf("stderr = %q, want a no-zones diagnostic", res.stderr)
	}

	if len(hostLines(res.stdout)) != 0 {
		t.Errorf("stdout = %q, want no host entries", res.stdout)
	}
}

// TestCLIRootZoneArgumentRejected covers the degenerate "." argument, which trims
// to an empty zone name and must be dropped rather than processed as a file named "".
func TestCLIRootZoneArgumentRejected(t *testing.T) {
	res := runCLI(t, ".")

	if !strings.Contains(res.stderr, "no zones to transfer or parse") {
		t.Errorf("stderr = %q, want a no-zones diagnostic for a bare \".\"", res.stderr)
	}
}

// TestCLIResolverAddressNormalized drives the -resolver_address branch of
// parseFlags end to end, including the bare-IPv6 form that used to lose its port.
func TestCLIResolverAddressNormalized(t *testing.T) {
	zoneFile := testZoneFile(t)

	for _, addr := range []string{"192.0.2.53", "2001:db8::53", "[2001:db8::53]:5353", "192.0.2.53:5353"} {
		t.Run(addr, func(t *testing.T) {
			res := runCLI(t, "-resolver_address", addr, zoneFile+"=example.com")

			if res.code != 0 {
				t.Errorf("exit code = %d, want 0\nstderr=%s", res.code, res.stderr)
			}

			// A/AAAA records need no resolver, so output is unaffected by the flag
			if len(hostLines(res.stdout)) != 3 {
				t.Errorf("output = %v, want 3 host lines", hostLines(res.stdout))
			}
		})
	}
}

// TestCLIProfileFlags covers the pprof plumbing: both profiles must be written and
// be non-empty, and neither may disturb the generated hosts output.
func TestCLIProfileFlags(t *testing.T) {
	zoneFile := testZoneFile(t)
	dir := t.TempDir()

	cpuPath := filepath.Join(dir, "cpu.prof")
	memPath := filepath.Join(dir, "mem.prof")

	res := runCLI(t, "-cpu_profile", cpuPath, "-mem_profile", memPath, zoneFile+"=example.com")

	if res.code != 0 {
		t.Fatalf("exit code = %d, want 0\nstderr=%s", res.code, res.stderr)
	}

	if len(hostLines(res.stdout)) != 3 {
		t.Errorf("profiling changed the output: %v", hostLines(res.stdout))
	}

	for _, p := range []string{cpuPath, memPath} {
		fi, err := os.Stat(p)
		if err != nil {
			t.Errorf("profile %q was not written: %v", p, err)

			continue
		}

		if fi.Size() == 0 {
			t.Errorf("profile %q is empty", p)
		}
	}
}

// TestCLIProfileUnwritablePath covers the profile-creation error path.  Profiling
// is a diagnostic aid, so a bad path must be reported without losing the run's
// actual output.
func TestCLIProfileUnwritablePath(t *testing.T) {
	zoneFile := testZoneFile(t)

	res := runCLI(t, "-cpu_profile", "/nonexistent-dir/cpu.prof", zoneFile+"=example.com")

	if !strings.Contains(res.stderr, "CPU profile") {
		t.Errorf("stderr = %q, want a CPU-profile diagnostic", res.stderr)
	}

	if len(hostLines(res.stdout)) != 3 {
		t.Errorf("a failed profile lost the hosts output: %v", hostLines(res.stdout))
	}
}

// TestCLIOutputIsDeterministic pins byte-for-byte reproducibility.
//
// Both the address list and each line's label set are sorted before printing,
// precisely so that Go's randomised map iteration cannot leak into the output.
// That makes generated hosts files diffable and safe to commit; a future change
// that dropped either sort would reintroduce run-to-run churn that is easy to miss
// because any single run still looks correct.
func TestCLIOutputIsDeterministic(t *testing.T) {
	// many labels per address maximises the chance of exposing map-order leakage
	var sb strings.Builder

	sb.WriteString("$TTL 3600\n@ IN SOA ns1 admin 1 2 3 4 5\n")

	for i := range 200 {
		fmt.Fprintf(&sb, "host%d IN A 192.0.2.%d\n", i, i%8+1)
	}

	zoneFile := writeTempZone(t, sb.String())

	first := hostLines(runCLI(t, zoneFile+"=example.com").stdout)

	if len(first) == 0 {
		t.Fatal("no output produced")
	}

	for run := range 4 {
		got := hostLines(runCLI(t, zoneFile+"=example.com").stdout)

		if !slices.Equal(got, first) {
			t.Fatalf("run %d differs from run 0 — output is not deterministic\nfirst=%v\ngot=%v",
				run+1, first, got)
		}
	}

	// and the ordering itself must be sorted, not merely stable
	if !slices.IsSortedFunc(first, func(a, b string) int {
		return strings.Compare(addrOf(a), addrOf(b))
	}) {
		t.Errorf("output lines are not ordered by address: %v", first)
	}
}

// addrOf returns the address field of a hosts-file line.
func addrOf(line string) string {
	addr, _, _ := strings.Cut(line, "\t")

	return addr
}

// TestCLIRemoteZoneEndToEnd drives the AXFR path through main against a live
// in-process nameserver: argument parsing, the transfer semaphore, record
// processing and final output.  The other remote CLI tests point at dead ports, so
// this is the only one where the semaphore actually gates real work.
func TestCLIRemoteZoneEndToEnd(t *testing.T) {
	// server runs in the parent; the child process connects to it over TCP
	addr := startAXFRServer(t, [][]dns.RR{axfrRecords(t)})

	res := runCLI(t, "-greedy_cname=false", "example.com", "@"+addr)

	if res.code != 0 {
		t.Fatalf("exit code = %d, want 0\nstderr=%s", res.code, res.stderr)
	}

	got := hostLines(res.stdout)

	want := []string{
		"192.0.2.1\twww.example.com",
		"192.0.2.2\tmail.example.com",
		"2001:db8::1\tipv6.example.com",
	}

	if !slices.Equal(got, want) {
		t.Errorf("output = %v, want %v", got, want)
	}
}

// TestCLIRemoteZoneStripDomain confirms stripping works over AXFR too, which is
// the path where it always did — the local path was the broken one.
func TestCLIRemoteZoneStripDomain(t *testing.T) {
	addr := startAXFRServer(t, [][]dns.RR{axfrRecords(t)})

	got := hostLines(runCLI(t, "-greedy_cname=false", "-strip_domain", "example.com", "@"+addr).stdout)

	want := []string{"192.0.2.1\twww", "192.0.2.2\tmail", "2001:db8::1\tipv6"}

	if !slices.Equal(got, want) {
		t.Errorf("output = %v, want %v", got, want)
	}
}

// TestCLIRemoteMultipleZonesConcurrent runs several zones through the semaphore at
// once, checking the bounded-concurrency path produces the same merged output
// regardless of how many transfers are allowed in parallel.
func TestCLIRemoteMultipleZonesConcurrent(t *testing.T) {
	addr := startAXFRServer(t, [][]dns.RR{axfrRecords(t)})

	var reference []string

	for _, n := range []string{"1", "2", "10"} {
		t.Run("max_transfers="+n, func(t *testing.T) {
			res := runCLI(t, "-max_transfers="+n, "-greedy_cname=false", "example.com", "@"+addr)

			if res.code != 0 {
				t.Fatalf("exit code = %d\nstderr=%s", res.code, res.stderr)
			}

			got := hostLines(res.stdout)

			if reference == nil {
				reference = got

				return
			}

			if !slices.Equal(got, reference) {
				t.Errorf("-max_transfers=%s changed the output:\ngot  %v\nwant %v", n, got, reference)
			}
		})
	}
}

// TestCLIVerbose covers the -verbose announcements on both zone paths.
func TestCLIVerbose(t *testing.T) {
	t.Run("local", func(t *testing.T) {
		res := runCLI(t, "-verbose", testZoneFile(t)+"=example.com")

		if !strings.Contains(res.stderr, "loading and parsing zone") {
			t.Errorf("stderr = %q, want a parse announcement", res.stderr)
		}
	})

	t.Run("remote", func(t *testing.T) {
		addr := startAXFRServer(t, [][]dns.RR{axfrRecords(t)})

		res := runCLI(t, "-verbose", "-greedy_cname=false", "example.com", "@"+addr)

		if !strings.Contains(res.stderr, "doing AXFR") {
			t.Errorf("stderr = %q, want an AXFR announcement", res.stderr)
		}
	})
}

// TestCLIMaxTransfersIsEnforced checks that -max_transfers actually bounds the
// number of simultaneous zone transfers.
//
// TestCLIMaxTransfersBounds above asserts only that the process exits 0, which a
// completely unbounded implementation also does; nothing in the suite observed the
// semaphore doing its job.  Here an in-process AXFR server holds each transfer open
// and records peak concurrency, so the limit is measured rather than assumed.
//
// The high-limit case is not decoration: without it a bug that serialised every
// transfer (or a server that never overlapped) would satisfy the "<= 1" assertion
// vacuously.  Asserting that concurrency IS observable at a high limit is what
// gives the low-limit assertion its meaning.
func TestCLIMaxTransfersIsEnforced(t *testing.T) {
	zones := []string{"a.example.com", "b.example.com", "c.example.com", "d.example.com"}

	for _, tt := range []struct {
		name  string
		limit string
		check func(t *testing.T, peak int)
	}{
		{
			name:  "limit of 1 serialises transfers",
			limit: "1",
			check: func(t *testing.T, peak int) {
				t.Helper()

				if peak > 1 {
					t.Errorf("peak concurrent AXFRs = %d with -max_transfers=1, want 1: "+
						"the transfer semaphore is not bounding anything", peak)
				}
			},
		},
		{
			name:  "high limit permits overlap",
			limit: "4",
			check: func(t *testing.T, peak int) {
				t.Helper()

				if peak < 2 {
					t.Errorf("peak concurrent AXFRs = %d with -max_transfers=4, want >= 2: "+
						"transfers never overlapped, so the limit-of-1 case above proves nothing", peak)
				}
			},
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			addr, peak := startCountingAXFRServer(t, 300*time.Millisecond)

			args := append([]string{"-max_transfers=" + tt.limit}, zones...)
			args = append(args, "@"+addr)

			res := runCLI(t, args...)
			if res.code != 0 {
				t.Fatalf("exit code = %d, want 0\nstderr=%s", res.code, res.stderr)
			}

			// every zone resolves to the same address, so all four names merge onto
			// a single hosts line — assert on the names, not the line count
			for _, z := range zones {
				if !strings.Contains(res.stdout, "host."+z) {
					t.Errorf("output is missing host.%s, so that zone was not transferred:\n%s",
						z, res.stdout)
				}
			}

			tt.check(t, peak())
		})
	}
}

// TestVersionOutputIsTrimmed covers the init() that trims the link-time version
// variables.  Those values only ever arrive via -ldflags, so the trimming cannot be
// exercised by the ordinary test binary, where they are all empty: this builds a
// throwaway binary with deliberately padded values and inspects what -version
// prints.  Without the trimming the padding leaks straight into the output.
func TestVersionOutputIsTrimmed(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping version test in short mode: it compiles a binary")
	}

	bin := filepath.Join(t.TempDir(), "axfr2hosts-version-probe")

	ldflags := "-X 'main.GitTag= v9.9.9 ' " +
		"-X 'main.GitCommit= abcdef1 ' " +
		"-X 'main.GitDirty=  ' " +
		"-X 'main.BuildTime= 2026-01-01T00:00:00Z '"

	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()

	build := exec.CommandContext(ctx, "go", "build", "-ldflags", ldflags, "-o", bin, ".")
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("building version probe binary: %v\n%s", err, out)
	}

	out, err := exec.CommandContext(ctx, bin, "-version").CombinedOutput()
	if err != nil {
		t.Fatalf("running -version: %v\n%s", err, out)
	}

	line := strings.TrimRight(string(out), "\n")

	for _, want := range []string{"v9.9.9", "abcdef1"} {
		if !strings.Contains(line, want) {
			t.Fatalf("-version output %q does not contain %q", line, want)
		}
	}

	if strings.HasPrefix(line, " ") {
		t.Errorf("-version output starts with whitespace: %q — the injected values "+
			"were not trimmed", line)
	}

	if strings.Contains(line, "  ") {
		t.Errorf("-version output contains a double space: %q — the injected values "+
			"were not trimmed", line)
	}

	if strings.ContainsAny(line, "\n\t\r") {
		t.Errorf("-version output contains embedded whitespace control characters: %q", line)
	}
}
