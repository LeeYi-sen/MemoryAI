package main

import (
	"encoding/json"
	"fmt"
	"runtime"
	"sync"
	"sync/atomic"
)

// parallelRuntime is a physical execution accelerator only. It never chooses
// which executable Memory should run, assigns semantic priority, or interprets
// vector dimensions.
type parallelRuntime struct {
	concurrency int64
	jobs        uint64
	peak        int64
	inflight    int64
}

var globalParallelRuntime = newParallelRuntime()

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

// Dot is dimension-agnostic physical arithmetic. Kernel knows only that each
// row has the same number of scalar lanes as weights; lane meaning, weighting,
// interpretation and any notion of a semantic score belong entirely to Memory.
func (r *parallelRuntime) Dot(vectors [][]float32, weights []float32) ([]float32, string, error) {
	if len(weights) == 0 {
		return nil, "", fmt.Errorf("parallel dot requires at least one physical lane")
	}
	for i := range vectors {
		if len(vectors[i]) != len(weights) {
			return nil, "", fmt.Errorf("parallel dot lane mismatch at row %d: %d != %d", i, len(vectors[i]), len(weights))
		}
	}
	out := make([]float32, len(vectors))
	parallelCPUFor(len(vectors), 0, func(i int) {
		var sum float32
		for lane := range weights {
			sum += vectors[i][lane] * weights[lane]
		}
		out[i] = sum
	})
	return out, "cpu-dot", nil
}

func (r *parallelRuntime) Info() map[string]any {
	return map[string]any{
		"backend":              "cpu",
		"physical_concurrency": atomic.LoadInt64(&r.concurrency),
		"gomaxprocs":           runtime.GOMAXPROCS(0),
		"jobs":                 atomic.LoadUint64(&r.jobs),
		"inflight":             atomic.LoadInt64(&r.inflight),
		"peak_parallel":        atomic.LoadInt64(&r.peak),
	}
}

func parallelRuntimeJSON() string {
	b, _ := json.MarshalIndent(globalParallelRuntime.Info(), "", "  ")
	return string(b)
}

func physicalRuntimeInfo() map[string]any {
	return map[string]any{
		"parallel":    globalParallelRuntime.Info(),
		"speculative": speculativeInfo(),
		"scheduler":   globalTxnScheduler.Info(),
	}
}

func parallelSelfTest() error {
	vectors := [][]float32{{1, 2, 3, 4}, {4, 3, 2, 1}}
	weights := []float32{1, 1, 1, 1}
	got, backend, err := globalParallelRuntime.Dot(vectors, weights)
	if err != nil {
		return err
	}
	if backend != "cpu-dot" || len(got) != 2 || got[0] != 10 || got[1] != 10 {
		return fmt.Errorf("parallel physical arithmetic self-test failed: backend=%s values=%v", backend, got)
	}
	wideVectors := [][]float32{{1, 1, 1, 1, 1, 1, 1, 1, 1}}
	wideWeights := []float32{1, 1, 1, 1, 1, 1, 1, 1, 1}
	wide, _, err := globalParallelRuntime.Dot(wideVectors, wideWeights)
	if err != nil || len(wide) != 1 || wide[0] != 9 {
		return fmt.Errorf("parallel arbitrary-lane self-test failed: values=%v err=%v", wide, err)
	}
	return nil
}
