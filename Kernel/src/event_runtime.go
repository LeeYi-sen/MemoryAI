package main

import (
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// PhysicalEvent is transport metadata only. Meanings, goals and responses are
// encoded by executable Memory structures that subscribe through exact Trigger
// predicates.
type PhysicalEvent struct {
	ID          string            `json:"id"`
	Name        string            `json:"name"`
	Subject     string            `json:"subject,omitempty"`
	Vars        map[string]string `json:"vars,omitempty"`
	CreatedNano int64             `json:"created_nano"`
	Depth       int               `json:"depth,omitempty"`
}

type eventRuntimeStats struct {
	mu          sync.Mutex
	indexReady  bool
	indexErr    error
	emitted     uint64
	dispatched  uint64
	handlerRuns uint64
}

var physicalEventSequence uint64
var physicalEventHandlerRuns uint64
var physicalEventHandlerFailures uint64

const hardEventDispatchBatchHandlers = 256

func eventDispatchBatchSize(total, workers int) int {
	if total <= 0 {
		return 1
	}
	if workers < 1 {
		workers = 1
	}
	batch := workers * 4
	if batch < workers {
		batch = workers
	}
	if batch > hardEventDispatchBatchHandlers {
		batch = hardEventDispatchBatchHandlers
	}
	if batch > total {
		batch = total
	}
	return batch
}

func newPhysicalEvent(name, subject string, vars map[string]string) PhysicalEvent {
	copyVars := map[string]string{}
	for k, v := range vars {
		copyVars[k] = v
	}
	n := atomic.AddUint64(&physicalEventSequence, 1)
	now := time.Now().UnixNano()
	return PhysicalEvent{
		ID:          fmt.Sprintf("event-%d-%d", now, n),
		Name:        strings.TrimSpace(name),
		Subject:     strings.TrimSpace(subject),
		Vars:        copyVars,
		CreatedNano: now,
	}
}

func eventFabricRoot(e *Engine) *Engine {
	root := fabricRootFor(e)
	if root != nil {
		return root
	}
	return e
}

func (e *Engine) ensureEventPhysicalIndex() error {
	if e == nil {
		return fmt.Errorf("physical event index requires engine")
	}
	root := eventFabricRoot(e)
	if root != e {
		return root.ensureEventPhysicalIndex()
	}
	if e.speculative {
		if origin, ok := speculativeOriginEngine(e); ok && origin != nil {
			return eventFabricRoot(origin).ensureEventPhysicalIndex()
		}
		return nil
	}
	e.eventStats.mu.Lock()
	defer e.eventStats.mu.Unlock()
	if e.eventStats.indexReady {
		return e.eventStats.indexErr
	}
	e.eventStats.indexErr = globalActivationRuntime.Build(e)
	e.eventStats.indexReady = true
	return e.eventStats.indexErr
}

func memoryHasTag(m *Memory, tag string) bool {
	if m == nil {
		return false
	}
	for _, t := range m.Tags {
		if t == tag {
			return true
		}
	}
	return false
}

func eventSubjectTags(e *Engine, subject string) map[string]bool {
	out := map[string]bool{}
	if e == nil || subject == "" {
		return out
	}
	var m *Memory
	var err error
	if e.speculative {
		m, err = e.resolveIDLocal(subject)
	} else {
		root := eventFabricRoot(e)
		_, m, err = root.resolveLocalFabricMemory(subject)
	}
	if err != nil || m == nil {
		return out
	}
	for _, t := range m.Tags {
		out[t] = true
	}
	return out
}

func memoryMatchesPhysicalEvent(m *Memory, ev PhysicalEvent, subjectTags map[string]bool) bool {
	if m == nil || len(m.Program) == 0 || len(m.Trigger) == 0 {
		return false
	}
	for _, raw := range m.Trigger {
		trigger := strings.TrimSpace(raw)
		switch {
		case strings.HasPrefix(trigger, "event:"):
			if strings.TrimPrefix(trigger, "event:") != ev.Name {
				return false
			}
		case strings.HasPrefix(trigger, "subject_tag:"):
			if !subjectTags[strings.TrimPrefix(trigger, "subject_tag:")] {
				return false
			}
		case strings.HasPrefix(trigger, "subject_id:"):
			if strings.TrimPrefix(trigger, "subject_id:") != ev.Subject {
				return false
			}
		default:
			// Unknown trigger vocabularies are never guessed by Kernel. A future
			// Memory-owned adapter can lower them into exact physical predicates.
			return false
		}
	}
	return true
}

func (e *Engine) eventCandidateIDs(ev PhysicalEvent, subjectTags map[string]bool) ([]string, error) {
	if e == nil {
		return nil, fmt.Errorf("physical event candidates require engine")
	}
	keys := []string{"event:" + ev.Name}
	if ev.Subject != "" {
		keys = append(keys, "subject_id:"+ev.Subject)
	}
	for tag := range subjectTags {
		keys = append(keys, "subject_tag:"+tag)
	}
	sort.Strings(keys)

	root := eventFabricRoot(e)
	if err := root.ensureEventPhysicalIndex(); err != nil {
		return nil, err
	}
	queryEngine := root
	if e.speculative {
		// ExactFeatureIDs on the speculative Engine reads the same root Fabric
		// indexes through the pinned snapshot path and overlays private mutations.
		queryEngine = e
	}

	ids := map[string]struct{}{}
	for _, key := range keys {
		matches, err := globalActivationRuntime.ExactFeatureIDs(queryEngine, "trigger:"+key)
		if err != nil {
			return nil, err
		}
		for _, id := range matches {
			ids[id] = struct{}{}
		}
	}

	out := make([]string, 0, len(ids))
	for id := range ids {
		out = append(out, id)
	}
	sort.Strings(out)
	return out, nil
}

func applyPhysicalEventFrame(f *Frame, ev PhysicalEvent) {
	f.Vars["__event"] = ev.Name
	f.Vars["__event_id"] = ev.ID
	f.Vars["__subject"] = ev.Subject
	for k, v := range ev.Vars {
		if !strings.HasPrefix(k, "__") {
			f.Vars[k] = v
			f.Vars["__event."+k] = v
		}
	}
}

func equalFrameList(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func mergeIndependentEventFrame(dst, base, branch *Frame) {
	for k, v := range branch.Vars {
		if old, ok := base.Vars[k]; !ok || old != v {
			dst.Vars[k] = v
		}
	}
	for k, values := range branch.Lists {
		if !equalFrameList(values, base.Lists[k]) {
			dst.Lists[k] = append([]string(nil), values...)
		}
	}
	if len(branch.Output) > len(base.Output) {
		dst.Output = append(dst.Output, branch.Output[len(base.Output):]...)
	}
	if len(branch.Events) > 0 {
		dst.Events = append(dst.Events, branch.Events...)
	}
	dst.memoryWrites += branch.memoryWrites
	dst.eventCount += branch.eventCount
}

func (e *Engine) dispatchPhysicalEvent(f *Frame, ev PhysicalEvent) error {
	if strings.TrimSpace(ev.Name) == "" {
		return fmt.Errorf("physical event name required")
	}
	if ev.Depth > 64 {
		return fmt.Errorf("physical event depth exceeded: %d", ev.Depth)
	}
	root := eventFabricRoot(e)
	subjectTags := eventSubjectTags(e, ev.Subject)
	ids, err := e.eventCandidateIDs(ev, subjectTags)
	if err != nil {
		return err
	}
	atomic.AddUint64(&root.eventStats.dispatched, 1)
	applyPhysicalEventFrame(f, ev)

	matched := make([]string, 0, len(ids))
	for _, id := range ids {
		var handler *Memory
		if e.speculative {
			handler, err = e.resolveIDLocal(id)
		} else {
			_, handler, err = root.resolveLocalFabricMemory(id)
		}
		if err == nil && memoryMatchesPhysicalEvent(handler, ev, subjectTags) {
			matched = append(matched, id)
		}
	}
	if len(matched) == 0 {
		return nil
	}

	// Speculative/canonical execution already owns an outer transaction lane.
	// Keep nested dispatch serial to avoid re-entering physical commit machinery,
	// but isolate failures so one Memory cannot suppress unrelated handlers.
	if e.speculative || f.Vars["__txn_canonical"] == "1" || physicalConcurrency(0) <= 1 || len(matched) == 1 {
		var errs []error
		for _, id := range matched {
			// A sibling Memory must always observe the physical event that selected it.
			// Nested emitted events may reuse the same Frame, so restore the parent
			// event metadata before every serial handler without interpreting payload.
			applyPhysicalEventFrame(f, ev)
			atomic.AddUint64(&root.eventStats.handlerRuns, 1)
			atomic.AddUint64(&physicalEventHandlerRuns, 1)
			var runErr error
			if e.speculative {
				runErr = e.run(id, f)
			} else if f.Vars["__txn_canonical"] == "1" {
				if _, _, er := root.resolveLocalFabricMemory(id); er != nil {
					runErr = er
				} else {
					runErr = root.run(id, f)
				}
			} else {
				runErr = globalTxnScheduler.run(root, id, f)
			}
			if runErr != nil {
				atomic.AddUint64(&physicalEventHandlerFailures, 1)
				errs = append(errs, fmt.Errorf("event handler %s: %w", id, runErr))
			}
		}
		return errors.Join(errs...)
	}

	// Top-level independent handlers receive identical physical event frames and
	// execute through the existing speculative scheduler. Process a bounded
	// physical batch at a time so queued goroutines/results never scale with the
	// total exact trigger set. matched remains stable Memory-ID order.
	base := cloneFrame(f)
	type handlerResult struct {
		frame *Frame
		err   error
	}
	workers := physicalConcurrency(0)
	batchCap := eventDispatchBatchSize(len(matched), workers)
	var errs []error
	for start := 0; start < len(matched); start += batchCap {
		end := start + batchCap
		if end > len(matched) {
			end = len(matched)
		}
		batchLen := end - start
		results := make([]handlerResult, batchLen)
		parallelCPUFor(batchLen, workers, func(offset int) {
			memoryID := matched[start+offset]
			branch := cloneFrame(base)
			atomic.AddUint64(&root.eventStats.handlerRuns, 1)
			atomic.AddUint64(&physicalEventHandlerRuns, 1)
			runErr := globalTxnScheduler.run(root, memoryID, branch)
			if runErr != nil {
				atomic.AddUint64(&physicalEventHandlerFailures, 1)
			}
			results[offset] = handlerResult{frame: branch, err: runErr}
		})
		for offset := 0; offset < batchLen; offset++ {
			id := matched[start+offset]
			if results[offset].frame != nil {
				mergeIndependentEventFrame(f, base, results[offset].frame)
			}
			if results[offset].err != nil {
				errs = append(errs, fmt.Errorf("event handler %s: %w", id, results[offset].err))
			}
		}
	}
	return errors.Join(errs...)
}

func (e *Engine) enqueueEvent(f *Frame, ev PhysicalEvent) error {
	if f == nil {
		return fmt.Errorf("physical event frame required")
	}
	// Physical event metadata is stack-scoped. Memory handlers share an
	// ephemeral Frame, but a nested event must not replace its caller's
	// __event/__subject namespace after returning. Cognitive payload/output
	// variables remain untouched; only transport metadata is restored.
	parentEventMeta := map[string]string{}
	hadParentEvent := f.Vars["__event"] != "" || f.Vars["__event_id"] != ""
	if hadParentEvent {
		for k, v := range f.Vars {
			if k == "__event" || k == "__event_id" || k == "__subject" || strings.HasPrefix(k, "__event.") {
				parentEventMeta[k] = v
			}
		}
	}
	if parentDepth := f.Vars["__event_depth"]; parentDepth != "" {
		var d int
		_, _ = fmt.Sscan(parentDepth, &d)
		ev.Depth = d + 1
	}
	f.Events = append(f.Events, ev)
	f.eventCount++
	root := eventFabricRoot(e)
	atomic.AddUint64(&root.eventStats.emitted, 1)
	oldDepth := f.Vars["__event_depth"]
	f.Vars["__event_depth"] = fmt.Sprint(ev.Depth)
	err := e.dispatchPhysicalEvent(f, ev)
	if oldDepth == "" {
		delete(f.Vars, "__event_depth")
	} else {
		f.Vars["__event_depth"] = oldDepth
	}
	if hadParentEvent {
		for k := range f.Vars {
			if k == "__event" || k == "__event_id" || k == "__subject" || strings.HasPrefix(k, "__event.") {
				delete(f.Vars, k)
			}
		}
		for k, v := range parentEventMeta {
			f.Vars[k] = v
			if strings.HasPrefix(k, "__event.") {
				f.Vars[strings.TrimPrefix(k, "__event.")] = v
			}
		}
	} else {
		// Keep the top-level physical event as the visible transport context.
		applyPhysicalEventFrame(f, ev)
	}
	return err
}

func (e *Engine) fireEvent(name, subject string, f *Frame) error {
	if f == nil {
		f = newFrame()
	}
	vars := map[string]string{}
	for k, v := range f.Vars {
		if !strings.HasPrefix(k, "__") {
			vars[k] = v
		}
	}
	ev := newPhysicalEvent(name, subject, vars)
	if strings.TrimSpace(name) == "mesh.shared.proposal" {
		return executeMeshProposalEventOnce(e, f, ev, func() error {
			return e.enqueueEvent(f, ev)
		})
	}
	return e.enqueueEvent(f, ev)
}
