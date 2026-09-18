#!/usr/bin/env python3
from __future__ import annotations

import json
from pathlib import Path
import re
import sys

ROOT = Path(__file__).resolve().parents[1]
SRC = ROOT / "Kernel/src"


def read(rel: str) -> str:
    return (ROOT / rel).read_text(encoding="utf-8")


def require(text: str, tokens: list[str], label: str) -> None:
    missing = [token for token in tokens if token not in text]
    if missing:
        raise RuntimeError(f"{label} missing architecture boundary: {missing}")


def forbid(text: str, tokens: list[str], label: str) -> None:
    found = [token for token in tokens if token in text]
    if found:
        raise RuntimeError(f"{label} contains forbidden architecture boundary: {found}")



def load_memory_seed() -> list[dict[str, object]]:
    payload = json.loads(read("Kernel/current-required-structures.json"))
    if isinstance(payload, dict):
        memories = payload.get("memories")
    else:
        memories = payload
    if not isinstance(memories, list):
        raise RuntimeError("current Memory seed must contain a memories list")
    return [m for m in memories if isinstance(m, dict)]


def kernel_primitive_opcodes() -> set[str]:
    kernel = read("Kernel/src/kernel.go")
    start = kernel.find("func (e *Engine) execPrimitive")
    end = kernel.find("\nfunc fieldString", start)
    if start < 0 or end < 0:
        raise RuntimeError("cannot locate Kernel execPrimitive opcode switch")
    block = kernel[start:end]
    return set(re.findall(r'case\s+"([^"]+)"\s*:', block))


def privileged_opcode_capabilities() -> dict[str, str]:
    security = read("Kernel/src/security_runtime.go")
    start = security.find("var privilegedOpCapability")
    end = security.find("\n}\n", start)
    if start < 0 or end < 0:
        raise RuntimeError("cannot locate privilegedOpCapability map")
    block = security[start:end]
    return dict(re.findall(r'"([^"]+)"\s*:\s*"([^"]+)"', block))


def audit_memory_kernel_abi() -> dict[str, int]:
    memories = load_memory_seed()
    supported = kernel_primitive_opcodes()
    privileged = privileged_opcode_capabilities()
    unsupported: list[str] = []
    missing_caps: list[str] = []
    for memory in memories:
        mid = str(memory.get("id", "<unknown>"))
        capabilities = {str(c) for c in (memory.get("capabilities") or [])}
        for op in memory.get("program") or []:
            if not isinstance(op, dict):
                continue
            code = str(op.get("code", "")).strip()
            if not code:
                continue
            if code not in supported:
                unsupported.append(f"{mid}:{code}")
            required = privileged.get(code)
            if required and required not in capabilities and "kernel.admin" not in capabilities:
                missing_caps.append(f"{mid}:{code}->{required}")
    if unsupported:
        raise RuntimeError(f"unsupported Memory opcode(s): {unsupported}")
    if missing_caps:
        raise RuntimeError(f"Memory opcode missing capability: {missing_caps}")
    return {"memory_programs": len(memories), "kernel_opcodes": len(supported)}

