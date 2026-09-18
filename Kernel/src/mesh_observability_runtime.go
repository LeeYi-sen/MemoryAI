package main

import (
	"encoding/json"
	"errors"
	"os"
	"sort"
	"strings"
)

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

func meshInfoMap() map[string]any {
	m := meshRuntimeCurrent()
	if m == nil {
		return map[string]any{"role": "standalone", "remote_memory": "disabled"}
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	return map[string]any{
		"role":                          m.role,
		"node_id":                       m.nodeID,
		"endpoint":                      m.endpoint,
		"authority_configured":          m.authorityURL != "",
		"directory_nodes":               len(m.directory),
		"shared_records":                len(m.shared),
		"deferred_journal":              len(m.journal),
		"startup_error":                 m.startupErr,
		"remote_memory_semantics":       "direct-read-no-import",
		"routing":                       "stable-physical-availability-only",
		"shared_authorization":          "sovereign-memory-event+ed25519-grant",
		"transport_authentication":      "hmac-channel-only",
		"cross_host_transport":          "tls-required",
		"sovereign_public_key_present":  strings.TrimSpace(os.Getenv("MEMORYAI_MESH_SOVEREIGN_PUBLIC_KEY_B64")) != "",
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
	loopbackHTTP := validateMeshEndpoint("http://127.0.0.1:8787") == nil
	remoteHTTPRejected := validateMeshEndpoint("http://192.0.2.10:8787") != nil
	httpsAccepted := validateMeshEndpoint("https://mesh.example.invalid:8787") == nil
	ok := valid && tamperRejected && grantOK && loopbackHTTP && remoteHTTPRejected && httpsAccepted
	out := map[string]any{
		"ok":                        ok,
		"hmac_valid":                valid,
		"transport_tamper_rejected": tamperRejected,
		"sovereign_grant":           grantTest,
		"loopback_http_allowed":     loopbackHTTP,
		"remote_http_rejected":      remoteHTTPRejected,
		"https_accepted":            httpsAccepted,
		"remote_exec_caps":          "sovereign-grant+memory-capability-signature",
	}
	if !ok {
		if grantErr != nil {
			out["grant_error"] = grantErr.Error()
		}
		return out, errors.New("mesh security self-test failed")
	}
	return out, nil
}
