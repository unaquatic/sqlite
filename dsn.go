// Copyright 2025 The Sqlite Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package sqlite // import "github.com/unaquatic/sqlite"

import (
	"fmt"
	"maps"
	"net/url"
	"slices"
	"strings"
)

// config is what a DSN asks for. parseDSN validates every parameter before
// the database is opened, so a DSN carrying a bad value fails without
// creating the database file or changing it.
type config struct {
	// name is the filename argument of sqlite3_open_v2.
	name string

	// pragmas are the PRAGMA statements to run once the database is open,
	// in the order they must run.
	pragmas []string

	// beginMode is the _txlock mode, or "" for SQLite's default.
	beginMode string
}

// journalModes are the values _journal_mode accepts, in any case.
var journalModes = []string{"DELETE", "TRUNCATE", "PERSIST", "MEMORY", "WAL", "OFF"}

// parseDSN parses and validates dsn. See [Driver.Open] for the parameters.
func parseDSN(dsn string) (*config, error) {
	cfg := &config{name: dsn}

	// The parameters follow the first '?'. A file: URI keeps them, because
	// SQLite parses the URI itself and needs its own parameters, such as
	// mode and cache; it ignores the ones it does not know. Any other name
	// is a plain path and loses them. See
	// https://github.com/mattn/go-sqlite3/blob/3392062c729d77820afc1f5cae3427f0de39e954/sqlite3.go#L1046
	pos := strings.IndexByte(dsn, '?')
	if pos < 1 {
		return cfg, nil
	}
	if !strings.HasPrefix(dsn, "file:") {
		cfg.name = dsn[:pos]
	}
	q, err := url.ParseQuery(dsn[pos+1:])
	if err != nil {
		return nil, err
	}

	// SQLite's own URI parameters never start with '_', so such a parameter
	// is meant for the driver, and one it does not know is a mistake.
	for _, key := range slices.Sorted(maps.Keys(q)) {
		switch key {
		case "_pragma", "_journal_mode", "_txlock":
		default:
			if strings.HasPrefix(key, "_") {
				return nil, fmt.Errorf("unknown DSN parameter %q", key)
			}
		}
	}

	// The _pragma values run busy_timeout first, so that the statements
	// after it wait for locks instead of failing with SQLITE_BUSY, and the
	// rest sorted, so that their order does not depend on the DSN
	// (https://gitlab.com/cznic/sqlite/-/issues/198#note_2233423463).
	pragmas := slices.Clone(q["_pragma"])
	slices.SortStableFunc(pragmas, func(a, b string) int {
		a, b = strings.ToLower(strings.TrimSpace(a)), strings.ToLower(strings.TrimSpace(b))
		aBusy, bBusy := strings.HasPrefix(a, "busy_timeout"), strings.HasPrefix(b, "busy_timeout")
		switch {
		case aBusy && !bBusy:
			return -1
		case bBusy && !aBusy:
			return 1
		}
		return strings.Compare(a, b)
	})
	for _, p := range pragmas {
		cfg.pragmas = append(cfg.pragmas, "pragma "+p)
	}

	// journal_mode runs after the _pragma values, so that a page_size among
	// them applies to a new database before WAL mode fixes its page size.
	if v := q.Get("_journal_mode"); v != "" {
		if !slices.ContainsFunc(journalModes, func(m string) bool { return strings.EqualFold(v, m) }) {
			return nil, fmt.Errorf("invalid _journal_mode %q, expecting one of: %s", v, strings.Join(journalModes, " "))
		}
		cfg.pragmas = append(cfg.pragmas, "pragma journal_mode = "+v)
	}

	if v := q.Get("_txlock"); v != "" {
		switch strings.ToLower(v) {
		case "deferred", "immediate", "exclusive":
			cfg.beginMode = v
		default:
			return nil, fmt.Errorf("unknown _txlock %q", v)
		}
	}
	return cfg, nil
}
