<p align="center">
  <a href="https://github.com/unaquatic/sqlite">
    <picture>
      <source media="(prefers-color-scheme: dark)" srcset="docs/logo-dark.svg">
      <img src="docs/logo-light.svg" alt="sqlite">
    </picture>
  </a>
</p>

<p align="center">
  <a href="https://go.dev/"><img src="https://img.shields.io/badge/Go-1.27-555?labelColor=888&logo=go&logoColor=white&style=flat-square" alt="Go"></a>
  <a href="https://github.com/unaquatic/sqlite/blob/main/LICENSE"><img src="https://img.shields.io/github/license/unaquatic/sqlite?labelColor=888&color=555&style=flat-square" alt="License"></a>
</p>

<p align="center"><b>A modern sqlite driver written in golang.</b></p>

---

> **Fork notice**
>
> `github.com/unaquatic/sqlite` is a hard fork of
> [`modernc.org/sqlite`](https://gitlab.com/cznic/sqlite) at tag **v1.59.0**
> (embedded SQLite **3.53.4**). Import paths have been rewritten from
> `modernc.org/sqlite` to `github.com/unaquatic/sqlite`; everything else is
> upstream's work.
>
> **Requires Go 1.27 or newer.** `internal/libc` uses the standard library
> `uuid` package (added in Go 1.27) instead of `github.com/google/uuid`.
>
> This build has **no `modernc.org/*` module dependencies**. The runtime
> support libraries are vendored under `internal/`:
>
> | Path                | Vendored from            | Version   |
> | ------------------- | ------------------------ | --------- |
> | `internal/libc`     | `modernc.org/libc`       | `v1.75.7` |
> | `internal/libc`     | `modernc.org/mathutil`   | `v1.7.1`  |
> | `internal/memory`   | `modernc.org/memory`     | `v1.12.1` |
>
> Only the reachable part of `mathutil` is kept, folded into
> `internal/libc/int128_math.go` and `internal/libc/overflow.go`; its license
> is `internal/libc/LICENSE-MATHUTIL`.
>
> They are under `internal/` on purpose. `libc` holds process-global C state
> (allocator, fd table, stdio tables), so two copies linked into one binary
> would be unsafe; `internal/` makes the compiler reject outside imports.
> Upstream test suites, `testdata/` and `generator.go` were not vendored.
>
> Upstream is BSD-3-Clause — see `LICENSE`, plus `LICENSE-SQLITE`,
> `AUTHORS`, `CONTRIBUTORS`, and the `LICENSE`/`AUTHORS` files retained inside
> each `internal/*` directory. Code original to this fork is covered by
> `LICENSE-UNAQUATIC`.
>
> **Do not report fork issues upstream.** The docs below, including the
> contributing table, links and badges, are upstream's and refer to upstream.
>
> Syncing a newer upstream release — note that upstream's `lib/` (here
> `internal/lib/`) is transpiled against one exact `libc`, so sqlite and
> `internal/libc` must be re-vendored together:
>
> ```sh
> # 1. sqlite itself, minus the packages and API this fork removed
> #    (see "Removed from this fork" below)
> git fetch --depth=1 --no-tags upstream refs/tags/vX.Y.Z
> git checkout FETCH_HEAD -- .
> git rm -rq vec vfs pcache vtab
> git rm -rq internal/lib && git mv lib internal/lib
> rg -l 'modernc\.org/sqlite[/"]' --type go \
>   | xargs sed -i '' -E \
>     's|modernc\.org/sqlite/lib"|github.com/unaquatic/sqlite/internal/lib"|g; s|modernc\.org/sqlite/|github.com/unaquatic/sqlite/|g; s|modernc\.org/sqlite"|github.com/unaquatic/sqlite"|g'
>
> # 2. re-vendor the libc version that release's go.mod names
> #    (capi_*.go are ccgo symbol tables nothing in this tree reads)
> for m in libc@vA.B.C memory@vG.H.I; do
>   rsync -a --exclude 'testdata/' --exclude '*_test.go' --exclude 'generator.go' \
>     --exclude 'capi_*.go' \
>     "$(go env GOMODCACHE)/modernc.org/$m/" "internal/${m%@*}/"
> done
> chmod -R u+w internal && find internal -name go.mod -o -name go.sum | xargs rm -f
>
> # 3. rewrite the vendored paths
> rg -l 'modernc\.org/(libc|memory)' --type go | xargs sed -i '' -E \
>   -e 's|modernc\.org/libc|github.com/unaquatic/sqlite/internal/libc|g' \
>   -e 's|modernc\.org/memory|github.com/unaquatic/sqlite/internal/memory|g'
>
> # 4. re-apply the local divergence: stdlib uuid instead of google/uuid
> rg -l 'guuid "github.com/google/uuid"' --type go \
>   | xargs sed -i '' 's|guuid "github.com/google/uuid"|guuid "uuid"|'
>
> # 5. restore module path + go 1.27.0 in go.mod, drop upstream retracts
> gofmt -w . && go mod tidy && go build ./...
> go list -deps ./... | grep modernc.org   # must print nothing
> ```
>
> Steps 1–5 are mechanical. The other changes this fork made are not: the
> trimmed root-package API, and inside `internal/` the pruned sources, the
> `mathutil` fold, and the removed `go-isatty`, `go-strftime`, `go-humanize`
> and `bigfft` dependencies. Re-apply them by reviewing `git log` after step 5.

