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
1. Repair explicit `space_copy/space_move` target-capacity semantics and rollback/cleanup on failed target persistence; explicit placement must respect the same physical boundedness invariants as automatic shard placement without introducing semantic placement policy.
2. Re-run the entire recovery/package/race Gate when an executable runner is available.
3. Continue Kernel production scan for residual hard-coded cognitive concepts/ranking/vector semantics and move cognition-owned state out of Kernel.
4. Add safe durable receipt ACK/GC for completed Sovereign proposal replay receipts only if delayed-node-spool replay cannot reopen duplicate Memory-event execution; until then bounded-ledger exhaustion must backpressure, never evict silently.
5. Continue scalability review of full-body `saveBody` rebuild cost; persistence is crash-safe/snapshot-consistent but large bodies still rebuild indexed sections during durable commit.

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
