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
	ExactTopK      int     `json:"exact_topk_order"` // legacy field name: exact stable physical page order
	MaxScoreDiff   float32 `json:"max_score_diff"`   // compatibility metric; must remain zero
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
	tags     []string
	trigger  []string
}

func buildReferenceActivationCorpus(e *Engine) ([]referenceActivationNode, error) {
	ids, err := e.storeAllIDs()
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
		m, er := e.storeGetID(id)
		if er != nil || m == nil {
			continue
		}
		out = append(out, referenceActivationNode{
			id:       id,
			features: activationFeaturesForMemory(m, globalActivationRuntime.maxState),
			tags:     append([]string(nil), m.Tags...),
			trigger:  append([]string(nil), m.Trigger...),
		})
	}

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
		cp.State = cloneActivationState(m.State)
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
		byID[m.ID] = referenceActivationNode{
			id:       m.ID,
			features: activationFeaturesForMemory(m, globalActivationRuntime.maxState),
			tags:     append([]string(nil), m.Tags...),
			trigger:  append([]string(nil), m.Trigger...),
		}
	}

	out = out[:0]
	for _, n := range byID {
		out = append(out, n)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].id < out[j].id })
	return out, nil
}

func referenceActivationFullScanCorpus(corpus []referenceActivationNode, query string, limit int) (ActivationResult, error) {
	qf := queryActivationFeatures(query, globalActivationRuntime.maxState)
	if len(qf) == 0 {
		return ActivationResult{
			Query:        query,
			Backend:      "cpu-exact-reference",
			IndexedNodes: len(corpus),
		}, nil
	}
	qset := map[string]bool{}
	for _, q := range qf {
		qset[q] = true
	}

	hit := map[string]int{}
	for _, n := range corpus {
		hitCount := 0
		for _, f := range n.features {
			if qset[f] {
				hitCount++
			}
		}
		if hitCount > 0 {
			hit[n.id] = hitCount
		}
	}

	ids := make([]string, 0, len(hit))
	for id := range hit {
		ids = append(ids, id)
	}
	sort.Strings(ids)

	count := len(ids)
	if limit > 0 && limit < len(ids) {
		ids = ids[:limit]
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
		IndexedNodes:   len(corpus),
		Backend:        "cpu-exact-reference",
	}, nil
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
			add("tag:" + n.tags[0])
		}
		if len(n.trigger) > 0 {
			add("trigger:" + n.trigger[0])
		}
	}
	return queries
}

func qualifySparseActivation(e *Engine) (ActivationQualificationReport, error) {
	start := time.Now()
	report := ActivationQualificationReport{
		Mode: "full-scan-reference-vs-physical-exact-index-no-cognitive-ranking",
	}
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
	limit := globalActivationRuntime.topK

	for _, q := range queries {
		sparse, er := globalActivationRuntime.Activate(e, q, limit)
		if er != nil {
			return report, er
		}
		ref, er := referenceActivationFullScanCorpus(corpus, q, limit)
		if er != nil {
			return report, er
		}
		report.ReferenceNodes = ref.IndexedNodes

		if sparse.CandidateCount != ref.CandidateCount {
			report.Failure = fmt.Sprintf(
				"candidate count mismatch query=%q sparse=%d reference=%d",
				q, sparse.CandidateCount, ref.CandidateCount,
			)
			break
		}
		report.ExactCandidate++

		if len(sparse.Candidates) != len(ref.Candidates) {
			report.Failure = fmt.Sprintf("physical page length mismatch query=%q", q)
			break
		}

		exact := true
		for i := range sparse.Candidates {
			if sparse.Candidates[i].ID != ref.Candidates[i].ID {
				exact = false
				report.Failure = fmt.Sprintf(
					"stable physical order mismatch query=%q rank=%d sparse=%s reference=%s",
					q, i, sparse.Candidates[i].ID, ref.Candidates[i].ID,
				)
				break
			}
			if sparse.Candidates[i].FeatureHit != ref.Candidates[i].FeatureHit {
				exact = false
				report.Failure = fmt.Sprintf(
					"physical feature-hit mismatch query=%q id=%s sparse=%d reference=%d",
					q,
					sparse.Candidates[i].ID,
					sparse.Candidates[i].FeatureHit,
					ref.Candidates[i].FeatureHit,
				)
				break
			}
			if sparse.Candidates[i].Score != 0 || ref.Candidates[i].Score != 0 {
				exact = false
				report.Failure = fmt.Sprintf(
					"kernel cognitive score must remain zero query=%q id=%s",
					q, sparse.Candidates[i].ID,
				)
				break
			}
		}
		if !exact {
			break
		}
		report.ExactTopK++
	}

	report.OK = report.Failure == "" &&
		report.ExactCandidate == report.Queries &&
		report.ExactTopK == report.Queries &&
		report.MaxScoreDiff == 0
	report.ElapsedMS = float64(time.Since(start).Microseconds()) / 1000
	activationQualification.Store(report)
	if !report.OK {
		return report, fmt.Errorf("physical exact activation qualification failed: %s", report.Failure)
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
