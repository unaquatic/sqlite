// Copyright (c) 2018 The mathutil Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE-MATHUTIL file.
//
// Derived from modernc.org/mathutil v1.7.1, file int.go. This fork vendored
// that module, then folded the reachable part into package libc. The only
// changes are identifier renames, to avoid colliding with libc's own Int128
// and Uint128, plus the bigOne variable relocated from mathutil.go. No logic
// was modified.
//
// Note: the LICENSE file in this directory belongs to libc, a different
// project with a different copyright holder. mathutil's terms -- the same
// BSD-3-Clause text, a different copyright line -- are in LICENSE-MATHUTIL,
// with AUTHORS-MATHUTIL and CONTRIBUTORS-MATHUTIL naming the holders.

package libc

import (
	"fmt"
	"math"
	"math/big"
)

var (
	// maxMathInt128 represents the maximun mathInt128 value as big.Int
	maxMathInt128 *big.Int
	// minMathInt128 represents the minimun mathInt128 value as big.Int
	minMathInt128 *big.Int
	// maxMathUint128 represents the maximun mathUint128 value as big.Int
	maxMathUint128 *big.Int
)

// bigOne lived in upstream's mathutil.go, which this fork does not vendor.
var bigOne = big.NewInt(1)

func init() {
	var ok bool
	maxMathInt128, ok = big.NewInt(0).SetString("0x7fffffff_ffffffff_ffffffff_ffffffff", 0)
	if !ok {
		panic("internal error")
	}

	minMathInt128 = big.NewInt(0).Set(maxMathInt128)
	minMathInt128.Add(minMathInt128, bigOne)
	minMathInt128.Neg(minMathInt128)

	maxMathUint128, ok = big.NewInt(0).SetString("0xffffffff_ffffffff_ffffffff_ffffffff", 0)
	if !ok {
		panic("internal error")
	}
}

const (
	maxInt128  = 1<<127 - 1
	maxUint128 = 1<<128 - 1
	minInt128  = -maxInt128 - 1
)

// mathInt128 is an 128 bit signed integer.
type mathInt128 struct {
	Lo int64 // Bits 63..0.
	Hi int64 // Bits 127..64.
}

// Add returns the sum of x and y and a carry indication.
func (x mathInt128) Add(y mathInt128) (r mathInt128, cy bool) {
	r.Lo = x.Lo + y.Lo
	r.Hi = x.Hi + y.Hi
	if uint64(r.Lo) < uint64(x.Lo) {
		r.Hi++
	}
	return r, (r.Cmp(x) < 0) == (y.Sign() >= 0)
}

// BigInt returns x in the form of a big.Int.
func (x mathInt128) BigInt() *big.Int {
	r := big.NewInt(x.Hi)
	r.Lsh(r, 64)
	lo := big.NewInt(0)
	lo.SetUint64(uint64(x.Lo))
	return r.Add(r, lo)
}

// Cmp compares x and y and returns:
//
//	-1 if x <  y
//	 0 if x == y
//	+1 if x >  y
func (x mathInt128) Cmp(y mathInt128) int {
	if x.Hi > y.Hi {
		return 1
	}

	if x.Hi < y.Hi {
		return -1
	}

	if uint64(x.Lo) > uint64(y.Lo) {
		return 1
	}

	if uint64(x.Lo) < uint64(y.Lo) {
		return -1
	}

	return 0
}

// Neg returns -x and an indication that x was not equal to minMathInt128.
func (x mathInt128) Neg() (r mathInt128, ok bool) {
	if x == (mathInt128{Hi: math.MinInt64}) {
		return x, false
	}

	x.Lo = ^x.Lo
	x.Hi = ^x.Hi
	r, _ = x.Add(mathInt128{Lo: 1})
	return r, true
}

// SetBigInt sets x to y, returns x and an error, if any.
func (x *mathInt128) SetBigInt(y *big.Int) (r mathInt128, err error) {
	if y.Cmp(maxMathInt128) > 0 {
		return *x, fmt.Errorf("%T.SetInt: overflow", x)
	}
	if y.Cmp(minMathInt128) < 0 {
		return *x, fmt.Errorf("%T.SetInt: underflow", x)
	}
	neg := y.Sign() < 0
	var z big.Int
	z.Set(y)
	if neg {
		z.Neg(&z)
	}
	r.Lo = z.Int64()
	z.Rsh(&z, 64)
	r.Hi = z.Int64()
	if neg {
		r, _ = r.Neg()
	}
	*x = r
	return r, nil
}

