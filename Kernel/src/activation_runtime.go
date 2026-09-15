package main

import (
	"encoding/json"
	"fmt"
	"math"
	"os"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
	"unicode"
)

// SparseActivationRuntime is an engineering accelerator only. It never mutates
// cognition semantics and never becomes another AI. It maintains a sparse,
// local inverted index over the single executable Memory.mem core so candidate
// discovery is proportional to the stimulated neighborhood rather than the
// total number of memories.
type SparseActivationRuntime struct {
	mu          sync.RWMutex
	postings    map[string]map[string]struct{}
	nodeFeature map[string][]string
	fingerprint map[string]uint64
	nodes       int64
	queries     uint64
	candidates  uint64
	batches     uint64
	scored      uint64
	lastBackend atomic.Value // string
	topK        int
	maxTokens   int
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
	maxTokens := 128
	if s := os.Getenv("MEMORYAI_ACTIVATION_MAX_TOKENS"); s != "" {
		if n, err := strconv.Atoi(s); err == nil && n > 0 {
			maxTokens = n
		}
	}
	r := &SparseActivationRuntime{
		postings:    map[string]map[string]struct{}{},
		nodeFeature: map[string][]string{},
		fingerprint: map[string]uint64{},
		topK:        topK,
		maxTokens:   maxTokens,
	}
	r.lastBackend.Store("uninitialized")
	return r
}

func activationTokens(s string, limit int) []string {
	s = strings.ToLower(strings.TrimSpace(s))
	if s == "" {
		return nil
	}
	seen := map[string]bool{}
	out := make([]string, 0, 16)
	var b strings.Builder
	flush := func() {
		if b.Len() == 0 {
			return
		}
		q := b.String()
		b.Reset()
		if !seen[q] {
			seen[q] = true
			out = append(out, q)
		}
	}
	for _, r := range s {
		if unicode.IsLetter(r) || unicode.IsDigit(r) || r == '_' || r == '-' || r == '.' {
			b.WriteRune(r)
		} else {
			flush()
		}
		if limit > 0 && len(out) >= limit {
			break
		}
	}
	flush()
	if limit > 0 && len(out) > limit {
		out = out[:limit]
	}
	return out
}

