# MemoryAI Continuous Development Log

> Mandatory recovery file. After interruption/context loss, read this file first, then sync current GitHub `main` before modifying code.

## Mandatory Git rule
Every commit to `main` must update this file in the same commit. Multi-file implementation/tests/config + this log must be one Git tree/commit. Every entry records base HEAD, changes, architecture protection, validation, remaining defects, and next exact work.

## Frozen architecture invariants
- Memory is the AI; AI is Memory. Never convert the system into Agent/plugin architecture.
- Minimal immutable Kernel owns only physical primitives: atomic storage, time, quotas, scheduling, sandbox VM, I/O, security boundary, crash recovery.
- Kernel must not hard-code cognitive ranking/relevance/goals/drives/semantic dimensions/policy/learning strategy/fixed cognitive vector shapes.
- No `Skill`; capability is experience -> stable executable/reusable Memory structure. Dynamic Belief State belongs to Memory.
- Executable Memory carries physical trigger/state/input/output/resource/success/mutation data and executes through VM/DSL.
- Sovereign Memory Mesh = one Sovereign AI + autonomous nodes + distributed Memory Fabric. Sharing requires Sovereign authorization; data plane prefers direct node access.
- One AI identity; Memory containers may expand/fuse. Remote Memory is directly read, never silently imported.
- Automatic local expansion requires >=5 GiB free space. `Memory.mem` is portable; Kernel is architecture-specific.
- Every code change starts from current GitHub `main` HEAD.

## Source/recovery model
Historical `Kernel/src/kernel.go` is retained as exact deterministic v27 bootstrap chunks. `tools/restore_source.py` verifies historical hashes and applies ordered forward overlays. Never mutate historical bootstrap bytes to implement new behavior. Exact historical raw SHA-256: `8b43be53777e2964297a4f0c1f1df061c06d2a7f8e515636ff2367681420018e`.

Current forward work includes Kernel ABI/cognitive-shape cleanup, persisted activation, lazy speculation/Fabric transaction fixes, resource/parallel/scheduler/event/structure/source-adapter/daemon/Mesh runtimes, bounded durable Mesh deferred spool, remote physical-body lifecycle/locking, remote mutation ACK durability, loss-averse transfer ordering, and durable Sovereign proposal replay fencing.

## Validation limitation
GitHub Actions has repeatedly returned platform-level `startup_failure` / zero-job runs. Never report CI PASS unless jobs actually execute. Local/static targeted validation is used while this persists; full restore -> gofmt -> vet -> test -> race Gate remains required on a working runner.

## Open defects / next exact work
1. Re-run the entire recovery/package/race Gate when an executable runner is available.
2. Continue Kernel production scan for residual hard-coded cognitive concepts/ranking/vector semantics and move cognition-owned state out of Kernel.
3. Add safe durable receipt ACK/GC for completed Sovereign proposal replay receipts only if delayed-node-spool replay cannot reopen duplicate Memory-event execution; until then bounded-ledger exhaustion must backpressure, never evict silently.
4. Continue scalability review of full-body `saveBody` rebuild cost; persistence is crash-safe/snapshot-consistent but large bodies still rebuild indexed sections during durable commit.
5. Consider a durable transfer-outcome journal only if operators need automatic reconciliation of ambiguous post-rename finalization failures; current loss-averse semantics retain the source and any possibly durable target copy rather than risking Memory loss.

## Commit entries

### Entry 001 — establish mandatory continuity log
- **Base HEAD:** `f4e1fd55d8500f71e7616e9864c2a319ed2dc09f`.
- **Changed:** created this mandatory recovery/continuity file with frozen invariants, source model, validation limitation, and next work.
- **Protection/validation:** prevents context-loss development from drifting from the Memory-is-AI architecture; base `main` was explicitly read before commit.
- **Next:** bound Mesh deferred journal; then remote durability ordering.

