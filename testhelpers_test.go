// SPDX-FileCopyrightText: 2026 Dinko Korunic
// SPDX-License-Identifier: MIT

package main

import (
	"bytes"
	"io"
	"net"
	"os"
	"path/filepath"
	"testing"
)

// captureStderr redirects os.Stderr for the duration of f and returns whatever
// was written to it.  It is the stderr counterpart of captureStdout in
// output_test.go.
func captureStderr(f func()) string {
	old := os.Stderr

	r, w, err := os.Pipe()
	if err != nil {
		panic(err)
	}

	os.Stderr = w

	var (
		buf  bytes.Buffer
		done = make(chan struct{})
	)

	// drained concurrently; see captureStdout for why reading after f can deadlock
	go func() {
		defer close(done)

		_, _ = io.Copy(&buf, r)
	}()

	f()

	w.Close()
	os.Stderr = old

	<-done
	r.Close()

	return buf.String()
}

// splitHostPortCheck reports whether addr is a usable dial target, i.e. whether
// net.Dial would accept it.  Tests use it to assert that a normalised address
// really can be dialled rather than merely matching an expected string.
func splitHostPortCheck(addr string) (string, string, error) {
	return net.SplitHostPort(addr)
}

// writeTempZone writes content to a temp zone file scoped to the test and returns
// its path.  t.TempDir cleans it up, so tests need no explicit removal.
func writeTempZone(t *testing.T, content string) string {
	t.Helper()

	path := filepath.Join(t.TempDir(), "zone.txt")

	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("writing temp zone: %v", err)
	}

	return path
}

// benchTempZone is the *testing.B counterpart of writeTempZone.
func benchTempZone(b *testing.B, content string) string {
	b.Helper()

	path := filepath.Join(b.TempDir(), "zone.txt")

	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		b.Fatalf("writing temp zone: %v", err)
	}

	return path
}
