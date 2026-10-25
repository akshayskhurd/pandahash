package main

import (
	"fmt"
	"strings"
	"testing"
)

func TestPandaHashCorrectness(t *testing.T) {
	testCases := []struct {
		name  string
		input string
	}{
		{
			name:  "Empty String",
			input: "",
		},
		{
			name:  "Short String (less than one chunk)",
			input: "short",
		},
		{
			name:  "One chunk exactly",
			input: "exactly8",
		},
		{
			name:  "Exactly two chunks",
			input: "1234567890123456",
		},
		{
			name:  "One byte short of a chunk",
			input: "1234567",
		},
		{
			name:  "One byte over a chunk",
			input: "123456789",
		},
		{
			name:  "UTF-8 characters",
			input: "你好世界",
		},
		{
			name:  "Multi-chunk string",
			input: "This is a test string for our custom hash function.",
		},
		{
			name:  "Slightly different multi-chunk string",
			input: "This is a slightly different test string for our custom hash function.",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			hash := PandaHash(tc.input)

			// Verify hash is 64 characters (256 bits)
			if len(hash) != 64 {
				t.Errorf("PandaHash(%q) returned hash of length %d, expected 64", tc.input, len(hash))
			}

			// Verify hash contains only hex characters
			for _, char := range hash {
				if !strings.ContainsRune("0123456789abcdef", char) {
					t.Errorf("PandaHash(%q) returned non-hex character: %c", tc.input, char)
				}
			}

			// Verify consistency - same input should produce same hash
			hash2 := PandaHash(tc.input)
			if hash != hash2 {
				t.Errorf("PandaHash(%q) is not consistent: %s != %s", tc.input, hash, hash2)
			}
		})
	}
}

func TestPandaHashEmptyString(t *testing.T) {
	expected := "0000000000000000000000000000000000000000000000000000000000000000"
	actual := PandaHash("")
	if actual != expected {
		t.Errorf("PandaHash(\"\") = %s; want %s", actual, expected)
	}
}

func TestPandaHashWithSeed(t *testing.T) {
	input := "test string"

	// Different seeds should produce different hashes
	hash1 := PandaHashWithSeed(input, 0)
	hash2 := PandaHashWithSeed(input, 1)
	hash3 := PandaHashWithSeed(input, 42)

	if hash1 == hash2 {
		t.Error("Different seeds produced same hash")
	}
	if hash1 == hash3 {
		t.Error("Different seeds produced same hash")
	}
	if hash2 == hash3 {
		t.Error("Different seeds produced same hash")
	}

	// Same seed should produce same hash
	hash4 := PandaHashWithSeed(input, 42)
	if hash3 != hash4 {
		t.Error("Same seed produced different hashes")
	}
}

func TestPandaHash64(t *testing.T) {
	input := "test string"
	hash64 := PandaHash64(input)

	// Should be non-zero for non-empty input
	if hash64 == 0 {
		t.Error("PandaHash64 returned zero for non-empty input")
	}

	// Should be consistent
	hash64_2 := PandaHash64(input)
	if hash64 != hash64_2 {
		t.Error("PandaHash64 is not consistent")
	}
}

func TestPandaHashAvalanche(t *testing.T) {
	// Test avalanche effect - small input changes should cause large output changes
	base := "test string"
	modified := "test strinG" // Changed last character

	hash1 := PandaHash(base)
	hash2 := PandaHash(modified)

	if hash1 == hash2 {
		t.Error("Similar inputs produced identical hashes - poor avalanche effect")
	}

	// Count different bits in hex representation
	differentChars := 0
	for i := 0; i < len(hash1) && i < len(hash2); i++ {
		if hash1[i] != hash2[i] {
			differentChars++
		}
	}

	// Should have significant differences (at least 25% of characters different)
	if differentChars < len(hash1)/4 {
		t.Errorf("Poor avalanche effect: only %d/%d characters different", differentChars, len(hash1))
	}
}