### Entry 002 — bounded durable Mesh deferred spool
- **Base HEAD:** `9a139cac852035b106279cb0f7ec16bb87f2587b`.
- **Changed:** added `Kernel/src/mesh_journal_runtime.go` + tests + `tools/kernel_mesh_journal_overlay.py`; restore chain routes generated proposal/flush VM paths through an atomic physical spool. Startup restores it; exact shared-proposal replacement is bounded by entry/byte limits; successful flush durably removes exact accepted requests; temp write + fsync + rename + directory sync is used.
- **Protection:** network failure bookkeeping remains bounded physical Kernel state; no cognitive ranking/policy is introduced and Sovereign authorization remains Memory-owned.
- **Validation:** fail-closed overlay assertions plus recovery/backpressure/replacement/durable-removal/oversize tests. Full repo Gate still blocked by runner startup failure.
- **Next:** remote mutation ACK + transfer source-delete sequencing.

### Entry 003 — remote ACK and transfer durability barrier
- **Base HEAD:** `f7e9725574385a18db7cd766757d0a17625773a2`.
- **Source proof:** reconstructed exact v27 kernel SHA above. Historical mem-node `put/delete` wrote before replying, but VM mutation primitives ignored `{ok:false}` and historical transfer could mark source deleted before durable target commit.
- **Changed:** added `Kernel/src/remote_durability_runtime.go` + tests + `tools/kernel_remote_durability_overlay.py`; appended restore overlay; strengthened `.github/workflows/remote-body-integrity.yml`. `remote_space_create/put/upsert/delete` reject negative application ACKs. mem-node persistence uses canonical Engine `bodyPath`. Transfer now orders: destination mutation -> durable persist -> read-back verify -> unchanged-source check -> source delete -> durable source persist -> absence verify -> success. Source-delete persistence failure restores/retains source when safe; duplicate is preferred to Memory loss.
- **Protection:** physical transaction/ACK/crash-safety only; no semantic ranking/policy/goal/relevance/learning/vector logic.
- **Validation:** gofmt; signature-compatible runtime compile/tests; Python overlay syntax and exact historical block application checks. Full repository Gate not reported PASS because runner unavailable.
- **Next:** Mesh proposal replay suppression, then explicit transfer-target capacity.

### Entry 004 — durable Sovereign proposal replay fence
- **Base HEAD:** `e496098a1ade8aca805221c535fd9607657b25d8`.
- **Defect:** Entry 002's durable node spool was still at-least-once across the window after Sovereign executed `mesh.shared.proposal` but before node durably removed its spool item. A retry could re-fire the same Memory-owned authorization event. A latest-only receipt was also insufficient because a delayed older spool retry must still recover its original result after a newer revision is accepted.
- **Changed:** added `Kernel/src/mesh_proposal_replay_runtime.go` and tests; `event_runtime.go` routes only `mesh.shared.proposal` through it; `fabric_context_runtime.go` releases only the in-memory replay cache on core close; durable ledger remains for restart recovery; added `.github/workflows/mesh-replay-integrity.yml`.
- **Semantics:** stable request identity hashes event/subject/memory/origin/digest/revision/sorted tags. Before handler execution, an `executing` fence is fsync/rename persisted. Completion persists only decision/reason fields as the receipt. Completed replay restores the original result without running handlers. Crash-left `executing` replay fails closed. Per origin+Memory slot revisions move monotonically; same-revision conflicting identity is rejected; historical request-ID receipts remain addressable so delayed old spool retries still get their original ACK after newer revisions. Different core body paths use distinct ledger files.
- **Boundedness:** `MEMORYAI_MESH_REPLAY_MAX_ENTRIES` / `MEMORYAI_MESH_REPLAY_MAX_BYTES` enforce hard physical limits. Full capacity backpressures; receipts are never silently evicted because that could reopen duplicate event execution.
- **Protection:** replay state is physical crash/transport bookkeeping only. Kernel does not decide approve/deny/relevance/policy; executable Memory remains the only producer of `mesh_decision`/reason. This is durable at-most-once Memory-event firing for a stable request identity, not a false claim of universal distributed exactly-once execution.
- **Validation before commit:** edited existing files were reconstructed from exact GitHub blobs (`event_runtime.go` `6c93de70...`, `fabric_context_runtime.go` `11f24c9a...`, prior continuity `8d7e49af...`); Go files are gofmt-clean. A signature-compatible local package passed targeted `go test -run MeshProposalReplay` and `go test -race -run MeshProposalReplay`, covering duplicate replay, restart recovery, crash fence, higher-revision progress, same-revision conflict, late old receipt after newer revision, per-body isolation, and bounded-ledger backpressure. Full repo Gate remains unreported because Actions is still startup-failure/zero-job.
- **Next:** repair explicit transfer-target quota/rollback semantics; then cognitive-boundary and `saveBody` O(N) scalability review. Receipt ACK/GC is later and must preserve delayed-spool replay safety.

