# Plan: hand-written float64→string formatter to close canada encode

## Context

After E19 (Eisel-Lemire port) the only remaining benchmark behind
`bytedance/sonic` is canada encode (+12.6 %). Profile: 71 % of canada
encode CPU sits in `strconv.genericFtoa` — Go stdlib's Ryu. Sonic's
native encoder is hand-written asm; it's ≈ 10 % faster than stdlib Ryu.

Closing this gap is the last open target for the library to be
≥ 10 % faster than sonic on **all 9** canonical benchmarks.

## Correction from initial framing

I was planning to "port Ryu to asm". Research surfaced two facts that
change the plan materially:

1. **sonic doesn't use Ryu.** Its `native/f64toa.c` is a port of
   Alexander Bolz's **Schubfach** (BSL-1.0). Schubfach's inner loop is
   strictly shorter than Ryu's — one 128-bit multiply + a round-odd
   step, vs. Ryu's mul-low + mul-upper + mul-center triplet and its
   exact-trailing-zeros search. Most of sonic's ≈ 10 % edge over Go
   stdlib is **algorithmic, not asm**.
2. **A pure-Go Schubfach port alone is projected to recover 5-8 %** of
   the 12.6 % gap before any asm is touched. That shifts the order of
   operations: pure-Go port first, asm second, and the asm budget ends
   up being smaller than I initially estimated.

## Phases

### Phase 0 — de-risk the premise (~30 min)

Before touching any code, confirm the gap is actually in the
formatter and not in the buffer append / caller boilerplate. Write a
micro-bench that calls *only* `strconv.AppendFloat` on the exact
distribution of float values in canada.json, and compare to
`sonic.Marshal` of those same floats wrapped in a `[]float64`. If
the **isolated** gap is <8 %, phase 1 alone will probably be enough.

### Phase 1 — pure-Go Schubfach port (~1 day, ~350 LOC)

Port sonic's `native/f64toa.c` → `ryu_schubfach.go` keeping BSL-1.0
attribution header. Reuse Go stdlib's
`strconv.detailedPowersOfTen[696][2]uint64` table (already in
`eisel_lemire.go` from E19 — no new table needed).

Entry points:
- `schubfachD2d(bits uint64) (sig uint64, exp int32)` — core
- `writeDec(buf []byte, sig uint64, exp int32) []byte` — format
- `appendFloat64(buf []byte, f float64) []byte` — public

Integrate into `encode.go:writeFloat` behind a `hasSchubfach` build-
tag-ish boolean so we can A/B test vs. `strconv.AppendFloat`.

**Projected win on its own: 5-8 %** (~ canada encode +12.6 % → +4 to
+7 %). Already closes most of the gap.

### Phase 2 — digit emission asm (~1 day, ~200 LOC via avo)

The hot sub-loop after `schubfachD2d` is emitting 1-17 decimal
digits. Sonic's C uses a two-digit LUT (`"00".."99"`, 200 bytes)
with `movw` writes. Go's compiler can't fully elide the bounds checks
on this LUT + the concat-style buffer extension.

Write `writeDecAsm(buf *byte, sig uint64, exp int32) int` in avo,
returning bytes written. Correctness oracle: differential against the
pure-Go `writeDec` from phase 1.

**Projected additional win: 3-5 %.**

### Phase 3 — `schubfachD2d` core in asm with MULX + ADX (~2 days, ~250 LOC via avo)

Where MULX/ADCX/ADOX actually pay: the inner `mul128` is
```
hi, lo = bits.Mul64(m, pow[0])
...    = bits.Mul64(m, pow[1])
add with carry
```
Go compiles `bits.Mul64` to `MULQ` which writes `RDX:RAX` and forces
register juggling. **MULX** writes arbitrary destinations and leaves
flags alone, so a second `MULX` can fire while `ADCX/ADOX` run the
carry chain on the previous product. Saves 3-4 cycles per call; we
call it ~1×/float (vs. Ryu's 3×).

**Projected additional win: 2-4 %.**

### Phase 4 — pow10 table layout (~2 h, ~50 LOC)

Cheap tweaks: align the 10.4 KB table to 64 B (one cache line per
entry), and if canada's exponent distribution is narrow (expect most
exponents in [-20, +20] since values are in [-180, 180]), put that
hot range first so the hot line prefetch picks it up.

