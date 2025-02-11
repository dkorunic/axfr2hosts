// @license
// Copyright (C) 2023  Dinko Korunic
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
