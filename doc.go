// Copyright 2017 The Sqlite Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package sqlite is a sql/database driver using a CGo-free port of the C
// SQLite3 library.
//
// SQLite is an in-process implementation of a self-contained, serverless,
// zero-configuration, transactional SQL database engine.
//
// The package exports nothing. Importing it registers a database/sql driver
// named "sqlite"; see "Connecting to a database" for the DSN it accepts and
// "Errors" for reading SQLite's result codes.
//
// # OFD locking (Linux)
//
// On Linux the library can take Open File Description (OFD) locks instead of
// POSIX record locks on database files, which stops an unrelated os.File
// close anywhere in the process from silently stripping SQLite's transaction
// locks. The switch is process-wide and off by default: set
// MODERNC_SQLITE_OFD_LOCK=1 in the environment the process starts with.
//
// # Vendored libc
//
// The transpiled SQLite core must run against one exact libc revision.
// Upstream shipped libc as a separate module and asked importers to pin the
// matching version by hand; this fork vendors it at internal/libc instead, so
// the requirement is satisfied structurally and there is nothing to pin. The
// internal/ path is deliberate: libc keeps process-global C state, so the
// compiler must reject a second copy in the same binary. See go.mod for the
// upstream revisions each vendored tree corresponds to.
//
// # Thanks
//
// This project is sponsored by Schleibinger Geräte Teubert u. Greim GmbH by
// allowing one of the maintainers to work on it also in office hours.
//
// # Supported platforms and architectures
//
// These combinations of GOOS and GOARCH are currently supported
//
//	OS      Arch    SQLite version
//	------------------------------
//	darwin	amd64   3.53.4
//	darwin	arm64   3.53.4
//	freebsd	386     3.53.4
//	freebsd	amd64   3.53.4
//	freebsd	arm     3.53.4
//	freebsd	arm64   3.53.4
//	linux	386     3.53.4
//	linux	amd64   3.53.4
//	linux	arm     3.53.4
//	linux	arm64   3.53.4
//	linux	loong64 3.53.4
//	linux	ppc64le 3.53.4
//	linux	riscv64 3.53.4
//	linux	s390x   3.53.4
//	netbsd	amd64   3.53.4
//	openbsd	amd64   3.53.4
//	openbsd	arm64   3.53.4
//	windows	386     3.53.4
//	windows	amd64   3.53.4
//	windows	arm64   3.53.4
//
// # Benchmarks
//
// [The SQLite Drivers Benchmarks Game]
//
// # Performance
//
// The transpiled SQLite core runs slower than the same C compiled natively.
// The gap is in CPU-bound work: the bytecode interpreter loop, b-tree page
// balancing and record building. I/O-bound work is dominated by the operating
// system either way. The ratios below are CPU time per query, measured in
// September 2026 on linux/amd64 with Go 1.27 and the vendored internal/libc,
// against SQLite 3.53.4 compiled with the same compile-time options this
// package uses:
//
//	Workload                                                        Driver vs C
//	-------------------------------------------------------------------------
//	Unindexed ORDER BY ... LIMIT 100 over 584k rows of 23 columns      2.0x
//	GROUP BY aggregate over the same table                             1.9x
//	Correlated subquery walking an index with text comparisons         1.3x
//
// Throughput across four connections scaled at least as well as the C build
// did, so the ratios hold under concurrency.
//
// Two things follow. First, this package uses the same query planner as C
// SQLite, so a query that is slow in C is slower here by the ratio above and
// no more; but a missing index costs the same ratio more, and a query that is
// merely sluggish in C can cross a deadline here. Check EXPLAIN QUERY PLAN for
// USE TEMP B-TREE and index the columns that ORDER BY, GROUP BY and WHERE use.
// Second, database/sql opens connections without limit by default. Each
// connection carries its own page cache and its own libc thread state, and a
// periodic query that takes longer than its period piles up without bound.
// Bound the pool with [sql.DB.SetMaxOpenConns] and do not issue a periodic
// query before the previous one has returned.
//
// Part of the gap is in internal/libc rather than in the transpiled SQLite.
// On Linux, libc revisions before the vendored one implemented memcpy,
// memmove, memset and memcmp as transpiled musl loops moving at most four
// bytes per step; the vendored revision replaced them with native Go routines
// backed by the runtime's vectorized memmove, and the three workloads above
// went from 3.0x, 2.2x and 1.6x to the figures shown. On the non-Linux
// targets memcpy and memmove are native Go copies already; memcmp there is
// still a byte loop.
//
// Because everything is Go, the usual Go tooling reaches into the SQLite core:
// a CPU profile taken with runtime/pprof attributes time to the transpiled
// SQLite functions under their C names, for example lib._balance_nonroot or
// lib.Xsqlite3_step, and to the libc routines they call.
//
// # Connecting to a database
//
// Import the package for its side effect of registering the driver, then
// open databases through database/sql:
//
//	import (
//		"database/sql"
//
//		_ "github.com/unaquatic/sqlite"
//	)
//
//	db, err := sql.Open("sqlite", "file:app.db?_pragma=foreign_keys(1)")
//
// The DSN is a filename, such as "/tmp/app.db", or a URI, such as
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
//
// # Errors
//
// An error SQLite reports has a Code method returning its result code,
// extended where SQLite reports one, such as 2067 for
// SQLITE_CONSTRAINT_UNIQUE (https://www.sqlite.org/rescode.html). Reach it
// through an interface:
//
//	var e interface{ Code() int }
//	if errors.As(err, &e) && e.Code()&0xff == 5 { // SQLITE_BUSY
//		// retry
//	}
//
// # Debug and development versions
//
// The transpiled SQLite sources under internal/lib/ are not generated in this
// repository and cannot be regenerated here. Upstream produces them with
// modernc.org/libsqlite3, which owns the transpilation and the SQLite
// compile-time options. To move to a newer or differently configured SQLite,
// re-vendor from upstream modernc.org/sqlite as described in README.md.
//
// # Hacking
//
// The leak tests count live allocations of the C allocator, which counts them
// only with -tags=memory.counters; without it they skip. make test_leaks runs
// them with the tag.
//
// internal/libc is part of this module, so debug edits to it take effect
// directly, with no go.work needed. Its upstream debug build tags (libc.dmesg,
// libc.strace, libc.memgrind, libc.membrk and libc.nofsync) have no effect
// here: the files they select were not vendored.
//
// # Sqlite documentation
//
// See https://sqlite.org/docs.html
//
// [The SQLite Drivers Benchmarks Game]: https://pkg.go.dev/modernc.org/sqlite-bench#readme-tl-dr-scorecard
package sqlite // import "github.com/unaquatic/sqlite"
