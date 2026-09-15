package main

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"sort"
	"strings"
	"sync"
	"time"
)

const meshRPCPath = "/memoryai/mesh/v1"

type MeshNode struct {
	ID       string `json:"id"`
	Role     string `json:"role"`
	Endpoint string `json:"endpoint,omitempty"`
	LastSeen int64  `json:"last_seen"`
}

type MeshRecord struct {
	MemoryID   string   `json:"memory_id"`
	OriginNode string   `json:"origin_node"`
	Endpoint   string   `json:"endpoint,omitempty"`
	Digest     string   `json:"digest"`
	Revision   uint64   `json:"revision,omitempty"`
	Tags       []string `json:"tags,omitempty"`
}

type MeshRequest struct {
	Op             string            `json:"op"`
	Query          string            `json:"query,omitempty"`
	Tags           []string          `json:"tags,omitempty"`
	MemoryID       string            `json:"memory_id,omitempty"`
	ProposalDigest string            `json:"proposal_digest,omitempty"`
	Revision       uint64            `json:"revision,omitempty"`
	OriginNode     string            `json:"origin_node,omitempty"`
	Endpoint       string            `json:"endpoint,omitempty"`
	Vars           map[string]string `json:"vars,omitempty"`
	Node           *MeshNode         `json:"node,omitempty"`
	Grant          *MeshGrant        `json:"grant,omitempty"`
}

type MeshResponse struct {
	OK       bool         `json:"ok"`
	Error    string       `json:"error,omitempty"`
	Status   string       `json:"status,omitempty"`
	Decision string       `json:"decision,omitempty"`
	Reason   string       `json:"reason,omitempty"`
	Records  []MeshRecord `json:"records,omitempty"`
	Nodes    []MeshNode   `json:"nodes,omitempty"`
	Memory   *Memory      `json:"memory,omitempty"`
	Frame    *Frame       `json:"frame,omitempty"`
	Grant    *MeshGrant   `json:"grant,omitempty"`
}

type meshRuntime struct {
	mu           sync.RWMutex
	role         string
	nodeID       string
	endpoint     string
	authorityURL string
	engine       *Engine
	directory    map[string]MeshNode
	shared       map[string]MeshRecord
	journal      []MeshRequest
	client       *http.Client
	server       *http.Server
	listenAddr   string
	startupErr   string
}

var meshGlobalMu sync.Mutex
var meshGlobal *meshRuntime

func normalizedMeshRole(raw string) string {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "sovereign", "master", "main":
		return "sovereign"
	case "node", "worker":
		return "node"
	default:
		return "standalone"
	}
}

func newMeshRuntimeFromEnv() *meshRuntime {
	role := normalizedMeshRole(os.Getenv("MEMORYAI_MESH_ROLE"))
	if role == "standalone" {
		return nil
	}
	nodeID := strings.TrimSpace(os.Getenv("MEMORYAI_MESH_NODE_ID"))
	if nodeID == "" {
		nodeID = role + "-local"
	}
	listen := strings.TrimSpace(os.Getenv("MEMORYAI_MESH_LISTEN"))
	endpoint := strings.TrimRight(strings.TrimSpace(os.Getenv("MEMORYAI_MESH_ENDPOINT")), "/")
	if endpoint == "" && listen != "" {
		endpoint = "http://" + listen
	}
	return &meshRuntime{
		role:         role,
		nodeID:       nodeID,
		endpoint:     endpoint,
		authorityURL: strings.TrimRight(strings.TrimSpace(os.Getenv("MEMORYAI_MESH_AUTHORITY")), "/"),
		directory:    map[string]MeshNode{},
		shared:       map[string]MeshRecord{},
		client:       &http.Client{Timeout: 8 * time.Second},
		listenAddr:   listen,
	}
}

func meshRuntimeCurrent() *meshRuntime {
	meshGlobalMu.Lock()
	defer meshGlobalMu.Unlock()
	if meshGlobal == nil {
		meshGlobal = newMeshRuntimeFromEnv()
	}
	return meshGlobal
}

