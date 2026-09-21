// Copyright 2016 The Sqlite Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package sqlite // import "github.com/unaquatic/sqlite"

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"math/rand"
	"strings"
	"sync"
	"testing"
	"time"
)

// Goroutines share a prepared statement of one transaction. Run with -race.
func TestConcurrentInserts(t *testing.T) {
	for _, tc := range []struct {
		name       string
		goroutines int
		maxConns   int // 0 for no limit
	}{
		{"few goroutines", 8, 0},
		{"single conn", 100, 1},
		{"multiple conns", 100, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			const rowsEach = 5000
			db := tempDB(t)
			db.SetMaxOpenConns(tc.maxConns)
			tx, err := db.BeginTx(context.Background(), nil)
			if err != nil {
				t.Fatal(err)
			}
			defer tx.Rollback()
			mustExec(t, tx, "create table t(i)")
			prep, err := tx.Prepare("insert into t values(?)")
			if err != nil {
				t.Fatal(err)
			}

			// Every value in [0, total) is inserted once, in random order.
			total := tc.goroutines * rowsEach
			values := make(chan int, 100)
			go func() {
				for _, v := range rand.Perm(total) {
					values <- v
				}
			}()

			t0 := time.Now()
			var wg sync.WaitGroup
			for id := range tc.goroutines {
				wg.Go(func() {
					for range rowsEach {
						v := <-values
						var err error
						for range 10 {
							if _, err = prep.Exec(v); err == nil {
								break
							}
						}
						if err != nil {
							t.Errorf("goroutine %d: %v", id, err)
							return
						}
					}
				})
			}
			wg.Wait()
			if err := tx.Commit(); err != nil {
				t.Fatal(err)
			}
			t.Logf("%d goroutines inserted %d rows in %v", tc.goroutines, total, time.Since(t0))

			rows, err := db.Query("select * from t order by i")
			if err != nil {
				t.Fatal(err)
			}
			defer rows.Close()
			n := 0
			for ; rows.Next(); n++ {
				var v int
				if err := rows.Scan(&v); err != nil {
					t.Fatal(err)
				}
				if v != n {
					t.Fatalf("row %d: got %d", n, v)
				}
			}
			if err := rows.Err(); err != nil {
				t.Fatal(err)
			}
			if n != total {
				t.Fatalf("got %d rows, want %d", n, total)
			}
		})
	}
}

// Goroutines share prepared statements of the database, which run on
// whichever connection the pool hands them.
// https://gitlab.com/cznic/sqlite/issues/20
func TestConcurrentPreparedStatements(t *testing.T) {
	// A busy timeout of 25 seconds or more was needed on a slow
	// linux/ppc64le VM.
	db := openDB(t, tempPath(t, "foo.db")+"?_pragma=busy_timeout%3d50000")
	mustExec(t, db, "CREATE TABLE gosqltest_t (count INT)")
	sel, err := db.Prepare("SELECT count FROM gosqltest_t ORDER BY count DESC")
	if err != nil {
		t.Fatal(err)
	}
	defer sel.Close()
	ins, err := db.Prepare("INSERT INTO gosqltest_t (count) VALUES (?)")
	if err != nil {
		t.Fatal(err)
	}
	defer ins.Close()
	for n := 1; n <= 3; n++ {
		if _, err := ins.Exec(n); err != nil {
			t.Fatal(err)
		}
	}

	var wg sync.WaitGroup
	for range 10 {
		wg.Go(func() {
			for range 10 {
				var count int
				if err := sel.QueryRow().Scan(&count); err != nil && err != sql.ErrNoRows {
					t.Errorf("query: %v", err)
					return
				}
				if _, err := ins.Exec(rand.Intn(100)); err != nil {
					t.Errorf("insert: %v", err)
					return
				}
			}
		})
	}
	wg.Wait()
}

