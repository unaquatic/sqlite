// Copyright 2016 The Sqlite Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package sqlite // import "github.com/unaquatic/sqlite"

import (
	"database/sql"
	"fmt"
	"strings"
	"testing"
)

// fill inserts n rows into the table insert names, with the arguments args
// returns for each, in one transaction.
func fill(b *testing.B, db *sql.DB, n int, insert string, args func(i int) []any) {
	b.Helper()
	tx, err := db.Begin()
	if err != nil {
		b.Fatal(err)
	}
	s, err := tx.Prepare(insert)
	if err != nil {
		b.Fatal(err)
	}
	for i := range n {
		if _, err := s.Exec(args(i)...); err != nil {
			b.Fatal(err)
		}
	}
	s.Close()
	if err := tx.Commit(); err != nil {
		b.Fatal(err)
	}
}

// scanAll runs query and scans every row into dest.
func scanAll(b *testing.B, db *sql.DB, query string, dest ...any) {
	rows, err := db.Query(query)
	if err != nil {
		b.Fatal(err)
	}
	for rows.Next() {
		if err := rows.Scan(dest...); err != nil {
			b.Fatal(err)
		}
	}
	if err := rows.Err(); err != nil {
		b.Fatal(err)
	}
	rows.Close()
}

// sizes are the row counts of the insert and scan benchmarks.
var sizes = []int{1e1, 1e2, 1e3, 1e4, 1e5, 1e6}

func BenchmarkInsertMemory(b *testing.B) {
	for i, n := range sizes {
		b.Run(fmt.Sprintf("1e%d", i+1), func(b *testing.B) {
			db := openDB(b, "file::memory:")
			db.SetMaxOpenConns(1)
			b.ReportAllocs()
			b.ResetTimer()
			for range b.N {
				b.StopTimer()
				mustExec(b, db, `drop table if exists t; create table t(i int); begin;`)
				s, err := db.Prepare("insert into t values(?)")
				if err != nil {
					b.Fatal(err)
				}
				b.StartTimer()
				for i := range n {
					if _, err := s.Exec(int64(i)); err != nil {
						b.Fatal(err)
					}
				}
				b.StopTimer()
				s.Close()
				mustExec(b, db, `commit;`)
				b.StartTimer()
			}
		})
	}
}

func BenchmarkNextMemory(b *testing.B) {
	for i, n := range sizes {
		b.Run(fmt.Sprintf("1e%d", i+1), func(b *testing.B) {
			db := openDB(b, "file::memory:")
			db.SetMaxOpenConns(1)
			mustExec(b, db, `create table t(i int)`)
			fill(b, db, n, "insert into t values(?)", func(i int) []any { return []any{int64(i)} })
			var v int
			b.ReportAllocs()
			for b.Loop() {
				scanAll(b, db, "select * from t", &v)
			}
		})
	}
}

// BenchmarkColumnTextScan reads TEXT columns. Small values are dominated by
// the allocation for each value, large ones by the copy out of SQLite.
func BenchmarkColumnTextScan(b *testing.B) {
	for _, size := range []int{16, 256, 4096} {
		b.Run(fmt.Sprint(size), func(b *testing.B) {
			db := openDB(b, "file::memory:")
			db.SetMaxOpenConns(1)
			mustExec(b, db, `CREATE TABLE t (s TEXT)`)
			payload := strings.Repeat("X", size)
			fill(b, db, 1000, `INSERT INTO t (s) VALUES (?)`, func(int) []any { return []any{payload} })
			var s string
			b.ReportAllocs()
			for b.Loop() {
				scanAll(b, db, `SELECT s FROM t`, &s)
			}
		})
	}
}