func bindMeshEngine(e *Engine) {
	m := meshRuntimeCurrent()
	if m == nil || e == nil {
		return
	}
	m.mu.Lock()
	m.engine = e
	m.directory[m.nodeID] = MeshNode{ID: m.nodeID, Role: m.role, Endpoint: m.endpoint, LastSeen: time.Now().Unix()}
	listen := m.listenAddr
	alreadyServing := m.server != nil
	m.mu.Unlock()

	if listen != "" && !alreadyServing {
		if err := m.startHTTPServer(listen); err != nil {
			m.mu.Lock()
			m.startupErr = err.Error()
			m.mu.Unlock()
		}
	}
	if m.role == "node" && m.authorityURL != "" {
		go m.registerWithAuthority()
	}
}

// meshTransportKey authenticates the channel only. It is intentionally not a
// Sovereign authorization key: remote Memory access/execution also requires a
// short-lived Ed25519 MeshGrant signed by the Sovereign.
func meshTransportKey() []byte {
	return []byte(strings.TrimSpace(os.Getenv("MEMORYAI_MESH_CAPABILITY_KEY")))
}

func meshSignBytes(key, body []byte) string {
	h := hmac.New(sha256.New, key)
	_, _ = h.Write(body)
	return hex.EncodeToString(h.Sum(nil))
}

func meshVerifyBytes(key, body []byte, signature string) bool {
	if len(key) == 0 || strings.TrimSpace(signature) == "" {
		return false
	}
	want := meshSignBytes(key, body)
	return hmac.Equal([]byte(want), []byte(strings.ToLower(strings.TrimSpace(signature))))
}

func (m *meshRuntime) rpc(endpoint string, req MeshRequest) (MeshResponse, error) {
	var out MeshResponse
	endpoint = strings.TrimRight(strings.TrimSpace(endpoint), "/")
	if endpoint == "" {
		return out, errors.New("mesh endpoint unavailable")
	}
	key := meshTransportKey()
	if len(key) == 0 {
		return out, errors.New("MEMORYAI_MESH_CAPABILITY_KEY required for remote mesh transport")
	}
	body, err := json.Marshal(req)
	if err != nil {
		return out, err
	}
	hreq, err := http.NewRequest(http.MethodPost, endpoint+meshRPCPath, bytes.NewReader(body))
	if err != nil {
		return out, err
	}
	hreq.Header.Set("Content-Type", "application/json")
	hreq.Header.Set("X-MemoryAI-Mesh-Signature", meshSignBytes(key, body))
	resp, err := m.client.Do(hreq)
	if err != nil {
		return out, err
	}
	defer resp.Body.Close()
	payload, err := io.ReadAll(io.LimitReader(resp.Body, 2<<20))
	if err != nil {
		return out, err
	}
	if err := json.Unmarshal(payload, &out); err != nil {
		return out, err
	}
	if !out.OK {
		if out.Error == "" {
			out.Error = resp.Status
		}
		return out, errors.New(out.Error)
	}
	return out, nil
}

func (m *meshRuntime) startHTTPServer(addr string) error {
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return err
	}
	server := &http.Server{Handler: http.HandlerFunc(m.handleHTTP)}
	m.mu.Lock()
	if m.server != nil {
		m.mu.Unlock()
		_ = ln.Close()
		return nil
	}
	m.server = server
	m.mu.Unlock()
	go func() {
		if err := server.Serve(ln); err != nil && !errors.Is(err, http.ErrServerClosed) {
			m.mu.Lock()
			m.startupErr = err.Error()
			m.mu.Unlock()
		}
	}()
	return nil
}

