// Copyright 2016 The Sqlite Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package sqlite // import "github.com/unaquatic/sqlite"

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"
)

// bindRunners query through the database and through a prepared statement,
// which take different paths to bind.
var bindRunners = []struct {
	name  string
	query func(t *testing.T, db *sql.DB, query string, args ...any) *sql.Row
}{
	{"DB", func(_ *testing.T, db *sql.DB, query string, args ...any) *sql.Row {
		return db.QueryRow(query, args...)
	}},
	{"Prepare", func(t *testing.T, db *sql.DB, query string, args ...any) *sql.Row {
		s, err := db.Prepare(query)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { s.Close() })
		return s.QueryRow(args...)
	}},
}

// https://sqlite.org/lang_expr.html#varparam
// https://gitlab.com/cznic/sqlite/-/issues/42
func TestBinding(t *testing.T) {
	for _, run := range bindRunners {
		t.Run(run.name, func(t *testing.T) {
			db := openDB(t, "file::memory:")
			for _, tc := range []struct {
				q    string
				args []any
				want []int
			}{
				{"?, ?, ?", []any{1, 2, 3}, []int{1, 2, 3}},
				{"?1, ?2, ?3", []any{1, 2, 3}, []int{1, 2, 3}},
				{"?1, ?, ?3", []any{1, 2, 3}, []int{1, 2, 3}},
				{"?3, ?2, ?1", []any{1, 2, 3}, []int{3, 2, 1}},
				{"?1, ?1, ?2", []any{1, 2}, []int{1, 1, 2}},
				// Named parameters bind by position too.
				{":one, :two, :three", []any{1, 2, 3}, []int{1, 2, 3}},
				{"$1, $2, $3", []any{1, 2, 3}, []int{1, 2, 3}},
				{"@one, @one, @two", []any{1, 2}, []int{1, 1, 2}},
			} {
				got := make([]int, len(tc.want))
				dest := make([]any, len(got))
				for i := range got {
					dest[i] = &got[i]
				}
				if err := run.query(t, db, "select "+tc.q, tc.args...).Scan(dest...); err != nil {
					t.Errorf("select %s %v: %v", tc.q, tc.args, err)
					continue
				}
				if !reflect.DeepEqual(got, tc.want) {
					t.Errorf("select %s %v = %v, want %v", tc.q, tc.args, got, tc.want)
				}
			}
		})
	}
}

func TestBindingError(t *testing.T) {
	for _, run := range bindRunners {
		t.Run(run.name, func(t *testing.T) {
			db := openDB(t, "file::memory:")
			for _, tc := range []struct {
				q    string
				args []any
				want string
			}{
				{"?", []any{}, "missing argument with index 1"},
				{"?500, ?", []any{1, 2}, "missing argument with index 3"},
				{":one, :two", []any{1}, "missing argument with index 2"},
				{":one", []any{sql.Named("one", 1)}, `sqlite: named argument "one": only positional arguments are supported`},
			} {
				var a, b int
				err := run.query(t, db, "select "+tc.q, tc.args...).Scan(&a, &b)
				if err == nil || err.Error() != tc.want {
					t.Errorf("select %s %v: got error %v, want %q", tc.q, tc.args, err, tc.want)
				}
			}
		})
	}
}

func TestNullBinding(t *testing.T) {
	db := openDB(t, "file::memory:")
	mustExec(t, db, `
	CREATE TABLE table1 (field1 varchar NULL);
	INSERT INTO table1 (field1) VALUES (?);
	`, sql.NullString{})
}

