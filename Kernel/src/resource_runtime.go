package main

import (
	"fmt"
	"runtime"
	"time"
)

// resourceSample contains only physical process/runtime telemetry. It carries no
// semantic utility, confidence, priority or other cognitive interpretation.
type resourceSample struct {
	at         time.Time
	totalAlloc uint64
	heapAlloc  uint64
}

func sampleResources() resourceSample {
	var ms runtime.MemStats
	runtime.ReadMemStats(&ms)
	return resourceSample{at: time.Now(), totalAlloc: ms.TotalAlloc, heapAlloc: ms.HeapAlloc}
}

func checkResourceBudget(b ResourceBudget, start resourceSample, ops, writes, events int) error {
	if b.MaxOps > 0 && ops > b.MaxOps {
		return fmt.Errorf("physical resource budget exceeded: ops=%d max=%d", ops, b.MaxOps)
	}
	if b.MaxMemoryWrites > 0 && writes > b.MaxMemoryWrites {
		return fmt.Errorf("physical resource budget exceeded: memory_writes=%d max=%d", writes, b.MaxMemoryWrites)
	}
	if b.MaxEvents > 0 && events > b.MaxEvents {
		return fmt.Errorf("physical resource budget exceeded: events=%d max=%d", events, b.MaxEvents)
	}
	if b.MaxCPUUS > 0 && time.Since(start.at).Microseconds() > b.MaxCPUUS {
		// Go does not expose portable per-goroutine CPU time. The immutable
		// sandbox therefore treats this field as an execution-time ceiling.
		return fmt.Errorf("physical resource budget exceeded: execution_us>%d", b.MaxCPUUS)
	}
	if b.MaxAllocBytes > 0 || b.MaxHeapGrowthBytes > 0 {
		var ms runtime.MemStats
		runtime.ReadMemStats(&ms)
		if b.MaxAllocBytes > 0 {
			delta := int64(0)
			if ms.TotalAlloc >= start.totalAlloc {
				delta = int64(ms.TotalAlloc - start.totalAlloc)
			}
			if delta > b.MaxAllocBytes {
				return fmt.Errorf("physical resource budget exceeded: alloc_bytes=%d max=%d", delta, b.MaxAllocBytes)
			}
		}
		if b.MaxHeapGrowthBytes > 0 {
			delta := int64(ms.HeapAlloc) - int64(start.heapAlloc)
			if delta < 0 {
				delta = 0
			}
			if delta > b.MaxHeapGrowthBytes {
				return fmt.Errorf("physical resource budget exceeded: heap_growth_bytes=%d max=%d", delta, b.MaxHeapGrowthBytes)
			}
		}
	}
	return nil
}
