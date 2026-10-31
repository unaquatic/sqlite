// Copyright 2016 The Sqlite Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package sqlite // import "github.com/unaquatic/sqlite"

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"testing"

	"github.com/unaquatic/sqlite/internal/libc"
)

// openDB opens dsn with this package's driver. The database is closed when
// the test ends.
func openDB(t testing.TB, dsn string) *sql.DB {
	t.Helper()
	db, err := sql.Open(driverName, dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

// tempPath returns the path of a file named name in a temporary directory
// that is removed when the test ends.
func tempPath(t testing.TB, name string) string {
	t.Helper()
	return filepath.Join(t.TempDir(), name)
}

// tempDB opens a new database file in a temporary directory.
func tempDB(t testing.TB) *sql.DB {
	t.Helper()
	return openDB(t, tempPath(t, "test.db"))
}

// openErr opens dsn and returns the error of its first connection.
// sql.Open does not connect, so DSN errors surface only here.
func openErr(t testing.TB, dsn string) error {
	t.Helper()
	return openDB(t, dsn).Ping()
}

// errorCode returns the SQLite result code err carries, read the way the
// package documentation tells callers to, and whether it carries one.
func errorCode(err error) (int, bool) {
	var e interface{ Code() int }
	if errors.As(err, &e) {
		return e.Code(), true
	}
	return 0, false
}

// execer is implemented by *sql.DB, *sql.Conn and *sql.Tx.
type execer interface {
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
}

// queryer is implemented by *sql.DB, *sql.Conn and *sql.Tx.
type queryer interface {
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}

// mustExec runs query and fails the test on error.
func mustExec(t testing.TB, db execer, query string, args ...any) sql.Result {
	t.Helper()
	r, err := db.ExecContext(context.Background(), query, args...)
	if err != nil {
		t.Fatalf("%s: %v", query, err)
	}
	return r
}

// queryValue runs query, which must return one row of one column, and
// returns that value as a T.
func queryValue[T any](t testing.TB, db queryer, query string, args ...any) T {
	t.Helper()
	var v T
	if err := db.QueryRowContext(context.Background(), query, args...).Scan(&v); err != nil {
		t.Fatalf("%s: %v", query, err)
	}
	return v
}

// rawConn runs f on the driver connection behind c.
func rawConn(t testing.TB, c *sql.Conn, f func(*conn)) {
	t.Helper()
	if err := c.Raw(func(dc any) error {
		f(dc.(*conn))
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}

// cAllocsLeaked runs f warmup times to reach a steady state, then n times, and
// returns how many more C allocations are live after the n runs than before.
// It skips the test unless the C allocator counts its allocations, which
// takes -tags memory.counters.
func cAllocsLeaked(t testing.TB, warmup, n int, f func()) int {
	t.Helper()
	for range warmup {
		f()
	}
	before := libc.MemStat()
	if before.Allocs == 0 && before.Bytes == 0 {
		t.Skip("counting C allocations takes -tags memory.counters")
	}
	for range n {
		f()
	}
	after := libc.MemStat()
	t.Logf("live C allocations: before=%d after=%d", before.Allocs, after.Allocs)
	return after.Allocs - before.Allocs
}
