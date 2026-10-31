// Copyright 2016 The Sqlite Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package sqlite // import "github.com/unaquatic/sqlite"

import (
	"database/sql"
	"fmt"
	"reflect"
	"strings"
	"testing"
)

// Columns are reported whether or not there are rows.
// https://gitlab.com/cznic/sqlite/-/issues/32
func TestColumns(t *testing.T) {
	for _, insert := range []bool{true, false} {
		t.Run(fmt.Sprintf("rows=%v", insert), func(t *testing.T) {
			db := openDB(t, "file::memory:")
			mustExec(t, db, "create table t1(a integer, b text, c blob)")
			if insert {
				mustExec(t, db, "insert into t1 (a) values (1)")
			}
			rows, err := db.Query("select * from t1")
			if err != nil {
				t.Fatal(err)
			}
			defer rows.Close()
			got, err := rows.Columns()
			if err != nil {
				t.Fatal(err)
			}
			if want := []string{"a", "b", "c"}; !reflect.DeepEqual(got, want) {
				t.Errorf("got columns %v, want %v", got, want)
			}
		})
	}
}

// TestColumnText reads text of several lengths, empty and non-ASCII
// included. A row's text must not change when the next row is read, as it
// would if it pointed into SQLite's memory instead of being copied.
func TestColumnText(t *testing.T) {
	db := openDB(t, "file::memory:")
	mustExec(t, db, `CREATE TABLE t (id INTEGER PRIMARY KEY, s TEXT)`)
	want := []string{"hello", "", "unicode é 中文 \U0001F600", strings.Repeat("a1B2c3D4", 256)}
	for i, s := range want {
		mustExec(t, db, `INSERT INTO t(id, s) VALUES (?, ?)`, i, s)
	}
	for i, s := range want {
		if got := queryValue[string](t, db, `SELECT s FROM t WHERE id = ?`, i); got != s {
			t.Errorf("id %d: got %q, want %q", i, got, s)
		}
	}

	rows, err := db.Query(`SELECT s FROM t ORDER BY id`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var got []string
	for rows.Next() {
		var s string
		if err := rows.Scan(&s); err != nil {
			t.Fatal(err)
		}
		got = append(got, s)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %q, want %q", got, want)
	}
}

// A NULL scans into sql.Null types as invalid.
func TestNullColumn(t *testing.T) {
	db := openDB(t, ":memory:")
	v := queryValue[sql.NullString](t, db, "SELECT NULL")
	if v.Valid {
		t.Fatalf("got %+v, want NULL", v)
	}
}
