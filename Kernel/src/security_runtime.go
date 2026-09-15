package main

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strings"
)

var privilegedOpCapability = map[string]string{
	"state_set": "memory.write", "state_list_append": "memory.write", "state_list_unique_append": "memory.write", "state_num_add": "memory.write", "metric_add": "memory.write",
	"memory_new": "memory.write", "memory_copy": "memory.write", "memory_tag_add": "memory.write", "memory_tag_remove": "memory.write",
	"memory_delete":      "memory.delete",
	"memory_import_json": "memory.import",
	"program_import":     "program.mutate", "program_set_field": "program.mutate", "program_set_var_ref": "program.mutate",
	"program_delete": "program.mutate", "program_insert_from": "program.mutate", "program_replace_from": "program.mutate",
	"process_restart": "process.restart", "persist": "persist", "cognition_concurrency_set": "scheduler.control",
	"space_next_path": "storage.manage", "space_create": "storage.manage", "space_mount": "storage.manage", "space_unmount": "storage.manage",
	"space_select_write": "storage.manage", "space_copy": "storage.manage", "space_move": "storage.manage", "space_merge": "storage.manage", "space_fsck": "storage.manage",
	"remote_space_create": "storage.remote.write", "remote_space_put": "storage.remote.write", "remote_space_import": "storage.remote.write", "remote_space_upsert": "storage.remote.write", "remote_space_delete": "storage.remote.write",
	"remote_space_info": "storage.remote.read", "remote_space_get": "storage.remote.read", "remote_space_digest": "storage.remote.read", "remote_space_list": "storage.remote.read", "remote_space_tag_list": "storage.remote.read",
	"artifact_write": "artifact.write", "artifact_read": "artifact.read", "artifact_digest": "artifact.read", "artifact_exists": "artifact.read",
	"mesh_shared_propose": "mesh.write", "mesh_shared_reconcile": "mesh.write", "mesh_shared_search": "mesh.read", "mesh_shared_fetch": "mesh.read", "mesh_structure_run": "mesh.run", "mesh_fanout": "mesh.run", "mesh_route_cognition": "mesh.run", "mesh_journal_flush": "mesh.write", "mesh_directory": "mesh.read",
	"physical_exchange": "network.raw", "emit_event": "event.emit", "credential_get": "credential.read",
}

func hasCapability(m *Memory, cap string) bool {
	if cap == "" {
		return true
	}
	for _, c := range m.Capabilities {
		if c == cap || c == "kernel.admin" {
			return true
		}
	}
	return false
}

func requiredCapability(code string) string { return privilegedOpCapability[code] }

func checkPrimitiveCapability(m *Memory, code string) error {
	cap := requiredCapability(code)
	if cap == "" {
		return nil
	}
	if hasCapability(m, cap) {
		return nil
	}
	return fmt.Errorf("capability denied: memory=%s op=%s requires=%s", m.ID, code, cap)
}

var remoteDeniedCaps = map[string]bool{
	"kernel.admin": true, "credential.read": true, "external.private": true, "network.raw": true,
	"process.restart": true, "persist": true, "scheduler.control": true, "storage.manage": true, "storage.remote.write": true,
	"artifact.write": true, "mesh.write": true, "mesh.run": true, "memory.delete": true, "memory.import": true,
}

func validateMemoryCapabilities(m *Memory, remote bool) error {
	for _, op := range m.Program {
		cap := requiredCapability(op.Code)
		if cap != "" && !hasCapability(m, cap) {
			return fmt.Errorf("memory %s op %s missing capability %s", m.ID, op.Code, cap)
		}
		if remote && cap != "" && remoteDeniedCaps[cap] {
			return fmt.Errorf("remote memory %s capability %s denied", m.ID, cap)
		}
	}
	if remote {
		for _, c := range m.Capabilities {
			if remoteDeniedCaps[c] {
				return fmt.Errorf("remote memory %s declared denied capability %s", m.ID, c)
			}
		}
		if (len(m.Program) > 0 || len(m.Trigger) > 0) && !verifyCapabilitySignature(m) {
			return fmt.Errorf("remote executable memory %s has invalid capability signature", m.ID)
		}
	}
	return nil
}

func capabilitySigningBytes(m *Memory) []byte {
	caps := append([]string(nil), m.Capabilities...)
	sort.Strings(caps)
	q := struct {
		ID           string   `json:"id"`
		Revision     uint64   `json:"revision"`
		Capabilities []string `json:"capabilities"`
		Trigger      []string `json:"trigger"`
		Program      []Op     `json:"program"`
	}{m.ID, m.Revision, caps, m.Trigger, m.Program}
	b, _ := json.Marshal(q)
	return b
}

func meshCapabilityKey() []byte {
	return []byte(strings.TrimSpace(os.Getenv("MEMORYAI_MESH_CAPABILITY_KEY")))
}

func signCapability(m *Memory) string {
	key := meshCapabilityKey()
	if len(key) == 0 {
		return ""
	}
	h := hmac.New(sha256.New, key)
	_, _ = h.Write(capabilitySigningBytes(m))
	return hex.EncodeToString(h.Sum(nil))
}

func verifyCapabilitySignature(m *Memory) bool {
	key := meshCapabilityKey()
	if len(key) == 0 || strings.TrimSpace(m.CapabilitySig) == "" {
		return false
	}
	want := signCapability(m)
	return hmac.Equal([]byte(strings.ToLower(want)), []byte(strings.ToLower(strings.TrimSpace(m.CapabilitySig))))
}
