// Copyright 2025 The Sqlite Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package sqlite // import "github.com/unaquatic/sqlite"

import (
	"context"
	"database/sql/driver"

	"github.com/unaquatic/sqlite/internal/libc"
)

var (
	_ driver.Stmt             = (*stmt)(nil)
	_ driver.StmtExecContext  = (*stmt)(nil)
	_ driver.StmtQueryContext = (*stmt)(nil)
	_ driver.Result           = (*result)(nil)
)

// stmt is SQL text that can be run repeatedly. When the text holds exactly
// one statement it is prepared once, in pstmt, and reset between runs. Any
// other text, a script of several statements or text with no statement at
// all, is prepared afresh statement by statement on every run.
type stmt struct {
	c     *conn
	psql  uintptr // the SQL text, as a C string
	pstmt uintptr // the prepared statement, or 0 for a script
}

func newStmt(c *conn, sql string) (*stmt, error) {
	p, err := libc.CString(sql)
	if err != nil {
		return nil, err
	}

	tail := p
	pstmt, err := c.prepare(&tail)
	if err != nil {
		c.free(p)
		return nil, err
	}

	s := &stmt{c: c, psql: p}
	switch {
	case pstmt == 0:
		// No statement before the tail: a script, or no SQL at all.
	case atEnd(tail):
		s.pstmt = pstmt
	default:
		// A script. Run prepares its statements one at a time.
		if err := c.finalize(pstmt); err != nil {
			c.free(p)
			return nil, err
		}
	}
	return s, nil
}

// Close implements driver.Stmt.
func (s *stmt) Close() (err error) {
	if s.pstmt != 0 {
		err = s.c.finalize(s.pstmt)
		s.pstmt = 0
	}
	if s.psql != 0 {
		s.c.free(s.psql)
		s.psql = 0
	}
	return err
}

// NumInput implements driver.Stmt. It returns -1, so database/sql leaves
// checking the number of arguments to bind.
func (s *stmt) NumInput() int { return -1 }

// Exec implements driver.Stmt.
//
// Deprecated: database/sql calls ExecContext.
func (s *stmt) Exec(args []driver.Value) (driver.Result, error) {
	return s.ExecContext(context.Background(), toNamedValues(args))
}

// Query implements driver.Stmt.
//
// Deprecated: database/sql calls QueryContext.
func (s *stmt) Query(args []driver.Value) (driver.Rows, error) {
	return s.QueryContext(context.Background(), toNamedValues(args))
}

// ExecContext implements driver.StmtExecContext.
func (s *stmt) ExecContext(ctx context.Context, args []driver.NamedValue) (driver.Result, error) {
	return s.exec(ctx, args)
}

// QueryContext implements driver.StmtQueryContext.
func (s *stmt) QueryContext(ctx context.Context, args []driver.NamedValue) (driver.Rows, error) {
	r, err := s.query(ctx, args)
	if err != nil {
		return nil, err
	}
	return r, nil
}

func toNamedValues(vals []driver.Value) []driver.NamedValue {
	r := make([]driver.NamedValue, len(vals))
	for i, v := range vals {
		r[i] = driver.NamedValue{Ordinal: i + 1, Value: v}
	}
	return r
}

// exec runs every statement of s and returns the result of the last one.
func (s *stmt) exec(ctx context.Context, args []driver.NamedValue) (driver.Result, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	w := s.c.watchCancel(ctx)
	defer w.stop()

	var r driver.Result
	var err error
	if s.pstmt != 0 {
		r, err = s.c.execOne(ctx, w, s.pstmt, args)
		if e := s.c.reset(s.pstmt); err == nil {
			err = e
		}
		if e := s.c.clearBindings(s.pstmt); err == nil {
			err = e
		}
	} else {
		r, err = s.execScript(ctx, w, args)
	}
	if w.cancelled() {
		return nil, ctx.Err()
	}
	return r, err
}

