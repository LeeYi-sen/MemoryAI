# MemoryAI Continuous Development Log

> **Mandatory recovery file.** After any interruption, reconnect, context loss, or handoff, read this file first and continue from its latest entry before modifying code.

## Git commit rule (mandatory)

From the commit that introduces this file onward, **every Git commit to `main` must update this same file in the same commit**. A code/config/test change must never be committed without a corresponding entry here.

Each entry must record:
- parent/base HEAD before the commit;
- files and behavior changed;
- why the change was required and which architecture invariant it protects;
- validation performed and validation still blocked;
- known remaining defects / next exact work item.

For multi-file commits, use one Git tree/commit so implementation + tests + this log are atomic.

## Frozen architecture invariants

- Memory is the AI; AI is Memory. Do not introduce Agent/plugin cognitive architecture.
- Minimal immutable Kernel only owns physical primitives: atomic storage, time, quotas, scheduling, sandbox VM, I/O, security boundary, crash recovery.
- Kernel must not hard-code cognitive ranking, relevance, goals, drives, semantic dimensions, policy, learning strategy, or fixed cognitive vector shapes.
- No `Skill`; capability is experience -> stable executable/reusable Memory structure.
- Dynamic Belief State belongs to Memory structures, not hard-coded Kernel cognition.
- Executable Memory carries physical trigger/state/input/output/resource/success/mutation data and executes through VM/DSL.
- Sovereign Memory Mesh: one Sovereign AI + autonomous nodes + distributed Memory Fabric; sharing requires Sovereign authorization; data plane should prefer direct node access.
- One AI identity; Memory containers may expand/fuse. Remote Memory is directly read, never silently imported.
- Automatic local expansion requires at least 5 GiB free space.
- Memory.mem is portable; Kernel is architecture-specific.
- Every code change starts from current GitHub `main` HEAD.

## Current source/recovery model

Historical `Kernel/src/kernel.go` is stored as exact v27 deterministic bootstrap chunks. `tools/restore_source.py` verifies the historical hashes and then applies ordered forward overlays. Do not mutate historical bootstrap bytes to implement new behavior.

Current overlay chain includes Kernel ABI cleanup, Mesh binding, speculative boundaries, durable Mesh deferred-journal VM routing, fixed-score primitive removal, lazy speculation, scale/remote-memory/persistence/shard/store/fabric/owner-lock fixes, and remote physical body transaction fixes.

## Recent completed work before this log

### Remote physical Memory body lifecycle

Implemented physical-path serialization around passive remote body mutation and local mount lifecycle:
- passive request lock spans `load -> mutate -> persist -> close`;
- local mount waits on same body-path transaction lease;
- unmount and root close wait on same lease;
- active physical body registry prevents a second Engine from opening an already-mounted body;
- transaction lock registry uses waiter/holder reference counts and reclaims unused path entries;
- physical path canonicalization resolves symlink aliases;
- generated Engine opens use canonical physical path so atomic persistence does not replace a symlink alias;
- duplicate mount through symlink alias is rejected;
- tests cover same-body serialization, different-body parallelism, failed-load lease release, mounted/passive exclusivity, mount-after-passive durability, lock-table reclamation, symlink alias locking, unmount waiting, root-close waiting, symlink persistence, duplicate alias mount;
- `.github/workflows/remote-body-integrity.yml` freezes these boundaries.

### Kernel cognitive-shape cleanup

Removed fixed six-lane score ABI:
- `parallelRuntime.Score6([6])` replaced by dimension-agnostic physical `Dot([])` arithmetic;
- generated VM `parallel_score6` case is removed by `tools/kernel_parallel_boundary_overlay.py`;
- tests prove 3-lane and 11-lane arithmetic and reject dimensional mismatch;
- Kernel no longer defines a fixed cognitive score vector shape.

### Mesh deferred journal durability

Production VM Mesh proposal/flush entry points are routed away from the historical unbounded in-memory journal and into a bounded physical spool:
- journal is atomically persisted before a caller receives `deferred`;
- startup binding restores the spool into the bounded in-memory mirror;
- exact shared-proposal replacement keeps only the newest deferred proposal per Memory ID;
- entry and byte limits enforce backpressure rather than dropping old requests or consuming unbounded RAM;
- successful flush removes exact accepted requests from the durable spool;
- spool uses temp write + file sync + rename + directory sync;
- tests cover restart recovery, entry backpressure, proposal replacement, byte backpressure without spool mutation, durable flush removal, and oversized recovery rejection;
- generated Kernel overlay asserts legacy `.proposeShared(` and `.flushJournal()` VM calls are absent.

## Known validation limitation

GitHub Actions has repeatedly produced platform-level `startup_failure` / zero-job runs. Do not report CI PASS unless jobs actually execute. Local/static source-boundary validation remains necessary while this platform issue persists.

## Open defects / next exact work

1. **Remote `remote_space_create/put/upsert/delete` durability ACK ordering is not fully source-proven yet.** Verify server success is returned only after durable target persistence; for any move/migration path, source deletion must occur only after target durability ACK.
2. Re-run full recovery chain and static package closure after the journal overlay: restore -> gofmt -> vet -> test -> race when executable runner is available.
3. Continue scanning Kernel production runtime for residual hard-coded cognitive concepts/ranking/vector semantics.
4. Review Mesh deferred-spool exactly-once semantics: current durability is at-least-once across the narrow crash window after Sovereign ACK but before local spool removal; if authority-side proposal events are not idempotent, add request IDs + Sovereign replay suppression.

## Commit entries

### Entry 001 — establish mandatory continuity log

- **Base HEAD:** `f4e1fd55d8500f71e7616e9864c2a319ed2dc09f`
- **Commit purpose:** establish this single mandatory continuity/recovery MD before further development.
- **Changed:** `DEVELOPMENT_CONTINUITY.md` created with frozen architecture invariants, current recovery model, recent completed repairs, known validation limitation, and exact next work.
- **Validation:** base `main` HEAD was explicitly read from GitHub before this commit.
- **Next:** repair Mesh unbounded deferred journal, then prove remote durability ACK/source-delete ordering.

### Entry 002 — bounded durable Mesh deferred spool

- **Base HEAD:** `9a139cac852035b106279cb0f7ec16bb87f2587b`.
- **Commit purpose:** eliminate the unbounded/in-memory-only Mesh deferred journal from production VM paths.
- **Changed:** add `Kernel/src/mesh_journal_runtime.go` with bounded atomic physical spool, startup recovery, proposal replacement, byte/entry backpressure, exact durable removal, durable proposal wrapper and bind recovery hook; add `Kernel/src/mesh_journal_runtime_test.go`; add `tools/kernel_mesh_journal_overlay.py`; update `tools/restore_source.py` to route generated VM proposal/flush calls through durable boundaries.
- **Architecture protection:** network failure state remains a bounded physical Kernel concern; no cognitive policy/ranking is introduced. Sovereign authorization remains Memory-owned.
- **Validation:** overlay contains fail-closed occurrence assertions; tests cover recovery, bounded growth, durable flush and corrupt/oversized recovery. Full `restore/gofmt/vet/test/race` execution still requires a working runner and must not be reported PASS until actually executed.
- **Next:** prove/fix `remote_space_create/put/upsert/delete` durability ACK ordering and migration source-delete sequencing; then run full recovery/package/race Gate.
