package main

import (
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"sort"
	"strings"
)

type structureBundle struct {
	Format   string    `json:"format"`
	Memories []*Memory `json:"memories"`
}

func memoryJSONDigest(m *Memory) string {
	b, _ := json.Marshal(m)
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
	if err := validateMemoryCapabilities(&m, remote); err != nil {
		return m.ID, "denied", err
	}

	if current, err := e.resolveIDLocal(m.ID); err == nil && current != nil {
		if memoryJSONDigest(current) == memoryJSONDigest(&m) {
			return m.ID, "unchanged", nil
		}
		if m.Revision <= current.Revision {
			return m.ID, "stale", fmt.Errorf(
				"import revision must advance existing Memory: id=%s incoming=%d current=%d",
				m.ID, m.Revision, current.Revision,
			)
		}
	}

	e.upsertExplicitMemory(&m)
	return m.ID, "imported", nil
}

func (e *Engine) upsertExplicitMemory(m *Memory) {
	q := copyMemory(m)
	if q.State == nil {
		q.State = map[string]any{}
	}
	e.dataMu.Lock()
	if old := e.cache[q.ID]; old != nil {
		for _, t := range old.Tags {
			e.tagDeltaRemoveLocked(q.ID, t)
		}
	} else if old, err := e.store.GetID(q.ID); err == nil && old != nil {
		for _, t := range old.Tags {
			e.tagDeltaRemoveLocked(q.ID, t)
		}
	}
	e.cache[q.ID] = q
	for _, t := range q.Tags {
		e.tagDeltaAddLocked(q.ID, t)
	}
	if _, err := e.store.GetID(q.ID); err != nil {
		e.newIDs[q.ID] = true
	}
	delete(e.deletedIDs, q.ID)
	e.dirtyIDs[q.ID] = true
	e.dirty = true
	e.dataMu.Unlock()
}

func (e *Engine) explicitDeleteMemory(id string) error {
	m, err := e.resolveIDLocal(id)
	if err != nil {
		return err
	}
	e.dataMu.Lock()
	for _, t := range m.Tags {
		e.tagDeltaRemoveLocked(id, t)
	}
	e.deletedIDs[id] = true
	delete(e.cache, id)
	delete(e.newIDs, id)
	delete(e.dirtyIDs, id)
	e.dirty = true
	e.dataMu.Unlock()
	return nil
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
	b, err := json.MarshalIndent(structureBundle{
		Format:   "memoryai-structure-bundle-v1",
		Memories: memories,
	}, "", "  ")
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
		current, er := e.resolveIDLocal(incoming.ID)
		if er == nil && memoryJSONDigest(current) == memoryJSONDigest(incoming) {
			continue
		}
		q := copyMemory(incoming)
		if er == nil && q.Revision <= current.Revision {
			q.Revision = current.Revision + 1
		}
		e.upsertExplicitMemory(q)
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
