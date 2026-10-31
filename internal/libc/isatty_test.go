// Copyright 2026 The Sqlite Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// isTerminal exists only where libc.go is compiled, i.e. everywhere except
// linux, which gets Xisatty from the transpiled musl instead.
//go:build darwin || freebsd || netbsd || openbsd || windows

package libc

import (
	"os"
	"testing"
)

// Behavioural check for the hand-written isTerminal shim that replaced
// github.com/mattn/go-isatty. Run under a pty to cover the positive case:
//
//	script -q /dev/null go test -count=1 -run TestIsTerminalBehaviour ./internal/libc
func TestIsTerminalBehaviour(t *testing.T) {
	// A regular file is never a terminal.
	f, err := os.CreateTemp("", "isatty")
	if err != nil {
		t.Fatal(err)
	}
	defer os.Remove(f.Name())
	defer f.Close()
	if isTerminal(f.Fd()) {
		t.Errorf("regular file %q reported as terminal", f.Name())
	} else {
		t.Logf("regular file: not a terminal (correct)")
	}

	// A pipe is never a terminal.
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	defer w.Close()
	if isTerminal(r.Fd()) {
		t.Errorf("pipe read end reported as terminal")
	} else {
		t.Logf("pipe: not a terminal (correct)")
	}

	// /dev/null is a character device but not a terminal.
	dn, err := os.Open(os.DevNull)
	if err != nil {
		t.Fatal(err)
	}
	defer dn.Close()
	if isTerminal(dn.Fd()) {
		t.Errorf("%s reported as terminal", os.DevNull)
	} else {
		t.Logf("%s: not a terminal (correct)", os.DevNull)
	}

	// Positive case. Under a pty (e.g. `script -q /dev/null go test ...`)
	// stdin is the terminal; otherwise fall back to /dev/tty.
	if isTerminal(os.Stdin.Fd()) {
		t.Logf("stdin: is a terminal (correct, running under a pty)")
		return
	}
	t.Logf("stdin is not a terminal (no pty), trying /dev/tty")

	tty, err := os.Open("/dev/tty")
	if err != nil {
		t.Logf("no /dev/tty available, positive case NOT covered: %v", err)
		return
	}
	defer tty.Close()
	if !isTerminal(tty.Fd()) {
		t.Errorf("/dev/tty NOT reported as terminal -- shim is broken")
	} else {
		t.Logf("/dev/tty: is a terminal (correct)")
	}
}
