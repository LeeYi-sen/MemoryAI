package main

import (
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"
)

type structureBundle struct {
	Format   string    `json:"format"`
	Memories []*Memory `json:"memories"`
}

// memoryJSONDigest is a structural identity digest, not an execution telemetry
// digest. RuntimeExecCount is physical runtime accounting and must never make a
// shared/imported Memory appear semantically changed merely because it ran.
func memoryJSONDigest(m *Memory) string {
	if m == nil {
		return ""
	}
	q := copyMemory(m)
	q.RuntimeExecCount = 0
	q.CapabilitySig = ""
	b, _ := json.Marshal(q)
	h := sha256.Sum256(b)
	return fmt.Sprintf("%x", h[:])
}

func (e *Engine) exportMemoryJSON(id string, sign bool) (string, error) {
	m, err := e.resolve(id)
	if err != nil {
		return "", err
	}
	out := copyMemory(m)
	if sign {
		out.CapabilitySig = signCapability(out)
		if strings.TrimSpace(out.CapabilitySig) == "" {
			return "", errors.New("capability signing key unavailable")
		}
	}
	b, err := json.Marshal(out)
	if err != nil {
		return "", err
	}
	return string(b), nil
}

func (e *Engine) importMemoryJSON(raw string, remote bool) (string, string, error) {
	// Frozen architecture: remote Memory is never copied/imported into this
	// body. A remote Memory is remembered only while its origin is reachable and
	// is consumed through the Sovereign Mesh direct-read path.
	if remote {
		return "", "denied", errors.New("remote Memory import disabled; use Sovereign Mesh direct read")
	}
	if err := ensureMemoryRecordRawBytesWithinPhysicalLimit(raw, "memory_import_json"); err != nil {
		return "", "invalid", err
	}
	var m Memory
	if err := json.Unmarshal([]byte(raw), &m); err != nil {
		return "", "invalid", err
	}
	m.ID = strings.TrimSpace(m.ID)
	if m.ID == "" {
		return "", "invalid", errors.New("imported Memory id required")
	}
	if m.State == nil {
		m.State = map[string]any{}
	}
	if err := validateMemoryCapabilities(&m, false); err != nil {
		return m.ID, "denied", err
	}

	_, current, err := e.resolveLocalFabricMemory(m.ID)
	if err == nil && current != nil {
		if memoryJSONDigest(current) == memoryJSONDigest(&m) {
			return m.ID, "unchanged", nil
		}
		if m.Revision <= current.Revision {
			return m.ID, "stale", fmt.Errorf(
				"import revision must advance existing Memory: id=%s incoming=%d current=%d",
				m.ID, m.Revision, current.Revision,
			)
		}
	} else if err != nil && !errors.Is(err, io.EOF) {
		return m.ID, "error", err
	}
	if err := e.upsertExplicitMemoryBounded(&m); err != nil {
		return m.ID, "error", err
	}
	return m.ID, "imported", nil
}

func (e *Engine) explicitDeleteMemory(id string) error {
	return e.deleteExplicitMemoryBounded(strings.TrimSpace(id))
}

func (e *Engine) collectStructures(ids []string, closure bool) ([]*Memory, error) {
	seen := map[string]bool{}
	queue := append([]string(nil), ids...)
	out := []*Memory{}
	for len(queue) > 0 {
		id := strings.TrimSpace(queue[0])
		queue = queue[1:]
		if id == "" || seen[id] {
			continue
		}
		seen[id] = true
		m, err := e.resolve(id)
		if err != nil {
			return nil, err
		}
		out = append(out, copyMemory(m))
		if closure {
			queue = append(queue, m.Parents...)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out, nil
}

func printStructureBundle(memories []*Memory) error {
	b, err := json.MarshalIndent(structureBundle{Format: "memoryai-structure-bundle-v1", Memories: memories}, "", "  ")
	if err != nil {
		return err
	}
	fmt.Println(string(b))
	return nil
}

func (e *Engine) exportStructures(ids []string) error {
	memories, err := e.collectStructures(ids, false)
	if err != nil {
		return err
	}
	return printStructureBundle(memories)
}

func (e *Engine) exportStructureClosure(ids []string) error {
	memories, err := e.collectStructures(ids, true)
	if err != nil {
		return err
	}
	return printStructureBundle(memories)
}

func decodeStructureBundle(data []byte) ([]*Memory, error) {
	var bundle structureBundle
	if err := json.Unmarshal(data, &bundle); err == nil && len(bundle.Memories) > 0 {
		return bundle.Memories, nil
	}
	var direct []*Memory
	if err := json.Unmarshal(data, &direct); err == nil && direct != nil {
		return direct, nil
	}
	var wrapper struct {
		Required []*Memory `json:"required"`
		Items    []*Memory `json:"items"`
	}
	if err := json.Unmarshal(data, &wrapper); err != nil {
		return nil, err
	}
	if len(wrapper.Required) > 0 {
		return wrapper.Required, nil
	}
	if wrapper.Items != nil {
		return wrapper.Items, nil
	}
	return nil, errors.New("structure bundle contains no memories")
}

func (e *Engine) syncRequiredStructures(path string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	memories, err := decodeStructureBundle(data)
	if err != nil {
		return err
	}
	sort.Slice(memories, func(i, j int) bool { return memories[i].ID < memories[j].ID })
	for _, incoming := range memories {
		if incoming == nil || strings.TrimSpace(incoming.ID) == "" {
			return errors.New("required structure has empty id")
		}
		if err := validateMemoryCapabilities(incoming, false); err != nil {
			return err
		}
		_, current, er := e.resolveLocalFabricMemory(incoming.ID)
		if er == nil && memoryJSONDigest(current) == memoryJSONDigest(incoming) {
			continue
		}
		if er != nil && !errors.Is(er, io.EOF) {
			return er
		}
		q := copyMemory(incoming)
		if er == nil && q.Revision <= current.Revision {
			q.CapabilitySig = ""
			q.Revision = current.Revision + 1
		}
		if err := e.upsertExplicitMemoryBounded(q); err != nil {
			return err
		}
	}
	return nil
}

func (e *Engine) deleteStructures(ids []string) error {
	ordered := append([]string(nil), ids...)
	sort.Strings(ordered)
	for _, id := range ordered {
		if err := e.explicitDeleteMemory(strings.TrimSpace(id)); err != nil {
			return err
		}
	}
	return nil
}
