// Copyright 2025 The Sqlite Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package sqlite // import "github.com/unaquatic/sqlite"

import (
	"context"
	"database/sql/driver"

	sqlite3 "github.com/unaquatic/sqlite/internal/lib"
	"github.com/unaquatic/sqlite/internal/libc"
)

var _ driver.Tx = (*tx)(nil)

// tx is a transaction on a connection.
type tx struct {
	c *conn
}

// begin starts a transaction in the _txlock mode. A read-only transaction
// uses plain BEGIN: a mode such as IMMEDIATE would take a write lock.
// opts.Isolation is ignored; SQLite transactions are serializable.
func (c *conn) begin(ctx context.Context, opts driver.TxOptions) (*tx, error) {
	sql := "begin"
	if !opts.ReadOnly && c.beginMode != "" {
		sql = "begin " + c.beginMode
	}
	if err := c.execControl(ctx, sql); err != nil {
		return nil, err
	}
	return &tx{c: c}, nil
}

// Commit implements driver.Tx.
func (t *tx) Commit() error {
	err := t.c.execControl(context.Background(), "commit")
	if err == nil {
		return nil
	}
	// A failed COMMIT, with SQLITE_BUSY for one, can leave the transaction
	// open. database/sql considers it over once Commit returns and would put
	// the connection back in the pool still inside it, so roll it back. The
	// error to report is the COMMIT's.
	// See https://github.com/modernc-org/sqlite/issues/2.
	if t.c.db != 0 && sqlite3.Xsqlite3_get_autocommit(t.c.tls, t.c.db) == 0 {
		t.c.execControl(context.Background(), "rollback")
	}
	return err
}

// Rollback implements driver.Tx.
func (t *tx) Rollback() error {
	return t.c.execControl(context.Background(), "rollback")
}

// execControl runs a transaction-control statement, which takes no
// arguments and returns no rows, with sqlite3_exec.
func (c *conn) execControl(ctx context.Context, sql string) error {
	p, err := libc.CString(sql)
	if err != nil {
		return err
	}
	defer c.free(p)
	defer c.watchCancel(ctx).stop()

	if rc := sqlite3.Xsqlite3_exec(c.tls, c.db, p, 0, 0, 0); rc != sqlite3.SQLITE_OK {
		return c.errstr(rc)
	}
	return nil
}
