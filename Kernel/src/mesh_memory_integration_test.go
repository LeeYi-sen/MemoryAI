package main

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

func installMeshRuntimeForTest(t *testing.T, m *meshRuntime) {
	t.Helper()
	meshGlobalMu.Lock()
	old := meshGlobal
	meshGlobal = m
	meshGlobalMu.Unlock()
	t.Cleanup(func() {
		meshGlobalMu.Lock()
		meshGlobal = old
		meshGlobalMu.Unlock()
	})
}

func setMeshGrantKeysForTest(t *testing.T) {
	t.Helper()
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("MEMORYAI_MESH_SOVEREIGN_PRIVATE_KEY_B64", base64.StdEncoding.EncodeToString(priv))
	t.Setenv("MEMORYAI_MESH_SOVEREIGN_PUBLIC_KEY_B64", base64.StdEncoding.EncodeToString(pub))
	t.Setenv("MEMORYAI_MESH_CAPABILITY_KEY", "mesh-integration-transport-key")
}

func TestMeshGlobalSyncDirectFetchIsTransient(t *testing.T) {
	setMeshGrantKeysForTest(t)
	e := loadCurrentBodyForGrowthTest(t)
	remote := &Memory{
		ID: "remote.belief", Layer: "emergent", Revision: 3,
		Tags:  []string{"memory", "cog.global.organism", "cog.belief", "cog.belief.supported"},
		State: map[string]any{"subject_id": "remote.subject", "object_id": "remote.object"},
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req MeshRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Errorf("decode remote request: %v", err)
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		switch req.Op {
		case "shared_fetch":
			_ = json.NewEncoder(w).Encode(MeshResponse{OK: true, Status: "remembered", Memory: remote})
		default:
			_ = json.NewEncoder(w).Encode(MeshResponse{OK: false, Error: "unexpected op " + req.Op})
		}
	}))
	defer server.Close()

	m := &meshRuntime{
		role: "sovereign", nodeID: "sovereign-test", engine: e,
		directory: map[string]MeshNode{"sovereign-test": {ID: "sovereign-test", Role: "sovereign"}},
		shared: map[string]MeshRecord{
			remote.ID: {
				MemoryID: remote.ID, OriginNode: "node-remote", Endpoint: server.URL,
				Digest: memoryJSONDigest(remote), Revision: remote.Revision, Tags: append([]string(nil), remote.Tags...),
			},
		},
		client: server.Client(),
	}
	installMeshRuntimeForTest(t, m)

	f := newFrame()
	if err := globalTxnScheduler.run(e, "mesh.global.sync.parent", f); err != nil {
		t.Fatal(err)
	}
	if got := f.Vars["mesh_integrated_remote_id"]; got != remote.ID {
		t.Fatalf("transient remote Memory did not enter integration frame: got=%q vars=%#v", got, f.Vars)
	}
	if _, _, err := e.resolveLocalFabricMemory(remote.ID); !errors.Is(err, io.EOF) {
		t.Fatalf("remote Memory was imported/cached locally: err=%v", err)
	}
	sync, err := e.resolve("mesh.global.sync.parent")
	if err != nil {
		t.Fatal(err)
	}
	if sync.State["last_integrated_count"] != "1" && sync.State["last_integrated_count"] != float64(1) {
		t.Fatalf("sync did not record one reachable transient Memory: %#v", sync.State)
	}
}

func TestMeshRemoteExecuteUsesSharedOwner(t *testing.T) {
	setMeshGrantKeysForTest(t)
	e := loadCurrentBodyForGrowthTest(t)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req MeshRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		if req.Op != "structure_run" || req.MemoryID != "remote.exec" {
			_ = json.NewEncoder(w).Encode(MeshResponse{OK: false, Error: "unexpected remote execution request"})
			return
		}
		out := newFrame()
		out.Vars["remote_result"] = "executed-at-owner"
		_ = json.NewEncoder(w).Encode(MeshResponse{OK: true, Status: "executed", Frame: out})
	}))
	defer server.Close()

	m := &meshRuntime{
		role: "sovereign", nodeID: "sovereign-test", engine: e,
		directory: map[string]MeshNode{"sovereign-test": {ID: "sovereign-test", Role: "sovereign"}},
		shared: map[string]MeshRecord{
			"remote.exec": {MemoryID: "remote.exec", OriginNode: "node-remote", Endpoint: server.URL, Digest: "opaque", Revision: 1},
		},
		client: server.Client(),
	}
	installMeshRuntimeForTest(t, m)

	f := newFrame()
	f.Vars["__event.memory_id"] = "remote.exec"
	if err := globalTxnScheduler.run(e, "mesh.remote.execute.parent", f); err != nil {
		t.Fatal(err)
	}
	if f.Vars["mesh_remote_execution_status"] != "executed" {
		t.Fatalf("remote executable did not run at owner: %#v", f.Vars)
	}
	if f.Vars["mesh_remote_execution_frame"] == "" {
		t.Fatalf("remote execution frame not returned transiently: %#v", f.Vars)
	}
}

func TestMeshDigestConflictEntersMemoryFrontier(t *testing.T) {
	setMeshGrantKeysForTest(t)
	e := loadCurrentBodyForGrowthTest(t)
	m := &meshRuntime{
		role: "sovereign", nodeID: "sovereign-test", engine: e,
		directory: map[string]MeshNode{"sovereign-test": {ID: "sovereign-test", Role: "sovereign"}},
		shared: map[string]MeshRecord{
			"remote.belief": {MemoryID: "remote.belief", OriginNode: "node-remote", Digest: "directory-digest", Revision: 4},
		},
		client: &http.Client{},
	}
	installMeshRuntimeForTest(t, m)

	conflict := &Memory{
		ID: "mesh.conflict.test", Layer: "emergent", Revision: 1,
		Tags:  []string{"memory", "cog.mesh.directory.conflict"},
		State: map[string]any{"memory_id": "remote.belief", "winner_digest": "memory-selected-digest"},
	}
	e.addRuntimeMemory(conflict)

	f := newFrame()
	f.Vars["__subject"] = conflict.ID
	if err := globalTxnScheduler.run(e, "mesh.conflict.reconcile.parent", f); err != nil {
		t.Fatal(err)
	}
	got, err := e.resolve(conflict.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.State["status"] != "conflict-preserved" || got.State["reconcile_status"] != "conflict" {
		t.Fatalf("conflict result was not preserved for Memory: %#v", got.State)
	}
	conflicts, err := e.listTagFabric("cog.mesh.version.conflict")
	if err != nil || len(conflicts) == 0 {
		t.Fatalf("no Memory-visible version conflict: ids=%v err=%v", conflicts, err)
	}
	frontiers, err := e.listTagFabric("cog.frontier.mesh")
	if err != nil || len(frontiers) == 0 {
		t.Fatalf("version conflict did not enter Memory frontier: ids=%v err=%v", frontiers, err)
	}
}
