package main

import (
	"archive/zip"
	"bytes"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
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

func TestNextIDConcurrentUniqueness(t *testing.T) {
	const workers = 32
	const perWorker = 2000
	seen := make(map[string]struct{}, workers*perWorker)
	var mu sync.Mutex
	var wg sync.WaitGroup
	errs := make(chan string, workers)

	for w := 0; w < workers; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < perWorker; i++ {
				id := nextID("mem")
				if !strings.HasPrefix(id, "mem-") {
					errs <- "unexpected prefix: " + id
					return
				}
				mu.Lock()
				if _, exists := seen[id]; exists {
					mu.Unlock()
					errs <- "duplicate id: " + id
					return
				}
				seen[id] = struct{}{}
				mu.Unlock()
			}
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Fatal(err)
	}
	if len(seen) != workers*perWorker {
		t.Fatalf("identity count mismatch: got=%d want=%d", len(seen), workers*perWorker)
	}
}

func TestPhysicalExchangeRejectsOversizedResponse(t *testing.T) {
	t.Setenv("MEMORYAI_PHYSICAL_EXCHANGE_MAX_BYTES", "1024")
	if _, err := physicalExchange("tcp", "127.0.0.1", "1", strings.Repeat("q", 1025), time.Second); err == nil || !strings.Contains(strings.ToLower(err.Error()), "request exceeds") {
		t.Fatalf("oversized physical exchange request was not rejected before I/O: %v", err)
	}
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	go func() {
		c, er := ln.Accept()
		if er != nil {
			return
		}
		defer c.Close()
		_ = c.SetDeadline(time.Now().Add(2 * time.Second))
		buf := make([]byte, 16)
		_, _ = c.Read(buf)
		_, _ = c.Write([]byte(strings.Repeat("x", 2048)))
	}()
	addr := ln.Addr().(*net.TCPAddr)
	_, err = physicalExchange("tcp", "127.0.0.1", strconv.Itoa(addr.Port), "PING", 2*time.Second)
	if err == nil || !strings.Contains(strings.ToLower(err.Error()), "response exceeds") {
		t.Fatalf("oversized physical exchange was not rejected: %v", err)
	}
}

func TestArtifactPrimitivesEnforcePhysicalByteCeiling(t *testing.T) {
	t.Setenv("MEMORYAI_ARTIFACT_MAX_BYTES", "1024")
	dir := t.TempDir()
	body := filepath.Join(dir, "data", "Memory.mem")
	if err := os.MkdirAll(filepath.Dir(body), 0o755); err != nil {
		t.Fatal(err)
	}
	e := &Engine{bodyPath: body, cache: map[string]*Memory{}, newIDs: map[string]bool{}, dirtyIDs: map[string]bool{}, deletedIDs: map[string]bool{}, tagAdded: map[string]map[string]bool{}, tagRemoved: map[string]map[string]bool{}, spaces: map[string]*Engine{}, dataMu: &sync.RWMutex{}}
	t.Setenv("MEMORYAI_WORKSPACE", filepath.Join(dir, "workspace"))
	if err := os.MkdirAll(filepath.Join(dir, "workspace"), 0o755); err != nil {
		t.Fatal(err)
	}
	p := filepath.Join(dir, "workspace", "large.bin")
	if err := os.WriteFile(p, []byte(strings.Repeat("a", 2048)), 0o644); err != nil {
		t.Fatal(err)
	}
	f := newFrame()
	self := &Memory{ID: "artifact-test", Capabilities: []string{"artifact.read", "artifact.write"}}
	if _, err := e.execPrimitive(self, Op{Code: "artifact_read", A: "large.bin", B: "out"}, f, 0, nil); err == nil {
		t.Fatal("artifact_read accepted file above physical byte ceiling")
	}
	if _, err := e.execPrimitive(self, Op{Code: "artifact_digest", A: "large.bin", B: "digest"}, f, 0, nil); err == nil {
		t.Fatal("artifact_digest accepted file above physical byte ceiling")
	}
	f.Vars["payload"] = strings.Repeat("b", 2048)
	if _, err := e.execPrimitive(self, Op{Code: "artifact_write", A: "too-large.bin", B: "{{payload}}"}, f, 0, nil); err == nil {
		t.Fatal("artifact_write accepted payload above physical byte ceiling")
	}
	if _, err := os.Stat(filepath.Join(dir, "workspace", "too-large.bin")); !os.IsNotExist(err) {
		t.Fatalf("oversized artifact write created file: %v", err)
	}
}

