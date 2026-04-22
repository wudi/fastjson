// NEON arm64 port of the amd64 skipws_amd64.s. Finds the first
// non-whitespace byte (not space/tab/LF/CR) in p[0:n], processing
// 16 bytes per iteration via VCMEQ.
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

// func skipWSSIMD(p *byte, n int) int
TEXT ·skipWSSIMD(SB), NOSPLIT, $0-24
	MOVD p+0(FP), R0
	MOVD n+8(FP), R1

	MOVD ZR, R2

	// Broadcast constants once to high registers.
	VMOVI $0x20, V24.B16                // ' '
	VMOVI $0x09, V25.B16                // '\t'
	VMOVI $0x0a, V26.B16                // '\n'
	VMOVI $0x0d, V27.B16                // '\r'

loop16:
	SUB  R2, R1, R3
	CMP  $16, R3
	BLT  tail

	ADD  R0, R2, R4
	VLD1 (R4), [V0.B16]

	VCMEQ V0.B16, V24.B16, V2.B16
	VCMEQ V0.B16, V25.B16, V8.B16
	VORR  V8.B16, V2.B16, V9.B16
	VCMEQ V0.B16, V26.B16, V10.B16
	VORR  V10.B16, V9.B16, V9.B16
	VCMEQ V0.B16, V27.B16, V11.B16
	VORR  V11.B16, V9.B16, V9.B16

	// V9 byte lanes: 0xFF if WS, 0x00 otherwise. We want the first
	// NON-WS byte, so check "all WS" by ANDing both 64-bit halves.
	VMOV V9.D[0], R5
	VMOV V9.D[1], R6
	AND  R6, R5, R5
	CMP  $-1, R5
	BNE  tail                           // not all 16 are WS → drop to scalar

	ADD  $16, R2, R2
	B    loop16

tail:
	CMP  R1, R2
	BGE  done
	MOVBU (R0)(R2), R4
	CMP  $0x20, R4
	BEQ  next
	CMP  $0x09, R4
	BEQ  next
	CMP  $0x0a, R4
	BEQ  next
	CMP  $0x0d, R4
	BEQ  next
	B    done

next:
	ADD  $1, R2, R2
	B    tail

done:
	MOVD R2, ret+16(FP)
	RET
