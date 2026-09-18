// MemoryAI 10.0 Root Memory Image
// The boot kernel only transfers control. This image is carried inside .mem and
// provides generic physical/data primitives; cognitive policy lives in Memory programs.
package main

import (
	"archive/zip"
	"bytes"
	"crypto/sha256"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"
	"unicode"
)

const imageVersion = "28.9.0-memory-fabric-sovereign"
const memoryABI = "memoryai-memory-abi-v1"

type Manifest struct {
	Role        string            `json:"role,omitempty"`
	Format      string            `json:"format"`
	Version     string            `json:"version"`
	MemoryABI   string            `json:"memory_abi,omitempty"`
	BodyID      string            `json:"body_id,omitempty"`
	Root        string            `json:"root"`
	GenesisPath string            `json:"genesis_path"`
	Entry       map[string]string `json:"entry"`
	Hashes      map[string]string `json:"hashes,omitempty"`
	Store       StoreManifest     `json:"store"`
}

type Op struct {
	Code string            `json:"code"`
	A    string            `json:"a,omitempty"`
	B    string            `json:"b,omitempty"`
	C    string            `json:"c,omitempty"`
	Args map[string]string `json:"args,omitempty"`
}

type ResourceBudget struct {
	MaxOps             int   `json:"max_ops,omitempty"`
	MaxMemoryWrites    int   `json:"max_memory_writes,omitempty"`
	MaxEvents          int   `json:"max_events,omitempty"`
	MaxCPUUS           int64 `json:"max_cpu_us,omitempty"`
	MaxAllocBytes      int64 `json:"max_alloc_bytes,omitempty"`
	MaxHeapGrowthBytes int64 `json:"max_heap_growth_bytes,omitempty"`
}

type Memory struct {
	ID               string         `json:"id"`
	Layer            string         `json:"layer"`
	Generation       int            `json:"generation"`
	Parents          []string       `json:"parents,omitempty"`
	Tags             []string       `json:"tags,omitempty"`
	Content          string         `json:"content,omitempty"`
	Executable       bool           `json:"executable,omitempty"`
	InputPattern     map[string]any `json:"input_pattern,omitempty"`
	OutputEffect     map[string]any `json:"output_effect,omitempty"`
	SuccessHistory   []string       `json:"success_history,omitempty"`
	FailureHistory   []string       `json:"failure_history,omitempty"`
	MutationVariants []string       `json:"mutation_variants,omitempty"`
	Trigger          []string       `json:"trigger,omitempty"`
	Capabilities     []string       `json:"capabilities,omitempty"`
	CapabilitySig    string         `json:"capability_sig,omitempty"`
	Budget           ResourceBudget `json:"budget,omitempty"`
	Revision         uint64         `json:"revision,omitempty"`
	Program          []Op           `json:"program,omitempty"`
	State            map[string]any `json:"state,omitempty"`
	RuntimeExecCount uint64         `json:"runtime_exec_count,omitempty"`
	CreatedUnix      int64          `json:"created_unix,omitempty"`
}

type Genesis struct {
	Format      string `json:"format"`
	Root        string `json:"root"`
	MemoryCount int    `json:"memory_count"`
}

type Engine struct {
	bodyPath           string
	manifest           Manifest
	genesis            Genesis
	store              *IndexedStore
	cache              map[string]*Memory
	newIDs             map[string]bool
	mu                 sync.Mutex
	dataMu             *sync.RWMutex
	trace              []string
	dirty              bool
	dirtyIDs           map[string]bool
	deletedIDs         map[string]bool
	spaces             map[string]*Engine
	writeSpace         string
	spaceMu            sync.RWMutex
	pageIns            uint64
	speculative        bool
	speculativeBlocked uint32
	tagAdded           map[string]map[string]bool
	tagRemoved         map[string]map[string]bool
	eventStats         eventRuntimeStats
	externalIOMu       sync.Mutex
}

type Frame struct {
	Vars          map[string]string   `json:"vars"`
	Lists         map[string][]string `json:"lists,omitempty"`
	Output        []string            `json:"output,omitempty"`
	Events        []PhysicalEvent     `json:"-"`
	opCount       int
	memoryWrites  int
	eventCount    int
	resourceScope *frameResourceScope
	resourceStart resourceSample
}

func main() {
	// Native deployment control path. This does not load Memory.mem.
	// Usage: Kernel client <unix-socket> health|persist|run|activate|input ...
	if len(os.Args) >= 4 && os.Args[1] == "client" {
		if err := runDaemonClient(os.Args[2], os.Args[3:]); err != nil {
			die(err)
		}
		return
	}
	// Native two-file runtime: Kernel <Memory.mem> [command...]
	if len(os.Args) >= 2 && os.Args[1] == "--version" {
		fmt.Println(imageVersion)
		return
	}
	if len(os.Args) >= 2 && os.Args[1] == "mem-node" {
		if len(os.Args) < 4 {
			die(errors.New("usage: Kernel mem-node <storage-dir> <listen-address>"))
		}
		if err := serveMemNode(os.Args[2], os.Args[3]); err != nil {
			die(err)
		}
		return
	}
	if len(os.Args) < 2 {
		die(errors.New("usage: Kernel client <socket> health|persist|run|activate|input ... OR Kernel <Memory.mem> [version|inspect|fsck|run|run-debug|live|persist|lineage|runtime-info|parallel-selftest|activation-info|activation-qualify|scheduler-info|scheduler-classify|scheduler-selftest|activate|input|structure-export|structure-export-closure|structure-sync|structure-delete|executable-list|source-list|source-upsert|source-delete|source-test|mesh-info|mesh-security-selftest|daemon] OR Kernel mem-node <dir> <listen>"))
	}
	body, err := filepath.Abs(os.Args[1])
	if err != nil {
		die(err)
	}
	e, err := loadEngineWithMutationJournal(body)
	if err != nil {
		die(err)
	}
	bindMeshEngine(e)
	if err := migrateLegacyRuntimeState(e); err != nil {
		die(err)
	}
	if err := recoverMeshJournalAfterBind(e); err != nil {
		die(err)
	}
	// A core Memory body reattaches every registered local Fabric segment on boot.
	// The registry is cognitive state; Kernel only performs the physical mounts it names.
	if e.manifest.Role == "core" {
		// Automatic Memory.N.mem files are physical shards and are recovered
		// independently from Memory-owned registry semantics.
		e.mountAutomaticStorageShards()
		e.mountRegisteredStorageForIntegrity()
	}
	defer e.close()
	args := os.Args[2:]
	// A storage .mem is a passive memory container, never an AI runtime.
	// Inspect/fsck/persist remain available for physical maintenance only.
	cmd := ""
	if len(args) > 0 {
		cmd = args[0]
	}
	if e.manifest.Role != "core" && (cmd == "" || cmd == "run" || cmd == "run-debug" || cmd == "live" || cmd == "lineage") {
		die(fmt.Errorf("Memory role %q is passive storage and cannot execute cognition", e.manifest.Role))
	}
	if len(args) == 0 {
		// No implicit cognition entry exists. Runtime cognition starts only through
		// physical events (daemon input/experience/idle) or an explicit run target.
		b, _ := json.MarshalIndent(map[string]any{"ok": true, "version": imageVersion, "memory_count": e.localMemoryCount(), "cognition_entry": "event-trigger-ecology", "root_fallback": false}, "", "  ")
		fmt.Println(string(b))
		return
	}
	switch args[0] {
	case "version":
		fmt.Println(imageVersion)
	case "inspect":
		err = e.inspect()
	case "fsck":
		err = e.fsck()
	case "run":
		if len(args) < 2 {
			err = errors.New("run requires Memory id/tag")
		} else {
			f := frameFromArgs(args[2:])
			err = e.run(args[1], f)
			if err == nil {
				printFrame(f)
			}
		}
	case "run-debug":
		if len(args) < 2 {
			err = errors.New("run-debug requires Memory id/tag")
		} else {
			f := frameFromArgs(args[2:])
			err = e.run(args[1], f)
			if err == nil {
				b, _ := json.MarshalIndent(map[string]any{"frame": f, "page_ins": e.pageIns, "hot_cache": len(e.cache), "trace": e.trace}, "", "  ")
				fmt.Println(string(b))
			}
		}
	case "live":
		n := 1
		if len(args) >= 2 {
			n, _ = strconv.Atoi(args[1])
			if n < 1 {
				n = 1
			}
		}
		f := newFrame()
		for i := 0; i < n; i++ {
			f.Vars["cycle"] = strconv.Itoa(i)
			if err = e.fireEvent("idle", "", f); err != nil {
				break
			}
		}
		if err == nil {
			printFrame(f)
		}
	case "persist":
		out := body + ".grown.mem"
		if len(args) >= 2 {
			out = args[1]
		}
		err = e.saveBody(out)
		if err == nil {
			fmt.Println(out)
		}
	case "lineage":
		if len(args) < 2 {
			err = errors.New("lineage requires id")
		} else {
			err = e.lineage(args[1])
		}
	case "structure-export":
		if len(args) < 2 {
			err = errors.New("structure-export requires one or more Memory ids")
		} else {
			err = e.exportStructures(args[1:])
		}
	case "structure-export-closure":
		if len(args) < 2 {
			err = errors.New("structure-export-closure requires one or more Memory ids")
		} else {
			err = e.exportStructureClosure(args[1:])
		}
	case "structure-sync":
		if len(args) != 2 {
			err = errors.New("structure-sync requires required-structures.json")
		} else {
			err = e.syncRequiredStructures(args[1])
		}
	case "structure-delete":
		if len(args) < 2 {
			err = errors.New("structure-delete requires one or more Memory ids")
		} else {
			err = e.deleteStructures(args[1:])
		}
	case "executable-list":
		var ms []*Memory
		ms, err = e.allMemories()
		if err == nil {
			type row struct {
				ID           string   `json:"id"`
				ProgramOps   int      `json:"program_ops"`
				Trigger      []string `json:"trigger,omitempty"`
				Tags         []string `json:"tags,omitempty"`
				Capabilities []string `json:"capabilities,omitempty"`
				Revision     uint64   `json:"revision"`
			}
			rows := []row{}
			for _, m := range ms {
				if len(m.Program) > 0 || len(m.Trigger) > 0 {
					rows = append(rows, row{m.ID, len(m.Program), append([]string(nil), m.Trigger...), append([]string(nil), m.Tags...), append([]string(nil), m.Capabilities...), m.Revision})
				}
			}
			sort.Slice(rows, func(i, j int) bool { return rows[i].ID < rows[j].ID })
			b, _ := json.MarshalIndent(map[string]any{"count": len(rows), "memories": rows}, "", "  ")
			fmt.Println(string(b))
		}
	case "runtime-info":
		fmt.Println(parallelRuntimeJSON())
	case "parallel-selftest":
		err = parallelSelfTest()
	case "activation-info":
		if err = globalActivationRuntime.Build(e); err == nil {
			fmt.Println(activationInfoJSON())
		}
	case "activation-qualify":
		var qr ActivationQualificationReport
		qr, err = qualifySparseActivation(e)
		b, _ := json.MarshalIndent(qr, "", "  ")
		fmt.Println(string(b))
	case "scheduler-info":
		b, _ := json.MarshalIndent(globalTxnScheduler.Info(), "", "  ")
		fmt.Println(string(b))
	case "scheduler-classify":
		ids, er := e.storeAllIDs()
		if er != nil {
			err = er
		} else {
			classes := globalTxnScheduler.classifyTargets(e, ids)
			b, _ := json.MarshalIndent(classes, "", "  ")
			fmt.Println(string(b))
		}
	case "scheduler-selftest":
		var out map[string]any
		out, err = e.schedulerSelfTest()
		b, _ := json.MarshalIndent(out, "", "  ")
		fmt.Println(string(b))
	case "source-list":
		b, _ := json.MarshalIndent(map[string]any{"ok": true, "sources": e.listSourceAdapters()}, "", "  ")
		fmt.Println(string(b))
	case "source-upsert":
		v := kvArgs(args[1:])
		if strings.TrimSpace(v["id"]) == "" {
			err = errors.New("source-upsert requires id=<id>")
		} else {
			var m *Memory
			m, err = e.upsertSourceAdapter(sourceAdapterFromVars(v))
			if err == nil {
				err = e.persistAll()
			}
			if err == nil {
				b, _ := json.MarshalIndent(map[string]any{"ok": true, "id": m.ID, "revision": m.Revision}, "", "  ")
				fmt.Println(string(b))
			}
		}
	case "source-delete":
		if len(args) < 2 {
			err = errors.New("source-delete requires id")
		} else {
			err = e.deleteSourceAdapter(args[1])
			if err == nil {
				err = e.persistAll()
			}
		}
	case "source-test":
		if len(args) < 2 {
			err = errors.New("source-test requires id")
		} else {
			to := time.Duration(15) * time.Second
			if len(args) > 2 {
				to = sourceParseTimeout(args[2])
			}
			var out map[string]any
			out, err = e.testSourceAdapter(args[1], to)
			b, _ := json.MarshalIndent(out, "", "  ")
			if out != nil {
				fmt.Println(string(b))
			}
		}
	case "mesh-security-selftest":
		var out map[string]any
		out, err = meshSecuritySelfTest()
		b, _ := json.MarshalIndent(out, "", "  ")
		fmt.Println(string(b))
	case "mesh-info":
		b, _ := json.MarshalIndent(meshInfoMap(), "", "  ")
		fmt.Println(string(b))
	case "activate":
		if len(args) < 2 {
			err = errors.New("activate requires stimulus text")
		} else {
			if err = globalActivationRuntime.Build(e); err == nil {
				pageCap := 0
				if len(args) >= 3 {
					pageCap, _ = strconv.Atoi(args[2])
				}
				var ar ActivationResult
				ar, err = globalActivationRuntime.Activate(e, args[1], pageCap)
				if err == nil {
					b, _ := json.MarshalIndent(ar, "", "  ")
					fmt.Println(string(b))
				}
			}
		}
	case "input":
		if len(args) < 2 {
			err = errors.New("input requires raw UTF-8 surface bytes")
		} else {
			f := newFrame()
			f.Vars["raw_input"] = args[1]
			f.Vars["surface"] = args[1]
			f.Vars["source_id"] = "source.human.input"
			err = e.fireEvent("input", "", f)
			if err == nil {
				printFrame(f)
			}
		}
	case "daemon":
		sock := filepath.Join(filepath.Dir(e.bodyPath), "..", "run", "memoryai.sock")
		if len(args) >= 2 && strings.TrimSpace(args[1]) != "" {
			sock = args[1]
		}
		err = e.runDaemon(sock)
	default:
		err = fmt.Errorf("unknown Memory Image command %q", args[0])
	}
	if err != nil {
		die(err)
	}
}
func die(err error)    { fmt.Fprintln(os.Stderr, "MEMORY ERROR:", err); os.Exit(1) }
func newFrame() *Frame { return &Frame{Vars: map[string]string{}, Lists: map[string][]string{}} }
func frameFromArgs(xs []string) *Frame {
	f := newFrame()
	for _, x := range xs {
		if i := strings.IndexByte(x, '='); i > 0 {
			f.Vars[x[:i]] = x[i+1:]
		}
	}
	return f
}
func printFrame(f *Frame) {
	if len(f.Output) > 0 {
		fmt.Println(strings.Join(f.Output, "\n"))
		return
	}
	b, _ := json.MarshalIndent(f, "", "  ")
	fmt.Println(string(b))
}

func loadEngine(body string) (*Engine, error) {
	zr, err := zip.OpenReader(body)
	if err != nil {
		return nil, err
	}
	defer zr.Close()
	if err := validateUniqueZipEntryNames(&zr.Reader); err != nil {
		return nil, err
	}
	var mf Manifest
	if err = readJSONZip(&zr.Reader, "manifest.json", &mf); err != nil {
		return nil, err
	}
	if mf.Format != "memoryai-body-v2" {
		return nil, fmt.Errorf("unsupported format %q", mf.Format)
	}
	if mf.MemoryABI == "" {
		// memoryai-body-v2 predates an explicit ABI marker but is ABI-v1 compatible.
		mf.MemoryABI = memoryABI
	}
	if mf.MemoryABI != memoryABI {
		return nil, fmt.Errorf("unsupported Memory ABI %q", mf.MemoryABI)
	}
	if err = verifyBodyLoadIntegrity(&zr.Reader, mf); err != nil {
		return nil, err
	}
	if mf.BodyID == "" {
		// Legacy bodies remain readable after AI program generations change.
		// Body identity is derived from the physical image only when it was not stored explicitly.
		if fi, er := os.Stat(body); er == nil {
			mf.BodyID = fmt.Sprintf("legacy-body-%x-%d", sha256.Sum256([]byte(filepath.Clean(body))), fi.Size())
		}
	}
	var g Genesis
	if err = readJSONZip(&zr.Reader, mf.GenesisPath, &g); err != nil {
		return nil, err
	}
	if mf.Role == "" {
		if g.Root == "" {
			mf.Role = "storage"
		} else {
			mf.Role = "core"
		}
	}
	if mf.Role != "core" && mf.Role != "storage" {
		return nil, fmt.Errorf("unsupported Memory role %q", mf.Role)
	}
	st, err := openIndexedStore(body, zr, mf.Store)
	if err != nil {
		return nil, err
	}
	if err = validateIndexedStore(st); err != nil {
		st.Close()
		return nil, err
	}
	e := &Engine{bodyPath: body, manifest: mf, genesis: g, store: st, cache: map[string]*Memory{}, newIDs: map[string]bool{}, dirtyIDs: map[string]bool{}, deletedIDs: map[string]bool{}, spaces: map[string]*Engine{}, dataMu: &sync.RWMutex{}, tagAdded: map[string]map[string]bool{}, tagRemoved: map[string]map[string]bool{}}
	return e, nil
}

