// Copyright 2026 The Sqlite Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package libc

import (
	"math"
	"math/big"
	"testing"
)

// These five functions were extracted verbatim from upstream's mathutil.go so
// that the rest of that file, and the 6995-line tables.go it depends on, could
// be dropped. libc calls them for __builtin_{add,sub,mul}_overflow and for the
// calloc size check, so a silent error here would corrupt allocation sizes.
func TestOverflowHelpers(t *testing.T) {
	t.Run("addOverflowInt64", func(t *testing.T) {
		for _, c := range []struct {
			a, b int64
			r    int64
			ovf  bool
		}{
			{1, 2, 3, false},
			{0, 0, 0, false},
			{-1, 1, 0, false},
			{math.MaxInt64, 0, math.MaxInt64, false},
			{math.MaxInt64, 1, math.MinInt64, true},
			{math.MaxInt64, math.MaxInt64, -2, true},
			{math.MinInt64, -1, math.MaxInt64, true},
			{math.MinInt64, math.MinInt64, 0, true},
		} {
			r, ovf := addOverflowInt64(c.a, c.b)
			if r != c.r || ovf != c.ovf {
				t.Errorf("addOverflowInt64(%d,%d) = (%d,%v), want (%d,%v)", c.a, c.b, r, ovf, c.r, c.ovf)
			}
		}
	})

	t.Run("subOverflowInt64", func(t *testing.T) {
		for _, c := range []struct {
			a, b int64
			r    int64
			ovf  bool
		}{
			{3, 2, 1, false},
			{0, 0, 0, false},
			{math.MinInt64, 0, math.MinInt64, false},
			{math.MinInt64, 1, math.MaxInt64, true},
			{math.MaxInt64, -1, math.MinInt64, true},
		} {
			r, ovf := subOverflowInt64(c.a, c.b)
			if r != c.r || ovf != c.ovf {
				t.Errorf("subOverflowInt64(%d,%d) = (%d,%v), want (%d,%v)", c.a, c.b, r, ovf, c.r, c.ovf)
			}
		}
	})

	t.Run("mulOverflowInt64", func(t *testing.T) {
		for _, c := range []struct {
			a, b int64
			ovf  bool
		}{
			{0, 0, false},
			{1, math.MaxInt64, false},
			{math.MaxInt64, 1, false},
			{2, math.MaxInt64, true},
			{math.MaxInt64, 2, true},
			{math.MinInt64, -1, true},
			{-1, math.MinInt64, true},
			{1 << 31, 1 << 31, false},
			{1 << 32, 1 << 32, true},
		} {
			_, ovf := mulOverflowInt64(c.a, c.b)
			if ovf != c.ovf {
				t.Errorf("mulOverflowInt64(%d,%d) overflow = %v, want %v", c.a, c.b, ovf, c.ovf)
			}
		}
	})

	// Cross-check the 128-bit product against math/big over the full range.
	t.Run("mulUint128_64", func(t *testing.T) {
		for _, c := range [][2]uint64{
			{0, 0},
			{1, 1},
			{math.MaxUint64, 1},
			{math.MaxUint64, math.MaxUint64},
			{1 << 32, 1 << 32},
			{0xdeadbeefcafebabe, 0x0123456789abcdef},
		} {
			hi, lo := mulUint128_64(c[0], c[1])

			got := new(big.Int).Lsh(new(big.Int).SetUint64(hi), 64)
			got.Or(got, new(big.Int).SetUint64(lo))

			want := new(big.Int).Mul(
				new(big.Int).SetUint64(c[0]),
				new(big.Int).SetUint64(c[1]),
			)
			if got.Cmp(want) != 0 {
				t.Errorf("mulUint128_64(%d,%d) = %s, want %s", c[0], c[1], got, want)
			}
		}
	})

	t.Run("addUint128_64", func(t *testing.T) {
		for _, c := range [][2]uint64{
			{0, 0},
			{1, 1},
			{math.MaxUint64, 0},
			{math.MaxUint64, 1},
			{math.MaxUint64, math.MaxUint64},
		} {
			hi, lo := addUint128_64(c[0], c[1])

			got := new(big.Int).Lsh(new(big.Int).SetUint64(hi), 64)
			got.Or(got, new(big.Int).SetUint64(lo))

			want := new(big.Int).Add(
				new(big.Int).SetUint64(c[0]),
				new(big.Int).SetUint64(c[1]),
			)
			if got.Cmp(want) != 0 {
				t.Errorf("addUint128_64(%d,%d) = %s, want %s", c[0], c[1], got, want)
			}
		}
	})
}
