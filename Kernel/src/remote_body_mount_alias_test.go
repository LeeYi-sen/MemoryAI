package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestMountRejectsSymlinkAliasOfLivePhysicalBody(t *testing.T) {
	dir := t.TempDir()
	e := loadRemoteTxnRoot(t, dir)
	realPath := filepath.Join(dir, "Mounted.real.mem")
	aliasPath := filepath.Join(dir, "Mounted.alias.mem")
	writeRemoteTxnBody(t, realPath, "remote.mount.alias", "value")
	if err := os.Symlink(realPath, aliasPath); err != nil {
		t.Fatal(err)
	}

	mounted, err := e.mountSpace(realPath)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := e.mountSpace(aliasPath); err == nil {
		t.Fatal("symlink alias created a second Engine for an already mounted physical body")
	}
	if got := len(e.mountedSpacePaths()); got != 1 {
		t.Fatalf("duplicate alias mount changed Fabric topology: mounted=%d paths=%v", got, e.mountedSpacePaths())
	}
	owner, got, err := e.resolveLocalFabricMemory("remote.mount.alias")
	if err != nil {
		t.Fatal(err)
	}
	if owner == e || got.State["value"] != "value" {
		t.Fatalf("live mounted owner changed after alias rejection: owner=%p state=%v", owner, got.State)
	}
	if !e.unmountSpace(mounted) {
		t.Fatal("failed to unmount real physical body after alias rejection")
	}
}