func (e *Engine) close() {
	if e == nil {
		return
	}
	e.spaceMu.Lock()
	spaces := make([]*Engine, 0, len(e.spaces))
	for path, sp := range e.spaces {
		if sp != nil {
			spaces = append(spaces, sp)
		}
		delete(e.spaces, path)
	}
	e.writeSpace = ""
	e.spaceMu.Unlock()
	for _, sp := range spaces {
		releaseBodyCloseTxn := acquireRemoteBodyTransaction(sp.bodyPath)
		forgetFabricOwner(sp)
		sp.closeStore()
		releaseBodyCloseTxn()
	}
	forgetFabricOwner(e)
	e.closeStore()
	releasePassiveBodyTransaction(e)
}

func (e *Engine) resolveIDLocal(id string) (*Memory, error) {
	e.dataMu.RLock()
	if e.deletedIDs[id] {
		e.dataMu.RUnlock()
		return nil, io.EOF
	}
	if m := e.cache[id]; m != nil {
		e.dataMu.RUnlock()
		return m, nil
	}
	e.dataMu.RUnlock()
	m, err := e.storeGetID(id)
	if err != nil {
		return nil, err
	}
	if err := recordSpeculativeBaseline(e, id, m); err != nil {
		return nil, err
	}
	e.dataMu.Lock()
	defer e.dataMu.Unlock()
	if e.deletedIDs[id] {
		return nil, io.EOF
	}
	if existing := e.cache[id]; existing != nil {
		return existing, nil
	}
	e.cache[id] = m
	e.pageIns++
	return m, nil
}

type RemoteMemoryEndpoint struct {
	DescriptorID string
	Host         string
	Port         string
	Name         string
	BodyID       string
	MemoryABI    string
	Status       string
	Timeout      time.Duration
}

func (e *Engine) remoteMemoryEndpoints() []RemoteMemoryEndpoint {
	if e == nil || e.manifest.Role != "core" {
		return nil
	}
	ids := []string{}
	seen := map[string]bool{}
	if reg, er := e.resolveIDLocal("memory.space.registry"); er == nil {
		for _, id := range stateList(reg.State["remote_spaces"]) {
			if id != "" && !seen[id] {
				seen[id] = true
				ids = append(ids, id)
			}
		}
	}
	if tagged, er := e.listTagLocal("physical.storage.remote.endpoint"); er == nil {
		for _, id := range tagged {
			if id != "" && !seen[id] {
				seen[id] = true
				ids = append(ids, id)
			}
		}
	}
	out := make([]RemoteMemoryEndpoint, 0, len(ids))
	for _, id := range ids {
		m, er := e.resolveIDLocal(id)
		if er != nil || m == nil {
			continue
		}
		host := fmt.Sprint(m.State["host"])
		port := fmt.Sprint(m.State["port"])
		name := fmt.Sprint(m.State["name"])
		if host == "" || port == "" || name == "" {
			continue
		}
		to := parseTimeout(fmt.Sprint(m.State["timeout_ms"]))
		if to <= 0 || to > 5*time.Second {
			to = 1200 * time.Millisecond
		}
		out = append(out, RemoteMemoryEndpoint{
			DescriptorID: id,
			Host:         host,
			Port:         port,
			Name:         name,
			BodyID:       fmt.Sprint(m.State["body_id"]),
			MemoryABI:    fmt.Sprint(m.State["memory_abi"]),
			Status:       fmt.Sprint(m.State["status"]),
			Timeout:      to,
		})
	}
	return out
}

func remoteMemoryGet(ep RemoteMemoryEndpoint, id string) (*Memory, error) {
	resp, err := remoteSpaceRequest(ep.Host, ep.Port, map[string]any{"op": "get", "name": ep.Name, "id": id}, ep.Timeout)
	if err != nil {
		return nil, err
	}
	var rr struct {
		OK        bool    `json:"ok"`
		Memory    *Memory `json:"memory"`
		BodyID    string  `json:"body_id"`
		MemoryABI string  `json:"memory_abi"`
	}
	if err = json.Unmarshal([]byte(resp), &rr); err != nil {
		return nil, err
	}
	if !rr.OK || rr.Memory == nil {
		return nil, io.EOF
	}
	if strings.TrimSpace(rr.Memory.ID) != strings.TrimSpace(id) {
		return nil, fmt.Errorf("remote Memory integrity: requested id %q but endpoint returned %q", id, rr.Memory.ID)
	}
	if ep.BodyID != "" && rr.BodyID != ep.BodyID {
		return nil, fmt.Errorf("remote Memory integrity: endpoint BodyID drift: expected=%q got=%q", ep.BodyID, rr.BodyID)
	}
	if ep.MemoryABI != "" && rr.MemoryABI != ep.MemoryABI {
		return nil, fmt.Errorf("remote Memory integrity: endpoint Memory ABI drift: expected=%q got=%q", ep.MemoryABI, rr.MemoryABI)
	}
	if rr.MemoryABI != "" && rr.MemoryABI != memoryABI {
		return nil, fmt.Errorf("remote Memory ABI %q incompatible with %q", rr.MemoryABI, memoryABI)
	}
	return rr.Memory, nil
}

func (e *Engine) resolveRemoteID(id string) (*Memory, error) {
	id = strings.TrimSpace(id)
	if id == "" {
		return nil, io.EOF
	}
	var selected *Memory
	selectedDigest := ""
	var integrityErr error
	for _, ep := range e.remoteMemoryEndpoints() {
		m, er := remoteMemoryGet(ep, id)
		if er != nil {
			if !errors.Is(er, io.EOF) && strings.Contains(er.Error(), "remote Memory integrity:") && integrityErr == nil {
				integrityErr = er
			}
			continue
		}
		if m == nil {
			continue
		}
		digest := memoryJSONDigest(m)
		if selected == nil {
			selected = m
			selectedDigest = digest
			continue
		}
		if digest != selectedDigest {
			return nil, fmt.Errorf("remote Memory identity conflict: id=%q reachable replicas have divergent structural digests %s and %s", id, selectedDigest, digest)
		}
	}
	if integrityErr != nil {
		return nil, integrityErr
	}
	if selected != nil {
		// Direct read: this object is intentionally ephemeral. It is NOT added to
		// Memory.mem cache/store and therefore does not become a local replica.
		return selected, nil
	}
	return nil, io.EOF
}

func (e *Engine) remoteTagIDs(tag string) []string {
	seen := map[string]bool{}
	out := []string{}
	for _, ep := range e.remoteMemoryEndpoints() {
		resp, er := remoteSpaceRequest(ep.Host, ep.Port, map[string]any{"op": "tag", "name": ep.Name, "id": tag}, ep.Timeout)
		if er != nil {
			continue
		}
		var rr struct {
			OK  bool     `json:"ok"`
			IDs []string `json:"ids"`
		}
		if json.Unmarshal([]byte(resp), &rr) != nil || !rr.OK {
			continue
		}
		for _, id := range rr.IDs {
			if id != "" && !seen[id] {
				seen[id] = true
				out = append(out, id)
			}
		}
	}
	sort.Strings(out)
	return out
}

func (e *Engine) resolveID(id string) (*Memory, error) {
	if e == nil {
		return nil, io.EOF
	}
	if e.speculative {
		if m, err := e.resolveIDLocal(id); err == nil {
			return m, nil
		}
	} else {
		root := fabricRootFor(e)
		if root == nil {
			root = e
		}
		_, m, err := root.resolveLocalFabricMemory(id)
		if err == nil {
			return m, nil
		}
		if err != io.EOF {
			return nil, err
		}
		e = root
	}
	if m, err := e.resolveRemoteID(id); err == nil {
		return m, nil
	} else if !errors.Is(err, io.EOF) {
		return nil, err
	}
	// Sovereign Mesh is a virtual remote address space. Shared Memory is read
	// directly from a live origin and is never imported into local Memory.mem.
	if mr := meshRuntimeCurrent(); mr != nil && mr.role != "standalone" {
		if res, er := mr.sharedFetch(id); er == nil && res.Memory != nil {
			return res.Memory, nil
		}
	}
	return nil, io.EOF
}

// resolveMutable resolves a mutable Memory in the local mounted Memory Fabric.
// Remote Memory is never silently cached or mutated through a local pointer; remote
// mutation must remain an explicit remote operation at the owning node. The Kernel
// performs only deterministic address resolution and does not interpret cognitive
// Layer/Generation semantics.
func (e *Engine) resolveMutable(idOrTag string) (*Memory, error) {
	if e == nil {
		return nil, fmt.Errorf("mutable memory %q not found in mounted Memory Fabric", idOrTag)
	}
	if e.speculative {
		if m, err := e.resolveIDLocal(idOrTag); err == nil {
			return m, nil
		}
		ids, err := e.listTagLocal(idOrTag)
		if err != nil && err != io.EOF {
			return nil, err
		}
		sort.Strings(ids)
		if len(ids) > 1 {
			return nil, fmt.Errorf("ambiguous mutable Memory tag %q matched %d identities; Memory must select an explicit ID", idOrTag, len(ids))
		}
		if len(ids) == 1 {
			if m, er := e.resolveIDLocal(ids[0]); er == nil {
				return m, nil
			} else if er != io.EOF {
				return nil, er
			}
		}
		return nil, fmt.Errorf("mutable memory %q not found in mounted Memory Fabric", idOrTag)
	}

	root := fabricRootFor(e)
	if root == nil {
		root = e
	}
	_, m, err := root.resolveLocalFabricMemory(idOrTag)
	if err == nil {
		return m, nil
	}
	if err != io.EOF {
		return nil, err
	}
	ids, _, err := root.listTagFabricOwned(idOrTag)
	if err != nil {
		return nil, err
	}
	if len(ids) == 0 {
		return nil, fmt.Errorf("mutable memory %q not found in mounted Memory Fabric", idOrTag)
	}
	if len(ids) > 1 {
		return nil, fmt.Errorf("ambiguous mutable Memory tag %q matched %d identities; Memory must select an explicit ID", idOrTag, len(ids))
	}
	_, m, er := root.resolveLocalFabricMemory(ids[0])
	if er == nil {
		return m, nil
	}
	if er != io.EOF {
		return nil, er
	}
	return nil, fmt.Errorf("mutable memory %q not found in mounted Memory Fabric", idOrTag)
}

func (e *Engine) tagDeltaAddLocked(id, tag string) {
	if tag == "" {
		return
	}
	if e.tagAdded[tag] == nil {
		e.tagAdded[tag] = map[string]bool{}
	}
	e.tagAdded[tag][id] = true
	if e.tagRemoved[tag] != nil {
		delete(e.tagRemoved[tag], id)
	}
}
func (e *Engine) tagDeltaRemoveLocked(id, tag string) {
	if tag == "" {
		return
	}
	if e.tagRemoved[tag] == nil {
		e.tagRemoved[tag] = map[string]bool{}
	}
	e.tagRemoved[tag][id] = true
	if e.tagAdded[tag] != nil {
		delete(e.tagAdded[tag], id)
	}
}
func (e *Engine) listTagLocal(tag string) ([]string, error) {
	ids, err := e.storeTagIDs(tag)
	if err != nil && err != io.EOF {
		return nil, err
	}
	seen := map[string]bool{}
	out := []string{}
	e.dataMu.RLock()
	removed := e.tagRemoved[tag]
	for _, id := range ids {
		if e.deletedIDs[id] || (removed != nil && removed[id]) {
			continue
		}
		seen[id] = true
		out = append(out, id)
	}
	if add := e.tagAdded[tag]; add != nil {
		for id := range add {
			if !e.deletedIDs[id] && !seen[id] {
				seen[id] = true
				out = append(out, id)
			}
		}
	}
	// Cache verification is required for old store-index entries whose tag was
	// removed before a persistence compaction.
	for id, m := range e.cache {
		if e.deletedIDs[id] {
			continue
		}
		has := contains(m.Tags, tag)
		if has && !seen[id] {
			seen[id] = true
			out = append(out, id)
		}
		if !has && seen[id] && removed != nil && removed[id] { /* excluded above */
		}
	}
	e.dataMu.RUnlock()
	sort.Strings(out)
	return out, nil
}
func (e *Engine) listTag(tag string) ([]string, error) {
	out, err := e.listTagLocal(tag)
	if err != nil {
		return nil, err
	}
	seen := map[string]bool{}
	for _, id := range out {
		seen[id] = true
	}
	e.spaceMu.RLock()
	spaces := make([]*Engine, 0, len(e.spaces))
	for _, sp := range e.spaces {
		spaces = append(spaces, sp)
	}
	e.spaceMu.RUnlock()
	for _, sp := range spaces {
		ids, er := sp.listTagLocal(tag)
		if er != nil {
			return nil, er
		}
		for _, id := range ids {
			if !seen[id] {
				seen[id] = true
				out = append(out, id)
			}
		}
	}
	for _, id := range e.remoteTagIDs(tag) {
		if !seen[id] {
			seen[id] = true
			out = append(out, id)
		}
	}
	if mr := meshRuntimeCurrent(); mr != nil && mr.role != "standalone" {
		if res, er := mr.authorityRPC(MeshRequest{Op: "shared_search", Tags: []string{tag}}); er == nil && res.OK {
			for _, rec := range res.Records {
				if rec.MemoryID != "" && !seen[rec.MemoryID] {
					seen[rec.MemoryID] = true
					out = append(out, rec.MemoryID)
				}
			}
		}
	}
	sort.Strings(out)
	return out, nil
}

// resolveExecutable resolves executable Memory from the local mounted Memory Fabric.
// Mounted .mem containers are part of the same logical AI body. Remote executable
// Memory is executed at its origin through Mesh/remote execution and is not imported.
func (e *Engine) resolveExecutable(idOrTag string) (*Memory, error) {
	m, err := e.resolveMutable(idOrTag)
	if err != nil {
		return nil, err
	}
	if len(m.Program) == 0 && len(m.Trigger) == 0 {
		return nil, fmt.Errorf("memory %q is not executable", idOrTag)
	}
	return m, nil
}

func (e *Engine) resolve(idOrTag string) (*Memory, error) {
	m, err := e.resolveID(idOrTag)
	if err == nil {
		return m, nil
	}
	if err != io.EOF {
		return nil, err
	}
	ids, err := e.listTag(idOrTag)
	if err != nil {
		return nil, err
	}
	if len(ids) == 0 {
		return nil, fmt.Errorf("memory %q not found", idOrTag)
	}
	sort.Strings(ids)
	if len(ids) > 1 {
		return nil, fmt.Errorf("ambiguous Memory tag %q matched %d identities; Memory must select an explicit ID", idOrTag, len(ids))
	}
	if m, er := e.resolveID(ids[0]); er == nil {
		return m, nil
	} else if er != io.EOF {
		return nil, er
	}
	return nil, fmt.Errorf("memory %q not found", idOrTag)
}

func readJSONZip(zr *zip.Reader, name string, dst any) error {
	file, err := uniqueZipFile(zr, name)
	if err != nil {
		return err
	}
	b, err := readZipFileBounded(file, hardJSONZipEntryMaxBytes, "Memory metadata")
	if err != nil {
		return err
	}
	return json.Unmarshal(b, dst)
}
func readZipBytes(zr *zip.Reader, name string) ([]byte, error) {
	file, err := uniqueZipFile(zr, name)
	if err != nil {
		return nil, err
	}
	return readZipFileBounded(file, hardOpaqueZipEntryMaxBytes, "Memory opaque entry")
}

