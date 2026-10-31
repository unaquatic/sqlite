// Copyright 2024 The Sqlite Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package sqlite // import "github.com/unaquatic/sqlite"

import (
	"context"
	"database/sql/driver"
	"net/url"
	"os"
	"testing"
)

func TestDSNPragmas(t *testing.T) {
	// verifyPragma opens a database file with the DSN parameters query and
	// checks the PRAGMAs named in kvs, given as name-value pairs. Some
	// PRAGMAs need a database file, which is created on first use.
	verifyPragma := func(t *testing.T, query string, kvs ...string) {
		t.Helper()
		db := openDB(t, tempPath(t, "tmp.db")+"?"+query)
		for i := 0; i < len(kvs); i += 2 {
			if got := queryValue[string](t, db, "PRAGMA "+kvs[i]); got != kvs[i+1] {
				t.Fatalf("%s: PRAGMA %s = %q, want %q", query, kvs[i], got, kvs[i+1])
			}
		}
	}

	t.Run("_pragma", func(t *testing.T) {
		verifyPragma(t, "", "foreign_keys", "0", "busy_timeout", "0")
		verifyPragma(t, "_pragma=foreign_keys=on", "foreign_keys", "1")
		verifyPragma(t, "_pragma=foreign_keys=on&_pragma=busy_timeout=5000", "foreign_keys", "1", "busy_timeout", "5000")
		verifyPragma(t, "_pragma=foreign_keys%3Don&_pragma=busy_timeout%3D5000", "foreign_keys", "1", "busy_timeout", "5000")
	})

	t.Run("_journal_mode", func(t *testing.T) {
		verifyPragma(t, "", "journal_mode", "delete")
		verifyPragma(t, "_journal_mode=wal", "journal_mode", "wal")
		verifyPragma(t, "_journal_mode=TRUNCATE", "journal_mode", "truncate")
	})

	// _journal_mode runs after the _pragma values, so a new database gets
	// the page size they ask for although WAL mode would fix it first.
	t.Run("page_size before WAL", func(t *testing.T) {
		verifyPragma(t, "_journal_mode=WAL&_pragma=page_size(16384)", "journal_mode", "wal", "page_size", "16384")
	})
}

// TestDSNInvalidValues checks the error for each parameter value the driver
// rejects, and that the rejection comes before the database file is created.
func TestDSNInvalidValues(t *testing.T) {
	for _, tc := range []struct{ query, want string }{
		{"_journal_mode=WAL2", `invalid _journal_mode "WAL2", expecting one of: DELETE TRUNCATE PERSIST MEMORY WAL OFF`},
		{"_txlock=bogus", `unknown _txlock "bogus"`},
		{"_journal_mode=wal&_txlock=bogus", `unknown _txlock "bogus"`},
		// Parameters the driver does not read, among them ones it once did.
		{"_fk=1", `unknown DSN parameter "_fk"`},
		{"_pragma=foreign_keys(1)&_busy_timeout=5000", `unknown DSN parameter "_busy_timeout"`},
		{"_time_format=sqlite", `unknown DSN parameter "_time_format"`},
	} {
		t.Run(tc.query, func(t *testing.T) {
			path := tempPath(t, "tmp.db")
			if err := openErr(t, path+"?"+tc.query); err == nil || err.Error() != tc.want {
				t.Fatalf("got error %v, want %q", err, tc.want)
			}
			if _, err := os.Stat(path); !os.IsNotExist(err) {
				t.Fatalf("the rejected DSN created the database file: %v", err)
			}
		})
	}
}

// _pragma values apply to every connection, including one that replaces a
// connection the pool dropped.
// https://gitlab.com/cznic/sqlite/-/issues/37
func TestPragmasApplyToEveryConnection(t *testing.T) {
	t.Chdir(t.TempDir())
	pragmas := []struct {
		name, value string
		want        any
	}{
		{"foreign_keys", "on", int64(1)},
		{"analysis_limit", "1000", int64(1000)},
		{"application_id", "214", int64(214)},
		{"encoding", "'UTF-16le'", "UTF-16le"},
	}
	q := url.Values{}
	for _, p := range pragmas {
		q.Add("_pragma", p.name+"="+p.value)
	}

	for _, name := range []string{"testpersistpragma.sqlite", "file::memory:", ":memory:"} {
		t.Run(name, func(t *testing.T) {
			db := openDB(t, name+"?"+q.Encode())
			db.SetMaxOpenConns(1)
			check := func() {
				t.Helper()
				for _, p := range pragmas {
					if got := queryValue[any](t, db, "PRAGMA "+p.name); got != p.want {
						t.Errorf("PRAGMA %s = %v, want %v", p.name, got, p.want)
					}
				}
			}
			check()

			c, err := db.Conn(context.Background())
			if err != nil {
				t.Fatal(err)
			}
			// Returning ErrBadConn makes the pool drop the connection.
			c.Raw(func(any) error { return driver.ErrBadConn })
			check()
		})
	}
}
