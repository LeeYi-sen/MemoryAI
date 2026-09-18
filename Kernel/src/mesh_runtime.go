package main

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"os"
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
	ReceiptID      string            `json:"receipt_id,omitempty"`
	Vars           map[string]string `json:"vars,omitempty"`
	Node           *MeshNode         `json:"node,omitempty"`
	Grant          *MeshGrant        `json:"grant,omitempty"`
}

type MeshResponse struct {
	OK        bool         `json:"ok"`
	Error     string       `json:"error,omitempty"`
	Status    string       `json:"status,omitempty"`
	Decision  string       `json:"decision,omitempty"`
	Reason    string       `json:"reason,omitempty"`
	ReceiptID string       `json:"receipt_id,omitempty"`
	Records   []MeshRecord `json:"records,omitempty"`
	Nodes     []MeshNode   `json:"nodes,omitempty"`
	Memory    *Memory      `json:"memory,omitempty"`
	Frame     *Frame       `json:"frame,omitempty"`
	Grant     *MeshGrant   `json:"grant,omitempty"`
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
	startupErr := ""
	if endpoint == "" && listen != "" {
		if generated, err := defaultMeshEndpoint(listen); err != nil {
			startupErr = err.Error()
		} else {
			endpoint = generated
		}
	}
	if endpoint != "" {
		if err := validateMeshEndpoint(endpoint); err != nil {
			startupErr = err.Error()
		}
	}
	if listen != "" && strings.HasPrefix(strings.ToLower(endpoint), "https://") {
		certFile, keyFile := meshTLSFiles()
		if certFile == "" || keyFile == "" {
			startupErr = "HTTPS mesh listener requires MEMORYAI_MESH_TLS_CERT and MEMORYAI_MESH_TLS_KEY"
		}
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
		startupErr:   startupErr,
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
	startupErr := m.startupErr
	m.mu.Unlock()

	if startupErr == "" && listen != "" && !alreadyServing {
		if err := m.startHTTPServer(listen); err != nil {
			m.mu.Lock()
			m.startupErr = err.Error()
			m.mu.Unlock()
		}
	}
	if startupErr == "" && m.role == "node" && m.authorityURL != "" {
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
	if err := validateMeshEndpoint(endpoint); err != nil {
		return out, err
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
	useTLS, err := meshListenUsesTLS(addr)
	if err != nil {
		return err
	}
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
		var serveErr error
		if useTLS {
			certFile, keyFile := meshTLSFiles()
			serveErr = server.ServeTLS(ln, certFile, keyFile)
		} else {
			serveErr = server.Serve(ln)
		}
		if serveErr != nil && !errors.Is(serveErr, http.ErrServerClosed) {
			m.mu.Lock()
			m.startupErr = serveErr.Error()
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