func (e *Engine) run(idOrTag string, f *Frame) error {
	m, err := e.resolveExecutable(expand(idOrTag, f.Vars))
	if err != nil {
		return err
	}
	// Execution count and trace are physical runtime telemetry only. Kernel does
	// not inspect Memory semantic tags to decide whether an execution is visible.
	executionOwner := e.ownerOf(m.ID)
	if executionOwner == nil {
		return fmt.Errorf("physical execution owner unavailable: %s", m.ID)
	}
	executionOwner.dataMu.Lock()
	m.RuntimeExecCount++
	program := append([]Op(nil), m.Program...)
	executionOwner.dataMu.Unlock()
	e.mu.Lock()
	e.trace = append(e.trace, m.ID)
	e.mu.Unlock()
	labels := map[string]int{}
	for i, op := range program {
		if op.Code == "label" {
			labels[op.A] = i
		}
	}
	budgetStart := sampleResources()
	_, restoreResourceScope := enterFrameResourceScope(f, m.Budget)
	defer restoreResourceScope()
	opsExecuted := 0
	for pc := 0; pc < len(program); pc++ {
		op := program[pc]
		if err := checkPrimitiveCapability(m, op.Code); err != nil {
			return fmt.Errorf("%s pc=%d %s: %w", m.ID, pc, op.Code, err)
		}
		if err := checkResourceBudgetBeforePrimitive(m.Budget, budgetStart, opsExecuted, f, op.Code); err != nil {
			return fmt.Errorf("%s: %w", m.ID, err)
		}
		next, err := e.execPrimitive(m, op, f, pc, labels)
		opsExecuted++
		if err != nil {
			return fmt.Errorf("%s pc=%d %s: %w", m.ID, pc, op.Code, err)
		}
		if err := checkResourceBudget(m.Budget, budgetStart, opsExecuted); err != nil {
			return fmt.Errorf("%s: %w", m.ID, err)
		}
		if next >= 0 {
			pc = next - 1
		}
	}
	return nil
}

