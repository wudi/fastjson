package fastjson

import (
	"strconv"
	"unsafe"
)

// pow10 stores exactly-representable powers of 10 for 0..22.
var pow10 = [23]float64{
	1e0, 1e1, 1e2, 1e3, 1e4, 1e5, 1e6, 1e7, 1e8, 1e9, 1e10,
	1e11, 1e12, 1e13, 1e14, 1e15, 1e16, 1e17, 1e18, 1e19, 1e20, 1e21, 1e22,
}

// scanNumber consumes a JSON number literal starting at d.p, advances d.p
// past it, and returns (value, ok). When ok is false, returns the raw slice
// for strconv fallback via scanNumberSlow.
//
// Single-pass design: accumulates mantissa in a uint64, decimal shift, and
// exponent. Applies Clinger fast path when feasible, else re-parses the same
// slice with strconv.
func (d *decoder) scanNumber() (float64, error) {
	b := d.data
	p := d.p
	start := p
	neg := false
	if p < len(b) && b[p] == '-' {
		neg = true
		p++
	}
	intStart := p
	var mant uint64
	digits := 0
	for p < len(b) {
		c := b[p]
		if c < '0' || c > '9' {
			break
		}
		mant = mant*10 + uint64(c-'0')
		digits++
		p++
	}
	if p == intStart {
		return 0, syntaxErr("invalid number", start)
	}
	frac := 0
	if p < len(b) && b[p] == '.' {
		p++
		fracStart := p
		for p < len(b) {
			c := b[p]
			if c < '0' || c > '9' {
				break
			}
			mant = mant*10 + uint64(c-'0')
			digits++
			p++
		}
		frac = p - fracStart
	}
	tooManyDigits := digits > 18
	exp := 0
	hasExp := false
	if p < len(b) && (b[p] == 'e' || b[p] == 'E') {
		hasExp = true
		p++
		eneg := false
		if p < len(b) && (b[p] == '+' || b[p] == '-') {
			if b[p] == '-' {
				eneg = true
			}
			p++
		}
		if p >= len(b) || b[p] < '0' || b[p] > '9' {
			return 0, syntaxErr("invalid exponent", p)
		}
		for p < len(b) {
			c := b[p]
			if c < '0' || c > '9' {
				break
			}
			exp = exp*10 + int(c-'0')
			if exp > 400 {
				tooManyDigits = true
			}
			p++
		}
		if eneg {
			exp = -exp
		}
	}
	d.p = p
	// Effective decimal exponent.
	effExp := exp - frac

	if !tooManyDigits {
		// Clinger fast path: mant must fit 2^53, |effExp| ≤ 22.
		if mant == 0 {
			if neg {
				return -0.0, nil
			}
			return 0.0, nil
		}
		if mant <= 1<<53 {
			if effExp == 0 {
				if neg {
					return -float64(mant), nil
				}
				return float64(mant), nil
			}
			if effExp > 0 && effExp <= 22 {
				f := float64(mant) * pow10[effExp]
				if neg {
					f = -f
				}
				return f, nil
			}
			if effExp < 0 && -effExp <= 22 {
				f := float64(mant) / pow10[-effExp]
				if neg {
					f = -f
				}
				return f, nil
			}
			// split: mant * 10^22 * 10^(effExp-22) when 22..37 range, still
			// exact when the product of the two powers stays within double
			// precision. Skip — fall through to strconv.
		}
	}
	_ = hasExp
	// Eisel-Lemire fast path — uses the mantissa and decimal exponent
	// already extracted above. Avoids strconv.ParseFloat's redundant
	// digit scan (was 25 % of canada decode CPU). Only valid when we
	// haven't overflowed the uint64 mantissa. Returns ok=false when the
	// answer is round-ambiguous; we then fall back to strconv.
	if !tooManyDigits {
		if f, ok := eiselLemire64(mant, effExp, neg); ok {
			return f, nil
		}
	}
	// True slow path for ambiguous / too-many-digits cases.
	f, err := strconv.ParseFloat(b2sUnsafe(b[start:p]), 64)
	if err != nil {
		return 0, syntaxErr("invalid number", start)
	}
	return f, nil
}

var _ = unsafe.Pointer(nil)
