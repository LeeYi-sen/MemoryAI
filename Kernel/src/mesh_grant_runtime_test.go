package main

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"path/filepath"
	"testing"
	"time"
)

func snapshotConsumedMeshGrantNoncesForTest() map[any]any {
	snapshot := map[any]any{}
	consumedMeshGrantNonces.Range(func(key, value any) bool {
		snapshot[key] = value
		return true
	})
	return snapshot
}

func restoreConsumedMeshGrantNoncesForTest(snapshot map[any]any) {
	consumedMeshGrantNonces.Range(func(key, value any) bool {
		consumedMeshGrantNonces.Delete(key)
		return true
	})
	for key, value := range snapshot {
		consumedMeshGrantNonces.Store(key, value)
	}
}

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
	oldConsumed := snapshotConsumedMeshGrantNoncesForTest()
	restoreConsumedMeshGrantNoncesForTest(nil)
	defer func() {
		meshGlobalMu.Lock()
		meshGlobal = oldGlobal
		meshGlobalMu.Unlock()
		restoreConsumedMeshGrantNoncesForTest(oldConsumed)
	}()

	grant, err := issueMeshGrant("shared_execute", "memory.x", "requester", "target-node", base64.StdEncoding.EncodeToString(pub), 30*time.Second)
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
	restoreConsumedMeshGrantNoncesForTest(nil)
	if err := consumeMeshGrant(grant, []string{"shared_execute"}, "memory.x", "target-node"); err == nil {
		t.Fatal("restart accepted a previously consumed side-effect grant")
	}
}