// execPrimitive intentionally contains only generic data/control/memory/physical primitives.
// Domain meanings such as selection, credit, source provenance, HTTP parsing and learning
// are expressed by Memory programs in Genesis, not as dedicated cases here.
func (e *Engine) execPrimitive(self *Memory, op Op, f *Frame, pc int, labels map[string]int) (int, error) {
	x := func(s string) string { return expand(s, f.Vars) }
	if e.speculative && (speculativeForbiddenPrimitive(op.Code) || speculativeAdditionalForbiddenPrimitive(op.Code)) {
		atomic.StoreUint32(&e.speculativeBlocked, 1)
		return -1, fmt.Errorf("%w: %s", errSpeculativeSideEffect, op.Code)
	}
	switch op.Code {
	case "label":
	case "set":
		f.Vars[op.A] = x(op.B)
	case "copy":
		f.Vars[op.A] = f.Vars[op.B]
	case "var_default":
		if _, ok := f.Vars[op.A]; !ok || f.Vars[op.A] == "" {
			f.Vars[op.A] = x(op.B)
		}
	case "var_set":
		f.Vars[x(op.A)] = x(op.B)
	case "var_get":
		f.Vars[op.B] = f.Vars[x(op.A)]
	case "self":
		f.Vars[op.A] = self.ID
	case "state_get":
		target := self
		if op.A != "" {
			var err error
			target, err = e.resolve(x(op.A))
			if err != nil {
				return -1, err
			}
		}
		v, ok := target.State[x(op.B)]
		if !ok || v == nil {
			f.Vars[op.C] = ""
		} else {
			f.Vars[op.C] = fmt.Sprint(v)
		}
	case "state_keys":
		target := self
		if op.A != "" {
			var err error
			target, err = e.resolve(x(op.A))
			if err != nil {
				return -1, err
			}
		}
		keys := make([]string, 0, len(target.State))
		for k := range target.State {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		f.Lists[op.B] = keys
	case "state_has":
		target := self
		if op.A != "" {
			var err error
			target, err = e.resolve(x(op.A))
			if err != nil {
				return -1, err
			}
		}
		_, ok := target.State[x(op.B)]
		if ok {
			f.Vars[op.C] = "1"
		} else {
			f.Vars[op.C] = "0"
		}
	case "state_list_len":
		target, err := e.resolve(x(op.A))
		if err != nil {
			return -1, err
		}
		f.Vars[op.C] = strconv.Itoa(len(stateList(target.State[x(op.B)])))
	case "state_list_get":
		target, err := e.resolve(x(op.A))
		if err != nil {
			return -1, err
		}
		idx, _ := strconv.Atoi(x(op.Args["index"]))
		ls := stateList(target.State[x(op.B)])
		if idx >= 0 && idx < len(ls) {
			f.Vars[op.C] = ls[idx]
		} else {
			f.Vars[op.C] = ""
		}
	case "state_list_contains":
		target, err := e.resolve(x(op.A))
		if err != nil {
			return -1, err
		}
		want := x(op.C)
		found := "0"
		for _, v := range stateList(target.State[x(op.B)]) {
			if v == want {
				found = "1"
				break
			}
		}
		if k := op.Args["out"]; k != "" {
			f.Vars[k] = found
		} else {
			f.Vars[op.C] = found
		}
	case "state_list_append":
		target, err := e.resolveMutable(x(op.A))
		if err != nil {
			return -1, err
		}
		owner := e.ownerOf(target.ID)
		if owner == nil {
			return -1, fmt.Errorf("mutable owner unavailable: %s", target.ID)
		}
		owner.dataMu.Lock()
		if target.State == nil {
			target.State = map[string]any{}
		}
		key := x(op.B)
		ls := stateList(target.State[key])
		ls = append(ls, x(op.C))
		target.State[key] = ls
		target.CapabilitySig = ""
		target.Revision++
		owner.dirty = true
		owner.dirtyIDs[target.ID] = true
		owner.dataMu.Unlock()
		f.memoryWrites++
	case "state_list_unique_append":
		target, err := e.resolveMutable(x(op.A))
		if err != nil {
			return -1, err
		}
		owner := e.ownerOf(target.ID)
		if owner == nil {
			return -1, fmt.Errorf("mutable owner unavailable: %s", target.ID)
		}
		val := x(op.C)
		owner.dataMu.Lock()
		if target.State == nil {
			target.State = map[string]any{}
		}
		key := x(op.B)
		ls := stateList(target.State[key])
		seen := false
		for _, v := range ls {
			if v == val {
				seen = true
				break
			}
		}
		if !seen {
			ls = append(ls, val)
			target.State[key] = ls
			target.CapabilitySig = ""
			target.Revision++
			owner.dirty = true
			owner.dirtyIDs[target.ID] = true
			f.memoryWrites++
		}
		owner.dataMu.Unlock()
		if k := op.Args["added_out"]; k != "" {
			if seen {
				f.Vars[k] = "0"
			} else {
				f.Vars[k] = "1"
			}
		}
	case "state_num_add":
		target, err := e.resolveMutable(x(op.A))
		if err != nil {
			return -1, err
		}
		owner := e.ownerOf(target.ID)
		if owner == nil {
			return -1, fmt.Errorf("mutable owner unavailable: %s", target.ID)
		}
		key := x(op.B)
		delta := num(x(op.C))
		owner.dataMu.Lock()
		if target.State == nil {
			target.State = map[string]any{}
		}
		cur := num(fmt.Sprint(target.State[key]))
		nv := cur + delta
		target.State[key] = ff(nv)
		target.CapabilitySig = ""
		target.Revision++
		owner.dirty = true
		owner.dirtyIDs[target.ID] = true
		owner.dataMu.Unlock()
		f.memoryWrites++
		if k := op.Args["out"]; k != "" {
			f.Vars[k] = ff(nv)
		}
	case "state_set":
		target, err := e.resolveMutable(x(op.A))
		if err != nil {
			return -1, err
		}
		owner := e.ownerOf(target.ID)
		if owner == nil {
			return -1, fmt.Errorf("mutable owner unavailable: %s", target.ID)
		}
		owner.dataMu.Lock()
		if target.State == nil {
			target.State = map[string]any{}
		}
		target.State[x(op.B)] = x(op.C)
		target.CapabilitySig = ""
		target.Revision++
		owner.dirty = true
		owner.dirtyIDs[target.ID] = true
		owner.dataMu.Unlock()
		f.memoryWrites++
	case "field_get":
		target, err := e.resolve(x(op.A))
		if err != nil {
			return -1, err
		}
		f.Vars[op.C] = fieldString(target, x(op.B))
	case "memory_refs":
		target, err := e.resolve(x(op.A))
		if err != nil {
			return -1, err
		}
		refs := []string{}
		seen := map[string]bool{}
		add := func(id string) {
			id = strings.TrimSpace(id)
			if id == "" || id == target.ID || seen[id] {
				return
			}
			if _, er := e.resolveIDLocal(id); er == nil {
				seen[id] = true
				refs = append(refs, id)
			}
		}
		for _, id := range target.Parents {
			add(id)
		}
		e.dataMu.RLock()
		stateCopy := map[string]any{}
		for k, v := range target.State {
			stateCopy[k] = v
		}
		e.dataMu.RUnlock()
		for _, v := range stateCopy {
			switch q := v.(type) {
			case string:
				for _, part := range splitCSV(q) {
					add(part)
				}
			case []string:
				for _, part := range q {
					add(part)
				}
			case []any:
				for _, part := range q {
					if ss, ok := part.(string); ok {
						add(ss)
					}
				}
			}
		}
		sort.Strings(refs)
		f.Lists[op.B] = refs
	case "cache_list":
		e.dataMu.RLock()
		ids := make([]string, 0, len(e.cache))
		for id := range e.cache {
			ids = append(ids, id)
		}
		e.dataMu.RUnlock()
		sort.Strings(ids)
		f.Lists[op.A] = ids
	case "cache_count":
		e.dataMu.RLock()
		n := len(e.cache)
		e.dataMu.RUnlock()
		f.Vars[op.A] = strconv.Itoa(n)
	case "cache_drop":
		id := x(op.A)
		e.dataMu.Lock()
		if !e.dirtyIDs[id] && !e.newIDs[id] {
			delete(e.cache, id)
		}
		e.dataMu.Unlock()
	case "body_count":
		f.Vars[op.A] = strconv.Itoa(localMemoryCountFast(e))
	case "body_list":
		ids, err := boundedLegacyBodyIDs(e)
		if err != nil {
			return -1, err
		}
		f.Lists[op.A] = ids
	case "tag_list":
		ids, err := e.listTag(x(op.A))
		if err != nil {
			return -1, err
		}
		f.Lists[op.B] = ids
	case "list_len":
		f.Vars[op.B] = strconv.Itoa(len(f.Lists[op.A]))
	case "list_get":
		i, _ := strconv.Atoi(x(op.B))
		ls := f.Lists[op.A]
		if i < 0 || i >= len(ls) {
			f.Vars[op.C] = ""
		} else {
			f.Vars[op.C] = ls[i]
		}
	case "num_add":
		f.Vars[op.A] = ff(num(x(op.B)) + num(x(op.C)))
	case "num_sub":
		f.Vars[op.A] = ff(num(x(op.B)) - num(x(op.C)))
	case "num_mul":
		f.Vars[op.A] = ff(num(x(op.B)) * num(x(op.C)))
	case "num_div":
		d := num(x(op.C))
		if d == 0 {
			f.Vars[op.A] = "0"
		} else {
			f.Vars[op.A] = ff(num(x(op.B)) / d)
		}
	case "cmp_gt":
		if num(x(op.B)) > num(x(op.C)) {
			f.Vars[op.A] = "1"
		} else {
			f.Vars[op.A] = "0"
		}
	case "cmp_ge":
		if num(x(op.B)) >= num(x(op.C)) {
			f.Vars[op.A] = "1"
		} else {
			f.Vars[op.A] = "0"
		}
	case "cmp_lt":
		if num(x(op.B)) < num(x(op.C)) {
			f.Vars[op.A] = "1"
		} else {
			f.Vars[op.A] = "0"
		}
	case "cmp_le":
		if num(x(op.B)) <= num(x(op.C)) {
			f.Vars[op.A] = "1"
		} else {
			f.Vars[op.A] = "0"
		}
	case "cmp_eq":
		if x(op.B) == x(op.C) {
			f.Vars[op.A] = "1"
		} else {
			f.Vars[op.A] = "0"
		}
	case "jump":
		p, ok := labels[op.A]
		if !ok {
			return -1, fmt.Errorf("label %s missing", op.A)
		}
		return p, nil
	case "jump_if":
		if truth(x(op.A)) {
			p, ok := labels[op.B]
			if !ok {
				return -1, fmt.Errorf("label %s missing", op.B)
			}
			return p, nil
		}
	case "call":
		if err := e.run(x(op.A), f); err != nil {
			return -1, err
		}
	case "call_isolated":
		child := newFrame()
		for _, k := range splitCSV(x(op.Args["keep"])) {
			child.Vars[k] = f.Vars[k]
		}
		if err := e.run(x(op.A), child); err != nil {
			return -1, err
		}
		for _, k := range splitCSV(x(op.Args["out"])) {
			f.Vars[k] = child.Vars[k]
		}
	case "call_try_isolated":
		child := newFrame()
		for _, k := range splitCSV(x(op.Args["keep"])) {
			child.Vars[k] = f.Vars[k]
		}
		err := e.run(x(op.A), child)
		okout := op.Args["ok_out"]
		if okout != "" {
			if err == nil {
				f.Vars[okout] = "1"
			} else {
				f.Vars[okout] = "0"
			}
		}
		if err == nil {
			for _, k := range splitCSV(x(op.Args["out"])) {
				f.Vars[k] = child.Vars[k]
			}
		}
	case "call_parallel":
		sourceTargets := f.Lists[x(op.Args["targets_list"])]
		maxTargets := parallelFanoutMaxTargets()
		if len(sourceTargets) > maxTargets {
			return -1, fmt.Errorf("call_parallel fan-out exceeds physical target ceiling: targets=%d max=%d", len(sourceTargets), maxTargets)
		}
		targets := append([]string(nil), sourceTargets...)
		results := make([]string, len(targets))
		oks := make([]string, len(targets))
		maxp, _ := strconv.Atoi(x(op.Args["max_parallel"]))
		parallelCPUFor(len(targets), maxp, func(i int) {
			t := targets[i]
			ch := newFrame()
			er := e.run(t, ch)
			if er == nil {
				oks[i] = "1"
				outk := x(op.Args["out"])
				if outk != "" {
					results[i] = ch.Vars[outk]
				}
				if results[i] == "" && len(ch.Output) > 0 {
					results[i] = strings.Join(ch.Output, "\n")
				}
			} else {
				oks[i] = "0"
			}
		})
		if k := x(op.Args["out"]); k != "" {
			f.Lists[k] = results
		}
		if k := x(op.Args["ok_list"]); k != "" {
			f.Lists[k] = oks
		}
	case "emit":
		f.Output = append(f.Output, x(op.A))
	case "url_escape":
		f.Vars[op.B] = url.QueryEscape(f.Vars[op.A])
	case "str_after":
		src := f.Vars[op.A]
		sep := x(op.B)
		i := strings.Index(src, sep)
		if i < 0 {
			f.Vars[op.C] = ""
		} else {
			f.Vars[op.C] = src[i+len(sep):]
		}
	case "str_before":
		src := f.Vars[op.A]
		sep := x(op.B)
		i := strings.Index(src, sep)
		if i < 0 {
			f.Vars[op.C] = src
		} else {
			f.Vars[op.C] = src[:i]
		}
	case "str_contains":
		if strings.Contains(x(op.B), x(op.C)) {
			f.Vars[op.A] = "1"
		} else {
			f.Vars[op.A] = "0"
		}
	case "utf8_bytes":
		src := []byte(f.Vars[op.A])
		out := make([]string, len(src))
		for i, b := range src {
			out[i] = fmt.Sprintf("%02x", b)
		}
		f.Lists[op.B] = out
		f.Vars[op.B+"_count"] = strconv.Itoa(len(out))
	case "unicode_runes":
		out := make([]string, 0, len(f.Vars[op.A]))
		for _, r := range f.Vars[op.A] {
			out = append(out, fmt.Sprintf("%x", r))
		}
		f.Lists[op.B] = out
		f.Vars[op.B+"_count"] = strconv.Itoa(len(out))
	case "unicode_windows":
		runes := []rune(f.Vars[op.A])
		minN, maxN, maxOut := 1, 6, 256
		if q, er := strconv.Atoi(x(op.Args["min"])); er == nil && q > 0 {
			minN = q
		}
		if q, er := strconv.Atoi(x(op.Args["max"])); er == nil && q > 0 {
			maxN = q
		}
		if q, er := strconv.Atoi(x(op.Args["limit"])); er == nil && q > 0 {
			maxOut = q
		}
		if maxN < minN {
			maxN = minN
		}
		if maxN > len(runes) {
			maxN = len(runes)
		}
		seen := map[string]bool{}
		out := make([]string, 0, maxOut)
		for n := maxN; n >= minN && len(out) < maxOut; n-- {
			for i := 0; i+n <= len(runes) && len(out) < maxOut; i++ {
				w := string(runes[i : i+n])
				if strings.TrimSpace(w) == "" || seen[w] {
					continue
				}
				seen[w] = true
				out = append(out, w)
			}
		}
		f.Lists[op.B] = out
		f.Vars[op.B+"_count"] = strconv.Itoa(len(out))
	case "list_append":
		f.Lists[op.A] = append(f.Lists[op.A], x(op.B))
		f.Vars[op.A+"_count"] = strconv.Itoa(len(f.Lists[op.A]))
	case "list_contains":
		want := x(op.B)
		found := "0"
		for _, v := range f.Lists[op.A] {
			if v == want {
				found = "1"
				break
			}
		}
		if k := op.Args["out"]; k != "" {
			f.Vars[k] = found
		} else {
			f.Vars[op.C] = found
		}
	case "list_unique_append":
		val := x(op.B)
		seen := false
		for _, v := range f.Lists[op.A] {
			if v == val {
				seen = true
				break
			}
		}
		if !seen {
			f.Lists[op.A] = append(f.Lists[op.A], val)
		}
		f.Vars[op.A+"_count"] = strconv.Itoa(len(f.Lists[op.A]))
		if k := op.Args["added_out"]; k != "" {
			if seen {
				f.Vars[k] = "0"
			} else {
				f.Vars[k] = "1"
			}
		}
	case "sha256_text":
		// DSL convention: a bare existing Frame variable names its current value;
		// otherwise A is treated as a template/literal expression. This keeps
		// content-addressing dependent on cognitive data rather than variable names.
		src := x(op.A)
		if !strings.Contains(op.A, "{{") {
			if v, ok := f.Vars[op.A]; ok {
				src = v
			}
		}
		h := sha256.Sum256([]byte(src))
		f.Vars[op.B] = fmt.Sprintf("%x", h[:])
	case "time_unix":
		f.Vars[op.A] = strconv.FormatInt(time.Now().Unix(), 10)
	case "time_unix_nano":
		f.Vars[op.A] = strconv.FormatInt(time.Now().UnixNano(), 10)
	case "runtime_cpu_count":
		f.Vars[op.A] = strconv.Itoa(runtime.GOMAXPROCS(0))
	case "physical_runtime_stats":
		b, _ := json.Marshal(physicalRuntimeInfo())
		f.Vars[op.A] = string(b)
	case "physical_execution_concurrency_set":
		n := int(num(x(op.A)))
		if n < 1 {
			n = 1
		}
		maxPhysical := runtime.GOMAXPROCS(0)
		if maxPhysical < 1 {
			maxPhysical = 1
		}
		if n > maxPhysical {
			n = maxPhysical
		}
		setPhysicalExecutionConcurrency(n)
	case "str_join":
		f.Vars[op.C] = x(op.A) + x(op.Args["sep"]) + x(op.B)
	case "str_len":
		f.Vars[op.B] = strconv.Itoa(len([]rune(x(op.A))))
	case "emit_event":
		vars := map[string]string{}
		for _, k := range splitCSV(x(op.Args["vars"])) {
			if k != "" {
				vars[k] = f.Vars[k]
			}
		}
		ev := newPhysicalEvent(x(op.A), x(op.B), vars)
		if err := e.enqueueEvent(f, ev); err != nil {
			return -1, err
		}
	case "credential_get":
		name := strings.TrimSpace(x(op.A))
		if !strings.HasPrefix(name, "MEMORYAI_CREDENTIAL_") {
			return -1, fmt.Errorf("credential env name denied: %s", name)
		}
		f.Vars[op.B] = os.Getenv(name)
	case "url_parse":
		u, err := url.Parse(x(op.A))
		if err != nil {
			return -1, err
		}
		if k := op.Args["scheme_out"]; k != "" {
			f.Vars[k] = u.Scheme
		}
		if k := op.Args["host_out"]; k != "" {
			f.Vars[k] = u.Hostname()
		}
		if k := op.Args["port_out"]; k != "" {
			f.Vars[k] = u.Port()
		}
		if k := op.Args["path_out"]; k != "" {
			f.Vars[k] = u.EscapedPath()
			if f.Vars[k] == "" {
				f.Vars[k] = u.Path
			}
		}
	case "regex_first":
		re, err := regexp.Compile(x(op.B))
		if err != nil {
			return -1, err
		}
		mm := re.FindStringSubmatch(f.Vars[op.A])
		if len(mm) > 1 {
			f.Vars[op.C] = mm[1]
		} else {
			f.Vars[op.C] = ""
		}
	case "json_keys":
		var obj map[string]any
		if err := json.Unmarshal([]byte(x(op.A)), &obj); err != nil {
			return -1, fmt.Errorf("json_keys: %w", err)
		}
		keys := make([]string, 0, len(obj))
		for k := range obj {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		f.Lists[op.B] = keys
	case "json_get":
		var obj map[string]any
		if err := json.Unmarshal([]byte(x(op.A)), &obj); err != nil {
			return -1, fmt.Errorf("json_get: %w", err)
		}
		v, ok := obj[x(op.B)]
		if !ok || v == nil {
			f.Vars[op.C] = ""
			break
		}
		switch q := v.(type) {
		case string:
			f.Vars[op.C] = q
		case float64:
			f.Vars[op.C] = strconv.FormatFloat(q, 'f', -1, 64)
		case bool:
			f.Vars[op.C] = strconv.FormatBool(q)
		default:
			b, _ := json.Marshal(q)
			f.Vars[op.C] = string(b)
		}
	case "json_array_strings":
		raw := x(op.A)
		key := x(op.B)
		var arr []any
		if key != "" {
			var obj map[string]any
			if er := json.Unmarshal([]byte(raw), &obj); er != nil {
				return -1, fmt.Errorf("json_array_strings: %w", er)
			}
			v, ok := obj[key]
			if !ok {
				f.Lists[op.C] = nil
				break
			}
			q, ok := v.([]any)
			if !ok {
				return -1, fmt.Errorf("json_array_strings: key %s is not array", key)
			}
			arr = q
		} else {
			if er := json.Unmarshal([]byte(raw), &arr); er != nil {
				return -1, fmt.Errorf("json_array_strings: %w", er)
			}
		}
		out := []string{}
		for _, v := range arr {
			switch q := v.(type) {
			case string:
				out = append(out, q)
			default:
				b, _ := json.Marshal(q)
				out = append(out, string(b))
			}
		}
		f.Lists[op.C] = out
		f.Vars[op.C+"_count"] = strconv.Itoa(len(out))
	case "regex_all":
		re, err := regexp.Compile(x(op.B))
		if err != nil {
			return -1, err
		}
		mm := re.FindAllStringSubmatch(f.Vars[op.A], -1)
		vals := []string{}
		for _, m := range mm {
			if len(m) > 1 {
				vals = append(vals, m[1])
			} else if len(m) > 0 {
				vals = append(vals, m[0])
			}
		}
		f.Lists[op.C] = vals
		f.Vars[op.C+"_count"] = strconv.Itoa(len(vals))
		if len(vals) > 0 {
			f.Vars[op.C+"_first"] = vals[0]
		}
	case "program_export":
		target, err := e.resolveExecutable(x(op.A))
		if err != nil {
			return -1, err
		}
		b, _ := json.Marshal(target.Program)
		f.Vars[op.B] = string(b)
	case "program_import":
		target, err := e.resolveExecutable(x(op.A))
		if err != nil {
			return -1, err
		}
		var pp []Op
		if err = json.Unmarshal([]byte(f.Vars[op.B]), &pp); err != nil {
			return -1, err
		}
		e.dataMu.Lock()
		target.Program = pp
		target.CapabilitySig = ""
		target.Revision++
		e.dirty = true
		e.dirtyIDs[target.ID] = true
		e.dataMu.Unlock()
		f.memoryWrites++
	case "program_op_get":
		target, err := e.resolveExecutable(x(op.A))
		if err != nil {
			return -1, err
		}
		idx, _ := strconv.Atoi(x(op.B))
		if idx < 0 || idx >= len(target.Program) {
			return -1, fmt.Errorf("program index %d out of range", idx)
		}
		q := target.Program[idx]
		if k := op.Args["code_out"]; k != "" {
			f.Vars[k] = q.Code
		}
		if k := op.Args["a_out"]; k != "" {
			f.Vars[k] = q.A
		}
		if k := op.Args["b_out"]; k != "" {
			f.Vars[k] = q.B
		}
		if k := op.Args["c_out"]; k != "" {
			f.Vars[k] = q.C
		}
		if k := op.Args["args_out"]; k != "" {
			b, _ := json.Marshal(q.Args)
			f.Vars[k] = string(b)
		}
	case "program_set_field":
		target, err := e.resolveExecutable(x(op.A))
		if err != nil {
			return -1, err
		}
		idx, _ := strconv.Atoi(x(op.B))
		if idx < 0 || idx >= len(target.Program) {
			return -1, fmt.Errorf("program index %d out of range", idx)
		}
		v := x(op.C)
		switch op.Args["field"] {
		case "code":
			target.Program[idx].Code = v
		case "a":
			target.Program[idx].A = v
		case "b":
			target.Program[idx].B = v
		case "c":
			target.Program[idx].C = v
		default:
			return -1, fmt.Errorf("unknown program field %q", op.Args["field"])
		}
		target.CapabilitySig = ""
		target.Revision++
		e.markDirty(target.ID)
		f.memoryWrites++
	case "program_set_var_ref":
		target, err := e.resolveExecutable(x(op.A))
		if err != nil {
			return -1, err
		}
		idx, _ := strconv.Atoi(x(op.B))
		if idx < 0 || idx >= len(target.Program) {
			return -1, fmt.Errorf("program index %d out of range", idx)
		}
		v := "{{" + x(op.C) + "}}"
		switch op.Args["field"] {
		case "a":
			target.Program[idx].A = v
		case "b":
			target.Program[idx].B = v
		case "c":
			target.Program[idx].C = v
		default:
			return -1, fmt.Errorf("unknown program field %q", op.Args["field"])
		}
		target.CapabilitySig = ""
		target.Revision++
		e.markDirty(target.ID)
		f.memoryWrites++
	case "program_len":
		target, err := e.resolveExecutable(x(op.A))
		if err != nil {
			return -1, err
		}
		f.Vars[op.B] = strconv.Itoa(len(target.Program))
	case "program_delete":
		target, err := e.resolveExecutable(x(op.A))
		if err != nil {
			return -1, err
		}
		i, _ := strconv.Atoi(x(op.B))
		if i < 0 || i >= len(target.Program) {
			return -1, fmt.Errorf("program index %d out of range", i)
		}
		target.Program = append(target.Program[:i], target.Program[i+1:]...)
		target.CapabilitySig = ""
		target.Revision++
		e.markDirty(target.ID)
		f.memoryWrites++
	case "program_insert_from":
		target, err := e.resolveExecutable(x(op.A))
		if err != nil {
			return -1, err
		}
		source, err := e.resolveExecutable(x(op.C))
		if err != nil {
			return -1, err
		}
		i, _ := strconv.Atoi(x(op.B))
		start, _ := strconv.Atoi(x(op.Args["start"]))
		count, _ := strconv.Atoi(x(op.Args["count"]))
		if i < 0 || i > len(target.Program) || start < 0 || start > len(source.Program) {
			return -1, fmt.Errorf("program splice index out of range")
		}
		if count <= 0 || start+count > len(source.Program) {
			count = len(source.Program) - start
		}
		frag := append([]Op(nil), source.Program[start:start+count]...)
		target.Program = append(target.Program, make([]Op, len(frag))...)
		copy(target.Program[i+len(frag):], target.Program[i:len(target.Program)-len(frag)])
		copy(target.Program[i:i+len(frag)], frag)
		target.CapabilitySig = ""
		target.Revision++
		e.markDirty(target.ID)
		f.memoryWrites++
	case "program_replace_from":
		target, err := e.resolveExecutable(x(op.A))
		if err != nil {
			return -1, err
		}
		source, err := e.resolveExecutable(x(op.C))
		if err != nil {
			return -1, err
		}
		i, _ := strconv.Atoi(x(op.B))
		deleteCount, _ := strconv.Atoi(x(op.Args["delete_count"]))
		start, _ := strconv.Atoi(x(op.Args["start"]))
		count, _ := strconv.Atoi(x(op.Args["count"]))
		if deleteCount < 0 {
			deleteCount = 0
		}
		if i < 0 || i > len(target.Program) || i+deleteCount > len(target.Program) || start < 0 || start > len(source.Program) {
			return -1, fmt.Errorf("program replace index out of range")
		}
		if count <= 0 || start+count > len(source.Program) {
			count = len(source.Program) - start
		}
		frag := append([]Op(nil), source.Program[start:start+count]...)
		newp := make([]Op, 0, len(target.Program)-deleteCount+len(frag))
		newp = append(newp, target.Program[:i]...)
		newp = append(newp, frag...)
		newp = append(newp, target.Program[i+deleteCount:]...)
		target.Program = newp
		target.CapabilitySig = ""
		target.Revision++
		e.markDirty(target.ID)
		f.memoryWrites++
	case "memory_new":
		layer := x(op.Args["layer"])
		parents := splitCSV(x(op.Args["parents"]))
		tags := splitCSV(x(op.Args["tags"]))
		content := x(op.Args["content"])
		gen := 0
		for _, pid := range parents {
			pm, er := e.resolveID(pid)
			if er != nil {
				return -1, fmt.Errorf("memory_new parent %q unresolved: %w", pid, er)
			}
			if pm.Generation >= gen {
				gen = pm.Generation + 1
			}
		}
		child := &Memory{ID: nextID("mem"), Layer: layer, Generation: gen, Parents: parents, Tags: tags, Content: content, State: map[string]any{}, CreatedUnix: time.Now().Unix(), Revision: 1}
		if caps := splitCSV(x(op.Args["capabilities"])); len(caps) > 0 {
			child.Capabilities = caps
		}
		// Memory decides that the structure is born; Kernel only selects a bounded
		// physical shard for its bytes.
		if err := e.placeRuntimeMemory(child); err != nil {
			return -1, err
		}
		f.memoryWrites++
		f.Vars[op.A] = child.ID
	case "memory_copy":
		parent, err := e.resolve(x(op.B))
		if err != nil {
			return -1, err
		}
		child := copyMemory(parent)
		child.ID = nextID("mem")
		child.Layer = x(op.Args["layer"])
		if child.Layer == "" {
			child.Layer = "acquired"
		}
		child.Generation = parent.Generation + 1
		child.Parents = []string{parent.ID}
		child.CapabilitySig = ""
		child.SuccessHistory = nil
		child.FailureHistory = nil
		child.MutationVariants = nil
		child.RuntimeExecCount = 0
		child.CreatedUnix = time.Now().Unix()
		child.Revision = parent.Revision + 1
		for _, t := range splitCSV(x(op.Args["tags"])) {
			if t != "" {
				child.Tags = append(child.Tags, t)
			}
		}
		// Memory decides that the structure is born; Kernel only selects a bounded
		// physical shard for its bytes.
		if err := e.placeRuntimeMemory(child); err != nil {
			return -1, err
		}
		f.memoryWrites++
		f.Vars[op.A] = child.ID
	case "memory_history_append":
		target, err := e.resolveMutable(x(op.A))
		if err != nil {
			return -1, err
		}
		owner := e.ownerOf(target.ID)
		if owner == nil {
			return -1, fmt.Errorf("mutable owner unavailable: %s", target.ID)
		}
		value := strings.TrimSpace(x(op.B))
		field := strings.TrimSpace(x(op.Args["field"]))
		if value == "" {
			break
		}
		owner.dataMu.Lock()
		var dst *[]string
		switch field {
		case "success_history":
			dst = &target.SuccessHistory
		case "failure_history":
			dst = &target.FailureHistory
		case "mutation_variants":
			dst = &target.MutationVariants
		default:
			owner.dataMu.Unlock()
			return -1, fmt.Errorf("unknown Memory history field %q", field)
		}
		if !contains(*dst, value) {
			*dst = append(*dst, value)
			target.CapabilitySig = ""
			target.Revision++
			owner.dirty = true
			owner.dirtyIDs[target.ID] = true
			f.memoryWrites++
		}
		owner.dataMu.Unlock()
	case "memory_tag_add":
		target, err := e.resolveMutable(x(op.A))
		if err != nil {
			return -1, err
		}
		owner := e.ownerOf(target.ID)
		if owner == nil {
			return -1, fmt.Errorf("mutable owner unavailable: %s", target.ID)
		}
		t := x(op.B)
		if t != "" {
			owner.dataMu.Lock()
			if !contains(target.Tags, t) {
				target.Tags = append(target.Tags, t)
				target.CapabilitySig = ""
				target.Revision++
				owner.tagDeltaAddLocked(target.ID, t)
				owner.dirty = true
				owner.dirtyIDs[target.ID] = true
				f.memoryWrites++
			}
			owner.dataMu.Unlock()
		}
	case "memory_tag_remove":
		target, err := e.resolveMutable(x(op.A))
		if err != nil {
			return -1, err
		}
		owner := e.ownerOf(target.ID)
		if owner == nil {
			return -1, fmt.Errorf("mutable owner unavailable: %s", target.ID)
		}
		t := x(op.B)
		owner.dataMu.Lock()
		had := contains(target.Tags, t)
		nt := target.Tags[:0]
		for _, q := range target.Tags {
			if q != t {
				nt = append(nt, q)
			}
		}
		target.Tags = nt
		if had {
			target.CapabilitySig = ""
			target.Revision++
			owner.tagDeltaRemoveLocked(target.ID, t)
			owner.dirty = true
			owner.dirtyIDs[target.ID] = true
			f.memoryWrites++
		}
		owner.dataMu.Unlock()
	case "memory_delete":
		id := x(op.A)
		ok := "0"
		if owner := e.ownerOf(id); owner != nil {
			if m, er := owner.resolveIDLocal(id); er == nil {
				owner.dataMu.Lock()
				for _, t := range m.Tags {
					owner.tagDeltaRemoveLocked(id, t)
				}
				owner.deletedIDs[id] = true
				delete(owner.cache, id)
				delete(owner.newIDs, id)
				delete(owner.dirtyIDs, id)
				owner.dirty = true
				owner.dataMu.Unlock()
				ok = "1"
				f.memoryWrites++
			}
		}
		if k := op.Args["ok_out"]; k != "" {
			f.Vars[k] = ok
		}
	case "memory_exists":
		if _, err := e.resolve(x(op.A)); err == nil {
			f.Vars[op.B] = "1"
		} else {
			f.Vars[op.B] = "0"
		}
	case "memory_digest":
		target, err := e.resolve(x(op.A))
		if err != nil {
			return -1, err
		}
		b, _ := json.Marshal(target)
		f.Vars[op.B] = fmt.Sprintf("%x", sha256.Sum256(b))
	case "memory_export_json":
		raw, er := e.exportMemoryJSON(x(op.A), truth(x(op.Args["sign"])))
		if er != nil {
			return -1, er
		}
		f.Vars[op.B] = raw
	case "memory_import_json":
		id, status, er := e.importMemoryJSON(x(op.A), truth(x(op.Args["remote"])))
		if er != nil {
			return -1, er
		}
		if status == "imported" {
			f.memoryWrites++
		}
		if op.B != "" {
			f.Vars[op.B] = id
		}
		if op.C != "" {
			f.Vars[op.C] = status
		}

	case "process_restart":
		// Generic physical primitive only. Memory decides when to restart and
		// which continuation cognition should receive control afterwards.
		if err := e.persistAll(); err != nil {
			return -1, err
		}
		target := x(op.A)
		if target == "" {
			target = e.genesis.Root
		}
		exe, err := os.Executable()
		if err != nil {
			return -1, err
		}
		argv := []string{exe, e.bodyPath, "run", target}
		for _, k := range splitCSV(x(op.Args["keep"])) {
			if v, ok := f.Vars[k]; ok {
				argv = append(argv, k+"="+v)
			}
		}
		if err := syscall.Exec(exe, argv, os.Environ()); err != nil {
			return -1, err
		}
		return -1, nil
	case "space_info":
		info, err := e.spaceInfo()
		if err != nil {
			return -1, err
		}
		b, _ := json.Marshal(info)
		f.Vars[op.A] = string(b)
	case "space_list":
		f.Lists[op.A] = e.mountedSpacePaths()
	case "space_next_path":
		f.Vars[op.A] = e.nextSpacePath()
	case "space_create":
		path := x(op.A)
		if path == "" {
			path = e.nextSpacePath()
		}
		cp, err := e.createAndMountSpace(path)
		if err != nil {
			return -1, err
		}
		if op.B != "" {
			f.Vars[op.B] = cp
		}
	case "space_mount":
		cp, err := e.mountSpace(x(op.A))
		if err != nil {
			return -1, err
		}
		if op.B != "" {
			f.Vars[op.B] = cp
		}
	case "space_unmount":
		ok := e.unmountSpace(x(op.A))
		if op.B != "" {
			if ok {
				f.Vars[op.B] = "1"
			} else {
				f.Vars[op.B] = "0"
			}
		}
	case "space_select_write":
		if err := e.selectWriteSpace(x(op.A)); err != nil {
			return -1, err
		}
		if op.B != "" {
			f.Vars[op.B] = "1"
		}
	case "space_write_target":
		if e.writeSpace == "" {
			f.Vars[op.A] = "primary"
		} else {
			f.Vars[op.A] = e.writeSpace
		}
	case "space_copy":
		newid, err := e.transferMemory(x(op.A), x(op.B), false)
		if err != nil {
			return -1, err
		}
		if op.C != "" {
			f.Vars[op.C] = newid
		}
	case "space_move":
		newid, err := e.transferMemory(x(op.A), x(op.B), true)
		if err != nil {
			return -1, err
		}
		if op.C != "" {
			f.Vars[op.C] = newid
		}
	case "space_merge":
		stat, err := e.mergeSpace(x(op.A), x(op.B))
		if err != nil {
			return -1, err
		}
		b, _ := json.Marshal(stat)
		if op.C != "" {
			f.Vars[op.C] = string(b)
		}
	case "space_all_ids":
		ids, err := e.allMountedIDs()
		if err != nil {
			return -1, err
		}
		f.Lists[op.A] = ids
	case "remote_space_info":
		resp, err := remoteSpaceRequest(x(op.Args["host"]), x(op.Args["port"]), map[string]any{"op": "stat"}, parseTimeout(x(op.Args["timeout_ms"])))
		if err != nil {
			return -1, err
		}
		f.Vars[op.Args["out"]] = resp
	case "remote_space_create":
		resp, err := remoteSpaceRequest(x(op.Args["host"]), x(op.Args["port"]), map[string]any{"op": "create", "name": x(op.Args["name"])}, parseTimeout(x(op.Args["timeout_ms"])))
		if err != nil {
			return -1, err
		}
		if err = requireRemoteMutationACK("space_create", resp); err != nil {
			return -1, err
		}
		f.Vars[op.Args["out"]] = resp
	case "remote_space_put":
		m, err := e.resolve(x(op.A))
		if err != nil {
			return -1, err
		}
		resp, err := remoteSpaceRequest(x(op.Args["host"]), x(op.Args["port"]), map[string]any{"op": "put", "name": x(op.Args["name"]), "memory": m}, parseTimeout(x(op.Args["timeout_ms"])))
		if err != nil {
			return -1, err
		}
		var putResult struct {
			OK     bool   `json:"ok"`
			Status string `json:"status,omitempty"`
		}
		if err = json.Unmarshal([]byte(resp), &putResult); err != nil {
			return -1, err
		}
		if op.Args["out"] != "" {
			f.Vars[op.Args["out"]] = resp
		}
		if k := op.Args["status_out"]; k != "" {
			f.Vars[k] = putResult.Status
		}
		if !putResult.OK && putResult.Status == "conflict" {
			break
		}
		if err = requireRemoteMutationACK("space_put", resp); err != nil {
			return -1, err
		}
	case "remote_space_get":
		resp, err := remoteSpaceRequest(x(op.Args["host"]), x(op.Args["port"]), map[string]any{"op": "get", "name": x(op.Args["name"]), "id": x(op.Args["id"])}, parseTimeout(x(op.Args["timeout_ms"])))
		if err != nil {
			return -1, err
		}
		var rr struct {
			OK        bool    `json:"ok"`
			Memory    *Memory `json:"memory"`
			MemoryABI string  `json:"memory_abi"`
		}
		if err = json.Unmarshal([]byte(resp), &rr); err != nil {
			return -1, err
		}
		if !rr.OK || rr.Memory == nil {
			return -1, errors.New("remote memory missing")
		}
		if rr.MemoryABI != "" && rr.MemoryABI != memoryABI {
			return -1, fmt.Errorf("remote Memory ABI %q incompatible with %q", rr.MemoryABI, memoryABI)
		}
		// Direct remote read only. The record is not imported, cached or persisted
		// into Memory.mem. Its identity remains remote while its data is visible to
		// the current cognition frame through normal resolve/read primitives.
		if op.Args["out"] != "" {
			f.Vars[op.Args["out"]] = rr.Memory.ID
		}
		if k := op.Args["json_out"]; k != "" {
			b, _ := json.Marshal(rr.Memory)
			f.Vars[k] = string(b)
		}
	case "remote_space_digest":
		resp, err := remoteSpaceRequest(x(op.Args["host"]), x(op.Args["port"]), map[string]any{"op": "digest", "name": x(op.Args["name"]), "id": x(op.Args["id"])}, parseTimeout(x(op.Args["timeout_ms"])))
		if err != nil {
			return -1, err
		}
		var rr struct {
			OK     bool   `json:"ok"`
			Digest string `json:"digest"`
		}
		if err = json.Unmarshal([]byte(resp), &rr); err != nil {
			return -1, err
		}
		if !rr.OK {
			return -1, errors.New("remote memory digest unavailable")
		}
		f.Vars[op.Args["out"]] = rr.Digest
	case "remote_space_import":
		return -1, errors.New("remote_space_import disabled: remote Memory is direct-read only; disconnected means forgotten, reconnected means remembered")
	case "remote_space_list":
		resp, err := remoteSpaceRequest(x(op.Args["host"]), x(op.Args["port"]), map[string]any{"op": "list", "name": x(op.Args["name"])}, parseTimeout(x(op.Args["timeout_ms"])))
		if err != nil {
			return -1, err
		}
		var rr struct {
			OK  bool     `json:"ok"`
			IDs []string `json:"ids"`
		}
		if err = json.Unmarshal([]byte(resp), &rr); err != nil {
			return -1, err
		}
		f.Lists[op.Args["out_list"]] = rr.IDs
	case "remote_space_upsert":
		m, err := e.resolve(x(op.A))
		if err != nil {
			return -1, err
		}
		host := x(op.Args["host"])
		port := x(op.Args["port"])
		name := x(op.Args["name"])
		timeout := parseTimeout(x(op.Args["timeout_ms"]))
		expectedDigest, exists, err := remoteSpaceObservedDigest(host, port, name, m.ID, timeout)
		if err != nil {
			return -1, err
		}
		req := map[string]any{"op": "put", "name": name, "memory": m, "replace": true}
		if exists {
			req["expected_digest"] = expectedDigest
		}
		resp, err := remoteSpaceRequest(host, port, req, timeout)
		if err != nil {
			return -1, err
		}
		var upsertResult struct {
			OK     bool   `json:"ok"`
			Status string `json:"status,omitempty"`
		}
		if err = json.Unmarshal([]byte(resp), &upsertResult); err != nil {
			return -1, err
		}
		if op.Args["out"] != "" {
			f.Vars[op.Args["out"]] = resp
		}
		if k := op.Args["status_out"]; k != "" {
			f.Vars[k] = upsertResult.Status
		}
		if !upsertResult.OK && upsertResult.Status == "conflict" {
			break
		}
		if err = requireRemoteMutationACK("space_upsert", resp); err != nil {
			return -1, err
		}
	case "remote_space_delete":
		host := x(op.Args["host"])
		port := x(op.Args["port"])
		name := x(op.Args["name"])
		id := x(op.Args["id"])
		timeout := parseTimeout(x(op.Args["timeout_ms"]))
		expectedDigest, exists, err := remoteSpaceObservedDigest(host, port, name, id, timeout)
		if err != nil {
			return -1, err
		}
		if !exists {
			return -1, errors.New("remote memory missing")
		}
		resp, err := remoteSpaceRequest(host, port, map[string]any{
			"op": "delete", "name": name, "id": id, "expected_digest": expectedDigest,
		}, timeout)
		if err != nil {
			return -1, err
		}
		var deleteResult struct {
			OK     bool   `json:"ok"`
			Status string `json:"status,omitempty"`
		}
		if err = json.Unmarshal([]byte(resp), &deleteResult); err != nil {
			return -1, err
		}
		if op.Args["out"] != "" {
			f.Vars[op.Args["out"]] = resp
		}
		if k := op.Args["status_out"]; k != "" {
			f.Vars[k] = deleteResult.Status
		}
		if !deleteResult.OK && deleteResult.Status == "conflict" {
			break
		}
		if err = requireRemoteMutationACK("space_delete", resp); err != nil {
			return -1, err
		}
	case "remote_space_tag_list":
		resp, err := remoteSpaceRequest(x(op.Args["host"]), x(op.Args["port"]), map[string]any{"op": "tag", "name": x(op.Args["name"]), "id": x(op.Args["tag"])}, parseTimeout(x(op.Args["timeout_ms"])))
		if err != nil {
			return -1, err
		}
		var rr struct {
			OK  bool     `json:"ok"`
			IDs []string `json:"ids"`
		}
		if err = json.Unmarshal([]byte(resp), &rr); err != nil {
			return -1, err
		}
		f.Lists[op.Args["out_list"]] = rr.IDs
	case "space_fsck":
		stat, err := e.fsckMounted()
		if err != nil {
			return -1, err
		}
		b, _ := json.Marshal(stat)
		f.Vars[op.A] = string(b)
	case "artifact_write":
		p, err := e.artifactPath(x(op.A))
		if err != nil {
			return -1, err
		}
		if err = os.MkdirAll(filepath.Dir(p), 0755); err != nil {
			return -1, err
		}
		raw := x(op.B)
		maxBytes := artifactMaxBytes()
		if int64(len(raw)) > maxBytes {
			return -1, fmt.Errorf("artifact write exceeds physical byte ceiling: size=%d max=%d", len(raw), maxBytes)
		}
		data := []byte(raw)
		tmp := p + ".tmp-" + nextID("artifact")
		if err = os.WriteFile(tmp, data, 0644); err != nil {
			return -1, err
		}
		if err = os.Rename(tmp, p); err != nil {
			_ = os.Remove(tmp)
			return -1, err
		}
		if op.C != "" {
			h := sha256.Sum256(data)
			f.Vars[op.C] = fmt.Sprintf("%x", h[:])
		}
	case "artifact_read":
		p, err := e.artifactPath(x(op.A))
		if err != nil {
			return -1, err
		}
		maxBytes := artifactMaxBytes()
		if err := ensureArtifactSizeWithinLimit(p, maxBytes); err != nil {
			return -1, err
		}
		file, err := os.Open(p)
		if err != nil {
			return -1, err
		}
		b, readErr := readAllPhysicalBounded(file, maxBytes, "artifact read")
		_ = file.Close()
		if readErr != nil {
			return -1, readErr
		}
		f.Vars[op.B] = string(b)
	case "artifact_digest":
		p, err := e.artifactPath(x(op.A))
		if err != nil {
			return -1, err
		}
		maxBytes := artifactMaxBytes()
		if err := ensureArtifactSizeWithinLimit(p, maxBytes); err != nil {
			return -1, err
		}
		file, err := os.Open(p)
		if err != nil {
			return -1, err
		}
		h := sha256.New()
		written, copyErr := io.Copy(h, io.LimitReader(file, maxBytes+1))
		_ = file.Close()
		if copyErr != nil {
			return -1, copyErr
		}
		if written > maxBytes {
			return -1, fmt.Errorf("artifact digest exceeds physical byte ceiling: max=%d", maxBytes)
		}
		f.Vars[op.B] = fmt.Sprintf("%x", h.Sum(nil))
	case "artifact_exists":
		p, err := e.artifactPath(x(op.A))
		if err != nil {
			return -1, err
		}
		if _, err = os.Stat(p); err == nil {
			f.Vars[op.B] = "1"
		} else if errors.Is(err, os.ErrNotExist) {
			f.Vars[op.B] = "0"
		} else {
			return -1, err
		}
	case "mesh_role":
		role := "standalone"
		if m := meshRuntimeCurrent(); m != nil {
			role = m.role
		}
		f.Vars[op.A] = role
	case "mesh_shared_propose":
		m := meshRuntimeCurrent()
		if m == nil {
			if op.B != "" {
				f.Vars[op.B] = "standalone"
			}
			if op.C != "" {
				f.Vars[op.C] = "local-only"
			}
			break
		}
		res, err := m.proposeSharedDurable(x(op.A))
		if err != nil {
			return -1, err
		}
		if op.B != "" {
			f.Vars[op.B] = res.Status
		}
		if op.C != "" {
			f.Vars[op.C] = res.Decision
		}
		if k := op.Args["reason_out"]; k != "" {
			f.Vars[k] = res.Reason
		}
	case "mesh_shared_search":
		m := meshRuntimeCurrent()
		if m == nil {
			f.Lists[op.B] = nil
			break
		}
		res, err := m.authorityRPC(MeshRequest{Op: "shared_search", Query: x(op.A), Tags: splitCSV(x(op.Args["tags"]))})
		if err != nil {
			return -1, err
		}
		out := make([]string, 0, len(res.Records))
		for _, r := range res.Records {
			b, _ := json.Marshal(r)
			out = append(out, string(b))
		}
		f.Lists[op.B] = out
	case "mesh_shared_fetch":
		m := meshRuntimeCurrent()
		if m == nil {
			return -1, errors.New("mesh shared fetch requires sovereign/node mode")
		}
		res, err := m.sharedFetch(x(op.A))
		if err != nil {
			if !truth(x(op.Args["soft_fail"])) {
				return -1, err
			}
			if op.B != "" {
				f.Vars[op.B] = ""
			}
			if op.C != "" {
				f.Vars[op.C] = "forgotten"
			}
			if k := op.Args["reason_out"]; k != "" {
				f.Vars[k] = err.Error()
			}
			break
		}
		b, _ := json.Marshal(res.Memory)
		if op.B != "" {
			f.Vars[op.B] = string(b)
		}
		if op.C != "" {
			f.Vars[op.C] = res.Status
		}
		if k := op.Args["reason_out"]; k != "" {
			f.Vars[k] = res.Reason
		}
	case "mesh_shared_reconcile":
		m := meshRuntimeCurrent()
		if m == nil {
			return -1, errors.New("mesh runtime unavailable")
		}
		res, err := m.authorityRPC(MeshRequest{Op: "shared_reconcile", MemoryID: x(op.A), ProposalDigest: x(op.B)})
		if err != nil {
			return -1, err
		}
		if !res.OK {
			return -1, errors.New(res.Error)
		}
		if op.C != "" {
			f.Vars[op.C] = res.Status
		}
		if k := op.Args["reason_out"]; k != "" {
			f.Vars[k] = res.Reason
		}
	case "mesh_structure_run":
		m := meshRuntimeCurrent()
		if m == nil {
			return -1, errors.New("mesh structure execution requires sovereign/node mode")
		}
		vars := map[string]string{}
		for k, v := range f.Vars {
			if !strings.HasPrefix(k, "__") {
				vars[k] = v
			}
		}
		res, err := m.sharedExecute(x(op.A), vars)
		if err != nil {
			return -1, err
		}
		b, _ := json.Marshal(res.Frame)
		if op.B != "" {
			f.Vars[op.B] = string(b)
		}
		if op.C != "" {
			f.Vars[op.C] = res.Status
		}
	case "mesh_route_execution":
		m := meshRuntimeCurrent()
		if m == nil {
			return -1, errors.New("mesh route execution requires sovereign mode")
		}
		vars := map[string]string{}
		for k, v := range f.Vars {
			if !strings.HasPrefix(k, "__") || k == "__subject" {
				vars[k] = v
			}
		}
		node, rf, er := m.routeExecution(x(op.A), vars, truth(x(op.Args["prefer_local"])))
		if er != nil {
			return -1, er
		}
		if op.B != "" {
			f.Vars[op.B] = node
		}
		if op.C != "" && rf != nil {
			b, _ := json.Marshal(rf)
			f.Vars[op.C] = string(b)
		}
		if truth(x(op.Args["merge_frame"])) && rf != nil {
			for k, v := range rf.Vars {
				if !strings.HasPrefix(k, "__") {
					f.Vars[k] = v
				}
			}
			for k, v := range rf.Lists {
				if !strings.HasPrefix(k, "__") {
					f.Lists[k] = append([]string(nil), v...)
				}
			}
		}
	case "mesh_fanout":
		m := meshRuntimeCurrent()
		if m == nil {
			return -1, errors.New("mesh fanout requires sovereign mode")
		}
		vars := map[string]string{}
		for k, v := range f.Vars {
			if !strings.HasPrefix(k, "__") {
				vars[k] = v
			}
		}
		includeSelf := x(op.Args["include_self"]) != "0"
		out, err := m.fanout(x(op.A), vars, includeSelf)
		if err != nil {
			return -1, err
		}
		f.Lists[op.B] = out
	case "mesh_journal_flush":
		m := meshRuntimeCurrent()
		status := "standalone"
		if m != nil {
			if err := flushMeshDeferredJournal(m); err != nil {
				status = "deferred:" + err.Error()
			} else {
				status = "clean"
			}
		}
		if op.A != "" {
			f.Vars[op.A] = status
		}
	case "mesh_directory":
		m := meshRuntimeCurrent()
		if m == nil {
			f.Lists[op.A] = nil
			break
		}
		res, err := m.authorityRPC(MeshRequest{Op: "directory"})
		if err != nil {
			return -1, err
		}
		out := make([]string, 0, len(res.Nodes))
		for _, n := range res.Nodes {
			b, _ := json.Marshal(n)
			out = append(out, string(b))
		}
		f.Lists[op.A] = out
	case "physical_exchange":
		transport := x(op.Args["transport"])
		host := x(op.Args["host"])
		port := x(op.Args["port"])
		request := x(op.Args["request"])
		timeoutMS, _ := strconv.Atoi(x(op.Args["timeout_ms"]))
		if timeoutMS <= 0 {
			timeoutMS = 5000
		}
		resp, err := physicalExchange(transport, host, port, request, time.Duration(timeoutMS)*time.Millisecond)
		if err != nil {
			return -1, err
		}
		f.Vars[op.Args["out"]] = resp
	case "persist":
		out := x(op.A)
		if out == "" {
			if f.Vars["__daemon_defer_persist"] == "1" {
				out = e.bodyPath
			} else {
				if err := e.persistAll(); err != nil {
					return -1, err
				}
				out = e.bodyPath
			}
		} else {
			if err := e.saveBody(out); err != nil {
				return -1, err
			}
		}
		f.Vars[op.B] = out
	case "halt":
		return len(self.Program), nil
	default:
		return -1, fmt.Errorf("unknown primitive %q", op.Code)
	}
	return -1, nil
}

func fieldString(m *Memory, k string) string {
	switch k {
	case "id":
		return m.ID
	case "layer":
		return m.Layer
	case "generation":
		return strconv.Itoa(m.Generation)
	case "runtime_exec_count":
		return strconv.FormatUint(m.RuntimeExecCount, 10)
	case "content":
		return m.Content
	case "parents":
		return strings.Join(m.Parents, ",")
	case "tags":
		return strings.Join(m.Tags, ",")
	}
	return ""
}
func max0(x float64) float64 {
	if x < 0 {
		return 0
	}
	return x
}
func num(s string) float64 { v, _ := strconv.ParseFloat(strings.TrimSpace(s), 64); return v }
func ff(v float64) string  { return strconv.FormatFloat(v, 'f', -1, 64) }
func truth(s string) bool {
	return s == "1" || strings.EqualFold(s, "true") || strings.EqualFold(s, "yes")
}

func (e *Engine) artifactPath(rel string) (string, error) {
	root := strings.TrimSpace(os.Getenv("MEMORYAI_WORKSPACE"))
	if root == "" {
		root = filepath.Join(filepath.Dir(e.bodyPath), "..", "workspace")
	}
	return physicalSandboxPath(root, rel, "artifact workspace")
}

type SpaceInfo struct {
	Primary        string `json:"primary"`
	PrimaryBytes   int64  `json:"primary_bytes"`
	TotalBytes     uint64 `json:"physical_total_bytes"`
	FreeBytes      uint64 `json:"physical_free_bytes"`
	AvailableBytes uint64 `json:"physical_available_bytes"`
	Mounted        int    `json:"mounted_spaces"`
	WriteTarget    string `json:"write_target"`
}

func (e *Engine) markDirty(id string) {
	if o := e.ownerOf(id); o != nil {
		o.dataMu.Lock()
		o.dirty = true
		o.dirtyIDs[id] = true
		o.dataMu.Unlock()
	}
}
func (e *Engine) ownerOf(id string) *Engine {
	if e == nil {
		return nil
	}
	if e.speculative {
		if _, err := e.resolveIDLocal(id); err == nil {
			return e
		}
		return nil
	}
	root := fabricRootFor(e)
	if root == nil {
		root = e
	}
	owner, _, err := root.resolveLocalFabricMemory(id)
	if err != nil {
		return nil
	}
	return owner
}
func (e *Engine) writeEngine(selfID string) *Engine {
	e.spaceMu.RLock()
	target := e.writeSpace
	sp := e.spaces[target]
	e.spaceMu.RUnlock()
	if target != "" && sp != nil {
		return sp
	}
	return e
}

func (e *Engine) mountedSpacePaths() []string {
	e.spaceMu.RLock()
	defer e.spaceMu.RUnlock()
	xs := make([]string, 0, len(e.spaces))
	for p := range e.spaces {
		xs = append(xs, p)
	}
	sort.Strings(xs)
	return xs
}
func (e *Engine) localLocatorPath(path string) (string, error) {
	p := strings.TrimSpace(path)
	if strings.HasPrefix(p, "local://") {
		rel := strings.TrimPrefix(p, "local://")
		if rel == "" || filepath.IsAbs(rel) || strings.HasPrefix(filepath.Clean(rel), "..") {
			return "", fmt.Errorf("invalid local memory locator %q", path)
		}
		return filepath.Abs(filepath.Join(filepath.Dir(e.bodyPath), rel))
	}
	if filepath.IsAbs(p) {
		return filepath.Clean(p), nil
	}
	return filepath.Abs(filepath.Join(filepath.Dir(e.bodyPath), p))
}

func (e *Engine) mountSpace(path string) (string, error) {
	cp, err := e.localLocatorPath(path)
	if err != nil {
		return "", err
	}
	if cp == e.bodyPath {
		return cp, nil
	}
	e.spaceMu.RLock()
	_, ok := e.spaces[cp]
	e.spaceMu.RUnlock()
	if ok {
		return cp, nil
	}
	releaseMountBodyTxn := acquireRemoteBodyTransaction(cp)
	defer releaseMountBodyTxn()
	if live, mounted := activePhysicalBody(cp); mounted {
		return "", fmt.Errorf("physical Memory body already mounted: %s (%s)", cp, live.manifest.BodyID)
	}
	sp, err := loadEngineWithMutationJournal(cp)
	if err != nil {
		return "", err
	}
	if sp.manifest.Role != "storage" {
		sp.close()
		return "", fmt.Errorf("cannot mount executable core Memory as storage: %s", cp)
	}
	e.spaceMu.Lock()
	e.spaces[cp] = sp
	e.spaceMu.Unlock()
	rememberFabricOwner(e, sp)
	return cp, nil
}
func (e *Engine) unmountSpace(path string) bool {
	cp, err := e.localLocatorPath(path)
	if err != nil {
		return false
	}
	releaseUnmountBodyTxn := acquireRemoteBodyTransaction(cp)
	defer releaseUnmountBodyTxn()
	e.spaceMu.Lock()
	sp := e.spaces[cp]
	if sp == nil {
		e.spaceMu.Unlock()
		return false
	}
	delete(e.spaces, cp)
	if e.writeSpace == cp {
		e.writeSpace = ""
	}
	e.spaceMu.Unlock()
	forgetFabricOwner(sp)
	if err := persistEngineIfDirty(sp); err != nil {
		// Historical unmount returns bool only; fail closed and keep the Engine
		// open when durability cannot be established.
		rememberFabricOwner(e, sp)
		e.spaceMu.Lock()
		e.spaces[cp] = sp
		e.spaceMu.Unlock()
		return false
	}
	sp.close()
	return true
}
func (e *Engine) selectWriteSpace(path string) error {
	if path == "" || path == "primary" || path == e.bodyPath {
		e.writeSpace = ""
		return nil
	}
	cp, err := e.mountSpace(path)
	if err != nil {
		return err
	}
	e.spaceMu.Lock()
	e.writeSpace = cp
	e.spaceMu.Unlock()
	return nil
}
func (e *Engine) nextSpacePath() string {
	dir := filepath.Dir(e.bodyPath)
	for i := 1; ; i++ {
		name := fmt.Sprintf("Memory.%d.mem", i)
		p := filepath.Join(dir, name)
		if _, err := os.Stat(p); errors.Is(err, os.ErrNotExist) {
			return "local://" + name
		}
	}
}
func (e *Engine) spaceInfo() (SpaceInfo, error) {
	var st syscall.Statfs_t
	dir := filepath.Dir(e.bodyPath)
	if err := syscall.Statfs(dir, &st); err != nil {
		return SpaceInfo{}, err
	}
	fi, _ := os.Stat(e.bodyPath)
	var sz int64
	if fi != nil {
		sz = fi.Size()
	}
	ws := e.writeSpace
	if ws == "" {
		ws = "primary"
	}
	return SpaceInfo{e.bodyPath, sz, st.Blocks * uint64(st.Bsize), st.Bfree * uint64(st.Bsize), st.Bavail * uint64(st.Bsize), len(e.mountedSpacePaths()), ws}, nil
}
func createEmptyBody(path string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}
	records, ididx, tagidx, taglists, err := buildIndexedSections(nil)
	if err != nil {
		return err
	}
	g := Genesis{Format: "memoryai-genesis-index-v1", Root: "", MemoryCount: 0}
	gb, _ := json.MarshalIndent(g, "", "  ")
	gb = append(gb, '\n')
	sm := StoreManifest{Records: "store/records.bin", IDIndex: "store/id.idx", TagIndex: "store/tag.idx", TagLists: "store/taglists.bin", IndexFormat: indexFormatV2, MemoryCount: 0}
	parts := []zipEntry{{"genesis.json", gb, false}, {sm.Records, records, true}, {sm.IDIndex, ididx, true}, {sm.TagIndex, tagidx, true}, {sm.TagLists, taglists, true}}
	hashes := map[string]string{}
	for _, q := range parts {
		hashes[q.name] = fmt.Sprintf("%x", sha256.Sum256(q.b))
	}
	mf := Manifest{Role: "storage", Format: "memoryai-body-v2", Version: imageVersion, MemoryABI: memoryABI, BodyID: nextID("body"), Root: "", GenesisPath: "genesis.json", Entry: map[string]string{}, Hashes: hashes, Store: sm}
	mb, _ := json.MarshalIndent(mf, "", "  ")
	mb = append(mb, '\n')
	parts = append(parts, zipEntry{"manifest.json", mb, false})
	return writeDetZip(path, parts)
}
func (e *Engine) createAndMountSpace(path string) (string, error) {
	locator := strings.TrimSpace(path)
	if locator == "" {
		locator = e.nextSpacePath()
	}
	cp, err := e.localLocatorPath(locator)
	if err != nil {
		return "", err
	}
	if _, err = os.Stat(cp); errors.Is(err, os.ErrNotExist) {
		var st syscall.Statfs_t
		if er := syscall.Statfs(filepath.Dir(cp), &st); er != nil {
			return "", er
		}
		available := st.Bavail * uint64(st.Bsize)
		minExpansionFree := uint64(automaticShardMinFreeBytes())
		if available < minExpansionFree {
			return "", fmt.Errorf("Memory expansion requires at least %d free bytes at target path: available=%d", minExpansionFree, available)
		}
		if err = createEmptyBody(cp); err != nil {
			return "", err
		}
	} else if err != nil {
		return "", err
	}
	if _, err = e.mountSpace(cp); err != nil {
		return "", err
	}
	if strings.HasPrefix(locator, "local://") {
		return locator, nil
	}
	if rel, er := filepath.Rel(filepath.Dir(e.bodyPath), cp); er == nil && rel != "." && !strings.HasPrefix(rel, "..") {
		return "local://" + filepath.ToSlash(rel), nil
	}
	return cp, nil
}
func (e *Engine) persistAll() error {
	if err := persistEngineIncremental(e); err != nil {
		return err
	}
	e.spaceMu.RLock()
	spaces := make([]*Engine, 0, len(e.spaces))
	for _, sp := range e.spaces {
		spaces = append(spaces, sp)
	}
	e.spaceMu.RUnlock()
	for _, sp := range spaces {
		if err := persistEngineIncremental(sp); err != nil {
			return err
		}
	}
	return nil
}
func (e *Engine) allMountedIDs() ([]string, error) {
	ids, err := e.localIDs()
	if err != nil {
		return nil, err
	}
	seen := map[string]bool{}
	for _, id := range ids {
		seen[id] = true
	}
	e.spaceMu.RLock()
	spaces := make([]*Engine, 0, len(e.spaces))
	for _, sp := range e.spaces {
		spaces = append(spaces, sp)
	}
	e.spaceMu.RUnlock()
	for _, sp := range spaces {
		xs, er := sp.localIDs()
		if er != nil {
			return nil, er
		}
		for _, id := range xs {
			if !seen[id] {
				seen[id] = true
				ids = append(ids, id)
			}
		}
	}
	sort.Strings(ids)
	return ids, nil
}
func (e *Engine) transferMemory(id, target string, move bool) (string, error) {
	return transferMemoryDurable(e, id, target, move)
}
func sameStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
func sameArgs(a, b map[string]string) bool {
	if len(a) != len(b) {
		return false
	}
	for k, v := range a {
		if b[k] != v {
			return false
		}
	}
	return true
}
func sameProgram(a, b []Op) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i].Code != b[i].Code || a[i].A != b[i].A || a[i].B != b[i].B || a[i].C != b[i].C || !sameArgs(a[i].Args, b[i].Args) {
			return false
		}
	}
	return true
}
func structuralMemoryEqual(a, b *Memory) bool {
	if a == nil || b == nil || a.ID != b.ID || a.Layer != b.Layer || a.Generation != b.Generation || a.Content != b.Content || a.Executable != b.Executable {
		return false
	}
	if !sameStrings(a.Parents, b.Parents) || !sameStrings(a.Tags, b.Tags) || !sameStrings(a.Trigger, b.Trigger) || !sameStrings(a.Capabilities, b.Capabilities) || !sameProgram(a.Program, b.Program) ||
		!sameStrings(a.SuccessHistory, b.SuccessHistory) || !sameStrings(a.FailureHistory, b.FailureHistory) || !sameStrings(a.MutationVariants, b.MutationVariants) {
		return false
	}
	ab, _ := json.Marshal(struct {
		Budget       ResourceBudget
		InputPattern map[string]any
		OutputEffect map[string]any
	}{a.Budget, a.InputPattern, a.OutputEffect})
	bb, _ := json.Marshal(struct {
		Budget       ResourceBudget
		InputPattern map[string]any
		OutputEffect map[string]any
	}{b.Budget, b.InputPattern, b.OutputEffect})
	return string(ab) == string(bb)
}

