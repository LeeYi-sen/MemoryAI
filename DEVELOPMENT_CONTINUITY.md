# MemoryAI Continuous Development Log

> Mandatory recovery file. After interruption/context loss, read this file first, then sync current GitHub `main` before modifying code.

## Mandatory Git rule
Every commit to `main` must update this same file in the same commit. Implementation, tests, workflows, overlays and this log must be one Git tree/commit. Never develop from an old chat/local copy without first syncing current GitHub `main`.

## Frozen architecture invariants
- Memory is the AI; AI is Memory. Never convert the system into Agent/plugin architecture.
- Minimal immutable Kernel owns only physical primitives: atomic storage, time, quotas, scheduling, sandbox VM, I/O, security boundary, crash recovery.
- Kernel must not hard-code cognitive ranking, relevance, goals, drives, semantic dimensions, policy, learning strategy, fixed cognitive vector shapes, or Skill abstractions.
- Capability is experience -> stable executable/reusable Memory structure; Dynamic Belief State belongs to Memory.
- Sovereign Memory Mesh = one Sovereign AI + autonomous nodes + distributed Memory Fabric. Sharing requires Sovereign authorization; remote Memory is direct-read, never silently imported.
- One AI identity; Memory containers may expand/fuse. Automatic local expansion requires >=5 GiB free space.
- `Memory.mem` is portable; Kernel is architecture-specific.

## Source / recovery model
Historical `Kernel/src/kernel.go` is retained as exact deterministic v27 bootstrap chunks. `tools/restore_source.py` verifies historical bytes then applies ordered forward overlays. Never mutate historical bootstrap bytes for new behavior.

Exact historical raw SHA-256: `8b43be53777e2964297a4f0c1f1df061c06d2a7f8e515636ff2367681420018e`.

Current forward work includes ABI/cognitive-shape cleanup, persisted activation, lazy speculative Fabric transactions, resource/parallel/scheduler/event/structure/source-adapter/daemon/Mesh runtimes, bounded physical shards, Store lifetime guards, durable Mesh deferred spool, Sovereign grants/replay fencing, remote physical-body locking, remote mutation ACK durability, and loss-averse transfer ordering/rollback.

## Validation limitation
GitHub Actions has repeatedly returned platform-level `startup_failure` / zero-job runs. Never report CI PASS unless jobs actually execute. Local/static targeted validation is used meanwhile; full `restore_source -> gofmt -> go vet -> go test -> go test -race` remains required on a working runner.

## Open defects / next exact work
1. Remove the now-unreachable historical `routeCognition` source helper from `mesh_runtime.go` and continue scanning production source for behavior-bearing cognition remnants.
2. Attack `saveBody` O(N) per-body rebuild cost. Current persistence is crash-safe/snapshot-consistent and bodies are bounded, but durable mutation still rebuilds records and indexes for the entire body.
3. Re-run the complete recovery/package/race Gate when an executable runner is available.
4. Add safe replay-receipt ACK/GC only if delayed node-spool replay cannot reopen duplicate Memory-event execution; until then bounded ledger exhaustion must backpressure rather than evict.
5. Consider a durable transfer-outcome journal only if automatic reconciliation of ambiguous post-rename finalization failures becomes necessary; current semantics prefer duplicates over Memory loss.

## Commit entries

### Entry 001 — mandatory continuity log
- **Base HEAD:** `f4e1fd55d8500f71e7616e9864c2a319ed2dc09f`.
- Created this mandatory recovery file and frozen the Git/main + Memory-is-AI development rules.

### Entry 002 — bounded durable Mesh deferred spool
- **Base HEAD:** `9a139cac852035b106279cb0f7ec16bb87f2587b`.
- Added bounded atomic disk spool, startup recovery, replacement, entry/byte backpressure, fsync+rename durability and exact removal after successful authority flush. Production VM no longer depends on the historical unbounded in-memory proposal journal.

