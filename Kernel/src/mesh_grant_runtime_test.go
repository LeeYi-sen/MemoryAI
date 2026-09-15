package main

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"os"
	"testing"
	"time"
)

func withMeshGrantKeys(t *testing.T) (ed25519.PublicKey, ed25519.PrivateKey) {
	t.Helper()
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	oldPub := os.Getenv("MEMORYAI_MESH_SOVEREIGN_PUBLIC_KEY_B64")
	oldPriv := os.Getenv("MEMORYAI_MESH_SOVEREIGN_PRIVATE_KEY_B64")
	if err := os.Setenv("MEMORYAI_MESH_SOVEREIGN_PUBLIC_KEY_B64", base64.StdEncoding.EncodeToString(pub)); err != nil {
		t.Fatal(err)
	}
	if err := os.Setenv("MEMORYAI_MESH_SOVEREIGN_PRIVATE_KEY_B64", base64.StdEncoding.EncodeToString(priv)); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = os.Setenv("MEMORYAI_MESH_SOVEREIGN_PUBLIC_KEY_B64", oldPub)
		_ = os.Setenv("MEMORYAI_MESH_SOVEREIGN_PRIVATE_KEY_B64", oldPriv)
	})
	return pub, priv
}

func TestMeshGrantRequiresSovereignAuthorization(t *testing.T) {
	withMeshGrantKeys(t)
	grant, err := issueMeshGrant("shared_fetch", "memory-1", "requester", "node-a", 30*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if err := consumeMeshGrant(grant, []string{"shared_fetch"}, "memory-1", "node-a"); err != nil {
		t.Fatalf("valid grant denied: %v", err)
	}
}

func TestMeshGrantRejectsReplayAndBindingChanges(t *testing.T) {
	withMeshGrantKeys(t)
	grant, err := issueMeshGrant("shared_execute", "memory-2", "requester", "node-b", 30*time.Second)
	if err != nil {
		t.Fatal(err)
	}

	wrongMemory := *grant
	wrongMemory.MemoryID = "memory-other"
	if err := consumeMeshGrant(&wrongMemory, []string{"shared_execute"}, "memory-other", "node-b"); err == nil {
		t.Fatal("tampered MemoryID grant was accepted")
	}

	wrongTarget := *grant
	wrongTarget.TargetNode = "node-c"
	if err := consumeMeshGrant(&wrongTarget, []string{"shared_execute"}, "memory-2", "node-c"); err == nil {
		t.Fatal("tampered target grant was accepted")
	}

	if err := consumeMeshGrant(grant, []string{"shared_execute"}, "memory-2", "node-b"); err != nil {
		t.Fatalf("valid grant denied: %v", err)
	}
	if err := consumeMeshGrant(grant, []string{"shared_execute"}, "memory-2", "node-b"); err == nil {
		t.Fatal("replayed grant was accepted")
	}
}

func TestMeshGrantRejectsMissingGrantEvenWithTransportAuth(t *testing.T) {
	if err := consumeMeshGrant(nil, []string{"shared_fetch"}, "memory-3", "node-d"); err == nil {
		t.Fatal("missing Sovereign grant was accepted")
	}
	// A valid transport HMAC is intentionally irrelevant to authorization.
	key := []byte("shared-channel-key")
	body := []byte(`{"op":"shared_fetch","memory_id":"memory-3"}`)
	if !meshVerifyBytes(key, body, meshSignBytes(key, body)) {
		t.Fatal("transport self-check failed")
	}
	if err := consumeMeshGrant(nil, []string{"shared_fetch"}, "memory-3", "node-d"); err == nil {
		t.Fatal("transport authentication incorrectly substituted for Sovereign authorization")
	}
}
