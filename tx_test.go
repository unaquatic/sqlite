// Copyright 2016 The Sqlite Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package sqlite // import "github.com/unaquatic/sqlite"

import (
	"context"
	"database/sql"
	"fmt"
	"math/rand"
	"sync"
	"testing"

	sqlite3 "github.com/unaquatic/sqlite/internal/lib"
	"github.com/unaquatic/sqlite/internal/libc"
)

// _txlock decides which lock BEGIN takes.
// https://gitlab.com/cznic/sqlite/-/issues/92
func TestBeginMode(t *testing.T) {
	for _, tc := range []struct {
		mode string
		want int32
	}{
		{"deferred", sqlite3.SQLITE_TXN_NONE},
		{"immediate", sqlite3.SQLITE_TXN_WRITE},
		// EXCLUSIVE differs from IMMEDIATE only in how it treats other
		// connections in rollback-journal modes, which the transaction
		// state does not show.
		{"exclusive", sqlite3.SQLITE_TXN_WRITE},
	} {
		for _, journal := range []string{"delete", "wal"} {
			t.Run(journal+"/"+tc.mode, func(t *testing.T) {
				db := openDB(t, tempPath(t, "test.db")+fmt.Sprintf("?_txlock=%s&_pragma=journal_mode(%s)", tc.mode, journal))
				ctx := context.Background()
				c, err := db.Conn(ctx)
				if err != nil {
					t.Fatal(err)
				}
				defer c.Close()
				tx, err := c.BeginTx(ctx, nil)
				if err != nil {
					t.Fatal(err)
				}
				defer tx.Rollback()

				rawConn(t, c, func(c *conn) {
					zMain, err := libc.CString("main")
					if err != nil {
						t.Fatal(err)
					}
					defer c.free(zMain)
					if got := sqlite3.Xsqlite3_txn_state(c.tls, c.db, zMain); got != tc.want {
						t.Errorf("transaction state %d, want %d", got, tc.want)
					}
				})
			})
		}
	}
}

// A COMMIT that fails with SQLITE_BUSY is rolled back, so the connection
// returns to the pool outside any transaction.
// https://github.com/modernc-org/sqlite/issues/2
func TestTxCommitBusy(t *testing.T) {
	ctx := context.Background()
	path := tempPath(t, "issue2.db")
	mustExec(t, openDB(t, path), "create table foo (bar integer primary key)")

	db1, db2 := openDB(t, path), openDB(t, path)
	db1.SetMaxOpenConns(1)
	db2.SetMaxOpenConns(1)

	tx1, err := db1.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer tx1.Rollback()
	tx2, err := db2.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer tx2.Rollback()

	// tx2 reads, taking a SHARED lock; tx1 writes, taking a RESERVED lock.
	// tx1's COMMIT then needs an EXCLUSIVE lock, which tx2's lock denies.
	mustExec(t, tx2, "select * from foo")
	mustExec(t, tx1, "insert into foo (bar) values (42)")
	err = tx1.Commit()
	if code, _ := errorCode(err); code != sqlite3.SQLITE_BUSY {
		t.Fatalf("Commit = %v, want SQLITE_BUSY", err)
	}
	if err := tx1.Rollback(); err != sql.ErrTxDone {
		t.Fatalf("Rollback after Commit = %v, want sql.ErrTxDone", err)
	}

	// Without the rollback in Commit the connection would still be inside
	// tx1: "cannot start a transaction within a transaction".
	tx3, err := db1.BeginTx(ctx, nil)
	if err != nil {
		t.Fatalf("the connection is still in a transaction after the failed Commit: %v", err)
	}
	tx3.Rollback()
}

// A transaction opened with BEGIN in a script spans the statements after it.
// https://gitlab.com/cznic/sqlite/issues/11
func TestScriptBegin(t *testing.T) {
	db := tempDB(t)
	db.SetMaxOpenConns(1)
	mustExec(t, db, `CREATE TABLE t1 (t INT); BEGIN;`)
	for i := range 6570 {
		mustExec(t, db, "INSERT INTO t1 (t) VALUES (?)", i)
	}
	mustExec(t, db, "COMMIT;")
}

// Concurrent read and write transactions on one database. Without a busy
// timeout they may fail with SQLITE_BUSY; with one, they must not.
// https://gitlab.com/cznic/sqlite/-/issues/65
func TestConcurrentTransactions(t *testing.T) {
	for _, tc := range []struct {
		name    string
		query   string
		canFail bool
	}{
		{"no busy timeout", "", true},
		{"busy timeout", "?_pragma=busy_timeout%3d80000", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()
			db := openDB(t, tempPath(t, "test.db")+tc.query)
			mustExec(t, db, "CREATE TABLE foo (department INTEGER, profits INTEGER)")
			mustExec(t, db, "INSERT INTO foo VALUES (1, 10), (1, 20), (1, 45), (2, 42), (2, 115)")

			read := func() error {
				tx, err := db.BeginTx(ctx, nil)
				if err != nil {
					return err
				}
				defer tx.Rollback()
				var dept, count int64
				return tx.QueryRowContext(ctx, "SELECT department, COUNT(*) FROM foo GROUP BY department").Scan(&dept, &count)
			}
			write := func() error {
				tx, err := db.BeginTx(ctx, nil)
				if err != nil {
					return err
				}
				defer tx.Rollback()
				if _, err := tx.ExecContext(ctx, "INSERT INTO foo(department, profits) VALUES (?, ?)", rand.Int(), rand.Int()); err != nil {
					return err
				}
				return tx.Commit()
			}

			var wg sync.WaitGroup
			for name, f := range map[string]func() error{"read": read, "write": write} {
				wg.Go(func() {
					for range 100 {
						if err := f(); err != nil {
							if tc.canFail {
								t.Logf("%s: %v", name, err)
							} else {
								t.Errorf("%s: %v", name, err)
							}
							return
						}
					}
				})
			}
			wg.Wait()
		})
	}
}
