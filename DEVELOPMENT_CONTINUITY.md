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

### Entry 020 baseline details
- Base HEAD: `aefc5bb1773a097bc4f16ab39abe3562aac60964`.
- Context keys come only from real `Experience.Context`; no semantic dimension table or confidence threshold.
- Branch formation needs two repeated exact Reality Experiences, then a third independent witness through the existing validation ledger.
- Validated split records are ordinary `memory-context-split` Memory inside `memory.mem`.
- Once all branch candidates validate, coarse parent becomes historical `context_split`, leaves validated/executable state, but remains in Memory lineage.
- New contradictory Reality can mark a split `contested` before completion.
- Grounded-action lineage prevents a contextual child from replaying a parent's already-consumed action ID.
- Entry 020 HEAD: `5541088050acebfbf8e572f9023064a36649cfc3`.

### Entry 021
- Base HEAD: `5541088050acebfbf8e572f9023064a36649cfc3`.
- Added Memory-native contextual execution resolution over independently validated `memory-context-split` facts. Kernel performs exact Context key/value equality only; it does not rank branches, infer missing values or select semantic relevance.
- A coarse/contextual Structure ID can resolve recursively through multiple validated splits to one unique validated leaf. Historical intermediate Structures already retired to `context_split` remain routing lineage nodes; the final executable leaf must still be currently validated.
- Missing required Context, unknown value, multiple validated splits, multiple matching branches, lineage disagreement or routing cycle never falls back to an arbitrary child. Each refusal is persisted as an ordinary `memory-context-activation-fact` inside `memory.mem`.
- Exact matched conditions are materialized into the selected executable Memory State as `memory_context_condition:<key>` plus root/leaf identity. Existing physical exact-index and scheduler responsibilities remain unchanged and non-cognitive.
- Live daemon `run` resolves an already-validated contextual leaf before invoking the existing transaction scheduler/VM. Non-contextual targets retain the historical direct execution path.
- After contextual VM execution, caller live Context + actual predicted-outcome fields are recorded as a new Experience with exact PredictionError and passed through the existing Dynamic Belief updater. A validated leaf can therefore accumulate its own Reality evidence and split again recursively.
- Context descendants that inherited a parent's autonomous grounded-action identity receive a deterministic fresh physical action ID derived from parent action identity + child Structure identity, while retaining `grounded_parent_action_id` provenance. This avoids parent receipt replay without inventing semantic action policy.
- Context activation match/refusal facts, executable Context conditions, refreshed action identity, Experience and Belief all persist through existing `memory.mem`; no context-router DB, sidecar, second executor, semantic selector, ranking service or background scheduler was added.
- Added targeted coverage for exact branch selection + restart persistence, missing/unknown/direct-leaf mismatch refusal, recursive two-level traversal through a retired intermediate, fresh grounded action identity, and live contextual VM feedback into leaf Experience/Belief.
- Local gofmt, production compile-only stubs, split-test compile stubs, combined Entry 020+021 compile, and recursive behavior harnesses passed. Full repository Gate/CI PASS remain unreported until a real runner executes recovery/gofmt/vet/test/race.
- Next: let contextual descendants issue repeated/new grounded trials through Memory-authored action instances; then expose Belief/split evidence across Sovereign Memory Mesh by direct remote read without importing remote Memory, while continuing physical-kernel cleanup.

### Entry 022
- Base HEAD: `fd181a313a666aa52e23c1e9632e1f4c7c359eaa`.
- Added `grounded_trial_series_id` as a Memory Structure-authored declaration for repeated grounded trials. Presence of the series is the cognitive authorization; Kernel does not choose a goal, success threshold, retry count or reward policy. Structures without a series retain historical one-shot grounded-action behavior.
- Added ordinary `memory-grounded-action-instance` records inside `memory.mem`. Each instance stores Structure/series/template/adaptor identity, exact Context, generation, parent instance/action and parent Experience provenance; no action-instance database, queue file, sidecar or second executor was introduced.
- Action Instance identity is deterministically derived from `series + Structure + template action + parent Experience`. There is no volatile global sequence source: restart reconstructs the same next physical identity from Memory lineage and cannot renumber or replay completed actions. Generation is retained only as lineage metadata.
- The validated Structure itself carries the currently authored instance identity, generation, template action identity and immediate trial parent action. Only physical identity metadata changes; action semantics, adapter and autonomous authorization remain Memory-authored and unchanged.
- At-most-once crash semantics now fence the entire repeated lineage. A current instance with no receipt waits for dispatch; `prepared` freezes the series because the external side effect is ambiguous; `result_persisted` must recover Experience/Belief before any successor is authored; only `feedback_committed` with a real persisted Experience can become the next instance's parent.
- Grounded feedback recognizes trial action identities and records Action Instance/series/generation provenance into the resulting Experience. Generation N+1 Experience has Generation N Reality Experience as its direct parent, forming `Structure -> Action Instance -> Reality -> Experience -> next Action Instance` rather than repeatedly pointing back to the original source Experience.
- Trial continuation is independent of prediction success/failure. Both matching and conflicting Reality become ordinary Experience/Belief facts; Kernel never stops or continues a declared series based on reward, fitness or PredictionError sign. Memory can stop/fork a series only by changing its own validated Structure contract/series identity through Memory growth.
- Existing context-derived fresh grounded identities remain compatible: a contextual leaf can first obtain its Structure-specific physical template identity, then author a repeated series of unique Action Instances without replaying the parent receipt.
- Added targeted coverage for three consecutive generations with unique physical IDs and exact Experience lineage, restart continuation to generation four, prepared-receipt lineage freeze/no replay, result-persisted recovery before successor authoring, and repeated conflicting Reality proving no Kernel outcome policy.
- Local gofmt, compile-only contract check, test compile check, `go vet`, targeted behavior tests and targeted `go test -race` passed in harnesses. Full repository recovery/gofmt/vet/test/race and CI PASS remain unreported until an actual repository runner executes them.
- Next: expose Belief/Context Split/Action Instance evidence through Sovereign Memory Mesh as authorized direct remote reads without importing remote Memory; then remove the dead historical `routeCognition` helper and continue physical-kernel closeout.
