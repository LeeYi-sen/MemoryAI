package main

import (
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"
)

func (m *meshRuntime) registerWithAuthority() {
	m.mu.RLock()
	node := m.directory[m.nodeID]
	authority := m.authorityURL
	m.mu.RUnlock()
	if authority == "" {
		return
	}
	_, err := m.rpc(authority, MeshRequest{Op: "register_node", Node: &node})
	if err != nil {
		m.mu.Lock()
		m.startupErr = "authority register: " + err.Error()
		m.mu.Unlock()
	}
}

func (m *meshRuntime) authorityRPC(req MeshRequest) (MeshResponse, error) {
	if m == nil {
		return MeshResponse{}, errors.New("mesh runtime unavailable")
	}
	if m.role == "sovereign" {
		res := m.handleAuthority(req)
		if !res.OK {
			return res, errors.New(res.Error)
		}
		return res, nil
	}
	if m.authorityURL == "" {
		return MeshResponse{}, errors.New("sovereign authority unavailable")
	}
	return m.rpc(m.authorityURL, req)
}

func (m *meshRuntime) handleRPC(req MeshRequest) MeshResponse {
	switch req.Op {
	case "shared_fetch":
		if err := consumeMeshGrant(req.Grant, []string{"shared_fetch"}, req.MemoryID, m.nodeID); err != nil {
			return MeshResponse{OK: false, Error: "sovereign grant denied: " + err.Error()}
		}
		return m.serveLocalMemory(req.MemoryID)
	case "structure_run":
		if err := consumeMeshGrant(req.Grant, []string{"shared_execute", "route_execute"}, req.MemoryID, m.nodeID); err != nil {
			return MeshResponse{OK: false, Error: "sovereign grant denied: " + err.Error()}
		}
		return m.serveLocalExecution(req.MemoryID, req.Vars)
	default:
		if m.role != "sovereign" {
			return MeshResponse{OK: false, Error: "authority operation requires sovereign role"}
		}
		return m.handleAuthority(req)
	}
}

func (m *meshRuntime) handleAuthority(req MeshRequest) MeshResponse {
	switch req.Op {
	case "register_node":
		if req.Node == nil || strings.TrimSpace(req.Node.ID) == "" {
			return MeshResponse{OK: false, Error: "node registration missing id"}
		}
		n := *req.Node
		n.LastSeen = time.Now().Unix()
		if err := persistSovereignMeshNode(m, n); err != nil {
			return MeshResponse{OK: false, Error: "persist Sovereign node directory: " + err.Error()}
		}
		m.mu.Lock()
		m.directory[n.ID] = n
		m.mu.Unlock()
		return MeshResponse{OK: true, Status: "registered"}
	case "directory":
		m.mu.RLock()
		nodes := make([]MeshNode, 0, len(m.directory))
		for _, n := range m.directory {
			nodes = append(nodes, n)
		}
		m.mu.RUnlock()
		sort.Slice(nodes, func(i, j int) bool { return nodes[i].ID < nodes[j].ID })
		return MeshResponse{OK: true, Status: "directory", Nodes: nodes}
	case "shared_propose":
		return m.authorizeSharedProposal(req)
	case "shared_search":
		return m.searchShared(req)
	case "shared_grant":
		return m.issueSharedGrant(req)
	case "shared_reconcile":
		m.mu.RLock()
		rec, ok := m.shared[req.MemoryID]
		m.mu.RUnlock()
		if !ok {
			return MeshResponse{OK: false, Error: "shared Memory not found", Status: "forgotten"}
		}
		if rec.Digest == req.ProposalDigest {
			return MeshResponse{OK: true, Status: "consistent"}
		}
		return MeshResponse{OK: true, Status: "conflict", Reason: "digest mismatch requires Memory-owned reconciliation"}
	default:
		return MeshResponse{OK: false, Error: "unsupported mesh authority operation: " + req.Op}
	}
}

