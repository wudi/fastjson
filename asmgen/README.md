# asmgen

Avo-based source for the three amd64 assembly kernels in this package.

## Regenerate

From this directory:

```sh
go run gen.go              -out ../scan_amd64.s        -pkg jsonx
go run gen_skipws.go       -out ../skipws_amd64.s      -pkg jsonx
go run gen_writedigits.go  -out ../writedigits_amd64.s -pkg jsonx
```

## Kernels

- `gen.go` → `scanStringAVX512` — AVX-512BW string-body scan (finds
  `"`, `\\`, or `<0x20`). 64 B/inst loop with VMOVDQU64 + VPCMPEQB ×2 +
  VPCMPUB + KORQ + KMOVQ + TZCNTQ. Gated at the call site on
  `hasAVX512` and `n >= 64`; pure-Go SWAR fallback.

- `gen_skipws.go` → `skipWSAVX512` — AVX-512BW whitespace skipper
  (`' '`, `\t`, `\n`, `\r`). Same 64 B/inst pattern with one
  VPCMPEQB per whitespace byte, KORQ'd together, then KNOTQ + KTESTQ.

- `gen_writedigits.go` → `writeDigitsAsm` — Schubfach digit-emission
  kernel. One 64-bit DIV by 1e8 splits off the top 8 digits, then
  unrolled 4-digit chunks via IMUL3Q-based magic-div-by-100 with MOVW
  stores into the packed `digits100` LUT. Always available on amd64
  (IMUL3Q + DIV); `hasBMI2ADX` runtime detection is wired in
  `writedigits_amd64.go` as the slot for a future MULX+ADX rewrite of
  `roundOdd` or `f64todec`.

All three are ABI0 (stack-based) with the `//go:noescape` pragma. The
generated `.s` files and their Go stubs are checked in — you don't
need to run the generator unless you're modifying a kernel.
