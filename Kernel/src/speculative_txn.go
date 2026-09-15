package main

import (
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"strconv"
	"sync"
	"sync/atomic"
)

var errSpeculativeSideEffect = errors.New("speculative transaction reached external side effect")

// speculativeForbiddenPrimitive defines the physical boundary of Snapshot/Diff
// cognition. Memory/state/program mutation is safe inside the private snapshot;
// external I/O, storage topology changes and process/persistence effects are not.
func speculativeForbiddenPrimitive(code string) bool {
	switch code {
	case "process_restart", "space_next_path", "space_create", "space_mount", "space_unmount", "space_select_write", "space_copy", "space_move", "space_merge", "space_fsck",
		"remote_space_info", "remote_space_create", "remote_space_put", "remote_space_get", "remote_space_digest", "remote_space_import", "remote_space_list", "remote_space_upsert", "remote_space_delete", "remote_space_tag_list",
		"physical_exchange", "persist", "artifact_write", "artifact_read", "artifact_digest", "artifact_exists", "mesh_shared_propose", "mesh_shared_search", "mesh_shared_fetch", "mesh_structure_run", "mesh_fanout", "mesh_journal_flush", "mesh_directory":
		return true
	}
	return false
}

type memorySnapshot struct {
	memories   map[string]*Memory
	newIDs     map[string]bool
	deletedIDs map[string]bool
	dirtyIDs   map[string]bool
}

type transactionDiff struct {
	changed    map[string]*Memory
	deleted    map[string]bool
	created    map[string]*Memory
	reuseDelta map[string]uint64
}

func memoryDigestNoReuse(m *Memory) string {
	if m == nil {
		return "<nil>"
	}
	q := copyMemory(m)
	q.Reuse = 0
	b, _ := json.Marshal(q)
	h := sha256.Sum256(b)
	return fmt.Sprintf("%x", h[:])
}

func cloneFrame(f *Frame) *Frame {
	q := newFrame()
	for k, v := range f.Vars {
		q.Vars[k] = v
	}
	for k, vs := range f.Lists {
		q.Lists[k] = append([]string(nil), vs...)
	}
	q.Output = append([]string(nil), f.Output...)
	return q
}

func assignFrame(dst, src *Frame) {
	dst.Vars = map[string]string{}
	dst.Lists = map[string][]string{}
	for k, v := range src.Vars {
		dst.Vars[k] = v
	}
	for k, vs := range src.Lists {
		dst.Lists[k] = append([]string(nil), vs...)
	}
	dst.Output = append([]string(nil), src.Output...)
}

func (e *Engine) snapshotForSpeculation(maxMem int) (*Engine, *memorySnapshot, error) {
	ids, err := e.localIDs()
	if err != nil {
		return nil, nil, err
	}
	if maxMem > 0 && len(ids) > maxMem {
		return nil, nil, fmt.Errorf("snapshot memory limit exceeded: %d > %d", len(ids), maxMem)
	}
	base := &memorySnapshot{memories: map[string]*Memory{}, newIDs: map[string]bool{}, deletedIDs: map[string]bool{}, dirtyIDs: map[string]bool{}}
	cloneCache := map[string]*Memory{}
	e.dataMu.RLock()
	for _, id := range ids {
		if e.deletedIDs[id] {
			continue
		}
		m := e.cache[id]
		if m == nil {
			e.dataMu.RUnlock()
			mm, er := e.resolveIDLocal(id)
			if er != nil {
				return nil, nil, er
			}
			e.dataMu.RLock()
			m = mm
		}
		cm := copyMemory(m)
		base.memories[id] = copyMemory(m)
		cloneCache[id] = cm
	}
	for id, v := range e.newIDs {
		base.newIDs[id] = v
	}
	for id, v := range e.deletedIDs {
		base.deletedIDs[id] = v
	}
	for id, v := range e.dirtyIDs {
		base.dirtyIDs[id] = v
	}
	e.dataMu.RUnlock()

	ce := &Engine{
		bodyPath: e.bodyPath, manifest: e.manifest, genesis: e.genesis, store: e.store,
		cache: cloneCache, newIDs: map[string]bool{}, dirtyIDs: map[string]bool{}, deletedIDs: map[string]bool{},
		spaces: map[string]*Engine{}, writeSpace: "", dataMu: &sync.RWMutex{}, speculative: true, tagAdded: map[string]map[string]bool{}, tagRemoved: map[string]map[string]bool{},
	}
	for id, v := range base.newIDs {
		ce.newIDs[id] = v
	}
	for id, v := range base.deletedIDs {
		ce.deletedIDs[id] = v
	}
	for id, v := range base.dirtyIDs {
		ce.dirtyIDs[id] = v
	}
	return ce, base, nil
}

