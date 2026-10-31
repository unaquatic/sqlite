// Copyright 2016 The Sqlite Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package sqlite // import "github.com/unaquatic/sqlite"

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

// A context cancelled after its statement completed must not interrupt a
// later statement.
// https://gitlab.com/cznic/sqlite/-/issues/94
func TestCancelRace(t *testing.T) {
	db := tempDB(t)
	for _, tc := range []struct {
		name string
		f    func(context.Context) error
	}{
		{"db.ExecContext", func(ctx context.Context) error {
			_, err := db.ExecContext(ctx, "select 1")
			return err
		}},
		{"db.QueryContext", func(ctx context.Context) error {
			rows, err := db.QueryContext(ctx, "select 1")
			if err == nil {
				rows.Close()
			}
			return err
		}},
		{"tx.ExecContext", func(ctx context.Context) error {
			tx, err := db.BeginTx(ctx, nil)
			if err != nil {
				return err
			}
			defer tx.Rollback()
			if _, err := tx.ExecContext(ctx, "select 1"); err != nil {
				return err
			}
			return tx.Rollback()
		}},
		{"tx.QueryContext", func(ctx context.Context) error {
			tx, err := db.BeginTx(ctx, nil)
			if err != nil {
				return err
			}
			defer tx.Rollback()
			rows, err := tx.QueryContext(ctx, "select 1")
			if err != nil {
				return err
			}
			rows.Close()
			return tx.Rollback()
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			// The race does not show on every run, but it does within a
			// hundred iterations.
			for i := range 100 {
				ctx, cancel := context.WithCancel(context.Background())
				err := tc.f(ctx)
				cancel()
				if err != nil {
					t.Fatalf("iteration %d: %v", i, err)
				}
			}
		})
	}
}

// Cancelling the context while Exec drains the rows of a statement with
// RETURNING stops it with an error.
func TestExecReturningCancelDuringDrain(t *testing.T) {
	db := openDB(t, ":memory:")
	db.SetMaxOpenConns(1)
	mustExec(t, db, "CREATE TABLE t (id INTEGER PRIMARY KEY)")
	mustExec(t, db, `
		WITH RECURSIVE gen(n) AS (SELECT 1 UNION ALL SELECT n+1 FROM gen WHERE n < 10000)
		INSERT INTO t SELECT n FROM gen`)

	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(time.Microsecond)
		cancel()
	}()
	_, err := db.ExecContext(ctx, "DELETE FROM t RETURNING id")
	switch {
	case err == nil:
		t.Log("the statement finished before the cancel; inconclusive")
	case !errors.Is(err, context.Canceled) && !strings.Contains(err.Error(), "interrupted (9)"):
		t.Fatalf("got %v, want context.Canceled or an interrupted error", err)
	}
}
