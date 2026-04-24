// NEON arm64 skipWSSIMD. Finds the first byte > 0x20 in p[0:n].
// For JSON structural positions, the only bytes ≤ 0x20 that can
// appear are WS (0x09/0x0a/0x0d/0x20); any other low byte is malformed
// and will be rejected by the next value parse. Treating "byte ≤ 0x20"
// as WS lets the kernel run a single CMHI compare per 16 bytes instead
// of four VCMEQ + OR tree.
//
// Go's arm64 assembler doesn't spell CMHI directly, so the single-op
// compare is emitted via WORD. Encoding: 0Q 1 01110 size 1 Rm 001101
// Rn Rd — for 16B (Q=1, size=00) base 0x6E203400, then + (Rm<<16) +
// (Rn<<5) + Rd.
//
// Register usage:
//   R0  p
//   R1  n
//   R2  off
//   R3  remain
//   R4  scratch / byte
//   R5  OR-reduction scratch
//   R6  D[1] half

#include "textflag.h"

// CMHI V9.16B, V0.16B, V1.16B  (V9[i] = (V0[i] > V1[i]))
#define CMHI_V0_V1_V9 WORD $0x6E213409

// func skipWSSIMD(p *byte, n int) int
TEXT ·skipWSSIMD(SB), NOSPLIT, $0-24
	MOVD p+0(FP), R0
	MOVD n+8(FP), R1
	MOVD ZR, R2

	VMOVI $0x20, V1.B16                 // ' '

loop16:
	SUB  R2, R1, R3
	CMP  $16, R3
	BLT  tail

	ADD  R0, R2, R4
	VLD1 (R4), [V0.B16]

	// V9[i] = 0xFF if V0[i] > V1[i] (byte > 0x20 — non-WS candidate).
	CMHI_V0_V1_V9

	// "any non-WS in 16" — OR the two 8-byte halves; nonzero ⇒ break.
	VMOV V9.D[0], R5
	VMOV V9.D[1], R6
	ORR  R6, R5, R5
	CBNZ R5, tail

	ADD  $16, R2, R2
	B    loop16

tail:
	CMP  R1, R2
	BGE  done
	MOVBU (R0)(R2), R4
	CMP  $0x20, R4
	BHI  done                           // > 0x20 ⇒ stop (any non-WS byte)
	ADD  $1, R2, R2
	B    tail

done:
	MOVD R2, ret+16(FP)
	RET
