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

type frameResourceScope struct {
	maxMemoryWrites int
	maxEvents       int
	memoryWrites    int
	events          int
}

func sampleResources() resourceSample {
	var ms runtime.MemStats
	runtime.ReadMemStats(&ms)
	return resourceSample{at: time.Now(), totalAlloc: ms.TotalAlloc, heapAlloc: ms.HeapAlloc}
}

func checkResourceBudget(b ResourceBudget, start resourceSample, ops int) error {
	if b.MaxOps > 0 && ops > b.MaxOps {
		return fmt.Errorf("physical resource budget exceeded: ops=%d max=%d", ops, b.MaxOps)
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

func enterFrameResourceScope(f *Frame, b ResourceBudget) (*frameResourceScope, func()) {
	scope := &frameResourceScope{
		maxMemoryWrites: b.MaxMemoryWrites,
		maxEvents:       b.MaxEvents,
	}
	if f == nil {
		return scope, func() {}
	}
	prev := f.resourceScope
	f.resourceScope = scope
	return scope, func() {
		f.resourceScope = prev
	}
}

func primitiveMayMemoryWrite(code string) bool {
	switch code {
	case "state_list_append",
		"state_list_unique_append",
		"state_num_add",
		"state_set",
		"program_import",
		"program_set_field",
		"program_set_var_ref",
		"program_delete",
		"program_insert_from",
		"program_replace_from",
		"memory_new",
		"memory_copy",
		"memory_history_append",
		"memory_tag_add",
		"memory_tag_remove",
		"memory_delete",
		"memory_import_json":
		return true
	default:
		return false
	}
}

func primitiveMayEmitEvent(code string) bool {
	return code == "emit_event"
}

func reservePrimitiveResourceBudget(f *Frame, code string) error {
	if f == nil || f.resourceScope == nil {
		return nil
	}
	scope := f.resourceScope
	if primitiveMayMemoryWrite(code) {
		next := scope.memoryWrites + 1
		if scope.maxMemoryWrites > 0 && next > scope.maxMemoryWrites {
			return fmt.Errorf("physical resource budget exceeded: memory_writes=%d max=%d", next, scope.maxMemoryWrites)
		}
		scope.memoryWrites = next
	}
	if primitiveMayEmitEvent(code) {
		next := scope.events + 1
		if scope.maxEvents > 0 && next > scope.maxEvents {
			return fmt.Errorf("physical resource budget exceeded: events=%d max=%d", next, scope.maxEvents)
		}
		scope.events = next
	}
	return nil
}

func checkResourceBudgetBeforePrimitive(b ResourceBudget, start resourceSample, opsExecuted int, f *Frame, code string) error {
	if b.MaxOps > 0 && opsExecuted+1 > b.MaxOps {
		return fmt.Errorf("physical resource budget exceeded: ops=%d max=%d", opsExecuted+1, b.MaxOps)
	}
	if b.MaxCPUUS > 0 && time.Since(start.at).Microseconds() > b.MaxCPUUS {
		return fmt.Errorf("physical resource budget exceeded: execution_us>%d", b.MaxCPUUS)
	}
	return reservePrimitiveResourceBudget(f, code)
}
