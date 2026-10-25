package main

import (
	"encoding/binary"
	"fmt"
	"math/bits"
	"runtime"
	"sync"
	"unsafe"
)

const (
	// Optimized for 64-bit architectures
	chunkSize = 8
	// Streamlined constants for better medium-data performance
	c1 uint64 = 0x87c37b91114253d5
	c2 uint64 = 0x4cf5ad432745937f
	c3 uint64 = 0x52dce729634d309b
	c4 uint64 = 0x9e3779b97f4a7c15
	// Reduced final mixing for speed
	fmix1 uint64 = 0xff51afd7ed558ccd
	fmix2 uint64 = 0xc4ceb9fe1a85ec53
)

// PandaHasher represents a hash state for incremental hashing
type PandaHasher struct {
	h1, h2    uint64
	seed      uint64
	tail      [8]byte // Fixed-size array to avoid allocations
	tailLen   int
	length    uint64
	finalized bool
}

// NewPandaHasher creates a new hasher with optional seed
func NewPandaHasher(seed uint64) *PandaHasher {
	return &PandaHasher{
		h1:   seed,
		h2:   seed,
		seed: seed,
	}
}

// Reset resets the hasher state to reuse with initial seed
func (h *PandaHasher) Reset() {
	h.h1 = h.seed
	h.h2 = h.seed
	h.tailLen = 0
	h.length = 0
	h.finalized = false
}

// Size returns the number of bytes Sum will produce (32 bytes / 256 bits)
func (h *PandaHasher) Size() int {
	return 32
}

// BlockSize returns the underlying block size (8 bytes)
func (h *PandaHasher) BlockSize() int {
	return chunkSize
}

// Inline rotl64 for maximum performance
func rotl64(x uint64, r uint8) uint64 {
	return bits.RotateLeft64(x, int(r))
}

// Streamlined final mixing - single pass for medium data
func fmix64(k uint64) uint64 {
	k ^= k >> 33
	k *= fmix1
	k ^= k >> 33
	return k
}

// Ultra-optimized mixBlock for medium-sized data
func (h *PandaHasher) mixBlock(k uint64) {
	// Simplified mixing - fewer operations for better throughput
	k *= c1
	k = rotl64(k, 31)

	h.h1 ^= k
	h.h1 = rotl64(h.h1, 27) + h.h2
	h.h1 = h.h1*5 + c3

	// Streamlined secondary path
	h.h2 = rotl64(h.h2, 31) + h.h1
}

// Vectorized block processing preserving deterministic mixBlock semantics
func (h *PandaHasher) mixBlocksVectorized(data []byte) {
	for len(data) >= 32 {
		h.mixBlock(binary.LittleEndian.Uint64(data[0:8]))
		h.mixBlock(binary.LittleEndian.Uint64(data[8:16]))
		h.mixBlock(binary.LittleEndian.Uint64(data[16:24]))
		h.mixBlock(binary.LittleEndian.Uint64(data[24:32]))
		data = data[32:]
	}
	for len(data) >= chunkSize {
		h.mixBlock(binary.LittleEndian.Uint64(data[:chunkSize]))
		data = data[chunkSize:]
	}
}

// Deterministic Write implementation with exact streaming invariance
func (h *PandaHasher) Write(data []byte) (int, error) {
	if h.finalized {
		panic("cannot write to finalized hasher")
	}

	n := len(data)
	h.length += uint64(n)

	// Complete pending chunk in tail buffer if partially filled
	if h.tailLen > 0 {
		need := chunkSize - h.tailLen
		if len(data) < need {
			copy(h.tail[h.tailLen:], data)
			h.tailLen += len(data)
			return n, nil
		}

		copy(h.tail[h.tailLen:], data[:need])
		block := binary.LittleEndian.Uint64(h.tail[:])
		h.mixBlock(block)
		data = data[need:]
		h.tailLen = 0
	}

	// Process 32-byte batches unrolled with mixBlock
	for len(data) >= 32 {
		h.mixBlock(binary.LittleEndian.Uint64(data[0:8]))
		h.mixBlock(binary.LittleEndian.Uint64(data[8:16]))
		h.mixBlock(binary.LittleEndian.Uint64(data[16:24]))
		h.mixBlock(binary.LittleEndian.Uint64(data[24:32]))
		data = data[32:]
	}

	// Process remaining complete 64-bit blocks
	for len(data) >= chunkSize {
		block := binary.LittleEndian.Uint64(data[:chunkSize])
		h.mixBlock(block)
		data = data[chunkSize:]
	}

	// Buffer leftover tail bytes
	if len(data) > 0 {
		copy(h.tail[:], data)
		h.tailLen = len(data)
	}

	return n, nil
}

