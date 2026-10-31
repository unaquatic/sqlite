// Copyright 2025 The Sqlite Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package sqlite // import "github.com/unaquatic/sqlite"

import (
	"context"
	"database/sql/driver"
	"sync"
	"sync/atomic"

	sqlite3 "github.com/unaquatic/sqlite/internal/lib"
	"github.com/unaquatic/sqlite/internal/libc"
)

var (
	_ driver.Conn               = (*conn)(nil)
	_ driver.ConnBeginTx        = (*conn)(nil)
	_ driver.ConnPrepareContext = (*conn)(nil)
	_ driver.ExecerContext      = (*conn)(nil)
	_ driver.QueryerContext     = (*conn)(nil)
	_ driver.Pinger             = (*conn)(nil)
	_ driver.SessionResetter    = (*conn)(nil)
	_ driver.Validator          = (*conn)(nil)
)

// conn is a connection: one SQLite database handle and the libc thread state
// the transpiled SQLite code runs on. database/sql uses it from one goroutine
// at a time; only interrupt runs concurrently, when a context is cancelled.
type conn struct {
	db  uintptr // *sqlite3
	tls *libc.TLS

	// mu serializes Close and interrupt.
	mu sync.Mutex

	beginMode string // _txlock

	// inMemory reports whether the database lives only in memory, as for
	// ":memory:" and "file::memory:". Its only connection is then the
	// database itself, so an interrupted statement must not get the
	// connection discarded. See https://gitlab.com/cznic/sqlite/-/issues/196.
	inMemory bool
}

func newConn(dsn string) (*conn, error) {
	cfg, err := parseDSN(dsn)
	if err != nil {
		return nil, err
	}

	c := &conn{tls: libc.NewTLS(), beginMode: cfg.beginMode}
	if err := c.open(cfg.name); err != nil {
		c.tls.Close()
		return nil, err
	}
	if err := c.configure(cfg.pragmas); err != nil {
		c.Close()
		return nil, err
	}
	return c, nil
}

// configure readies the newly opened database and runs pragmas on it.
func (c *conn) configure(pragmas []string) error {
	// Report extended result codes, such as SQLITE_CONSTRAINT_UNIQUE, in
	// place of their primary codes.
	if rc := sqlite3.Xsqlite3_extended_result_codes(c.tls, c.db, 1); rc != sqlite3.SQLITE_OK {
		return c.errstr(rc)
	}

	// sqlite3_db_filename is "" for a database not backed by a file.
	zMain, err := libc.CString("main")
	if err != nil {
		return err
	}
	c.inMemory = libc.GoString(sqlite3.Xsqlite3_db_filename(c.tls, c.db, zMain)) == ""
	c.free(zMain)

	for _, pragma := range pragmas {
		if _, err := c.exec(context.Background(), pragma, nil); err != nil {
			return err
		}
	}
	return nil
}

// Close implements driver.Conn.
func (c *conn) Close() error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.db != 0 {
		if err := c.close(); err != nil {
			return err
		}
		c.db = 0
	}
	if c.tls != nil {
		c.tls.Close()
		c.tls = nil
	}
	return nil
}

// ResetSession implements driver.SessionResetter.
func (c *conn) ResetSession(context.Context) error {
	if !c.usable() {
		return driver.ErrBadConn
	}
	return nil
}

// IsValid implements driver.Validator.
func (c *conn) IsValid() bool { return c.usable() }

// usable reports whether database/sql may keep using c. A connection whose
// last statement was interrupted is not, unless its database is in memory and
// would be lost with it (https://gitlab.com/cznic/sqlite/-/issues/196 and
// https://gitlab.com/cznic/sqlite/-/issues/198).
func (c *conn) usable() bool {
	switch {
	case c.db == 0:
		return false
	case c.inMemory:
		return true
	default:
		return sqlite3.Xsqlite3_is_interrupted(c.tls, c.db) == 0
	}
}

// Ping implements driver.Pinger.
func (c *conn) Ping(ctx context.Context) error {
	_, err := c.exec(ctx, "select 1", nil)
	return err
}

