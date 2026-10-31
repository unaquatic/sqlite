// Copyright 2025 The Sqlite Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package sqlite // import "github.com/unaquatic/sqlite"

import (
	"fmt"

	sqlite3 "github.com/unaquatic/sqlite/internal/lib"
	"github.com/unaquatic/sqlite/internal/libc"
)

// sqliteError is an error reported by SQLite. Callers reach its Code through
// an interface; see "Errors" in the package documentation.
type sqliteError struct {
	msg  string
	code int
}

func (e *sqliteError) Error() string { return e.msg }

// Code returns the SQLite result code, extended where SQLite reports one.
func (e *sqliteError) Code() int { return e.code }

// errstr returns the error for result code rc of an operation on c.
func (c *conn) errstr(rc int32) error { return newError(c.tls, c.db, rc) }

// newError returns the error for result code rc of an operation on the
// database handle db, which may be 0.
//
// The message is sqlite3_errstr(rc), followed by sqlite3_errmsg(db) when that
// says something more. errmsg describes the most recent failure on db, which
// is not always the operation that returned rc: a failed open, for example,
// can leave a stale message on the handle it returns. So errmsg is added
// only when sqlite3_extended_errcode(db) agrees with rc, in full or in its
// primary code. See https://gitlab.com/cznic/sqlite/-/issues/230.
func newError(tls *libc.TLS, db uintptr, rc int32) error {
	msg := libc.GoString(sqlite3.Xsqlite3_errstr(tls, rc))
	if db != 0 {
		if ext := sqlite3.Xsqlite3_extended_errcode(tls, db); ext == rc || ext&0xff == rc&0xff {
			if detail := libc.GoString(sqlite3.Xsqlite3_errmsg(tls, db)); detail != msg {
				msg += ": " + detail
			}
		}
	}
	msg = fmt.Sprintf("%s (%d)", msg, rc)
	if rc == sqlite3.SQLITE_BUSY {
		msg += " (SQLITE_BUSY)"
	}
	return &sqliteError{msg: msg, code: int(rc)}
}
