package main

import (
	. "github.com/mmcloughlin/avo/build"
	. "github.com/mmcloughlin/avo/operand"
)

// skipWSAVX512(p *byte, n int) int
// Returns the index of the first byte in p[0..n] that is NOT one of
// space (0x20), tab (0x09), LF (0x0A), or CR (0x0D). Returns n if all
// bytes are whitespace.
func main() {
	TEXT("skipWSAVX512", NOSPLIT, "func(p *byte, n int) int")
	Pragma("noescape")

	p := Load(Param("p"), GP64())
	n := Load(Param("n"), GP64())

	off := GP64()
	XORQ(off, off)

	tmp := GP64()
	zSpace, zTab, zLF, zCR := ZMM(), ZMM(), ZMM(), ZMM()

	MOVQ(U32(0x20), tmp)
	VPBROADCASTB(tmp.As32(), zSpace)
	MOVQ(U32(0x09), tmp)
	VPBROADCASTB(tmp.As32(), zTab)
	MOVQ(U32(0x0A), tmp)
	VPBROADCASTB(tmp.As32(), zLF)
	MOVQ(U32(0x0D), tmp)
	VPBROADCASTB(tmp.As32(), zCR)

	Label("loop")
	remain := GP64()
	MOVQ(n, remain)
	SUBQ(off, remain)
	CMPQ(remain, U32(64))
	JL(LabelRef("tail"))

	z0 := ZMM()
	VMOVDQU64(Mem{Base: p, Index: off, Scale: 1}, z0)

	k1, k2, k3, k4, kOr := K(), K(), K(), K(), K()
	VPCMPEQB(zSpace, z0, k1)
	VPCMPEQB(zTab, z0, k2)
	VPCMPEQB(zLF, z0, k3)
	VPCMPEQB(zCR, z0, k4)
	KORQ(k1, k2, kOr)
	KORQ(kOr, k3, kOr)
	KORQ(kOr, k4, kOr)
	// kOr has bit=1 for WS bytes. We want first non-WS → NOT then TZCNT.
	KNOTQ(kOr, kOr)
	KTESTQ(kOr, kOr)
	JNZ(LabelRef("found64"))

	ADDQ(U32(64), off)
	JMP(LabelRef("loop"))

	Label("found64")
	bx := GP64()
	KMOVQ(kOr, bx)
	TZCNTQ(bx, bx)
	ADDQ(bx, off)
	Store(off, ReturnIndex(0))
	VZEROUPPER()
	RET()

	Label("tail")
	b := GP8()
	Label("tail_loop")
	CMPQ(off, n)
	JGE(LabelRef("notfound"))
	MOVB(Mem{Base: p, Index: off, Scale: 1}, b)
	CMPB(b, U8(0x20))
	JE(LabelRef("next"))
	CMPB(b, U8(0x09))
	JE(LabelRef("next"))
	CMPB(b, U8(0x0A))
	JE(LabelRef("next"))
	CMPB(b, U8(0x0D))
	JE(LabelRef("next"))
	// non-WS found at off
	JMP(LabelRef("done"))
	Label("next")
	INCQ(off)
	JMP(LabelRef("tail_loop"))

	Label("notfound")
	Label("done")
	Store(off, ReturnIndex(0))
	VZEROUPPER()
	RET()

	Generate()
}