def audit() -> dict[str, object]:
    growth = sorted(p.name for p in SRC.glob("memory_growth_*.go"))
    if growth:
        raise RuntimeError(f"compiled Kernel still contains cognitive growth implementation: {growth}")

    identity = read("Kernel/src/identity_runtime.go")
    require(
        identity,
        [
            '"crypto/rand"',
            "identityProcessNonce",
            "identitySequence",
            "atomic.AddUint64(&identitySequence",
        ],
        "identity generator",
    )
    if re.search(r"func\s+nextID\s*\([^)]*\).*?UnixNano\(", identity, re.S):
        raise RuntimeError("identity generator must not depend on wall-clock granularity")
    kernel_source_text = read("Kernel/src/kernel.go")
    if "func nextID(" in kernel_source_text:
        raise RuntimeError("identity generator must live in the dedicated physical identity runtime")
    writer_start = kernel_source_text.find("func writeDetZip(")
    if writer_start < 0:
        raise RuntimeError("initial body writer missing")
    writer = kernel_source_text[writer_start:]
    require(
        writer,
        [
            "f.Sync()",
            "os.Rename(tmp, path)",
            "os.Open(filepath.Dir(path))",
            "dir.Sync()",
            "os.Remove(tmp)",
        ],
        "initial body writer",
    )

    path_sandbox = read("Kernel/src/path_sandbox_runtime.go")
    require(
        path_sandbox,
        [
            "physicalSandboxPath",
            "filepath.EvalSymlinks(absRoot)",
            "os.Lstat(current)",
            "os.ModeSymlink",
            "path contains symlink component",
        ],
        "physical path sandbox",
    )
    require(
        kernel_source_text,
        [
            'return physicalSandboxPath(root, rel, "artifact workspace")',
            'return physicalSandboxPath(root, name, "remote storage")',
        ],
        "shared physical path sandbox",
    )

    physical_limits = read("Kernel/src/physical_limits_runtime.go")
    require(
        physical_limits,
        [
            "hardPhysicalExchangeMaxBytes",
            "hardArtifactMaxBytes",
            "hardDaemonTransportMaxBytes",
            "hardMeshTransportMaxBytes",
            "MEMORYAI_PHYSICAL_EXCHANGE_MAX_BYTES",
            "MEMORYAI_ARTIFACT_MAX_BYTES",
            "MEMORYAI_DAEMON_MAX_BYTES",
            "MEMORYAI_MESH_MAX_BYTES",
            "io.LimitReader(r, maxBytes+1)",
            "encodeJSONPhysicalBounded",
            "encoded payload exceeds physical byte ceiling",
            "response exceeds physical byte ceiling",
        ],
        "physical byte ceilings",
    )
    require(
        kernel_source_text,
        [
            "physical exchange request exceeds physical byte ceiling",
            'readAllPhysicalBounded(c, maxBytes, "physical exchange")',
            "artifactMaxBytes()",
            'readAllPhysicalBounded(file, maxBytes, "artifact read")',
            "io.Copy(h, io.LimitReader(file, maxBytes+1))",
        ],
        "single-primitive physical I/O boundary",
    )

    body_input = read("Kernel/src/body_input_runtime.go")
    store_source = read("Kernel/src/store.go")
    body_builder = read("tools/build_memory_body.py")
    require(
        body_input,
        [
            "hardJSONZipEntryMaxBytes",
            "hardMemoryRecordMaxBytes",
            "hardTagListPayloadMaxBytes",
            "readZipFileBounded",
            "indexedSliceBounds",
            "validateUniqueZipEntryNames",
            "verifyBodyLoadIntegrity",
            "Memory manifest hash missing",
        ],
        "Memory body input bounds",
    )
    require(
        kernel_source_text + store_source,
        [
            'readZipFileBounded(file, hardJSONZipEntryMaxBytes, "Memory metadata")',
            "hashZipFileStreaming(file)",
            'indexedSliceBounds(s.records, e.off, e.length, hardMemoryRecordMaxBytes, "Memory record")',
            'indexedSliceBounds(s.tagLists, e.off, e.length, hardTagListPayloadMaxBytes, "tag-list payload")',
            "indexed section %s escapes physical body file",
            "indexFormatV2",
            "Memory record digest mismatch",
            "tag-list payload digest mismatch",
            "validateUniqueZipEntryNames(&zr.Reader)",
            "verifyBodyLoadIntegrity(&zr.Reader, mf)",
        ],
        "Memory indexed body bounds",
    )
    require(
        body_builder,
        [
            'INDEX_FORMAT = "memoryai-index-v2-sha256"',
            'struct.pack("<QQII32s"',
            '"index_format": INDEX_FORMAT',
        ],
        "Memory body builder index integrity",
    )
    v2_integrity = re.search(r"case indexFormatV2:(.*?)(?:default:)", body_input, re.S)
    if not v2_integrity:
        raise RuntimeError("Memory v2 lazy integrity branch missing")
    if "verify = append" in v2_integrity.group(1):
        raise RuntimeError("Memory v2 load integrity regressed to full records/taglists scan")
    legacy_integrity = re.search(r'case "":(.*?)case indexFormatV2:', body_input, re.S)
    if not legacy_integrity or "mf.Store.Records" not in legacy_integrity.group(1) or "mf.Store.TagLists" not in legacy_integrity.group(1):
        raise RuntimeError("Memory legacy load integrity must verify complete records/taglists")

    mesh_node_identity = read("Kernel/src/mesh_node_identity_runtime.go")
    mesh_runtime = read("Kernel/src/mesh_runtime.go")
    mesh_grant = read("Kernel/src/mesh_grant_runtime.go")
    mesh_authority = read("Kernel/src/mesh_authority_runtime.go")
    require(
        mesh_node_identity,
        [
            "MEMORYAI_MESH_NODE_PRIVATE_KEY_B64",
            "meshNodeSignatureHeader",
            "ed25519.Sign",
            "ed25519.Verify",
            "req.AuthenticatedNodeID = nodeID",
            "Mesh grant origin-node/requester mismatch",
        ],
        "Mesh node identity",
    )
    require(
        mesh_runtime + mesh_grant + mesh_authority,
        [
            "PublicKey string",
            "OriginPublicKey string",
            "meshGrantVersion           = 2",
            "shared grant requester identity mismatch",
            "node identity rebind denied",
            "sovereign grant requester identity mismatch",
        ],
        "Sovereign node identity binding",
    )

    event_runtime = read("Kernel/src/event_runtime.go")
    parallel_runtime = read("Kernel/src/parallel_runtime.go")
    require(
        event_runtime,
        [
            "hardEventDispatchBatchHandlers = 256",
            "eventDispatchBatchSize",
            "parallelCPUFor(batchLen, workers",
            "results := make([]handlerResult, batchLen)",
        ],
        "bounded event fanout",
    )
    forbid(
        event_runtime,
        ["go func(index int, memoryID string)"],
        "bounded event fanout",
    )
    require(
        parallel_runtime + kernel_source_text,
        [
            "hardParallelFanoutMaxTargets    = 65536",
            "MEMORYAI_PARALLEL_FANOUT_MAX_TARGETS",
            "parallelFanoutMaxTargets()",
            "call_parallel fan-out exceeds physical target ceiling",
        ],
        "bounded call_parallel fanout",
    )

    require(
        physical_limits + kernel_source_text,
        [
            "defaultFrameListMaxItems",
            "hardFrameListMaxItems",
            "MEMORYAI_FRAME_LIST_MAX_ITEMS",
            "ensureFrameListItems",
            'ensureFrameListItems(maxOut, "unicode_windows")',
            'ensureFrameListItems(len(f.Lists[op.A])+1, "list_append")',
            "FindAllStringSubmatch(f.Vars[op.A], maxItems+1)",
        ],
        "bounded Frame-list cardinality",
    )
    forbid(
        kernel_source_text,
        ["FindAllStringSubmatch(f.Vars[op.A], -1)"],
        "bounded Frame-list cardinality",
    )

    require(
        physical_limits + kernel_source_text,
        [
            "defaultFrameValueMaxBytes",
            "hardFrameValueMaxBytes",
            "MEMORYAI_FRAME_VALUE_MAX_BYTES",
            "defaultFrameOutputMaxItems",
            "hardFrameOutputMaxItems",
            "MEMORYAI_FRAME_OUTPUT_MAX_ITEMS",
            "defaultFrameOutputMaxBytes",
            "hardFrameOutputMaxBytes",
            "MEMORYAI_FRAME_OUTPUT_MAX_BYTES",
            "ensureFrameJoinBytes",
            "ensureFrameOutputAppend",
            'ensureFrameJoinBytes("str_join", left, sep, right)',
            "ensureFrameOutputAppend(f.Output, value)",
        ],
        "bounded Frame scalar/output growth",
    )

    require(
        kernel_source_text,
        [
            'ensureMemoryRecordWithinPhysicalLimit(&candidate, "memory_history_append")',
            'ensureMemoryRecordWithinPhysicalLimit(&candidate, "memory_tag_add")',
        ],
        "bounded Memory metadata record growth",
    )

    require(
        physical_limits + kernel_source_text,
        [
            "MEMORYAI_MEMORY_RECORD_MAX_BYTES",
            "hardMemoryRecordMaxBytes",
            "memoryRecordMaxBytes",
            "ensureMemoryRecordWithinPhysicalLimit",
            "memoryStateCandidateWithinPhysicalLimit",
            'memoryStateCandidateWithinPhysicalLimit(target, key, ls, "state_list_append")',
            'memoryStateCandidateWithinPhysicalLimit(target, key, ls, "state_list_unique_append")',
            'memoryStateCandidateWithinPhysicalLimit(target, key, ff(nv), "state_num_add")',
            'memoryStateCandidateWithinPhysicalLimit(target, key, value, "state_set")',
        ],
        "bounded Memory State record growth",
    )

    require(
        kernel_source_text,
        [
            "expandFrameValueBounded",
            "replaceAllFrameValueBounded",
            'expandedTarget, err := expandFrameValueBounded(idOrTag, f.Vars)',
            's, err = replaceAllFrameValueBounded(s, "{{"+k+"}}", v, maxBytes)',
            "template expansion exceeds physical Frame-value byte ceiling",
        ],
        "bounded template expansion",
    )
    forbid(
        kernel_source_text,
        [
            'func expand(s string, vars map[string]string) string',
            's = strings.ReplaceAll(s, "{{"+k+"}}", v)',
        ],
        "bounded template expansion",
    )

    require(
        physical_limits + kernel_source_text,
        [
            "defaultProgramMaxOps",
            "hardProgramMaxOps",
            "MEMORYAI_PROGRAM_MAX_OPS",
            "defaultProgramMaxBytes",
            "hardProgramMaxBytes",
            "MEMORYAI_PROGRAM_MAX_BYTES",
            "ensureProgramWithinPhysicalLimits",
            'ensureProgramOpCount(newLen, "program_insert_from")',
            'ensureProgramWithinPhysicalLimits(candidate, "program_set_field")',
            'ensureProgramWithinPhysicalLimits(candidate, "program_set_var_ref")',
            'ensureProgramWithinPhysicalLimits(newp, "program_replace_from")',
            'ensureProgramWithinPhysicalLimits(pp, "program_import")',
            'int64(len(rawProgram)) > programMaxBytes()',
        ],
        "bounded executable Program growth",
    )


    resource_runtime = read("Kernel/src/resource_runtime.go")
    require(
        resource_runtime + kernel_source_text,
        [
            "enterFrameResourceScope",
            "reservePrimitiveResourceBudget",
            "primitiveMayMemoryWrite",
            "primitiveMayEmitEvent",
            "checkResourceBudgetBeforePrimitive",
            "execPrimitive(m, op, f, pc, labels)",
        ],
        "pre-side-effect resource budget",
    )
    budget_check = kernel_source_text.find("checkResourceBudgetBeforePrimitive")
    primitive_exec = kernel_source_text.find("execPrimitive(m, op, f, pc, labels)")
    if budget_check < 0 or primitive_exec < 0 or budget_check > primitive_exec:
        raise RuntimeError("resource budget check must precede primitive execution")
    forbid(
        kernel_source_text + resource_runtime,
        ["memoryWriteCeiling", "eventCeiling", "enterFrameResourceCeilings"],
        "pre-side-effect resource budget",
    )

    daemon = read("Kernel/src/daemon_runtime.go")
    require(daemon, ["event:memory.activity" if False else 'fireEvent("memory.activity"', 'fireEvent("memory.run.resolve"'], "daemon")
    require(
        daemon + physical_limits,
        [
            "daemonConnectionTimeout",
            "daemonMaxConcurrent",
            "SetDeadline",
            "io.LimitedReader{R: conn, N: maxBytes + 1}",
            "writeDaemonResponseBounded",
            "encodeJSONPhysicalBounded",
            "daemon physical concurrency limit reached",
        ],
        "daemon transport boundary",
    )
    require(
        mesh_runtime + physical_limits,
        [
            "meshTransportMaxBytes",
            "mesh request exceeds physical byte ceiling",
            'readAllPhysicalBounded(resp.Body, maxBytes, "mesh response")',
            'readAllPhysicalBounded(r.Body, meshTransportMaxBytes(), "mesh request")',
            "writeMeshResponseBounded",
            "newMeshHTTPServer",
            "ReadHeaderTimeout: meshHTTPReadHeaderTimeout",
            "ReadTimeout:       meshHTTPReadTimeout",
            "WriteTimeout:      meshHTTPWriteTimeout",
            "IdleTimeout:       meshHTTPIdleTimeout",
            "MaxHeaderBytes:    meshHTTPMaxHeaderBytes",
        ],
        "Mesh HTTP transport boundary",
    )
    forbid(
        mesh_runtime,
        ['io.LimitReader(resp.Body, 2<<20)', 'io.LimitReader(r.Body, 2<<20)'],
        "Mesh HTTP transport boundary",
    )

    storage_transport = read("Kernel/src/storage_transport_security.go")
    require(
        storage_transport + physical_limits,
        [
            "storageTransportMaxBytes",
            "MEMORYAI_STORAGE_MAX_BYTES",
            "storageConnectionTimeout",
            "MEMORYAI_STORAGE_TIMEOUT_MS",
            "storageMaxConcurrent",
            "MEMORYAI_STORAGE_MAX_CONCURRENT",
            "io.LimitedReader{R: r, N: maxBytes + 1}",
            "encodeJSONPhysicalBounded",
            "writeStorageReplyBounded",
        ],
        "remote storage transport boundary",
    )
    require(
        kernel_source_text,
        [
            "make(chan struct{}, storageMaxConcurrent())",
            "case concurrency <- struct{}{}:",
            "remote storage physical concurrency limit reached",
            "SetDeadline(time.Now().Add(storageConnectionTimeout()))",
            "writeStorageReplyBounded(c",
        ],
        "remote storage transport boundary",
    )
    forbid(
        storage_transport + kernel_source_text,
        ['io.LimitReader(r, 4<<20)', 'io.LimitReader(c, 4<<20)', 'go handleMemNodeConn(root, c)'],
        "remote storage transport boundary",
    )

    source_adapter = read("Kernel/src/source_adapter_runtime.go")
    require(
        physical_limits,
        [
            "hardPhysicalExchangeTimeout",
            "parsePhysicalExchangeTimeoutMS",
            "physicalExchangeTimeout",
        ],
        "external I/O timeout boundary",
    )
    require(
        kernel_source_text,
        [
            "timeout = physicalExchangeTimeout(timeout)",
            "timeout = storageRequestTimeout(timeout)",
        ],
        "external I/O timeout boundary",
    )
    require(
        source_adapter + storage_transport,
        [
            "hardSourceAdapterTimeout",
            "clampPhysicalTimeout(d, defaultSourceAdapterTimeout, hardSourceAdapterTimeout)",
            "storageRequestTimeout",
        ],
        "external I/O timeout boundary",
    )
    forbid(
        daemon,
        [
            "RunAutonomousRemoteEvidenceIntakeCycle",
            "RunAutonomousGroundedActionCycle",
            "RunAutonomousContextBranchingCycle",
            "RunAutonomousMemoryGrowthCycle",
            "ResolveContextualExecutionTarget",
            "RecordContextualExecutionFeedback",
        ],
        "daemon",
    )

    journal = read("Kernel/src/mesh_journal_runtime.go")
    replay = read("Kernel/src/mesh_proposal_replay_runtime.go")
    state = read("Kernel/src/mesh_state_memory.go")
    grant = read("Kernel/src/mesh_grant_runtime.go")
    observability = read("Kernel/src/mesh_observability_runtime.go")
    legacy = read("Kernel/src/legacy_runtime_migration.go")
    require(journal, ["memory-mesh-deferred-journal", '"durability":   "memory.mem"', '"sidecar":      false'], "mesh journal")
    require(replay, ["memory-mesh-proposal-replay", "persistAll()", "meshProposalReplayExecuting"], "mesh replay")
    require(state, ["memory-mesh-directory-node", "memory-mesh-shared-record", "recoverSovereignMeshState"], "sovereign state")
    require(grant, ["memory-mesh-grant-consumed", "persistConsumedMeshGrant", "Persist the fence before"], "mesh grants")
    forbid(journal + replay + state + grant, ["Memory.mesh-journal.", "Memory.mesh-proposal-replay.", "persistMeshJournalFile"], "active Mesh runtime")
    if re.search(r"func\s+\(m \*meshRuntime\)\s+flushJournal\s*\(", observability):
        raise RuntimeError("non-durable Mesh journal flush implementation remains active")
    require(legacy, ["Memory.mesh-journal.*.json", "Memory.mesh-proposal-replay.*.json", "persist legacy runtime migration into memory.mem"], "legacy migration")

    shard = read("Kernel/src/shard_runtime.go")
    require(shard, ["minimumAutomaticShardFreeBytes int64 = 5 << 30", "ensureAutomaticShardDiskBudget", "syscall.Statfs"], "automatic Memory expansion")
    forbid(shard, ["createAutomaticWritableShard"], "Memory-owned expansion")
    require(
        shard,
        ["Memory Fabric capacity exhausted: Memory must create/select storage explicitly"],
        "Memory-owned expansion",
    )

    persistence = read("Kernel/src/persistence_runtime.go")
    require(
        persistence,
        ["func structuralMemoryDigest", "return memoryJSONDigest(m)"],
        "unified structural Memory digest",
    )

    kernel = read("Kernel/src/kernel.go")
    remote_durability = read("Kernel/src/remote_durability_runtime.go")
    storage_security = read("Kernel/src/storage_transport_security.go")
    require(
        storage_security,
        [
            "storageWireEnvelope",
            "storageTransportKey",
            "storageListenUsesTLS",
            "non-loopback Memory storage listener requires",
            "storageEnvelopeBody",
            "meshVerifyBytes",
        ],
        "remote storage transport security",
    )
    require(
        kernel,
        [
            "storageDial(host, port, timeout)",
            "writeStorageEnvelope(c, req)",
            "readStorageEnvelope(c, &req)",
            "upsertExplicitMemoryBounded(q)",
            "divergent same-ID Memory requires Memory-owned reconciliation",
            "remote storage body missing; explicit create required",
            "remote replace compare-and-swap conflict",
            "remote delete compare-and-swap conflict",
            "remote replace revision must advance monotonically",
            "remoteSpaceObservedDigest",
            "expected_digest",
            "memory_new parent %q unresolved",
            "ambiguous Memory tag",
            "ambiguous mutable Memory tag",
            "Memory must select an explicit ID",
        ],
        "remote storage/physical single-target boundary",
    )
    forbid(
        kernel,
        ["Deterministic physical tie-break only. Cognitive arbitration belongs to Memory."],
        "single-target Memory resolution",
    )
    forbid(
        kernel + remote_durability + storage_security,
        ['merge_source_id', 'createAutomaticWritableShard', 'net.DialTimeout("tcp"'],
        "physical remote conflict/transport boundary",
    )

    activation = read("Kernel/src/activation_runtime.go")
    activation_qualification = read("Kernel/src/activation_qualification.go")
    activation_sources = "\n".join(
        path.read_text(encoding="utf-8")
        for path in sorted(SRC.glob("activation*.go"))
        if not path.name.endswith("_test.go")
    )
    require(
        activation,
        [
            "pageCap        int",
            "MEMORYAI_ACTIVATION_PAGE_CAP",
            '"default_page_cap"',
            '"physical_page_cap_only"',
        ],
        "physical activation",
    )
    require(activation_qualification, ["ExactPageOrder", 'json:"exact_page_order"'], "physical activation qualification")
    forbid(
        activation_sources,
        [
            'json:"score"',
            "MEMORYAI_ACTIVATION_TOPK",
            '"default_top_k"',
            '"cognitive_ranking"',
            "ExactTopK",
            "MaxScoreDiff",
        ],
        "physical activation",
    )
    if re.search(r"\b(?:Score|topK)\b", activation_sources):
        raise RuntimeError("physical activation retains cognitive-shaped Score/topK identifier")
    activation_callers = daemon + "\n" + read("Kernel/src/kernel.go")
    if re.search(r"\btopK\b", activation_callers):
        raise RuntimeError("physical activation caller retains cognitive-shaped topK identifier")

    remote = read("Kernel/src/remote_durability_runtime.go")
    require(remote, ["physicalMemoryWithJournal", "readMutationJournal", "persistEngineIncremental(dst)", "persistEngineIncremental(owner)"], "remote durability")
    forbid(remote, ["persistEngineIfDirty(dst)", "persistEngineIfDirty(owner)"], "remote durability")

    # Direct-source repository: production durability is audited in the Go runtime
    # itself. Historical source-reconstruction overlays are intentionally absent.

    parallel = read("Kernel/src/parallel_runtime.go")
    gpu_linux = read("Kernel/src/physical_gpu_opencl_linux.go")
    require(parallel, ["physicalGPUBackend", "cpu+", "hybrid-dot", "gpu_fallback"], "parallel runtime")
    require(gpu_linux, ["libOpenCL.so.1", "dlopen", "memai_opencl_dot", "semantic"], "OpenCL physical backend")
    forbid(parallel + gpu_linux, ["semantic score", "fixed_semantic_lanes", "cognitive_selection"], "GPU runtime")

    abi = audit_memory_kernel_abi()

    production_kernel_sources = {
        path.name: path.read_text(encoding="utf-8")
        for path in SRC.glob("*.go")
        if not path.name.endswith("_test.go")
    }
    cognitive_namespace_hits = [
        f"{name}:{token}"
        for name, source in production_kernel_sources.items()
        for token in ("cog.surface.", "cog.input.")
        if token in source
    ]
    if cognitive_namespace_hits:
        raise RuntimeError(
            f"physical Kernel contains forbidden cognitive namespace interpretation: {cognitive_namespace_hits}"
        )
    active_storage_sources = {
        name: source
        for name, source in production_kernel_sources.items()
        if name != "legacy_runtime_migration.go"
    }
    storage_namespace_hits = [
        f"{name}:cog.storage."
        for name, source in active_storage_sources.items()
        if "cog.storage." in source
    ]
    if storage_namespace_hits:
        raise RuntimeError(
            f"active physical storage namespace retains cog.storage prefix: {storage_namespace_hits}"
        )

    stale_signature_revision_hits: list[str] = []
    for name, source in production_kernel_sources.items():
        lines = source.splitlines()
        for index, line in enumerate(lines):
            match = re.search(r"\b([A-Za-z_][A-Za-z0-9_]*)\.Revision\+\+", line)
            if not match:
                continue
            target = match.group(1)
            window = "\n".join(lines[max(0, index - 6): index + 1])
            if f'{target}.CapabilitySig = ""' not in window:
                stale_signature_revision_hits.append(f"{name}:{index + 1}:{target}.Revision++")
    if stale_signature_revision_hits:
        raise RuntimeError(
            f"revision mutation can retain stale CapabilitySig: {stale_signature_revision_hits}"
        )

    structure_runtime = read("Kernel/src/structure_runtime.go")
    if not re.search(
        r'q\.CapabilitySig\s*=\s*""\s*\n\s*q\.Revision\s*=\s*current\.Revision\s*\+\s*1',
        structure_runtime,
    ):
        raise RuntimeError("structure-sync revision bump can retain stale CapabilitySig")

    kernel_source = ROOT / "Kernel/src/kernel.go"
    memory_seed = ROOT / "Kernel/current-required-structures.json"
    if not kernel_source.is_file():
        raise RuntimeError("direct Kernel source missing: Kernel/src/kernel.go")
    if not memory_seed.is_file():
        raise RuntimeError("direct Memory seed missing: Kernel/current-required-structures.json")
    packed = [p for p in ROOT.rglob("*") if p.is_file() and (".gz.b64.part" in p.name or p.name.endswith(".bootstrap.json"))]
    if packed:
        raise RuntimeError(f"packaged bootstrap payloads remain in direct-source repository: {packed}")
    v29 = read("tools/migrations/v29_runtime_ownership.py")
    require(v29, ["memory.lifecycle.dispatch.parent", '"event:memory.activity"', "DRIVE_FACTORS"], "v29 Memory ownership")
    if '"prediction_error", "w_prediction_error"' in v29:
        raise RuntimeError("v29 Drive still declares prediction_error as a factor")

    cognitive_symbols = (
        "RunAutonomousRemoteEvidenceIntakeCycle",
        "RunAutonomousGroundedActionCycle",
        "RunAutonomousContextBranchingCycle",
        "RunAutonomousMemoryGrowthCycle",
        "ResolveContextualExecutionTarget",
        "RecordContextualExecutionFeedback",
        "NewMemoryExperienceLedger",
        "NewMemoryStructureFormation",
        "NewMemoryStructureValidationLedger",
        "memoryGrowthStateRecordID",
        "decodeMemoryGrowthState",
        "MemoryExperience",
        "MemoryStructureCandidate",
        "memoryStructureValidationLedger",
    )
    cognitive_hits: list[str] = []
    for path in SRC.glob("*.go"):
        if path.name.endswith("_test.go"):
            continue
        text = path.read_text(encoding="utf-8")
        for token in cognitive_symbols:
            if token in text:
                cognitive_hits.append(f"{path.name}:{token}")
    if cognitive_hits:
        raise RuntimeError(f"production Kernel retains Go-owned cognition: {cognitive_hits}")

    evidence = read("Kernel/src/mesh_evidence_runtime.go")
    require(evidence, ["RemoteMemoryEvidence", "json.Marshal(memory)", "shared_fetch"], "remote evidence")
    forbid(
        evidence,
        [
            "memoryGrowthStateRecordID", "decodeMemoryGrowthState", "MemoryExperience",
            "memoryBeliefTag", "memoryContextSplitTag", "groundedActionInstanceTag",
        ],
        "remote evidence",
    )

    body_builder = read("tools/build_memory_body.py")
    require(
        body_builder,
        ["memoryai-body-v2", "memoryai.phys.v1:", "JOURNAL_COMMENT_SIZE = 65535", "verify_body"],
        "Memory body builder",
    )

    allowed_sidecar_file = "legacy_runtime_migration.go"
    forbidden_suffixes = (".wal", ".delta.json", "Memory.mesh-journal.", "Memory.mesh-proposal-replay.")
    sidecar_hits: list[str] = []
    for path in SRC.glob("*.go"):
        if path.name == allowed_sidecar_file or path.name.endswith("_test.go"):
            continue
        text = path.read_text(encoding="utf-8")
        for token in forbidden_suffixes:
            if token in text:
                sidecar_hits.append(f"{path.name}:{token}")
    if sidecar_hits:
        raise RuntimeError(f"production persistent sidecar regression: {sidecar_hits}")

    return {
        "ok": True,
        "compiled_growth_files": 0,
        "mesh_durability": "memory.mem",
        "automatic_expansion_floor_bytes": 5 << 30,
        "remote_verification": "base+journal",
        "physical_activation": "exact-candidate-page/no-score",
        "physical_gpu": "OpenCL dynamic + CPU fallback",
        "cognitive_dispatch_owner": "Memory",
        "memory_programs_audited": abi["memory_programs"],
        "kernel_opcodes_audited": abi["kernel_opcodes"],
    }


def main() -> int:
    try:
        report = audit()
    except Exception as exc:
        print(f"ARCHITECTURE AUDIT FAILED: {exc}", file=sys.stderr)
        return 1
    print(json.dumps(report, indent=2, sort_keys=True))
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