func diffSnapshot(base *memorySnapshot, ce *Engine) transactionDiff {
	d := transactionDiff{changed: map[string]*Memory{}, deleted: map[string]bool{}, created: map[string]*Memory{}, reuseDelta: map[string]uint64{}}
	ce.dataMu.RLock()
	defer ce.dataMu.RUnlock()
	for id, bm := range base.memories {
		cm := ce.cache[id]
		if ce.deletedIDs[id] || cm == nil {
			d.deleted[id] = true
			continue
		}
		if cm.Reuse > bm.Reuse {
			d.reuseDelta[id] = cm.Reuse - bm.Reuse
		}
		if memoryDigestNoReuse(cm) != memoryDigestNoReuse(bm) {
			d.changed[id] = copyMemory(cm)
		}
	}
	for id, cm := range ce.cache {
		if _, existed := base.memories[id]; !existed && !ce.deletedIDs[id] {
			d.created[id] = copyMemory(cm)
		}
	}
	return d
}

func currentMemoryNoReuseDigest(e *Engine, id string) string {
	m, err := e.resolveIDLocal(id)
	if err != nil {
		return "<missing>"
	}
	return memoryDigestNoReuse(m)
}

func (e *Engine) commitSnapshotDiff(base *memorySnapshot, d transactionDiff) bool {
	// Caller holds daemonBarrier write lock + daemonCognitionLane.
	for id := range d.changed {
		bm := base.memories[id]
		if bm == nil || currentMemoryNoReuseDigest(e, id) != memoryDigestNoReuse(bm) {
			return false
		}
	}
	for id := range d.deleted {
		bm := base.memories[id]
		if bm == nil || currentMemoryNoReuseDigest(e, id) != memoryDigestNoReuse(bm) {
			return false
		}
	}
	for id := range d.created {
		if _, err := e.resolveIDLocal(id); err == nil {
			return false
		}
	}

	e.dataMu.Lock()
	defer e.dataMu.Unlock()
	for id, cm := range d.changed {
		curReuse := uint64(0)
		if cur := e.cache[id]; cur != nil {
			curReuse = cur.Reuse
		}
		q := copyMemory(cm)
		q.Reuse = curReuse + d.reuseDelta[id]
		e.cache[id] = q
		e.dirtyIDs[id] = true
		e.dirty = true
	}
	for id := range d.deleted {
		delete(e.cache, id)
		delete(e.newIDs, id)
		delete(e.dirtyIDs, id)
		e.deletedIDs[id] = true
		e.dirty = true
	}
	for id, cm := range d.created {
		e.cache[id] = copyMemory(cm)
		e.newIDs[id] = true
		e.dirtyIDs[id] = true
		e.dirty = true
	}
	for id, delta := range d.reuseDelta {
		if _, structural := d.changed[id]; structural {
			continue
		}
		if m := e.cache[id]; m != nil {
			m.Reuse += delta
		}
	}
	return true
}

func snapshotMemoryLimit() int {
	n := 4096
	if s := os.Getenv("MEMORYAI_SNAPSHOT_MAX_MEMORIES"); s != "" {
		if q, err := strconv.Atoi(s); err == nil && q >= 0 {
			n = q
		}
	}
	return n
}

// counters are kept here so the scheduler can report genuine speculative work.
var speculativeStarted uint64
var speculativeCommitted uint64
var speculativeConflicted uint64
var speculativeSideEffectFallback uint64
var speculativeLimitFallback uint64
var speculativeInflight int64
var speculativePeak int64

func (e *Engine) speculativeSideEffectHit() bool {
	return e != nil && atomic.LoadUint32(&e.speculativeBlocked) != 0
}

func speculativeInfo() map[string]any {
	return map[string]any{
		"started":                 atomic.LoadUint64(&speculativeStarted),
		"committed":               atomic.LoadUint64(&speculativeCommitted),
		"conflicted":              atomic.LoadUint64(&speculativeConflicted),
		"side_effect_fallback":    atomic.LoadUint64(&speculativeSideEffectFallback),
		"snapshot_limit_fallback": atomic.LoadUint64(&speculativeLimitFallback),
		"inflight":                atomic.LoadInt64(&speculativeInflight),
		"peak_parallel":           atomic.LoadInt64(&speculativePeak),
		"snapshot_max_memories":   snapshotMemoryLimit(),
	}
}

func installTxnSelfTestMemory(e *Engine, slots int) {
	writer := &Memory{
		ID: "__txn.selftest.writer", Layer: "inherited", Generation: 0,
		Tags: []string{"memory", "__txn.selftest.writer"}, Content: "ephemeral scheduler self-test writer",
		State: map[string]any{}, Program: []Op{
			{Code: "state_get", A: "{{target}}", B: "counter", C: "tv"},
			{Code: "var_default", A: "tv", B: "0"},
			{Code: "var_default", A: "spin_limit", B: "20000"},
			{Code: "set", A: "spin", B: "0"},
			{Code: "label", A: "spin_loop"},
			{Code: "cmp_ge", A: "spin_done", B: "{{spin}}", C: "{{spin_limit}}"},
			{Code: "jump_if", A: "{{spin_done}}", B: "write"},
			{Code: "num_add", A: "spin", B: "{{spin}}", C: "1"},
			{Code: "jump", A: "spin_loop"},
			{Code: "label", A: "write"},
			{Code: "num_add", A: "tv", B: "{{tv}}", C: "1"},
			{Code: "state_set", A: "{{target}}", B: "counter", C: "{{tv}}"},
		},
	}
	e.dataMu.Lock()
	e.cache[writer.ID] = writer
	e.newIDs[writer.ID] = true
	for i := 0; i < slots; i++ {
		id := fmt.Sprintf("__txn.selftest.slot.%d", i)
		e.cache[id] = &Memory{ID: id, Layer: "inherited", Tags: []string{"memory", id}, State: map[string]any{"counter": "0"}, Program: []Op{{Code: "halt"}}}
		e.newIDs[id] = true
	}
	e.dataMu.Unlock()
}

