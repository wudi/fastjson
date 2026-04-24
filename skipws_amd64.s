// AVX-512BW skipWSSIMD. Finds the first byte > 0x20 in p[0:n].
// In JSON structural positions every byte ≤ 0x20 is either whitespace
// (space / tab / LF / CR) or a malformed byte that the next token parse
// rejects, so "byte > 0x20" is equivalent to "non-WS". That collapses
// the 4×VPCMPEQB + OR-tree of the strict kernel into a single VPCMPUB
// compare per 64-byte chunk.

#include "textflag.h"

// func skipWSSIMD(p *byte, n int) int
// Requires: AVX, AVX512BW, AVX512F, BMI
TEXT ·skipWSSIMD(SB), NOSPLIT, $0-24
	MOVQ         p+0(FP), AX
	MOVQ         n+8(FP), CX
	XORQ         DX, DX
	MOVQ         $0x00000020, BX
	VPBROADCASTB BX, Z0

loop:
	MOVQ      CX, BX
	SUBQ      DX, BX
	CMPQ      BX, $0x00000040
	JL        tail
	VMOVDQU64 (AX)(DX*1), Z1
	// K1[i] = 1 iff Z1[i] > Z0[i]  (byte > 0x20 ⇒ non-WS candidate).
	VPCMPUB   $0x06, Z0, Z1, K1
	KTESTQ    K1, K1
	JNZ       found64
	ADDQ      $0x00000040, DX
	JMP       loop

found64:
	KMOVQ  K1, BX
	TZCNTQ BX, BX
	ADDQ   BX, DX
	MOVQ   DX, ret+16(FP)
	VZEROUPPER
	RET

tail:
tail_loop:
	CMPQ DX, CX
	JGE  done
	MOVB (AX)(DX*1), BL
	CMPB BL, $0x20
	JA   done
	INCQ DX
	JMP  tail_loop

done:
	MOVQ DX, ret+16(FP)
	VZEROUPPER
	RET