Credit
------

Essentially all of this code is the work of the `modernc.org/sqlite` authors
and contributors — see `AUTHORS`, `CONTRIBUTORS` and `LICENSE`. Upstream is
developed at [gitlab.com/cznic/sqlite](https://gitlab.com/cznic/sqlite); if
you find this driver useful, support the upstream project rather than this
fork. Upstream release notes live in that repository's `CHANGELOG.md`.

Removed from this fork
----------------------

Upstream files that only serve upstream's own development were dropped:
`.github/`, `sponsors/`, `examples/`, `testdata/`, `vendor_libs/`,
`issue198/`, `addport.go`, `issue120.diff`, `builder.json`, `tpch.sh`,
`unconvert.sh`, `HACKING.md`, `GOVERNANCE.md`, `CHANGELOG.md`, `CLAUDE.md`
and the `logo.png`/`bench.png` assets. The `Makefile` was trimmed to the
targets that can run without upstream's sibling checkouts.

Consequence: SQLite cannot be re-transpiled in this tree. Moving to a newer
SQLite means re-vendoring from upstream, per the procedure above.

The API is trimmed to what this fork's consumers use: the `database/sql`
driver registered as `sqlite`. Removed with it:

- Packages: `vec` (sqlite-vec), `vfs` (`fs.FS`-backed VFS), `pcache`, `vtab`,
  `cmd/sqlite`, and the fork's own `jsonschema`.
- Root-package API: application-defined functions and collations
  (`RegisterFunction`, `RegisterScalarFunction`,
  `RegisterDeterministicScalarFunction`, `RegisterCollationUtf8` and their
  `Must…` and `Driver` method forms), connection hooks, pre-update, commit and
  rollback hooks, virtual table modules, `RegisterPageCache`, `OFDLocking`,
  `NewConnector`, `Limit`, `ColumnInfo`, `Backup`, `Serialize`/`Deserialize`,
  `IsReadOnly`, `DBStatus` and `FileControl`.
- DSN parameters: `_error_rc`, `_defensive`, `_dqs`, the shorthands
  `_busy_timeout`/`_timeout`, `_foreign_keys`/`_fk`, `_synchronous`/`_sync`,
  `_auto_vacuum`/`_vacuum`, `_query_only` and `_journal`, and the time
  options `_time_format`, `_time_integer_format`, `_timezone`, `_inttotime`
  and `_texttotime`. Use `_pragma` for the PRAGMAs; any other unknown
  parameter starting with `_` is now an error. `vfs` is no longer read by the
  driver; SQLite still reads it from a `file:` URI.
- Time conversion: a `time.Time` argument is stored as text that SQLite's
  date functions read, and `DATE`, `DATETIME` and `TIMESTAMP` columns read
  back as the text or number they hold.
- `sql.Named` arguments: every argument binds by position, named SQL
  parameters included.
- Column type metadata (`rows.ColumnTypes()` reports names only), the
  shared-cache unlock-notify retry, `ErrorCodeString` and `Version`.
- The `lib` package, the transpiled C API, now `internal/lib`.
- The exported `Driver` and `Error` types. The package exports nothing; an
  error SQLite reports still has a `Code() int` method, which
  `errors.As(err, &e)` with `var e interface{ Code() int }` reaches.

What remains: `sql.Open("sqlite", dsn)` with the `_pragma`, `_journal_mode`
and `_txlock` DSN parameters documented in the package documentation. OFD
locking can still be enabled with `MODERNC_SQLITE_OFD_LOCK=1`, which the SQLite
core reads itself.

Generated sources (deduplication)
---------------------------------

The transpiled SQLite C amalgamation in `internal/lib/` ships one generated Go file per
`GOOS`/`GOARCH`. Declarations that are byte-identical across targets are folded
into build-tagged shared files — `internal/lib/sqlite.go` plus `internal/lib/sqlite_g_<hex>.go` —
by [`modernc.org/undup`](https://gitlab.com/cznic/undup), which upstream runs
when vendoring; this fork receives the folded files as-is.
Go's build constraints make every target compile exactly
the same set of declarations as before, so this is purely a packaging change: it
keeps each tag's module download well under Go's 500&nbsp;MB cap and does not
affect the public API or behavior.

To read or debug a single target's full, self-contained generated source, expand
the tree back to one complete file per target:

```sh
go run modernc.org/undup@v0.0.5 -expand -dir internal/lib   # writes full internal/lib/sqlite_<goos>_<goarch>.go
```

This removes the shared `*_g_*.go` files and rewrites each
`*_<goos>_<goarch>.go` as a standalone file — convenient for grepping, reading, or
stepping through one platform's code. Restore the committed, deduplicated form
with `git checkout -- internal/lib`, or re-fold in place with
`go run modernc.org/undup@v0.0.5 -dir internal/lib`. Hand-written platform
files (`libsqlite3_*.go`, `hooks_*.go`, …) carry no generated-code marker and are
never touched by either step.
