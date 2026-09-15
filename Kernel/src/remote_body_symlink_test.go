package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestPassiveSymlinkPersistenceTargetsRealBody(t *testing.T) {
	dir := t.TempDir()
	realPath := filepath.Join(dir, "Memory.real.mem")
	aliasPath := filepath.Join(dir, "Memory.alias.mem")
	writeRemoteTxnBody(t, realPath, "remote.symlink.persist", "before")
	if err := os.Symlink(realPath, aliasPath); err != nil {
		t.Fatal(err)
	}

	passive, err := loadPassiveStorageSerialized(aliasPath)
	if err != nil {
		t.Fatal(err)
	}
	if passive.bodyPath != canonicalPhysicalBodyPath(realPath) {
		passive.close()
		t.Fatalf("passive Engine kept alias bodyPath: got=%q want=%q", passive.bodyPath, canonicalPhysicalBodyPath(realPath))
	}
	current, err := passive.resolveIDLocal("remote.symlink.persist")
	if err != nil {
		passive.close()
		t.Fatal(err)
	}
	q := copyMemory(current)
	q.State["value"] = "after"
	q.Revision++
	if err := upsertExplicitMemoryOnOwner(passive, q); err != nil {
		passive.close()
		t.Fatal(err)
	}
	if err := persistEngineIfDirty(passive); err != nil {
		passive.close()
		t.Fatal(err)
	}
	passive.close()

	info, err := os.Lstat(aliasPath)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode()&os.ModeSymlink == 0 {
		t.Fatal("atomic persistence replaced symlink alias instead of real Memory body")
	}

	verify, err := loadPassiveStorageSerialized(realPath)
	if err != nil {
		t.Fatal(err)
	}
	defer verify.close()
	got, err := verify.resolveIDLocal("remote.symlink.persist")
	if err != nil {
		t.Fatal(err)
	}
	if got.State["value"] != "after" || got.Revision != 2 {
		t.Fatalf("real body did not receive symlink-opened persistence: state=%v revision=%d", got.State, got.Revision)
	}
}
