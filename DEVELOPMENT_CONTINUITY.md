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

Current overlay chain includes Kernel ABI cleanup, Mesh binding, speculative boundaries, durable Mesh deferred-journal VM routing, fixed-score primitive removal, lazy speculation, scale/remote-memory/persistence/shard/store/fabric/owner-lock fixes, remote physical body transaction fixes, and remote mutation/move durability enforcement.

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

### Remote mutation and local move durability

Remote/local physical mutation success now means durable success, not merely transport success:
- `remote_space_create/put/upsert/delete` reject mem-node `{ok:false}` application ACKs instead of treating a successful TCP read as VM success;
- mem-node `put/delete` persist through the Engine's canonical `bodyPath`, never the request alias path;
- historical `transferMemory` is routed to a durable two-phase implementation;
- copy/move commits and verifies the destination before a move may mark the source deleted;
- source structural state is rechecked after destination durability, so a concurrently changed source is retained rather than deleted;
- source deletion is durably persisted and verified before move success is returned;
- if source-delete persistence fails after target commit, source state is restored when safe; a duplicate is preferred to Memory loss;
- tests cover negative remote ACK, successful durable move, destination persistence failure retaining source, changed-source delete rejection, and source-delete persistence failure restoration;
- `.github/workflows/remote-body-integrity.yml` freezes these new boundaries together with the existing remote-body transaction rules.

## Known validation limitation

GitHub Actions has repeatedly produced platform-level `startup_failure` / zero-job runs. Do not report CI PASS unless jobs actually execute. Local/static source-boundary validation remains necessary while this platform issue persists.

## Open defects / next exact work

1. Re-run the **entire** recovery/package Gate on the new remote-durability overlay when an executable runner is available: bootstrap hash verify -> restore all overlays -> `gofmt` -> `go vet` -> full `go test` -> `go test -race`. Do not treat GitHub platform startup failure as a code failure or a PASS.
2. Continue scanning Kernel production runtime for residual hard-coded cognitive concepts/ranking/vector semantics and move any remaining cognition-owned state out of Kernel.
3. Review Mesh deferred-spool exactly-once semantics: current durability is at-least-once across the narrow crash window after Sovereign ACK but before local spool removal; if authority-side proposal events are not idempotent, add request IDs + Sovereign replay suppression.
4. Review explicit `space_copy/space_move` target capacity semantics. Durability is now protected, but an explicitly selected storage body still follows historical placement semantics rather than the automatic bounded-shard placement policy.
5. Continue scalability review of full-body `saveBody` rebuild cost. Persistence is crash-safe and snapshot-consistent, but large physical bodies still rebuild indexed sections during a durable commit.

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

### Entry 003 — remote ACK and transfer durability barrier

- **Base HEAD:** `f7e9725574385a18db7cd766757d0a17625773a2`.
- **Source proof before modification:** exact historical v27 `Kernel/src/kernel.go` was reconstructed and SHA-256 verified as `8b43be53777e2964297a4f0c1f1df061c06d2a7f8e515636ff2367681420018e`. The recovered source proved mem-node `put/delete` already wrote before replying but VM mutation primitives failed to inspect `{ok:false}`, and historical `transferMemory` could mark the source deleted before either body had a durable commit.
- **Commit purpose:** make every reported remote mutation / local Memory move success correspond to a verified physical durability boundary and eliminate the source-before-target-loss window.
- **Changed:** add `Kernel/src/remote_durability_runtime.go` with application ACK validation and loss-averse two-phase transfer; add `Kernel/src/remote_durability_runtime_test.go`; add `tools/kernel_remote_durability_overlay.py`; append the overlay to `tools/restore_source.py`; update `.github/workflows/remote-body-integrity.yml` to freeze remote ACK, canonical persistence, target-first move ordering and failure retention semantics.
- **Durability ordering:** destination mutation -> destination durable persistence -> destination read-back verification -> unchanged-source check -> source deletion -> source durable persistence -> source absence verification -> success. On source-delete persistence failure the destination remains durable and the source is restored/retained when safe.
- **Remote protocol fix:** `remote_space_create/put/upsert/delete` now translate mem-node negative ACKs into VM errors. mem-node `put/delete` use `persistEngineIfDirty(sp)` so symlink/request aliases cannot become the persistence target.
- **Architecture protection:** this adds only physical transaction/ACK/crash-safety behavior. No semantic ranking, policy, goal, relevance, learning logic or cognitive vector is introduced into Kernel.
- **Validation performed before commit:** new Go runtime and tests were `gofmt` formatted; runtime compiled against a signature-compatible physical stub; new Python overlay and updated restore script passed Python syntax compilation; the overlay was applied to the exact v27 transfer/remote blocks with the current passive-loader boundary simulated and its fail-closed assertions removed all historical `sp.saveBody(p)` and `src.deletedIDs[id] = true` paths. Full repository `go test`/race execution remains blocked by the unavailable working CI runner and must not be reported PASS.
- **Next:** execute the full recovery/package/race Gate when runner execution is available, then continue Kernel cognitive-boundary scan and Mesh exactly-once replay suppression review.
