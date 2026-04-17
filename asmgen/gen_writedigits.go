package main

import (
	. "github.com/mmcloughlin/avo/build"
	. "github.com/mmcloughlin/avo/operand"
	"github.com/mmcloughlin/avo/reg"
)

// writeDigitsAsm(sig uint64, buf *byte, cnt uint64, trim uint64, tab *uint16) uint64
//
// Emits exactly `cnt` decimal digits of `sig` into buf[0:cnt], most
// significant first. `cnt` must be in [1, 17]. When `trim` is nonzero
// and the trailing 8 digits of `sig` are all zero, those 8 bytes are
// NOT written and the return value is `cnt-8`; otherwise return is `cnt`.
//
// `tab` is the caller's packed 2-digit ASCII LUT (Go-side `digits100`).
func main() {
	TEXT("writeDigitsAsm", NOSPLIT, "func(sig uint64, buf *byte, cnt uint64, trim uint64, tab *uint16) uint64")
	Pragma("noescape")

	sig := Load(Param("sig"), GP64())
	buf := Load(Param("buf"), GP64())
	cnt := Load(Param("cnt"), GP64())
	trim := Load(Param("trim"), GP64())
	tab := Load(Param("tab"), GP64())

	p := GP64()
	MOVQ(cnt, p)
	retval := GP64()
	MOVQ(cnt, retval)

	// Check high 32 bits. Zero → skip big path.
	hi := GP64()
	MOVQ(sig, hi)
	SHRQ(U8(32), hi)
	TESTQ(hi, hi)
	JZ(LabelRef("loop4"))

	// --- big path: 1× DIV by 1e8 splits off top 8 digits ---
	e8 := GP64()
	MOVQ(U64(100000000), e8)
	XORQ(reg.RDX, reg.RDX)
	MOVQ(sig, reg.RAX)
	DIVQ(e8) // RAX = q = sig/1e8, RDX = r = sig%1e8

	q := GP64()
	r := GP64()
	MOVQ(reg.RAX, q)
	MOVQ(reg.RDX, r)

	// Continue with sig = q regardless of trim decision.
	MOVQ(q, sig)

	// if trim != 0 && r == 0: skip emission of 8 zeros.
	TESTQ(trim, trim)
	JZ(LabelRef("emit8"))
	TESTQ(r, r)
	JNZ(LabelRef("emit8"))
	SUBQ(U32(8), retval)
	SUBQ(U32(8), p)
	JMP(LabelRef("loop4"))

	Label("emit8")
	// Split r (0..99_999_999) into hi4 = r/1e4, lo4 = r%1e4.
	e4 := GP64()
	MOVQ(U64(10000), e4)
	XORQ(reg.RDX, reg.RDX)
	MOVQ(r, reg.RAX)
	DIVQ(e4)
	hi4 := GP64()
	lo4 := GP64()
	MOVQ(reg.RAX, hi4) // hi4 (0..9999)
	MOVQ(reg.RDX, lo4) // lo4 (0..9999)

	// lo4 → buf[p-4..p-1] (two 2-digit pairs).
	//   lo4_lo2 = lo4 % 100, lo4_hi2 = lo4 / 100
	//   MOVW tab[lo4_lo2*2] → buf[p-2]
	//   MOVW tab[lo4_hi2*2] → buf[p-4]
	emit4(buf, tab, p, lo4, -2, -4)
	// hi4 → buf[p-8..p-5]
	emit4(buf, tab, p, hi4, -6, -8)

	SUBQ(U32(8), p)

	// --- small loop: emit 4 digits at a time from sig ---
	Label("loop4")
	CMPQ(sig, U32(10000))
	JL(LabelRef("tail"))

	// m = sig % 1e4, sig = sig / 1e4
	e4b := GP64()
	MOVQ(U64(10000), e4b)
	XORQ(reg.RDX, reg.RDX)
	MOVQ(sig, reg.RAX)
	DIVQ(e4b)
	m := GP64()
	MOVQ(reg.RAX, sig)
	MOVQ(reg.RDX, m)

	emit4(buf, tab, p, m, -2, -4)
	SUBQ(U32(4), p)
	JMP(LabelRef("loop4"))

	// --- tail: sig is now in [0, 9999], 1..4 digits remaining ---
	Label("tail")
	CMPQ(sig, U32(100))
	JL(LabelRef("under100"))
	// 3 or 4 digits: lo2 = sig%100 at buf[p-2]; sig/=100 (still <=99).
	lo2 := GP64()
	IMUL3Q(U32(10486), sig, lo2)
	SHRQ(U8(20), lo2) // lo2 = sig/100
	siglo2 := GP64()
	IMUL3Q(U8(100), lo2, siglo2)
	SUBQ(siglo2, sig) // sig = sig%100
	// store lo pair (sig) at buf[p-2]
	w := GP64()
	MOVWQZX(Mem{Base: tab, Index: sig, Scale: 2}, w)
	MOVW(w.As16(), Mem{Base: buf, Index: p, Scale: 1, Disp: -2})
	SUBQ(U32(2), p)
	MOVQ(lo2, sig) // sig = hi2 (0..99)

	Label("under100")
	CMPQ(sig, U32(10))
	JL(LabelRef("single"))
	w2 := GP64()
	MOVWQZX(Mem{Base: tab, Index: sig, Scale: 2}, w2)
	MOVW(w2.As16(), Mem{Base: buf, Index: p, Scale: 1, Disp: -2})
	JMP(LabelRef("done"))

	Label("single")
	// buf[p-1] = '0' + sig  (sig ∈ [0,9])
	ADDQ(U32('0'), sig)
	// Store the low byte via AL.
	MOVQ(sig, reg.RAX)
	MOVB(reg.AL, Mem{Base: buf, Index: p, Scale: 1, Disp: -1})

	Label("done")
	Store(retval, ReturnIndex(0))
	RET()

	Generate()
}

// emit4 writes the 4 decimal digits of v (0..9999) to
// buf[p+offHi..p+offHi+1] (= hundreds digits) and
// buf[p+offLo..p+offLo+1] (= units digits).
//
// v is clobbered (used as a scratch).
func emit4(buf, tab, p, v reg.Register, offLo, offHi int) {
	// hi2 = v/100 via (v * 10486) >> 20 (valid for v ≤ 16383).
	hi2 := GP64()
	IMUL3Q(U32(10486), v, hi2)
	SHRQ(U8(20), hi2)
	// lo2 = v - hi2*100
	scratch := GP64()
	IMUL3Q(U8(100), hi2, scratch)
	SUBQ(scratch, v) // v now holds lo2

	w1 := GP64()
	MOVWQZX(Mem{Base: tab, Index: v, Scale: 2}, w1)
	MOVW(w1.As16(), Mem{Base: buf, Index: p, Scale: 1, Disp: offLo})

	w2 := GP64()
	MOVWQZX(Mem{Base: tab, Index: hi2, Scale: 2}, w2)
	MOVW(w2.As16(), Mem{Base: buf, Index: p, Scale: 1, Disp: offHi})
}
