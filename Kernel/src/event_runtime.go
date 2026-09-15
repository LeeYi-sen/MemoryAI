package main

import (
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

func (e *Engine) ensureEventPhysicalIndex() error {
	if e.speculative {
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
	if subject == "" {
		return out
	}
	m, err := e.resolve(subject)
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
	keys := []string{"event:" + ev.Name}
	if ev.Subject != "" {
		keys = append(keys, "subject_id:"+ev.Subject)
	}
	for tag := range subjectTags {
		keys = append(keys, "subject_tag:"+tag)
	}
	sort.Strings(keys)

	ids := map[string]struct{}{}
	if e.speculative {
		// Speculative engines already contain a bounded private snapshot. Scanning
		// that private cache does not reintroduce a production Memory-wide scan.
		e.dataMu.RLock()
		for id, m := range e.cache {
			if e.deletedIDs[id] || m == nil {
				continue
			}
			for _, trigger := range m.Trigger {
				for _, key := range keys {
					if strings.TrimSpace(trigger) == key {
						ids[id] = struct{}{}
					}
				}
			}
		}
		e.dataMu.RUnlock()
	} else {
		if err := e.ensureEventPhysicalIndex(); err != nil {
			return nil, err
		}
		for _, key := range keys {
			matches, err := globalActivationRuntime.ExactFeatureIDs(e, "trigger:"+key)
			if err != nil {
				return nil, err
			}
			for _, id := range matches {
				ids[id] = struct{}{}
			}
		}
	}

	out := make([]string, 0, len(ids))
	for id := range ids {
		out = append(out, id)
	}
	sort.Strings(out)
	return out, nil
}

func (e *Engine) dispatchPhysicalEvent(f *Frame, ev PhysicalEvent) error {
	if strings.TrimSpace(ev.Name) == "" {
		return fmt.Errorf("physical event name required")
	}
	if ev.Depth > 64 {
		return fmt.Errorf("physical event depth exceeded: %d", ev.Depth)
	}
	subjectTags := eventSubjectTags(e, ev.Subject)
	ids, err := e.eventCandidateIDs(ev, subjectTags)
	if err != nil {
		return err
	}
	atomic.AddUint64(&e.eventStats.dispatched, 1)

	for _, id := range ids {
		m, err := e.resolveIDLocal(id)
		if err != nil || !memoryMatchesPhysicalEvent(m, ev, subjectTags) {
			continue
		}
		f.Vars["__event"] = ev.Name
		f.Vars["__event_id"] = ev.ID
		f.Vars["__subject"] = ev.Subject
		for k, v := range ev.Vars {
			if !strings.HasPrefix(k, "__") {
				f.Vars[k] = v
			}
		}
		atomic.AddUint64(&e.eventStats.handlerRuns, 1)
		if err := e.run(id, f); err != nil {
			return err
		}
	}
	return nil
}

func (e *Engine) enqueueEvent(f *Frame, ev PhysicalEvent) error {
	if f == nil {
		return fmt.Errorf("physical event frame required")
	}
	if parentDepth := f.Vars["__event_depth"]; parentDepth != "" {
		var d int
		_, _ = fmt.Sscan(parentDepth, &d)
		ev.Depth = d + 1
	}
	f.Events = append(f.Events, ev)
	f.eventCount++
	atomic.AddUint64(&e.eventStats.emitted, 1)
	oldDepth := f.Vars["__event_depth"]
	f.Vars["__event_depth"] = fmt.Sprint(ev.Depth)
	err := e.dispatchPhysicalEvent(f, ev)
	if oldDepth == "" {
		delete(f.Vars, "__event_depth")
	} else {
		f.Vars["__event_depth"] = oldDepth
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
	return e.enqueueEvent(f, newPhysicalEvent(name, subject, vars))
}
