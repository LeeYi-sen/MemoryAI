package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

func startTestMemNode(t *testing.T, root string) (host, port string) {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ln.Close() })
	go func() {
		for {
			c, er := ln.Accept()
			if er != nil {
				return
			}
			go handleMemNodeConn(root, c)
		}
	}()
	addr := ln.Addr().(*net.TCPAddr)
	return "127.0.0.1", strconv.Itoa(addr.Port)
}

func writeTestRemoteBody(t *testing.T, root, name string, memories []*Memory) string {
	t.Helper()
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(root, name)
	writeBodyForPersistenceTest(t, path, "storage", memories)
	e, err := loadEngine(path)
	if err != nil {
		t.Fatal(err)
	}
	bodyID := e.manifest.BodyID
	e.close()
	return bodyID
}

func decodeRemoteReply(t *testing.T, raw string) map[string]any {
	t.Helper()
	var out map[string]any
	if err := json.Unmarshal([]byte(raw), &out); err != nil {
		t.Fatal(err)
	}
	return out
}

func TestRuntimePlacementDoesNotMakeExpansionDecision(t *testing.T) {
	t.Setenv("MEMORYAI_SHARD_MAX_MEMORIES", "1")
	dir := t.TempDir()
	primary := filepath.Join(dir, "Memory.mem")
	root := &Memory{ID: "root", Layer: "inherited", Tags: []string{"memory"}, Revision: 1}
	writeBodyForPersistenceTest(t, primary, "core", []*Memory{root})
	e, err := loadEngine(primary)
	if err != nil {
		t.Fatal(err)
	}
	defer e.close()

	child := &Memory{ID: "child", Layer: "emergent", Tags: []string{"memory"}, Revision: 1}
	if err := e.placeRuntimeMemory(child); err == nil {
		t.Fatal("Kernel created storage capacity instead of failing closed for Memory-owned expansion")
	}
	if matches, err := filepath.Glob(filepath.Join(dir, "Memory.*.mem")); err != nil {
		t.Fatal(err)
	} else if len(matches) != 0 {
		t.Fatalf("Kernel created shard without Memory decision: %v", matches)
	}
	if _, _, err := e.resolveLocalFabricMemory(child.ID); !errors.Is(err, io.EOF) {
		t.Fatalf("capacity-rejected child leaked into Fabric: %v", err)
	}
}

func TestStructuralDigestIgnoresCapabilitySignatureEverywhere(t *testing.T) {
	a := &Memory{ID: "digest-unified", Layer: "emergent", Revision: 4, Tags: []string{"memory"}, CapabilitySig: "sig-a"}
	b := copyMemory(a)
	b.CapabilitySig = "sig-b"
	if structuralMemoryDigest(a) != structuralMemoryDigest(b) {
		t.Fatal("persistence structural digest still treats CapabilitySig as Memory identity")
	}
	if structuralMemoryDigest(a) != memoryJSONDigest(a) {
		t.Fatal("production structural digest definitions diverged")
	}
}

func TestRemoteMemoryGetRejectsPinnedBodyIdentityDrift(t *testing.T) {
	t.Setenv("MEMORYAI_STORAGE_CAPABILITY_KEY", "storage-test-key")
	root := t.TempDir()
	bodyID := writeTestRemoteBody(t, root, "remote.mem", []*Memory{{ID: "m", Layer: "emergent", Tags: []string{"memory"}, Revision: 1}})
	host, port := startTestMemNode(t, root)
	_, err := remoteMemoryGet(RemoteMemoryEndpoint{
		Host: host, Port: port, Name: "remote.mem", BodyID: bodyID + "-wrong", MemoryABI: memoryABI, Timeout: 2 * time.Second,
	}, "m")
	if err == nil {
		t.Fatal("remote direct read accepted endpoint BodyID drift")
	}
}