**Projected win: <1 %, but almost free.**

## Total projection

| phase | LOC | effort | cumulative canada-encode Δ |
|---|---|---|---|
| 0: de-risk | 50 | 30 min | — (measurement only) |
| 1: pure-Go Schubfach | 350 | 1 day | +12.6 % → +4 to +7 % |
| 2: digit emission asm | 200 avo | 1 day | → 0 to +3 % |
| 3: core asm w/ MULX+ADX | 250 avo | 2 days | → −2 to −3 % |
| 4: table layout | 50 | 2 h | → −3 to −4 % |

Total: **~850 LOC** (400 Go / 450 avo), **~4.5 days**. Ends with
canada encode ≈ tied or slightly ahead of sonic. Combined with the
existing lead elsewhere, the library hits the ≥ 10 % bar on all 9
benchmarks only if phase 3 or 4 pushes the gap to ≥ 10 % — realistic
but not guaranteed without extra tuning.

If tie-but-not-10 % is the outcome, that's still a cleanly winning
library; the asm work is primarily about closing the last gap, not
widening already-won leads.

## Correctness strategy (3 layers)

1. **Exhaustive float32** (~10 min on one core). Every `uint32` bit
   pattern → float32 → widen to float64 → format with our code AND
   `strconv.AppendFloat` → assert identical bytes. Catches ~99 % of
   bugs.
2. **Differential fuzz** via `testing/fuzz` with a seed corpus that
   **explicitly** includes: subnormals, `math.SmallestNonzeroFloat64`,
   `math.MaxFloat64`, `1 − 2^−53`, all `10^k` for k ∈ [−20, 20],
   `0.5^k` for k ∈ [1, 53], exact halves (ties-to-even), and all
   floats extracted from canada.json.
3. **Round-trip**: `ParseFloat(AppendFloat(x)) == x` for every finite
   float64 in the fuzz corpus.

Phase 1 lands the oracle harness; phases 2-3 reuse it.

## Licensing

- **Schubfach (Bolz)**: BSL-1.0 — same as Boost. Permissive, non-viral.
  Attribution: keep the 2020 Bolz copyright comment verbatim at the
  top of `ryu_schubfach.go` (sonic does this).
- **Go stdlib table** (`detailedPowersOfTen`): BSD-3-Clause. Attribution
  already present in `eisel_lemire.go` from E19; no new obligations.
- jsonx's own license remains whatever we pick (MIT/Apache/BSD all
  compatible with both of the above).

## Top risks

1. **Edge cases in round-odd / exact-bits logic.** Schubfach has ~5
   special-case branches (`mant == 0`, exact-integer shortcut,
   `e2 == 0`, subnormals, exact-halfway round-to-even). These are
   invisible in random fuzz — seed the corpus explicitly.
2. **ABI friction on the asm.** avo's default `Package().Function()` is
   ABI0, which forces memory round-trips at the Go↔asm boundary and
   can **eat the entire MULX win**. Must emit `ABIInternal` stubs
   (Go 1.17+ register ABI). Micro-benchmark the asm callsite before
   and after phase 3 to catch this.
3. **Benchmark attribution lying.** "71 % in `genericFtoa`" includes
   the write-to-buffer and caller bounds checks. Phase 0 confirms how
   much of the 12.6 % is actually the formatter. If <8 %, phases 2-3
   may not be necessary.

## Key reference paths

- `/usr/local/go/src/strconv/ftoaryu.go` — stdlib Ryu (oracle + table format)
- `~/go/pkg/mod/github.com/bytedance/sonic@v1.15.0/native/f64toa.c` —
  Schubfach port to study (BSL-1.0, the primary source for phase 1)
- `~/go/pkg/mod/github.com/bytedance/sonic@v1.15.0/internal/native/avx2/f64toa_text_amd64.go` —
  sonic's generated Go-asm output; read-only reference for "what does
  the final asm look like", **not** a source to copy (different ABI)
- `eisel_lemire.go` — existing `detailedPowersOfTen` table, reusable
- `float_fast.go` — integration point on decode side (already uses Eisel-Lemire)
- `encode.go:writeFloat` — integration point on encode side
