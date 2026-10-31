// Copyright 2020 The Libc Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// This fork does not vendor libc's nofsync.go, so this file is unconditional
// and the upstream libc.nofsync build tag has no effect.

package libc // import "github.com/unaquatic/sqlite/internal/libc"

const noFsync = false
