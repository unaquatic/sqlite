// Copyright 2016 The Sqlite Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package sqlite // import "github.com/unaquatic/sqlite"

import (
	"database/sql"
	"fmt"
	"math/rand"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
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

// BenchmarkConcurrent measures reads and writes per second with readers and
// writers working at once, each on a database/sql handle of its own, against
// a WAL database of a million rows.
//
// The benchmark is derived from https://github.com/kalafut/go-sqlite-bench:
//
// # MIT License
//
// # Copyright (c) 2022 Jim Kalafut
//
// Permission is hereby granted, free of charge, to any person obtaining a copy
// of this software and associated documentation files (the "Software"), to deal
// in the Software without restriction, including without limitation the rights
// to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
// copies of the Software, and to permit persons to whom the Software is
// furnished to do so, subject to the following conditions:
//
// The above copyright notice and this permission notice shall be included in all
// copies or substantial portions of the Software.
//
// THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
// IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
// FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
// AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
// LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
// OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE
// SOFTWARE.
func BenchmarkConcurrent(b *testing.B) {
	for _, measure := range []string{"reads", "writes"} {
		for _, writers := range []int{0, 1, 10, 100} {
			for _, readers := range []int{0, 1, 10, 100} {
				if measure == "reads" && readers == 0 || measure == "writes" && writers == 0 {
					continue
				}
				b.Run(fmt.Sprintf("%s readers %d writers %d", measure, readers, writers), func(b *testing.B) {
					benchmarkConcurrent(b, readers, writers, measure)
				})
			}
		}
	}
}

func benchmarkConcurrent(b *testing.B, readers, writers int, measure string) {
	const rows = 1e6
	dsn := tempPath(b, "test.db") + "?_pragma=busy_timeout(10000)&_pragma=synchronous(NORMAL)&_pragma=journal_mode(WAL)"
	randString := func() string {
		s := make([]byte, 30)
		for i := range s {
			s[i] = byte('A' + rand.Intn(26))
		}
		return string(s)
	}
	// busy reports whether err is a lock conflict worth retrying.
	busy := func(err error) bool {
		s := strings.ToLower(err.Error())
		return strings.Contains(s, "lock") || strings.Contains(s, "busy")
	}

	db := openDB(b, dsn)
	mustExec(b, db, "CREATE TABLE foo (id INTEGER NOT NULL PRIMARY KEY, name TEXT)")
	fill(b, db, rows, "INSERT INTO foo(name) VALUES(?)", func(int) []any { return []any{randString()} })
	var id int
	var name string
	scanAll(b, db, "SELECT * FROM foo", &id, &name) // warm the cache

	var reads, writes, records atomic.Int64
	records.Store(rows)
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		b.StopTimer()
		start, stop := make(chan struct{}), make(chan struct{})
		var ready, done sync.WaitGroup
		worker := func(work func(db *sql.DB) error) {
			ready.Add(1)
			done.Go(func() {
				db, err := sql.Open(driverName, dsn)
				ready.Done()
				if err != nil {
					b.Error(err)
					return
				}
				defer db.Close()
				<-start
				for {
					select {
					case <-stop:
						return
					default:
					}
					if err := work(db); err != nil && !busy(err) {
						b.Error(err)
						return
					}
				}
			})
		}
		for range readers {
			worker(func(db *sql.DB) error {
				var id int
				var name string
				err := db.QueryRow("SELECT * FROM foo WHERE id=?", rand.Int63n(records.Load())+1).Scan(&id, &name)
				if err == nil {
					reads.Add(1)
				}
				return err
			})
		}
		for range writers {
			worker(func(db *sql.DB) error {
				_, err := db.Exec("INSERT INTO FOO(name) VALUES(?)", randString())
				if err == nil {
					records.Add(1)
					writes.Add(1)
				}
				return err
			})
		}
		ready.Wait()
		time.AfterFunc(time.Second, func() { close(stop) })
		b.StartTimer()
		close(start)
		done.Wait()
	}
	// Each iteration runs for one second.
	switch measure {
	case "reads":
		b.ReportMetric(float64(reads.Load())/float64(b.N), "reads/s")
	case "writes":
		b.ReportMetric(float64(writes.Load())/float64(b.N), "writes/s")
	}
}