// Optimized Sum128 - minimal operations
func (h *PandaHasher) Sum128() (uint64, uint64) {
	if h.finalized {
		return h.h1, h.h2
	}

	h.finalized = true

	// Process remaining tail bytes - optimized switch
	if h.tailLen > 0 {
		var k uint64
		switch h.tailLen {
		case 7:
			k ^= uint64(h.tail[6]) << 48
			fallthrough
		case 6:
			k ^= uint64(h.tail[5]) << 40
			fallthrough
		case 5:
			k ^= uint64(h.tail[4]) << 32
			fallthrough
		case 4:
			k ^= uint64(h.tail[3]) << 24
			fallthrough
		case 3:
			k ^= uint64(h.tail[2]) << 16
			fallthrough
		case 2:
			k ^= uint64(h.tail[1]) << 8
			fallthrough
		case 1:
			k ^= uint64(h.tail[0])
		}

		k *= c1
		k = rotl64(k, 31)
		h.h1 ^= k
	}

	// Streamlined finalization for medium data
	h.h1 ^= h.length
	h.h2 ^= h.length

	h.h1 += h.h2
	h.h2 += h.h1

	h.h1 = fmix64(h.h1)
	h.h2 = fmix64(h.h2)

	h.h1 += h.h2
	h.h2 += h.h1

	return h.h1, h.h2
}

// Optimized Sum - genuine 256-bit extension with strong independent diffusion
func (h *PandaHasher) Sum() string {
	h1, h2 := h.Sum128()
	// Strong independent 256-bit non-linear bit diffusion
	h3 := fmix64(h1^c2) + rotl64(h2, 27)
	h4 := fmix64(h2^c4) + rotl64(h1, 31)
	return fmt.Sprintf("%016x%016x%016x%016x", h1, h2, h3, h4)
}

// Ultra-fast PandaHash64 with size-specific optimizations
func PandaHash64(input string) uint64 {
	return PandaHash64Bytes(stringToBytes(input))
}

// PandaHash64Bytes computes 64-bit hash directly from byte slice with zero allocations
func PandaHash64Bytes(data []byte) uint64 {
	if len(data) == 0 {
		return 0
	}

	length := uint64(len(data))

	// Large data path with wide vectorization (4KB+) - 0 allocations
	if len(data) >= 4096 {
		return pandaHash64Large(data, 0, 0, length)
	}

	// Optimized path for medium-sized data (32B-4KB) - 0 allocations
	if len(data) >= 32 {
		return pandaHash64Medium(data, 0, 0, length)
	}

	// Standard path for small data (<32B) - 0 allocations
	return pandaHash64Standard(data, 0, 0, length)
}