func (m *meshRuntime) handleHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	if r.Method != http.MethodPost || r.URL.Path != meshRPCPath {
		w.WriteHeader(http.StatusNotFound)
		_ = json.NewEncoder(w).Encode(MeshResponse{OK: false, Error: "mesh endpoint not found"})
		return
	}
	body, err := io.ReadAll(io.LimitReader(r.Body, 2<<20))
	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(MeshResponse{OK: false, Error: err.Error()})
		return
	}
	if !meshVerifyBytes(meshTransportKey(), body, r.Header.Get("X-MemoryAI-Mesh-Signature")) {
		w.WriteHeader(http.StatusUnauthorized)
		_ = json.NewEncoder(w).Encode(MeshResponse{OK: false, Error: "mesh transport signature denied"})
		return
	}
	var req MeshRequest
	if err := json.Unmarshal(body, &req); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(MeshResponse{OK: false, Error: err.Error()})
		return
	}
	res := m.handleRPC(req)
	if !res.OK {
		w.WriteHeader(http.StatusConflict)
	}
	_ = json.NewEncoder(w).Encode(res)
}

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

func (m *meshRuntime) proposeShared(id string) (MeshResponse, error) {
	m.mu.RLock()
	e := m.engine
	origin := m.nodeID
	endpoint := m.endpoint
	m.mu.RUnlock()
	if e == nil {
		return MeshResponse{}, errors.New("mesh engine unavailable")
	}
	mem, err := e.resolveIDLocal(strings.TrimSpace(id))
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
	mem, err := e.resolveIDLocal(strings.TrimSpace(id))
	if err != nil {
		return MeshResponse{OK: false, Error: err.Error(), Status: "forgotten"}
	}
	return MeshResponse{OK: true, Status: "remembered", Memory: copyMemory(mem)}
}