### Entry 005 — explicit transfer target quota and failure-safe rollback
- **Base HEAD:** `7b64c62906e33eab115410d4e8a110878bbbc8c3`.
- **Defects:** explicit `space_copy/space_move` targeted a mounted body with direct `dst.addRuntimeMemory(candidate)`, bypassing `MEMORYAI_SHARD_MAX_MEMORIES`. A destination persistence failure could also leave the failed candidate in live `cache/newIDs/dirtyIDs/tagAdded`, allowing a later unrelated persistence to commit an operation that had already returned failure. The committed regression file also contained the compile typo `origiginalBodyPath`.
- **Changed:** `Kernel/src/remote_durability_runtime.go` now rechecks target ID and admits a new explicit-transfer candidate under `automaticShardMu`, using `shardHasCapacity(dst, 1)` before insertion. Persistence failure handling directly probes the physical body without opening a second Engine: only a proven on-disk absence permits rollback of the exact still-new candidate; matching durable, conflicting, or unreadable/unknown disk state is retained loss-aversely. Exact rollback removes only that candidate from cache/new/dirty/tag-added deltas and preserves unrelated dirty state. `Kernel/src/remote_durability_runtime_test.go` fixes the typo and adds full-target rejection, exact rollback, unrelated-dirty preservation, later-unrelated-persist leakage prevention, and post-rename/durable-on-disk retention coverage. Added `.github/workflows/transfer-target-integrity.yml`.
- **Concurrency/boundedness:** capacity check + candidate insertion share `automaticShardMu`, closing races with automatic `memory_new`/import placement without holding that global placement lock across disk I/O. Failed-persistence probe + rollback hold the Engine persistence lock so another persistence pass cannot race between the physical absence observation and rollback. Structurally identical pre-existing target Memory consumes no new slot. Source deletion is never attempted after any target persistence error.
- **Loss semantics:** if the target is provably absent, the transient candidate is removed and source remains. If target persistence crossed atomic rename before a later finalization error, or physical status cannot be proven, target state is retained and source remains; a duplicate is preferred to Memory loss.
- **Architecture protection:** this is physical quota/admission/crash-recovery bookkeeping only. Kernel does not choose a semantic destination, rank Memory, or add cognition policy; explicit target choice remains Memory/VM input.
- **Validation before commit:** current GitHub continuity baseline was reconstructed exactly (Git blob `1d3e600b1830427c88f69ee35bbb63b44b9e07ff`). Candidate Go files are `gofmt` clean. A signature-compatible Go package passed `go test` and `go test -race` for failed-candidate rollback and preservation of unrelated dirty state. Static workflow assertions cover bounded admission, physical probe, rollback, compile-typo prevention, and required regression names. Full repository Gate remains unreported because GitHub Actions runners remain platform `startup_failure` / zero-job.
- **Next:** continue Kernel cognitive-boundary scan, then attack `saveBody` O(N) full-body rebuild cost; replay receipt ACK/GC remains separate and must preserve delayed-spool replay safety.