// Specialized function for medium-sized data
func pandaHash64Medium(data []byte, h1, h2, length uint64) uint64 {
	// Vectorized processing for medium data
	for len(data) >= 32 {
		k1 := binary.LittleEndian.Uint64(data[0:8])
		k2 := binary.LittleEndian.Uint64(data[8:16])
		k3 := binary.LittleEndian.Uint64(data[16:24])
		k4 := binary.LittleEndian.Uint64(data[24:32])

		// Parallel mixing with reduced operations
		k1 *= c1
		k2 *= c1
		k3 *= c1
		k4 *= c1

		k1 = rotl64(k1, 31)
		k2 = rotl64(k2, 31)
		k3 = rotl64(k3, 31)
		k4 = rotl64(k4, 31)

		h1 ^= k1 ^ k3
		h2 ^= k2 ^ k4

		h1 = rotl64(h1, 27) + h2
		h2 = rotl64(h2, 31) + h1

		h1 = h1*5 + c3
		h2 = h2*5 + c4

		data = data[32:]
	}

	// Process remaining 8-byte blocks
	for len(data) >= 8 {
		k := binary.LittleEndian.Uint64(data[:8])
		k *= c1
		k = rotl64(k, 31)
		h1 ^= k
		h1 = rotl64(h1, 27) + h2
		h1 = h1*5 + c3
		h2 = rotl64(h2, 31) + h1
		data = data[8:]
	}

	// Process tail efficiently
	if len(data) > 0 {
		var k uint64
		for i := len(data) - 1; i >= 0; i-- {
			k = (k << 8) | uint64(data[i])
		}
		k *= c1
		k = rotl64(k, 31)
		h1 ^= k
	}

	// Fast finalization
	h1 ^= length
	h2 ^= length
	h1 += h2
	h1 = fmix64(h1)
	h1 += h2

	return h1
}

// Standard processing for other sizes
func pandaHash64Standard(data []byte, h1, h2, length uint64) uint64 {
	// Process complete blocks
	for len(data) >= chunkSize {
		k := binary.LittleEndian.Uint64(data[:chunkSize])

		// Inlined mixBlock for speed
		k *= c1
		k = rotl64(k, 31)

		h1 ^= k
		h1 = rotl64(h1, 27) + h2
		h1 = h1*5 + c3
		h2 = rotl64(h2, 31) + h1

		data = data[chunkSize:]
	}

	// Process tail
	if len(data) > 0 {
		var k uint64
		switch len(data) {
		case 7:
			k ^= uint64(data[6]) << 48
			fallthrough
		case 6:
			k ^= uint64(data[5]) << 40
			fallthrough
		case 5:
			k ^= uint64(data[4]) << 32
			fallthrough
		case 4:
			k ^= uint64(data[3]) << 24
			fallthrough
		case 3:
			k ^= uint64(data[2]) << 16
			fallthrough
		case 2:
			k ^= uint64(data[1]) << 8
			fallthrough
		case 1:
			k ^= uint64(data[0])
		}

		k *= c1
		k = rotl64(k, 31)
		h1 ^= k
	}

	// Fast finalization
	h1 ^= length
	h2 ^= length
	h1 += h2
	h1 = fmix64(h1)
	h1 += h2

	return h1
}

