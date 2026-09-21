// Copyright 2026 The Sqlite Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package sqlite // import "github.com/unaquatic/sqlite"

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	sqlite3 "github.com/unaquatic/sqlite/internal/lib"
)

// A database that cannot be opened reports SQLITE_CANTOPEN as an *Error,
// without a stale message from the handle the failed open returns: the
// original report saw "out of memory" opening /sys/repro.db on Linux.
// https://gitlab.com/cznic/sqlite/-/issues/230
func TestOpenFailure(t *testing.T) {
	// A path under a regular file fails on every platform.
	notDir := tempPath(t, "notdir")
	if err := os.WriteFile(notDir, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	err := openErr(t, filepath.Join(notDir, "child", "x.db"))
	var e *Error
	if !errors.As(err, &e) {
		t.Fatalf("got %T (%v), want *Error", err, err)
	}
	if e.Code() != sqlite3.SQLITE_CANTOPEN {
		t.Errorf("Code() = %d, want SQLITE_CANTOPEN (%d)", e.Code(), sqlite3.SQLITE_CANTOPEN)
	}
	if !strings.Contains(e.Error(), "unable to open database file") {
		t.Errorf("error %q lacks the canonical message", e)
	}
	if strings.Contains(strings.ToLower(e.Error()), "out of memory") {
		t.Errorf("error %q carries a stale message", e)
	}
}

// When the handle's error matches the operation's, as it does for syntax and
// constraint errors, the message keeps the detail.
func TestErrorDetail(t *testing.T) {
	_, err := openDB(t, ":memory:").Exec("SELECT not-a-column FROM not-a-table")
	if err == nil {
		t.Fatal("no error")
	}
	if low := strings.ToLower(err.Error()); !strings.Contains(low, "no such table") && !strings.Contains(low, "syntax error") {
		t.Errorf("error %q lacks the detail", err)
	}
}

// newError leaves out the handle's message when it belongs to another
// operation: here a healthy handle, whose message is "not an error", and
// SQLITE_CANTOPEN.
func TestErrorStaleMessage(t *testing.T) {
	c, err := newConn(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()

	err = newError(c.tls, c.db, sqlite3.SQLITE_CANTOPEN)
	var e *Error
	if !errors.As(err, &e) {
		t.Fatalf("got %T (%v), want *Error", err, err)
	}
	if e.Code() != sqlite3.SQLITE_CANTOPEN {
		t.Errorf("Code() = %d, want SQLITE_CANTOPEN", e.Code())
	}
	if got, want := e.Error(), "unable to open database file (14)"; got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}