func runParallelTxnRequests(e *Engine, targets []string, spin string) []error {
	errs := make([]error, len(targets))
	start := make(chan struct{})
	var wg sync.WaitGroup
	wg.Add(len(targets))
	for i, target := range targets {
		go func(i int, target string) {
			defer wg.Done()
			<-start
			f := newFrame()
			f.Vars["target"] = target
			f.Vars["spin_limit"] = spin
			errs[i] = globalTxnScheduler.run(e, "__txn.selftest.writer", f)
		}(i, target)
	}
	close(start)
	wg.Wait()
	return errs
}

func (e *Engine) schedulerSelfTest() (map[string]any, error) {
	const slots = 16
	installTxnSelfTestMemory(e, slots)

	beforeCommit := atomic.LoadUint64(&speculativeCommitted)
	unique := make([]string, slots)
	for i := 0; i < slots; i++ {
		unique[i] = fmt.Sprintf("__txn.selftest.slot.%d", i)
	}
	errs := runParallelTxnRequests(e, unique, "30000")
	for _, er := range errs {
		if er != nil {
			return nil, er
		}
	}
	uniqueOK := true
	for _, id := range unique {
		m, er := e.resolveIDLocal(id)
		if er != nil {
			return nil, er
		}
		if fmt.Sprint(m.State["counter"]) != "1" {
			uniqueOK = false
		}
	}

	// Conflict replay gate: every request increments the same logical counter.
	conflictTarget := unique[0]
	e.dataMu.Lock()
	e.cache[conflictTarget].State["counter"] = "0"
	e.dataMu.Unlock()
	conflicts := make([]string, slots)
	for i := range conflicts {
		conflicts[i] = conflictTarget
	}
	errs = runParallelTxnRequests(e, conflicts, "12000")
	for _, er := range errs {
		if er != nil {
			return nil, er
		}
	}
	m, er := e.resolveIDLocal(conflictTarget)
	if er != nil {
		return nil, er
	}
	conflictFinal := fmt.Sprint(m.State["counter"])
	conflictOK := conflictFinal == strconv.Itoa(slots)

	// Side-effect exactly-once gate: speculative execution must stop before the
	// network write and the canonical fallback must emit one physical request.
	ln, er := net.Listen("tcp", "127.0.0.1:0")
	if er != nil {
		return nil, er
	}
	defer ln.Close()
	var networkHits int64
	serverDone := make(chan struct{})
	go func() {
		defer close(serverDone)
		c, er := ln.Accept()
		if er != nil {
			return
		}
		atomic.AddInt64(&networkHits, 1)
		buf := make([]byte, 64)
		_, _ = c.Read(buf)
		_, _ = c.Write([]byte("PONG"))
		_ = c.Close()
	}()
	addr := ln.Addr().(*net.TCPAddr)
	ext := &Memory{ID: "__txn.selftest.external", Layer: "inherited", Tags: []string{"memory", "__txn.selftest.external"}, State: map[string]any{}, Program: []Op{{Code: "physical_exchange", Args: map[string]string{"transport": "tcp", "host": "127.0.0.1", "port": strconv.Itoa(addr.Port), "request": "PING", "timeout_ms": "2000", "out": "resp"}}}}
	e.dataMu.Lock()
	e.cache[ext.ID] = ext
	e.newIDs[ext.ID] = true
	e.dataMu.Unlock()
	fallbackBefore := atomic.LoadUint64(&speculativeSideEffectFallback)
	f := newFrame()
	if er = globalTxnScheduler.run(e, ext.ID, f); er != nil {
		return nil, er
	}
	<-serverDone
	externalOK := atomic.LoadInt64(&networkHits) == 1 && f.Vars["resp"] == "PONG" && atomic.LoadUint64(&speculativeSideEffectFallback) > fallbackBefore

	info := globalTxnScheduler.Info()
	result := map[string]any{
		"ok":                    uniqueOK && conflictOK && externalOK,
		"unique_disjoint":       map[string]any{"requests": slots, "all_counter_1": uniqueOK, "speculative_commits_delta": atomic.LoadUint64(&speculativeCommitted) - beforeCommit},
		"conflict_replay":       map[string]any{"requests": slots, "final_counter": conflictFinal, "expected": strconv.Itoa(slots), "ok": conflictOK},
		"external_exactly_once": map[string]any{"network_hits": atomic.LoadInt64(&networkHits), "response": f.Vars["resp"], "ok": externalOK},
		"scheduler":             info,
	}
	if !result["ok"].(bool) {
		return result, errors.New("scheduler self-test failed")
	}
	return result, nil
}