// Begin implements driver.Conn.
//
// Deprecated: database/sql calls BeginTx.
func (c *conn) Begin() (driver.Tx, error) {
	return c.BeginTx(context.Background(), driver.TxOptions{})
}

// BeginTx implements driver.ConnBeginTx.
func (c *conn) BeginTx(ctx context.Context, opts driver.TxOptions) (driver.Tx, error) {
	t, err := c.begin(ctx, opts)
	if err != nil {
		return nil, err
	}
	return t, nil
}

// Prepare implements driver.Conn.
func (c *conn) Prepare(query string) (driver.Stmt, error) {
	return c.PrepareContext(context.Background(), query)
}

// PrepareContext implements driver.ConnPrepareContext.
func (c *conn) PrepareContext(ctx context.Context, query string) (driver.Stmt, error) {
	s, err := newStmt(c, query)
	if err != nil {
		return nil, err
	}
	return s, nil
}

// ExecContext implements driver.ExecerContext.
func (c *conn) ExecContext(ctx context.Context, query string, args []driver.NamedValue) (driver.Result, error) {
	return c.exec(ctx, query, args)
}

// QueryContext implements driver.QueryerContext.
func (c *conn) QueryContext(ctx context.Context, query string, args []driver.NamedValue) (driver.Rows, error) {
	r, err := c.query(ctx, query, args)
	if err != nil {
		return nil, err
	}
	return r, nil
}

// exec runs query, which may hold several statements, once.
func (c *conn) exec(ctx context.Context, query string, args []driver.NamedValue) (driver.Result, error) {
	s, err := newStmt(c, query)
	if err != nil {
		return nil, err
	}
	r, err := s.exec(ctx, args)
	if e := s.Close(); err == nil {
		err = e
	}
	if err != nil {
		return nil, err
	}
	return r, nil
}

// query runs query, which may hold several statements, once and returns the
// rows of its last statement.
func (c *conn) query(ctx context.Context, query string, args []driver.NamedValue) (*rows, error) {
	s, err := newStmt(c, query)
	if err != nil {
		return nil, err
	}
	r, err := s.query(ctx, args)
	if err != nil {
		s.Close()
		return nil, err
	}
	// s lives for this one query. Hand its prepared statement to the rows,
	// which finalize it on Close, so that closing s leaves it alone. See
	// https://gitlab.com/cznic/sqlite/-/issues/118.
	if s.pstmt != 0 {
		s.pstmt = 0
		r.reuseStmt = false
	}
	if err := s.Close(); err != nil {
		r.Close()
		return nil, err
	}
	return r, nil
}

// interrupt makes the statement running on c, if any, fail with
// SQLITE_INTERRUPT. It is safe to call from any goroutine.
func (c *conn) interrupt() {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.tls != nil {
		sqlite3.Xsqlite3_interrupt(c.tls, c.db)
	}
}

// cancelWatch interrupts a connection when a context is cancelled, until it
// is stopped. A nil *cancelWatch watches nothing.
type cancelWatch struct {
	release func() bool
	fired   atomic.Bool

	mu      sync.Mutex
	stopped bool
}

// watchCancel interrupts c when ctx is cancelled, until the returned watch
// is stopped. It returns nil for a context that cannot be cancelled.
func (c *conn) watchCancel(ctx context.Context) *cancelWatch {
	if ctx.Done() == nil {
		return nil
	}
	w := &cancelWatch{}
	w.release = context.AfterFunc(ctx, func() {
		w.mu.Lock()
		defer w.mu.Unlock()
		if !w.stopped {
			w.fired.Store(true)
			c.interrupt()
		}
	})
	return w
}

// stop ends the watch. Once it returns, the watch will not interrupt the
// connection, which is free for the next statement.
func (w *cancelWatch) stop() {
	if w == nil {
		return
	}
	w.release()
	w.mu.Lock()
	w.stopped = true
	w.mu.Unlock()
}

// cancelled reports whether the watch interrupted the connection.
func (w *cancelWatch) cancelled() bool { return w != nil && w.fired.Load() }
