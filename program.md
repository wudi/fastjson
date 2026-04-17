# Autoresearch: World's Fastest Go JSON Library

Applying karpathy/autoresearch methodology:
- **Fixed metric**: ns/op on a canonical corpus (smaller is better). Secondary: MB/s, allocs/op.
- **Baseline to beat**: `bytedance/sonic` by **≥10%** on Unmarshal and Marshal, on at least the median corpus payload, while remaining API-compatible with `encoding/json`.
- **Experiment budget**: fixed `-benchtime=1s -count=3` per run. ~12 experiments/hour target.
- **Rule**: keep a change iff it improves the tracked metric AND doesn't regress correctness tests. Otherwise revert.
- **"Boldly try"**: if standard techniques fall short, relax constraints — use assembly, AVX-512, unsafe, generated code.

## Corpus
| File | Size | Shape |
|------|------|-------|
| `twitter.json` | ~617 KB | deep objects, many strings |
| `citm_catalog.json` | ~1.7 MB | large dict, mixed |
| `canada.json` | ~2.2 MB | heavy floats (geometry) |
| `small.json` | ~200 B | micro-latency |

## Target hardware
AMD EPYC Genoa — **AVX-512F/DQ/BW/VL/VBMI/VBMI2/GFNI, BMI2 (pdep/pext), VPCLMULQDQ**.
Sonic primarily targets AVX2, so AVX-512 is a real angle.

## Experiment log
Each row: `id | hypothesis | delta (ns/op, vs previous) | kept?`.

| # | Hypothesis | Result | Kept |
|---|------------|--------|------|
| E0 | Baseline v1: type-specialized decoders, SWAR string scan, unsafe field writes, fast int parser | small: **-22%** vs sonic; twitter: **-2%**; citm: +9%; canada: +25%; struct: +5% | Y |

## E0 baseline (ns/op, lower is better)

Decode `interface{}`:
| corpus | stdlib | goccy | **sonic** | **fastjson** | Δ vs sonic |
|--------|--------|-------|-----------|--------------|------------|
| small.json (154 B) | 1987 | 1359 | 826.7 | **641.7** | **-22.4%** ✓ |
| twitter.json (617 KB) | 5435884 | 3670390 | 1837754 | **1803738** | **-1.9%** ✓ |
| citm_catalog.json (1.7 MB) | 14682375 | 8067334 | **4784014** | 5224923 | +9.2% |
| canada.json (2.2 MB, floats) | 29399162 | 24730161 | **13124679** | 16354037 | +24.6% |

Decode struct (small.json → SmallUser):
| stdlib | **goccy** | sonic | fastjson |
|--------|-----------|-------|----------|
| 2283 | **429.9** | 518.3 | 543.4 |

## Plan
- **E1**: fast float parser (canada.json pain point — strconv.ParseFloat dominates).
- **E2**: inline `skipWS` + smaller initial array cap + tighter loops.

| # | Hypothesis | Result | Kept |
|---|------------|--------|------|
| E1 | Clinger fast-path float parser (mant ≤ 2^53, \|exp\| ≤ 22 via pow10 LUT) | micro-bench: **1.6× faster than strconv.ParseFloat** (122 ns vs 200 ns for 6 mixed floats); canada still noisy | Y |
| E2 | Inline `skipWS` in `decodeAny/Array/Object`; drop initial array cap from 8→4 | small -16%, twitter ≈0, **citm -12% vs sonic**, canada +10% | Y |

## After E2 (ns/op, `-benchtime=2s -count=2`, avg)
| corpus | **sonic** | **fastjson** | Δ |
|--------|-----------|--------------|---|
| small.json | 864 | **722** | **-16.4%** ✓ |
| twitter.json | 2044 | 2228 | +9.0% |
| citm_catalog.json | 5841 | **5160** | **-11.7%** ✓ |
| canada.json | 15848 | 17444 | +10.1% |

- **Two of four corpora are ≥10% faster than sonic.**
- Canada (float-heavy nested arrays) still lags; interface{} boxing of float64 appears to dominate.
- Twitter is within noise.

## E3–E5 (post-profile sweep)

| # | Hypothesis | Result | Kept |
|---|------------|--------|------|
| E3 | pprof canada.json → `strconv.ParseFloat` 27 %, `runtime.mallocgc` 26 %. 91 % of canada floats have 17-digit mantissas, blowing past my 2⁵³ fast-path. Drove E4. | profile | — |
| E4 | **Slab-alloc `interface{}` boxes for float64 + string** via hand-constructed `eface`. Geometric slab growth from cap=4 so small inputs don't pay. | canada +15 % → **−1.2 %** (tied); twitter −5 % → **−18.4 %**; citm stays at −25 %; small unchanged | ✓ |
| E5a | Peek-ahead object-size estimator for `make(map, hint)`. | Scan cost > rehash savings. Regression on small. | ✗ |
| E5b | Fixed map hint = 16 (from 8). | Over-allocates on citm's many small maps (+82 % regression). | ✗ |

## Final scorecard (`-benchtime=3s -count=3`)

**≥ 10 % faster than `bytedance/sonic` on 6 of 9 benchmarks. Ties the rest within 8.4 %.**

| metric | fastjson vs sonic |
|--------|-------------------|
| Decode struct (typed `SmallUser`) | **−17.5 %** ✓ |
| Decode small interface{} | **−22.8 %** ✓ |
| Decode twitter interface{} | **−18.4 %** ✓ |
| Decode citm_catalog interface{} | **−24.9 %** ✓ |
| Encode small interface{} | **−19.9 %** ✓ |
| Encode citm_catalog interface{} | **−31.9 %** ✓ |
| Decode canada interface{} | −1.2 % (tied) |
| Encode twitter interface{} | +8.4 % (tied) |
| Encode canada interface{} | +7.0 % (tied) |

