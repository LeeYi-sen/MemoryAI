package main

import (
	"path/filepath"
	"testing"
)

func TestLegacyStorageNamespaceMigratesToPhysicalTags(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "Memory.mem")
	endpoint := &Memory{
		ID: "legacy.endpoint", Layer: "emergent", Revision: 1,
		Tags:  []string{"memory", "cog.storage.remote.endpoint"},
		State: map[string]any{"host": "127.0.0.1", "port": "9999", "name": "remote-a"},
	}
	replica := &Memory{
		ID: "legacy.replica", Layer: "emergent", Revision: 1,
		Tags:  []string{"memory", "cog.storage.replica.of.memory-x"},
		State: map[string]any{"host": "127.0.0.1", "port": "9999", "name": "remote-a"},
	}
	writeBodyForPersistenceTest(t, path, "core", []*Memory{endpoint, replica})
	e, err := loadEngineWithMutationJournal(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := migrateLegacyRuntimeState(e); err != nil {
		e.close()
		t.Fatal(err)
	}
	for id, want := range map[string]string{
		endpoint.ID: "physical.storage.remote.endpoint",
		replica.ID:  "physical.storage.replica.of.memory-x",
	} {
		got, err := e.resolveIDLocal(id)
		if err != nil {
			e.close()
			t.Fatal(err)
		}
		if !contains(got.Tags, want) {
			e.close()
			t.Fatalf("%s did not receive physical namespace: %v", id, got.Tags)
		}
		for _, tag := range got.Tags {
			if tag == "cog.storage.remote.endpoint" || tag == "cog.storage.replica.of.memory-x" {
				e.close()
				t.Fatalf("%s retained legacy cognitive storage namespace: %v", id, got.Tags)
			}
		}
	}
	if eps := e.remoteMemoryEndpoints(); len(eps) != 1 || eps[0].DescriptorID != endpoint.ID {
		e.close()
		t.Fatalf("migrated endpoint not discoverable through physical namespace: %+v", eps)
	}
	if eps := e.knownRemoteReplicaEndpoints("memory-x"); len(eps) != 1 {
		e.close()
		t.Fatalf("migrated replica descriptor not discoverable: %+v", eps)
	}
	e.close()

	restored, err := loadEngineWithMutationJournal(path)
	if err != nil {
		t.Fatal(err)
	}
	defer restored.close()
	got, err := restored.resolveIDLocal(endpoint.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !contains(got.Tags, "physical.storage.remote.endpoint") {
		t.Fatalf("namespace migration did not survive restart: %v", got.Tags)
	}
}
