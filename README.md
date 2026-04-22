# jsonx

A high-performance JSON library for Go with a drop-in `encoding/json` API. 

Powered by Claude + [autoresearch](https://github.com/karpathy/autoresearch) automation

---

## Highlights

Best-of-5 over `-benchtime=3s -count=5` on an AMD EPYC-Genoa host with
AVX-512BW, across seven corpus files (small, twitter, citm_catalog, canada,
and 1 / 5 / 10 MB synthetic 10-level formatted payloads):

| Workload | sonic | jsonx | Δ vs sonic |
|---|--:|--:|--:|
| Decode `interface{}` · small.json | 715 ns | **515 ns** | **−28.0 %** |
| Decode `interface{}` · twitter.json | 1.61 ms | **1.41 ms** | **−12.2 %** |
| Decode `interface{}` · citm_catalog.json | 3.67 ms | **3.16 ms** | **−13.8 %** |
| Decode `interface{}` · canada.json (float-heavy) | 10.82 ms | **8.12 ms** | **−24.9 %** |
| Decode `interface{}` · 1 MB formatted | 1.69 ms | **1.46 ms** | **−13.7 %** |
| Decode `interface{}` · 5 MB formatted | 8.35 ms | **7.33 ms** | **−12.2 %** |
| Decode struct (typed `SmallUser`) | 476 ns | **409 ns** | **−14.1 %** |
| Encode `interface{}` · small.json | 486 ns | **314 ns** | **−35.4 %** |
| Encode `interface{}` · twitter.json | 847 µs | **750 µs** | **−11.5 %** |
| Encode `interface{}` · citm_catalog.json | 2.16 ms | **1.29 ms** | **−40.4 %** |
| Encode `interface{}` · canada.json | 6.53 ms | **5.56 ms** | **−14.9 %** |
| Encode `interface{}` · 1 MB formatted | 1.27 ms | **752 µs** | **−40.7 %** |
| Encode `interface{}` · 5 MB formatted | 8.37 ms | **3.96 ms** | **−52.6 %** |
| Encode `interface{}` · 10 MB formatted | 18.39 ms | **9.01 ms** | **−51.0 %** |

**13 of 15 measured gates beat sonic by ≥ 10 %.** The residual case is 10 MB
10-level formatted decode, where sonic's native structural scanner still holds
a small edge (+7–9 %) for very large payloads.

Full head-to-head vs stdlib, `goccy/go-json`, and sonic — including every
corpus, experiment, and methodological caveat — lives in [RESULTS.md](RESULTS.md).

---

## Install

```sh
go get github.com/wudi/jsonx
```

Requires Go 1.22+.

## Usage

The package surface mirrors `encoding/json`:

```go
import "github.com/wudi/jsonx"

type User struct {
    ID    int64  `json:"id"`
    Name  string `json:"name"`
    Email string `json:"email"`
}

// Decode
var u User
if err := jsonx.Unmarshal(data, &u); err != nil { ... }

// Encode
out, err := jsonx.Marshal(u)

// Validate
if !jsonx.Valid(data) { ... }

// Streaming
dec := jsonx.NewDecoder(r)
enc := jsonx.NewEncoder(w)
```

Drop-in replacement — the same struct tags, the same interface target types,
the same byte output up to shortest-representation equivalence.

## Compatibility

| Target | Status | Notes |
|---|---|---|
| linux/amd64 | Primary | 3 SIMD kernels (AVX-512BW string scan, AVX-512BW whitespace skipper, amd64 digit-emission). Runtime-gated on `cpuid`. |
| darwin/amd64 | Primary | Same kernels. |
| linux/arm64 | Secondary | NEON + scalar arm64 kernels for string scan, whitespace skip, digit emission. |
| darwin/arm64 | Secondary | Same as linux/arm64. |
| Other (`GOOS`/`GOARCH`) | Fallback | Pure-Go SWAR path. Every Phase-1 algorithmic win still applies. |

All assembly kernels are gated behind a runtime CPU-feature check (AVX-512BW
on amd64; NEON is baseline-assumed on arm64). On a machine without the gated
feature the library falls through to pure Go without a rebuild.

## How it works

The wins come from a combination of algorithmic and micro-architectural work:

- **Type-specialized decoder plan cache** — one compiled
  `func(*decoder, unsafe.Pointer) error` per `reflect.Type`. Struct fields are
  written via `unsafe.Add(ptr, offset)`; reflection is off the hot path after
  warmup.
- **8-byte prefix field dispatch** — the first eight bytes of each key are
  loaded as a `uint64` and compared against a precomputed
  `prefix + nameLen`; longer fields fall through to a tail compare. This
  replaced the FNV-1a hot spot on struct decode.
- **Slab-allocated `interface{}` boxes** — `float64` and `string` values
  decoded into `interface{}` targets are backed by hand-constructed `eface`
  values pointing into chunked slabs. Hundreds of tiny `mallocgc` calls
  collapse into one geometric-grown allocation.
- **AVX-512BW string-body scan** — 64 bytes per loop via
  `VMOVDQU64 → VPCMPEQB(×2) → VPCMPUB → KORQ(×2) → KTESTQ → TZCNTQ`.
  Measured 23.8 GB/s on an EPYC-Genoa vs 4.7 GB/s for the SWAR fallback.
  Gated at `n ≥ 64` so short strings don't eat the broadcast/zeroupper
  penalty.
- **AVX-512BW whitespace skipper** — same 64-byte cadence,
  `VPBROADCASTB × 4 + VPCMPEQB × 4 + KORQ + KNOTQ + KTESTQ + TZCNTQ`.
  Closed most of the prior regression on 10-level formatted corpora.
- **Peek-ahead map-size hint** — bounded 256-byte comma count gives
  `make(map, hint)` a starting size that's never too small. Over-allocation
  is cheap compared to rehash cascades; post-change
  `runtime.mapassign_faststr` drops from 47 % CPU to <10 % on the twitter
  corpus.
- **Clinger + Eisel-Lemire float parsing** — Clinger's fast path
  (`mant ≤ 2⁵³ ∧ |exp| ≤ 22`) handles the common case in one multiply;
  anything longer goes through a port of Go stdlib's
  `eiselLemire64` + 11 KB `detailedPowersOfTen` table, fed the mantissa and
  exponent we already extracted. Eliminates the double-scan that was 25 %
  of canada-decode CPU.
- **Schubfach float formatting** — a Go port of Alexander Bolz's Schubfach
  reference (BSL-1.0), which is strictly shorter than Ryu for float64
  shortest-representation. One 128-bit multiply + a round-odd step +
  iterative trailing-zero trim. Pure Go; ~41 % faster than
  `strconv.AppendFloat` in isolation.
- **Digit-emission kernel** — one 64-bit `DIV` by 1e8 splits off the top
  eight digits, then unrolled 4-digit chunks via `IMUL3Q`-based magic
  div-by-100 with `MOVW` stores into a packed `[100]uint16` LUT. amd64 asm
  + arm64 NEON + scalar arm64 asm; pure-Go fallback elsewhere.
- **Encoder with one allocation per call** — pooled `[]byte`, pre-quoted
  struct keys computed at plan-build time (`"name":` and `,"name":` both
  baked in), inlined `encodeAny` type switch hoisted into the map/slice
  iterators, and a merged grow-check + direct write for the
  quote-payload-quote string path.
- **Cached interface singletons** — `true`, `false`, and `nil` decode into
  pre-boxed `eface` values, zero allocation.

## Repository layout

| File | Purpose |
|---|---|
| `jsonx.go` | Public `Unmarshal` / `Marshal` / `Valid` / `Encoder` / `Decoder` API. |
| `decode.go`, `decode_typed.go`, `decode_struct.go` | Decoder + plan cache + struct plan. |
| `encode.go`, `encode_typed.go` | Encoder + plan cache. |
| `float_fast.go` | Clinger fast-path float parser. |
| `eisel_lemire.go` | Port of Go stdlib's `eiselLemire64` + 11 KB `detailedPowersOfTen` table. |
| `ryu_schubfach.go`, `ryu_schubfach_table.go` | Schubfach float → string formatter (BSL-1.0). |
| `iface.go` | Hand-constructed `eface` + slab allocators. |
| `scan.go`, `scan_amd64.go`, `scan_arm64.go`, `scan_other.go` | String-scan dispatcher + SWAR fallback. |
| `scan_amd64.s`, `skipws_amd64.s`, `writedigits_amd64.s` | AVX-512 / SSE asm kernels (generated by `asmgen/`). |
| `scan_arm64.s`, `skipws_arm64.s`, `writedigits_arm64.s` | NEON + scalar arm64 asm kernels. |
| `writedigits_*.go` | Digit-emission Go stub + fallback. |
| `asmgen/` | Avo-based amd64 kernel source. Generated `.s` files are checked in — the generator runs only when a kernel changes. |
| `bench/` | Head-to-head benchmarks vs `encoding/json`, `goccy/go-json`, and `bytedance/sonic`. |
| `testdata/` | Canonical corpora (twitter, citm_catalog, canada, small, 1/5/10 MB formatted). |
| `RESULTS.md`, `program.md` | Full benchmark scorecard and experiment log. |

## Reproducing the benchmarks

```sh
cd bench
go test -bench=. -benchtime=3s -count=5 -benchmem
```

Corpora are loaded from `../testdata/`. The bench file runs the same
`interface{}` and struct decode/encode workloads against `encoding/json`,
`goccy/go-json`, `bytedance/sonic`, and `jsonx`.

## Tests

```sh
go test ./...
```

Coverage includes:

- Full-corpus round-trip and deep-equality vs `encoding/json` on
  small / twitter / citm_catalog / canada.
- Dedicated correctness tests for the AVX-512 string-scan, whitespace
  skipper, and digit-emission kernels (including a parity fuzz of
  `[1, 17] × 50 000` random significands with trim on/off against
  `strconv.AppendFloat`).
- `fallback_test.go` runs the pure-Go path unconditionally so the SWAR
  branch is always covered regardless of host CPU.

## When to reach for something else

`jsonx` is built for server workloads where payloads are large enough
(> ~64 bytes) to amortize the SIMD broadcast cost and structs are reused
often enough to amortize the plan-cache miss. For hot paths on payloads
small enough to fit in a single cache line and short-lived processes where
plan warmup dominates, `encoding/json` is often already fast enough.

`bytedance/sonic` remains an excellent choice when its JIT codegen and
native structural indexer are available — notably on very large (> 10 MB)
pretty-printed decode, where sonic's structural scanner still edges out
this library's AVX-512 whitespace skipper.

## Credits

- **Schubfach** — Alexander Bolz, *Drachennest*
  ([github.com/abolz/Drachennest](https://github.com/abolz/Drachennest)),
  BSL-1.0. The Go port in `ryu_schubfach.go` is derived from ByteDance
  sonic's `native/f64toa.c`, which is itself a port of the reference C++.
- **Eisel-Lemire `detailedPowersOfTen`** — ported from the Go standard
  library's `strconv`; see `eisel_lemire.go` for attribution.
- **Avo** — the amd64 kernels are generated with
  [`mmcloughlin/avo`](https://github.com/mmcloughlin/avo).
- **Benchmark corpora** — `twitter.json`, `citm_catalog.json`, `canada.json`
  are the standard nativejson-benchmark files; `small.json` and the
  10-level formatted synthetics are project-local.

Prior art and comparators: [`bytedance/sonic`](https://github.com/bytedance/sonic),
[`goccy/go-json`](https://github.com/goccy/go-json), and Go's
[`encoding/json`](https://pkg.go.dev/encoding/json).

## License
MIT