func TestResolveRemoteIDRejectsDivergentReachableReplicas(t *testing.T) {
	t.Setenv("MEMORYAI_STORAGE_CAPABILITY_KEY", "storage-test-key")
	rootA, rootB := t.TempDir(), t.TempDir()
	mA := &Memory{ID: "shared-id", Layer: "emergent", Tags: []string{"memory"}, State: map[string]any{"v": "a"}, Revision: 1}
	mB := copyMemory(mA)
	mB.State = map[string]any{"v": "b"}
	bodyA := writeTestRemoteBody(t, rootA, "a.mem", []*Memory{mA})
	bodyB := writeTestRemoteBody(t, rootB, "b.mem", []*Memory{mB})
	hostA, portA := startTestMemNode(t, rootA)
	hostB, portB := startTestMemNode(t, rootB)

	dir := t.TempDir()
	primary := filepath.Join(dir, "Memory.mem")
	descriptors := []*Memory{
		{ID: "ep-a", Layer: "emergent", Tags: []string{"memory", "physical.storage.remote.endpoint"}, Revision: 1, State: map[string]any{"host": hostA, "port": portA, "name": "a.mem", "body_id": bodyA, "memory_abi": memoryABI, "timeout_ms": "2000"}},
		{ID: "ep-b", Layer: "emergent", Tags: []string{"memory", "physical.storage.remote.endpoint"}, Revision: 1, State: map[string]any{"host": hostB, "port": portB, "name": "b.mem", "body_id": bodyB, "memory_abi": memoryABI, "timeout_ms": "2000"}},
	}
	writeBodyForPersistenceTest(t, primary, "core", descriptors)
	e, err := loadEngine(primary)
	if err != nil {
		t.Fatal(err)
	}
	defer e.close()

	if _, err := e.resolveRemoteID("shared-id"); err == nil || !strings.Contains(strings.ToLower(err.Error()), "conflict") {
		t.Fatalf("divergent reachable replicas did not fail closed: %v", err)
	}
}

