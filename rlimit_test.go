// SPDX-FileCopyrightText: 2026 Dinko Korunic
// SPDX-License-Identifier: MIT

//go:build unix

package main

import (
	"runtime"
	"syscall"
	"testing"
)

// TestSetNofile checks the open-file limit is actually raised.  setNofile is
// best-effort by design — main ignores its error — so the assertion is that it
// either succeeds and moves the soft limit to the platform target, or fails
// cleanly without disturbing the current limit.  An unprivileged process cannot
// raise the hard limit, so a failure here is legitimate, not a test bug.
func TestSetNofile(t *testing.T) {
	var before syscall.Rlimit
	if err := syscall.Getrlimit(syscall.RLIMIT_NOFILE, &before); err != nil {
		t.Fatalf("Getrlimit: %v", err)
	}

	t.Cleanup(func() {
		_ = syscall.Setrlimit(syscall.RLIMIT_NOFILE, &before)
	})

	want := uint64(defaultNoFile)
	if runtime.GOOS == "darwin" {
		want = darwinMagic
	}

	err := setNofile()

	var after syscall.Rlimit
	if gerr := syscall.Getrlimit(syscall.RLIMIT_NOFILE, &after); gerr != nil {
		t.Fatalf("Getrlimit after setNofile: %v", gerr)
	}

	if err != nil {
		// permitted: the hard limit may be below the target for an unprivileged process
		if after.Cur != before.Cur {
			t.Errorf("setNofile() failed with %v but still changed the soft limit from %d to %d",
				err, before.Cur, after.Cur)
		}

		t.Logf("setNofile() returned %v (hard limit %d below target %d) — limit left untouched",
			err, before.Max, want)

		return
	}

	if after.Cur != want {
		t.Errorf("setNofile() soft limit = %d, want %d", after.Cur, want)
	}
}

// TestSetNofileTargets pins the platform constants.  macOS caps RLIMIT_NOFILE at
// 24576 per setrlimit(2); asking for more fails outright, so this value is not a
// tunable but a hard platform ceiling.
func TestSetNofileTargets(t *testing.T) {
	if darwinMagic != 24576 {
		t.Errorf("darwinMagic = %d, want 24576 (macOS setrlimit(2) ceiling)", darwinMagic)
	}

	if defaultNoFile <= darwinMagic {
		t.Errorf("defaultNoFile = %d, want a value above the macOS ceiling %d",
			defaultNoFile, darwinMagic)
	}
}
