package main

import (
	"encoding/json"
	"fmt"
	"os"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
)

// physicalGPUBackend is a physical arithmetic accelerator only. It receives
// anonymous scalar lanes; no semantic dimension, score, utility or policy is
// visible to the backend.
type physicalGPUBackend interface {
	Name() string
	Available() bool
	Dot(vectors [][]float32, weights []float32) ([]float32, error)
	Info() map[string]any
}

type parallelRuntime struct {
	concurrency int64
	jobs        uint64
	peak        int64
	inflight    int64
	gpuJobs     uint64
	gpuFallback uint64
}

var globalParallelRuntime = newParallelRuntime()
var globalPhysicalGPU physicalGPUBackend = newPhysicalGPUBackend()
var physicalGPUMinScalarOps int64 = defaultPhysicalGPUThreshold()

func defaultPhysicalGPUThreshold() int64 {
	const fallback int64 = 16384
	raw := strings.TrimSpace(os.Getenv("MEMORYAI_GPU_MIN_SCALAR_OPS"))
	if raw == "" {
		return fallback
	}
	n, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || n < 1 {
		return fallback
	}
	return n
}

func newParallelRuntime() *parallelRuntime {
	n := runtime.GOMAXPROCS(0)
	if n < 1 {
		n = 1
	}
	return &parallelRuntime{concurrency: int64(n)}
}

func setPhysicalExecutionConcurrency(n int) {
	if n < 1 {
		n = 1
	}
	physical := runtime.GOMAXPROCS(0)
	if physical < 1 {
		physical = 1
	}
	if n > physical {
		n = physical
	}
	atomic.StoreInt64(&globalParallelRuntime.concurrency, int64(n))
}

func physicalConcurrency(requested int) int {
	limit := int(atomic.LoadInt64(&globalParallelRuntime.concurrency))
	if limit < 1 {
		limit = 1
	}
	if requested > 0 && requested < limit {
		limit = requested
	}
	if limit < 1 {
		limit = 1
	}
	return limit
}

func observeParallelInflight(delta int64) {
	n := atomic.AddInt64(&globalParallelRuntime.inflight, delta)
	if delta <= 0 {
		return
	}
	for {
		old := atomic.LoadInt64(&globalParallelRuntime.peak)
		if n <= old || atomic.CompareAndSwapInt64(&globalParallelRuntime.peak, old, n) {
			return
		}
	}
}

func parallelCPUFor(count, maxParallel int, fn func(int)) {
	if count <= 0 || fn == nil {
		return
	}
	workers := physicalConcurrency(maxParallel)
	if workers > count {
		workers = count
	}
	atomic.AddUint64(&globalParallelRuntime.jobs, uint64(count))
	if workers <= 1 {
		for i := 0; i < count; i++ {
			observeParallelInflight(1)
			fn(i)
			observeParallelInflight(-1)
		}
		return
	}
	jobs := make(chan int)
	var wg sync.WaitGroup
	wg.Add(workers)
	for w := 0; w < workers; w++ {
		go func() {
			defer wg.Done()
			for i := range jobs {
				observeParallelInflight(1)
				fn(i)
				observeParallelInflight(-1)
			}
		}()
	}
	for i := 0; i < count; i++ {
		jobs <- i
	}
	close(jobs)
	wg.Wait()
}

func cpuDot(vectors [][]float32, weights []float32) []float32 {
	out := make([]float32, len(vectors))
	parallelCPUFor(len(vectors), 0, func(i int) {
		var sum float32
		for lane := range weights {
			sum += vectors[i][lane] * weights[lane]
		}
		out[i] = sum
	})
	return out
}

func validateDotShape(vectors [][]float32, weights []float32) error {
	if len(weights) == 0 {
		return fmt.Errorf("parallel dot requires at least one physical lane")
	}
	for i := range vectors {
		if len(vectors[i]) != len(weights) {
			return fmt.Errorf("parallel dot lane mismatch at row %d: %d != %d", i, len(vectors[i]), len(weights))
		}
	}
	return nil
}