func (s *stmt) execScript(ctx context.Context, w *cancelWatch, args []driver.NamedValue) (driver.Result, error) {
	var r driver.Result
	for psql := s.psql; !atEnd(psql) && !w.cancelled(); {
		pstmt, err := s.c.prepare(&psql)
		if err != nil {
			return nil, err
		}
		if pstmt == 0 {
			continue
		}
		r, err = s.c.execOne(ctx, w, pstmt, args)
		if e := s.c.finalize(pstmt); err == nil {
			err = e
		}
		if err != nil {
			return nil, err
		}
	}
	if r == nil {
		// The SQL held no statement. database/sql would hand a nil Result to
		// its caller, whose RowsAffected would then panic.
		r = &result{}
	}
	return r, nil
}

// execOne binds args to pstmt and steps it to completion. Rows it returns,
// as a SELECT or a statement with RETURNING does, are read and dropped, as
// sqlite3_exec does: an INSERT ... RETURNING stopped at its first row would
// insert only that row.
func (c *conn) execOne(ctx context.Context, w *cancelWatch, pstmt uintptr, args []driver.NamedValue) (driver.Result, error) {
	allocs, err := c.bind(pstmt, args)
	if err != nil {
		return nil, err
	}
	defer c.freeAllocs(allocs)

	for {
		row, err := c.step(pstmt)
		if err != nil {
			return nil, err
		}
		if !row {
			return c.result(), nil
		}
		if w.cancelled() {
			return nil, ctx.Err()
		}
	}
}

// query runs s and returns the rows of its last statement.
func (s *stmt) query(ctx context.Context, args []driver.NamedValue) (*rows, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	w := s.c.watchCancel(ctx)
	defer w.stop()

	var r *rows
	var err error
	if s.pstmt != 0 {
		r, err = s.queryPrepared(args)
	} else {
		r, err = s.queryScript(w, args)
	}
	if w.cancelled() {
		if r != nil {
			r.Close()
		}
		return nil, ctx.Err()
	}
	return r, err
}

// queryPrepared runs s.pstmt to its first row. The rows it returns reset the
// statement on Close, for the next run.
func (s *stmt) queryPrepared(args []driver.NamedValue) (*rows, error) {
	c := s.c
	allocs, err := c.bind(s.pstmt, args)
	if err == nil {
		var row bool
		if row, err = c.step(s.pstmt); err == nil {
			r := newRows(c, s.pstmt, allocs, !row)
			r.reuseStmt = true
			return r, nil
		}
		c.freeAllocs(allocs)
	}
	c.reset(s.pstmt)
	c.clearBindings(s.pstmt)
	return nil, err
}

// queryScript runs the statements of a script in turn and returns rows for
// one of them: the last statement that returned a row, unless the script
// ends with a statement that returned none, whose empty rows are returned
// instead. A statement that returns rows is only stepped to its first row
// before the script moves on.
func (s *stmt) queryScript(w *cancelWatch, args []driver.NamedValue) (*rows, error) {
	c := s.c
	var r *rows
	fail := func(err error) (*rows, error) {
		if r != nil {
			r.Close()
		}
		return nil, err
	}

	for psql := s.psql; !atEnd(psql) && !w.cancelled(); {
		pstmt, err := c.prepare(&psql)
		if err != nil {
			return fail(err)
		}
		if pstmt == 0 {
			continue
		}

		allocs, err := c.bind(pstmt, args)
		if err != nil {
			c.finalize(pstmt)
			return fail(err)
		}
		row, err := c.step(pstmt)
		if err != nil {
			c.freeAllocs(allocs)
			c.finalize(pstmt)
			return fail(err)
		}

		if row || r == nil || atEnd(psql) {
			if r != nil {
				r.Close()
			}
			r = newRows(c, pstmt, allocs, !row)
			continue
		}
		c.freeAllocs(allocs)
		if err := c.finalize(pstmt); err != nil {
			return fail(err)
		}
	}

	if r == nil {
		// The SQL held no statement: no columns, no rows.
		r = newRows(c, 0, nil, true)
	}
	return r, nil
}

// result implements driver.Result.
type result struct {
	lastInsertID int64
	rowsAffected int64
}

// LastInsertId implements driver.Result.
func (r *result) LastInsertId() (int64, error) { return r.lastInsertID, nil }

// RowsAffected implements driver.Result.
func (r *result) RowsAffected() (int64, error) { return r.rowsAffected, nil }
