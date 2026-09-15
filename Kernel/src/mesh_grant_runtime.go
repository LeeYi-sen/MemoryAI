package main

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
	"sync"
	"time"
)

const (
	meshGrantVersion = 1
	meshGrantTTL     = 30 * time.Second
)

// MeshGrant is a Sovereign-issued authorization ticket. Transport HMAC proves
// that a packet came from a mesh peer; this ticket separately proves that the
// Sovereign authorized this exact physical operation.
type MeshGrant struct {
	Version     int    `json:"version"`
	Operation   string `json:"operation"`
	MemoryID    string `json:"memory_id"`
	OriginNode  string `json:"origin_node"`
	TargetNode  string `json:"target_node"`
	ExpiresUnix int64  `json:"expires_unix"`
	Nonce       string `json:"nonce"`
	Signature   string `json:"signature"`
}

type meshGrantPayload struct {
	Version     int    `json:"version"`
	Operation   string `json:"operation"`
	MemoryID    string `json:"memory_id"`
	OriginNode  string `json:"origin_node"`
	TargetNode  string `json:"target_node"`
	ExpiresUnix int64  `json:"expires_unix"`
	Nonce       string `json:"nonce"`
}

var consumedMeshGrantNonces sync.Map // nonce -> expiresUnix

func meshGrantPayloadBytes(g *MeshGrant) ([]byte, error) {
	if g == nil {
		return nil, errors.New("mesh grant required")
	}
	return json.Marshal(meshGrantPayload{
		Version: g.Version, Operation: g.Operation, MemoryID: g.MemoryID,
		OriginNode: g.OriginNode, TargetNode: g.TargetNode,
		ExpiresUnix: g.ExpiresUnix, Nonce: g.Nonce,
	})
}

func decodeEd25519Private(raw string) (ed25519.PrivateKey, error) {
	b, err := base64.StdEncoding.DecodeString(strings.TrimSpace(raw))
	if err != nil {
		return nil, fmt.Errorf("decode sovereign private key: %w", err)
	}
	switch len(b) {
	case ed25519.SeedSize:
		return ed25519.NewKeyFromSeed(b), nil
	case ed25519.PrivateKeySize:
		return ed25519.PrivateKey(b), nil
	default:
		return nil, fmt.Errorf("sovereign private key must decode to %d-byte seed or %d-byte private key", ed25519.SeedSize, ed25519.PrivateKeySize)
	}
}

func decodeEd25519Public(raw string) (ed25519.PublicKey, error) {
	b, err := base64.StdEncoding.DecodeString(strings.TrimSpace(raw))
	if err != nil {
		return nil, fmt.Errorf("decode sovereign public key: %w", err)
	}
	if len(b) != ed25519.PublicKeySize {
		return nil, fmt.Errorf("sovereign public key must decode to %d bytes", ed25519.PublicKeySize)
	}
	return ed25519.PublicKey(b), nil
}

func meshSovereignPrivateKey() (ed25519.PrivateKey, error) {
	raw := strings.TrimSpace(os.Getenv("MEMORYAI_MESH_SOVEREIGN_PRIVATE_KEY_B64"))
	if raw == "" {
		return nil, errors.New("MEMORYAI_MESH_SOVEREIGN_PRIVATE_KEY_B64 required on sovereign for remote grants")
	}
	return decodeEd25519Private(raw)
}

func meshSovereignPublicKey() (ed25519.PublicKey, error) {
	if raw := strings.TrimSpace(os.Getenv("MEMORYAI_MESH_SOVEREIGN_PUBLIC_KEY_B64")); raw != "" {
		return decodeEd25519Public(raw)
	}
	// Sovereign may verify its own grants without duplicating public-key config.
	priv, err := meshSovereignPrivateKey()
	if err != nil {
		return nil, errors.New("MEMORYAI_MESH_SOVEREIGN_PUBLIC_KEY_B64 required on mesh nodes")
	}
	pub, ok := priv.Public().(ed25519.PublicKey)
	if !ok {
		return nil, errors.New("invalid sovereign Ed25519 private key")
	}
	return pub, nil
}

func signMeshGrantWithKey(g *MeshGrant, privateKey ed25519.PrivateKey) error {
	payload, err := meshGrantPayloadBytes(g)
	if err != nil {
		return err
	}
	g.Signature = base64.StdEncoding.EncodeToString(ed25519.Sign(privateKey, payload))
	return nil
}

