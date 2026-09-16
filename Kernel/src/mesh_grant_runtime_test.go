package main

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

func TestMeshGrantSideEffectReplayFenceSurvivesRestart(t *testing.T) {
	dir := t.TempDir()
	_, e := testMeshMemoryRuntime(t, dir, "target-node", "node")
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("MEMORYAI_MESH_SOVEREIGN_PRIVATE_KEY_B64", base64.StdEncoding.EncodeToString(priv))
	t.Setenv("MEMORYAI_MESH_SOVEREIGN_PUBLIC_KEY_B64", base64.StdEncoding.EncodeToString(pub))
	m := &meshRuntime{role: "node", nodeID: "target-node", engine: e, directory: map[string]MeshNode{}, shared: map[string]MeshRecord{}}
	meshGlobalMu.Lock()
	oldGlobal := meshGlobal
	meshGlobal = m
	meshGlobalMu.Unlock()
	oldConsumed := consumedMeshGrantNonces
	consumedMeshGrantNonces = sync.Map{}
	defer func() {
		meshGlobalMu.Lock()
		meshGlobal = oldGlobal
		meshGlobalMu.Unlock()
		consumedMeshGrantNonces = oldConsumed
	}()

	grant, err := issueMeshGrant("shared_execute", "memory.x", "requester", "target-node", 30*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if err := consumeMeshGrant(grant, []string{"shared_execute"}, "memory.x", "target-node"); err != nil {
		t.Fatal(err)
	}
	e.close()

	restored, err := loadEngineWithMutationJournal(filepath.Join(dir, "Memory.mem"))
	if err != nil {
		t.Fatal(err)
	}
	defer restored.close()
	m.engine = restored
	consumedMeshGrantNonces = sync.Map{}
	if err := consumeMeshGrant(grant, []string{"shared_execute"}, "memory.x", "target-node"); err == nil {
		t.Fatal("restart accepted a previously consumed side-effect grant")
	}
}
