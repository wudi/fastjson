# jsonx

A high-performance JSON library for Go with a drop-in `encoding/json` API. 

> Powered by AI + [autoresearch](https://github.com/karpathy/autoresearch) automation. I don't know how the fuck it works, but it’s fast as hell.
---

## Highlights

### Decode (amd64 AMD EPYC-Genoa, AVX-512BW, `-benchtime=5s -count=2`)

| Workload | sonic | jsonx | Δ vs sonic |
|---|--:|--:|--:|
| Decode `interface{}` · small.json | 218 MB/s | **262 MB/s** | **+20.2 %** |
| Decode `interface{}` · twitter.json | 381 MB/s | **477 MB/s** | **+25.2 %** |
| Decode `interface{}` · citm_catalog.json | 403 MB/s | **618 MB/s** | **+53.4 %** |
| Decode `interface{}` · canada.json (float-heavy) | 198 MB/s | **322 MB/s** | **+62.6 %** |
| Decode `interface{}` · 1 MB formatted | 581 MB/s | **731 MB/s** | **+25.8 %** |
| Decode `interface{}` · 5 MB formatted | 506 MB/s | **667 MB/s** | **+31.8 %** |
| Decode `interface{}` · 10 MB formatted | 598 MB/s | **660 MB/s** | **+10.4 %** |
| Decode struct (typed `SmallUser`) | 344 MB/s | **516 MB/s** | **+50.0 %** |
| Decode struct · 1 MB formatted | 797 MB/s | **1499 MB/s** | **+88.1 %** |
| Decode struct · 5 MB formatted | 767 MB/s | **1372 MB/s** | **+78.9 %** |
| Decode struct · 10 MB formatted | 787 MB/s | **1348 MB/s** | **+71.3 %** |

**All 11 decode gates beat sonic by ≥ 10 %.** Struct gates cluster at
+50–88 %, interface{} gates at +10–63 %. (Encode figures are in
[RESULTS.md](RESULTS.md); unchanged from the prior commit series.)
Memory footprint is a fraction of sonic's — the 10 MB interface{} bench
allocates 23.7 MB vs sonic's 38.7 MB; the 10 MB struct bench allocates
9.1 MB vs sonic's 14.8 MB.

### ARM64 (Ampere Altra / Neoverse-N1)

Typed struct decoding on the same 1/5/10 MB 10-level formatted corpus,
`-benchtime=5s -count=2`:

| Workload | sonic | jsonx | Δ vs sonic |
|---|--:|--:|--:|
| Decode struct · 1 MB formatted | 626 MB/s | **698 MB/s** | **+11.5 %** |
| Decode struct · 5 MB formatted | 601 MB/s | **688 MB/s** | **+14.5 %** |
| Decode struct · 10 MB formatted | 594 MB/s | **675 MB/s** | **+13.6 %** |

jsonx on this target uses about 11 % of sonic's resident memory (9 MB
vs 83 MB for the 10 MB input) and half the allocations (22 K vs 45 K).

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
| linux/arm64 | Primary | NEON + scalar arm64 kernels for string scan, whitespace skip, digit emission. Whitespace skipper compiles to a single `CMHI` compare per 16-byte chunk — lets struct decode beat sonic by 11–14 % on the deeply-formatted corpora. |
| darwin/arm64 | Primary | Same as linux/arm64. |
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
- **AVX-512BW whitespace skipper** — single `VPCMPUB $6, Z0, Z1, K1`
  ("byte > 0x20") per 64-byte chunk. In JSON structural position every
  byte ≤ 0x20 is either whitespace or a malformed byte the next token
  parse will reject, so the 4× VPCMPEQB + KORQ-tree of the strict kernel
  collapses to one comparator. Same trick applied to the arm64 NEON
  kernel via a single `CMHI`.
- **`[]interface{}` header slab (`sliceIfaceSlab`)** — `decodeAny`
  returning a slice used to box the 24-byte header into the 8-byte
  iface data word with a mallocgc per array (measured 5.9 M flat
  allocs on the 10 MB interface{} profile). Pooling the headers
  through a slab identical in shape to `floatSlab`/`stringSlab` drops
  it to one chunked allocation per ~256 arrays.
- **Position-pinning SWAR string scan** — `decodeString` /
  `decodeStringRaw` compute a combined `"`/`\\`/ctl mask over an 8-byte
  word and resolve the exact byte offset via `bits.TrailingZeros64`.
  Eliminates the per-byte scalar retry loop we used to fall into after
  the SWAR said "something's in this word" — cut `scanStringSIMD`
  self-time from 3.32 s to 0.14 s on the 10 MB struct bench.
- **Adjacency fast-paths in `decodeObject`/`decodeArray`** — compact
  JSON (and most struct values) places `:`, `,`, or `}` / `]`
  immediately after the preceding token. Inline checks for those bytes
  before calling `skipWSFast` skip the function-call dispatch in the
  common case.
- **ARM64 NEON whitespace skipper** — a single `CMHI` compare against `0x20`
  per 16-byte load. In JSON structural positions every byte ≤ 0x20 is either
  whitespace (space / tab / LF / CR) or malformed input that the next token
  parse rejects, so the four-way VCMEQ + OR-tree of the textbook kernel
  collapses to one comparator. Took the skipWS self-time from 27 % of
  arm64 decode CPU down into the single digits.
- **SWAR prefix on string body scan** — two 8-byte probes of the
  `hasQuoteOrBackslashOrCtl` trick before dispatching `scanStringSIMD`. Most
  struct keys and many string values fit in 16 bytes and skip the SIMD-kernel
  call entirely — `scanStringSIMD` self-time drops from ~20 % to ~1 % of
  arm64 decode CPU.
- **Direct `mallocgc` for slice growth** — `growSlice` reaches
  `reflect.unsafe_NewArray` via `go:linkname` and caches the element `*rtype`
  at plan-build time, skipping the `reflect.SliceOf` sync.Map lookup and the
  `reflect.Value` wrapping that `reflect.MakeSlice` performs per grow.
- **Tight struct decode loop** — fold the post-`{` skipWS into the loop
  head, fast-path `:` / `,` / `}` when no whitespace sits between the value
  and the next structural char, and a 1-byte-space inline in `skipWSFast`
  for the common `": "<value>` separator. Cuts the skipWS call count per
  struct field from four to roughly one and a half.
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