func (e *Engine) mergeSpace(source, target string) (map[string]any, error) {
	cp, err := e.mountSpace(source)
	if err != nil {
		return nil, err
	}
	src := e.spaces[cp]
	var dst *Engine
	if target == "" || target == "primary" || target == e.bodyPath {
		dst = e
	} else {
		tp, er := e.mountSpace(target)
		if er != nil {
			return nil, er
		}
		dst = e.spaces[tp]
	}
	if src == dst {
		return nil, errors.New("space_merge source and target must differ")
	}
	ms, err := src.allMemories()
	if err != nil {
		return nil, err
	}
	stat := map[string]any{"copied": 0, "duplicates": 0, "conflicts": 0, "conflict_ids": []string{}, "conflict_records": []map[string]string{}}
	conflictIDs := []string{}
	conflictRecords := []map[string]string{}
	for _, m := range ms {
		existing, er := dst.resolveIDLocal(m.ID)
		if er == nil && existing != nil {
			if memoryJSONDigest(existing) == memoryJSONDigest(m) {
				stat["duplicates"] = stat["duplicates"].(int) + 1
				continue
			}
			stat["conflicts"] = stat["conflicts"].(int) + 1
			conflictIDs = append(conflictIDs, m.ID)
			sourceJSON, _ := json.Marshal(m)
			targetJSON, _ := json.Marshal(existing)
			conflictRecords = append(conflictRecords, map[string]string{
				"id": m.ID, "source_json": string(sourceJSON), "target_json": string(targetJSON),
				"source_digest": memoryJSONDigest(m), "target_digest": memoryJSONDigest(existing),
			})
			continue
		}
		if er != nil && er != io.EOF {
			return nil, er
		}
		dst.addRuntimeMemory(copyMemory(m))
		stat["copied"] = stat["copied"].(int) + 1
	}
	stat["conflict_ids"] = conflictIDs
	stat["conflict_records"] = conflictRecords
	return stat, nil
}

