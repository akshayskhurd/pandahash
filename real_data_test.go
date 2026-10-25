package main

import (
	"hash/crc32"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/cespare/xxhash/v2"
	"github.com/dgryski/go-wyhash"
	"github.com/spaolacci/murmur3"
	"github.com/zeebo/xxh3"
)

// getAllCorpusFiles returns all non-hidden, non-markdown data files in the downloaded corpus
func getAllCorpusFiles(t testing.TB) []string {
	var files []string
	root := filepath.Join("testdata", "corpora")
	if _, err := os.Stat(root); os.IsNotExist(err) {
		t.Skip("testdata/corpora not present; skipping real-world corpus test")
		return nil
	}
	err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if !info.IsDir() && !strings.HasPrefix(info.Name(), ".") && !strings.HasSuffix(info.Name(), ".md") {
			files = append(files, path)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("Failed to scan testdata: %v", err)
	}
	if len(files) == 0 {
		t.Skip("No corpus files found in testdata/corpora")
		return nil
	}
	return files
}

// 1. ADVERSARIAL STREAMING VERIFICATION: Chunked Write MUST match All-at-Once Write
func TestAllCorporaStreamingInvariance(t *testing.T) {
	files := getAllCorpusFiles(t)
	chunkSizes := []int{1, 3, 7, 8, 15, 31, 32, 64, 1024, 4096}

	// Test across files spanning each domain
	for _, path := range files {
		info, err := os.Stat(path)
		if err != nil || info.Size() > 2*1024*1024 { // Cap at 2MB for per-byte chunk testing speed
			continue
		}

		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("Failed to read %s: %v", path, err)
		}

		// Baseline: All at once
		hBase := NewPandaHasher(12345)
		hBase.Write(data)
		expectedHash := hBase.Sum()

		for _, cs := range chunkSizes {
			hChunked := NewPandaHasher(12345)
			for i := 0; i < len(data); i += cs {
				end := i + cs
				if end > len(data) {
					end = len(data)
				}
				hChunked.Write(data[i:end])
			}
			chunkedHash := hChunked.Sum()

			if chunkedHash != expectedHash {
				t.Fatalf("[STREAMING INVARIANCE BREACH] File: %s (%d bytes), ChunkSize: %d\n  Expected: %s\n  Got:      %s",
					path, len(data), cs, expectedHash, chunkedHash)
			}
		}
	}
}

// 2. COLLISION VERIFICATION: Across ALL files in Calgary, Canterbury, Large, Silesia, Snappy
func TestAllCorporaCollisionFreeness(t *testing.T) {
	files := getAllCorpusFiles(t)
	seen64 := make(map[uint64]string)
	seen256 := make(map[string]string)

	for _, path := range files {
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("Failed to read %s: %v", path, err)
		}

		h64 := PandaHash64Bytes(data)
		h256 := PandaHashBytes(data)

		// Check for 64-bit collision
		if prevFile, exists := seen64[h64]; exists {
			// Compare contents to ensure files are actually distinct
			prevData, _ := os.ReadFile(prevFile)
			if string(data) != string(prevData) {
				t.Fatalf("[64-BIT COLLISION DETECTED] %s and %s produce identical hash: %016x",
					path, prevFile, h64)
			}
		} else {
			seen64[h64] = path
		}

		// Check for 256-bit collision
		if prevFile, exists := seen256[h256]; exists {
			prevData, _ := os.ReadFile(prevFile)
			if string(data) != string(prevData) {
				t.Fatalf("[256-BIT COLLISION DETECTED] %s and %s produce identical hash: %s",
					path, prevFile, h256)
			}
		} else {
			seen256[h256] = path
		}
	}

	t.Logf("Audited %d distinct real-world files: ZERO collisions detected in 64-bit and 256-bit sets.", len(files))
}

// 3. MATHEMATICAL ENTROPY VERIFICATION: Ensure h4 is NEVER rotl64(h3, 32)
func TestAllCorporaIndependentEntropy(t *testing.T) {
	files := getAllCorpusFiles(t)

	for _, path := range files {
		data, err := os.ReadFile(path)
		if err != nil {
			continue
		}

		h := NewPandaHasher(0)
		h.Write(data)
		h1, h2 := h.Sum128()
		h3 := fmix64(h1^c2) + rotl64(h2, 27)
		h4 := fmix64(h2^c4) + rotl64(h1, 31)

		if h4 == rotl64(h3, 32) {
			t.Fatalf("[ENTROPY DEFECT] File %s: h4 is a degenerate 32-bit rotation of h3", path)
		}
	}
}

