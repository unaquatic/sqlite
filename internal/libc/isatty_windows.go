// Copyright 2026 The Sqlite Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

//go:build windows

package libc

import "golang.org/x/sys/windows"

// isTerminal reports whether fd refers to a console handle.
//
// This replaces github.com/mattn/go-isatty. That package reached
// GetConsoleMode through a syscall.LazyDLL lookup of kernel32.dll;
// golang.org/x/sys/windows is already imported by libc_windows_*.go and
// exposes the same call directly.
//
// go-isatty also offered IsCygwinTerminal, which inspects the handle's object
// name to detect a cygwin/msys pty. C isatty() has no such notion -- it
// answers for the tty device only -- and go-isatty kept that out of
// IsTerminal as well, so nothing is lost here.
func isTerminal(fd uintptr) bool {
	var mode uint32
	return windows.GetConsoleMode(windows.Handle(fd), &mode) == nil
}