func remoteEndpointKey(host, port, name string) string {
	return strings.TrimSpace(host) + "|" + strings.TrimSpace(port) + "|" + strings.TrimSpace(name)
}

// knownRemoteReplicaEndpoints returns the remote storage endpoints where the sole
// AI has previously persisted a concrete Memory identity.  These descriptors
// live in the core and survive even when the physical remote store is offline.
// They are evidence of temporary unreachability, not proof of corruption.
func (e *Engine) knownRemoteReplicaEndpoints(memoryID string) []RemoteMemoryEndpoint {
	if e == nil || e.manifest.Role != "core" || memoryID == "" {
		return nil
	}
	ids, er := e.listTagLocal("physical.storage.replica.of." + memoryID)
	if er != nil {
		return nil
	}
	seen := map[string]bool{}
	out := []RemoteMemoryEndpoint{}
	for _, id := range ids {
		m, er := e.resolveIDLocal(id)
		if er != nil || m == nil {
			continue
		}
		host := fmt.Sprint(m.State["host"])
		port := fmt.Sprint(m.State["port"])
		name := fmt.Sprint(m.State["name"])
		if host == "" || port == "" || name == "" {
			continue
		}
		key := remoteEndpointKey(host, port, name)
		if seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, RemoteMemoryEndpoint{Host: host, Port: port, Name: name, Timeout: parseTimeout(fmt.Sprint(m.State["timeout_ms"]))})
	}
	return out
}

