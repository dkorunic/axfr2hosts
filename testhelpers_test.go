// SPDX-FileCopyrightText: 2026 Dinko Korunic
// SPDX-License-Identifier: MIT

package main

import (
	"bytes"
	"io"
	"os"
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

	f()

	w.Close()
	os.Stderr = old

	var buf bytes.Buffer
	_, _ = io.Copy(&buf, r)
	r.Close()

	return buf.String()
}
