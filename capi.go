// Copyright 2025 The Sqlite Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package sqlite // import "github.com/unaquatic/sqlite"

// Thin wrappers over the SQLite C API, as transpiled into package lib. They
// convert between Go values and the C strings, pointers and result codes the
// API uses, and nothing more.

import (
	"database/sql/driver"
	"fmt"
	"time"
	"unsafe"

	sqlite3 "github.com/unaquatic/sqlite/internal/lib"
	"github.com/unaquatic/sqlite/internal/libc"
	"github.com/unaquatic/sqlite/internal/libc/sys/types"
)

const (
	ptrSize = int(unsafe.Sizeof(uintptr(0)))

	// openFlags are the sqlite3_open_v2 flags of every connection. Opening
	// FULLMUTEX lets a cancelled context interrupt a statement from another
	// goroutine.
	openFlags = sqlite3.SQLITE_OPEN_READWRITE | sqlite3.SQLITE_OPEN_CREATE |
		sqlite3.SQLITE_OPEN_FULLMUTEX | sqlite3.SQLITE_OPEN_URI

	// timeFormat is how a time.Time argument is stored: as text in format 4
	// of https://www.sqlite.org/lang_datefunc.html#time_values, with
	// sub-second precision and the zone offset, which SQLite's date and time
	// functions read.
	timeFormat = "2006-01-02 15:04:05.999999999-07:00"
)

// load reads the pointer stored at p.
func load(p uintptr) uintptr { return *(*uintptr)(unsafe.Pointer(p)) }

// atEnd reports whether the C string p is empty.
func atEnd(p uintptr) bool { return *(*byte)(unsafe.Pointer(p)) == 0 }

func (c *conn) malloc(n int) (uintptr, error) {
	if p := libc.Xmalloc(c.tls, types.Size_t(n)); p != 0 || n == 0 {
		return p, nil
	}
	return 0, fmt.Errorf("sqlite: cannot allocate %d bytes of memory", n)
}

func (c *conn) free(p uintptr) {
	if p != 0 {
		libc.Xfree(c.tls, p)
	}
}

func (c *conn) freeAllocs(allocs []uintptr) {
	for _, p := range allocs {
		c.free(p)
	}
}

// open opens the database name into c.db. It calls sqlite3_open_v2, the
// variant of sqlite3_open that takes openFlags.
func (c *conn) open(name string) error {
	zName, err := libc.CString(name)
	if err != nil {
		return err
	}
	defer c.free(zName)

	ppDb := c.tls.Alloc(ptrSize)
	defer c.tls.Free(ptrSize)
	*(*uintptr)(unsafe.Pointer(ppDb)) = 0

	rc := sqlite3.Xsqlite3_open_v2(c.tls, zName, ppDb, openFlags, 0)
	db := load(ppDb)
	if rc != sqlite3.SQLITE_OK {
		// sqlite3_open_v2 can return a handle even when it fails. The handle
		// carries the error message, and it must be closed all the same.
		err := newError(c.tls, db, rc)
		if db != 0 {
			sqlite3.Xsqlite3_close_v2(c.tls, db)
		}
		return err
	}
	c.db = db
	return nil
}

// close closes c.db. It calls sqlite3_close_v2, which, unlike sqlite3_close,
// does not fail while a statement is left unfinalized: it closes the handle
// once the last one is.
func (c *conn) close() error {
	if rc := sqlite3.Xsqlite3_close_v2(c.tls, c.db); rc != sqlite3.SQLITE_OK {
		return c.errstr(rc)
	}
	return nil
}

// prepare compiles the first statement of the C string *zSQL and advances
// *zSQL past it. The statement is 0 when that part of the SQL holds no
// statement, only whitespace, comments or an empty ';'.
func (c *conn) prepare(zSQL *uintptr) (uintptr, error) {
	pp := c.tls.Alloc(2 * ptrSize)
	defer c.tls.Free(2 * ptrSize)
	ppStmt, pzTail := pp, pp+uintptr(ptrSize)
	if rc := sqlite3.Xsqlite3_prepare_v2(c.tls, c.db, *zSQL, -1, ppStmt, pzTail); rc != sqlite3.SQLITE_OK {
		return 0, c.errstr(rc)
	}
	*zSQL = load(pzTail)
	return load(ppStmt), nil
}