func (e *Engine) fsckMounted() (map[string]any, error) {
	engines := []*Engine{e}
	e.spaceMu.RLock()
	for _, sp := range e.spaces {
		engines = append(engines, sp)
	}
	e.spaceMu.RUnlock()
	all := map[string]*Memory{}
	locations := map[string][]string{}
	replicas := 0
	for _, sp := range engines {
		if err := sp.validateStore(); err != nil {
			return nil, err
		}
		ms, err := sp.allMemories()
		if err != nil {
			return nil, err
		}
		for _, m := range ms {
			if old := all[m.ID]; old != nil {
				if !structuralMemoryEqual(old, m) {
					return nil, fmt.Errorf("divergent duplicate memory identity across bodies: %s", m.ID)
				}
				replicas++
				locations[m.ID] = append(locations[m.ID], sp.bodyPath)
				continue
			}
			all[m.ID] = m
			locations[m.ID] = []string{sp.bodyPath}
		}
	}

	remoteBodies := 0
	remoteOffline := 0
	offlineEndpoints := map[string]bool{}
	onlineEndpoints := map[string]bool{}
	for _, ep := range e.remoteMemoryEndpoints() {
		key := remoteEndpointKey(ep.Host, ep.Port, ep.Name)
		resp, er := remoteSpaceRequest(ep.Host, ep.Port, map[string]any{"op": "list", "name": ep.Name}, ep.Timeout)
		if er != nil {
			remoteOffline++
			offlineEndpoints[key] = true
			continue
		}
		var rr struct {
			OK  bool     `json:"ok"`
			IDs []string `json:"ids"`
		}
		if json.Unmarshal([]byte(resp), &rr) != nil || !rr.OK {
			remoteOffline++
			offlineEndpoints[key] = true
			continue
		}
		onlineEndpoints[key] = true
		remoteBodies++
		loc := "remote://" + net.JoinHostPort(ep.Host, ep.Port) + "/" + ep.Name
		for _, id := range rr.IDs {
			m, er := remoteMemoryGet(ep, id)
			if er != nil || m == nil {
				continue
			}
			if old := all[m.ID]; old != nil {
				if !structuralMemoryEqual(old, m) {
					return nil, fmt.Errorf("divergent duplicate memory identity across address space: %s", m.ID)
				}
				replicas++
				locations[m.ID] = append(locations[m.ID], loc)
				continue
			}
			all[m.ID] = m
			locations[m.ID] = []string{loc}
		}
	}

	// A missing lineage target is only considered temporarily UNAVAILABLE when
	// the core has durable evidence that this exact Memory identity was stored on
	// a currently unreachable remote endpoint.  A merely-offline unrelated node
	// never masks a true local/online missing-parent error.
	unavailableIDs := map[string]bool{}
	unavailableRefs := 0
	for _, m := range all {
		for _, pid := range m.Parents {
			if pid == "" || all[pid] != nil {
				continue
			}
			known := e.knownRemoteReplicaEndpoints(pid)
			temporarilyUnavailable := false
			for _, ep := range known {
				key := remoteEndpointKey(ep.Host, ep.Port, ep.Name)
				if offlineEndpoints[key] {
					// One known physical replica may still contain this identity.  If no
					// reachable replica supplied it, that is temporary forgetting.
					temporarilyUnavailable = true
				}
			}
			if temporarilyUnavailable {
				unavailableIDs[pid] = true
				unavailableRefs++
				continue
			}
			return nil, fmt.Errorf("missing parent %s of %s across Memory address space", pid, m.ID)
		}
	}
	unavailableList := make([]string, 0, len(unavailableIDs))
	for id := range unavailableIDs {
		unavailableList = append(unavailableList, id)
	}
	sort.Strings(unavailableList)
	return map[string]any{
		"ok":                               true,
		"integrity_ok":                     true,
		"complete":                         remoteOffline == 0 && len(unavailableList) == 0,
		"bodies":                           len(engines) + remoteBodies,
		"local_bodies":                     len(engines),
		"remote_bodies":                    remoteBodies,
		"remote_offline":                   remoteOffline,
		"temporarily_unavailable_memories": len(unavailableList),
		"temporarily_unavailable_refs":     unavailableRefs,
		"unavailable_ids":                  unavailableList,
		"memories":                         len(all),
		"replicas":                         replicas,
		"identities_with_locations":        len(locations),
	}, nil
}

func parseTimeout(s string) time.Duration {
	n, _ := strconv.Atoi(s)
	if n <= 0 {
		n = 5000
	}
	return time.Duration(n) * time.Millisecond
}
func remoteSpaceRequest(host, port string, req map[string]any, timeout time.Duration) (string, error) {
	c, err := storageDial(host, port, timeout)
	if err != nil {
		return "", err
	}
	defer c.Close()
	_ = c.SetDeadline(time.Now().Add(timeout))
	if err = writeStorageEnvelope(c, req); err != nil {
		return "", err
	}
	var body json.RawMessage
	if err = readStorageEnvelope(c, &body); err != nil {
		return "", err
	}
	return string(body), nil
}

func remoteSpaceObservedDigest(host, port, name, id string, timeout time.Duration) (string, bool, error) {
	resp, err := remoteSpaceRequest(host, port, map[string]any{"op": "digest", "name": name, "id": id}, timeout)
	if err != nil {
		return "", false, err
	}
	var rr struct {
		OK     bool   `json:"ok"`
		Digest string `json:"digest"`
		Error  string `json:"error"`
	}
	if err := json.Unmarshal([]byte(resp), &rr); err != nil {
		return "", false, err
	}
	if !rr.OK {
		if strings.EqualFold(strings.TrimSpace(rr.Error), "not found") {
			return "", false, nil
		}
		return "", false, fmt.Errorf("remote memory digest unavailable: %s", strings.TrimSpace(rr.Error))
	}
	if strings.TrimSpace(rr.Digest) == "" {
		return "", false, errors.New("remote memory digest empty")
	}
	return rr.Digest, true, nil
}

type memNodeRequest struct {
	Op             string  `json:"op"`
	Name           string  `json:"name"`
	ID             string  `json:"id"`
	Memory         *Memory `json:"memory,omitempty"`
	Replace        bool    `json:"replace,omitempty"`
	ExpectedDigest string  `json:"expected_digest,omitempty"`
}

func safeNodePath(root, name string) (string, error) {
	if strings.TrimSpace(name) == "" {
		return "", errors.New("name required")
	}
	return physicalSandboxPath(root, name, "remote storage")
}
func loadPassiveStorage(path string) (*Engine, error) {
	sp, err := loadEngineWithMutationJournal(path)
	if err != nil {
		return nil, err
	}
	if sp.manifest.Role != "storage" {
		sp.close()
		return nil, fmt.Errorf("remote node accepts passive storage .mem only, got role %q", sp.manifest.Role)
	}
	return sp, nil
}

