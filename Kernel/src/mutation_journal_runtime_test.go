package main

import (
	"os"
	"path/filepath"
	"testing"
)

func markMutationJournalTestMemory(t *testing.T, e *Engine, id, value string) {
	t.Helper()
	m, err := e.resolveIDLocal(id)
	if err != nil {
		t.Fatal(err)
	}
	q := copyMemory(m)
	if q.State == nil {
		q.State = map[string]any{}
	}
	q.State["value"] = value
	q.Revision++
	e.dataMu.Lock()
	e.cache[id] = q
	e.dirtyIDs[id] = true
	e.dirty = true
	e.dataMu.Unlock()
}

func TestMutationJournalRestartRestoresWithoutFullBodyRewrite(t *testing.T) {
	path := filepath.Join(t.TempDir(), "Memory.mem")
	writeBodyForPersistenceTest(t, path, "core", []*Memory{{
		ID: "journal.target", Layer: "emergent", Tags: []string{"memory"},
		State: map[string]any{"value": "before"}, Revision: 1,
	}})

	e, err := loadEngineCanonical(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := e.saveBody(e.bodyPath); err != nil {
		e.close()
		t.Fatal(err)
	}
	markMutationJournalTestMemory(t, e, "journal.target", "after")
	if err := persistEngineIncremental(e); err != nil {
		e.close()
		t.Fatal(err)
	}
	e.close()

	baseOnly, err := loadEngineCanonical(path)
	if err != nil {
		t.Fatal(err)
	}
	base, err := baseOnly.resolveIDLocal("journal.target")
	if err != nil {
		baseOnly.close()
		t.Fatal(err)
	}
	if base.State["value"] != "before" {
		baseOnly.close()
		t.Fatalf("incremental persist unexpectedly rewrote full base body: %v", base.State)
	}
	baseOnly.close()

	restored, err := loadEngineWithMutationJournal(path)
	if err != nil {
		t.Fatal(err)
	}
	got, err := restored.resolveIDLocal("journal.target")
	if err != nil {
		restored.close()
		t.Fatal(err)
	}
	if got.State["value"] != "after" || got.Revision != 2 {
		restored.close()
		t.Fatalf("journal overlay was not restored: state=%v revision=%d", got.State, got.Revision)
	}
	if err := restored.saveBody(restored.bodyPath); err != nil {
		restored.close()
		t.Fatal(err)
	}
	restored.close()

	compacted, err := loadEngineCanonical(path)
	if err != nil {
		t.Fatal(err)
	}
	defer compacted.close()
	final, err := compacted.resolveIDLocal("journal.target")
	if err != nil {
		t.Fatal(err)
	}
	if final.State["value"] != "after" || final.Revision != 2 {
		t.Fatalf("full compaction did not absorb journal overlay: state=%v revision=%d", final.State, final.Revision)
	}
	rec, err := readMutationJournal(path)
	if err != nil {
		t.Fatal(err)
	}
	if rec != nil {
		t.Fatalf("full compaction left active mutation journal seq=%d", rec.seq)
	}
}

func TestMutationJournalFallsBackToPreviousValidSlot(t *testing.T) {
	path := filepath.Join(t.TempDir(), "journal.zip")
	if err := writeDetZipWithMutationJournal(path, []zipEntry{{name: "x", b: []byte("x"), store: true}}); err != nil {
		t.Fatal(err)
	}
	first := []byte(`{"version":1,"base":"a","mutations":[{"id":"one","deleted":true}]}`)
	second := []byte(`{"version":1,"base":"a","mutations":[{"id":"two","deleted":true}]}`)
	if err := writeMutationJournal(path, 0, first); err != nil {
		t.Fatal(err)
	}
	if err := writeMutationJournal(path, 1, second); err != nil {
		t.Fatal(err)
	}
	off, err := mutationJournalRegion(path)
	if err != nil {
		t.Fatal(err)
	}
	f, err := os.OpenFile(path, os.O_RDWR, 0)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.WriteAt([]byte{0xff}, off+mutationJournalSlotSize+mutationJournalHeaderSize); err != nil {
		f.Close()
		t.Fatal(err)
	}
	if err := f.Sync(); err != nil {
		f.Close()
		t.Fatal(err)
	}
	f.Close()

	rec, err := readMutationJournal(path)
	if err != nil {
		t.Fatal(err)
	}
	if rec == nil || rec.seq != 1 || !equalBytes(rec.raw, first) {
		t.Fatalf("journal did not fall back to previous valid slot: %#v", rec)
	}
}

func TestMutationJournalNeverCreatesSidecarFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "journal.zip")
	if err := writeDetZipWithMutationJournal(path, []zipEntry{{name: "x", b: []byte("x"), store: true}}); err != nil {
		t.Fatal(err)
	}
	if err := writeMutationJournal(path, 0, []byte(`{"version":1,"base":"a","mutations":[{"id":"one","deleted":true}]}`)); err != nil {
		t.Fatal(err)
	}
	for _, suffix := range []string{".delta", ".wal", ".journal"} {
		if _, err := os.Stat(path + suffix); !os.IsNotExist(err) {
			t.Fatalf("incremental persistence created forbidden sidecar %s", path+suffix)
		}
	}
}
