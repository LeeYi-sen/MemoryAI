package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
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
	ExactPageOrder int     `json:"exact_page_order"`
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

func referenceActivationNodeFromMemory(m *Memory) referenceActivationNode {
	return referenceActivationNode{
		id:       m.ID,
		features: activationFeaturesForMemory(m, globalActivationRuntime.maxState),
		tags:     append([]string(nil), m.Tags...),
		trigger:  append([]string(nil), m.Trigger...),
	}
}

// buildReferenceActivationCorpus is an explicit qualification full scan. It is
// intentionally not a production activation path. The reference must cover the
// entire mounted local Fabric, including live dirty/new/deleted overlays, or a
// correct Fabric-wide exact index could be falsely compared against a
// primary-only baseline.
func buildReferenceActivationCorpus(e *Engine) ([]referenceActivationNode, error) {
	root := fabricRootFor(e)
	if root == nil {
		return nil, fmt.Errorf("activation qualification requires local Fabric")
	}

	byID := map[string]referenceActivationNode{}
	owners := map[string]*Engine{}
	for _, body := range root.localFabricEngines() {
		if body == nil {
			continue
		}
		persistedIDs, err := body.storeAllIDsLocal()
		if err != nil && !errors.Is(err, io.EOF) {
			return nil, err
		}
		ids := map[string]bool{}
		for _, id := range persistedIDs {
			ids[id] = true
		}
		body.dataMu.RLock()
		for id := range body.cache {
			ids[id] = true
		}
		deleted := make(map[string]bool, len(body.deletedIDs))
		for id, yes := range body.deletedIDs {
			deleted[id] = yes
		}
		body.dataMu.RUnlock()

		ordered := make([]string, 0, len(ids))
		for id := range ids {
			ordered = append(ordered, id)
		}
		sort.Strings(ordered)
		for _, id := range ordered {
			if deleted[id] {
				continue
			}
			m, err := body.resolveIDLocal(id)
			if errors.Is(err, io.EOF) {
				continue
			}
			if err != nil {
				return nil, err
			}
			if m == nil {
				continue
			}
			if previous := owners[id]; previous != nil && previous != body {
				return nil, duplicateFabricIdentityError(id)
			}
			owners[id] = body
			byID[id] = referenceActivationNodeFromMemory(m)
		}
	}

	out := make([]referenceActivationNode, 0, len(byID))
	for _, n := range byID {
		out = append(out, n)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].id < out[j].id })
	return out, nil
}

func referenceActivationFullScanCorpus(corpus []referenceActivationNode, query string, pageCap int) (ActivationResult, error) {
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
	if pageCap > 0 && pageCap < len(ids) {
		ids = ids[:pageCap]
	}

	out := make([]ActivationCandidate, 0, len(ids))
	for _, id := range ids {
		out = append(out, ActivationCandidate{
			ID:         id,
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
		Mode: "full-fabric-scan-reference-vs-physical-exact-index",
	}
	root := fabricRootFor(e)
	if root == nil {
		return report, fmt.Errorf("activation qualification requires local Fabric")
	}
	if err := globalActivationRuntime.Build(root); err != nil {
		return report, err
	}
	n := 48
	if x := os.Getenv("MEMORYAI_ACTIVATION_QUALIFY_QUERIES"); x != "" {
		if q, er := strconv.Atoi(x); er == nil && q > 0 {
			n = q
		}
	}

	corpus, err := buildReferenceActivationCorpus(root)
	if err != nil {
		return report, err
	}
	queries := activationQualificationQueriesFromCorpus(corpus, n)
	report.Queries = len(queries)
	report.IndexedNodes = int(atomic.LoadInt64(&globalActivationRuntime.nodes))
	report.ReferenceNodes = len(corpus)
	pageCap := globalActivationRuntime.pageCap

	for _, q := range queries {
		sparse, er := globalActivationRuntime.Activate(root, q, pageCap)
		if er != nil {
			return report, er
		}
		ref, er := referenceActivationFullScanCorpus(corpus, q, pageCap)
		if er != nil {
			return report, er
		}

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
					"stable physical order mismatch query=%q position=%d sparse=%s reference=%s",
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
		}
		if !exact {
			break
		}
		report.ExactPageOrder++
	}

	report.OK = report.Failure == "" &&
		report.ExactCandidate == report.Queries &&
		report.ExactPageOrder == report.Queries &&
		report.IndexedNodes == report.ReferenceNodes
	report.ElapsedMS = float64(time.Since(start).Microseconds()) / 1000
	activationQualification.Store(report)
	if !report.OK {
		if report.Failure == "" {
			report.Failure = fmt.Sprintf(
				"Fabric node count mismatch indexed=%d reference=%d",
				report.IndexedNodes, report.ReferenceNodes,
			)
			activationQualification.Store(report)
		}
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