// SetInt64 sets x to y and returns x.
func (x *mathInt128) SetInt64(y int64) (r mathInt128) {
	r.Lo = y
	if y >= 0 {
		r.Hi = 0
		*x = r
		return r
	}

	r.Hi = -1
	*x = r
	return r
}

// SetUint64 sets x to y and returns x.
func (x *mathInt128) SetUint64(y uint64) (r mathInt128) {
	r = mathInt128{Lo: int64(y)}
	*x = r
	return r
}

// Sign returns:
//
//	-1 if x <  0
//	 0 if x == 0
//	+1 if x >  0
func (x mathInt128) Sign() int {
	if x.Hi < 0 {
		return -1
	}

	if x.Hi != 0 || x.Lo != 0 {
		return 1
	}

	return 0
}

// String implements fmt.Stringer()
func (x mathInt128) String() string { return x.BigInt().String() }

// newMathInt128FromInt64 return a new mathInt128 value initialized to n.
func newMathInt128FromInt64(n int64) (r mathInt128) {
	r.Lo = n
	if n < 0 {
		r.Hi = -1
	}
	return r
}

// newMathInt128FromUint64 return a new mathInt128 value initialized to n.
func newMathInt128FromUint64(n uint64) (r mathInt128) { return mathInt128{Lo: int64(n)} }

// newMathInt128FromFloat32 returns a new mathInt128 value initialized to n. Result is
// not specified in n does not represent a number within the range of mathInt128
// values.
func newMathInt128FromFloat32(n float32) (r mathInt128) {
	if n >= minInt128 && n <= maxInt128 {
		if n >= math.MinInt64 && n <= math.MaxInt64 {
			return newMathInt128FromInt64(int64(n))
		}

		f := big.NewFloat(float64(n))
		bi, _ := f.Int(nil)
		r.SetBigInt(bi)
	}
	return r
}

// newMathInt128FromFloat64 returns a new mathInt128 value initialized to n. Result is
// not specified in n does not represent a number within the range of mathInt128
// values.
func newMathInt128FromFloat64(n float64) (r mathInt128) {
	if n >= minInt128 && n <= maxInt128 {
		if n >= math.MinInt64 && n <= math.MaxInt64 {
			return newMathInt128FromInt64(int64(n))
		}

		f := big.NewFloat(n)
		bi, _ := f.Int(nil)
		r.SetBigInt(bi)
	}
	return r
}

// mathUint128 is an 128 bit unsigned integer.
type mathUint128 struct {
	Lo uint64 // Bits 63..0.
	Hi uint64 // Bits 127..64.
}

// newMathUint128FromInt64 return a new mathUint128 value initialized to n.
func newMathUint128FromInt64(n int64) (r mathUint128) {
	r.Lo = uint64(n)
	if n < 0 {
		r.Hi = ^uint64(0)
	}
	return r
}

// newMathUint128FromUint64 return a new mathUint128 value initialized to n.
func newMathUint128FromUint64(n uint64) (r mathUint128) { return mathUint128{Lo: n} }

// newMathUint128FromFloat32 returns a new mathUint128 value initialized to n. Result is
// not specified in n does not represent a number within the range of mathUint128
// values.
func newMathUint128FromFloat32(n float32) (r mathUint128) {
	if n >= 0 {
		if n <= math.MaxUint64 {
			return newMathUint128FromUint64(uint64(n))
		}

		f := big.NewFloat(float64(n))
		bi, _ := f.Int(nil)
		r.SetBigInt(bi)
	}
	return r
}

// newMathUint128FromFloat64 returns a new mathUint128 value initialized to n. Result is
// not specified in n does not represent a number within the range of mathUint128
// values.
func newMathUint128FromFloat64(n float64) (r mathUint128) {
	if n >= 0 && n <= maxUint128 {
		if n <= math.MaxUint64 {
			return newMathUint128FromUint64(uint64(n))
		}

		f := big.NewFloat(n)
		bi, _ := f.Int(nil)
		r.SetBigInt(bi)
	}
	return r
}

// SetBigInt sets x to y, returns x and an error, if any.
func (x *mathUint128) SetBigInt(y *big.Int) (r mathUint128, err error) {
	if y.Sign() < 0 || y.Cmp(maxMathUint128) > 0 {
		return *x, fmt.Errorf("%T.SetInt: overflow", x)
	}

	var z big.Int
	z.Set(y)
	r.Lo = z.Uint64()
	z.Rsh(&z, 64)
	r.Hi = z.Uint64()
	*x = r
	return r, nil
}
