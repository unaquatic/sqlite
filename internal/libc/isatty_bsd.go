// Copyright 2026 The Sqlite Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

//go:build darwin || freebsd || netbsd || openbsd

package libc

import "golang.org/x/sys/unix"

// isTerminal reports whether fd refers to a terminal.
//
// This replaces github.com/mattn/go-isatty, which used exactly this call on
// these platforms. golang.org/x/sys is already a dependency of this package,
// so the shim costs nothing.
//
// Only the BSD-family request name is correct here: TIOCGETA. Linux spells it
// TCGETS, but linux never compiles this file -- libc.go is constrained to
// !linux, and linux gets Xisatty from the transpiled musl in ccgo_linux_*.go.
func isTerminal(fd uintptr) bool {
	_, err := unix.IoctlGetTermios(int(fd), unix.TIOCGETA)
	return err == nil
}
