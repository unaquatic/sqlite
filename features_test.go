// Copyright 2026 The Sqlite Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package sqlite // import "github.com/unaquatic/sqlite"

// Tests of SQLite features that depend on how it was built or on the C
// library it runs on, rather than on the driver.

import (
	"testing"
)

// This fork removed the C strftime shim (libc.Xstrftime) with the
// github.com/ncruces/go-strftime dependency, because SQLite implements the
// SQL date and time functions itself. If that were wrong, these would break
// first.
func TestSQLDateTimeFunctions(t *testing.T) {
	db := openDB(t, ":memory:")
	for _, tc := range []struct{ name, query, want string }{
		{"date", `SELECT date('2024-01-15')`, "2024-01-15"},
		{"time", `SELECT time('2024-01-15 13:45:56')`, "13:45:56"},
		{"datetime", `SELECT datetime('2024-01-15 13:45:56')`, "2024-01-15 13:45:56"},
		{"unixepoch", `SELECT unixepoch('2024-01-15')`, "1705276800"},

		// Every strftime specifier SQLite documents.
		{"%Y", `SELECT strftime('%Y', '2024-01-15 13:45:56')`, "2024"},
		{"%m", `SELECT strftime('%m', '2024-01-15 13:45:56')`, "01"},
		{"%d", `SELECT strftime('%d', '2024-01-15 13:45:56')`, "15"},
		{"%H", `SELECT strftime('%H', '2024-01-15 13:45:56')`, "13"},
		{"%M", `SELECT strftime('%M', '2024-01-15 13:45:56')`, "45"},
		{"%S", `SELECT strftime('%S', '2024-01-15 13:45:56')`, "56"},
		{"%j", `SELECT strftime('%j', '2024-01-15')`, "015"},
		{"%w", `SELECT strftime('%w', '2024-01-15')`, "1"},
		{"%W", `SELECT strftime('%W', '2024-01-15')`, "03"},
		{"%s", `SELECT strftime('%s', '2024-01-15')`, "1705276800"},
		{"%f", `SELECT strftime('%f', '2024-01-15 13:45:56')`, "56.000"},
		{"combined", `SELECT strftime('%Y-%m-%dT%H:%M:%S', '2024-01-15 13:45:56')`, "2024-01-15T13:45:56"},

		// Modifiers exercise the date arithmetic.
		{"+1 day", `SELECT date('2024-01-15', '+1 day')`, "2024-01-16"},
		{"leap day", `SELECT date('2024-02-28', '+1 day')`, "2024-02-29"},
		{"month end", `SELECT date('2024-01-31', '+1 month')`, "2024-03-02"},
		{"start of month", `SELECT date('2024-01-15', 'start of month')`, "2024-01-01"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := queryValue[string](t, db, tc.query); got != tc.want {
				t.Errorf("%s = %q, want %q", tc.query, got, tc.want)
			}
		})
	}

	// 2024-01-15 00:00 UTC is Julian day 2460324.5.
	if jd := queryValue[float64](t, db, `SELECT julianday('2024-01-15')`); jd != 2460324.5 {
		t.Errorf("julianday('2024-01-15') = %v, want 2460324.5", jd)
	}

	// 'now' and 'localtime' read the clock and the zone through the C
	// library.
	if now := queryValue[string](t, db, `SELECT strftime('%Y-%m-%d %H:%M:%S', 'now')`); len(now) != len("2006-01-02 15:04:05") {
		t.Errorf("strftime('now') = %q", now)
	}
	// https://gitlab.com/cznic/sqlite/-/issues/49
	mustExec(t, db, "select datetime('now', 'localtime')")
}

// The build enables SQLITE_ENABLE_DBPAGE_VTAB.
func TestDBPageVtab(t *testing.T) {
	db := openDB(t, ":memory:")
	db.SetMaxOpenConns(1)
	mustExec(t, db, `
		CREATE TABLE test_dbpage (id INTEGER PRIMARY KEY, val TEXT);
		INSERT INTO test_dbpage (val) VALUES ('hello dbpage');
	`)

	var pgno int
	var data []byte
	if err := db.QueryRow("SELECT pgno, data FROM sqlite_dbpage WHERE pgno = 1").Scan(&pgno, &data); err != nil {
		t.Fatal(err)
	}
	if pgno != 1 {
		t.Errorf("got page %d, want 1", pgno)
	}
	const header = "SQLite format 3\x00"
	if len(data) < len(header) || string(data[:len(header)]) != header {
		t.Errorf("page 1 does not start with the SQLite header: %q", data[:min(len(data), 16)])
	}
}
