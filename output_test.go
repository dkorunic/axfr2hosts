package main

import (
	"bytes"
	"io"
	"net/netip"
	"os"
	"strings"
	"testing"
)

func captureStdout(f func()) string {
	old := os.Stdout

	r, w, err := os.Pipe()
	if err != nil {
		panic(err)
	}

	os.Stdout = w

	f()

	w.Close()
	os.Stdout = old

	var buf bytes.Buffer
	_, _ = io.Copy(&buf, r)
	r.Close()

	return buf.String()
}

func TestDisplayHostEntries(t *testing.T) {
	ip1 := netip.MustParseAddr("192.0.2.1")
	ip2 := netip.MustParseAddr("192.0.2.2")
	ip3 := netip.MustParseAddr("2001:db8::1")

	entries := HostMap{
		ip1: {"host-a": struct{}{}, "host-b": struct{}{}},
		ip2: {"host-c": struct{}{}},
		ip3: {"host-d": struct{}{}},
	}
	// Deliberately unsorted to verify output sorts by IP.
	keys := []netip.Addr{ip3, ip1, ip2}

	output := captureStdout(func() {
		displayHostEntries(keys, entries)
	})

	lines := strings.Split(strings.TrimRight(output, "\n"), "\n")

	if len(lines) < 4 {
		t.Fatalf("Expected at least 4 lines, got %d:\n%s", len(lines), output)
	}

	if !strings.HasPrefix(lines[0], "# axfr2hosts generated list") {
		t.Errorf("line[0] expected header comment, got %q", lines[0])
	}

	// Lines 1-3 must be sorted by IP address.
	if !strings.HasPrefix(lines[1], "192.0.2.1\t") {
		t.Errorf("line[1] expected 192.0.2.1, got %q", lines[1])
	}

	if !strings.HasPrefix(lines[2], "192.0.2.2\t") {
		t.Errorf("line[2] expected 192.0.2.2, got %q", lines[2])
	}

	if !strings.HasPrefix(lines[3], "2001:db8::1\t") {
		t.Errorf("line[3] expected 2001:db8::1, got %q", lines[3])
	}

	// Multiple hostnames must be sorted alphabetically and space-separated.
	if lines[1] != "192.0.2.1\thost-a host-b" {
		t.Errorf("line[1] = %q, want %q", lines[1], "192.0.2.1\thost-a host-b")
	}

	if lines[2] != "192.0.2.2\thost-c" {
		t.Errorf("line[2] = %q, want %q", lines[2], "192.0.2.2\thost-c")
	}
}

func TestDisplayHostEntriesEmpty(t *testing.T) {
	output := captureStdout(func() {
		displayHostEntries([]netip.Addr{}, make(HostMap))
	})

	lines := strings.Split(strings.TrimRight(output, "\n"), "\n")

	if len(lines) != 1 {
		t.Errorf("Expected only header line for empty input, got %d lines:\n%s", len(lines), output)
	}

	if !strings.HasPrefix(lines[0], "# axfr2hosts generated list") {
		t.Errorf("Expected header comment, got %q", lines[0])
	}
}

func TestDisplayHostEntriesSingleHost(t *testing.T) {
	ip := netip.MustParseAddr("10.0.0.1")

	entries := HostMap{
		ip: {"myhost": struct{}{}},
	}
	keys := []netip.Addr{ip}

	output := captureStdout(func() {
		displayHostEntries(keys, entries)
	})

	lines := strings.Split(strings.TrimRight(output, "\n"), "\n")

	if len(lines) != 2 {
		t.Fatalf("Expected 2 lines, got %d:\n%s", len(lines), output)
	}

	if lines[1] != "10.0.0.1\tmyhost" {
		t.Errorf("line[1] = %q, want %q", lines[1], "10.0.0.1\tmyhost")
	}
}
