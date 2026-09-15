# MemoryAI Continuous Development Log

> Mandatory recovery file. After interruption/context loss, read this file first, then sync current GitHub `main` before modifying code.

## Mandatory Git rule
Every commit to `main` must update this same file in the same commit. Never develop from an old chat/local copy without first syncing GitHub `main`.

## Frozen architecture invariants
- Memory is the AI; AI is Memory. Never convert the system into Agent/plugin architecture.
- Minimal immutable Kernel owns only physical primitives: atomic storage, time, quotas, scheduling, sandbox VM, I/O, security boundary and crash recovery.
- Kernel must not hard-code cognitive ranking, relevance, goals, drives, semantic dimensions, policy, learning strategy, fixed cognitive vectors, reward/fitness or Skill abstractions.
- Capability = Experience -> validated executable/reusable Memory Structure. Dynamic Belief State belongs to Memory.
- Sovereign Memory Mesh = one Sovereign AI + autonomous nodes + distributed Memory Fabric. Sharing requires Sovereign authorization; remote Memory is direct-read, never silently imported.
- One AI identity; Memory containers may expand/fuse. Automatic local expansion requires >=5 GiB free space.
- `memory.mem` is portable; Kernel is architecture-specific.
- Runtime product remains executable + `memory.mem`: no persistent WAL/delta/journal/database/JSON sidecars.
- All Experience, Structure, Validation, Belief, Prediction, History, Growth, Lineage, Mutation, Recombination and contextual state ultimately persist inside `memory.mem`.

## Validation limitation
GitHub Actions has repeatedly produced platform-level startup/zero-job failures. Never report CI PASS unless jobs actually execute. Full recovery -> gofmt -> vet -> test -> race remains required on a working runner; targeted local compile/behavior harnesses are used meanwhile.

## Open physical-kernel work
1. Remove unreachable historical `routeCognition` helper from `mesh_runtime.go`.
2. Make remote/move physical verification journal-aware before migrating ACK-critical full-persist barriers.
3. Re-run complete recovery/package/race Gate when a working runner is available.
4. Add replay-receipt ACK/GC only if safely proven; otherwise bounded ledgers backpressure.
5. Add transfer-outcome journal only if ambiguous post-rename reconciliation truly requires it.

## Development history
- Entry 001: recovery log + frozen Memory-is-AI rules.
- Entry 002: bounded durable Mesh deferred spool/restart recovery.
- Entry 003: negative ACK + target-first durable transfer.
- Entry 004: durable Sovereign proposal replay fencing.
- Entry 005: physical transfer quotas/failure-safe rollback.
- Entry 006: removed cognition-shaped physical execution APIs.
- Entry 007: in-body dual-slot mutation journaling without sidecars.
- Entry 008: incremental `persistAll()` while preserving remote durability barriers.
- Entry 009: Memory-owned Experience ledger and factual validation history.
- Entry 010: deterministic repeated Experience -> candidate Structure.
- Entry 011: independent Reality Validation -> validated Structure; pending counters retained.
- Entry 012: Growth state bound into `__memoryai.growth.core` inside `memory.mem`.
- Entry 013: executable `Program []Op` carried by validated Memory Structure and existing VM.
- Entry 014: VM execution -> actual outcome -> PredictionError -> Experience.
- Entry 015: repeated PredictionError evidence -> independently validated Structure revision; old version becomes historical `superseded`.
- Entry 016: deterministic validated-Structure recombination with lineage/program provenance.
- Entry 017: event-driven autonomous Growth cycle; real VM witness required; restart identity fixed.
- Entry 018: evidence-driven multi-generation continuation; Reality-rejected candidates leave automatic retry queue.
- Entry 019: grounded HTTP/TCP/TLS action -> Reality -> Experience -> Belief, durable at-most-once receipt fence; Belief stores factual support/conflict/missing observations only.
- Entry 020: Belief -> dynamic Context discovery -> candidate branches -> third independent Reality witness -> validated branches; parent retires only after all branches validate; contested Reality preserves the coarse parent.
- Entry 021: exact Context activation recursively resolves validated leaves, persists activation facts and returns live Reality feedback into leaf Experience/Belief.
- Entry 022: Memory-authored grounded trial series generates durable per-Reality Action Instances with Experience-linked at-most-once lineage.