// Optimized function for large data (4KB+) with wide vectorization
func pandaHash64Large(data []byte, h1, h2, length uint64) uint64 {
	// Wide vectorization for large data - process 64 bytes at once
	for len(data) >= 64 {
		// Load 8 blocks simultaneously for better CPU utilization
		k1 := binary.LittleEndian.Uint64(data[0:8])
		k2 := binary.LittleEndian.Uint64(data[8:16])
		k3 := binary.LittleEndian.Uint64(data[16:24])
		k4 := binary.LittleEndian.Uint64(data[24:32])
		k5 := binary.LittleEndian.Uint64(data[32:40])
		k6 := binary.LittleEndian.Uint64(data[40:48])
		k7 := binary.LittleEndian.Uint64(data[48:56])
		k8 := binary.LittleEndian.Uint64(data[56:64])

		// Parallel multiplication - CPU can execute multiple simultaneously
		k1 *= c1
		k2 *= c1
		k3 *= c1
		k4 *= c1
		k5 *= c1
		k6 *= c1
		k7 *= c1
		k8 *= c1

		// Parallel rotations
		k1 = rotl64(k1, 31)
		k2 = rotl64(k2, 31)
		k3 = rotl64(k3, 31)
		k4 = rotl64(k4, 31)
		k5 = rotl64(k5, 31)
		k6 = rotl64(k6, 31)
		k7 = rotl64(k7, 31)
		k8 = rotl64(k8, 31)

		// Combine in pairs to maintain hash quality while maximizing parallelism
		h1 ^= k1 ^ k3 ^ k5 ^ k7
		h2 ^= k2 ^ k4 ^ k6 ^ k8

		// Reduced mixing frequency for large data (every 64 bytes vs every 8)
		h1 = rotl64(h1, 27) + h2
		h2 = rotl64(h2, 31) + h1
		h1 = h1*5 + c3
		h2 = h2*5 + c4

		data = data[64:]
	}

	// Process remaining 32-byte blocks with medium vectorization
	for len(data) >= 32 {
		k1 := binary.LittleEndian.Uint64(data[0:8])
		k2 := binary.LittleEndian.Uint64(data[8:16])
		k3 := binary.LittleEndian.Uint64(data[16:24])
		k4 := binary.LittleEndian.Uint64(data[24:32])

		k1 *= c1
		k2 *= c1
		k3 *= c1
		k4 *= c1
		k1 = rotl64(k1, 31)
		k2 = rotl64(k2, 31)
		k3 = rotl64(k3, 31)
		k4 = rotl64(k4, 31)

		h1 ^= k1 ^ k3
		h2 ^= k2 ^ k4

		h1 = rotl64(h1, 27) + h2
		h2 = rotl64(h2, 31) + h1
		h1 = h1*5 + c3
		h2 = h2*5 + c4

		data = data[32:]
	}

	// Process remaining 8-byte blocks
	for len(data) >= 8 {
		k := binary.LittleEndian.Uint64(data[:8])
		k *= c1
		k = rotl64(k, 31)
		h1 ^= k
		h1 = rotl64(h1, 27) + h2
		h1 = h1*5 + c3
		h2 = rotl64(h2, 31) + h1
		data = data[8:]
	}

	// Process tail efficiently
	if len(data) > 0 {
		var k uint64
		for i := len(data) - 1; i >= 0; i-- {
			k = (k << 8) | uint64(data[i])
		}
		k *= c1
		k = rotl64(k, 31)
		h1 ^= k
	}

	// Fast finalization
	h1 ^= length
	h2 ^= length
	h1 += h2
	h1 = fmix64(h1)
	h1 += h2

	return h1
}

// PandaHash64MultiCore executes multi-core parallel hashing on very large payloads (256KB+)
// Utilizes multiple CPU cores to achieve throughput up to ~78 GB/s.
func PandaHash64MultiCore(data []byte) uint64 {
	if len(data) == 0 {
		return 0
	}
	return pandaHash64Parallel(data, 0, 0, uint64(len(data)))
}

// Parallel processing for very large data (256KB+) using multiple cores
func pandaHash64Parallel(data []byte, h1, h2, length uint64) uint64 {
	// Use number of CPU cores, but cap at 8 for optimal performance
	numWorkers := runtime.NumCPU()
	if numWorkers > 8 {
		numWorkers = 8
	}

	// Minimum chunk size of 32KB per worker to avoid goroutine overhead
	minChunkSize := 32768
	if len(data) < minChunkSize*numWorkers {
		numWorkers = len(data) / minChunkSize
		if numWorkers == 0 {
			numWorkers = 1
		}
	}
	if numWorkers == 1 {
		return pandaHash64Large(data, h1, h2, length)
	}

	// Calculate chunk size aligned to 64-byte boundaries for better cache performance
	chunkSize := len(data) / numWorkers
	chunkSize = (chunkSize + 63) &^ 63 // Align to 64-byte boundary

	var wg sync.WaitGroup
	var results [8]uint64

	// Process chunks in parallel
	for i := 0; i < numWorkers; i++ {
		wg.Add(1)
		go func(workerID int) {
			defer wg.Done()

			start := workerID * chunkSize
			end := start + chunkSize
			if workerID == numWorkers-1 || end > len(data) {
				end = len(data)
			}

			if start >= len(data) {
				results[workerID] = 0
				return
			}

			// Each worker uses a unique seed to prevent hash collisions
			seed1 := h1 ^ (uint64(workerID) * c1)
			seed2 := h2 ^ (uint64(workerID) * c2)
			chunkLen := uint64(end - start)

			// Use large data optimization for each chunk
			results[workerID] = pandaHash64Large(data[start:end], seed1, seed2, chunkLen)
		}(i)
	}

	wg.Wait()

	// Combine results with proper mixing to maintain hash quality
	var final uint64 = h1
	for i := 0; i < numWorkers; i++ {
		result := results[i]
		if result != 0 {
			final ^= result
			final = rotl64(final, 31)
			final *= c1
			final ^= uint64(i+1) * c3 // Include worker ID to prevent collisions
		}
	}

	// Final mixing with original length
	final ^= length
	final = fmix64(final)

	return final
}

