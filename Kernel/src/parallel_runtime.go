package main

import (
	"encoding/json"
	"fmt"
	"runtime"
	"sync"
	"sync/atomic"
)

// parallelRuntime is a physical execution accelerator only. It never chooses
// cognitive work, assigns semantic priority, or interprets vector dimensions.
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

func setCognitionConcurrency(n int) {
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

// Score6 is a generic six-lane dot-product primitive. The meaning and weights
// of every lane are supplied by Memory; Kernel performs arithmetic only.
func (r *parallelRuntime) Score6(vectors [][6]float32, weights [6]float32) ([]float32, string, error) {
	out := make([]float32, len(vectors))
	parallelCPUFor(len(vectors), 0, func(i int) {
		var v float32
		for lane := 0; lane < 6; lane++ {
			v += vectors[i][lane] * weights[lane]
		}
		out[i] = v
	})
	return out, "cpu-dot6", nil
}

func (r *parallelRuntime) Info() map[string]any {
	return map[string]any{
		"backend":             "cpu",
		"physical_concurrency": atomic.LoadInt64(&r.concurrency),
		"gomaxprocs":           runtime.GOMAXPROCS(0),
		"jobs":                 atomic.LoadUint64(&r.jobs),
		"inflight":             atomic.LoadInt64(&r.inflight),
		"peak_parallel":        atomic.LoadInt64(&r.peak),
		"cognitive_selection":  false,
	}
}

func parallelRuntimeJSON() string {
	b, _ := json.MarshalIndent(globalParallelRuntime.Info(), "", "  ")
	return string(b)
}

func cognitionInfo() map[string]any {
	return map[string]any{
		"parallel":    globalParallelRuntime.Info(),
		"speculative": speculativeInfo(),
		"scheduler":   globalTxnScheduler.Info(),
	}
}

func parallelSelfTest() error {
	vectors := [][6]float32{{1, 2, 3, 4, 5, 6}, {6, 5, 4, 3, 2, 1}}
	weights := [6]float32{1, 1, 1, 1, 1, 1}
	got, backend, err := globalParallelRuntime.Score6(vectors, weights)
	if err != nil {
		return err
	}
	if backend != "cpu-dot6" || len(got) != 2 || got[0] != 21 || got[1] != 21 {
		return fmt.Errorf("parallel physical arithmetic self-test failed: backend=%s values=%v", backend, got)
	}
	return nil
}