func (m *meshRuntime) serveLocalExecution(id string, vars map[string]string) MeshResponse {
	m.mu.RLock()
	e := m.engine
	m.mu.RUnlock()
	if e == nil {
		return MeshResponse{OK: false, Error: "mesh engine unavailable"}
	}
	if _, err := e.resolveExecutable(id); err != nil {
		return MeshResponse{OK: false, Error: err.Error()}
	}
	f := newFrame()
	for k, v := range vars {
		if !strings.HasPrefix(k, "__") || k == "__subject" {
			f.Vars[k] = v
		}
	}
	if err := globalTxnScheduler.run(e, id, f); err != nil {
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

func (m *meshRuntime) routeCognition(id string, vars map[string]string, preferLocal bool) (string, *Frame, error) {
	if m.role != "sovereign" {
		return "", nil, errors.New("physical cognition routing requires sovereign role")
	}
	m.mu.RLock()
	nodes := make([]MeshNode, 0, len(m.directory))
	for _, n := range m.directory {
		nodes = append(nodes, n)
	}
	self := m.nodeID
	e := m.engine
	m.mu.RUnlock()
	sort.Slice(nodes, func(i, j int) bool { return nodes[i].ID < nodes[j].ID })

	if preferLocal && e != nil {
		if _, err := e.resolveExecutable(id); err == nil {
			res := m.serveLocalExecution(id, vars)
			if res.OK {
				return self, res.Frame, nil
			}
		}
	}
	if len(nodes) == 0 {
		return "", nil, errors.New("no mesh nodes available")
	}
	// Deterministic physical sharding only; there is no semantic score.
	h := sha256.Sum256([]byte(id))
	start := int(h[0]) % len(nodes)
	for i := 0; i < len(nodes); i++ {
		n := nodes[(start+i)%len(nodes)]
		if n.ID == self {
			if e == nil {
				continue
			}
			res := m.serveLocalExecution(id, vars)
			if res.OK {
				return n.ID, res.Frame, nil
			}
			continue
		}
		if n.Endpoint == "" {
			continue
		}
		grant, err := issueMeshGrant("route_execute", id, self, n.ID, meshGrantTTL)
		if err != nil {
			return "", nil, err
		}
		res, err := m.rpc(n.Endpoint, MeshRequest{Op: "structure_run", MemoryID: id, Vars: vars, Grant: grant})
		if err == nil && res.OK {
			return n.ID, res.Frame, nil
		}
	}
	return "", nil, errors.New("no physically reachable node could execute structure")
}

func (m *meshRuntime) fanout(id string, vars map[string]string, includeSelf bool) ([]string, error) {
	if m.role != "sovereign" {
		return nil, errors.New("mesh fanout requires sovereign role")
	}
	res, err := m.authorityRPC(MeshRequest{Op: "directory"})
	if err != nil {
		return nil, err
	}
	m.mu.RLock()
	self := m.nodeID
	m.mu.RUnlock()
	sort.Slice(res.Nodes, func(i, j int) bool { return res.Nodes[i].ID < res.Nodes[j].ID })
	out := make([]string, 0, len(res.Nodes))
	for _, n := range res.Nodes {
		if n.ID == self && !includeSelf {
			continue
		}
		row := map[string]any{"node": n.ID, "ok": false}
		var rr MeshResponse
		var er error
		if n.ID == self {
			rr = m.serveLocalExecution(id, vars)
			if !rr.OK {
				er = errors.New(rr.Error)
			}
		} else {
			grant, grantErr := issueMeshGrant("route_execute", id, self, n.ID, meshGrantTTL)
			if grantErr != nil {
				er = grantErr
			} else {
				rr, er = m.rpc(n.Endpoint, MeshRequest{Op: "structure_run", MemoryID: id, Vars: vars, Grant: grant})
			}
		}
		if er != nil {
			row["error"] = er.Error()
		} else {
			row["ok"] = rr.OK
			row["frame"] = rr.Frame
		}
		b, _ := json.Marshal(row)
		out = append(out, string(b))
	}
	return out, nil
}

func (m *meshRuntime) flushJournal() error {
	m.mu.Lock()
	pending := append([]MeshRequest(nil), m.journal...)
	m.journal = nil
	m.mu.Unlock()
	failed := make([]MeshRequest, 0)
	for _, req := range pending {
		if _, err := m.authorityRPC(req); err != nil {
			failed = append(failed, req)
		}
	}
	if len(failed) > 0 {
		m.mu.Lock()
		m.journal = append(failed, m.journal...)
		m.mu.Unlock()
		return fmt.Errorf("%d mesh journal entries remain deferred", len(failed))
	}
	return nil
}

func meshInfoMap() map[string]any {
	m := meshRuntimeCurrent()
	if m == nil {
		return map[string]any{"role": "standalone", "remote_memory": "disabled"}
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	return map[string]any{
		"role":                         m.role,
		"node_id":                      m.nodeID,
		"endpoint":                     m.endpoint,
		"authority_configured":         m.authorityURL != "",
		"directory_nodes":              len(m.directory),
		"shared_records":               len(m.shared),
		"deferred_journal":             len(m.journal),
		"startup_error":                m.startupErr,
		"remote_memory_semantics":      "direct-read-no-import",
		"routing":                      "stable-physical-availability-only",
		"shared_authorization":         "sovereign-memory-event+ed25519-grant",
		"transport_authentication":     "hmac-channel-only",
		"sovereign_public_key_present": strings.TrimSpace(os.Getenv("MEMORYAI_MESH_SOVEREIGN_PUBLIC_KEY_B64")) != "",
		"sovereign_private_key_present": strings.TrimSpace(os.Getenv("MEMORYAI_MESH_SOVEREIGN_PRIVATE_KEY_B64")) != "",
	}
}

func meshSecuritySelfTest() (map[string]any, error) {
	key := []byte("memoryai-mesh-selftest-key")
	body := []byte(`{"op":"directory"}`)
	sig := meshSignBytes(key, body)
	valid := meshVerifyBytes(key, body, sig)
	tamperRejected := !meshVerifyBytes(key, []byte(`{"op":"shared_fetch"}`), sig)
	grantTest, grantErr := meshGrantCryptoSelfTest()
	grantOK := grantErr == nil
	ok := valid && tamperRejected && grantOK
	out := map[string]any{
		"ok":                       ok,
		"hmac_valid":               valid,
		"transport_tamper_rejected": tamperRejected,
		"sovereign_grant":          grantTest,
		"remote_exec_caps":         "sovereign-grant+memory-capability-signature",
	}
	if !ok {
		if grantErr != nil {
			out["grant_error"] = grantErr.Error()
		}
		return out, errors.New("mesh security self-test failed")
	}
	return out, nil
}