// 4. REAL-WORLD AVALANCHE EFFECT TEST: 1-bit perturbation in actual files
func TestAllCorporaAvalancheEffect(t *testing.T) {
	files := getAllCorpusFiles(t)

	for _, path := range files {
		data, err := os.ReadFile(path)
		if err != nil || len(data) == 0 {
			continue
		}

		hashOrig := PandaHashBytes(data)

		// Perturb single bit in the middle of file
		perturbed := make([]byte, len(data))
		copy(perturbed, data)
		perturbed[len(perturbed)/2] ^= 0x01

		hashPerturbed := PandaHashBytes(perturbed)

		if hashOrig == hashPerturbed {
			t.Fatalf("[ZERO AVALANCHE] File %s: 1-bit modification resulted in identical hash", path)
		}

		// Count character differences in 64-char hex string
		diffCount := 0
		for i := 0; i < len(hashOrig) && i < len(hashPerturbed); i++ {
			if hashOrig[i] != hashPerturbed[i] {
				diffCount++
			}
		}

		// At least 25% of hex characters must change
		if diffCount < len(hashOrig)/4 {
			t.Errorf("[WEAK AVALANCHE] File %s: only %d/64 hex characters flipped on 1-bit delta",
				path, diffCount)
		}
	}
}

// 5. CONCURRENT STRESS TEST: Multi-threaded hashing across real datasets
func TestAllCorporaConcurrentStress(t *testing.T) {
	files := getAllCorpusFiles(t)
	var wg sync.WaitGroup

	for _, path := range files[:15] { // Select first 15 diverse files
		data, err := os.ReadFile(path)
		if err != nil {
			continue
		}

		expected := PandaHash64Bytes(data)

		for worker := 0; worker < 10; worker++ {
			wg.Add(1)
			go func(d []byte, exp uint64) {
				defer wg.Done()
				for iter := 0; iter < 50; iter++ {
					actual := PandaHash64Bytes(d)
					if actual != exp {
						t.Errorf("Concurrent mismatch: got %016x expected %016x", actual, exp)
						return
					}
				}
			}(data, expected)
		}
	}
	wg.Wait()
}

// 6. BENCHMARK SUITE: Spanning diverse real-world file categories
func BenchmarkAllCorporaRepresentative(b *testing.B) {
	targetFiles := []struct {
		desc string
		path string
	}{
		{"Protobuf_Binary", "testdata/corpora/snappy/geo.protodata"}, // 118 KB
		{"JPEG_Image", "testdata/corpora/snappy/fireworks.jpeg"},      // 123 KB
		{"PDF_Document", "testdata/corpora/snappy/paper-100k.pdf"},    // 102 KB
		{"Genomic_DNA", "testdata/corpora/large/E.coli"},              // 4.6 MB
		{"Source_Code", "testdata/corpora/canterbury/fields.c"},       // 11 KB
		{"HTML_Data", "testdata/corpora/canterbury/cp.html"},          // 24 KB
		{"Spreadsheet", "testdata/corpora/canterbury/kennedy.xls"},    // 1.0 MB
	}

	for _, tf := range targetFiles {
		data, err := os.ReadFile(tf.path)
		if err != nil {
			b.Fatalf("Failed to read %s: %v", tf.path, err)
		}

		b.Run("PandaHash64_"+tf.desc, func(b *testing.B) {
			b.SetBytes(int64(len(data)))
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				_ = PandaHash64Bytes(data)
			}
		})

		b.Run("MurmurHash3_"+tf.desc, func(b *testing.B) {
			b.SetBytes(int64(len(data)))
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				_ = murmur3.Sum64(data)
			}
		})

		b.Run("CRC32_"+tf.desc, func(b *testing.B) {
			b.SetBytes(int64(len(data)))
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				_ = crc32.ChecksumIEEE(data)
			}
		})

		b.Run("XXH3_"+tf.desc, func(b *testing.B) {
			b.SetBytes(int64(len(data)))
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				_ = xxh3.Hash(data)
			}
		})

		b.Run("xxHash64_"+tf.desc, func(b *testing.B) {
			b.SetBytes(int64(len(data)))
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				_ = xxhash.Sum64(data)
			}
		})

		b.Run("wyhash_"+tf.desc, func(b *testing.B) {
			b.SetBytes(int64(len(data)))
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				_ = wyhash.Hash(data, 0)
			}
		})
	}
}
