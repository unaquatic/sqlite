// Copyright 2016 The Sqlite Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package sqlite // import "github.com/unaquatic/sqlite"

// The C-allocation tests in this file count live allocations of the C
// allocator, which only counts them when built with -tags memory.counters.
// Without the tag they skip.

import (
	"context"
	"database/sql"
	"fmt"
	"runtime"
	"strings"
	"testing"
)

// Every connection allocates a libc.TLS, which must be freed with it. The test
// reads the heap profile, which samples one allocation per 512 KiB by
// default, so it opens enough connections to be sure to sample some.
// https://gitlab.com/cznic/sqlite/issues/118
func TestNoTLSLeak(t *testing.T) {
	for range 10000 {
		func() {
			db, err := sql.Open(driverName, ":memory:")
			if err != nil {
				t.Fatal(err)
			}
			defer db.Close()
			mustExec(t, db, `CREATE TABLE t1(v TEXT)`)
			mustExec(t, db, `INSERT INTO t1(v) VALUES(?)`, []byte(nil))
			if n := queryValue[int](t, db, "SELECT MAX(_ROWID_) FROM t1"); n <= 0 {
				t.Fatalf("MAX(_ROWID_) = %d", n)
			}
		}()
	}

	// The profile can be two cycles stale.
	runtime.GC()
	runtime.GC()
	var recs []runtime.MemProfileRecord
	n, _ := runtime.MemProfile(nil, true)
	for {
		recs = make([]runtime.MemProfileRecord, n+64)
		var ok bool
		if n, ok = runtime.MemProfile(recs, true); ok {
			recs = recs[:n]
			break
		}
	}

	seen := 0
	for _, rec := range recs {
		var stack []string
		isNewTLS := false
		frames := runtime.CallersFrames(rec.Stack())
		for {
			frame, more := frames.Next()
			stack = append(stack, fmt.Sprintf("%s (%s:%d)", frame.Function, frame.File, frame.Line))
			isNewTLS = isNewTLS || strings.Contains(frame.Function, "libc.NewTLS")
			if !more {
				break
			}
		}
		if !isNewTLS {
			continue
		}
		seen++
		if rec.InUseBytes() != 0 || rec.InUseObjects() != 0 {
			t.Errorf("libc.NewTLS leaked %d bytes in %d objects:\n%s",
				rec.InUseBytes(), rec.InUseObjects(), strings.Join(stack, "\n"))
		}
	}
	// Records of freed allocations prove the check is not vacuous.
	if seen == 0 {
		t.Fatal("the heap profile holds no libc.NewTLS allocation, so this test cannot detect a leak")
	}
	t.Logf("examined %d libc.NewTLS profile records", seen)
}

// A failed open frees its libc.TLS and the handle sqlite3_open_v2 returns.
func TestOpenFailureLeak(t *testing.T) {
	dsn := tempPath(t, "missing/impossible.db")
	leaked := cAllocsLeaked(t, 100, 1000, func() {
		if openErr(t, dsn) == nil {
			t.Fatal("opening an impossible path succeeded")
		}
	})
	if leaked > 100 {
		t.Fatalf("%d C allocations leaked over 1000 failed opens", leaked)
	}
}

// scriptConn returns a connection to an in-memory database holding table t
// with a unique column v, and a 1 KiB string already in it.
func scriptConn(t *testing.T) (*sql.Conn, string) {
	t.Helper()
	db := openDB(t, "file::memory:")
	db.SetMaxOpenConns(1)
	c, err := db.Conn(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { c.Close() })
	val := "collide-" + strings.Repeat("x", 1024)
	mustExec(t, c, "CREATE TABLE t(v TEXT UNIQUE)")
	mustExec(t, c, "INSERT INTO t VALUES(?)", val)
	return c, val
}

// TestScriptQueryLeaks runs scripts whose statements leave the bound
// arguments of a statement behind, each through a path that once leaked them.
func TestScriptQueryLeaks(t *testing.T) {
	ctx := context.Background()
	for _, tc := range []struct {
		name, script string
		wantErr      bool
	}{
		// The UPDATE binds and returns no row while the SELECT before it
		// already holds the rows to return: its arguments were dropped
		// unfreed, and the second SELECT's binding overwrote them.
		{"statement without rows mid-script", "SELECT 1; UPDATE t SET v = ? WHERE 0; SELECT ?", false},
		// The INSERT fails a UNIQUE constraint after binding.
		{"step error after bind", "INSERT INTO t VALUES(?); SELECT 1", true},
		// The INSERT fails after the SELECT produced rows, which the error
		// path dropped without closing them.
		{"error after rows", "SELECT ?; INSERT INTO t VALUES(?)", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c, val := scriptConn(t)
			leaked := cAllocsLeaked(t, 100, 1000, func() {
				rows, err := c.QueryContext(ctx, tc.script, val)
				if (err != nil) != tc.wantErr {
					t.Fatalf("got error %v, want error %v", err, tc.wantErr)
				}
				if err == nil {
					if err := rows.Close(); err != nil {
						t.Fatal(err)
					}
				}
			})
			if leaked > 100 {
				t.Fatalf("%d C allocations leaked over 1000 queries", leaked)
			}
		})
	}
}