func (r *parallelRuntime) Dot(vectors [][]float32, weights []float32) ([]float32, string, error) {
	if err := validateDotShape(vectors, weights); err != nil {
		return nil, "", err
	}
	if len(vectors) == 0 {
		return []float32{}, "cpu-dot", nil
	}
	gpu := globalPhysicalGPU
	scalarOps := int64(len(vectors)) * int64(len(weights))
	if gpu == nil || !gpu.Available() || scalarOps < atomic.LoadInt64(&physicalGPUMinScalarOps) || len(vectors) < 2 {
		return cpuDot(vectors, weights), "cpu-dot", nil
	}

	// Run CPU and GPU on disjoint rows concurrently. This is a physical workload
	// split only; row meaning and lane meaning remain opaque to Kernel.
	split := len(vectors) / 2
	cpuRows := vectors[:split]
	gpuRows := vectors[split:]
	out := make([]float32, len(vectors))
	var gpuOut []float32
	var gpuErr error
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		atomic.AddUint64(&r.gpuJobs, 1)
		gpuOut, gpuErr = gpu.Dot(gpuRows, weights)
	}()
	cpuOut := cpuDot(cpuRows, weights)
	wg.Wait()
	copy(out[:split], cpuOut)
	if gpuErr != nil || len(gpuOut) != len(gpuRows) {
		atomic.AddUint64(&r.gpuFallback, 1)
		copy(out[split:], cpuDot(gpuRows, weights))
		return out, "cpu-dot-gpu-fallback", nil
	}
	copy(out[split:], gpuOut)
	return out, "cpu+" + gpu.Name() + "-hybrid-dot", nil
}

func (r *parallelRuntime) Info() map[string]any {
	gpuInfo := map[string]any{"available": false, "backend": "none"}
	if globalPhysicalGPU != nil {
		gpuInfo = globalPhysicalGPU.Info()
	}
	backend := "cpu"
	if available, _ := gpuInfo["available"].(bool); available {
		backend = "cpu+gpu-hybrid"
	}
	return map[string]any{
		"backend":              backend,
		"physical_concurrency": atomic.LoadInt64(&r.concurrency),
		"gomaxprocs":           runtime.GOMAXPROCS(0),
		"jobs":                 atomic.LoadUint64(&r.jobs),
		"inflight":             atomic.LoadInt64(&r.inflight),
		"peak_parallel":        atomic.LoadInt64(&r.peak),
		"gpu_jobs":             atomic.LoadUint64(&r.gpuJobs),
		"gpu_fallbacks":        atomic.LoadUint64(&r.gpuFallback),
		"gpu_min_scalar_ops":   atomic.LoadInt64(&physicalGPUMinScalarOps),
		"gpu":                  gpuInfo,
	}
}

func parallelRuntimeJSON() string {
	b, _ := json.MarshalIndent(globalParallelRuntime.Info(), "", "  ")
	return string(b)
}

func physicalRuntimeInfo() map[string]any {
	return map[string]any{
		"parallel":               globalParallelRuntime.Info(),
		"speculative":            speculativeInfo(),
		"scheduler":              globalTxnScheduler.Info(),
		"event_handler_runs":     atomic.LoadUint64(&physicalEventHandlerRuns),
		"event_handler_failures": atomic.LoadUint64(&physicalEventHandlerFailures),
	}
}

func parallelSelfTest() error {
	vectors := [][]float32{{1, 2, 3, 4}, {4, 3, 2, 1}}
	weights := []float32{1, 1, 1, 1}
	got, _, err := globalParallelRuntime.Dot(vectors, weights)
	if err != nil {
		return err
	}
	if len(got) != 2 || got[0] != 10 || got[1] != 10 {
		return fmt.Errorf("parallel physical arithmetic self-test failed: values=%v", got)
	}
	wideVectors := [][]float32{{1, 1, 1, 1, 1, 1, 1, 1, 1}}
	wideWeights := []float32{1, 1, 1, 1, 1, 1, 1, 1, 1}
	wide, _, err := globalParallelRuntime.Dot(wideVectors, wideWeights)
	if err != nil || len(wide) != 1 || wide[0] != 9 {
		return fmt.Errorf("parallel arbitrary-lane self-test failed: values=%v err=%v", wide, err)
	}
	return nil
}