func verifyMeshGrantWithKey(g *MeshGrant, publicKey ed25519.PublicKey) error {
	payload, err := meshGrantPayloadBytes(g)
	if err != nil {
		return err
	}
	sig, err := base64.StdEncoding.DecodeString(strings.TrimSpace(g.Signature))
	if err != nil || len(sig) != ed25519.SignatureSize {
		return errors.New("invalid sovereign grant signature encoding")
	}
	if !ed25519.Verify(publicKey, payload, sig) {
		return errors.New("sovereign grant signature denied")
	}
	return nil
}

func issueMeshGrant(operation, memoryID, originNode, targetNode string, ttl time.Duration) (*MeshGrant, error) {
	privateKey, err := meshSovereignPrivateKey()
	if err != nil {
		return nil, err
	}
	if ttl <= 0 || ttl > 2*time.Minute {
		ttl = meshGrantTTL
	}
	nonce := make([]byte, 16)
	if _, err := rand.Read(nonce); err != nil {
		return nil, err
	}
	g := &MeshGrant{
		Version: meshGrantVersion, Operation: strings.TrimSpace(operation),
		MemoryID: strings.TrimSpace(memoryID), OriginNode: strings.TrimSpace(originNode),
		TargetNode: strings.TrimSpace(targetNode), ExpiresUnix: time.Now().Add(ttl).Unix(),
		Nonce: hex.EncodeToString(nonce),
	}
	if g.Operation == "" || g.MemoryID == "" || g.TargetNode == "" {
		return nil, errors.New("mesh grant requires operation, memory_id and target_node")
	}
	if err := signMeshGrantWithKey(g, privateKey); err != nil {
		return nil, err
	}
	return g, nil
}

func consumeMeshGrant(g *MeshGrant, allowedOperations []string, memoryID, targetNode string) error {
	if g == nil {
		return errors.New("sovereign authorization grant required")
	}
	if g.Version != meshGrantVersion {
		return fmt.Errorf("unsupported mesh grant version %d", g.Version)
	}
	allowed := false
	for _, op := range allowedOperations {
		if g.Operation == op {
			allowed = true
			break
		}
	}
	if !allowed {
		return fmt.Errorf("mesh grant operation %q not allowed", g.Operation)
	}
	if g.MemoryID != strings.TrimSpace(memoryID) {
		return errors.New("mesh grant MemoryID mismatch")
	}
	if g.TargetNode != strings.TrimSpace(targetNode) {
		return errors.New("mesh grant target-node mismatch")
	}
	now := time.Now().Unix()
	if g.ExpiresUnix <= now {
		return errors.New("mesh grant expired")
	}
	if g.ExpiresUnix > now+120 {
		return errors.New("mesh grant expiry exceeds physical authorization window")
	}
	if len(g.Nonce) < 16 {
		return errors.New("mesh grant nonce invalid")
	}
	publicKey, err := meshSovereignPublicKey()
	if err != nil {
		return err
	}
	if err := verifyMeshGrantWithKey(g, publicKey); err != nil {
		return err
	}
	if _, loaded := consumedMeshGrantNonces.LoadOrStore(g.Nonce, g.ExpiresUnix); loaded {
		return errors.New("mesh grant replay rejected")
	}
	// Opportunistic bounded cleanup; correctness does not depend on cleanup.
	consumedMeshGrantNonces.Range(func(key, value any) bool {
		expires, ok := value.(int64)
		if ok && expires <= now {
			consumedMeshGrantNonces.Delete(key)
		}
		return true
	})
	return nil
}

func meshGrantCryptoSelfTest() (map[string]any, error) {
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return nil, err
	}
	g := &MeshGrant{
		Version: meshGrantVersion, Operation: "shared_fetch", MemoryID: "m1",
		OriginNode: "requester", TargetNode: "owner", ExpiresUnix: time.Now().Add(30 * time.Second).Unix(),
		Nonce: "00112233445566778899aabbccddeeff",
	}
	if err := signMeshGrantWithKey(g, priv); err != nil {
		return nil, err
	}
	valid := verifyMeshGrantWithKey(g, pub) == nil
	tampered := *g
	tampered.MemoryID = "m2"
	tamperRejected := verifyMeshGrantWithKey(&tampered, pub) != nil
	ok := valid && tamperRejected
	out := map[string]any{"ok": ok, "ed25519_valid": valid, "tamper_rejected": tamperRejected}
	if !ok {
		return out, errors.New("mesh sovereign grant crypto self-test failed")
	}
	return out, nil
}