func serveMemNode(root, addr string) error {
	if err := os.MkdirAll(root, 0755); err != nil {
		return err
	}
	if len(storageTransportKey()) == 0 {
		return errors.New("remote Memory storage requires MEMORYAI_STORAGE_CAPABILITY_KEY or MEMORYAI_MESH_CAPABILITY_KEY")
	}
	ln, err := storageListen(addr)
	if err != nil {
		return err
	}
	defer ln.Close()
	fmt.Printf("MEMORYAI_MEM_NODE %s root=%s\\n", ln.Addr().String(), root)
	concurrency := make(chan struct{}, storageMaxConcurrent())
	for {
		c, er := ln.Accept()
		if er != nil {
			return er
		}
		select {
		case concurrency <- struct{}{}:
			go func(conn net.Conn) {
				defer func() { <-concurrency }()
				handleMemNodeConn(root, conn)
			}(c)
		default:
			fmt.Fprintln(os.Stderr, "MEMORYAI_MEM_NODE remote storage physical concurrency limit reached")
			_ = c.Close()
		}
	}
}
func handleMemNodeConn(root string, c net.Conn) {
	defer c.Close()
	_ = c.SetDeadline(time.Now().Add(storageConnectionTimeout()))
	var req memNodeRequest
	if err := readStorageEnvelope(c, &req); err != nil {
		_ = writeStorageReplyBounded(c, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	reply := func(v any) { _ = writeStorageReplyBounded(c, v) }
	if req.Op == "stat" {
		var st syscall.Statfs_t
		if err := syscall.Statfs(root, &st); err != nil {
			reply(map[string]any{"ok": false, "error": err.Error()})
			return
		}
		reply(map[string]any{"ok": true, "total_bytes": st.Blocks * uint64(st.Bsize), "free_bytes": st.Bfree * uint64(st.Bsize), "available_bytes": st.Bavail * uint64(st.Bsize), "memory_abi": memoryABI})
		return
	}
	p, err := safeNodePath(root, req.Name)
	if err != nil {
		reply(map[string]any{"ok": false, "error": err.Error()})
		return
	}
	switch req.Op {
	case "create":
		if _, er := os.Stat(p); errors.Is(er, os.ErrNotExist) {
			free, freeErr := physicalFreeBytes(p)
			minimum := automaticShardMinFreeBytes()
			if freeErr != nil {
				err = freeErr
			} else if free < minimum {
				err = fmt.Errorf("remote Memory expansion requires at least %d free bytes at target path; available=%d", minimum, free)
			} else {
				err = createEmptyBody(p)
			}
		}
		if err != nil {
			reply(map[string]any{"ok": false, "error": err.Error()})
		} else if sp, er := loadPassiveStorageSerialized(p); er == nil {
			defer sp.close()
			reply(map[string]any{"ok": true, "path": req.Name, "body_id": sp.manifest.BodyID, "memory_abi": sp.manifest.MemoryABI})
		} else {
			reply(map[string]any{"ok": false, "error": er.Error()})
		}
	case "list":
		sp, er := loadPassiveStorageSerialized(p)
		if er != nil {
			reply(map[string]any{"ok": false, "error": er.Error()})
			return
		}
		defer sp.close()
		ids, er := sp.localIDs()
		if er != nil {
			reply(map[string]any{"ok": false, "error": er.Error()})
		} else {
			reply(map[string]any{"ok": true, "ids": ids})
		}
	case "get":
		sp, er := loadPassiveStorageSerialized(p)
		if er != nil {
			reply(map[string]any{"ok": false, "error": er.Error()})
			return
		}
		defer sp.close()
		m, er := sp.resolveIDLocal(req.ID)
		if er != nil {
			reply(map[string]any{"ok": false, "error": "not found", "body_id": sp.manifest.BodyID, "memory_abi": sp.manifest.MemoryABI})
		} else {
			reply(map[string]any{"ok": true, "memory": m, "body_id": sp.manifest.BodyID, "memory_abi": sp.manifest.MemoryABI})
		}
	case "put":
		if req.Memory == nil {
			reply(map[string]any{"ok": false, "error": "memory required"})
			return
		}
		if _, er := os.Stat(p); errors.Is(er, os.ErrNotExist) {
			reply(map[string]any{"ok": false, "error": "remote storage body missing; explicit create required"})
			return
		} else if er != nil {
			reply(map[string]any{"ok": false, "error": er.Error()})
			return
		}
		sp, er := loadPassiveStorageSerialized(p)
		if er != nil {
			reply(map[string]any{"ok": false, "error": er.Error()})
			return
		}
		defer sp.close()
		q := copyMemory(req.Memory)
		id := strings.TrimSpace(q.ID)
		if id == "" {
			reply(map[string]any{"ok": false, "error": "memory id required"})
			return
		}
		if ex, resolveErr := sp.resolveIDLocal(id); resolveErr == nil {
			existingDigest := memoryJSONDigest(ex)
			incomingDigest := memoryJSONDigest(q)
			if existingDigest == incomingDigest {
				reply(map[string]any{"ok": true, "status": "same", "id": id, "same": true, "digest": existingDigest})
				return
			}
			if !req.Replace {
				reply(map[string]any{
					"ok": false, "status": "conflict", "id": id,
					"existing_digest": existingDigest, "incoming_digest": incomingDigest,
					"error": "divergent same-ID Memory requires Memory-owned reconciliation",
				})
				return
			}
			if strings.TrimSpace(req.ExpectedDigest) == "" {
				reply(map[string]any{
					"ok": false, "status": "conflict", "id": id,
					"existing_digest": existingDigest, "incoming_digest": incomingDigest,
					"error": "remote replace requires expected structural digest",
				})
				return
			}
			if req.ExpectedDigest != existingDigest {
				reply(map[string]any{
					"ok": false, "status": "conflict", "id": id,
					"existing_digest": existingDigest, "expected_digest": req.ExpectedDigest,
					"incoming_digest": incomingDigest,
					"error":           "remote replace compare-and-swap conflict",
				})
				return
			}
			if q.Revision <= ex.Revision {
				reply(map[string]any{
					"ok": false, "status": "conflict", "id": id,
					"existing_revision": ex.Revision, "incoming_revision": q.Revision,
					"error": "remote replace revision must advance monotonically",
				})
				return
			}
		} else if !errors.Is(resolveErr, io.EOF) {
			reply(map[string]any{"ok": false, "error": resolveErr.Error()})
			return
		} else if req.Replace && strings.TrimSpace(req.ExpectedDigest) != "" {
			reply(map[string]any{
				"ok": false, "status": "conflict", "id": id,
				"expected_digest": req.ExpectedDigest,
				"error":           "remote replace target disappeared before compare-and-swap",
			})
			return
		}
		if er = sp.upsertExplicitMemoryBounded(q); er != nil {
			reply(map[string]any{"ok": false, "error": er.Error()})
			return
		}
		if er = persistEngineIncremental(sp); er != nil {
			reply(map[string]any{"ok": false, "error": er.Error()})
		} else {
			reply(map[string]any{"ok": true, "status": "stored", "id": id, "digest": memoryJSONDigest(q)})
		}
	case "digest":
		sp, er := loadPassiveStorageSerialized(p)
		if er != nil {
			reply(map[string]any{"ok": false, "error": er.Error()})
			return
		}
		defer sp.close()
		m, er := sp.resolveIDLocal(req.ID)
		if er != nil {
			reply(map[string]any{"ok": false, "error": "not found"})
			return
		}
		reply(map[string]any{"ok": true, "digest": memoryJSONDigest(m)})
	case "delete":
		sp, er := loadPassiveStorageSerialized(p)
		if er != nil {
			reply(map[string]any{"ok": false, "error": er.Error()})
			return
		}
		defer sp.close()
		current, er := sp.resolveIDLocal(req.ID)
		if er != nil {
			reply(map[string]any{"ok": false, "error": "not found"})
			return
		}
		currentDigest := memoryJSONDigest(current)
		if strings.TrimSpace(req.ExpectedDigest) == "" {
			reply(map[string]any{
				"ok": false, "status": "conflict", "id": req.ID,
				"existing_digest": currentDigest,
				"error":           "remote delete requires expected structural digest",
			})
			return
		}
		if req.ExpectedDigest != currentDigest {
			reply(map[string]any{
				"ok": false, "status": "conflict", "id": req.ID,
				"existing_digest": currentDigest, "expected_digest": req.ExpectedDigest,
				"error": "remote delete compare-and-swap conflict",
			})
			return
		}
		if er = sp.deleteExplicitMemoryBounded(req.ID); er != nil {
			reply(map[string]any{"ok": false, "error": er.Error()})
			return
		}
		if er = persistEngineIncremental(sp); er != nil {
			reply(map[string]any{"ok": false, "error": er.Error()})
		} else {
			reply(map[string]any{"ok": true, "id": req.ID})
		}
	case "tag":
		sp, er := loadPassiveStorageSerialized(p)
		if er != nil {
			reply(map[string]any{"ok": false, "error": er.Error()})
			return
		}
		defer sp.close()
		ids, er := sp.listTagLocal(req.ID)
		if er != nil {
			reply(map[string]any{"ok": false, "error": er.Error()})
		} else {
			reply(map[string]any{"ok": true, "ids": ids})
		}
	default:
		reply(map[string]any{"ok": false, "error": "unknown op"})
	}
}

func stateList(v any) []string {
	switch q := v.(type) {
	case []string:
		return append([]string(nil), q...)
	case []any:
		out := make([]string, 0, len(q))
		for _, x := range q {
			out = append(out, fmt.Sprint(x))
		}
		return out
	case string:
		if strings.TrimSpace(q) == "" {
			return nil
		}
		var xs []string
		if json.Unmarshal([]byte(q), &xs) == nil {
			return xs
		}
		return splitCSV(q)
	default:
		return nil
	}
}
func unicodeTokens(s string) []string {
	out := []string{}
	var b strings.Builder
	flush := func() {
		if b.Len() > 0 {
			out = append(out, b.String())
			b.Reset()
		}
	}
	for _, r := range s {
		if unicode.IsLetter(r) || unicode.IsDigit(r) || r == '_' {
			b.WriteRune(r)
		} else {
			flush()
		}
	}
	flush()
	return out
}
func (e *Engine) localIDs() ([]string, error) {
	ids, err := e.storeAllIDs()
	if err != nil {
		return nil, err
	}
	seen := map[string]bool{}
	out := []string{}
	e.dataMu.RLock()
	for _, id := range ids {
		if e.deletedIDs[id] {
			continue
		}
		if !seen[id] {
			seen[id] = true
			out = append(out, id)
		}
	}
	for id := range e.newIDs {
		if e.deletedIDs[id] {
			continue
		}
		if !seen[id] {
			seen[id] = true
			out = append(out, id)
		}
	}
	e.dataMu.RUnlock()
	sort.Strings(out)
	return out, nil
}
func (e *Engine) localMemoryCount() int {
	return localMemoryCountFast(e)
}

func splitCSV(s string) []string {
	if strings.TrimSpace(s) == "" {
		return nil
	}
	xs := strings.Split(s, ",")
	out := []string{}
	for _, x := range xs {
		x = strings.TrimSpace(x)
		if x != "" {
			out = append(out, x)
		}
	}
	return out
}
func contains(xs []string, s string) bool {
	for _, x := range xs {
		if x == s {
			return true
		}
	}
	return false
}
func copyMemory(m *Memory) *Memory {
	b, _ := json.Marshal(m)
	var c Memory
	_ = json.Unmarshal(b, &c)
	return &c
}
func (e *Engine) isDirty() bool {
	e.dataMu.RLock()
	d := e.dirty
	e.dataMu.RUnlock()
	return d
}

func (e *Engine) addRuntimeMemory(m *Memory) {
	e.dataMu.Lock()
	e.cache[m.ID] = m
	e.newIDs[m.ID] = true
	e.dirtyIDs[m.ID] = true
	e.dirty = true
	for _, t := range m.Tags {
		e.tagDeltaAddLocked(m.ID, t)
	}
	e.dataMu.Unlock()
}

func physicalExchange(transport, host, port, request string, timeout time.Duration) (string, error) {
	maxBytes := physicalExchangeMaxBytes()
	if int64(len(request)) > maxBytes {
		return "", fmt.Errorf("physical exchange request exceeds physical byte ceiling: size=%d max=%d", len(request), maxBytes)
	}
	addr := net.JoinHostPort(host, port)
	d := &net.Dialer{Timeout: timeout}
	var c net.Conn
	var err error
	switch strings.ToLower(transport) {
	case "tcp", "":
		c, err = d.Dial("tcp", addr)
	case "tls":
		c, err = tls.DialWithDialer(d, "tcp", addr, &tls.Config{ServerName: host, MinVersion: tls.VersionTLS12})
	default:
		return "", fmt.Errorf("physical transport %q unsupported", transport)
	}
	if err != nil {
		return "", err
	}
	defer c.Close()
	_ = c.SetDeadline(time.Now().Add(timeout))
	if _, err = io.WriteString(c, request); err != nil {
		return "", err
	}
	b, err := readAllPhysicalBounded(c, maxBytes, "physical exchange")
	if err != nil {
		return "", err
	}
	return string(b), nil
}

func expand(s string, vars map[string]string) string {
	for pass := 0; pass < 6; pass++ {
		prev := s
		for k, v := range vars {
			s = strings.ReplaceAll(s, "{{"+k+"}}", v)
		}
		if s == prev {
			break
		}
	}
	return s
}

func (e *Engine) allMemories() ([]*Memory, error) {
	ids, err := e.localIDs()
	if err != nil {
		return nil, err
	}
	out := make([]*Memory, 0, len(ids))
	for _, id := range ids {
		m, er := e.resolveIDLocal(id)
		if er == io.EOF {
			continue
		}
		if er != nil {
			return nil, er
		}
		out = append(out, m)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out, nil
}
func (e *Engine) inspect() error {
	ms, err := e.allMemories()
	if err != nil {
		return err
	}
	counts := map[string]int{}
	for _, m := range ms {
		counts[m.Layer]++
	}
	out := map[string]any{"version": imageVersion, "memory_abi": e.manifest.MemoryABI, "body_id": e.manifest.BodyID, "role": e.manifest.Role, "root": e.genesis.Root, "memory_count": len(ms), "layers": counts, "trace": e.trace, "page_ins": e.pageIns, "hot_cache": len(e.cache)}
	b, _ := json.MarshalIndent(out, "", "  ")
	fmt.Println(string(b))
	return nil
}
func stateStringList(v any) []string {
	out := []string{}
	switch xs := v.(type) {
	case []string:
		for _, x := range xs {
			if strings.TrimSpace(x) != "" {
				out = append(out, x)
			}
		}
	case []any:
		for _, x := range xs {
			q := strings.TrimSpace(fmt.Sprint(x))
			if q != "" {
				out = append(out, q)
			}
		}
	case string:
		for _, x := range splitCSV(xs) {
			if strings.TrimSpace(x) != "" {
				out = append(out, strings.TrimSpace(x))
			}
		}
	}
	return out
}

func (e *Engine) mountRegisteredStorageForIntegrity() {
	if e.manifest.Role != "core" {
		return
	}
	reg, err := e.resolveIDLocal("memory.space.registry")
	if err != nil || reg == nil || reg.State == nil {
		return
	}
	for _, p := range stateStringList(reg.State["local_spaces"]) {
		cp, err := e.mountSpace(p)
		if err != nil {
			continue
		}
		e.spaceMu.RLock()
		sp := e.spaces[cp]
		e.spaceMu.RUnlock()
		if sp != nil && sp.manifest.Role != "storage" {
			_ = e.unmountSpace(cp)
		}
	}
}

func (e *Engine) fsck() error {
	zr, err := zip.OpenReader(e.bodyPath)
	if err != nil {
		return err
	}
	defer zr.Close()
	for name, want := range e.manifest.Hashes {
		file, er := uniqueZipFile(&zr.Reader, name)
		if er != nil {
			return er
		}
		got, er := hashZipFileStreaming(file)
		if er != nil {
			return er
		}
		if got != want {
			return fmt.Errorf("hash mismatch %s", name)
		}
	}
	if err = e.validateStore(); err != nil {
		return err
	}
	if e.manifest.Role == "core" {
		e.mountRegisteredStorageForIntegrity()
		stat, er := e.fsckMounted()
		if er != nil {
			return er
		}
		fmt.Printf("CLEAN version=%s abi=%s role=%s core_memories=%d bodies=%v memories=%v complete=%v remote_offline=%v unavailable_memories=%v\n", imageVersion, e.manifest.MemoryABI, e.manifest.Role, e.manifest.Store.MemoryCount, stat["bodies"], stat["memories"], stat["complete"], stat["remote_offline"], stat["temporarily_unavailable_memories"])
		return nil
	}
	ms, err := e.allMemories()
	if err != nil {
		return err
	}
	seen := map[string]bool{}
	for _, m := range ms {
		if m.ID == "" {
			return errors.New("empty memory id")
		}
		if seen[m.ID] {
			return fmt.Errorf("duplicate memory id %s", m.ID)
		}
		seen[m.ID] = true
	}
	// Storage bodies are fragments of one Memory Fabric; their parents may live in
	// another registered body. Cross-body lineage is validated by core fsckMounted().
	for _, m := range ms {
		for _, pid := range m.Parents {
			if pid == m.ID {
				return fmt.Errorf("self parent %s", m.ID)
			}
		}
	}
	fmt.Printf("CLEAN version=%s abi=%s role=%s memories=%d\n", imageVersion, e.manifest.MemoryABI, e.manifest.Role, len(ms))
	return nil
}
func (e *Engine) lineage(id string) error {
	m, err := e.resolveID(id)
	if err != nil {
		return fmt.Errorf("memory %s missing", id)
	}
	for m != nil {
		fmt.Printf("%s layer=%s gen=%d parents=%v tags=%v\n", m.ID, m.Layer, m.Generation, m.Parents, m.Tags)
		if len(m.Parents) == 0 {
			break
		}
		m, err = e.resolveID(m.Parents[0])
		if err != nil {
			return err
		}
	}
	return nil
}
func (e *Engine) saveBody(out string) error {
	unlockPersist := lockEnginePersistence(e)
	defer unlockPersist()
	ms, persistedSnapshot, err := snapshotMemoriesForPersistence(e)
	if err != nil {
		return err
	}
	records, ididx, tagidx, taglists, err := buildIndexedSections(ms)
	if err != nil {
		return err
	}
	root := Genesis{Format: "memoryai-genesis-index-v1", Root: e.genesis.Root, MemoryCount: len(ms)}
	gb, _ := json.MarshalIndent(root, "", "  ")
	gb = append(gb, '\n')
	zr, err := zip.OpenReader(e.bodyPath)
	if err != nil {
		return err
	}
	defer zr.Close()
	entries := []zipEntry{{e.manifest.GenesisPath, gb, false}, {e.manifest.Store.Records, records, true}, {e.manifest.Store.IDIndex, ididx, true}, {e.manifest.Store.TagIndex, tagidx, true}, {e.manifest.Store.TagLists, taglists, true}}
	for _, name := range e.manifest.Entry {
		b, er := readZipBytes(&zr.Reader, name)
		if er != nil {
			return er
		}
		entries = append(entries, zipEntry{name, b, false})
	}
	hashes := map[string]string{}
	for _, x := range entries {
		hashes[x.name] = fmt.Sprintf("%x", sha256.Sum256(x.b))
	}
	mf := e.manifest
	if mf.Role == "" {
		if e.genesis.Root == "" {
			mf.Role = "storage"
		} else {
			mf.Role = "core"
		}
	}
	mf.Version = imageVersion
	mf.Root = e.genesis.Root
	mf.Hashes = hashes
	mf.Store.IndexFormat = indexFormatV2
	mf.Store.MemoryCount = len(ms)
	mb, _ := json.MarshalIndent(mf, "", "  ")
	mb = append(mb, '\n')
	entries = append(entries, zipEntry{"manifest.json", mb, false})
	if err := writeDetZipWithMutationJournal(out, entries); err != nil {
		return err
	}
	return finalizePersistedBody(e, out, persistedSnapshot)
}

type zipEntry struct {
	name  string
	b     []byte
	store bool
}

func writeDetZip(path string, entries []zipEntry) error {
	sort.Slice(entries, func(i, j int) bool { return entries[i].name < entries[j].name })
	tmp := path + ".tmp"
	committed := false
	defer func() {
		if !committed {
			_ = os.Remove(tmp)
		}
	}()

	f, err := os.Create(tmp)
	if err != nil {
		return err
	}
	zw := zip.NewWriter(f)
	epoch := time.Date(1980, 1, 1, 0, 0, 0, 0, time.UTC)
	for _, e := range entries {
		method := uint16(zip.Deflate)
		if e.store {
			method = zip.Store
		}
		h := &zip.FileHeader{Name: e.name, Method: method}
		h.SetModTime(epoch)
		h.SetMode(0644)
		w, er := zw.CreateHeader(h)
		if er != nil {
			_ = zw.Close()
			_ = f.Close()
			return er
		}
		if _, er = w.Write(e.b); er != nil {
			_ = zw.Close()
			_ = f.Close()
			return er
		}
	}
	if err = zw.Close(); err != nil {
		_ = f.Close()
		return err
	}
	if err = f.Sync(); err != nil {
		_ = f.Close()
		return err
	}
	if err = f.Close(); err != nil {
		return err
	}
	if err = os.Rename(tmp, path); err != nil {
		return err
	}
	dir, err := os.Open(filepath.Dir(path))
	if err != nil {
		return err
	}
	defer dir.Close()
	if err = dir.Sync(); err != nil {
		return err
	}
	committed = true
	return nil
}

// Keep imports exercised by deterministic tooling and future Memory byte transforms.
var _ = bytes.Compare
var _ = filepath.Base
