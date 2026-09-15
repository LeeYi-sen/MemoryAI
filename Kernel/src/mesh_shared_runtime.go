package main

import (
	"errors"
	"strings"
)

func (m *meshRuntime) proposeShared(id string) (MeshResponse, error) {
	m.mu.RLock()
	e := m.engine
	origin := m.nodeID
	endpoint := m.endpoint
	m.mu.RUnlock()
	if e == nil {
		return MeshResponse{}, errors.New("mesh engine unavailable")
	}
	_, mem, err := e.resolveLocalFabricMemoryCopy(strings.TrimSpace(id))
	if err != nil {
		return MeshResponse{}, err
	}
	req := MeshRequest{
		Op: "shared_propose", MemoryID: mem.ID, ProposalDigest: memoryJSONDigest(mem),
		Revision: mem.Revision, OriginNode: origin, Endpoint: endpoint, Tags: append([]string(nil), mem.Tags...),
	}
	res, err := m.authorityRPC(req)
	if err != nil {
		m.mu.Lock()
		m.journal = append(m.journal, req)
		m.mu.Unlock()
		return MeshResponse{OK: true, Status: "deferred", Decision: "authority-unreachable", Reason: err.Error()}, nil
	}
	return res, nil
}

func (m *meshRuntime) serveLocalMemory(id string) MeshResponse {
	m.mu.RLock()
	e := m.engine
	m.mu.RUnlock()
	if e == nil {
		return MeshResponse{OK: false, Error: "mesh engine unavailable"}
	}
	_, mem, err := e.resolveLocalFabricMemoryCopy(strings.TrimSpace(id))
	if err != nil {
		return MeshResponse{OK: false, Error: err.Error(), Status: "forgotten"}
	}
	return MeshResponse{OK: true, Status: "remembered", Memory: mem}
}

func (m *meshRuntime) serveLocalExecution(id string, vars map[string]string) MeshResponse {
	m.mu.RLock()
	e := m.engine
	m.mu.RUnlock()
	if e == nil {
		return MeshResponse{OK: false, Error: "mesh engine unavailable"}
	}
	owner, executable, err := e.resolveLocalFabricMemory(strings.TrimSpace(id))
	if err != nil {
		return MeshResponse{OK: false, Error: err.Error()}
	}
	if owner == nil || executable == nil || len(executable.Program) == 0 {
		return MeshResponse{OK: false, Error: "Memory is not executable: " + strings.TrimSpace(id)}
	}
	if _, err := owner.resolveExecutable(id); err != nil {
		return MeshResponse{OK: false, Error: err.Error()}
	}
	f := newFrame()
	for k, v := range vars {
		if !strings.HasPrefix(k, "__") || k == "__subject" {
			f.Vars[k] = v
		}
	}
	if err := globalTxnScheduler.run(owner, id, f); err != nil {
		return MeshResponse{OK: false, Error: err.Error()}
	}
	return MeshResponse{OK: true, Status: "executed", Frame: f}
}

func (m *meshRuntime) requestSharedGrant(id, operation string) (MeshRecord, *MeshGrant, error) {
	m.mu.RLock()
	origin := m.nodeID
	m.mu.RUnlock()
	res, err := m.authorityRPC(MeshRequest{
		Op: "shared_grant", MemoryID: strings.TrimSpace(id), OriginNode: origin,
		Vars: map[string]string{"operation": operation},
	})
	if err != nil {
		return MeshRecord{}, nil, err
	}
	if len(res.Records) != 1 || res.Grant == nil {
		return MeshRecord{}, nil, errors.New("sovereign did not issue shared authorization grant")
	}
	return res.Records[0], res.Grant, nil
}

func (m *meshRuntime) sharedFetch(id string) (MeshResponse, error) {
	id = strings.TrimSpace(id)
	if id == "" {
		return MeshResponse{}, errors.New("shared Memory id required")
	}
	rec, grant, err := m.requestSharedGrant(id, "shared_fetch")
	if err != nil {
		return MeshResponse{}, err
	}
	m.mu.RLock()
	self := m.nodeID
	m.mu.RUnlock()
	if rec.OriginNode == self {
		res := m.serveLocalMemory(rec.MemoryID)
		if !res.OK {
			return res, errors.New(res.Error)
		}
		return res, nil
	}
	res, err := m.rpc(rec.Endpoint, MeshRequest{Op: "shared_fetch", MemoryID: rec.MemoryID, Grant: grant})
	if err != nil {
		return MeshResponse{}, err
	}
	if res.Memory == nil {
		return MeshResponse{}, errors.New("remote shared Memory unavailable")
	}
	if rec.Digest != "" && memoryJSONDigest(res.Memory) != rec.Digest {
		return MeshResponse{}, errors.New("remote shared Memory digest mismatch")
	}
	if err := validateMemoryCapabilities(res.Memory, true); err != nil {
		return MeshResponse{}, err
	}
	res.Status = "remembered"
	return res, nil
}

func (m *meshRuntime) sharedExecute(id string, vars map[string]string) (MeshResponse, error) {
	rec, grant, err := m.requestSharedGrant(id, "shared_execute")
	if err != nil {
		return MeshResponse{}, err
	}
	m.mu.RLock()
	self := m.nodeID
	m.mu.RUnlock()
	if rec.OriginNode == self {
		out := m.serveLocalExecution(id, vars)
		if !out.OK {
			return out, errors.New(out.Error)
		}
		return out, nil
	}
	return m.rpc(rec.Endpoint, MeshRequest{Op: "structure_run", MemoryID: id, Vars: vars, Grant: grant})
}