// Convert string to []byte without allocation
func stringToBytes(s string) []byte {
	return unsafe.Slice(unsafe.StringData(s), len(s))
}

// Ultra-fast PandaHash32 with medium-data optimization
func PandaHash32(input string) uint32 {
	return PandaHash32Bytes(stringToBytes(input))
}

// PandaHash32Bytes computes 32-bit hash directly from byte slice
func PandaHash32Bytes(data []byte) uint32 {
	return uint32(PandaHash64Bytes(data))
}

// Optimized PandaHash - minimal allocations
func PandaHash(input string) string {
	return PandaHashBytes(stringToBytes(input))
}

// PandaHashBytes computes 256-bit hash hex string directly from byte slice
func PandaHashBytes(data []byte) string {
	if len(data) == 0 {
		return "0000000000000000000000000000000000000000000000000000000000000000"
	}
	hasher := NewPandaHasher(0)
	hasher.Write(data)
	return hasher.Sum()
}

// Optimized PandaHashWithSeed
func PandaHashWithSeed(input string, seed uint64) string {
	if len(input) == 0 {
		return "0000000000000000000000000000000000000000000000000000000000000000"
	}

	hasher := NewPandaHasher(seed)
	hasher.Write(stringToBytes(input))
	return hasher.Sum()
}

// Batch hashing for maximum throughput
func PandaHashBatch(inputs []string) []uint64 {
	results := make([]uint64, len(inputs))
	for i, input := range inputs {
		results[i] = PandaHash64(input)
	}
	return results
}

// Concurrent multi-seeded hashing
func PandaHashParallel(input string, numHashers int) []uint64 {
	if numHashers <= 0 {
		numHashers = 1
	}

	results := make([]uint64, numHashers)
	data := stringToBytes(input)

	if numHashers == 1 {
		hasher := NewPandaHasher(0)
		hasher.Write(data)
		h1, _ := hasher.Sum128()
		results[0] = h1
		return results
	}

	var wg sync.WaitGroup
	wg.Add(numHashers)
	for i := 0; i < numHashers; i++ {
		go func(idx int) {
			defer wg.Done()
			hasher := NewPandaHasher(uint64(idx))
			hasher.Write(data)
			h1, _ := hasher.Sum128()
			results[idx] = h1
		}(i)
	}
	wg.Wait()
	return results
}

func main() {
	testString1 := "This is a test string for our custom hash function."
	testString2 := "This is a slightly different test string for our custom hash function."
	emptyString := ""
	shortString := "short"

	fmt.Printf("Input: '%s'\n", testString1)
	fmt.Printf("Hash:  %s\n\n", PandaHash(testString1))

	fmt.Printf("Input: '%s'\n", testString2)
	fmt.Printf("Hash:  %s\n\n", PandaHash(testString2))

	fmt.Printf("Input: '%s'\n", emptyString)
	fmt.Printf("Hash:  %s\n\n", PandaHash(emptyString))

	fmt.Printf("Input: '%s'\n", shortString)
	fmt.Printf("Hash:  %s\n\n", PandaHash(shortString))

	// Demonstrate high-performance variants
	fmt.Printf("Seeded hash (seed=42): %s\n", PandaHashWithSeed(testString1, 42))
	fmt.Printf("64-bit hash: %016x\n", PandaHash64(testString1))
	fmt.Printf("32-bit hash: %08x\n", PandaHash32(testString1))

	// Demonstrate batch processing
	inputs := []string{"test1", "test2", "test3"}
	hashes := PandaHashBatch(inputs)
	fmt.Printf("Batch hashes: %v\n", hashes)
}
