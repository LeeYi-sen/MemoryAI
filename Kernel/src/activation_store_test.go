package main

import (
	"archive/zip"
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

func containsString(xs []string, want string) bool {
	for _, x := range xs {
		if x == want {
			return true
		}
	}
	return false
}

func TestActivationFeaturesIndexAllStateFields(t *testing.T) {
	m := &Memory{
		ID:      "memory.large-state",
		Tags:    []string{"alpha"},
		Trigger: []string{"event:alpha"},
		State:   map[string]any{},
	}
	for i := 0; i < 192; i++ {
		m.State[fmt.Sprintf("field.%03d", i)] = fmt.Sprintf("value.%03d", i)
	}

	// The legacy limit argument must not hide any physical State field.
	features := activationFeaturesForMemory(m, 1)
	for _, want := range []string{
		"id:memory.large-state",
		"tag:alpha",
		"trigger:event:alpha",
		"state-key:field.191",
		"state-kv:field.191=value.191",
	} {
		if !containsString(features, want) {
			t.Fatalf("missing exact physical feature %q", want)
		}
	}
}

func writeIndexedStoreForTest(t *testing.T, memories []*Memory) (*zip.ReadCloser, string, StoreManifest) {
	t.Helper()
	records, ids, tags, lists, err := buildIndexedSections(memories)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "Memory.mem")
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	zw := zip.NewWriter(f)
	write := func(name string, data []byte) {
		t.Helper()
		h := &zip.FileHeader{Name: name, Method: zip.Store}
		w, er := zw.CreateHeader(h)
		if er != nil {
			t.Fatal(er)
		}
		if _, er = w.Write(data); er != nil {
			t.Fatal(er)
		}
	}
	write("records.bin", records)
	write("ids.bin", ids)
	write("tags.bin", tags)
	write("taglists.bin", lists)
	if err = zw.Close(); err != nil {
		t.Fatal(err)
	}
	if err = f.Close(); err != nil {
		t.Fatal(err)
	}
	zr, err := zip.OpenReader(path)
	if err != nil {
		t.Fatal(err)
	}
	return zr, path, StoreManifest{
		Records: "records.bin", IDIndex: "ids.bin", TagIndex: "tags.bin", TagLists: "taglists.bin",
		IndexFormat: indexFormatV2, MemoryCount: len(memories),
	}
}

func TestPersistedPhysicalIndexAvoidsStateTruncation(t *testing.T) {
	m := &Memory{
		ID:      "memory.persisted",
		Tags:    []string{"alpha"},
		Trigger: []string{"event:persisted"},
		State:   map[string]any{},
	}
	for i := 0; i < 192; i++ {
		m.State[fmt.Sprintf("field.%03d", i)] = fmt.Sprintf("value.%03d", i)
	}

	zr, path, manifest := writeIndexedStoreForTest(t, []*Memory{m})
	defer zr.Close()
	store, err := openIndexedStore(path, zr, manifest)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	has, err := store.HasPhysicalFeatureIndex()
	if err != nil {
		t.Fatal(err)
	}
	if !has {
		t.Fatal("persisted physical feature index not detected")
	}

	for _, feature := range []string{
		"id:memory.persisted",
		"tag:alpha",
		"trigger:event:persisted",
		"state-key:field.191",
		"state-kv:field.191=value.191",
	} {
		ids, er := store.PhysicalFeatureIDs(feature)
		if er != nil {
			t.Fatal(er)
		}
		if len(ids) != 1 || ids[0] != m.ID {
			t.Fatalf("physical feature %q resolved to %#v", feature, ids)
		}
	}
}
