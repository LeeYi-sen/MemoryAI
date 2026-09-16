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

    remote = read("Kernel/src/remote_durability_runtime.go")
    require(remote, ["physicalMemoryWithJournal", "readMutationJournal", "persistEngineIncremental(dst)", "persistEngineIncremental(owner)"], "remote durability")
    forbid(remote, ["persistEngineIfDirty(dst)", "persistEngineIfDirty(owner)"], "remote durability")

    mutation_overlay = read("tools/kernel_mutation_journal_overlay.py")
    remote_overlay = read("tools/kernel_remote_durability_overlay.py")
    forbid(mutation_overlay + remote_overlay, ["expected two remote full-durability barriers"], "generated remote durability")
    require(remote_overlay, ["persistEngineIncremental(sp)", "historical unsafe path remained", "remote node handler"], "generated remote durability")
    require(
        mutation_overlay,
        [
            "the only remaining mounted full-body helper is the local unmount",
            "unexpected mounted full-body persistence remained",
            "persistEngineIncremental(sp)",
        ],
        "mutation journal generation",
    )

    parallel = read("Kernel/src/parallel_runtime.go")
    gpu_linux = read("Kernel/src/physical_gpu_opencl_linux.go")
    require(parallel, ["physicalGPUBackend", "cpu+", "hybrid-dot", "gpu_fallback"], "parallel runtime")
    require(gpu_linux, ["libOpenCL.so.1", "dlopen", "memai_opencl_dot", "semantic"], "OpenCL physical backend")
    forbid(parallel + gpu_linux, ["semantic score", "fixed_semantic_lanes", "cognitive_selection"], "GPU runtime")

    restore = read("tools/restore_source.py")
    builder = read("tools/build_current_memory_seed.py")
    v29 = read("tools/migrations/v29_runtime_ownership.py")
    require(restore, ["build_current_seed", "current-required-structures.json"], "source restore")
    require(builder, ["v28_language_semantics.py", "v29_runtime_ownership.py", "9223866b6921c4eefa486e3b34597fabe0b8c823eb79a04a465e441b50128b80"], "current Memory seed builder")
    require(v29, ["memory.lifecycle.dispatch.parent", '"event:memory.activity"', "DRIVE_FACTORS"], "v29 Memory ownership")
    if '"prediction_error", "w_prediction_error"' in v29:
        raise RuntimeError("v29 Drive still declares prediction_error as a factor")

    # Production Kernel must not retain the old Go cognitive-growth ABI even under
    # a different filename. Historical implementations belong under legacy/ only.
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
    release_builder = read("tools/build_release.py")
    require(
        body_builder,
        ["memoryai-body-v2", "memoryai.phys.v1:", "JOURNAL_COMMENT_SIZE = 65535", "verify_body"],
        "Memory body builder",
    )
    require(
        release_builder,
        ["full_gate()", 'go", "test", "-race"', "daemon_smoke", "CHECKSUMS.sha256", "RELEASE-MANIFEST.json"],
        "release builder",
    )
    forbid(release_builder, ["--skip-race"], "release builder")

    # Production source must not grow a second persistent runtime state system.
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