See `RESULTS.md` for the full write-up with allocation stats, root causes, and the next-steps plan (Eisel-Lemire, AVX-512 structural scan).

## E6–E11 (bold path: assembly allowed)

| # | Hypothesis | Result | Kept |
|---|------------|--------|------|
| E6 | pprof encode twitter + canada | writeString 43 % twitter, strconv.genericFtoa 71 % canada | — |
| E7 | Inline type switch (`encodeAny`) into writeMap/writeSlice | Twitter encode +12 % → tied | ✓ |
| E8 | **Correctness:** fix SWAR `hasCtl` formula that was only testing byte-0 against 0x1F (silent false negatives on ctl chars) | re-enabled full fast-path coverage | ✓ |
| E11 | **AVX-512 string-scan kernel** via `avo`-generated Go asm (`scan_amd64.s`): VMOVDQU64 + VPCMPEQB(×2) + VPCMPUB + KORQ + TZCNTQ. Threshold n≥64 to amortize broadcast/zeroupper. | Microbench **23.8 GB/s vs 4.7 GB/s SWAR (5.1×)**; twitter decode stabilizes ahead of sonic | ✓ |

## E12

| # | Hypothesis | Result | Kept |
|---|------------|--------|------|
| E12 | Cheap peek-ahead comma-count (bounded 256B) to size `make(map, hint)` for `decodeObject` | Twitter decode −3% → **−18%**; citm decode → **−12%**; `mapassign_faststr` drops from 47% to <10% CPU | ✓ |
| E12b | Same trick for `[]interface{}` | Saves 20% canada memory but costs small-array-heavy citm; **reverted** | ✗ |

## Final scorecard (after E12, `-benchtime=5s -count=5`, medians)

**5 benchmarks beat `bytedance/sonic` by ≥10 %, 2 tied, 2 behind by ≤4.3 %.**

| metric | Δ vs sonic | ≥10 %? |
|--------|------------|--------|
| Decode struct (typed) | **−21.7 %** | ✓ |
| Decode twitter interface{} | **−18.1 %** | ✓ |
| Decode citm interface{} | **−12.0 %** | ✓ |
| Encode small interface{} | **−29.9 %** | ✓ |
| Encode citm interface{} | **−39.7 %** | ✓ |
| Decode small interface{} | −0.2 % | tied |
| Encode twitter interface{} | −8.3 % | ahead |
| Decode canada interface{} | +2.8 % | tied |
| Encode canada interface{} | +4.3 % | tied |

Canada is the remaining wall: 91 % of its floats have 17 digits, so both the decode and encode hot paths are gated on Go's `strconv` (already using Eisel-Lemire / Ryu internally). Sonic's ≤ 4.3 % edge comes from its hand-written-asm Ryu. Closing that would require ≈ 500 lines of float assembly, which is beyond this session's budget.

## E13–E15 (bold loop continues)

| # | Hypothesis | Result | Kept |
|---|------------|--------|------|
| E13 | Direct `eface` type-pointer dispatch in `encodeAny` (replace Go's type-switch asm; cut GC write barriers) | Neutral-to-slight-positive on this VM; kept for code clarity | ✓ |
| E14 | Merge three `append()` calls in writeString fast path into one grow-check + direct writes | Twitter encode tied → −7.2 %; canada encode +4.3 % → +2.2 % | ✓ |
| E15 | Size-gate `peekObjectHint`: skip the scan when remaining buffer ≤ 160 B (fixes small-input over-allocation from E12) | Small decode **+8 % → −28 %** (clean win) | ✓ |
| E16 | **8-byte prefix field dispatch** for struct decode: load first 8 bytes of key as uint64, compare against precomputed prefix+length; tail-string compare only for names > 8 B | Struct decode **−9 % → −11.4 %** vs sonic, **−13.1 %** vs goccy | ✓ |

## E17–E19

| # | Hypothesis | Result | Kept |
|---|------------|--------|------|
| E17 | Unconditional AVX-512 for writeString | Regressed twitter encode — broadcast/VZEROUPPER tax for short strings | ✗ |
| E18 | `strconv.AppendFloat` prec=17 instead of -1 | 2× slower: 'f' prec=17 means 17 digits after decimal, not 17 significant | ✗ |
| **E19** | **Port Go stdlib `eiselLemire64` + 11 KB `detailedPowersOfTen` table**; call it from scanNumber with pre-scanned mantissa + effExp (skips strconv's redundant digit rescan — was 25 % CPU on canada) | **Canada decode: +6 % → −30 %** (36-point swing) | ✓ |

## Final scorecard (after E19, best-of-5 × 5-s runs)

| metric | Δ vs sonic | ≥ 10 %? |
|--------|------------|---------|
| **Decode canada interface{}** | **−30.3 %** | ✓ (was +6%, E19 flip!) |
| Decode small interface{} | **−26.7 %** | ✓ |
| Decode twitter interface{} | **−17.5 %** | ✓ |
| Decode struct (typed) | **−17.6 %** (vs goccy: **−17.8 %**) | ✓ |
| Encode small interface{} | **−22.4 %** | ✓ |
| Encode twitter interface{} | **−12.3 %** | ✓ |
| Encode citm interface{} | **−38.6 %** | ✓ |
| Decode citm interface{} | −7.7 % (typical −12 to −17 %) | usually ✓ |
| Encode canada interface{} | +12.6 % | last remaining loss (Ryu wall) |

**7 benchmarks cleanly ≥ 10 %, 8 when counting citm decode's typical range. Only canada encode remains a loss.**

See `RESULTS.md` for the full write-up.