// Opening and closing a database for every statement, as a short-lived
// program would, keeps working.
// https://gitlab.com/cznic/sqlite/-/issues/51
func TestOpenClosePerStatement(t *testing.T) {
	path := tempPath(t, "test_issue51.db")
	mustExec(t, openDB(t, path), `
CREATE TABLE fileHash (
	"hash" TEXT NOT NULL PRIMARY KEY,
	"filename" TEXT,
	"lastChecked" INTEGER
);`)

	// run opens the database, runs query through a prepared statement,
	// reads any rows and closes everything.
	run := func(query string, args ...any) error {
		db, err := sql.Open(driverName, path)
		if err != nil {
			return err
		}
		s, err := db.Prepare(query)
		if err != nil {
			return errors.Join(err, db.Close())
		}
		rows, err := s.Query(args...)
		if err != nil {
			return errors.Join(err, s.Close(), db.Close())
		}
		for rows.Next() {
		}
		return errors.Join(rows.Err(), rows.Close(), s.Close(), db.Close())
	}

	// 2000 cycles open the database 4000 times, more than the default limit
	// on open files (256 on macOS, 1024 on Linux), so a leaked descriptor
	// fails the test.
	const cycles = 2000
	for i := range cycles {
		hash := fmt.Sprintf("%032d", i)
		if err := run(`SELECT hash, fileName, lastChecked FROM fileHash WHERE hash=?;`, hash); err != nil {
			t.Fatalf("lookup %d: %v", i, err)
		}
		if err := run(`INSERT OR REPLACE INTO fileHash(hash, fileName, lastChecked) VALUES(?, ?, ?);`, hash, hash+".temp", time.Now().Unix()); err != nil {
			t.Fatalf("save %d: %v", i, err)
		}
	}
	if n := queryValue[int](t, openDB(t, path), "select count(*) from fileHash"); n != cycles {
		t.Fatalf("got %d records, want %d", n, cycles)
	}
}

// A context cancelled after its statement completed must not interrupt a
// later statement.
// https://gitlab.com/cznic/sqlite/-/issues/94
func TestCancelRace(t *testing.T) {
	db := tempDB(t)
	for _, tc := range []struct {
		name string
		f    func(context.Context) error
	}{
		{"db.ExecContext", func(ctx context.Context) error {
			_, err := db.ExecContext(ctx, "select 1")
			return err
		}},
		{"db.QueryContext", func(ctx context.Context) error {
			rows, err := db.QueryContext(ctx, "select 1")
			if err == nil {
				rows.Close()
			}
			return err
		}},
		{"tx.ExecContext", func(ctx context.Context) error {
			tx, err := db.BeginTx(ctx, nil)
			if err != nil {
				return err
			}
			defer tx.Rollback()
			if _, err := tx.ExecContext(ctx, "select 1"); err != nil {
				return err
			}
			return tx.Rollback()
		}},
		{"tx.QueryContext", func(ctx context.Context) error {
			tx, err := db.BeginTx(ctx, nil)
			if err != nil {
				return err
			}
			defer tx.Rollback()
			rows, err := tx.QueryContext(ctx, "select 1")
			if err != nil {
				return err
			}
			rows.Close()
			return tx.Rollback()
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			// The race does not show on every run, but it does within a
			// hundred iterations.
			for i := range 100 {
				ctx, cancel := context.WithCancel(context.Background())
				err := tc.f(ctx)
				cancel()
				if err != nil {
					t.Fatalf("iteration %d: %v", i, err)
				}
			}
		})
	}
}

// Cancelling the context while Exec drains the rows of a statement with
// RETURNING stops it with an error.
func TestExecReturningCancelDuringDrain(t *testing.T) {
	db := openDB(t, ":memory:")
	db.SetMaxOpenConns(1)
	mustExec(t, db, "CREATE TABLE t (id INTEGER PRIMARY KEY)")
	mustExec(t, db, `
		WITH RECURSIVE gen(n) AS (SELECT 1 UNION ALL SELECT n+1 FROM gen WHERE n < 10000)
		INSERT INTO t SELECT n FROM gen`)

	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(time.Microsecond)
		cancel()
	}()
	_, err := db.ExecContext(ctx, "DELETE FROM t RETURNING id")
	switch {
	case err == nil:
		t.Log("the statement finished before the cancel; inconclusive")
	case !errors.Is(err, context.Canceled) && !strings.Contains(err.Error(), "interrupted (9)"):
		t.Fatalf("got %v, want context.Canceled or an interrupted error", err)
	}
}
