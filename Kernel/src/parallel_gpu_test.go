package main

import (
	"errors"
	"strings"
	"sync/atomic"
	"testing"
)

type fakePhysicalGPU struct {
	available bool
	fail      bool
	calls     int64
}

func (f *fakePhysicalGPU) Name() string    { return "fake-gpu" }
func (f *fakePhysicalGPU) Available() bool { return f.available }
func (f *fakePhysicalGPU) Info() map[string]any {
	return map[string]any{"available": f.available, "backend": "fake-gpu"}
}
func (f *fakePhysicalGPU) Dot(vectors [][]float32, weights []float32) ([]float32, error) {
	atomic.AddInt64(&f.calls, 1)
	if f.fail {
		return nil, errors.New("forced gpu failure")
	}
	out := make([]float32, len(vectors))
	for i := range vectors {
		for lane := range weights {
			out[i] += vectors[i][lane] * weights[lane]
		}
	}
	return out, nil
}

func TestParallelDotUsesPhysicalHybridBackendWithoutSemanticLanes(t *testing.T) {
	oldGPU := globalPhysicalGPU
	oldThreshold := atomic.LoadInt64(&physicalGPUMinScalarOps)
	defer func() {
		globalPhysicalGPU = oldGPU
		atomic.StoreInt64(&physicalGPUMinScalarOps, oldThreshold)
	}()
	fake := &fakePhysicalGPU{available: true}
	globalPhysicalGPU = fake
	atomic.StoreInt64(&physicalGPUMinScalarOps, 1)

	vectors := [][]float32{{1, 2, 3}, {4, 5, 6}, {7, 8, 9}, {1, 1, 1}}
	weights := []float32{2, 3, 4}
	got, backend, err := globalParallelRuntime.Dot(vectors, weights)
	if err != nil {
		t.Fatal(err)
	}
	want := []float32{20, 47, 74, 9}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("row %d: got %v want %v", i, got[i], want[i])
		}
	}
	if !strings.Contains(backend, "hybrid-dot") || atomic.LoadInt64(&fake.calls) != 1 {
		t.Fatalf("expected one physical hybrid GPU call, backend=%q calls=%d", backend, fake.calls)
	}
}

func TestParallelDotFallsBackToCPUWhenPhysicalGPUFails(t *testing.T) {
	oldGPU := globalPhysicalGPU
	oldThreshold := atomic.LoadInt64(&physicalGPUMinScalarOps)
	defer func() {
		globalPhysicalGPU = oldGPU
		atomic.StoreInt64(&physicalGPUMinScalarOps, oldThreshold)
	}()
	fake := &fakePhysicalGPU{available: true, fail: true}
	globalPhysicalGPU = fake
	atomic.StoreInt64(&physicalGPUMinScalarOps, 1)

	got, backend, err := globalParallelRuntime.Dot(
		[][]float32{{1, 2}, {3, 4}}, []float32{5, 6},
	)
	if err != nil {
		t.Fatal(err)
	}
	if backend != "cpu-dot-gpu-fallback" || len(got) != 2 || got[0] != 17 || got[1] != 39 {
		t.Fatalf("unexpected fallback: backend=%q got=%v", backend, got)
	}
}