// step runs pstmt to its next row. It reports whether there is one: true for
// SQLITE_ROW, false for SQLITE_DONE.
func (c *conn) step(pstmt uintptr) (bool, error) {
	switch rc := sqlite3.Xsqlite3_step(c.tls, pstmt); rc {
	case sqlite3.SQLITE_ROW:
		return true, nil
	case sqlite3.SQLITE_DONE:
		return false, nil
	default:
		return false, c.errstr(rc)
	}
}

func (c *conn) finalize(pstmt uintptr) error {
	if rc := sqlite3.Xsqlite3_finalize(c.tls, pstmt); rc != sqlite3.SQLITE_OK {
		return c.errstr(rc)
	}
	return nil
}

func (c *conn) reset(pstmt uintptr) error {
	if rc := sqlite3.Xsqlite3_reset(c.tls, pstmt); rc != sqlite3.SQLITE_OK {
		return c.errstr(rc)
	}
	return nil
}

func (c *conn) clearBindings(pstmt uintptr) error {
	if rc := sqlite3.Xsqlite3_clear_bindings(c.tls, pstmt); rc != sqlite3.SQLITE_OK {
		return c.errstr(rc)
	}
	return nil
}

// result returns the outcome of the statement that just finished on c.
func (c *conn) result() *result {
	return &result{
		lastInsertID: sqlite3.Xsqlite3_last_insert_rowid(c.tls, c.db),
		rowsAffected: int64(sqlite3.Xsqlite3_changes(c.tls, c.db)),
	}
}

func (c *conn) columnCount(pstmt uintptr) int {
	return int(sqlite3.Xsqlite3_column_count(c.tls, pstmt))
}

func (c *conn) columnName(pstmt uintptr, i int) string {
	return libc.GoString(sqlite3.Xsqlite3_column_name(c.tls, pstmt, int32(i)))
}

// columnType returns the storage class of column i in the current row.
func (c *conn) columnType(pstmt uintptr, i int) int32 {
	return sqlite3.Xsqlite3_column_type(c.tls, pstmt, int32(i))
}

func (c *conn) columnInt64(pstmt uintptr, i int) int64 {
	return sqlite3.Xsqlite3_column_int64(c.tls, pstmt, int32(i))
}

func (c *conn) columnDouble(pstmt uintptr, i int) float64 {
	return sqlite3.Xsqlite3_column_double(c.tls, pstmt, int32(i))
}

// columnText returns column i as a string copied into Go memory.
func (c *conn) columnText(pstmt uintptr, i int) string {
	p := sqlite3.Xsqlite3_column_text(c.tls, pstmt, int32(i))
	n := int(sqlite3.Xsqlite3_column_bytes(c.tls, pstmt, int32(i)))
	if p == 0 || n == 0 {
		return ""
	}
	// Copy the bytes once and use the copy as the string's memory. b is not
	// reachable as a []byte once this returns, so nothing can modify it, and
	// string(b) would only copy it a second time.
	b := make([]byte, n)
	copy(b, (*libc.RawMem)(unsafe.Pointer(p))[:n:n])
	return unsafe.String(unsafe.SliceData(b), n)
}

// columnBlob returns column i as a []byte copied into Go memory. An empty
// blob is nil.
func (c *conn) columnBlob(pstmt uintptr, i int) []byte {
	p := sqlite3.Xsqlite3_column_blob(c.tls, pstmt, int32(i))
	n := int(sqlite3.Xsqlite3_column_bytes(c.tls, pstmt, int32(i)))
	if p == 0 || n == 0 {
		return nil
	}
	b := make([]byte, n)
	copy(b, (*libc.RawMem)(unsafe.Pointer(p))[:n:n])
	return b
}