// TestValueRoundTrip writes each kind of argument and reads it back. A time
// comes back as the text it was stored as.
func TestValueRoundTrip(t *testing.T) {
	db := tempDB(t)
	t1 := time.Date(2017, 4, 20, 1, 2, 3, 56789, time.UTC)
	t2 := time.Date(2018, 5, 21, 2, 3, 4, 98765, time.UTC)
	r := mustExec(t, db, `
	create table t(i int, f double, b bool, s text, t time);
	insert into t values(12, 3.14, ?, 'foo', ?), (34, 2.78, ?, 'bar', ?);
	`, true, t1, false, t2)
	if n, err := r.RowsAffected(); err != nil || n != 2 {
		t.Fatalf("RowsAffected = %d, %v; want 2", n, err)
	}

	type rec struct {
		i int
		f float64
		b bool
		s string
		t string
	}
	rows, err := db.Query("select * from t")
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var got []rec
	for rows.Next() {
		var r rec
		if err := rows.Scan(&r.i, &r.f, &r.b, &r.s, &r.t); err != nil {
			t.Fatal(err)
		}
		got = append(got, r)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	want := []rec{{12, 3.14, true, "foo", t1.Format(timeFormat)}, {34, 2.78, false, "bar", t2.Format(timeFormat)}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
}

// A time is stored as text that reads back as the same instant and that
// SQLite's date and time functions understand, whatever the column's
// declared type.
func TestTimeArgument(t *testing.T) {
	db := openDB(t, ":memory:")
	mustExec(t, db, "CREATE TABLE t (d DATETIME)")
	tm := time.Date(2021, 1, 2, 16, 39, 17, 123456789, time.FixedZone("", -5*3600))
	mustExec(t, db, "INSERT INTO t VALUES (?)", tm)

	s := queryValue[string](t, db, "SELECT d FROM t")
	if want := "2021-01-02 16:39:17.123456789-05:00"; s != want {
		t.Fatalf("stored %q, want %q", s, want)
	}
	if got, err := time.Parse(timeFormat, s); err != nil || !got.Equal(tm) {
		t.Fatalf("parsed back %v, %v; want %v", got, err, tm)
	}
	if got, want := queryValue[string](t, db, "SELECT datetime(d) FROM t"), "2021-01-02 21:39:17"; got != want {
		t.Fatalf("datetime(d) = %q, want %q", got, want)
	}
}

func TestBlobRoundTrip(t *testing.T) {
	db := tempDB(t)
	want := [][]byte{[]byte(time.Now().String()), []byte("\x00foo\x00bar\x00")}
	mustExec(t, db, `
	create table t(b blob);
	insert into t values(?), (?);
	`, want[0], want[1])

	rows, err := db.Query("select * from t")
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var got [][]byte
	for rows.Next() {
		var b []byte
		if err := rows.Scan(&b); err != nil {
			t.Fatal(err)
		}
		got = append(got, b)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	if len(got) != len(want) || !bytes.Equal(got[0], want[0]) || !bytes.Equal(got[1], want[1]) {
		t.Fatalf("got %q, want %q", got, want)
	}
}

// An empty []byte is an empty blob, not NULL.
// https://gitlab.com/cznic/sqlite/issues/98
func TestEmptyBlobIsNotNull(t *testing.T) {
	db := tempDB(t)
	mustExec(t, db, "create table t(b mediumblob not null)")
	mustExec(t, db, "insert into t values (?)", []byte{})
	if _, err := db.Exec("insert into t values (?)", nil); err == nil {
		t.Fatal("inserting NULL into a NOT NULL column succeeded")
	}
}

// A nil []byte is NULL.
// https://gitlab.com/cznic/sqlite/issues/100
func TestNilBlobIsNull(t *testing.T) {
	db := openDB(t, ":memory:")
	mustExec(t, db, `CREATE TABLE t1(v TEXT)`)
	mustExec(t, db, `INSERT INTO t1(v) VALUES(?)`, []byte(nil))
	if v := queryValue[sql.NullByte](t, db, `SELECT v FROM t1 LIMIT 1`); v.Valid {
		t.Fatalf("got non-NULL %+v", v)
	}

	mustExec(t, db, `CREATE TABLE t2(v TEXT check(v is NULL OR(json_valid(v) AND json_type(v)='array')))`)
	for _, v := range [][]byte{nil, []byte(`["a"]`)} {
		mustExec(t, db, `INSERT INTO t2(v) VALUES(?)`, v)
	}
}

// SQL holding no statement returns no columns and no rows, and executes as a
// statement that changed nothing.
// https://gitlab.com/cznic/sqlite/issues/152
// https://gitlab.com/cznic/sqlite/issues/153
func TestNoStatement(t *testing.T) {
	ctx := context.Background()
	for _, query := range []string{"SELECT 1 WHERE false", "-- just a comment", ""} {
		t.Run(query, func(t *testing.T) {
			db := openDB(t, ":memory:")
			c, err := db.Conn(ctx)
			if err != nil {
				t.Fatal(err)
			}
			defer c.Close()

			for _, q := range []func() (*sql.Rows, error){
				func() (*sql.Rows, error) { return db.Query(query) },
				func() (*sql.Rows, error) { return c.QueryContext(ctx, query) },
			} {
				rows, err := q()
				if err != nil {
					t.Fatal(err)
				}
				for rows.Next() {
					t.Error("got a row")
				}
				if err := rows.Err(); err != nil {
					t.Fatal(err)
				}
				rows.Close()
			}

			r, err := db.Exec(query)
			if err != nil {
				t.Fatal(err)
			}
			if n, err := r.RowsAffected(); err != nil || n != 0 {
				t.Fatalf("RowsAffected = %d, %v; want 0", n, err)
			}
		})
	}
}

// https://gitlab.com/cznic/sqlite/-/issues/70
func TestQueryAfterRowsClose(t *testing.T) {
	db := openDB(t, "file::memory:")
	mustExec(t, db, `create table t (foo)`)
	for range 2 {
		r, err := db.Query("select * from t")
		if err != nil {
			t.Fatal(err)
		}
		if err := r.Close(); err != nil {
			t.Fatal(err)
		}
	}
}

// Query runs a statement that returns no rows, such as DDL, all the same.
func TestQueryStatementWithoutRows(t *testing.T) {
	db := tempDB(t)
	s, err := db.Prepare("create table t(i);")
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	rows, err := s.Query()
	if err != nil {
		t.Fatal(err)
	}
	rows.Close()
}

// https://gitlab.com/cznic/sqlite/-/issues/28
func TestQueryRowNoRows(t *testing.T) {
	db := tempDB(t)
	mustExec(t, db, `CREATE TABLE test (foo TEXT)`)
	var foo string
	if err := db.QueryRow(`SELECT foo FROM test`).Scan(&foo); err != sql.ErrNoRows {
		t.Fatalf("got %v, want %v", err, sql.ErrNoRows)
	}
}

// A script of quoted-identifier DDL and inserts, run on a database named by
// a path relative to the working directory.
// https://gitlab.com/cznic/sqlite/issues/19
func TestScriptWithRelativePath(t *testing.T) {
	t.Chdir(t.TempDir())
	db := openDB(t, "test.db")
	db.SetMaxOpenConns(1)

	mustExec(t, db, `drop table if exists products;`)
	mustExec(t, db, `
CREATE TABLE IF NOT EXISTS "products" (
	"id"	VARCHAR(255),
	"user_id"	VARCHAR(255),
	"name"	VARCHAR(255),
	"description"	VARCHAR(255),
	"created_at"	BIGINT,
	"credits_price"	BIGINT,
	"enabled"	BOOLEAN,
	PRIMARY KEY("id")
);`)
	mustExec(t, db, `
INSERT INTO "products" ("id", "user_id", "name", "description", "created_at", "credits_price", "enabled") VALUES ('9be4398c-d527-4efb-93a4-fc532cbaf804', '16935690-348b-41a6-bb20-f8bb16011015', 'dqdwqdwqdwqwqdwqd', 'qwdwqwqdwqdwqdwqd', '1577448686', '1', '0');
INSERT INTO "products" ("id", "user_id", "name", "description", "created_at", "credits_price", "enabled") VALUES ('759f10bd-9e1d-4ec7-b764-0868758d7b85', '16935690-348b-41a6-bb20-f8bb16011015', 'qdqwqwdwqdwqdwqwqd', 'wqdwqdwqdwqdwqdwq', '1577448692', '1', '1');
INSERT INTO "products" ("id", "user_id", "name", "description", "created_at", "credits_price", "enabled") VALUES ('512956e7-224d-4b2a-9153-b83a52c4aa38', '16935690-348b-41a6-bb20-f8bb16011015', 'qwdwqwdqwdqdwqwqd', 'wqdwdqwqdwqdwqdwqdwqdqw', '1577448699', '2', '1');
INSERT INTO "products" ("id", "user_id", "name", "description", "created_at", "credits_price", "enabled") VALUES ('02cd138f-6fa6-4909-9db7-a9d0eca4a7b7', '16935690-348b-41a6-bb20-f8bb16011015', 'qdwqdwqdwqwqdwdq', 'wqddwqwqdwqdwdqwdqwq', '1577448706', '3', '1');
`)

	const user = "16935690-348b-41a6-bb20-f8bb16011015"
	if n := queryValue[int](t, db, "select count(*) from products where user_id = ?", user); n != 4 {
		t.Fatalf("count(*) = %d, want 4", n)
	}
	countRows := func(query string, arg any) int {
		rows, err := db.Query(query, arg)
		if err != nil {
			t.Fatal(err)
		}
		defer rows.Close()
		n := 0
		for rows.Next() {
			n++
		}
		if err := rows.Err(); err != nil {
			t.Fatal(err)
		}
		return n
	}
	if n := countRows("select * from products where user_id = ?", user); n != 4 {
		t.Fatalf("selected %d rows, want 4", n)
	}
	if n := countRows("select * from products where enabled = ?", true); n != 3 {
		t.Fatalf("selected %d enabled rows, want 3", n)
	}
}

// QueryRow on a script returns the rows of its last statement.
// https://gitlab.com/cznic/sqlite/-/issues/53
func TestScriptQueryRow(t *testing.T) {
	t.Chdir(t.TempDir())
	db := openDB(t, "testissue53.sqlite")
	mustExec(t, db, `
CREATE TABLE IF NOT EXISTS loginst (
     instid INTEGER PRIMARY KEY,
     name   VARCHAR UNIQUE
);`)

	tx, err := db.Begin()
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	for i := range 5000 {
		x := fmt.Sprintf("foo%d", i)
		var id int
		if err := tx.QueryRow("INSERT OR IGNORE INTO loginst (name) VALUES (?); SELECT instid FROM loginst WHERE name = ?", x, x).Scan(&id); err != nil {
			t.Fatal(err)
		}
	}
}

// A writer on another connection cannot get the lock while unread rows hold
// a read transaction open: SQLite returns SQLITE_BUSY rather than wait
// forever, as an older implementation of step did.
// https://gitlab.com/cznic/sqlite/-/issues/66
func TestOpenRowsBlockWriters(t *testing.T) {
	db := tempDB(t)
	mustExec(t, db, `CREATE TABLE IF NOT EXISTS verdictcache (sha1 text);`)
	mustExec(t, db, "INSERT OR REPLACE INTO verdictcache (sha1) VALUES (?)", "a")

	rows, err := db.Query("SELECT * FROM verdictcache WHERE sha1=?", "a")
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()

	if _, err := db.Exec("INSERT OR REPLACE INTO verdictcache (sha1) VALUES (?)", "b"); err != nil {
		t.Logf("insert with rows open: %v", err)
		if !strings.Contains(err.Error(), "database is locked (5) (SQLITE_BUSY)") {
			t.Fatal(err)
		}
	}
}

// TestMultiStmtQueryStringRoundtrip runs a script whose first statement binds
// a string, then binds a fresh string straight away, which reuses the C
// memory a double free of the first one would have corrupted. Run with -race,
// whose checkptr catches the double free reliably.
func TestMultiStmtQueryStringRoundtrip(t *testing.T) {
	ctx := context.Background()
	db := openDB(t, "file::memory:")
	db.SetMaxOpenConns(1) // reuse the connection so corruption accumulates
	c, err := db.Conn(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	mustExec(t, c, "CREATE TABLE t(v TEXT)")

	val := "iter-" + strings.Repeat("x", 1024)
	rows, err := c.QueryContext(ctx, "INSERT INTO t VALUES(?); SELECT v FROM t WHERE rowid = last_insert_rowid()", val)
	if err != nil {
		t.Fatal(err)
	}
	var got string
	if rows.Next() {
		if err := rows.Scan(&got); err != nil {
			t.Fatal(err)
		}
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	if err := rows.Close(); err != nil {
		t.Fatal(err)
	}
	if got != val {
		t.Fatalf("got %d bytes, want %d", len(got), len(val))
	}

	check := "check-" + strings.Repeat("y", 1024)
	if got := queryValue[string](t, c, "SELECT ?", check); got != check {
		t.Fatalf("data corruption: got %d bytes, want %d", len(got), len(check))
	}
}

// Exec steps a statement with RETURNING to completion, so that it affects
// every row and not only the first.
func TestExecReturningMultiRow(t *testing.T) {
	db := openDB(t, ":memory:")
	mustExec(t, db, "CREATE TABLE t (id INTEGER PRIMARY KEY, v TEXT)")

	for _, tc := range []struct {
		query string
		check func() error
	}{
		{"INSERT INTO t (id, v) VALUES (1,'a'), (2,'b'), (3,'c') RETURNING id", func() error {
			if n := queryValue[int](t, db, "SELECT count(*) FROM t"); n != 3 {
				return fmt.Errorf("%d rows in the table, want 3", n)
			}
			return nil
		}},
		{"UPDATE t SET v = v || '!' RETURNING id", func() error {
			if s := queryValue[string](t, db, "SELECT group_concat(v, ' ') FROM (SELECT v FROM t ORDER BY id)"); s != "a! b! c!" {
				return fmt.Errorf("values %q, want %q", s, "a! b! c!")
			}
			return nil
		}},
		{"DELETE FROM t RETURNING id", func() error {
			if n := queryValue[int](t, db, "SELECT count(*) FROM t"); n != 0 {
				return fmt.Errorf("%d rows in the table, want 0", n)
			}
			return nil
		}},
	} {
		r := mustExec(t, db, tc.query)
		if n, err := r.RowsAffected(); err != nil || n != 3 {
			t.Errorf("%s: RowsAffected = %d, %v; want 3", tc.query, n, err)
		}
		if err := tc.check(); err != nil {
			t.Errorf("%s: %v", tc.query, err)
		}
	}
}

// The same holds for the last statement of a script.
func TestExecReturningMultiStatement(t *testing.T) {
	db := openDB(t, ":memory:")
	mustExec(t, db, "CREATE TABLE t (id INTEGER PRIMARY KEY, v TEXT)")
	r := mustExec(t, db, "SELECT 1; INSERT INTO t (id, v) VALUES (1,'a'), (2,'b'), (3,'c') RETURNING id")
	if n := queryValue[int](t, db, "SELECT count(*) FROM t"); n != 3 {
		t.Fatalf("%d rows in the table, want 3", n)
	}
	if n, err := r.RowsAffected(); err != nil || n != 3 {
		t.Errorf("RowsAffected = %d, %v; want 3", n, err)
	}
}

// https://gitlab.com/cznic/sqlite/-/issues/73
func TestConstraintErrors(t *testing.T) {
	for _, tc := range []struct{ column, want string }{
		{"hashval TEXT PRIMARY KEY NOT NULL", "constraint failed: UNIQUE constraint failed: hash.hashval (1555)"},
		{"hashval TEXT UNIQUE", "constraint failed: UNIQUE constraint failed: hash.hashval (2067)"},
	} {
		t.Run(tc.column, func(t *testing.T) {
			db := openDB(t, "file::memory:")
			mustExec(t, db, "CREATE TABLE hash ("+tc.column+")")
			mustExec(t, db, "INSERT INTO hash (hashval) VALUES (?)", "somehashval")
			_, err := db.Exec("INSERT INTO hash (hashval) VALUES (?)", "somehashval")
			if err == nil || err.Error() != tc.want {
				t.Fatalf("got error %v, want %q", err, tc.want)
			}
			var e *Error
			if !errors.As(err, &e) {
				t.Fatalf("got %T, want *Error", err)
			}
		})
	}
}
