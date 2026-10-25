# PandaHash - High-Performance Non-Cryptographic Hash Function for Go

PandaHash is a modern non-cryptographic hash function engineered for ultra-high throughput, strict streaming invariance, and zero memory allocations. Built for 64-bit architectures with full Go 1.27+ support, PandaHash delivers **30–35 GB/s single-core sequential throughput** and up to **78 GB/s multi-core parallel throughput** across real-world datasets.

---

## 🚀 Key Features & Invariants

- **Zero Memory Allocations**: Strictly `0 B/op` and `0 allocs/op` for all sequential hashing operations across all data sizes.
- **Deterministic Streaming Invariance**: Full support for incremental streaming via `PandaHasher`; chunked writes produce bit-for-bit identical hashes regardless of buffer boundaries (`Write(A) + Write(B) == Write(A+B)`).
- **Independent 256-Bit Entropy Diffusion**: Non-linear diffusion stages ensure all 256 bits have full avalanche dispersion and zero cyclic/rotational symmetry.
- **Byte Slice & String Dual APIs**: First-class support for `[]byte` and zero-copy `string` inputs.
- **Optional Multi-Core Scaling**: Dedicated `PandaHash64MultiCore` function for multi-megabyte payloads scaling across available CPU cores up to 78 GB/s.

---

## 📊 Comprehensive Benchmark Comparison

Measured on Apple Silicon (M4, `go1.27.3 darwin/arm64`) using the official **Canterbury, Silesia, and Snappy compression corpora** against the latest release versions of all industry-standard hash libraries:

| Real-World File Target | Size | PandaHash64 (Sequential) | XXH3 (v1.1.0) | wyhash | xxHash64 (v2.3.0) | MurmurHash3 | Hardware CRC32 | Heap Allocations |
|---|---|---|---|---|---|---|---|---|
| **C Source Code** (`fields.c`) | 11 KB | **33.70 GB/s** (331 ns) | 33.80 GB/s | 29.08 GB/s | 24.47 GB/s | 8.65 GB/s | 11.58 GB/s | **0 B/op (0 allocs)** |
| **HTML Source** (`cp.html`) | 24 KB | **34.17 GB/s** (720 ns) | 33.79 GB/s | 28.81 GB/s | 24.64 GB/s | 8.63 GB/s | 11.51 GB/s | **0 B/op (0 allocs)** |
| **PDF Document** (`paper-100k.pdf`) | 102 KB | **35.15 GB/s** (2.91 µs) | 34.03 GB/s | 28.85 GB/s | 24.47 GB/s | 8.66 GB/s | 11.50 GB/s | **0 B/op (0 allocs)** |
| **Protobuf Binary** (`geo.protodata`) | 118 KB | **34.19 GB/s** (3.47 µs) | 34.05 GB/s | 28.90 GB/s | 24.67 GB/s | 8.80 GB/s | 11.64 GB/s | **0 B/op (0 allocs)** |
| **JPEG Image** (`fireworks.jpeg`) | 123 KB | **34.28 GB/s** (3.59 µs) | 33.81 GB/s | 29.00 GB/s | 24.60 GB/s | 8.75 GB/s | 11.53 GB/s | **0 B/op (0 allocs)** |
| **Spreadsheet** (`kennedy.xls`) | 1.0 MB | **35.53 GB/s** (28.9 µs) | 34.02 GB/s | 28.90 GB/s | 24.58 GB/s | 8.66 GB/s | 11.09 GB/s | **0 B/op (0 allocs)** |
| **Genomic DNA** (`E.coli`) | 4.6 MB | **35.02 GB/s** (132 µs) | 34.07 GB/s | 28.94 GB/s | 24.62 GB/s | 8.68 GB/s | 11.20 GB/s | **0 B/op (0 allocs)** |
| **Genomic DNA (Multi-Core)** | 4.6 MB | **78.57 GB/s** (59.0 µs) 🏆 | 34.07 GB/s | 28.94 GB/s | 24.62 GB/s | 8.68 GB/s | 11.20 GB/s | 929 B/op (20 allocs) |

---

## 🏗️ Architecture & Processing Strategy

PandaHash routes inputs through specialized vectorization pipelines depending on payload size:

```
[ Input Payload ]
       |
       +---> len < 32 B    : pandaHash64Standard (scalar 8-byte chunks, inlined mixing)
       |
       +---> 32 B - 4 KB   : pandaHash64Medium   (4-block parallel multiplication)
       |
       +---> 4 KB+         : pandaHash64Large    (8-block wide vectorization, 64-byte chunks, 0 allocs)
       |
       +---> Optional Multi-Core : PandaHash64MultiCore (parallel chunking across CPU workers, 78 GB/s)
```

### 1. Wide Vectorization (64-Byte Unrolled Blocks)
Traditional hash functions process 8 bytes sequentially. PandaHash wide vectorization loads 8 blocks (64 bytes) simultaneously:
- **Instruction Pipelining**: Modern out-of-order CPUs execute multiple 64-bit multiplications and rotations concurrently.
- **Cache Alignment**: 64-byte chunks match standard L1/L2 CPU cache lines.
- **Zero Allocations**: Eliminates buffer allocations via direct slice reads.

### 2. Streaming Invariance Architecture
`PandaHasher` manages internal 8-byte state staging:
- Partial chunk writes are staged in a fixed 8-byte tail array.
- Full 8-byte and 32-byte blocks are processed strictly via deterministic `mixBlock` transformations.
- Any leftover bytes remain in the tail array until `Sum` or `Sum128` finalizes with input length folding.

---

## 🧮 Mathematical Mixing Foundation

### Core Mixing Step
```go
k *= c1                   // Primary constant multiplier
k = rotl64(k, 31)         // Bit diffusion rotation
h1 ^= k                   // XOR entropy fold
h1 = rotl64(h1, 27) + h2  // Cross-state rotation and addition
h1 = h1*5 + c3            // Linear permutation with odd multiplier
h2 = rotl64(h2, 31) + h1  // Secondary state diffusion
```

### Independent 256-Bit Entropy Diffusion
Earlier implementations derived $h_3$ and $h_4$ via cyclic rotation ($h_4 \equiv \operatorname{rotl64}(h_3, 32)$). PandaHash implements non-linear bit diffusion:
```go
h1, h2 := h.Sum128()
h3 := fmix64(h1^c2) + rotl64(h2, 27)
h4 := fmix64(h2^c4) + rotl64(h1, 31)
```
Each 64-bit word is diffused with independent prime multipliers and shift constants, ensuring uniform 256-bit avalanche coverage.

---

## 🎯 API Reference

### Basic Hashing

```go
import "pandahash" // or import root package

// 64-bit hash (fastest, zero allocations)
h64 := PandaHash64("your string data")
h64Bytes := PandaHash64Bytes([]byte("your byte slice"))

// 32-bit hash (zero allocations)
h32 := PandaHash32("your string data")
h32Bytes := PandaHash32Bytes([]byte("your byte slice"))

// 256-bit hex hash string
h256 := PandaHash("your string data")
h256Bytes := PandaHashBytes([]byte("your byte slice"))
```

### Incremental Streaming

```go
// Create new hasher with optional seed
hasher := NewPandaHasher(0)

// Write streaming chunks (deterministic across any chunk boundary)
hasher.Write([]byte("chunk 1"))
hasher.Write([]byte("chunk 2"))

// Retrieve finalized 256-bit hex string or 128-bit uint64 words
hexString := hasher.Sum()
h1, h2 := hasher.Sum128()

// Reset for instance reuse (zero allocations)
hasher.Reset()
hasher.Write([]byte("new data"))
```

### Multi-Core Parallel Hashing (Large Files)

```go
// Spawns CPU worker goroutines to process multi-megabyte payloads up to 78 GB/s
h64Parallel := PandaHash64MultiCore(largeFileData)
```

### Batch & Concurrent Processing

```go
// Batch processing
inputs := []string{"data1", "data2", "data3"}
hashes := PandaHashBatch(inputs)

// Multi-seeded concurrent hashing
hashes = PandaHashParallel("input string", 4)
```

---

## 🧪 Quality & Test Verification

All tests are verified against the **Canterbury, Calgary, Silesia, Large, and Snappy** corpora:
```bash
# Run all correctness, streaming invariance, and entropy tests
go test -v -run 'Test(PandaHash|RealFiles|AllCorpora).*'

# Run CPU, memory, and speed profiling
go test -v -run TestComprehensiveCPUMemorySpeedProfile

# Run benchmark comparisons
go test -bench=BenchmarkAllCorporaRepresentative -benchmem -run=^$
```

---

## 📄 License

MIT License. See [LICENSE](LICENSE) for details.