// bind binds args to the parameters of pstmt by position: args[0] to
// parameter 1, and so on, so a ?NNN parameter takes args[NNN-1]. Text and
// blob values are bound from C copies that must outlive the statement's
// execution; bind returns them for the caller to free once it is done
// stepping.
func (c *conn) bind(pstmt uintptr, args []driver.NamedValue) (allocs []uintptr, err error) {
	for _, a := range args {
		if a.Name != "" {
			return nil, fmt.Errorf("sqlite: named argument %q: only positional arguments are supported", a.Name)
		}
	}
	n := int(sqlite3.Xsqlite3_bind_parameter_count(c.tls, pstmt))
	if n == 0 {
		return nil, nil
	}

	defer func() {
		if err != nil {
			c.freeAllocs(allocs)
			allocs = nil
		}
	}()
	for i := 1; i <= n; i++ {
		if i > len(args) {
			return allocs, fmt.Errorf("missing argument with index %d", i)
		}
		p, err := c.bindValue(pstmt, i, args[i-1].Value)
		if err != nil {
			return allocs, err
		}
		if p != 0 {
			allocs = append(allocs, p)
		}
	}
	return allocs, nil
}

// bindValue binds v to parameter i and returns the C memory it allocated for
// the value, if any.
func (c *conn) bindValue(pstmt uintptr, i int, v any) (uintptr, error) {
	switch x := v.(type) {
	case nil:
		return 0, c.bindNull(pstmt, i)
	case int64:
		return 0, c.bindInt64(pstmt, i, x)
	case float64:
		return 0, c.bindDouble(pstmt, i, x)
	case bool:
		var n int64
		if x {
			n = 1
		}
		return 0, c.bindInt64(pstmt, i, n)
	case []byte:
		return c.bindBlob(pstmt, i, x)
	case string:
		return c.bindText(pstmt, i, x)
	case time.Time:
		// SQLite has no time type.
		return c.bindText(pstmt, i, x.Format(timeFormat))
	default:
		return 0, fmt.Errorf("sqlite: invalid driver.Value type %T", x)
	}
}

func (c *conn) bindNull(pstmt uintptr, i int) error {
	if rc := sqlite3.Xsqlite3_bind_null(c.tls, pstmt, int32(i)); rc != sqlite3.SQLITE_OK {
		return c.errstr(rc)
	}
	return nil
}

func (c *conn) bindInt64(pstmt uintptr, i int, v int64) error {
	if rc := sqlite3.Xsqlite3_bind_int64(c.tls, pstmt, int32(i), v); rc != sqlite3.SQLITE_OK {
		return c.errstr(rc)
	}
	return nil
}

func (c *conn) bindDouble(pstmt uintptr, i int, v float64) error {
	if rc := sqlite3.Xsqlite3_bind_double(c.tls, pstmt, int32(i), v); rc != sqlite3.SQLITE_OK {
		return c.errstr(rc)
	}
	return nil
}

// bindText binds v from a C copy, which the caller frees after stepping.
func (c *conn) bindText(pstmt uintptr, i int, v string) (uintptr, error) {
	p, err := libc.CString(v)
	if err != nil {
		return 0, err
	}
	if rc := sqlite3.Xsqlite3_bind_text(c.tls, pstmt, int32(i), p, int32(len(v)), sqlite3.SQLITE_STATIC); rc != sqlite3.SQLITE_OK {
		c.free(p)
		return 0, c.errstr(rc)
	}
	return p, nil
}

// bindBlob binds v from a C copy, which the caller frees after stepping. A
// nil v binds NULL.
func (c *conn) bindBlob(pstmt uintptr, i int, v []byte) (uintptr, error) {
	if v == nil {
		return 0, c.bindNull(pstmt, i)
	}
	p, err := c.malloc(len(v))
	if err != nil {
		return 0, err
	}
	if len(v) != 0 {
		copy((*libc.RawMem)(unsafe.Pointer(p))[:len(v):len(v)], v)
	}
	if rc := sqlite3.Xsqlite3_bind_blob(c.tls, pstmt, int32(i), p, int32(len(v)), sqlite3.SQLITE_STATIC); rc != sqlite3.SQLITE_OK {
		c.free(p)
		return 0, c.errstr(rc)
	}
	return p, nil
}