### Entry 003 — remote ACK and transfer durability barrier
- **Base HEAD:** `f7e9725574385a18db7cd766757d0a17625773a2`.
- Reconstructed exact v27 source and proved remote mutation primitives ignored negative application ACKs while historical transfer could delete source before durable destination commit.
- Added negative ACK propagation and target-first durable transfer: destination persist+verify -> unchanged-source check -> source durable delete+absence verify. Source is restored/retained on delete-persistence failure; duplicate is preferred to loss.

### Entry 004 — durable Sovereign proposal replay fence
- **Base HEAD:** `e496098a1ade8aca805221c535fd9607657b25d8`.
- Added durable request-identity replay ledger for `mesh.shared.proposal`: fsynced `executing` fence before handler execution, durable decision/reason receipt, restart recovery, late-old-request receipts, revision monotonicity, same-revision conflict rejection and hard entry/byte limits.
- Semantics are durable at-most-once Memory-event firing for a stable request identity, not a false universal distributed exactly-once claim.

### Entry 005 — explicit transfer target quota and failure-safe rollback
- **Base HEAD:** `7b64c62906e33eab115410d4e8a110878bbbc8c3`.
- Explicit `space_copy/space_move` now obeys the same per-body physical quota as automatic shard placement under `automaticShardMu`.
- Failed target persistence directly probes the physical body. Only proven on-disk absence permits exact transient-candidate rollback; matching/conflicting/unknown disk state is retained loss-aversely. Rollback preserves unrelated dirty state and prevents a failed transfer from leaking into a later unrelated durable commit.
- Fixed the committed regression typo and added transfer-target integrity Gate/tests.

### Entry 006 — physical execution ABI removes cognition-shaped Kernel primitives
- **Base HEAD:** `1f4ef65499a9c7d7ac517e04ad08e74a01a759b4`.
- **Defect:** production behavior was physically bounded, but the public VM/runtime surface still exposed `cognition_stats`, `cognition_concurrency_set`, `setCognitionConcurrency`, `cognitionInfo`, `mesh_route_cognition`, plus cognition-shaped diagnostic keys. These names made physical CPU/node plumbing look like a Kernel-owned cognition subsystem.
- **Changed:** `Kernel/src/parallel_runtime.go` renames concurrency/telemetry to `setPhysicalExecutionConcurrency` / `physicalRuntimeInfo` and removes cognition-shaped diagnostic keys. Added `Kernel/src/mesh_execution_routing.go`, which accepts one explicit executable Memory ID and performs deterministic physical reachability only. `Kernel/src/speculative_boundary_extra.go` marks the new physical side-effect primitive names canonical-only. `tools/kernel_parallel_boundary_overlay.py` still removes fixed `parallel_score6` and now migrates generated VM cases to `physical_runtime_stats`, `physical_execution_concurrency_set`, and `mesh_route_execution`; generated production code no longer calls the historical cognition-named route helper. Added `Kernel/src/physical_runtime_boundary_test.go` and `.github/workflows/physical-runtime-boundary.yml`.
- **Architecture protection:** this is ABI/name-boundary cleanup, not scheduler/cognition policy. Kernel exposes only physical capacity, telemetry and reachability. Executable Memory still decides what should execute and what any vector/priority means.
- **Validation before commit:** candidate Go files are gofmt-clean; Python overlay passes syntax compilation. A signature-compatible package passed `GO111MODULE=off go test` and `go test -race`. Applying the overlay to exact v27 source produced zero `parallel_score6`, `cognition_stats`, `cognition_concurrency_set`, `setCognitionConcurrency`, `mesh_route_cognition`, and `m.routeCognition` occurrences in generated output while each new physical primitive/call appears exactly once; generated output is gofmt-parseable. Full repository Gate is not reported PASS because Actions runners remain platform startup-failure/zero-job.
- **Next:** remove the dead historical `routeCognition` source helper itself, continue source cognitive-boundary scan, then attack `saveBody` O(N) full-body rebuild cost.