func (m *meshRuntime) searchShared(req MeshRequest) MeshResponse {
	m.mu.RLock()
	records := make([]MeshRecord, 0)
	for _, rec := range m.shared {
		if req.Query != "" && req.Query != rec.MemoryID && !containsExact(rec.Tags, req.Query) {
			continue
		}
		matched := true
		for _, tag := range req.Tags {
			if !containsExact(rec.Tags, tag) {
				matched = false
				break
			}
		}
		if matched {
			records = append(records, rec)
		}
	}
	m.mu.RUnlock()
	sort.Slice(records, func(i, j int) bool { return records[i].MemoryID < records[j].MemoryID })
	return MeshResponse{OK: true, Status: "exact", Records: records}
}

func (m *meshRuntime) issueSharedGrant(req MeshRequest) MeshResponse {
	operation := strings.TrimSpace(req.Vars["operation"])
	if operation != "shared_fetch" && operation != "shared_execute" {
		return MeshResponse{OK: false, Error: "shared grant operation denied"}
	}
	m.mu.RLock()
	rec, ok := m.shared[strings.TrimSpace(req.MemoryID)]
	m.mu.RUnlock()
	if !ok {
		return MeshResponse{OK: false, Error: "shared Memory not authorized", Status: "forgotten"}
	}
	grant, err := issueMeshGrant(operation, rec.MemoryID, req.OriginNode, rec.OriginNode, meshGrantTTL)
	if err != nil {
		return MeshResponse{OK: false, Error: err.Error()}
	}
	return MeshResponse{OK: true, Status: "authorized", Records: []MeshRecord{rec}, Grant: grant}
}

func containsExact(xs []string, want string) bool {
	for _, x := range xs {
		if x == want {
			return true
		}
	}
	return false
}

func (m *meshRuntime) authorizeSharedProposal(req MeshRequest) MeshResponse {
	if strings.TrimSpace(req.MemoryID) == "" || strings.TrimSpace(req.OriginNode) == "" {
		return MeshResponse{OK: false, Error: "shared proposal requires memory_id and origin_node"}
	}
	m.mu.RLock()
	e := m.engine
	m.mu.RUnlock()
	if e == nil {
		return MeshResponse{OK: false, Error: "sovereign engine unavailable"}
	}

	f := newFrame()
	f.Vars["memory_id"] = req.MemoryID
	f.Vars["origin_node"] = req.OriginNode
	f.Vars["proposal_digest"] = req.ProposalDigest
	f.Vars["revision"] = fmt.Sprint(req.Revision)
	f.Lists["tags"] = append([]string(nil), req.Tags...)
	if err := e.fireEvent("mesh.shared.proposal", req.MemoryID, f); err != nil {
		return MeshResponse{OK: false, Error: err.Error()}
	}
	decision := strings.ToLower(strings.TrimSpace(f.Vars["mesh_decision"]))
	if decision == "" {
		decision = strings.ToLower(strings.TrimSpace(f.Vars["decision"]))
	}
	reason := f.Vars["mesh_reason"]
	if reason == "" {
		reason = f.Vars["reason"]
	}
	switch decision {
	case "approve", "approved", "allow", "authorized":
		rec := MeshRecord{
			MemoryID: req.MemoryID, OriginNode: req.OriginNode, Endpoint: req.Endpoint,
			Digest: req.ProposalDigest, Revision: req.Revision, Tags: append([]string(nil), req.Tags...),
		}
		// Authorization is a durable Sovereign fact. Persist it inside Memory
		// before acknowledging approval so a restart cannot silently forget it.
		if err := persistSovereignSharedRecord(m, rec); err != nil {
			return MeshResponse{OK: false, Error: "persist Sovereign shared authorization: " + err.Error()}
		}
		m.mu.Lock()
		m.shared[rec.MemoryID] = rec
		m.mu.Unlock()
		return MeshResponse{OK: true, Status: "shared", Decision: "approved", Reason: reason, Records: []MeshRecord{rec}}
	case "deny", "denied", "reject", "rejected":
		return MeshResponse{OK: true, Status: "local", Decision: "denied", Reason: reason}
	default:
		return MeshResponse{OK: true, Status: "candidate", Decision: "pending-memory-policy", Reason: reason}
	}
}
