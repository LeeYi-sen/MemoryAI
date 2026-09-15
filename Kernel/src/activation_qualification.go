package main

import (
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strconv"
	"strings"
	"sync/atomic"
	"time"
)

type ActivationQualificationReport struct {
	OK             bool    `json:"ok"`
	Queries        int     `json:"queries"`
	ExactCandidate int     `json:"exact_candidate_sets"`
	ExactTopK      int     `json:"exact_topk_order"`
	MaxScoreDiff   float32 `json:"max_score_diff"`
	IndexedNodes   int     `json:"indexed_nodes"`
	ReferenceNodes int     `json:"reference_nodes"`
	ElapsedMS      float64 `json:"elapsed_ms"`
	Mode           string  `json:"mode"`
	Failure        string  `json:"failure,omitempty"`
}

var activationQualification atomic.Value // ActivationQualificationReport

type referenceActivationNode struct {
	id       string
	features []string
	content  string
	tags     []string
	trigger  []string
	fixed    [6]float32
}

func buildReferenceActivationCorpus(e *Engine) ([]referenceActivationNode, error) {
	ids, err := e.store.AllIDs()
	if err != nil {
		return nil, err
	}
	sort.Strings(ids)
	seen := map[string]bool{}
	out := make([]referenceActivationNode, 0, len(ids))
	for _, id := range ids {
		if seen[id] {
			continue
		}
		seen[id] = true
		m, er := e.store.GetID(id)
		if er != nil || m == nil {
			continue
		}
		v := activationMetricVector(m, 0, 1)
		out = append(out, referenceActivationNode{id: id, features: activationFeaturesForMemory(m, globalActivationRuntime.maxTokens), content: m.Content, tags: append([]string(nil), m.Tags...), trigger: append([]string(nil), m.Trigger...), fixed: v})
	}
	// Overlay post-load canonical cache changes without consulting the sparse
	// index. This keeps the reference independent while avoiding repeated disk
	// scans for every qualification query.
	e.dataMu.RLock()
	cached := make([]*Memory, 0, len(e.cache))
	deleted := make(map[string]bool, len(e.deletedIDs))
	for id, v := range e.deletedIDs {
		deleted[id] = v
	}
	for _, m := range e.cache {
		cp := *m
		cp.Tags = append([]string(nil), m.Tags...)
		cp.Trigger = append([]string(nil), m.Trigger...)
		cached = append(cached, &cp)
	}
	e.dataMu.RUnlock()
	byID := make(map[string]referenceActivationNode, len(out)+len(cached))
	for _, n := range out {
		if !deleted[n.id] {
			byID[n.id] = n
		}
	}
	for _, m := range cached {
		if m == nil || m.ID == "" || deleted[m.ID] {
			continue
		}
		v := activationMetricVector(m, 0, 1)
		byID[m.ID] = referenceActivationNode{id: m.ID, features: activationFeaturesForMemory(m, globalActivationRuntime.maxTokens), content: m.Content, tags: append([]string(nil), m.Tags...), trigger: append([]string(nil), m.Trigger...), fixed: v}
	}
	out = out[:0]
	for _, n := range byID {
		out = append(out, n)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].id < out[j].id })
	return out, nil
}

func referenceActivationFullScanCorpus(corpus []referenceActivationNode, query string, topK int) (ActivationResult, error) {
	qf := queryActivationFeatures(query, globalActivationRuntime.maxTokens)
	if len(qf) == 0 {
		return ActivationResult{Query: query, Backend: "cpu-reference", IndexedNodes: len(corpus)}, nil
	}
	qset := map[string]bool{}
	for _, q := range qf {
		qset[q] = true
	}
	vectors := make([][6]float32, 0)
	kept := make([]string, 0)
	hits := make([]int, 0)
	for _, n := range corpus {
		hitCount := 0
		for _, f := range n.features {
			if qset[f] {
				hitCount++
			}
		}
		if hitCount == 0 {
			continue
		}
		v := n.fixed
		v[0] = float32(hitCount) / float32(len(qf))
		vectors = append(vectors, v)
		kept = append(kept, n.id)
		hits = append(hits, hitCount)
	}
	weights := [6]float32{0.52, 0.12, 0.10, 0.08, 0.10, 0.08}
	scores := globalParallelRuntime.score6CPU(vectors, weights)
	out := make([]ActivationCandidate, len(scores))
	for i, sc := range scores {
		out[i] = ActivationCandidate{ID: kept[i], Score: sc, FeatureHit: hits[i]}
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Score == out[j].Score {
			return out[i].ID < out[j].ID
		}
		return out[i].Score > out[j].Score
	})
	count := len(out)
	if topK > 0 && topK < len(out) {
		out = out[:topK]
	}
	return ActivationResult{Query: query, Candidates: out, CandidateCount: count, IndexedNodes: len(corpus), Backend: "cpu-reference"}, nil
}