### Recent continuity (Entries 020-023)
- Entry 020 (`5541088050acebfbf8e572f9023064a36649cfc3`): dynamic Context keys come only from real Experience; 2 repeated formation facts + third independent Reality witness validate branches; parent retires only after all branches validate; contradiction can mark split `contested`.
- Entry 021 (`fd181a313a666aa52e23c1e9632e1f4c7c359eaa`): exact recursive Context activation resolves one validated leaf; missing/ambiguous context refuses rather than guesses; VM Reality returns to leaf Experience/Belief; contextual descendants get fresh grounded action identity.
- Entry 022 (`4c362da0ccaaed1652df511092802ab9ae804e07`): Memory-authored `grounded_trial_series_id` creates deterministic per-Reality Action Instances linked by parent Experience; prepared/result-persisted/feedback-committed fencing preserves at-most-once behavior across restart; PredictionError does not control continuation.
- Entry 023 (`a226c418ae47bebb459e1f1b2a1b5165814a0cab`): Sovereign-authorized transient direct read for Experience/Belief/Context Split/Action Instance; Experience is projected live from remote `__memoryai.growth.core`; no requester cache/upsert/import; disconnect means forgotten; provenance carries origin/backing revision/grant/read time/payload digest.
- Entries 020-023 added no semantic ranking, confidence/reward policy, second executor, remote cache DB or runtime sidecar; all persistent local AI state remains inside `memory.mem`.

### Entry 024
- Base HEAD: `a226c418ae47bebb459e1f1b2a1b5165814a0cab`.
- Added Memory-authored Remote Evidence intake. A validated Structure opts in through exact Action facts (`remote_evidence_intake`, kind and evidence id); Kernel does not choose which remote evidence is cognitively relevant.
- Every opportunity performs a fresh Entry 023 Sovereign-authorized direct read before consulting local decision history, so an unreachable origin is still forgotten even when an earlier intake fingerprint exists locally.
- The transient evidence/provenance is projected into the Structure's ordinary VM Frame. The existing Memory Program alone decides whether to form local Experience by emitting `memory_remote_evidence_commit=1` and explicitly authors selected Context/Observation/Action/Outcome/PredictionError fields. Kernel never auto-imports the remote payload.
- A committed local Experience is a new local cognitive event, not a copy of remote Memory. Kernel adds only factual provenance (origin/evidence/backing revision/payload digest/read time/grant nonce); raw remote payload is not automatically persisted. The new Experience enters the existing Dynamic Belief path and subsequent Context Split/Structure Growth cycles.
- Added ordinary `memory-remote-evidence-intake-fact` records containing only provenance fingerprint, Memory contract digest and Memory decision. Exact evidence+contract decisions are idempotent; changed remote revision/digest or changed Memory contract can be reconsidered without a volatile counter or sidecar.
- Remote Belief/Context Split/Action Instance/Experience views are all exposed to the same VM decision bridge; conflicting evidence is not adjudicated by Kernel. No semantic ranking, confidence, reward, truth score, remote cache DB or background cognitive scheduler was added.
- Fixed a physical security-boundary drift: `requiredCapability()` now maps current `mesh_route_execution` and `physical_execution_concurrency_set` primitives, while legacy cognition-shaped names no longer receive active capability mappings. Added a regression test for this boundary.
- Live `run/input/event` now opportunity-drives Remote Evidence Intake before Grounding/Context Split/Growth, allowing a Memory-authored local Experience to participate in the existing growth loop in the same live activity without adding a background scheduler.
- Historical `routeCognition()` remains an unreachable duplicate helper in `mesh_runtime.go`; active VM/speculative/security primitive names already use `routeExecution`/`mesh_route_execution`. Remove it only through a source-safe Mesh file refactor rather than rewriting the 24 KiB monolith unsafely.
- Local gofmt, Remote Evidence intake behavior tests, `go vet`, targeted `go test -race`, daemon integration compile, and physical capability regression tests passed in harnesses. Full repository recovery/gofmt/vet/test/race and CI PASS remain unreported until an actual repository runner executes them.
- Next: make Memory Structures combine multiple independently authorized remote evidence views within one local reasoning episode without caching them, then finish physical-kernel closeout including safe removal of the dead `routeCognition` duplicate and journal-aware remote durability verification.
