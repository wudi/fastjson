// NEON arm64 port of the amd64 scan_amd64.s. Scans 16 bytes per loop
// via VCMEQ + VAND (for the ctl-char test) + VORR. First-match
// position within a matching 16-byte chunk is found by a short scalar
// tail (same pattern as Go's SWAR fallback). Works on every 64-bit
// ARM CPU Go supports (NEON is mandatory in ARMv8-A).
//
// Tested under qemu-aarch64-static via the same correctness suite as
// the pure-Go scanStringSWAR.
//
// Register usage:
//   R0  p (input base)
//   R1  n (length)
//   R2  off (current offset)
//   R3  remain (n - off)
//   R4  scratch / byte value
//   R5  scratch / "any match" OR-reduction result
//   R6  scratch (D[1] half)

#include "textflag.h"

// func scanStringSIMD(p *byte, n int) int
TEXT ·scanStringSIMD(SB), NOSPLIT, $0-24
	MOVD p+0(FP), R0
	MOVD n+8(FP), R1

	MOVD ZR, R2                         // off = 0

	// Broadcast constants once.
	VMOVI $0x22, V1.B16                 // '"'
	VMOVI $0x5c, V3.B16                 // '\\'
	VMOVI $0xe0, V5.B16                 // ctl-detect mask: (byte & 0xe0) == 0 ⇔ byte < 0x20
	VMOVI $0x00, V7.B16

loop16:
	SUB  R2, R1, R3                     // remain = n - off
	CMP  $16, R3
	BLT  tail

	// Load 16 bytes starting at p+off. Use a tmp base R4 = p + off.
	ADD  R0, R2, R4
	VLD1 (R4), [V0.B16]

	VCMEQ V0.B16, V1.B16, V2.B16        // == '"'
	VCMEQ V0.B16, V3.B16, V8.B16        // == '\\'
	VORR  V8.B16, V2.B16, V9.B16

	VAND  V5.B16, V0.B16, V10.B16       // byte & 0xe0
	VCMEQ V10.B16, V7.B16, V11.B16      // == 0  ⇒  byte < 0x20
	VORR  V11.B16, V9.B16, V9.B16

	// "any match in the 16-byte chunk" — reduce via two VMOV halves.
	VMOV V9.D[0], R5
	VMOV V9.D[1], R6
	ORR  R6, R5, R5
	CBZ  R5, no_match16

	// Some byte in p[off:off+16] matches; fall into scalar to pin
	// the exact position. Scalar tail will scan from off.
	B    tail

no_match16:
	ADD  $16, R2, R2
	B    loop16

tail:
	CMP  R1, R2
	BGE  notfound
	MOVBU (R0)(R2), R4
	CMP  $0x22, R4
	BEQ  done
	CMP  $0x5c, R4
	BEQ  done
	CMP  $0x20, R4
	BLO  done
	ADD  $1, R2, R2
	B    tail

notfound:
	MOVD R1, R2                         // return n

done:
	MOVD R2, ret+16(FP)
	RET
