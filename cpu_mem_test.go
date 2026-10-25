package main

import (
	"fmt"
	"hash/crc32"
	"os"
	"runtime"
	"syscall"
	"testing"
	"time"

	"github.com/cespare/xxhash/v2"
	"github.com/dgryski/go-wyhash"
	"github.com/spaolacci/murmur3"
	"github.com/zeebo/xxh3"
)

type HashProfileResult struct {
	Name         string
	Throughput   float64 // GB/s
	LatencyNs    float64 // ns/op
	BytesPerOp   uint64  // B/op
	AllocsPerOp  uint64  // allocs/op
	CPUTimeNs    float64 // CPU time per op
	WallTimeNs   float64 // Wall time per op
	CPUEfficiency float64 // CPU Utilization ratio (CPU time / Wall time)
}

func getRusageCPUTime() time.Duration {
	var rusage syscall.Rusage
	_ = syscall.Getrusage(syscall.RUSAGE_SELF, &rusage)
	uSec := int64(rusage.Utime.Sec)*1e9 + int64(rusage.Utime.Usec)*1e3
	sSec := int64(rusage.Stime.Sec)*1e9 + int64(rusage.Stime.Usec)*1e3
	return time.Duration(uSec + sSec)
}

func profileAlgorithm(name string, data []byte, iterations int, fn func([]byte)) HashProfileResult {
	// Warmup to bring CPU frequency out of low-power idle state
	for i := 0; i < 1000; i++ {
		fn(data)
	}

	// Force GC and settle before measurement
	runtime.GC()
	time.Sleep(10 * time.Millisecond)

	var memStart, memEnd runtime.MemStats
	runtime.ReadMemStats(&memStart)

	cpuStart := getRusageCPUTime()
	wallStart := time.Now()

	for i := 0; i < iterations; i++ {
		fn(data)
	}

	wallElapsed := time.Since(wallStart)
	cpuElapsed := getRusageCPUTime() - cpuStart

	runtime.ReadMemStats(&memEnd)

	totalBytes := float64(len(data)) * float64(iterations)
	throughputGBs := (totalBytes / wallElapsed.Seconds()) / (1024 * 1024 * 1024)
	latencyNs := float64(wallElapsed.Nanoseconds()) / float64(iterations)
	cpuNs := float64(cpuElapsed.Nanoseconds()) / float64(iterations)

	bytesAlloc := (memEnd.TotalAlloc - memStart.TotalAlloc) / uint64(iterations)
	allocsCount := (memEnd.Mallocs - memStart.Mallocs) / uint64(iterations)

	cpuRatio := 0.0
	if latencyNs > 0 {
		cpuRatio = cpuNs / latencyNs
	}

	return HashProfileResult{
		Name:          name,
		Throughput:    throughputGBs,
		LatencyNs:     latencyNs,
		BytesPerOp:    bytesAlloc,
		AllocsPerOp:   allocsCount,
		CPUTimeNs:     cpuNs,
		WallTimeNs:    latencyNs,
		CPUEfficiency: cpuRatio,
	}
}

func TestComprehensiveCPUMemorySpeedProfile(t *testing.T) {
	testScenarios := []struct {
		name       string
		path       string
		iterations int
	}{
		{"Small File (11 KB, C Source Code)", "testdata/corpora/canterbury/fields.c", 50000},
		{"Medium File (118 KB, Protobuf Binary)", "testdata/corpora/snappy/geo.protodata", 15000},
		{"Large File (1.0 MB, Spreadsheet)", "testdata/corpora/canterbury/kennedy.xls", 2000},
		{"Huge File (4.6 MB, Genomic DNA)", "testdata/corpora/large/E.coli", 600},
	}

	for _, sc := range testScenarios {
		data, err := os.ReadFile(sc.path)
		if err != nil {
			t.Skipf("Corpus file %s not present; skipping profile", sc.path)
			continue
		}

		fmt.Printf("\n===================================================================================================\n")
		fmt.Printf("📊 BENCHMARK PROFILE: %s [%d bytes | %d iterations]\n", sc.name, len(data), sc.iterations)
		fmt.Printf("===================================================================================================\n")
		fmt.Printf("%-16s | %-12s | %-12s | %-11s | %-11s | %-12s | %-12s\n",
			"Algorithm", "Throughput", "Latency", "Heap Alloc", "Allocs/op", "CPU Time", "CPU Cores Used")
		fmt.Printf("-----------------|--------------|--------------|-------------|-------------|--------------|--------------\n")

		algos := []struct {
			name string
			fn   func([]byte)
		}{
			{"PandaHash64", func(d []byte) { _ = PandaHash64Bytes(d) }},
			{"XXH3 (v1.1.0)", func(d []byte) { _ = xxh3.Hash(d) }},
			{"wyhash", func(d []byte) { _ = wyhash.Hash(d, 0) }},
			{"xxHash64 (v2.3)", func(d []byte) { _ = xxhash.Sum64(d) }},
			{"Hardware CRC32", func(d []byte) { _ = crc32.ChecksumIEEE(d) }},
			{"MurmurHash3-64", func(d []byte) { _ = murmur3.Sum64(d) }},
		}

		for _, a := range algos {
			res := profileAlgorithm(a.name, data, sc.iterations, a.fn)

			latStr := fmt.Sprintf("%.1f ns", res.LatencyNs)
			if res.LatencyNs >= 1000 {
				latStr = fmt.Sprintf("%.2f µs", res.LatencyNs/1000)
			}
			cpuStr := fmt.Sprintf("%.1f ns", res.CPUTimeNs)
			if res.CPUTimeNs >= 1000 {
				cpuStr = fmt.Sprintf("%.2f µs", res.CPUTimeNs/1000)
			}

			fmt.Printf("%-16s | %7.2f GB/s  | %12s | %7d B/op  | %7d ops   | %12s | %6.2fx cores\n",
				res.Name, res.Throughput, latStr, res.BytesPerOp, res.AllocsPerOp, cpuStr, res.CPUEfficiency)
		}
	}
	fmt.Printf("===================================================================================================\n\n")
}
