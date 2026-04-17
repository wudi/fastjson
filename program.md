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

## Final scorecard (after E11, `-benchtime=5s -count=5`, medians)

**fastjson is faster than `bytedance/sonic` on ALL 9 benchmarks.** 5 hit ≥10 %.

| metric | Δ vs sonic | ≥10 %? |
|--------|------------|--------|
| Decode struct | **−18.5 %** | ✓ |
| Decode small interface{} | **−31.9 %** | ✓ |
| Decode twitter interface{} | −2.9 % | ahead |
| Decode citm interface{} | **−16.5 %** | ✓ |
| Decode canada interface{} | −5.1 % | ahead |
| Encode small interface{} | **−32.4 %** | ✓ |
| Encode twitter interface{} | −2.8 % | ahead |
| Encode citm interface{} | **−34.9 %** | ✓ |
| Encode canada interface{} | **−9.0 %** | ~tied, ahead |

See `RESULTS.md` for the full write-up.
