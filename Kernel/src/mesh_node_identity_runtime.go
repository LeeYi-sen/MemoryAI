package main

import (
	"crypto/ed25519"
	"encoding/base64"
	"errors"
	"fmt"
	"os"
	"strings"
)

const (
	meshNodeIDHeader        = "X-MemoryAI-Mesh-Node-ID"
	meshNodeSignatureHeader = "X-MemoryAI-Mesh-Node-Signature"
)

func meshNodePrivateKey(m *meshRuntime) (ed25519.PrivateKey, error) {
	if m == nil {
		return nil, errors.New("mesh runtime unavailable for node identity")
	}
	if m.role == "sovereign" {
		return meshSovereignPrivateKey()
	}
	raw := strings.TrimSpace(os.Getenv("MEMORYAI_MESH_NODE_PRIVATE_KEY_B64"))
	if raw == "" {
		return nil, errors.New("MEMORYAI_MESH_NODE_PRIVATE_KEY_B64 required on mesh node")
	}
	return decodeEd25519Private(raw)
}

func meshNodePublicKeyB64(m *meshRuntime) (string, error) {
	priv, err := meshNodePrivateKey(m)
	if err != nil {
		return "", err
	}
	pub, ok := priv.Public().(ed25519.PublicKey)
	if !ok {
		return "", errors.New("invalid Mesh node Ed25519 private key")
	}
	return base64.StdEncoding.EncodeToString(pub), nil
}

func signMeshNodeRequest(m *meshRuntime, body []byte) (string, string, error) {
	if m == nil || strings.TrimSpace(m.nodeID) == "" {
		return "", "", errors.New("mesh node identity unavailable")
	}
	priv, err := meshNodePrivateKey(m)
	if err != nil {
		return "", "", err
	}
	return m.nodeID, base64.StdEncoding.EncodeToString(ed25519.Sign(priv, body)), nil
}

func verifyMeshNodeRequestSignature(publicKeyB64 string, body []byte, signatureB64 string) error {
	pub, err := decodeEd25519Public(publicKeyB64)
	if err != nil {
		return err
	}
	sig, err := base64.StdEncoding.DecodeString(strings.TrimSpace(signatureB64))
	if err != nil || len(sig) != ed25519.SignatureSize {
		return errors.New("invalid Mesh node signature encoding")
	}
	if !ed25519.Verify(pub, body, sig) {
		return errors.New("Mesh node identity signature denied")
	}
	return nil
}

func (m *meshRuntime) nodePublicKey(nodeID string) (string, bool) {
	if m == nil {
		return "", false
	}
	nodeID = strings.TrimSpace(nodeID)
	if nodeID == "" {
		return "", false
	}
	if nodeID == strings.TrimSpace(m.nodeID) {
		pub, err := meshNodePublicKeyB64(m)
		return pub, err == nil && pub != ""
	}
	m.mu.RLock()
	node, ok := m.directory[nodeID]
	m.mu.RUnlock()
	if !ok || strings.TrimSpace(node.PublicKey) == "" {
		return "", false
	}
	return strings.TrimSpace(node.PublicKey), true
}

func (m *meshRuntime) authenticateMeshHTTPRequest(req *MeshRequest, body []byte, nodeID, signature string) error {
	if m == nil || req == nil {
		return errors.New("mesh request identity unavailable")
	}
	nodeID = strings.TrimSpace(nodeID)
	if nodeID == "" {
		return errors.New("Mesh node identity header required")
	}
	signature = strings.TrimSpace(signature)
	if signature == "" {
		return errors.New("Mesh node identity signature required")
	}

	var publicKey string
	switch req.Op {
	case "register_node":
		if m.role != "sovereign" {
			return errors.New("node registration requires sovereign")
		}
		if req.Node == nil || strings.TrimSpace(req.Node.ID) != nodeID {
			return errors.New("registered node identity mismatch")
		}
		publicKey = strings.TrimSpace(req.Node.PublicKey)
		if publicKey == "" {
			return errors.New("registered node public key required")
		}
	case "shared_fetch", "structure_run":
		if req.Grant == nil {
			return errors.New("Sovereign grant required for direct node request")
		}
		if strings.TrimSpace(req.Grant.OriginNode) != nodeID {
			return errors.New("Mesh grant origin-node/requester mismatch")
		}
		publicKey = strings.TrimSpace(req.Grant.OriginPublicKey)
		if publicKey == "" {
			return errors.New("Mesh grant origin public key required")
		}
	default:
		if m.role != "sovereign" {
			return errors.New("authority request identity requires sovereign target")
		}
		var ok bool
		publicKey, ok = m.nodePublicKey(nodeID)
		if !ok {
			return fmt.Errorf("Mesh node identity %q is not registered", nodeID)
		}
	}
	if err := verifyMeshNodeRequestSignature(publicKey, body, signature); err != nil {
		return err
	}
	req.AuthenticatedNodeID = nodeID
	return nil
}
