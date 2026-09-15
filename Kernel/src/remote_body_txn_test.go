package main

import (
	"path/filepath"
	"testing"
	"time"
)

func writeRemoteTxnBody(t *testing.T, path, id, value string) {
	t.Helper()
	writeBodyForPersistenceTest(t, path, "storage", []*Memory{{
		ID: id, Layer: "emergent", Tags: []string{"memory", "remote-txn-test"},
		State: map[string]any{"value": value}, Revision: 1,
	}})
}

func loadRemoteTxnRoot(t *testing.T, dir string) *Engine {
	t.Helper()
	primary := filepath.Join(dir, "Memory.mem")
	writeBodyForPersistenceTest(t, primary, "core", []*Memory{{
		ID: "root", Layer: "inherited", Tags: []string{"memory"}, State: map[string]any{}, Revision: 1,
	}})
	e, err := loadEngine(primary)
	if err != nil {
		t.Fatal(err)
	}
	_ = fabricRootFor(e)
	t.Cleanup(e.close)
	return e
}

func TestPassiveBodyTransactionSerializesLoadThroughClose(t *testing.T) {
	path := filepath.Join(t.TempDir(), "Memory.remote.mem")
	writeRemoteTxnBody(t, path, "remote.txn.target", "before")

	first, err := loadPassiveStorageSerialized(path)
	if err != nil {
		t.Fatal(err)
	}
	closedFirst := false
	defer func() {
		if !closedFirst {
			first.close()
		}
	}()

	type loadResult struct {
		e   *Engine
		err error
	}
	secondCh := make(chan loadResult, 1)
	go func() {
		e, er := loadPassiveStorageSerialized(path)
		secondCh <- loadResult{e: e, err: er}
	}()

	select {
	case r := <-secondCh:
		if r.e != nil {
			r.e.close()
		}
		t.Fatalf("second passive load entered before first close: %v", r.err)
	case <-time.After(100 * time.Millisecond):
		// Expected: the body-path lease spans the first Engine lifetime.
	}

	current, err := first.resolveIDLocal("remote.txn.target")
	if err != nil {
		t.Fatal(err)
	}
	q := copyMemory(current)
	q.State["value"] = "after"
	q.Revision++
	if err := upsertExplicitMemoryOnOwner(first, q); err != nil {
		t.Fatal(err)
	}
	if err := persistEngineIfDirty(first); err != nil {
		t.Fatal(err)
	}
	first.close()
	closedFirst = true

	select {
	case r := <-secondCh:
		if r.err != nil {
			t.Fatal(r.err)
		}
		defer r.e.close()
		got, err := r.e.resolveIDLocal("remote.txn.target")
		if err != nil {
			t.Fatal(err)
		}
		if got.State["value"] != "after" || got.Revision != 2 {
			t.Fatalf("second passive load observed stale body: state=%v revision=%d", got.State, got.Revision)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("second passive load did not acquire lease after first close")
	}
}

func TestPassiveBodyTransactionsDoNotSerializeDifferentBodies(t *testing.T) {
	dir := t.TempDir()
	pathA := filepath.Join(dir, "Memory.A.mem")
	pathB := filepath.Join(dir, "Memory.B.mem")
	writeRemoteTxnBody(t, pathA, "remote.txn.a", "a")
	writeRemoteTxnBody(t, pathB, "remote.txn.b", "b")

	a, err := loadPassiveStorageSerialized(pathA)
	if err != nil {
		t.Fatal(err)
	}
	defer a.close()

	done := make(chan error, 1)
	var b *Engine
	go func() {
		var er error
		b, er = loadPassiveStorageSerialized(pathB)
		done <- er
	}()

	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
		defer b.close()
	case <-time.After(500 * time.Millisecond):
		t.Fatal("different passive body paths were globally serialized")
	}
}

func TestPassiveBodyTransactionReleasesLeaseAfterLoadFailure(t *testing.T) {
	path := filepath.Join(t.TempDir(), "missing.mem")
	for i := 0; i < 2; i++ {
		done := make(chan error, 1)
		go func() {
			_, err := loadPassiveStorageSerialized(path)
			done <- err
		}()
		select {
		case err := <-done:
			if err == nil {
				t.Fatal("missing passive body unexpectedly loaded")
			}
		case <-time.After(500 * time.Millisecond):
			t.Fatal("failed passive load leaked physical body transaction lease")
		}
	}
}

func TestPassiveBodyRejectsSecondEngineWhileMounted(t *testing.T) {
	dir := t.TempDir()
	e := loadRemoteTxnRoot(t, dir)
	path := filepath.Join(dir, "RemoteStore.mem")
	writeRemoteTxnBody(t, path, "remote.mounted.target", "mounted")

	mounted, err := e.mountSpace(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := loadPassiveStorageSerialized(path); err == nil {
		t.Fatal("passive loader opened a second Engine for a live mounted body")
	}
	if !e.unmountSpace(mounted) {
		t.Fatal("failed to unmount test storage body")
	}

	passive, err := loadPassiveStorageSerialized(path)
	if err != nil {
		t.Fatalf("passive loader remained blocked after unmount: %v", err)
	}
	passive.close()
}

func TestMountWaitsForPassiveTransactionAndLoadsDurableResult(t *testing.T) {
	dir := t.TempDir()
	e := loadRemoteTxnRoot(t, dir)
	path := filepath.Join(dir, "RemoteStore.mem")
	writeRemoteTxnBody(t, path, "remote.mount.target", "before")

	passive, err := loadPassiveStorageSerialized(path)
	if err != nil {
		t.Fatal(err)
	}
	closedPassive := false
	defer func() {
		if !closedPassive {
			passive.close()
		}
	}()

	type mountResult struct {
		path string
		err  error
	}
	mountedCh := make(chan mountResult, 1)
	go func() {
		mounted, er := e.mountSpace(path)
		mountedCh <- mountResult{path: mounted, err: er}
	}()

	select {
	case r := <-mountedCh:
		t.Fatalf("mount read body while passive transaction was active: path=%q err=%v", r.path, r.err)
	case <-time.After(100 * time.Millisecond):
	}

	current, err := passive.resolveIDLocal("remote.mount.target")
	if err != nil {
		t.Fatal(err)
	}
	q := copyMemory(current)
	q.State["value"] = "after"
	q.Revision++
	if err := upsertExplicitMemoryOnOwner(passive, q); err != nil {
		t.Fatal(err)
	}
	if err := persistEngineIfDirty(passive); err != nil {
		t.Fatal(err)
	}
	passive.close()
	closedPassive = true

	select {
	case r := <-mountedCh:
		if r.err != nil {
			t.Fatal(r.err)
		}
		owner, got, err := e.resolveLocalFabricMemory("remote.mount.target")
		if err != nil {
			t.Fatal(err)
		}
		if owner == e || got.State["value"] != "after" || got.Revision != 2 {
			t.Fatalf("mount loaded stale or wrong owner after passive commit: owner=%p state=%v revision=%d", owner, got.State, got.Revision)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("mount did not proceed after passive transaction closed")
	}
}