func activationQualificationQueriesFromCorpus(corpus []referenceActivationNode, limit int) []string {
	if limit < 1 {
		limit = 48
	}
	queries := []string{}
	seen := map[string]bool{}
	add := func(q string) {
		q = strings.TrimSpace(q)
		if q != "" && !seen[q] && len(queries) < limit {
			seen[q] = true
			queries = append(queries, q)
		}
	}
	if len(corpus) == 0 {
		return queries
	}
	stride := len(corpus) / limit
	if stride < 1 {
		stride = 1
	}
	for i := 0; i < len(corpus) && len(queries) < limit; i += stride {
		n := corpus[i]
		add(n.id)
		if len(n.tags) > 0 {
			add(n.tags[0])
		}
		if len(n.trigger) > 0 {
			add(n.trigger[0])
		}
		ts := activationTokens(n.content, 4)
		if len(ts) > 0 {
			add(ts[0])
		}
	}
	return queries
}

func qualifySparseActivation(e *Engine) (ActivationQualificationReport, error) {
	start := time.Now()
	report := ActivationQualificationReport{Mode: "full-scan-reference-vs-sparse-index-topk"}
	if err := globalActivationRuntime.Build(e); err != nil {
		return report, err
	}
	n := 48
	if x := os.Getenv("MEMORYAI_ACTIVATION_QUALIFY_QUERIES"); x != "" {
		if q, er := strconv.Atoi(x); er == nil && q > 0 {
			n = q
		}
	}
	corpus, err := buildReferenceActivationCorpus(e)
	if err != nil {
		return report, err
	}
	queries := activationQualificationQueriesFromCorpus(corpus, n)
	report.Queries = len(queries)
	report.IndexedNodes = int(atomic.LoadInt64(&globalActivationRuntime.nodes))
	topK := globalActivationRuntime.topK
	tol := float32(1e-3)
	for _, q := range queries {
		sparse, er := globalActivationRuntime.Activate(e, q, topK)
		if er != nil {
			return report, er
		}
		ref, er := referenceActivationFullScanCorpus(corpus, q, topK)
		if er != nil {
			return report, er
		}
		report.ReferenceNodes = ref.IndexedNodes
		if sparse.CandidateCount != ref.CandidateCount {
			report.Failure = fmt.Sprintf("candidate count mismatch query=%q sparse=%d reference=%d", q, sparse.CandidateCount, ref.CandidateCount)
			break
		}
		report.ExactCandidate++
		if len(sparse.Candidates) != len(ref.Candidates) {
			report.Failure = fmt.Sprintf("topk length mismatch query=%q", q)
			break
		}
		exact := true
		for i := range sparse.Candidates {
			if sparse.Candidates[i].ID != ref.Candidates[i].ID || sparse.Candidates[i].FeatureHit != ref.Candidates[i].FeatureHit {
				exact = false
				report.Failure = fmt.Sprintf("topk semantic mismatch query=%q rank=%d sparse=%s reference=%s", q, i, sparse.Candidates[i].ID, ref.Candidates[i].ID)
				break
			}
			d := sparse.Candidates[i].Score - ref.Candidates[i].Score
			if d < 0 {
				d = -d
			}
			if d > report.MaxScoreDiff {
				report.MaxScoreDiff = d
			}
			if d > tol {
				exact = false
				report.Failure = fmt.Sprintf("score mismatch query=%q rank=%d diff=%g", q, i, d)
				break
			}
		}
		if !exact {
			break
		}
		report.ExactTopK++
	}
	report.OK = report.Failure == "" && report.ExactCandidate == report.Queries && report.ExactTopK == report.Queries
	report.ElapsedMS = float64(time.Since(start).Microseconds()) / 1000
	activationQualification.Store(report)
	if !report.OK {
		return report, fmt.Errorf("sparse activation qualification failed: %s", report.Failure)
	}
	return report, nil
}

func activationQualificationInfo() map[string]any {
	v := activationQualification.Load()
	if v == nil {
		return map[string]any{"status": "not-run"}
	}
	r := v.(ActivationQualificationReport)
	b, _ := json.Marshal(r)
	var out map[string]any
	_ = json.Unmarshal(b, &out)
	if r.OK {
		out["status"] = "qualified"
	} else {
		out["status"] = "failed"
	}
	return out
}
