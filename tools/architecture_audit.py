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


def audit() -> dict[str, object]:
    growth = sorted(p.name for p in SRC.glob("memory_growth_*.go"))
    if growth:
        raise RuntimeError(f"compiled Kernel still contains cognitive growth implementation: {growth}")

    daemon = read("Kernel/src/daemon_runtime.go")
    require(daemon, ["event:memory.activity" if False else 'fireEvent("memory.activity"', 'fireEvent("memory.run.resolve"'], "daemon")
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
    legacy = read("Kernel/src/legacy_runtime_migration.go")
    require(journal, ["memory-mesh-deferred-journal", '"durability":   "memory.mem"', '"sidecar":      false'], "mesh journal")
    require(replay, ["memory-mesh-proposal-replay", "persistAll()", "meshProposalReplayExecuting"], "mesh replay")
    require(state, ["memory-mesh-directory-node", "memory-mesh-shared-record", "recoverSovereignMeshState"], "sovereign state")
    require(grant, ["memory-mesh-grant-consumed", "persistConsumedMeshGrant", "Persist the fence before"], "mesh grants")
    forbid(journal + replay + state + grant, ["Memory.mesh-journal.", "Memory.mesh-proposal-replay.", "persistMeshJournalFile"], "active Mesh runtime")
    require(legacy, ["Memory.mesh-journal.*.json", "Memory.mesh-proposal-replay.*.json", "persist legacy runtime migration into memory.mem"], "legacy migration")

    shard = read("Kernel/src/shard_runtime.go")
    require(shard, ["minimumAutomaticShardFreeBytes int64 = 5 << 30", "ensureAutomaticShardDiskBudget", "syscall.Statfs"], "automatic Memory expansion")

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