func TestArtifactPathRejectsSymlinkEscape(t *testing.T) {
	dir := t.TempDir()
	workspace := filepath.Join(dir, "workspace")
	outside := filepath.Join(dir, "outside")
	if err := os.MkdirAll(workspace, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(outside, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(outside, "secret.txt"), []byte("outside-secret"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(workspace, "escape")); err != nil {
		t.Fatal(err)
	}
	t.Setenv("MEMORYAI_WORKSPACE", workspace)
	e := &Engine{bodyPath: filepath.Join(dir, "data", "Memory.mem")}
	f := newFrame()
	self := &Memory{ID: "artifact-symlink", Capabilities: []string{"artifact.read", "artifact.write"}}

	if _, err := e.execPrimitive(self, Op{Code: "artifact_read", A: "escape/secret.txt", B: "out"}, f, 0, nil); err == nil {
		t.Fatalf("artifact_read followed workspace symlink outside boundary: %q", f.Vars["out"])
	}
	f.Vars["payload"] = "must-not-escape"
	if _, err := e.execPrimitive(self, Op{Code: "artifact_write", A: "escape/new.txt", B: "{{payload}}"}, f, 0, nil); err == nil {
		t.Fatal("artifact_write followed workspace symlink outside boundary")
	}
	if _, err := os.Stat(filepath.Join(outside, "new.txt")); !os.IsNotExist(err) {
		t.Fatalf("artifact write escaped workspace via symlink: %v", err)
	}
}

func TestMemNodePathRejectsSymlinkEscape(t *testing.T) {
	t.Setenv("MEMORYAI_STORAGE_CAPABILITY_KEY", "storage-path-key")
	root := t.TempDir()
	outside := t.TempDir()
	m := &Memory{ID: "outside-memory", Layer: "emergent", Tags: []string{"memory"}, Revision: 1}
	writeTestRemoteBody(t, outside, "outside.mem", []*Memory{m})
	if err := os.Symlink(outside, filepath.Join(root, "escape")); err != nil {
		t.Fatal(err)
	}
	host, port := startTestMemNode(t, root)
	raw, err := remoteSpaceRequest(host, port, map[string]any{
		"op": "get", "name": "escape/outside.mem", "id": m.ID,
	}, 2*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	reply := decodeRemoteReply(t, raw)
	if reply["ok"] == true {
		t.Fatalf("mem-node followed storage-root symlink outside boundary: %#v", reply)
	}
}

func TestSovereignGrantRejectsUnboundClaimedOriginNode(t *testing.T) {
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("MEMORYAI_MESH_SOVEREIGN_PRIVATE_KEY_B64", base64.StdEncoding.EncodeToString(priv))
	t.Setenv("MEMORYAI_MESH_SOVEREIGN_PUBLIC_KEY_B64", base64.StdEncoding.EncodeToString(pub))

	m := &meshRuntime{
		role:   "sovereign",
		nodeID: "sovereign",
		directory: map[string]MeshNode{
			"node-a": {ID: "node-a", Role: "node", Endpoint: "https://node-a.invalid"},
			"node-b": {ID: "node-b", Role: "node", Endpoint: "https://node-b.invalid"},
		},
		shared: map[string]MeshRecord{
			"shared-memory": {MemoryID: "shared-memory", OriginNode: "node-a", Endpoint: "https://node-a.invalid", Digest: "abc"},
		},
	}
	res := m.handleAuthority(MeshRequest{
		Op: "shared_grant", MemoryID: "shared-memory",
		OriginNode: "node-a", AuthenticatedNodeID: "node-b",
		Vars: map[string]string{"operation": "shared_fetch"},
	})
	if res.OK {
		t.Fatalf("sovereign issued grant for claimed origin node without binding requester identity: %#v", res.Grant)
	}
}

func TestSovereignNodeIdentityRejectsPublicKeyRebind(t *testing.T) {
	e := loadCurrentBodyForGrowthTest(t)
	pubA, _, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	pubB, _, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	m := &meshRuntime{
		role: "sovereign", nodeID: "sovereign", engine: e,
		directory: map[string]MeshNode{
			"node-a": {ID: "node-a", Role: "node", PublicKey: base64.StdEncoding.EncodeToString(pubA)},
		},
		shared: map[string]MeshRecord{},
	}
	res := m.handleAuthority(MeshRequest{
		Op: "register_node", AuthenticatedNodeID: "node-a",
		Node: &MeshNode{ID: "node-a", Role: "node", PublicKey: base64.StdEncoding.EncodeToString(pubB)},
	})
	if res.OK || !strings.Contains(strings.ToLower(res.Error), "rebind") {
		t.Fatalf("sovereign accepted node identity public-key rebind: %#v", res)
	}
}

func TestMeshNodeRequestSignatureBindsNodeIdentity(t *testing.T) {
	pubA, privA, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	_, privB, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	m := &meshRuntime{
		role: "sovereign", nodeID: "sovereign",
		directory: map[string]MeshNode{
			"node-a": {ID: "node-a", Role: "node", PublicKey: base64.StdEncoding.EncodeToString(pubA)},
		},
		shared: map[string]MeshRecord{},
	}
	req := MeshRequest{Op: "directory"}
	body, err := json.Marshal(req)
	if err != nil {
		t.Fatal(err)
	}
	wrongSig := base64.StdEncoding.EncodeToString(ed25519.Sign(privB, body))
	if err := m.authenticateMeshHTTPRequest(&req, body, "node-a", wrongSig); err == nil {
		t.Fatal("shared transport member impersonated node-a with a different private key")
	}
	goodSig := base64.StdEncoding.EncodeToString(ed25519.Sign(privA, body))
	if err := m.authenticateMeshHTTPRequest(&req, body, "node-a", goodSig); err != nil {
		t.Fatalf("registered node identity signature rejected: %v", err)
	}
	if req.AuthenticatedNodeID != "node-a" {
		t.Fatalf("authenticated node identity not bound into request: %q", req.AuthenticatedNodeID)
	}
}

func TestDirectMeshGrantBindsOriginPublicKeyToRequesterSignature(t *testing.T) {
	sovereignPub, sovereignPriv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("MEMORYAI_MESH_SOVEREIGN_PRIVATE_KEY_B64", base64.StdEncoding.EncodeToString(sovereignPriv))
	t.Setenv("MEMORYAI_MESH_SOVEREIGN_PUBLIC_KEY_B64", base64.StdEncoding.EncodeToString(sovereignPub))
	originPub, originPriv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	_, attackerPriv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	grant, err := issueMeshGrant(
		"shared_fetch", "memory.x", "node-origin", "node-target",
		base64.StdEncoding.EncodeToString(originPub), 30*time.Second,
	)
	if err != nil {
		t.Fatal(err)
	}
	req := MeshRequest{Op: "shared_fetch", MemoryID: "memory.x", Grant: grant}
	body, err := json.Marshal(req)
	if err != nil {
		t.Fatal(err)
	}
	target := &meshRuntime{role: "node", nodeID: "node-target", directory: map[string]MeshNode{}, shared: map[string]MeshRecord{}}
	attackerSig := base64.StdEncoding.EncodeToString(ed25519.Sign(attackerPriv, body))
	if err := target.authenticateMeshHTTPRequest(&req, body, "node-origin", attackerSig); err == nil {
		t.Fatal("direct Mesh request accepted signature not matching Sovereign-bound origin public key")
	}
	originSig := base64.StdEncoding.EncodeToString(ed25519.Sign(originPriv, body))
	if err := target.authenticateMeshHTTPRequest(&req, body, "node-origin", originSig); err != nil {
		t.Fatalf("Sovereign-bound origin signature rejected: %v", err)
	}
}

func TestReadJSONZipRejectsOversizedMetadataEntry(t *testing.T) {
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	w, err := zw.Create("manifest.json")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := w.Write(bytes.Repeat([]byte("x"), int(hardJSONZipEntryMaxBytes)+1)); err != nil {
		t.Fatal(err)
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	zr, err := zip.NewReader(bytes.NewReader(buf.Bytes()), int64(buf.Len()))
	if err != nil {
		t.Fatal(err)
	}
	var dst map[string]any
	err = readJSONZip(zr, "manifest.json", &dst)
	if err == nil || !strings.Contains(strings.ToLower(err.Error()), "exceeds physical byte ceiling") {
		t.Fatalf("oversized Memory metadata was not rejected before decode: %v", err)
	}
}

func TestIndexedStoreRejectsRecordSliceBeforeAllocation(t *testing.T) {
	path := filepath.Join(t.TempDir(), "store.bin")
	if err := os.WriteFile(path, make([]byte, 64), 0o644); err != nil {
		t.Fatal(err)
	}
	file, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	store := &IndexedStore{file: file, records: section{off: 0, size: 64}}

	if _, err := store.readRecord(indexEntry{off: 60, length: 8}); err == nil || !strings.Contains(strings.ToLower(err.Error()), "escapes section") {
		t.Fatalf("record slice outside section was not rejected: %v", err)
	}
	if _, err := store.readRecord(indexEntry{off: 0, length: uint32(hardMemoryRecordMaxBytes + 1)}); err == nil || !strings.Contains(strings.ToLower(err.Error()), "byte ceiling") {
		t.Fatalf("oversized record was not rejected before allocation: %v", err)
	}
	if err := indexedSliceBounds(section{off: 0, size: int64(hardTagListPayloadMaxBytes + 2)}, 0, uint32(hardTagListPayloadMaxBytes+1), hardTagListPayloadMaxBytes, "tag-list payload"); err == nil {
		t.Fatal("oversized tag-list payload was not rejected before allocation")
	}
}

func TestMemoryZipRejectsDuplicateEntryNames(t *testing.T) {
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for _, payload := range []string{"{\"format\":\"first\"}", "{\"format\":\"second\"}"} {
		w, err := zw.Create("manifest.json")
		if err != nil {
			t.Fatal(err)
		}
		if _, err := w.Write([]byte(payload)); err != nil {
			t.Fatal(err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	zr, err := zip.NewReader(bytes.NewReader(buf.Bytes()), int64(buf.Len()))
	if err != nil {
		t.Fatal(err)
	}
	if err := validateUniqueZipEntryNames(zr); err == nil || !strings.Contains(strings.ToLower(err.Error()), "duplicate") {
		t.Fatalf("duplicate Memory ZIP entry names were not rejected: %v", err)
	}
	var manifest Manifest
	if err := readJSONZip(zr, "manifest.json", &manifest); err == nil || !strings.Contains(strings.ToLower(err.Error()), "duplicate") {
		t.Fatalf("duplicate manifest first-wins lookup survived: %v", err)
	}
}

func TestNormalLoadRejectsCorruptAuthenticatedIndex(t *testing.T) {
	path := filepath.Join(t.TempDir(), "Memory.mem")
	memory := &Memory{ID: "integrity.memory", Layer: "emergent", Tags: []string{"memory"}, Revision: 1}
	records, ididx, tagidx, taglists, err := buildIndexedSections([]*Memory{memory})
	if err != nil {
		t.Fatal(err)
	}
	genesis := Genesis{Format: "memoryai-genesis-index-v1", Root: memory.ID, MemoryCount: 1}
	gb, _ := json.Marshal(genesis)
	sm := StoreManifest{
		Records: "store/records.bin", IDIndex: "store/id.idx", TagIndex: "store/tag.idx",
		TagLists: "store/taglists.bin", IndexFormat: indexFormatV2, MemoryCount: 1,
	}
	entries := []zipEntry{
		{name: "genesis.json", b: gb},
		{name: sm.Records, b: records, store: true},
		{name: sm.IDIndex, b: ididx, store: true},
		{name: sm.TagIndex, b: tagidx, store: true},
		{name: sm.TagLists, b: taglists, store: true},
	}
	hashes := map[string]string{}
	for _, entry := range entries {
		sum := sha256.Sum256(entry.b)
		hashes[entry.name] = fmt.Sprintf("%x", sum[:])
	}
	hashes[sm.IDIndex] = strings.Repeat("0", 64)
	manifest := Manifest{
		Role: "core", Format: "memoryai-body-v2", Version: imageVersion, MemoryABI: memoryABI,
		BodyID: "integrity-test", Root: memory.ID, GenesisPath: "genesis.json",
		Entry: map[string]string{}, Hashes: hashes, Store: sm,
	}
	mb, _ := json.Marshal(manifest)
	entries = append(entries, zipEntry{name: "manifest.json", b: mb})
	if err := writeDetZip(path, entries); err != nil {
		t.Fatal(err)
	}
	if _, err := loadEngine(path); err == nil || !strings.Contains(strings.ToLower(err.Error()), "hash mismatch") {
		t.Fatalf("normal load accepted corrupt authenticated index: %v", err)
	}
}

func TestV2IndexedStoreLazilyRejectsCorruptRecordDigest(t *testing.T) {
	original := []byte(`{"id":"lazy.integrity","layer":"emergent","revision":1}`)
	path := filepath.Join(t.TempDir(), "records.bin")
	if err := os.WriteFile(path, original, 0o644); err != nil {
		t.Fatal(err)
	}
	file, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	digest := sha256.Sum256(original)
	store := &IndexedStore{
		file: file, records: section{off: 0, size: int64(len(original))},
		entrySize: indexEntrySize, hasDigests: true,
	}
	entry := indexEntry{off: 0, length: uint32(len(original)), digest: digest}
	got, err := store.readRecord(entry)
	if err != nil || got.ID != "lazy.integrity" {
		t.Fatalf("valid digest-backed record failed: got=%v err=%v", got, err)
	}
	if err := os.WriteFile(path, bytes.Repeat([]byte("x"), len(original)), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := store.readRecord(entry); err == nil || !strings.Contains(strings.ToLower(err.Error()), "digest mismatch") {
		t.Fatalf("corrupt record passed lazy digest verification: %v", err)
	}
}

func TestCanonicalMemoryWriteBudgetRejectsBeforeSecondMutation(t *testing.T) {
	e := loadCurrentBodyForGrowthTest(t)
	m := &Memory{
		ID: "budget.write.guard", Layer: "emergent", Revision: 1,
		Tags: []string{"memory"}, State: map[string]any{},
		Capabilities: []string{"memory.write"},
		Budget:       ResourceBudget{MaxMemoryWrites: 1},
		Program: []Op{
			{Code: "state_set", A: "budget.write.guard", B: "first", C: "1"},
			{Code: "state_set", A: "budget.write.guard", B: "second", C: "2"},
		},
	}
	e.addRuntimeMemory(m)
	f := newFrame()
	err := e.run(m.ID, f)
	if err == nil || !strings.Contains(strings.ToLower(err.Error()), "memory_writes") {
		t.Fatalf("expected memory-write budget error, got %v", err)
	}
	got, err := e.resolveIDLocal(m.ID)
	if err != nil {
		t.Fatal(err)
	}
	if fmt.Sprint(got.State["first"]) != "1" {
		t.Fatalf("first allowed mutation missing: %#v", got.State)
	}
	if _, exists := got.State["second"]; exists {
		t.Fatalf("budget-exceeding second mutation was applied before rejection: %#v", got.State)
	}
}

func TestNestedCallUsesCalleeResourceBudget(t *testing.T) {
	e := loadCurrentBodyForGrowthTest(t)
	child := &Memory{
		ID: "budget.child.writer", Layer: "emergent", Revision: 1,
		Tags: []string{"memory"}, State: map[string]any{},
		Capabilities: []string{"memory.write"},
		Budget:       ResourceBudget{MaxMemoryWrites: 1},
		Program: []Op{
			{Code: "state_set", A: "budget.child.writer", B: "first", C: "1"},
			{Code: "state_set", A: "budget.child.writer", B: "second", C: "2"},
		},
	}
	parent := &Memory{
		ID: "budget.parent.caller", Layer: "emergent", Revision: 1,
		Tags: []string{"memory"}, Program: []Op{{Code: "call", A: child.ID}},
	}
	e.addRuntimeMemory(child)
	e.addRuntimeMemory(parent)
	err := e.run(parent.ID, newFrame())
	if err == nil || !strings.Contains(strings.ToLower(err.Error()), "memory_writes") {
		t.Fatalf("callee write budget was not enforced: %v", err)
	}
	got, err := e.resolveIDLocal(child.ID)
	if err != nil {
		t.Fatal(err)
	}
	if fmt.Sprint(got.State["first"]) != "1" {
		t.Fatalf("first callee mutation missing: %#v", got.State)
	}
	if _, exists := got.State["second"]; exists {
		t.Fatalf("callee exceeded its own write budget before rejection: %#v", got.State)
	}
}

func TestCallerResourceBudgetDoesNotLeakIntoCallee(t *testing.T) {
	e := loadCurrentBodyForGrowthTest(t)
	child := &Memory{
		ID: "budget.child.independent", Layer: "emergent", Revision: 1,
		Tags: []string{"memory"}, State: map[string]any{},
		Capabilities: []string{"memory.write"},
		Program: []Op{
			{Code: "state_set", A: "budget.child.independent", B: "first", C: "1"},
			{Code: "state_set", A: "budget.child.independent", B: "second", C: "2"},
		},
	}
	parent := &Memory{
		ID: "budget.parent.independent", Layer: "emergent", Revision: 1,
		Tags: []string{"memory"}, Budget: ResourceBudget{MaxMemoryWrites: 1},
		Program: []Op{{Code: "call", A: child.ID}},
	}
	e.addRuntimeMemory(child)
	e.addRuntimeMemory(parent)
	if err := e.run(parent.ID, newFrame()); err != nil {
		t.Fatalf("caller write budget leaked into callee execution: %v", err)
	}
	got, err := e.resolveIDLocal(child.ID)
	if err != nil {
		t.Fatal(err)
	}
	if fmt.Sprint(got.State["first"]) != "1" || fmt.Sprint(got.State["second"]) != "2" {
		t.Fatalf("callee did not execute under its own budget: %#v", got.State)
	}
}

func TestEventBudgetRejectsBeforeSecondEmit(t *testing.T) {
	e := loadCurrentBodyForGrowthTest(t)
	m := &Memory{
		ID: "budget.event.guard", Layer: "emergent", Revision: 1,
		Tags: []string{"memory"}, Capabilities: []string{"event.emit"},
		Budget: ResourceBudget{MaxEvents: 1},
		Program: []Op{
			{Code: "emit_event", A: "test.budget.event.one"},
			{Code: "emit_event", A: "test.budget.event.two"},
		},
	}
	e.addRuntimeMemory(m)
	f := newFrame()
	err := e.run(m.ID, f)
	if err == nil || !strings.Contains(strings.ToLower(err.Error()), "events") {
		t.Fatalf("expected event budget error, got %v", err)
	}
	if f.eventCount != 1 || len(f.Events) != 1 {
		t.Fatalf("second event was emitted before budget rejection: count=%d events=%d", f.eventCount, len(f.Events))
	}
}

func TestDaemonConnectionRejectsOversizedRequest(t *testing.T) {
	t.Setenv("MEMORYAI_DAEMON_MAX_BYTES", "256")
	t.Setenv("MEMORYAI_DAEMON_TIMEOUT_MS", "1000")
	e := loadCurrentBodyForGrowthTest(t)
	server, client := net.Pipe()
	done := make(chan struct{})
	go func() {
		e.serveDaemonConn(server)
		close(done)
	}()
	go func() {
		payload, _ := json.Marshal(daemonRequest{Args: []string{"health", strings.Repeat("x", 1024)}})
		_, _ = client.Write(append(payload, '\n'))
	}()
	response, err := readAllPhysicalBounded(client, 1024, "daemon test response")
	if err != nil {
		t.Fatal(err)
	}
	<-done
	var res daemonResponse
	if err := json.Unmarshal(response, &res); err != nil {
		t.Fatal(err)
	}
	if res.OK {
		t.Fatalf("oversized daemon request was accepted: %#v", res)
	}
}

func TestMeshHTTPRejectsOversizedRequestBeforeAuthentication(t *testing.T) {
	t.Setenv("MEMORYAI_MESH_MAX_BYTES", "256")
	req := httptest.NewRequest(http.MethodPost, meshRPCPath, strings.NewReader(strings.Repeat("x", 1024)))
	rec := httptest.NewRecorder()
	m := &meshRuntime{}
	m.handleHTTP(rec, req)
	if rec.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("oversized Mesh request status=%d body=%s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(strings.ToLower(rec.Body.String()), "physical byte ceiling") {
		t.Fatalf("oversized Mesh request did not report physical ceiling: %s", rec.Body.String())
	}
}

func TestCanonicalEventBudgetRejectsBeforeSecondEmit(t *testing.T) {
	e := loadCurrentBodyForGrowthTest(t)
	m := &Memory{
		ID: "budget.event.guard", Layer: "emergent", Revision: 1,
		Tags: []string{"memory"}, Capabilities: []string{"event.emit"},
		Budget: ResourceBudget{MaxEvents: 1},
		Program: []Op{
			{Code: "emit_event", A: "budget.test.one"},
			{Code: "emit_event", A: "budget.test.two"},
		},
	}
	e.addRuntimeMemory(m)
	f := newFrame()
	err := e.run(m.ID, f)
	if err == nil || !strings.Contains(strings.ToLower(err.Error()), "events") {
		t.Fatalf("expected event budget error, got %v", err)
	}
	if len(f.Events) != 1 || f.Events[0].Name != "budget.test.one" {
		t.Fatalf("budget-exceeding event was emitted before rejection: %#v", f.Events)
	}
}

func TestCanonicalOpBudgetRejectsBeforeNextPrimitive(t *testing.T) {
	e := loadCurrentBodyForGrowthTest(t)
	m := &Memory{
		ID: "budget.ops.guard", Layer: "emergent", Revision: 1,
		Tags: []string{"memory"}, Budget: ResourceBudget{MaxOps: 1},
		Program: []Op{
			{Code: "set", A: "first", B: "1"},
			{Code: "set", A: "second", B: "2"},
		},
	}
	e.addRuntimeMemory(m)
	f := newFrame()
	err := e.run(m.ID, f)
	if err == nil || !strings.Contains(strings.ToLower(err.Error()), "ops") {
		t.Fatalf("expected op budget error, got %v", err)
	}
	if f.Vars["first"] != "1" || f.Vars["second"] != "" {
		t.Fatalf("budget-exceeding primitive executed: %#v", f.Vars)
	}
}

func TestDaemonResponseWriterEnforcesPhysicalCeiling(t *testing.T) {
	t.Setenv("MEMORYAI_DAEMON_MAX_BYTES", "256")
	var buf bytes.Buffer
	err := writeDaemonResponseBounded(&buf, daemonResponse{
		OK:   true,
		Data: map[string]any{"payload": strings.Repeat("x", 4096)},
	})
	if err != nil {
		t.Fatal(err)
	}
	if int64(buf.Len()) > daemonTransportMaxBytes() {
		t.Fatalf("daemon response exceeded physical ceiling: size=%d max=%d", buf.Len(), daemonTransportMaxBytes())
	}
	var res daemonResponse
	if err := json.Unmarshal(buf.Bytes(), &res); err != nil {
		t.Fatal(err)
	}
	if res.OK || !strings.Contains(strings.ToLower(res.Error), "physical byte ceiling") {
		t.Fatalf("oversized daemon response was not converted to bounded failure: %#v", res)
	}
}

func TestMeshResponseWriterEnforcesPhysicalCeiling(t *testing.T) {
	t.Setenv("MEMORYAI_MESH_MAX_BYTES", "256")
	rec := httptest.NewRecorder()
	writeMeshResponseBounded(rec, http.StatusOK, MeshResponse{
		OK:    true,
		Frame: &Frame{Vars: map[string]string{"payload": strings.Repeat("x", 4096)}},
	})
	if int64(rec.Body.Len()) > meshTransportMaxBytes() {
		t.Fatalf("Mesh response exceeded physical ceiling: size=%d max=%d", rec.Body.Len(), meshTransportMaxBytes())
	}
	if rec.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("oversized Mesh response status=%d body=%s", rec.Code, rec.Body.String())
	}
	var res MeshResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &res); err != nil {
		t.Fatal(err)
	}
	if res.OK || !strings.Contains(strings.ToLower(res.Error), "physical byte ceiling") {
		t.Fatalf("oversized Mesh response was not converted to bounded failure: %#v", res)
	}
}

func TestMeshHTTPServerCarriesPhysicalTimeouts(t *testing.T) {
	server := newMeshHTTPServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	if server.ReadHeaderTimeout != meshHTTPReadHeaderTimeout ||
		server.ReadTimeout != meshHTTPReadTimeout ||
		server.WriteTimeout != meshHTTPWriteTimeout ||
		server.IdleTimeout != meshHTTPIdleTimeout ||
		server.MaxHeaderBytes != meshHTTPMaxHeaderBytes {
		t.Fatalf("Mesh HTTP server physical limits missing: %#v", server)
	}
}

func TestEventFanoutDoesNotCreateGoroutinePerHandler(t *testing.T) {
	oldProcs := runtime.GOMAXPROCS(4)
	t.Cleanup(func() { runtime.GOMAXPROCS(oldProcs) })
	oldRuntime := globalParallelRuntime
	globalParallelRuntime = newParallelRuntime()
	setPhysicalExecutionConcurrency(2)
	t.Cleanup(func() { globalParallelRuntime = oldRuntime })
	oldActivation := globalActivationRuntime
	globalActivationRuntime = newSparseActivationRuntime()
	t.Cleanup(func() { globalActivationRuntime = oldActivation })

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	addr := ln.Addr().(*net.TCPAddr)
	release := make(chan struct{})
	accepted := make(chan struct{}, 64)
	go func() {
		for {
			c, er := ln.Accept()
			if er != nil {
				return
			}
			go func(conn net.Conn) {
				defer conn.Close()
				accepted <- struct{}{}
				buf := make([]byte, 8)
				_, _ = conn.Read(buf)
				<-release
				_, _ = conn.Write([]byte("ok"))
			}(c)
		}
	}()

	e := loadCurrentBodyForGrowthTest(t)
	const handlers = 48
	for i := 0; i < handlers; i++ {
		m := &Memory{
			ID: fmt.Sprintf("event.fanout.block.%03d", i), Layer: "emergent", Revision: 1,
			Tags: []string{"memory"}, Trigger: []string{"event:test.fanout.block"},
			Capabilities: []string{"network.raw"},
			Program: []Op{{Code: "physical_exchange", Args: map[string]string{
				"transport": "tcp", "host": "127.0.0.1", "port": strconv.Itoa(addr.Port),
				"request": "BLOCK", "timeout_ms": "5000", "out": "response",
			}}},
		}
		e.addRuntimeMemory(m)
	}
	if err := globalActivationRuntime.Build(e); err != nil {
		t.Fatal(err)
	}
	ids, err := globalActivationRuntime.ExactFeatureIDs(e, "trigger:event:test.fanout.block")
	if err != nil {
		t.Fatal(err)
	}
	if len(ids) != handlers {
		t.Fatalf("event trigger index mismatch: got=%d want=%d", len(ids), handlers)
	}
	baseline := runtime.NumGoroutine()
	done := make(chan error, 1)
	go func() { done <- e.fireEvent("test.fanout.block", "", newFrame()) }()

	time.Sleep(150 * time.Millisecond)
	select {
	case runErr := <-done:
		close(release)
		t.Fatalf("event fanout ended before blocked handlers: %v", runErr)
	default:
	}
	delta := runtime.NumGoroutine() - baseline
	close(release)
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("event fanout failed after release: %v", err)
		}
	case <-time.After(8 * time.Second):
		t.Fatal("event fanout did not finish")
	}
	if delta > 12 {
		t.Fatalf("event fanout created goroutines proportional to handlers: handlers=%d goroutine_delta=%d", handlers, delta)
	}
}

func TestCallParallelRejectsOversizedFanoutBeforeExecution(t *testing.T) {
	t.Setenv("MEMORYAI_PARALLEL_FANOUT_MAX_TARGETS", "8")
	e := loadCurrentBodyForGrowthTest(t)
	child := &Memory{
		ID: "fanout.child", Layer: "emergent", Revision: 1,
		Tags: []string{"memory"}, Program: []Op{{Code: "set", A: "value", B: "ok"}},
	}
	e.addRuntimeMemory(child)
	f := newFrame()
	for i := 0; i < 9; i++ {
		f.Lists["targets"] = append(f.Lists["targets"], child.ID)
	}
	op := Op{Code: "call_parallel", Args: map[string]string{
		"targets_list": "targets", "out": "value", "ok_list": "oks", "max_parallel": "2",
	}}
	_, err := e.execPrimitive(&Memory{ID: "fanout.caller"}, op, f, 0, nil)
	if err == nil || !strings.Contains(strings.ToLower(err.Error()), "fan-out") {
		t.Fatalf("oversized call_parallel did not fail closed: %v", err)
	}
	if len(f.Lists["value"]) != 0 || len(f.Lists["oks"]) != 0 {
		t.Fatalf("oversized call_parallel allocated/merged outputs before rejection: values=%d oks=%d", len(f.Lists["value"]), len(f.Lists["oks"]))
	}
}

func TestStorageEnvelopeWriterRejectsOversizedBody(t *testing.T) {
	t.Setenv("MEMORYAI_STORAGE_CAPABILITY_KEY", "storage-byte-key")
	t.Setenv("MEMORYAI_STORAGE_MAX_BYTES", "512")
	_, err := storageEnvelopeFor(map[string]any{"payload": strings.Repeat("x", 2048)})
	if err == nil || !strings.Contains(strings.ToLower(err.Error()), "physical byte ceiling") {
		t.Fatalf("storage envelope writer ignored physical byte ceiling: %v", err)
	}
}

func TestStorageEnvelopeReaderRejectsOversizedEnvelope(t *testing.T) {
	t.Setenv("MEMORYAI_STORAGE_CAPABILITY_KEY", "storage-byte-key")
	t.Setenv("MEMORYAI_STORAGE_MAX_BYTES", "512")
	body, err := json.Marshal(map[string]any{"payload": strings.Repeat("x", 2048)})
	if err != nil {
		t.Fatal(err)
	}
	env := storageWireEnvelope{Version: storageWireVersion, Body: body, Signature: meshSignBytes(storageTransportKey(), body)}
	encoded, err := json.Marshal(env)
	if err != nil {
		t.Fatal(err)
	}
	var out map[string]any
	err = readStorageEnvelope(bytes.NewReader(encoded), &out)
	if err == nil || !strings.Contains(strings.ToLower(err.Error()), "physical byte ceiling") {
		t.Fatalf("storage envelope reader accepted oversized envelope: %v", err)
	}
}

type deadlineRecordingConn struct {
	deadline time.Time
}

func (c *deadlineRecordingConn) Read([]byte) (int, error)         { return 0, io.EOF }
func (c *deadlineRecordingConn) Write(p []byte) (int, error)      { return len(p), nil }
func (c *deadlineRecordingConn) Close() error                     { return nil }
func (c *deadlineRecordingConn) LocalAddr() net.Addr              { return &net.TCPAddr{} }
func (c *deadlineRecordingConn) RemoteAddr() net.Addr             { return &net.TCPAddr{} }
func (c *deadlineRecordingConn) SetDeadline(v time.Time) error    { c.deadline = v; return nil }
func (c *deadlineRecordingConn) SetReadDeadline(time.Time) error  { return nil }
func (c *deadlineRecordingConn) SetWriteDeadline(time.Time) error { return nil }

func TestMemNodeConnectionDeadlineUsesBoundedStorageTimeout(t *testing.T) {
	t.Setenv("MEMORYAI_STORAGE_CAPABILITY_KEY", "storage-deadline-test")
	t.Setenv("MEMORYAI_STORAGE_TIMEOUT_MS", "25")
	conn := &deadlineRecordingConn{}
	before := time.Now()
	handleMemNodeConn(t.TempDir(), conn)
	if conn.deadline.IsZero() {
		t.Fatal("mem-node did not set a connection deadline")
	}
	delta := conn.deadline.Sub(before)
	if delta < 20*time.Millisecond || delta > 200*time.Millisecond {
		t.Fatalf("mem-node ignored configured bounded storage timeout: %v", delta)
	}
}

func TestStorageTransportOperatorLimitsCannotExceedKernelHardCaps(t *testing.T) {
	t.Setenv("MEMORYAI_STORAGE_MAX_BYTES", "999999999")
	t.Setenv("MEMORYAI_STORAGE_TIMEOUT_MS", "999999999")
	t.Setenv("MEMORYAI_STORAGE_MAX_CONCURRENT", "999999999")
	if got := storageTransportMaxBytes(); got != hardStorageTransportMaxBytes {
		t.Fatalf("storage byte ceiling escaped hard max: %d", got)
	}
	if got := storageConnectionTimeout(); got != hardStorageConnectionTimeout {
		t.Fatalf("storage timeout escaped hard max: %v", got)
	}
	if got := storageMaxConcurrent(); got != hardStorageMaxConcurrent {
		t.Fatalf("storage concurrency escaped hard max: %d", got)
	}
}

func TestStorageOversizedReplyFallsBackToBoundedSignedError(t *testing.T) {
	t.Setenv("MEMORYAI_STORAGE_CAPABILITY_KEY", "storage-reply-test")
	t.Setenv("MEMORYAI_STORAGE_MAX_BYTES", "512")
	var wire bytes.Buffer
	if err := writeStorageReplyBounded(&wire, map[string]any{"ok": true, "data": strings.Repeat("x", 4096)}); err != nil {
		t.Fatalf("bounded storage fallback failed: %v", err)
	}
	var reply map[string]any
	if err := readStorageEnvelope(bytes.NewReader(wire.Bytes()), &reply); err != nil {
		t.Fatalf("bounded storage fallback is not a valid signed envelope: %v", err)
	}
	if reply["ok"] != false || !strings.Contains(strings.ToLower(fmt.Sprint(reply["error"])), "physical byte ceiling") {
		t.Fatalf("oversized storage response did not become bounded failure: %#v", reply)
	}
}

func TestRemoteStorageTimeoutParserHardCapsMemoryValue(t *testing.T) {
	got := parseTimeout("999999999")
	if got > 120*time.Second {
		t.Fatalf("remote storage timeout escaped physical hard cap: %v", got)
	}
}

func TestSourceAdapterTimeoutParserHardCapsMemoryValue(t *testing.T) {
	got := sourceParseTimeout("999999h")
	if got > 120*time.Second {
		t.Fatalf("source adapter timeout escaped physical hard cap: %v", got)
	}
}

func TestPhysicalExchangeTimeoutParserHardCapsMemoryValue(t *testing.T) {
	got := parsePhysicalExchangeTimeoutMS("999999999")
	if got != hardPhysicalExchangeTimeout {
		t.Fatalf("raw exchange timeout escaped physical hard cap: %v", got)
	}
}

func TestRemoteStorageDirectTimeoutClamp(t *testing.T) {
	got := storageRequestTimeout(24 * time.Hour)
	if got != hardStorageConnectionTimeout {
		t.Fatalf("direct remote storage timeout escaped physical hard cap: %v", got)
	}
}

func TestUnicodeWindowsRejectsFrameListCardinalityOverflow(t *testing.T) {
	t.Setenv("MEMORYAI_FRAME_LIST_MAX_ITEMS", "8")
	e := loadCurrentBodyForGrowthTest(t)
	f := newFrame()
	f.Vars["src"] = "abcdefghijklmnopqrst"
	op := Op{Code: "unicode_windows", A: "src", B: "out", Args: map[string]string{"min": "1", "max": "1", "limit": "9"}}
	if _, err := e.execPrimitive(&Memory{ID: "frame-list-test"}, op, f, 0, nil); err == nil {
		t.Fatal("unicode_windows exceeded physical Frame-list cardinality without rejection")
	}
}

func TestRegexAllRejectsFrameListCardinalityOverflow(t *testing.T) {
	t.Setenv("MEMORYAI_FRAME_LIST_MAX_ITEMS", "8")
	e := loadCurrentBodyForGrowthTest(t)
	f := newFrame()
	f.Vars["src"] = "a a a a a a a a a"
	op := Op{Code: "regex_all", A: "src", B: "(a)", C: "matches"}
	if _, err := e.execPrimitive(&Memory{ID: "frame-list-test"}, op, f, 0, nil); err == nil {
		t.Fatal("regex_all exceeded physical Frame-list cardinality without rejection")
	}
}

func TestListAppendRejectsFrameListCardinalityOverflow(t *testing.T) {
	t.Setenv("MEMORYAI_FRAME_LIST_MAX_ITEMS", "8")
	e := loadCurrentBodyForGrowthTest(t)
	f := newFrame()
	f.Lists["items"] = []string{"1", "2", "3", "4", "5", "6", "7", "8"}
	op := Op{Code: "list_append", A: "items", B: "9"}
	if _, err := e.execPrimitive(&Memory{ID: "frame-list-test"}, op, f, 0, nil); err == nil {
		t.Fatal("list_append exceeded physical Frame-list cardinality without rejection")
	}
	if len(f.Lists["items"]) != 8 {
		t.Fatalf("overflowing list append mutated Frame before rejection: %d", len(f.Lists["items"]))
	}
}

func TestFrameListOperatorLimitCannotExceedKernelHardCap(t *testing.T) {
	t.Setenv("MEMORYAI_FRAME_LIST_MAX_ITEMS", "999999999")
	if got := frameListMaxItems(); got != hardFrameListMaxItems {
		t.Fatalf("Frame-list operator limit escaped Kernel hard cap: got=%d hard=%d", got, hardFrameListMaxItems)
	}
}

func TestStrJoinRejectsFrameValueByteOverflow(t *testing.T) {
	t.Setenv("MEMORYAI_FRAME_VALUE_MAX_BYTES", "64")
	e := loadCurrentBodyForGrowthTest(t)
	f := newFrame()
	f.Vars["a"] = strings.Repeat("a", 40)
	f.Vars["b"] = strings.Repeat("b", 40)
	op := Op{Code: "str_join", A: "{{a}}", B: "{{b}}", C: "out"}
	if _, err := e.execPrimitive(&Memory{ID: "frame-value-test"}, op, f, 0, nil); err == nil {
		t.Fatal("str_join exceeded physical Frame-value byte ceiling without rejection")
	}
	if f.Vars["out"] != "" {
		t.Fatalf("overflowing str_join mutated Frame before rejection: %d bytes", len(f.Vars["out"]))
	}
}

func TestEmitRejectsFrameOutputCardinalityOverflow(t *testing.T) {
	t.Setenv("MEMORYAI_FRAME_OUTPUT_MAX_ITEMS", "2")
	e := loadCurrentBodyForGrowthTest(t)
	f := newFrame()
	self := &Memory{ID: "frame-output-test"}
	for _, value := range []string{"one", "two"} {
		if _, err := e.execPrimitive(self, Op{Code: "emit", A: value}, f, 0, nil); err != nil {
			t.Fatalf("allowed emit failed: %v", err)
		}
	}
	if _, err := e.execPrimitive(self, Op{Code: "emit", A: "three"}, f, 0, nil); err == nil {
		t.Fatal("emit exceeded physical Frame-output cardinality without rejection")
	}
	if len(f.Output) != 2 {
		t.Fatalf("overflowing emit mutated Frame output before rejection: %d", len(f.Output))
	}
}

func TestEmitRejectsFrameOutputByteOverflow(t *testing.T) {
	t.Setenv("MEMORYAI_FRAME_OUTPUT_MAX_BYTES", "5")
	e := loadCurrentBodyForGrowthTest(t)
	f := newFrame()
	self := &Memory{ID: "frame-output-bytes-test"}
	if _, err := e.execPrimitive(self, Op{Code: "emit", A: "123"}, f, 0, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := e.execPrimitive(self, Op{Code: "emit", A: "456"}, f, 0, nil); err == nil {
		t.Fatal("emit exceeded physical Frame-output byte ceiling without rejection")
	}
	if len(f.Output) != 1 {
		t.Fatalf("overflowing emit mutated Frame output before rejection: %d", len(f.Output))
	}
}

func TestFrameValueAndOutputOperatorLimitsCannotExceedKernelHardCaps(t *testing.T) {
	t.Setenv("MEMORYAI_FRAME_VALUE_MAX_BYTES", "999999999999")
	t.Setenv("MEMORYAI_FRAME_OUTPUT_MAX_BYTES", "999999999999")
	t.Setenv("MEMORYAI_FRAME_OUTPUT_MAX_ITEMS", "999999999")
	if got := frameValueMaxBytes(); got != hardFrameValueMaxBytes {
		t.Fatalf("Frame-value operator limit escaped Kernel hard cap: got=%d hard=%d", got, hardFrameValueMaxBytes)
	}
	if got := frameOutputMaxBytes(); got != hardFrameOutputMaxBytes {
		t.Fatalf("Frame-output byte limit escaped Kernel hard cap: got=%d hard=%d", got, hardFrameOutputMaxBytes)
	}
	if got := frameOutputMaxItems(); got != hardFrameOutputMaxItems {
		t.Fatalf("Frame-output item limit escaped Kernel hard cap: got=%d hard=%d", got, hardFrameOutputMaxItems)
	}
}

func TestProgramInsertRejectsPhysicalProgramCardinalityOverflow(t *testing.T) {
	t.Setenv("MEMORYAI_PROGRAM_MAX_OPS", "130")
	e := loadCurrentBodyForGrowthTest(t)
	target, err := e.resolveExecutable("evolution.feedback.parent")
	if err != nil {
		t.Fatal(err)
	}
	before := len(target.Program)
	if before != 130 {
		t.Fatalf("unexpected canonical program length: %d", before)
	}
	f := newFrame()
	op := Op{Code: "program_insert_from", A: target.ID, B: "0", C: target.ID, Args: map[string]string{"start": "0", "count": "1"}}
	if _, err := e.execPrimitive(target, op, f, 0, nil); err == nil {
		t.Fatal("program_insert_from exceeded physical Program cardinality without rejection")
	}
	if len(target.Program) != before {
		t.Fatalf("overflowing program insert mutated target before rejection: before=%d after=%d", before, len(target.Program))
	}
}

func TestProgramSetFieldRejectsPhysicalProgramByteOverflow(t *testing.T) {
	t.Setenv("MEMORYAI_PROGRAM_MAX_BYTES", "1024")
	e := loadCurrentBodyForGrowthTest(t)
	target, err := e.resolveExecutable("evolution.feedback.parent")
	if err != nil {
		t.Fatal(err)
	}
	beforeA := target.Program[0].A
	f := newFrame()
	f.Vars["huge"] = strings.Repeat("x", 2048)
	op := Op{Code: "program_set_field", A: target.ID, B: "0", C: "{{huge}}", Args: map[string]string{"field": "a"}}
	if _, err := e.execPrimitive(target, op, f, 0, nil); err == nil {
		t.Fatal("program_set_field exceeded physical Program byte ceiling without rejection")
	}
	if target.Program[0].A != beforeA {
		t.Fatal("overflowing program_set_field mutated target before rejection")
	}
}

func TestProgramImportRejectsOversizedEnvelopeBeforeDecode(t *testing.T) {
	t.Setenv("MEMORYAI_PROGRAM_MAX_BYTES", "128")
	e := loadCurrentBodyForGrowthTest(t)
	target, err := e.resolveExecutable("evolution.feedback.parent")
	if err != nil {
		t.Fatal(err)
	}
	before := len(target.Program)
	f := newFrame()
	f.Vars["encoded"] = strings.Repeat("{", 256)
	op := Op{Code: "program_import", A: target.ID, B: "encoded"}
	_, err = e.execPrimitive(target, op, f, 0, nil)
	if err == nil || !strings.Contains(err.Error(), "physical Program byte ceiling") {
		t.Fatalf("program_import did not fail at physical byte boundary before decode: %v", err)
	}
	if len(target.Program) != before {
		t.Fatalf("oversized program import mutated target before rejection: before=%d after=%d", before, len(target.Program))
	}
}

func TestProgramPhysicalLimitsCannotExceedKernelHardCaps(t *testing.T) {
	t.Setenv("MEMORYAI_PROGRAM_MAX_OPS", "999999999")
	t.Setenv("MEMORYAI_PROGRAM_MAX_BYTES", "999999999999")
	if got := programMaxOps(); got != hardProgramMaxOps {
		t.Fatalf("Program operator limit escaped Kernel hard cap: got=%d hard=%d", got, hardProgramMaxOps)
	}
	if got := programMaxBytes(); got != hardProgramMaxBytes {
		t.Fatalf("Program byte limit escaped Kernel hard cap: got=%d hard=%d", got, hardProgramMaxBytes)
	}
}

func TestTemplateExpansionRejectsFrameValueOverflowBeforeSet(t *testing.T) {
	t.Setenv("MEMORYAI_FRAME_VALUE_MAX_BYTES", "64")
	e := loadCurrentBodyForGrowthTest(t)
	f := newFrame()
	f.Vars["chunk"] = strings.Repeat("x", 40)
	op := Op{Code: "set", A: "out", B: "{{chunk}}{{chunk}}"}
	if _, err := e.execPrimitive(&Memory{ID: "expand-bound-test"}, op, f, 0, nil); err == nil {
		t.Fatal("template expansion exceeded physical Frame-value byte ceiling without rejection")
	}
	if _, ok := f.Vars["out"]; ok {
		t.Fatal("overflowing template expansion mutated Frame before rejection")
	}
}

func TestTemplateExpansionRejectsRecursiveIntermediateGrowth(t *testing.T) {
	t.Setenv("MEMORYAI_FRAME_VALUE_MAX_BYTES", "128")
	e := loadCurrentBodyForGrowthTest(t)
	f := newFrame()
	f.Vars["grow"] = "{{grow}}{{grow}}"
	op := Op{Code: "set", A: "out", B: "{{grow}}"}
	if _, err := e.execPrimitive(&Memory{ID: "expand-recursive-test"}, op, f, 0, nil); err == nil {
		t.Fatal("recursive template expansion exceeded physical Frame-value byte ceiling without rejection")
	}
	if _, ok := f.Vars["out"]; ok {
		t.Fatal("recursive overflow mutated Frame before rejection")
	}
}

func TestStateSetRejectsMemoryRecordByteOverflowBeforeMutation(t *testing.T) {
	t.Setenv("MEMORYAI_MEMORY_RECORD_MAX_BYTES", "256")
	e := loadCurrentBodyForGrowthTest(t)
	target := &Memory{ID: "record-bound-state", Layer: "emergent", Tags: []string{"memory"}, State: map[string]any{"keep": "yes"}, Revision: 1}
	e.addRuntimeMemory(target)
	f := newFrame()
	f.Vars["huge"] = strings.Repeat("x", 512)
	op := Op{Code: "state_set", A: target.ID, B: "payload", C: "{{huge}}"}
	if _, err := e.execPrimitive(target, op, f, 0, nil); err == nil {
		t.Fatal("state_set exceeded physical Memory-record byte ceiling without rejection")
	}
	if _, exists := target.State["payload"]; exists {
		t.Fatal("overflowing state_set mutated Memory before rejection")
	}
	if target.Revision != 1 || target.State["keep"] != "yes" {
		t.Fatalf("overflowing state_set changed target metadata: revision=%d state=%v", target.Revision, target.State)
	}
}

func TestStateListAppendRejectsMemoryRecordByteOverflowBeforeMutation(t *testing.T) {
	t.Setenv("MEMORYAI_MEMORY_RECORD_MAX_BYTES", "256")
	e := loadCurrentBodyForGrowthTest(t)
	target := &Memory{ID: "record-bound-list", Layer: "emergent", Tags: []string{"memory"}, State: map[string]any{"items": []string{strings.Repeat("a", 100)}}, Revision: 1}
	e.addRuntimeMemory(target)
	f := newFrame()
	f.Vars["next"] = strings.Repeat("b", 100)
	op := Op{Code: "state_list_append", A: target.ID, B: "items", C: "{{next}}"}
	if _, err := e.execPrimitive(target, op, f, 0, nil); err == nil {
		t.Fatal("state_list_append exceeded physical Memory-record byte ceiling without rejection")
	}
	items := stateList(target.State["items"])
	if len(items) != 1 || items[0] != strings.Repeat("a", 100) || target.Revision != 1 {
		t.Fatalf("overflowing state_list_append mutated target: revision=%d items=%v", target.Revision, items)
	}
}

func TestStateListUniqueAppendRejectsMemoryRecordByteOverflowBeforeMutation(t *testing.T) {
	t.Setenv("MEMORYAI_MEMORY_RECORD_MAX_BYTES", "256")
	e := loadCurrentBodyForGrowthTest(t)
	target := &Memory{ID: "record-bound-unique", Layer: "emergent", Tags: []string{"memory"}, State: map[string]any{"items": []string{strings.Repeat("a", 100)}}, Revision: 1}
	e.addRuntimeMemory(target)
	f := newFrame()
	f.Vars["next"] = strings.Repeat("b", 100)
	op := Op{Code: "state_list_unique_append", A: target.ID, B: "items", C: "{{next}}", Args: map[string]string{"added_out": "added"}}
	if _, err := e.execPrimitive(target, op, f, 0, nil); err == nil {
		t.Fatal("state_list_unique_append exceeded physical Memory-record byte ceiling without rejection")
	}
	items := stateList(target.State["items"])
	if len(items) != 1 || target.Revision != 1 {
		t.Fatalf("overflowing state_list_unique_append mutated target: revision=%d items=%v", target.Revision, items)
	}
	if _, exists := f.Vars["added"]; exists {
		t.Fatal("overflowing state_list_unique_append published success output")
	}
}

func TestStateNumAddRejectsMemoryRecordByteOverflowBeforeMutation(t *testing.T) {
	t.Setenv("MEMORYAI_MEMORY_RECORD_MAX_BYTES", "256")
	e := loadCurrentBodyForGrowthTest(t)
	target := &Memory{ID: "record-bound-num", Layer: "emergent", Tags: []string{"memory"}, State: map[string]any{"pad": strings.Repeat("x", 150)}, Revision: 1}
	e.addRuntimeMemory(target)
	f := newFrame()
	op := Op{Code: "state_num_add", A: target.ID, B: "counter", C: "1", Args: map[string]string{"out": "sum"}}
	if _, err := e.execPrimitive(target, op, f, 0, nil); err == nil {
		t.Fatal("state_num_add exceeded physical Memory-record byte ceiling without rejection")
	}
	if _, exists := target.State["counter"]; exists || target.Revision != 1 {
		t.Fatalf("overflowing state_num_add mutated target: revision=%d state=%v", target.Revision, target.State)
	}
	if _, exists := f.Vars["sum"]; exists {
		t.Fatal("overflowing state_num_add published output")
	}
}

func TestMemoryHistoryAppendRejectsRecordByteOverflowBeforeMutation(t *testing.T) {
	t.Setenv("MEMORYAI_MEMORY_RECORD_MAX_BYTES", "256")
	e := loadCurrentBodyForGrowthTest(t)
	target := &Memory{ID: "record-bound-history", Layer: "emergent", Tags: []string{"memory"}, State: map[string]any{"pad": strings.Repeat("x", 130)}, Revision: 1}
	e.addRuntimeMemory(target)
	f := newFrame()
	f.Vars["history"] = strings.Repeat("h", 64)
	op := Op{Code: "memory_history_append", A: target.ID, B: "{{history}}", Args: map[string]string{"field": "success_history"}}
	if _, err := e.execPrimitive(target, op, f, 0, nil); err == nil {
		t.Fatal("memory_history_append exceeded physical Memory-record byte ceiling without rejection")
	}
	if len(target.SuccessHistory) != 0 || target.Revision != 1 {
		t.Fatalf("overflowing memory_history_append mutated target: revision=%d history=%v", target.Revision, target.SuccessHistory)
	}
}

func TestMemoryTagAddRejectsRecordByteOverflowBeforeMutation(t *testing.T) {
	t.Setenv("MEMORYAI_MEMORY_RECORD_MAX_BYTES", "256")
	e := loadCurrentBodyForGrowthTest(t)
	target := &Memory{ID: "record-bound-tag", Layer: "emergent", Tags: []string{"memory"}, State: map[string]any{"pad": strings.Repeat("x", 130)}, Revision: 1}
	e.addRuntimeMemory(target)
	f := newFrame()
	f.Vars["tag"] = strings.Repeat("t", 64)
	op := Op{Code: "memory_tag_add", A: target.ID, B: "{{tag}}"}
	if _, err := e.execPrimitive(target, op, f, 0, nil); err == nil {
		t.Fatal("memory_tag_add exceeded physical Memory-record byte ceiling without rejection")
	}
	if len(target.Tags) != 1 || target.Tags[0] != "memory" || target.Revision != 1 {
		t.Fatalf("overflowing memory_tag_add mutated target: revision=%d tags=%v", target.Revision, target.Tags)
	}
}

func TestMemoryImportRejectsOversizedRawRecordBeforeDecode(t *testing.T) {
	t.Setenv("MEMORYAI_MEMORY_RECORD_MAX_BYTES", "256")
	e := loadCurrentBodyForGrowthTest(t)
	_, _, err := e.importMemoryJSON(strings.Repeat("{", 512), false)
	if err == nil || !strings.Contains(err.Error(), "physical Memory-record byte ceiling") {
		t.Fatalf("oversized Memory import was not rejected before JSON decode: %v", err)
	}
}

func TestExplicitMemoryUpsertRejectsOversizedRecordBeforeInsert(t *testing.T) {
	t.Setenv("MEMORYAI_MEMORY_RECORD_MAX_BYTES", "256")
	path := filepath.Join(t.TempDir(), "Memory.mem")
	writeBodyForPersistenceTest(t, path, "storage", nil)
	e, err := loadEngine(path)
	if err != nil {
		t.Fatal(err)
	}
	defer e.close()
	incoming := &Memory{ID: "oversized-upsert", Layer: "emergent", Tags: []string{"memory"}, Content: strings.Repeat("x", 512), State: map[string]any{}, Revision: 1}
	if err := e.upsertExplicitMemoryBounded(incoming); err == nil || !strings.Contains(err.Error(), "physical Memory-record byte ceiling") {
		t.Fatalf("oversized explicit Memory upsert was not rejected: %v", err)
	}
	if _, err := e.resolveIDLocal(incoming.ID); !errors.Is(err, io.EOF) {
		t.Fatalf("oversized explicit Memory leaked into storage body: %v", err)
	}
}

func TestMemoryNewRejectsOversizedRecordBeforeFabricPlacement(t *testing.T) {
	t.Setenv("MEMORYAI_MEMORY_RECORD_MAX_BYTES", "256")
	dir := t.TempDir()
	primary := filepath.Join(dir, "Memory.mem")
	root := &Memory{ID: "root", Layer: "inherited", Tags: []string{"memory"}, State: map[string]any{}, Revision: 1}
	writeBodyForPersistenceTest(t, primary, "core", []*Memory{root})
	writeBodyForPersistenceTest(t, filepath.Join(dir, "Memory.1.mem"), "storage", nil)
	e, err := loadEngine(primary)
	if err != nil {
		t.Fatal(err)
	}
	defer e.close()
	e.mountAutomaticStorageShards()
	f := newFrame()
	f.Vars["huge"] = strings.Repeat("x", 512)
	op := Op{Code: "memory_new", A: "child_id", Args: map[string]string{"layer": "emergent", "tags": "memory", "content": "{{huge}}"}}
	if _, err := e.execPrimitive(root, op, f, 0, nil); err == nil || !strings.Contains(err.Error(), "physical Memory-record byte ceiling") {
		t.Fatalf("oversized memory_new was not rejected before Fabric placement: %v", err)
	}
	if f.Vars["child_id"] != "" {
		t.Fatalf("oversized memory_new published child id: %q", f.Vars["child_id"])
	}
	for _, owner := range e.mountedFabricEngines() {
		if localMemoryCountFast(owner) != 0 {
			t.Fatalf("oversized memory_new leaked into storage shard: count=%d", localMemoryCountFast(owner))
		}
	}
}