func activationFeaturesForMemory(m *Memory, limit int) []string {
	if m == nil {
		return nil
	}
	seen := map[string]bool{}
	add := func(x string) {
		x = strings.ToLower(strings.TrimSpace(x))
		if x == "" || seen[x] {
			return
		}
		seen[x] = true
	}
	add("id:" + m.ID)
	for _, t := range m.Tags {
		add("tag:" + t)
		for _, q := range activationTokens(t, limit) {
			add("tok:" + q)
		}
	}
	for _, t := range m.Trigger {
		add("trigger:" + t)
		for _, q := range activationTokens(t, limit) {
			add("tok:" + q)
		}
	}
	for _, q := range activationTokens(m.Content, limit) {
		add("tok:" + q)
	}
	// State is part of memory evidence. Only index scalar textual/numeric values;
	// nested structures stay in the canonical Memory and are read on demand.
	nstate := 0
	for k, v := range m.State {
		if nstate >= limit {
			break
		}
		add("state-key:" + k)
		switch x := v.(type) {
		case string:
			for _, q := range activationTokens(x, 8) {
				add("tok:" + q)
			}
		case float64, float32, int, int64, uint64, bool:
			add("state-val:" + fmt.Sprint(x))
		}
		nstate++
	}
	out := make([]string, 0, len(seen))
	for k := range seen {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func activationFingerprint(m *Memory) uint64 {
	if m == nil {
		return 0
	}
	// FNV-like compact fingerprint without adding another package dependency.
	var h uint64 = 1469598103934665603
	mix := func(s string) {
		for i := 0; i < len(s); i++ {
			h ^= uint64(s[i])
			h *= 1099511628211
		}
	}
	mix(m.ID)
	mix(m.Content)
	for _, x := range m.Tags {
		mix(x)
	}
	for _, x := range m.Trigger {
		mix(x)
	}
	mix(strconv.FormatUint(m.Reuse, 10))
	mix(strconv.FormatUint(m.Trials, 10))
	mix(strconv.FormatUint(m.Successes, 10))
	mix(strconv.FormatFloat(m.Reward, 'g', -1, 64))
	mix(strconv.FormatFloat(m.Stability, 'g', -1, 64))
	return h
}

func (r *SparseActivationRuntime) replaceNode(m *Memory) {
	if m == nil || m.ID == "" {
		return
	}
	features := activationFeaturesForMemory(m, r.maxTokens)
	fp := activationFingerprint(m)
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
		return fmt.Errorf("sparse activation requires core Memory.mem")
	}
	ids, err := e.store.AllIDs()
	if err != nil {
		return err
	}
	// Reset only this engineering index; the canonical Memory is untouched.
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
	// Include post-load acquired/emergent nodes already present in cache.
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

// RefreshDirty is called only while the daemon commit barrier is held exclusively.
// It incrementally updates the engineering index without rescanning the full body.
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

func queryActivationFeatures(q string, limit int) []string {
	toks := activationTokens(q, limit)
	seen := map[string]bool{}
	out := make([]string, 0, len(toks)*3+1)
	add := func(x string) {
		if x != "" && !seen[x] {
			seen[x] = true
			out = append(out, x)
		}
	}
	raw := strings.ToLower(strings.TrimSpace(q))
	if raw != "" {
		add("tag:" + raw)
		add("trigger:" + raw)
		add("id:" + raw)
	}
	for _, t := range toks {
		add("tok:" + t)
		add("tag:" + t)
		add("trigger:" + t)
	}
	return out
}

func activationMetricVector(m *Memory, hits int, qn int) [6]float32 {
	if qn < 1 {
		qn = 1
	}
	hitRatio := float32(hits) / float32(qn)
	stability := float32(m.Stability)
	if stability < 0 {
		stability = 0
	}
	if stability > 4 {
		stability = 4
	}
	stability /= 4
	reward := float32(1.0 / (1.0 + math.Exp(-m.Reward)))
	reuse := float32(math.Log1p(float64(m.Reuse)) / 8.0)
	if reuse > 1 {
		reuse = 1
	}
	success := float32(0)
	if m.Trials > 0 {
		success = float32(m.Successes) / float32(m.Trials)
	}
	executable := float32(0)
	if len(m.Program) > 0 {
		executable = 1
	}
	return [6]float32{hitRatio, stability, reward, reuse, success, executable}
}

func (r *SparseActivationRuntime) Activate(e *Engine, query string, topK int) (ActivationResult, error) {
	start := time.Now()
	atomic.AddUint64(&r.queries, 1)
	if topK <= 0 {
		topK = r.topK
	}
	qf := queryActivationFeatures(query, r.maxTokens)
	if len(qf) == 0 {
		return ActivationResult{Query: query, IndexedNodes: int(atomic.LoadInt64(&r.nodes)), Backend: "none", ElapsedUS: time.Since(start).Microseconds()}, nil
	}
	hit := map[string]int{}
	r.mu.RLock()
	for _, f := range qf {
		for id := range r.postings[f] {
			hit[id]++
		}
	}
	r.mu.RUnlock()
	if len(hit) == 0 {
		return ActivationResult{Query: query, IndexedNodes: int(atomic.LoadInt64(&r.nodes)), Backend: "none", ElapsedUS: time.Since(start).Microseconds()}, nil
	}
	ids := make([]string, 0, len(hit))
	for id := range hit {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	vectors := make([][6]float32, 0, len(ids))
	kept := make([]string, 0, len(ids))
	keptHits := make([]int, 0, len(ids))
	for _, id := range ids {
		m, er := e.resolveIDLocal(id)
		if er != nil || m == nil {
			continue
		}
		// Snapshot metrics under the canonical core lock. The index itself never mutates Memory.
		e.dataMu.RLock()
		cp := *m
		e.dataMu.RUnlock()
		vectors = append(vectors, activationMetricVector(&cp, hit[id], len(qf)))
		kept = append(kept, id)
		keptHits = append(keptHits, hit[id])
	}
	atomic.AddUint64(&r.candidates, uint64(len(vectors)))
	atomic.AddUint64(&r.scored, uint64(len(vectors)))
	scores, backend, batchRequests, batchVectors, er := globalActivationScoreBatcher.Score(vectors)
	if er != nil {
		return ActivationResult{}, er
	}
	r.lastBackend.Store(backend)
	_ = batchRequests
	_ = batchVectors
	out := make([]ActivationCandidate, len(scores))
	for i, s := range scores {
		out[i] = ActivationCandidate{ID: kept[i], Score: s, FeatureHit: keptHits[i]}
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Score == out[j].Score {
			return out[i].ID < out[j].ID
		}
		return out[i].Score > out[j].Score
	})
	if topK < len(out) {
		out = out[:topK]
	}
	return ActivationResult{Query: query, Candidates: out, CandidateCount: len(hit), IndexedNodes: int(atomic.LoadInt64(&r.nodes)), Backend: backend, ElapsedUS: time.Since(start).Microseconds()}, nil
}

func (r *SparseActivationRuntime) Info() map[string]any {
	r.mu.RLock()
	features := len(r.postings)
	r.mu.RUnlock()
	backend, _ := r.lastBackend.Load().(string)
	q := atomic.LoadUint64(&r.queries)
	c := atomic.LoadUint64(&r.candidates)
	avg := float64(0)
	if q > 0 {
		avg = float64(c) / float64(q)
	}
	return map[string]any{
		"mode":          "sparse-local-stimulus-index",
		"indexed_nodes": atomic.LoadInt64(&r.nodes), "features": features,
		"queries": q, "candidate_total": c, "avg_candidates": avg,
		"score_batches": atomic.LoadUint64(&globalActivationScoreBatcher.batches), "score_batch_requests": atomic.LoadUint64(&globalActivationScoreBatcher.requests), "score_batch_vectors": atomic.LoadUint64(&globalActivationScoreBatcher.vectors), "scored_vectors": atomic.LoadUint64(&r.scored),
		"last_backend": backend, "default_top_k": r.topK, "memory_semantics": "qualified-current-core-reference",
		"qualification": activationQualificationInfo(),
	}
}

func activationInfoJSON() string {
	b, _ := json.MarshalIndent(globalActivationRuntime.Info(), "", "  ")
	return string(b)
}

type activationScoreResponse struct {
	scores        []float32
	backend       string
	batchRequests int
	batchVectors  int
	err           error
}
type activationScoreJob struct {
	vectors [][6]float32
	ch      chan activationScoreResponse
}
type activationScoreBatcher struct {
	once       sync.Once
	ch         chan activationScoreJob
	window     time.Duration
	maxVectors int
	batches    uint64
	requests   uint64
	vectors    uint64
}

var globalActivationScoreBatcher = newActivationScoreBatcher()

func newActivationScoreBatcher() *activationScoreBatcher {
	window := 1200 * time.Microsecond
	if s := os.Getenv("MEMORYAI_GPU_BATCH_WINDOW_US"); s != "" {
		if n, er := strconv.Atoi(s); er == nil && n >= 0 {
			window = time.Duration(n) * time.Microsecond
		}
	}
	maxv := 65536
	if s := os.Getenv("MEMORYAI_GPU_BATCH_MAX_VECTORS"); s != "" {
		if n, er := strconv.Atoi(s); er == nil && n > 0 {
			maxv = n
		}
	}
	return &activationScoreBatcher{ch: make(chan activationScoreJob, 1024), window: window, maxVectors: maxv}
}
func (b *activationScoreBatcher) start() { b.once.Do(func() { go b.loop() }) }
func (b *activationScoreBatcher) Score(v [][6]float32) ([]float32, string, int, int, error) {
	if len(v) == 0 {
		return nil, "none", 1, 0, nil
	}
	b.start()
	ch := make(chan activationScoreResponse, 1)
	b.ch <- activationScoreJob{vectors: v, ch: ch}
	r := <-ch
	return r.scores, r.backend, r.batchRequests, r.batchVectors, r.err
}
func (b *activationScoreBatcher) loop() {
	weights := [6]float32{0.52, 0.12, 0.10, 0.08, 0.10, 0.08}
	for first := range b.ch {
		jobs := []activationScoreJob{first}
		total := len(first.vectors)
		timer := time.NewTimer(b.window)
	collect:
		for total < b.maxVectors {
			select {
			case j := <-b.ch:
				jobs = append(jobs, j)
				total += len(j.vectors)
				if total >= b.maxVectors {
					break collect
				}
			case <-timer.C:
				break collect
			}
		}
		if !timer.Stop() {
			select {
			case <-timer.C:
			default:
			}
		}
		all := make([][6]float32, 0, total)
		for _, j := range jobs {
			all = append(all, j.vectors...)
		}
		scores, backend, err := globalParallelRuntime.Score6(all, weights)
		atomic.AddUint64(&b.batches, 1)
		atomic.AddUint64(&b.requests, uint64(len(jobs)))
		atomic.AddUint64(&b.vectors, uint64(len(all)))
		off := 0
		for _, j := range jobs {
			n := len(j.vectors)
			part := []float32(nil)
			if err == nil {
				part = append([]float32(nil), scores[off:off+n]...)
			}
			j.ch <- activationScoreResponse{scores: part, backend: backend, batchRequests: len(jobs), batchVectors: len(all), err: err}
			off += n
		}
	}
}