func TestPandaHashDistribution(t *testing.T) {
	t.Run("Collision Test", func(t *testing.T) {
		hashSet := make(map[string]bool)
		numToTest := 10000 // Increased for better collision detection

		for i := 0; i < numToTest; i++ {
			input := fmt.Sprintf("test_string_%d", i)
			hash := PandaHash(input)

			if hashSet[hash] {
				t.Errorf("Collision detected for input %q, hash %q already exists", input, hash)
				return
			}
			hashSet[hash] = true
		}

		if len(hashSet) != numToTest {
			t.Errorf("Expected %d unique hashes, but got %d", numToTest, len(hashSet))
		}
	})

	t.Run("Bit Distribution Test", func(t *testing.T) {
		// Test that hash bits are roughly evenly distributed
		bitCounts := make([]int, 256) // 256 bits in hash
		numSamples := 1000

		for i := 0; i < numSamples; i++ {
			input := fmt.Sprintf("sample_%d", i)
			hash := PandaHash(input)

			// Convert hex string to bit counts
			for charIdx, char := range hash {
				var nibble uint8
				if char >= '0' && char <= '9' {
					nibble = uint8(char - '0')
				} else if char >= 'a' && char <= 'f' {
					nibble = uint8(char - 'a' + 10)
				}

				// Count bits in this nibble
				for bit := 0; bit < 4; bit++ {
					if (nibble>>bit)&1 == 1 {
						bitCounts[charIdx*4+bit]++
					}
				}
			}
		}

		// Each bit should be set roughly 50% of the time
		expectedCount := numSamples / 2
		tolerance := numSamples / 8 // 12.5% tolerance (more lenient)

		poorBits := 0
		for _, count := range bitCounts {
			if count < expectedCount-tolerance || count > expectedCount+tolerance {
				poorBits++
			}
		}

		// Allow up to 5% of bits to have poor distribution
		if poorBits > len(bitCounts)/20 {
			t.Errorf("Too many bits with poor distribution: %d/%d", poorBits, len(bitCounts))
		}
	})
}

func TestPandaHasherIncremental(t *testing.T) {
	testString := "The quick brown fox jumps over the lazy dog! 1234567890 PandaHash streaming verification test."
	data := []byte(testString)

	// Baseline: All at once
	hasherBase := NewPandaHasher(42)
	hasherBase.Write(data)
	expectedHash := hasherBase.Sum()

	// Test various chunk sizes from 1 byte to 64 bytes
	chunkSizes := []int{1, 2, 3, 4, 7, 8, 9, 15, 16, 24, 31, 32, 48, 64}

	for _, chunkSize := range chunkSizes {
		t.Run(fmt.Sprintf("ChunkSize_%d", chunkSize), func(t *testing.T) {
			hasher := NewPandaHasher(42)
			for i := 0; i < len(data); i += chunkSize {
				end := i + chunkSize
				if end > len(data) {
					end = len(data)
				}
				hasher.Write(data[i:end])
			}
			actualHash := hasher.Sum()
			if actualHash != expectedHash {
				t.Fatalf("Chunked hash mismatch for chunkSize %d:\n  Got:      %s\n  Expected: %s",
					chunkSize, actualHash, expectedHash)
			}
		})
	}
}

func TestPandaHasherReset(t *testing.T) {
	data := []byte("Testing hasher reset functionality.")
	hasher := NewPandaHasher(12345)
	hasher.Write(data)
	hash1 := hasher.Sum()

	// Reset and hash same data
	hasher.Reset()
	hasher.Write(data)
	hash2 := hasher.Sum()

	if hash1 != hash2 {
		t.Errorf("Hasher produced different hash after Reset: %s != %s", hash1, hash2)
	}
}

func TestPandaHasherFinalization(t *testing.T) {
	hasher := &PandaHasher{h1: 0, h2: 0}
	hasher.Write([]byte("test"))

	// First call should work
	hash1 := hasher.Sum()

	// Second call should return same result
	hash2 := hasher.Sum()
	if hash1 != hash2 {
		t.Error("Multiple calls to Sum() returned different results")
	}

	// Writing after finalization should panic
	defer func() {
		if r := recover(); r == nil {
			t.Error("Expected panic when writing to finalized hasher")
		}
	}()
	hasher.Write([]byte("more data"))
}

func BenchmarkPandaHash(b *testing.B) {
	// Helper function to create a string of a given size
	makeString := func(size int) string {
		bytes := make([]byte, size)
		for i := range bytes {
			bytes[i] = 'a'
		}
		return string(bytes)
	}

	benchmarkCases := []struct {
		name      string
		inputSize int
	}{
		{"8B", 8},
		{"64B", 64},
		{"1KB", 1024},
		{"8KB", 8 * 1024},
		{"64KB", 64 * 1024},
	}

	for _, bm := range benchmarkCases {
		b.Run(fmt.Sprintf("InputSize_%s", bm.name), func(b *testing.B) {
			input := makeString(bm.inputSize)
			b.ReportAllocs()
			b.ResetTimer()

			for i := 0; i < b.N; i++ {
				PandaHash(input)
			}
		})
	}
}

func BenchmarkPandaHashIncremental(b *testing.B) {
	input := []byte("This is a test string for benchmarking incremental hashing performance")

	b.ReportAllocs()
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		hasher := &PandaHasher{h1: 0, h2: 0}
		hasher.Write(input)
		hasher.Sum()
	}
}

func BenchmarkPandaHash64(b *testing.B) {
	input := "This is a test string for benchmarking 64-bit hash performance"

	b.ReportAllocs()
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		PandaHash64(input)
	}
}
