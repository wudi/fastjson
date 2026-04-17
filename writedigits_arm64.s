// Hand-written arm64 port of writedigits_amd64.s — Schubfach digit
// emission. UDIV+MUL+SUB dance instead of MSUB to keep operand order
// unambiguous. Parity-fuzzed against the pure-Go reference under
// qemu-aarch64-static.
//
// Register usage:
//   R0  sig (working value; clobbered through the algorithm)
//   R1  buf base
//   R2  cnt (copied to R5/R6; otherwise unused)
//   R3  trim
//   R4  tab (packed uint16 LUT base)
//   R5  p (write position)
//   R6  retval
//   R7  scratch: q / hi4
//   R8  scratch: divisor
//   R9  scratch: r / m
//   R10 scratch: lo4
//   R11 scratch: product for mod
//   R12 scratch: hi2
//   R13 scratch: lo2
//   R14 scratch: buf + p
//   R15 scratch: LUT halfword
//   R16 scratch: single-digit ASCII

#include "textflag.h"

// func writeDigitsAsm(sig uint64, buf *byte, cnt uint64, trim uint64, tab *uint16) uint64
TEXT ·writeDigitsAsm(SB), NOSPLIT, $0-48
	MOVD sig+0(FP), R0
	MOVD buf+8(FP), R1
	MOVD cnt+16(FP), R2
	MOVD trim+24(FP), R3
	MOVD tab+32(FP), R4

	MOVD R2, R5                         // p = cnt
	MOVD R2, R6                         // retval = cnt

	LSR  $32, R0, R7
	CBZ  R7, loop4

	// q = sig / 1e8, r = sig - q*1e8
	MOVD $100000000, R8
	UDIV R8, R0, R7                     // R7 = q
	MUL  R7, R8, R11                    // R11 = q*1e8
	SUB  R11, R0, R9                    // R9 = r
	MOVD R7, R0                         // sig = q

	CBZ  R3, emit8
	CBNZ R9, emit8
	SUB  $8, R6, R6
	SUB  $8, R5, R5
	B    loop4

emit8:
	// split r (R9) into hi4 (R7) and lo4 (R10) via /10000
	MOVD $10000, R8
	UDIV R8, R9, R7                     // hi4
	MUL  R7, R8, R11
	SUB  R11, R9, R10                   // lo4

	// emit4(lo4=R10, offLo=-2, offHi=-4)
	ADD  R1, R5, R14
	MOVD $100, R8
	UDIV R8, R10, R12                   // hi2
	MUL  R12, R8, R11
	SUB  R11, R10, R13                  // lo2
	MOVHU (R4)(R13<<1), R15
	MOVH  R15, -2(R14)
	MOVHU (R4)(R12<<1), R15
	MOVH  R15, -4(R14)

	// emit4(hi4=R7, offLo=-6, offHi=-8)
	UDIV R8, R7, R12                    // hi2
	MUL  R12, R8, R11
	SUB  R11, R7, R13                   // lo2
	MOVHU (R4)(R13<<1), R15
	MOVH  R15, -6(R14)
	MOVHU (R4)(R12<<1), R15
	MOVH  R15, -8(R14)

	SUB  $8, R5, R5

loop4:
	CMP  $10000, R0
	BLT  tail
	MOVD $10000, R8
	UDIV R8, R0, R7                     // q
	MUL  R7, R8, R11
	SUB  R11, R0, R9                    // m
	MOVD R7, R0                         // sig = q

	ADD  R1, R5, R14
	MOVD $100, R8
	UDIV R8, R9, R12
	MUL  R12, R8, R11
	SUB  R11, R9, R13
	MOVHU (R4)(R13<<1), R15
	MOVH  R15, -2(R14)
	MOVHU (R4)(R12<<1), R15
	MOVH  R15, -4(R14)

	SUB  $4, R5, R5
	B    loop4

tail:
	CMP  $100, R0
	BLT  under100
	// sig ≥ 100: emit lo2 at p-2, sig = hi2
	ADD  R1, R5, R14
	MOVD $100, R8
	UDIV R8, R0, R12                    // hi2 = sig/100
	MUL  R12, R8, R11
	SUB  R11, R0, R13                   // lo2 = sig%100
	MOVHU (R4)(R13<<1), R15
	MOVH  R15, -2(R14)
	SUB  $2, R5, R5
	MOVD R12, R0                        // sig = hi2

under100:
	CMP  $10, R0
	BLT  single
	ADD  R1, R5, R14
	MOVHU (R4)(R0<<1), R15
	MOVH  R15, -2(R14)
	B    done

single:
	ADD  $0x30, R0, R16
	ADD  R1, R5, R14
	MOVB R16, -1(R14)

done:
	MOVD R6, ret+40(FP)
	RET
