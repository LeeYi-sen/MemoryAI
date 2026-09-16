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
//   - Persisted physical secondary indexes are used when available so startup
//     cost does not grow as O(total Memory records).
//   - Any result cap is a deterministic physical page (stable ID order), never
//     a "best candidate" Top-K.
//
// Score is retained in ActivationCandidate only for wire compatibility with
// older clients. Kernel always emits Score=0; cognitive scoring belongs to
// Memory-owned executable structures.
type SparseActivationRuntime struct {
	mu             sync.RWMutex
	postings       map[string]map[string]struct{}
	nodeFeature    map[string][]string
	fingerprint    map[string]uint64
	persistentBase bool
	nodes          int64
	queries        uint64
	candidates     uint64
	topK           int // legacy name: physical response cap, not cognitive Top-K
	maxState       int // compatibility field; zero means all scalar State fields are indexed
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
	return &SparseActivationRuntime{
		postings:    map[string]map[string]struct{}{},
		nodeFeature: map[string][]string{},
		fingerprint: map[string]uint64{},
		topK:        topK,
		maxState:    0,
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

// activationFeaturesForMemory indexes every scalar State field. The former
// max-field cutoff could make valid Memory state physically unreachable once a
// structure grew beyond the cutoff. The limit argument is retained only to
// avoid breaking older call sites; it is intentionally ignored.
func activationFeaturesForMemory(m *Memory, _ int) []string {
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
	for _, k := range keys {
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
	if _, existed := r.nodeFeature[m.ID]; !existed && !r.persistentBase {
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
	if !r.persistentBase {
		atomic.AddInt64(&r.nodes, -1)
	}
}

func (r *SparseActivationRuntime) reset(persistent bool, baseNodes int) {
	r.mu.Lock()
	r.postings = map[string]map[string]struct{}{}
	r.nodeFeature = map[string][]string{}
	r.fingerprint = map[string]uint64{}
	r.persistentBase = persistent
	r.mu.Unlock()
	atomic.StoreInt64(&r.nodes, int64(baseNodes))
}

func (r *SparseActivationRuntime) Build(e *Engine) error {
	root := fabricRootFor(e)
	if root == nil || root.manifest.Role != "core" {
		return fmt.Errorf("physical activation index requires core Memory Fabric")
	}

	// Validate every mounted Store. The Fabric adapter performs per-body legacy
	// fallback only for a body that lacks the persisted physical feature index;
	// one historical shard never forces a whole-Fabric startup scan.
	if _, err := root.storeHasPhysicalFeatureIndex(); err != nil {
		return err
	}

	// storePhysicalFeatureIDs already merges persisted postings with each body's
	// live dirty/new/deleted overlay. Keep one authoritative physical view instead
	// of copying dirty records into a second global overlay (which would double
	// FeatureHit accounting). Startup cost is O(shard count), not O(total Memory).
	r.reset(true, fabricMemoryCountFast(root))
	return nil
}

// RefreshDirty is retained for compatibility with historical callers. In the
// current persistent Fabric backend the Store adapter itself overlays live
// dirty/new/deleted records, so duplicating them here would double-count exact
// feature hits. Legacy in-memory mode still uses the historical overlay path.
func (r *SparseActivationRuntime) RefreshDirty(e *Engine) {
	if e == nil {
		return
	}
	r.mu.RLock()
	persistent := r.persistentBase
	r.mu.RUnlock()
	if persistent {
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

func activationShadowedIDs(e *Engine) map[string]bool {
	out := map[string]bool{}
	if e == nil {
		return out
	}
	e.dataMu.RLock()
	defer e.dataMu.RUnlock()
	for id := range e.dirtyIDs {
		out[id] = true
	}
	for id := range e.newIDs {
		out[id] = true
	}
	for id, deleted := range e.deletedIDs {
		if deleted {
			out[id] = true
		}
	}
	return out
}

// Activate returns an exact physical candidate set.
//
// The legacy topK parameter is treated only as a transport/resource cap.
// Candidates are sorted by stable physical identity, not FeatureHit or Score.
// Memory-owned executable structures are responsible for relevance ranking and
// choosing which candidate to activate cognitively.
func (r *SparseActivationRuntime) Activate(e *Engine, query string, topK int) (ActivationResult, error) {
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
	persistent := r.persistentBase
	r.mu.RUnlock()

	if persistent {
		for _, f := range qf {
			ids, err := e.storePhysicalFeatureIDs(f)
			if err != nil {
				return ActivationResult{}, err
			}
			for _, id := range ids {
				hit[id]++
			}
		}
	}

	// Only historical non-persistent mode uses the in-memory posting overlay.
	if !persistent {
		r.mu.RLock()
		for _, f := range qf {
			for id := range r.postings[f] {
				hit[id]++
			}
		}
		r.mu.RUnlock()
	}
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

	backend := "cpu-exact-index-legacy-overlay"
	if persistent {
		backend = "fabric-persisted-exact-index+per-body-live-overlay"
	}
	return ActivationResult{
		Query:          query,
		Candidates:     out,
		CandidateCount: count,
		IndexedNodes:   int(atomic.LoadInt64(&r.nodes)),
		Backend:        backend,
		ElapsedUS:      time.Since(start).Microseconds(),
	}, nil
}

func (r *SparseActivationRuntime) Info() map[string]any {
	r.mu.RLock()
	features := len(r.postings)
	overlayNodes := len(r.nodeFeature)
	persistent := r.persistentBase
	r.mu.RUnlock()
	q := atomic.LoadUint64(&r.queries)
	c := atomic.LoadUint64(&r.candidates)
	avg := float64(0)
	if q > 0 {
		avg = float64(c) / float64(q)
	}
	backend := "cpu-exact-index-legacy-overlay"
	if persistent {
		backend = "fabric-persisted-exact-index+per-body-live-overlay"
	}
	return map[string]any{
		"mode":                      "physical-exact-index",
		"indexed_nodes":             atomic.LoadInt64(&r.nodes),
		"overlay_nodes":             overlayNodes,
		"overlay_features":          features,
		"queries":                   q,
		"candidate_total":           c,
		"avg_candidates":            avg,
		"last_backend":              backend,
		"default_top_k":             r.topK,
		"physical_cap_only":         true,
		"selection_order":           "stable-memory-id",
		"state_field_limit":         0,
		"persisted_secondary_index": persistent,
		"legacy_full_scan_fallback": false,
		"legacy_fallback_scope":     "per-body-on-demand",
		"cognitive_ranking":         false,
		"qualification":             activationQualificationInfo(),
	}
}

func activationInfoJSON() string {
	b, _ := json.MarshalIndent(globalActivationRuntime.Info(), "", "  ")
	return string(b)
}
