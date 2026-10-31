// Copyright (c) 2014 The mathutil Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE-MATHUTIL file.
//
// Derived from modernc.org/mathutil v1.7.1, file mathutil.go. These five
// functions are the only reachable part of it: libc calls them for
// __builtin_{add,sub,mul}_overflow and the calloc size check. Their bodies
// are verbatim; only the names were lowercased, to keep them out of libc's
// exported API. Extracting them lets the rest of mathutil.go go, along with
// the 6995-line tables.go it depended on -- a Miller-Rabin primality table
// and a popcount table, used only by the number-theory helpers.
//
// Note: the LICENSE file in this directory belongs to libc, a different
// project with a different copyright holder. mathutil's terms -- the same
// BSD-3-Clause text, a different copyright line -- are in LICENSE-MATHUTIL,
// with AUTHORS-MATHUTIL and CONTRIBUTORS-MATHUTIL naming the holders.

package libc

import "math"

func addOverflowInt64(a, b int64) (r int64, ovf bool) {
	r = a + b
	if a > 0 && b > 0 {
		return r, uint64(r) > math.MaxInt64
	}

	if a < 0 && b < 0 {
		return r, uint64(r) <= math.MaxInt64
	}

	return r, false
}

func subOverflowInt64(a, b int64) (r int64, ovf bool) {
	r = a - b
	if a >= 0 && b < 0 {
		return r, uint64(r) >= math.MaxInt64+1
	}

	if a < 0 && b > 0 {
		return r, uint64(r) <= math.MaxInt64
	}

	return r, false
}

func mulOverflowInt64(a, b int64) (r int64, ovf bool) {
	// https://groups.google.com/g/golang-nuts/c/h5oSN5t3Au4/m/KaNQREhZh0QJ
	const mostPositive = 1<<63 - 1
	const mostNegative = -(mostPositive + 1)
	r = a * b
	if a == 0 || b == 0 || a == 1 || b == 1 {
		return r, false
	}

	if a == mostNegative || b == mostNegative {
		return r, true
	}

	return r, r/b != a
}

func mulUint128_64(a, b uint64) (hi, lo uint64) {
	/*
		2^(2 W) ahi bhi + 2^W alo bhi + 2^W ahi blo + alo blo

		FEDCBA98 76543210 FEDCBA98 76543210
		                  ---- alo*blo ----
		         ---- alo*bhi ----
		         ---- ahi*blo ----
		---- ahi*bhi ----
	*/
	const w = 32
	const m = 1<<w - 1
	ahi, bhi, alo, blo := a>>w, b>>w, a&m, b&m
	lo = alo * blo
	mid1 := alo * bhi
	mid2 := ahi * blo
	c1, lo := addUint128_64(lo, mid1<<w)
	c2, lo := addUint128_64(lo, mid2<<w)
	_, hi = addUint128_64(ahi*bhi, mid1>>w+mid2>>w+c1+c2)
	return
}

// addUint128_64 returns the uint128 bit sum of uint64 a and b. Extracted
// alongside mulUint128_64, which calls it.
func addUint128_64(a, b uint64) (hi uint64, lo uint64) {
	lo = a + b
	if lo < a {
		hi = 1
	}
	return hi, lo
}
