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
	sql.Register(driverName, sqliteDriver{})
	sqlite3.PatchIssue199() // https://gitlab.com/cznic/sqlite/-/issues/199
}

var _ driver.Driver = sqliteDriver{}

// sqliteDriver is the driver this package registers as "sqlite".
type sqliteDriver struct{}

// Open implements driver.Driver. It opens a new connection to the database
// that name describes. See the package documentation for its parameters.
func (sqliteDriver) Open(name string) (driver.Conn, error) {
	c, err := newConn(name)
	if err != nil {
		return nil, err
	}
	return c, nil
}