func TestMemNodePutPreservesSameIDConflictForMemory(t *testing.T) {
	t.Setenv("MEMORYAI_STORAGE_CAPABILITY_KEY", "storage-test-key")
	root := t.TempDir()
	existing := &Memory{ID: "same-id", Layer: "emergent", Tags: []string{"memory"}, State: map[string]any{"v": "old"}, Revision: 1}
	writeTestRemoteBody(t, root, "remote.mem", []*Memory{existing})
	host, port := startTestMemNode(t, root)
	incoming := copyMemory(existing)
	incoming.State = map[string]any{"v": "new"}
	incoming.Revision = 2
	raw, err := remoteSpaceRequest(host, port, map[string]any{"op": "put", "name": "remote.mem", "memory": incoming}, 2*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	reply := decodeRemoteReply(t, raw)
	if reply["ok"] != false || reply["status"] != "conflict" {
		t.Fatalf("physical remote put did not preserve divergent same-ID conflict: %#v", reply)
	}
	if reply["existing_digest"] == "" || reply["incoming_digest"] == "" {
		t.Fatalf("conflict evidence missing structural digests: %#v", reply)
	}
}

func TestMemNodeDigestUsesUnifiedStructuralDigest(t *testing.T) {
	t.Setenv("MEMORYAI_STORAGE_CAPABILITY_KEY", "storage-test-key")
	root := t.TempDir()
	m := &Memory{ID: "digest-id", Layer: "emergent", Tags: []string{"memory"}, Revision: 1, RuntimeExecCount: 99, CapabilitySig: "physical-signature"}
	writeTestRemoteBody(t, root, "remote.mem", []*Memory{m})
	host, port := startTestMemNode(t, root)
	raw, err := remoteSpaceRequest(host, port, map[string]any{"op": "digest", "name": "remote.mem", "id": m.ID}, 2*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	reply := decodeRemoteReply(t, raw)
	if reply["digest"] != memoryJSONDigest(m) {
		t.Fatalf("remote digest is not unified structural digest: got=%v want=%s", reply["digest"], memoryJSONDigest(m))
	}
}

func TestMemNodeReplaceUsesBoundedTagSafeUpsert(t *testing.T) {
	t.Setenv("MEMORYAI_STORAGE_CAPABILITY_KEY", "storage-test-key")
	t.Setenv("MEMORYAI_SHARD_MAX_MEMORIES", "1")
	root := t.TempDir()
	existing := &Memory{ID: "replace-id", Layer: "emergent", Tags: []string{"memory", "old-tag"}, Revision: 1}
	writeTestRemoteBody(t, root, "remote.mem", []*Memory{existing})
	host, port := startTestMemNode(t, root)

	replacement := copyMemory(existing)
	replacement.Tags = []string{"memory", "new-tag"}
	replacement.Revision = 2
	observed := remoteTestDigest(t, host, port, "remote.mem", existing.ID)
	raw, err := remoteSpaceRequest(host, port, map[string]any{
		"op": "put", "name": "remote.mem", "memory": replacement, "replace": true, "expected_digest": observed,
	}, 2*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if reply := decodeRemoteReply(t, raw); reply["ok"] != true {
		t.Fatalf("explicit replace failed: %#v", reply)
	}
	oldRaw, err := remoteSpaceRequest(host, port, map[string]any{"op": "tag", "name": "remote.mem", "id": "old-tag"}, 2*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	newRaw, err := remoteSpaceRequest(host, port, map[string]any{"op": "tag", "name": "remote.mem", "id": "new-tag"}, 2*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	oldReply, newReply := decodeRemoteReply(t, oldRaw), decodeRemoteReply(t, newRaw)
	oldIDs, _ := oldReply["ids"].([]any)
	newIDs, _ := newReply["ids"].([]any)
	if len(oldIDs) != 0 || len(newIDs) != 1 || newIDs[0] != existing.ID {
		t.Fatalf("tag indexes drifted after remote replace: old=%#v new=%#v", oldReply, newReply)
	}

	extra := &Memory{ID: "overflow", Layer: "emergent", Tags: []string{"memory"}, Revision: 1}
	raw, err = remoteSpaceRequest(host, port, map[string]any{"op": "put", "name": "remote.mem", "memory": extra}, 2*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if reply := decodeRemoteReply(t, raw); reply["ok"] != false {
		t.Fatalf("full remote body accepted unbounded new Memory: %#v", reply)
	}
}

func TestRemoteStorageRejectsUnsignedMutation(t *testing.T) {
	t.Setenv("MEMORYAI_STORAGE_CAPABILITY_KEY", "storage-auth-key")
	root := t.TempDir()
	victim := &Memory{ID: "victim", Layer: "emergent", Tags: []string{"memory"}, Revision: 1}
	writeTestRemoteBody(t, root, "remote.mem", []*Memory{victim})
	host, port := startTestMemNode(t, root)

	conn, err := net.DialTimeout("tcp", net.JoinHostPort(host, port), 2*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	_ = conn.SetDeadline(time.Now().Add(2 * time.Second))
	if err := json.NewEncoder(conn).Encode(map[string]any{"op": "delete", "name": "remote.mem", "id": victim.ID}); err != nil {
		_ = conn.Close()
		t.Fatal(err)
	}
	var env storageWireEnvelope
	if err := json.NewDecoder(conn).Decode(&env); err != nil {
		_ = conn.Close()
		t.Fatal(err)
	}
	_ = conn.Close()
	body, err := storageEnvelopeBody(env)
	if err != nil {
		t.Fatal(err)
	}
	var rejected map[string]any
	if err := json.Unmarshal(body, &rejected); err != nil {
		t.Fatal(err)
	}
	if rejected["ok"] != false {
		t.Fatalf("unsigned storage mutation was not rejected: %#v", rejected)
	}

	raw, err := remoteSpaceRequest(host, port, map[string]any{"op": "get", "name": "remote.mem", "id": victim.ID}, 2*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if got := decodeRemoteReply(t, raw); got["ok"] != true {
		t.Fatalf("unsigned delete changed remote Memory: %#v", got)
	}
}

func TestRemoteStorageCrossHostRequiresTLS(t *testing.T) {
	t.Setenv("MEMORYAI_STORAGE_TLS_CERT", "")
	t.Setenv("MEMORYAI_STORAGE_TLS_KEY", "")
	if useTLS, err := storageListenUsesTLS("127.0.0.1:9999"); err != nil || useTLS {
		t.Fatalf("loopback storage listener should permit authenticated plaintext: tls=%v err=%v", useTLS, err)
	}
	if _, err := storageListenUsesTLS("0.0.0.0:9999"); err == nil {
		t.Fatal("non-loopback storage listener accepted plaintext transport")
	}
}

func TestRemotePutCannotImplicitlyCreateStorageBody(t *testing.T) {
	t.Setenv("MEMORYAI_STORAGE_CAPABILITY_KEY", "storage-create-key")
	root := t.TempDir()
	host, port := startTestMemNode(t, root)
	m := &Memory{ID: "new", Layer: "emergent", Tags: []string{"memory"}, Revision: 1}
	raw, err := remoteSpaceRequest(host, port, map[string]any{"op": "put", "name": "missing.mem", "memory": m}, 2*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if got := decodeRemoteReply(t, raw); got["ok"] != false {
		t.Fatalf("put implicitly created remote storage body: %#v", got)
	}
	if _, err := os.Stat(filepath.Join(root, "missing.mem")); !os.IsNotExist(err) {
		t.Fatalf("implicit remote body exists after rejected put: %v", err)
	}
}

func TestRemoteExplicitCreateHonorsRaisedFreeSpaceFloor(t *testing.T) {
	t.Setenv("MEMORYAI_STORAGE_CAPABILITY_KEY", "storage-floor-key")
	root := t.TempDir()
	free, err := physicalFreeBytes(filepath.Join(root, "new.mem"))
	if err != nil {
		t.Fatal(err)
	}
	floor := free + 1
	if floor < minimumAutomaticShardFreeBytes {
		floor = minimumAutomaticShardFreeBytes
	}
	if floor <= free {
		t.Skip("filesystem free-space value cannot be exceeded safely on this runner")
	}
	t.Setenv("MEMORYAI_SHARD_MIN_FREE_BYTES", fmt.Sprint(floor))
	host, port := startTestMemNode(t, root)
	raw, err := remoteSpaceRequest(host, port, map[string]any{"op": "create", "name": "new.mem"}, 2*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if got := decodeRemoteReply(t, raw); got["ok"] != false {
		t.Fatalf("remote create ignored physical free-space floor: %#v", got)
	}
	if _, err := os.Stat(filepath.Join(root, "new.mem")); !os.IsNotExist(err) {
		t.Fatalf("remote body exists despite failed physical floor: %v", err)
	}
}

func TestDurableTransferRejectsDivergentSameIDWithoutBranching(t *testing.T) {
	t.Setenv("MEMORYAI_SHARD_MAX_MEMORIES", "8")
	dir := t.TempDir()
	primary := filepath.Join(dir, "Memory.mem")
	target := filepath.Join(dir, "target.mem")
	source := &Memory{
		ID: "collision", Layer: "emergent", Tags: []string{"memory"},
		State: map[string]any{"v": "source"}, Revision: 2,
	}
	existing := &Memory{
		ID: "collision", Layer: "emergent", Tags: []string{"memory"},
		State: map[string]any{"v": "target"}, Revision: 2,
	}
	writeBodyForPersistenceTest(t, primary, "core", []*Memory{source})
	writeBodyForPersistenceTest(t, target, "storage", []*Memory{existing})
	e, err := loadEngine(primary)
	if err != nil {
		t.Fatal(err)
	}
	_, err = transferMemoryDurable(e, source.ID, target, false)
	e.close()
	if err == nil || !strings.Contains(strings.ToLower(err.Error()), "conflict") {
		t.Fatalf("divergent same-ID transfer did not fail closed: %v", err)
	}

	srcCheck, err := loadEngine(primary)
	if err != nil {
		t.Fatal(err)
	}
	gotSource, err := srcCheck.resolveIDLocal(source.ID)
	if err != nil {
		srcCheck.close()
		t.Fatal(err)
	}
	if gotSource.State["v"] != "source" {
		srcCheck.close()
		t.Fatalf("source changed after rejected conflict: %#v", gotSource.State)
	}
	srcCheck.close()

	dstCheck, err := loadEngine(target)
	if err != nil {
		t.Fatal(err)
	}
	defer dstCheck.close()
	ids, err := dstCheck.localIDs()
	if err != nil {
		t.Fatal(err)
	}
	if len(ids) != 1 || ids[0] != existing.ID {
		t.Fatalf("rejected transfer invented a branch identity: %v", ids)
	}
	gotTarget, err := dstCheck.resolveIDLocal(existing.ID)
	if err != nil {
		t.Fatal(err)
	}
	if gotTarget.State["v"] != "target" {
		t.Fatalf("target changed after rejected conflict: %#v", gotTarget.State)
	}
}

func TestExternalRequestLifecycleClearsStaleCapabilitySignature(t *testing.T) {
	m := &Memory{
		ID: "io.request.signed", Layer: "emergent", Revision: 7,
		Tags:          []string{"memory", externalRequestPendingTag},
		CapabilitySig: "stale-signature",
	}
	q := replaceExternalRequestLifecycle(m, "executing", externalRequestExecutingTag, nil)
	if q.Revision != 8 {
		t.Fatalf("lifecycle revision did not advance: %d", q.Revision)
	}
	if q.CapabilitySig != "" {
		t.Fatalf("lifecycle retained stale CapabilitySig: %q", q.CapabilitySig)
	}
}

func TestStructureSyncRevisionBumpClearsStaleCapabilitySignature(t *testing.T) {
	e := loadCurrentBodyForGrowthTest(t)
	current := &Memory{
		ID: "sync.signed", Layer: "emergent", Revision: 5,
		Tags: []string{"memory"}, State: map[string]any{"v": "old"},
	}
	e.addRuntimeMemory(current)

	incoming := &Memory{
		ID: current.ID, Layer: "emergent", Revision: 1,
		Tags: []string{"memory"}, State: map[string]any{"v": "new"},
		CapabilitySig: "incoming-signature-for-old-revision",
	}
	raw, err := json.Marshal([]*Memory{incoming})
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "required.json")
	if err := os.WriteFile(path, raw, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := e.syncRequiredStructures(path); err != nil {
		t.Fatal(err)
	}
	got, err := e.resolve(current.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Revision != 6 {
		t.Fatalf("sync did not enforce monotonic revision: %d", got.Revision)
	}
	if got.CapabilitySig != "" {
		t.Fatalf("sync revision bump retained stale CapabilitySig: %q", got.CapabilitySig)
	}
}

func TestSingleTargetResolutionRejectsAmbiguousTag(t *testing.T) {
	e := loadCurrentBodyForGrowthTest(t)
	tag := "test.ambiguous.single-target"
	a := &Memory{ID: "ambiguous.a", Layer: "emergent", Tags: []string{"memory", tag}, Revision: 1, Program: []Op{{Code: "halt"}}}
	b := &Memory{ID: "ambiguous.b", Layer: "emergent", Tags: []string{"memory", tag}, Revision: 1, Program: []Op{{Code: "halt"}}}
	e.addRuntimeMemory(a)
	e.addRuntimeMemory(b)

	if _, err := e.resolve(tag); err == nil || !strings.Contains(strings.ToLower(err.Error()), "ambiguous") {
		t.Fatalf("read resolution silently selected one of multiple tag matches: %v", err)
	}
	if _, err := e.resolveMutable(tag); err == nil || !strings.Contains(strings.ToLower(err.Error()), "ambiguous") {
		t.Fatalf("mutable resolution silently selected one of multiple tag matches: %v", err)
	}
	if err := e.run(tag, newFrame()); err == nil || !strings.Contains(strings.ToLower(err.Error()), "ambiguous") {
		t.Fatalf("execution silently selected one of multiple tag matches: %v", err)
	}
	if got, err := e.resolve(a.ID); err != nil || got.ID != a.ID {
		t.Fatalf("exact ID resolution regressed: got=%v err=%v", got, err)
	}
}

func remoteTestDigest(t *testing.T, host, port, name, id string) string {
	t.Helper()
	raw, err := remoteSpaceRequest(host, port, map[string]any{"op": "digest", "name": name, "id": id}, 2*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	reply := decodeRemoteReply(t, raw)
	if reply["ok"] != true {
		t.Fatalf("remote digest unavailable: %#v", reply)
	}
	digest, _ := reply["digest"].(string)
	if digest == "" {
		t.Fatalf("remote digest empty: %#v", reply)
	}
	return digest
}

func TestRemoteReplaceRejectsStaleObservedDigestAndRevisionRollback(t *testing.T) {
	t.Setenv("MEMORYAI_STORAGE_CAPABILITY_KEY", "storage-cas-key")
	root := t.TempDir()
	base := &Memory{ID: "cas-id", Layer: "emergent", Tags: []string{"memory"}, State: map[string]any{"v": "1"}, Revision: 1}
	writeTestRemoteBody(t, root, "remote.mem", []*Memory{base})
	host, port := startTestMemNode(t, root)
	observed := remoteTestDigest(t, host, port, "remote.mem", base.ID)

	newer := copyMemory(base)
	newer.State = map[string]any{"v": "3"}
	newer.Revision = 3
	raw, err := remoteSpaceRequest(host, port, map[string]any{
		"op": "put", "name": "remote.mem", "memory": newer, "replace": true, "expected_digest": observed,
	}, 2*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if got := decodeRemoteReply(t, raw); got["ok"] != true {
		t.Fatalf("fresh CAS replace failed: %#v", got)
	}

	stale := copyMemory(base)
	stale.State = map[string]any{"v": "2"}
	stale.Revision = 2
	raw, err = remoteSpaceRequest(host, port, map[string]any{
		"op": "put", "name": "remote.mem", "memory": stale, "replace": true, "expected_digest": observed,
	}, 2*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if got := decodeRemoteReply(t, raw); got["ok"] != false {
		t.Fatalf("stale remote replace rolled newer state backward: %#v", got)
	}

	raw, err = remoteSpaceRequest(host, port, map[string]any{"op": "get", "name": "remote.mem", "id": base.ID}, 2*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	got := decodeRemoteReply(t, raw)
	mem, _ := got["memory"].(map[string]any)
	if fmt.Sprint(mem["revision"]) != "3" {
		t.Fatalf("remote revision rolled backward after stale replace: %#v", got)
	}
}

func TestRemoteDeleteRejectsStaleObservedDigest(t *testing.T) {
	t.Setenv("MEMORYAI_STORAGE_CAPABILITY_KEY", "storage-delete-cas-key")
	root := t.TempDir()
	base := &Memory{ID: "delete-cas-id", Layer: "emergent", Tags: []string{"memory"}, State: map[string]any{"v": "1"}, Revision: 1}
	writeTestRemoteBody(t, root, "remote.mem", []*Memory{base})
	host, port := startTestMemNode(t, root)
	observed := remoteTestDigest(t, host, port, "remote.mem", base.ID)

	newer := copyMemory(base)
	newer.State = map[string]any{"v": "2"}
	newer.Revision = 2
	raw, err := remoteSpaceRequest(host, port, map[string]any{
		"op": "put", "name": "remote.mem", "memory": newer, "replace": true, "expected_digest": observed,
	}, 2*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if got := decodeRemoteReply(t, raw); got["ok"] != true {
		t.Fatalf("fresh replace failed: %#v", got)
	}

	raw, err = remoteSpaceRequest(host, port, map[string]any{
		"op": "delete", "name": "remote.mem", "id": base.ID, "expected_digest": observed,
	}, 2*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if got := decodeRemoteReply(t, raw); got["ok"] != false {
		t.Fatalf("stale delete removed a newer remote Memory: %#v", got)
	}
	raw, err = remoteSpaceRequest(host, port, map[string]any{"op": "get", "name": "remote.mem", "id": base.ID}, 2*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if got := decodeRemoteReply(t, raw); got["ok"] != true {
		t.Fatalf("newer remote Memory disappeared after stale delete: %#v", got)
	}
}

func TestMemoryNewRejectsUnresolvedParent(t *testing.T) {
	e := loadCurrentBodyForGrowthTest(t)
	before := fabricMemoryCountFast(e)
	f := newFrame()
	op := Op{
		Code: "memory_new", A: "created",
		Args: map[string]string{
			"layer": "emergent", "parents": "definitely-missing-parent",
			"tags": "memory,test-lineage", "content": "must not be created",
		},
	}
	if _, err := e.execPrimitive(&Memory{ID: "test.creator"}, op, f, 0, nil); err == nil {
		t.Fatal("memory_new accepted an unresolved parent and created dangling lineage")
	}
	if f.Vars["created"] != "" {
		t.Fatalf("memory_new returned child id despite unresolved parent: %q", f.Vars["created"])
	}
	if after := fabricMemoryCountFast(e); after != before {
		t.Fatalf("memory_new changed Fabric after unresolved parent: before=%d after=%d", before, after)
	}
}
