// Copyright 2025 The Sqlite Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package sqlite // import "github.com/unaquatic/sqlite"

import (
	"database/sql"
	"database/sql/driver"

	sqlite3 "github.com/unaquatic/sqlite/internal/lib"
)

const driverName = "sqlite"

func init() {
	sql.Register(driverName, &Driver{})
	sqlite3.PatchIssue199() // https://gitlab.com/cznic/sqlite/-/issues/199
}

var _ driver.Driver = (*Driver)(nil)

// Driver implements database/sql/driver.Driver. This package registers one
// as "sqlite"; use it through sql.Open("sqlite", dsn). The zero Driver is
// ready to use.
type Driver struct{}

// Open implements driver.Driver. It opens a new connection to the database
// that name describes.
//
// The name is a filename, such as "/tmp/app.db", or a URI, such as
// "file:/tmp/app.db?mode=ro". Parameters follow a '?'. SQLite reads its own,
// such as mode, cache and vfs, from a file: URI only, and ignores the rest.
// The driver reads these from either form:
//
// _pragma: A PRAGMA to run on every new connection, without the PRAGMA
// keyword, such as "foreign_keys(1)" or "busy_timeout(5000)". It may be given
// more than once. The values run busy_timeout first and the rest sorted, so
// their order does not depend on the DSN. SQLite runs them as given, so a DSN
// carrying _pragma must come from a trusted source, and a bad one fails only
// once the database is open, after the values before it have taken effect.
//
// _journal_mode: The journal mode, one of DELETE, TRUNCATE, PERSIST, MEMORY,
// WAL and OFF, in any case. It is set after the _pragma values, so that a
// _pragma=page_size(N) applies to a new database before WAL mode fixes its
// page size.
//
// _txlock: How BEGIN locks the database: "deferred" (the default),
// "immediate" or "exclusive", in any case. A read-only transaction always
// begins deferred. See
// https://www.sqlite.org/lang_transaction.html#deferred_immediate_and_exclusive_transactions
//
// Any other parameter whose name starts with '_' is an error. The parameters
// are checked before the database is opened, so a DSN they reject creates no
// database file.
func (d *Driver) Open(name string) (driver.Conn, error) {
	c, err := newConn(name)
	if err != nil {
		return nil, err
	}
	return c, nil
}
