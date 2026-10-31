// Copyright 2016 The Sqlite Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package sqlite // import "github.com/unaquatic/sqlite"

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	sqlite3 "github.com/unaquatic/sqlite/internal/lib"
)

// In-memory databases create no file, whatever parameters their DSN carries.
func TestInMemoryCreatesNoFile(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	for _, dsn := range []string{
		":memory:",
		"file::memory:",
		":memory:?mode=readonly", // the parameter is dropped with the query
		"file::memory:?_pragma=foreign_keys=on",
		":memory:?_pragma=foreign_keys=on",
	} {
		db := openDB(t, dsn)
		mustExec(t, db, `
		create table in_memory_test(i int, f double);
		insert into in_memory_test values(12, 3.14);
		`)
		entries, err := os.ReadDir(dir)
		if err != nil {
			t.Fatal(err)
		}
		if len(entries) != 0 {
			t.Fatalf("%s created %s", dsn, entries[0].Name())
		}
	}
}

// An in-memory database can grow past the 32 MiB its heap was once limited
// to.
// https://gitlab.com/cznic/sqlite/issues/12
func TestInMemoryLargeDatabase(t *testing.T) {
	db := openDB(t, "file::memory:")
	db.SetMaxOpenConns(1)
	mustExec(t, db, `create table t(s string); begin;`)
	s, err := db.Prepare("insert into t values(?)")
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	v := strings.Repeat("a", 1024)
	for i := range (64 << 20) / len(v) {
		if _, err := s.Exec(v); err != nil {
			t.Fatalf("after %d bytes: %v", i*len(v), err)
		}
	}
	mustExec(t, db, `commit;`)
}

// A file: URI passes SQLite its own parameters, here mode, alongside the
// driver's.
// https://gitlab.com/cznic/sqlite/issues/97
func TestReadOnlyDatabase(t *testing.T) {
	name := tempPath(t, "tmp.db")
	mustExec(t, openDB(t, "file:"+name), "create table t(b int)")

	_, err := openDB(t, "file:"+name+"?mode=ro&_txlock=deferred").Exec("drop table t")
	if err == nil || err.Error() != "attempt to write a readonly database (8)" {
		t.Fatalf("got error %v, want a read-only database error", err)
	}
}

// Closing the last connection to a WAL database checkpoints it and removes
// its -wal and -shm files.
// https://gitlab.com/cznic/sqlite/-/issues/171#note_1737746192
func TestWALFilesRemovedOnClose(t *testing.T) {
	path := tempPath(t, "db.db")
	db := openDB(t, "file:"+path+"?_pragma=journal_mode(WAL)")
	mustExec(t, db, `DROP TABLE IF EXISTS "proxy_urls";
	CREATE TABLE "proxy_urls" (
	  "id" integer PRIMARY KEY AUTOINCREMENT,
	  "created_at" datetime,
	  "updated_at" datetime,
	  "deleted_at" datetime,
	  "url" text,
	  "retry" integer,
	  "available" numeric,
	  "timeout" integer DEFAULT 0
	);`)
	for i := range 1000 {
		mustExec(t, db, `INSERT INTO "main"."proxy_urls" ("created_at", "updated_at", "deleted_at", "url", "retry", "available", "timeout") VALUES ('2024-01-23 15:56:11.3099801+08:00', '2024-01-23 15:56:13.7905746+08:00', NULL, ?, 1, 0, 0);`,
			fmt.Sprintf("socks5://127.0.0.%d:7777", i))
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}

	m, err := filepath.Glob(path + "-*")
	if err != nil {
		t.Fatal(err)
	}
	if len(m) != 0 {
		t.Fatalf("files left after Close: %v", m)
	}
}

// An interrupted connection is unusable and gets discarded, unless its
// database is in memory: discarding the connection would lose the database.
// https://gitlab.com/cznic/sqlite/-/issues/196
// https://gitlab.com/cznic/sqlite/-/issues/198
func TestInterruptedConnUsable(t *testing.T) {
	for _, tc := range []struct {
		name     string
		dsn      string
		inMemory bool
	}{
		{"in-memory", "file::memory:?cache=shared", true},
		{"file", "file:" + tempPath(t, "t.db"), false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			db := openDB(t, tc.dsn)
			db.SetMaxOpenConns(1)
			mustExec(t, db, "CREATE TABLE t (v INT)")
			mustExec(t, db, "INSERT INTO t VALUES (1), (2), (3)")

			c, err := db.Conn(context.Background())
			if err != nil {
				t.Fatal(err)
			}
			rawConn(t, c, func(c *conn) {
				if c.inMemory != tc.inMemory {
					t.Fatalf("inMemory = %v, want %v", c.inMemory, tc.inMemory)
				}
				if !c.usable() {
					t.Fatal("a fresh connection is not usable")
				}
				sqlite3.Xsqlite3_interrupt(c.tls, c.db)
				if c.usable() != tc.inMemory {
					t.Errorf("usable after interrupt = %v, want %v", c.usable(), tc.inMemory)
				}
			})
			c.Close()

			if n := queryValue[int](t, db, "SELECT count(*) FROM t"); n != 3 {
				t.Fatalf("got %d rows, want 3", n)
			}
		})
	}
}
