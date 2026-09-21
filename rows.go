// Copyright 2025 The Sqlite Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package sqlite // import "github.com/unaquatic/sqlite"

import (
	"database/sql/driver"
	"fmt"
	"io"

	sqlite3 "github.com/unaquatic/sqlite/internal/lib"
)

var _ driver.Rows = (*rows)(nil)

// rows reads the rows of a statement that has already been stepped once.
type rows struct {
	c     *conn
	pstmt uintptr

	// allocs is the C memory of the statement's bound arguments, freed on
	// Close.
	allocs []uintptr

	columns []string

	empty     bool // no row is ready, nor will be
	doStep    bool // Next must step to reach the next row
	reuseStmt bool // Close resets the statement instead of finalizing it
}

// newRows returns the rows of pstmt, which has been stepped once and is
// positioned on its first row unless empty is set. The rows take ownership
// of allocs.
func newRows(c *conn, pstmt uintptr, allocs []uintptr, empty bool) *rows {
	r := &rows{
		c:       c,
		pstmt:   pstmt,
		allocs:  allocs,
		columns: make([]string, c.columnCount(pstmt)),
		empty:   empty,
	}
	for i := range r.columns {
		r.columns[i] = c.columnName(pstmt, i)
	}
	return r
}

// Close implements driver.Rows.
func (r *rows) Close() error {
	r.c.freeAllocs(r.allocs)
	r.allocs = nil

	pstmt := r.pstmt
	if pstmt == 0 {
		return nil
	}
	r.pstmt = 0
	if !r.reuseStmt {
		return r.c.finalize(pstmt)
	}
	if err := r.c.reset(pstmt); err != nil {
		return err
	}
	return r.c.clearBindings(pstmt)
}

// Columns implements driver.Rows.
func (r *rows) Columns() []string { return r.columns }

// Next implements driver.Rows.
func (r *rows) Next(dest []driver.Value) error {
	if r.empty {
		return io.EOF
	}
	if r.doStep {
		row, err := r.c.step(r.pstmt)
		if err != nil {
			return err
		}
		if !row {
			r.empty = true
			return io.EOF
		}
	}
	r.doStep = true

	if len(dest) != len(r.columns) {
		return fmt.Errorf("sqlite: Next: have %v destination values, expected %v", len(dest), len(r.columns))
	}
	for i := range dest {
		v, err := r.value(i)
		if err != nil {
			return err
		}
		dest[i] = v
	}
	return nil
}

// value returns column i of the current row as the Go type of its storage
// class. SQLite has no time type, so a time reads back as the text or number
// it was stored as.
func (r *rows) value(i int) (driver.Value, error) {
	c, pstmt := r.c, r.pstmt
	switch t := c.columnType(pstmt, i); t {
	case sqlite3.SQLITE_INTEGER:
		return c.columnInt64(pstmt, i), nil
	case sqlite3.SQLITE_FLOAT:
		return c.columnDouble(pstmt, i), nil
	case sqlite3.SQLITE_TEXT:
		return c.columnText(pstmt, i), nil
	case sqlite3.SQLITE_BLOB:
		return c.columnBlob(pstmt, i), nil
	case sqlite3.SQLITE_NULL:
		return nil, nil
	default:
		return nil, fmt.Errorf("sqlite: column %d has unknown storage class %d", i, t)
	}
}
