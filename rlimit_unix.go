// SPDX-FileCopyrightText: 2023 Dinko Korunic
// SPDX-License-Identifier: MIT

//go:build unix

package main

import (
	"runtime"
	"syscall"
)

const (
	darwinMagic   = 24576
	defaultNoFile = 100000
)

// setNofile sets the maximum number of open files to the maximum allowed value.
//
// For darwin (macOS), the maximum allowed value is 24576 as per the
// documentation for setrlimit(2).
//
// For other platforms, the maximum allowed value is 100000.
//
// The setNofile function returns an error if the syscall.Setrlimit call fails.
func setNofile() error {
	if runtime.GOOS == "darwin" {
		return syscall.Setrlimit(syscall.RLIMIT_NOFILE, &syscall.Rlimit{
			Cur: darwinMagic,
			Max: darwinMagic,
		})
	}

	return syscall.Setrlimit(syscall.RLIMIT_NOFILE, &syscall.Rlimit{
		Cur: defaultNoFile,
		Max: defaultNoFile,
	})
}
