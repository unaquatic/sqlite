module github.com/unaquatic/sqlite

go 1.27.0

require golang.org/x/sys v0.48.0

// Fork of modernc.org/sqlite v1.59.0 (embedded SQLite 3.53.4).
//
// Upstream's retract directives were dropped: they name modernc.org/sqlite
// versions and carry no meaning for this fork's independent version line.
//
// The modernc.org/{libc,mathutil,memory} modules are vendored under
// internal/ rather than required here, so the version-lockstep requirement
// from upstream doc.go is now satisfied structurally instead of by a pin:
//
//	internal/libc    <- modernc.org/libc     v1.75.7
//	                    modernc.org/mathutil v1.7.1 (reachable subset, folded
//	                    into int128_math.go and overflow.go)
//	internal/memory  <- modernc.org/memory   v1.12.1
//
// They live under internal/ deliberately. libc keeps process-global C state
// (allocator, fd table, stdio tables), so two copies in one binary would be
// unsafe; internal/ makes the compiler reject any outside import of them.
//
// Syncing upstream sqlite therefore also means re-vendoring a matched libc.
