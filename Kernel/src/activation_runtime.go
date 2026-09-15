package main

import (
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// SparseActivationRuntime is a physical exact-index accelerator.
//
// Architecture boundary:
//   - Kernel may index opaque physical fields and return exact candidate sets.
//   - Kernel must not decide semantic relevance, utility, confidence, priority,
//     or cognitive rank.
//   - Any result cap is therefore a deterministic physical page (stable ID
//     order), never a "best candidate" Top-K.
//
// Score is retained in ActivationCandidate only for wire compatibility with
// older clients. Kernel always emits Score=0; cognitive scoring belongs to
// Memory-owned executable structures.
type SparseActivationRuntime struct {
	mu          sync.RWMutex
	postings    map[string]map[string]struct{}
	nodeFeature map[string][]string
	fingerprint map[string]uint64
	nodes       int64
	queries     uint64
	candidates  uint64
	topK        int // legacy name: physical response cap, not cognitive Top-K
	maxState    int
}

type ActivationCandidate struct {
	ID         string  `json:"id"`
	Score      float32 `json:"score"`
	FeatureHit int     `json:"feature_hits"`
}

type ActivationResult struct {
	Query          string                `json:"query"`
	Candidates     []ActivationCandidate `json:"candidates"`
	CandidateCount int                   `json:"candidate_count"`
	IndexedNodes   int                   `json:"indexed_nodes"`
	Backend        string                `json:"backend"`
	ElapsedUS      int64                 `json:"elapsed_us"`
}

var globalActivationRuntime = newSparseActivationRuntime()

func newSparseActivationRuntime() *SparseActivationRuntime {
	topK := 64
	if s := os.Getenv("MEMORYAI_ACTIVATION_TOPK"); s != "" {
		if n, err := strconv.Atoi(s); err == nil && n > 0 {
			topK = n
		}
	}
	maxState := 128
	if s := os.Getenv("MEMORYAI_ACTIVATION_MAX_STATE_FIELDS"); s != "" {
		if n, err := strconv.Atoi(s); err == nil && n > 0 {
			maxState = n
		}
	}
	return &SparseActivationRuntime{
		postings:    map[string]map[string]struct{}{},
		nodeFeature: map[string][]string{},
		fingerprint: map[string]uint64{},
		topK:        topK,
		maxState:    maxState,
	}
}

func cloneActivationState(in map[string]any) map[string]any {
	if in == nil {
		return nil
	}
	out := make(map[string]any, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}

func exactIndexValue(v any) (string, bool) {
	switch x := v.(type) {
	case string:
		return x, true
	case float64, float32, int, int8, int16, int32, int64,
		uint, uint8, uint16, uint32, uint64, bool:
		return fmt.Sprint(x), true
	default:
		return "", false
	}
}

func activationFeaturesForMemory(m *Memory, limit int) []string {
	if m == nil {
		return nil
	}
	seen := map[string]bool{}
	add := func(x string) {
		x = strings.TrimSpace(x)
		if x == "" || seen[x] {
			return
		}
		seen[x] = true
	}
	add("id:" + m.ID)
	for _, t := range m.Tags {
		add("tag:" + t)
	}
	for _, t := range m.Trigger {
		add("trigger:" + t)
	}

	keys := make([]string, 0, len(m.State))
	for k := range m.State {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for i, k := range keys {
		if limit > 0 && i >= limit {
			break
		}
		add("state-key:" + k)
		if v, ok := exactIndexValue(m.State[k]); ok {
			add("state-kv:" + k + "=" + v)
		}
	}

	out := make([]string, 0, len(seen))
	for k := range seen {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func activationFingerprint(m *Memory, limit int) uint64 {
	if m == nil {
		return 0
	}
	var h uint64 = 1469598103934665603
	mix := func(s string) {
		for i := 0; i < len(s); i++ {
			h ^= uint64(s[i])
			h *= 1099511628211
		}
	}
	for _, f := range activationFeaturesForMemory(m, limit) {
		mix(f)
	}
	return h
}

func (r *SparseActivationRuntime) replaceNode(m *Memory) {
	if m == nil || m.ID == "" {
		return
	}
	features := activationFeaturesForMemory(m, r.maxState)
	fp := activationFingerprint(m, r.maxState)
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.fingerprint[m.ID] == fp {
		return
	}
	if old := r.nodeFeature[m.ID]; len(old) > 0 {
		for _, f := range old {
			if p := r.postings[f]; p != nil {
				delete(p, m.ID)
				if len(p) == 0 {
					delete(r.postings, f)
				}
			}
		}
	}
	for _, f := range features {
		p := r.postings[f]
		if p == nil {
			p = map[string]struct{}{}
			r.postings[f] = p
		}
		p[m.ID] = struct{}{}
	}
	if _, existed := r.nodeFeature[m.ID]; !existed {
		atomic.AddInt64(&r.nodes, 1)
	}
	r.nodeFeature[m.ID] = features
	r.fingerprint[m.ID] = fp
}

func (r *SparseActivationRuntime) removeNode(id string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	old, ok := r.nodeFeature[id]
	if !ok {
		return
	}
	for _, f := range old {
		if p := r.postings[f]; p != nil {
			delete(p, id)
			if len(p) == 0 {
				delete(r.postings, f)
			}
		}
	}
	delete(r.nodeFeature, id)
	delete(r.fingerprint, id)
	atomic.AddInt64(&r.nodes, -1)
}

func (r *SparseActivationRuntime) Build(e *Engine) error {
	if e == nil || e.manifest.Role != "core" {
		return fmt.Errorf("physical activation index requires core Memory.mem")
	}
	ids, err := e.store.AllIDs()
	if err != nil {
		return err
	}
	r.mu.Lock()
	r.postings = map[string]map[string]struct{}{}
	r.nodeFeature = map[string][]string{}
	r.fingerprint = map[string]uint64{}
	r.mu.Unlock()
	atomic.StoreInt64(&r.nodes, 0)
	for _, id := range ids {
		m, er := e.store.GetID(id)
		if er == nil {
			r.replaceNode(m)
		}
	}
	e.dataMu.RLock()
	cached := make([]*Memory, 0, len(e.cache))
	for _, m := range e.cache {
		cached = append(cached, m)
	}
	e.dataMu.RUnlock()
	for _, m := range cached {
		r.replaceNode(m)
	}
	return nil
}

// RefreshDirty incrementally keeps the physical exact index consistent with
// new/dirty/deleted Memories. It does not interpret Memory semantics.
func (r *SparseActivationRuntime) RefreshDirty(e *Engine) {
	if e == nil {
		return
	}
	e.dataMu.RLock()
	changed := make([]*Memory, 0, len(e.dirtyIDs)+len(e.newIDs))
	ids := map[string]bool{}
	for id := range e.dirtyIDs {
		ids[id] = true
	}
	for id := range e.newIDs {
		ids[id] = true
	}
	deleted := make([]string, 0, len(e.deletedIDs))
	for id, v := range e.deletedIDs {
		if v {
			deleted = append(deleted, id)
		}
	}
	for id := range ids {
		if m := e.cache[id]; m != nil {
			cp := *m
			cp.Tags = append([]string(nil), m.Tags...)
			cp.Trigger = append([]string(nil), m.Trigger...)
			cp.Parents = append([]string(nil), m.Parents...)
			cp.State = cloneActivationState(m.State)
			changed = append(changed, &cp)
		}
	}
	e.dataMu.RUnlock()
	for _, id := range deleted {
		r.removeNode(id)
	}
	for _, m := range changed {
		r.replaceNode(m)
	}
}

func queryActivationFeatures(q string, _ int) []string {
	raw := strings.TrimSpace(q)
	if raw == "" {
		return nil
	}
	prefixes := []string{"id:", "tag:", "trigger:", "state-key:", "state-kv:"}
	for _, p := range prefixes {
		if strings.HasPrefix(raw, p) {
			return []string{raw}
		}
	}
	return []string{"id:" + raw, "tag:" + raw, "trigger:" + raw}
}

// Activate returns an exact physical candidate set.
//
// The legacy topK parameter is treated only as a transport/resource cap.
// Candidates are sorted by stable physical identity, not FeatureHit or Score.
// Memory-owned executable structures are responsible for relevance ranking and
// choosing which candidate to activate cognitively.
func (r *SparseActivationRuntime) Activate(_ *Engine, query string, topK int) (ActivationResult, error) {
	start := time.Now()
	atomic.AddUint64(&r.queries, 1)
	if topK <= 0 {
		topK = r.topK
	}
	qf := queryActivationFeatures(query, r.maxState)
	if len(qf) == 0 {
		return ActivationResult{
			Query:        query,
			IndexedNodes: int(atomic.LoadInt64(&r.nodes)),
			Backend:      "none",
			ElapsedUS:    time.Since(start).Microseconds(),
		}, nil
	}

	hit := map[string]int{}
	r.mu.RLock()
	for _, f := range qf {
		for id := range r.postings[f] {
			hit[id]++
		}
	}
	r.mu.RUnlock()
	atomic.AddUint64(&r.candidates, uint64(len(hit)))

	ids := make([]string, 0, len(hit))
	for id := range hit {
		ids = append(ids, id)
	}
	sort.Strings(ids)

	count := len(ids)
	if topK > 0 && topK < len(ids) {
		ids = ids[:topK]
	}

	out := make([]ActivationCandidate, 0, len(ids))
	for _, id := range ids {
		out = append(out, ActivationCandidate{
			ID:         id,
			Score:      0,
			FeatureHit: hit[id],
		})
	}

	return ActivationResult{
		Query:          query,
		Candidates:     out,
		CandidateCount: count,
		IndexedNodes:   int(atomic.LoadInt64(&r.nodes)),
		Backend:        "cpu-exact-index-stable-page",
		ElapsedUS:      time.Since(start).Microseconds(),
	}, nil
}

func (r *SparseActivationRuntime) Info() map[string]any {
	r.mu.RLock()
	features := len(r.postings)
	r.mu.RUnlock()
	q := atomic.LoadUint64(&r.queries)
	c := atomic.LoadUint64(&r.candidates)
	avg := float64(0)
	if q > 0 {
		avg = float64(c) / float64(q)
	}
	return map[string]any{
		"mode":              "physical-exact-index",
		"indexed_nodes":     atomic.LoadInt64(&r.nodes),
		"features":          features,
		"queries":           q,
		"candidate_total":   c,
		"avg_candidates":    avg,
		"last_backend":      "cpu-exact-index-stable-page",
		"default_top_k":     r.topK,
		"physical_cap_only": true,
		"selection_order":   "stable-memory-id",
		"cognitive_ranking": false,
		"qualification":     activationQualificationInfo(),
	}
}

func activationInfoJSON() string {
	b, _ := json.MarshalIndent(globalActivationRuntime.Info(), "", "  ")
	return string(b)
}
