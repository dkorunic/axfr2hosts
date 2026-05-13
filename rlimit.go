// SPDX-FileCopyrightText: 2023 Dinko Korunic
// SPDX-License-Identifier: MIT

//go:build !unix

package main

// setNofile is a no-op on non-Unix systems.
func setNofile() error {
	return nil
}
