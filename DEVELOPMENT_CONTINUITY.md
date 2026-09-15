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
- Runtime persistence must preserve the two-file product principle: no persistent WAL/delta sidecar files next to `Memory.mem`.

## Validation limitation
GitHub Actions has repeatedly returned platform-level `startup_failure` / zero-job runs. Never report CI PASS unless jobs actually execute. Local/static targeted validation is used meanwhile; full recovery -> gofmt -> vet -> test -> race remains required on a working runner.

## Open defects / next exact work
1. Remove the unreachable historical `routeCognition` source helper from `mesh_runtime.go`.
2. Make remote/move physical verification journal-aware before migrating those ACK-critical full-persist barriers.
3. Re-run the complete recovery/package/race Gate when an executable runner is available.
4. Add replay-receipt ACK/GC only if it is proven safe; otherwise bounded replay ledgers backpressure.
5. Consider a durable transfer-outcome journal only if ambiguous post-rename finalization requires reconciliation.

## Commit entries

### Entry 001
- Base HEAD: `f4e1fd55d8500f71e7616e9864c2a319ed2dc09f`.
- Created the mandatory recovery log and froze the Memory-is-AI architecture rules.

### Entry 002
- Base HEAD: `9a139cac852035b106279cb0f7ec16bb87f2587b`.
- Added bounded durable Mesh deferred spool and restart recovery.

### Entry 003
- Base HEAD: `f7e9725574385a18db7cd766757d0a17625773a2`.
- Added negative ACK propagation and target-first durable transfer; duplicate is preferred to Memory loss.

### Entry 004
- Base HEAD: `e496098a1ade8aca805221c535fd9607657b25d8`.
- Added durable Sovereign proposal replay fencing and restart recovery.

### Entry 005
- Base HEAD: `7b64c62906e33eab115410d4e8a110878bbbc8c3`.
- Added physical transfer quotas and failure-safe rollback.

### Entry 006
- Base HEAD: `1f4ef65499a9c7d7ac517e04ad08e74a01a759b4`.
- Removed cognition-shaped physical execution APIs and replaced them with execution-named primitives.

### Entry 007
- Base HEAD: `02cfdae62461dd2e33d082045ad487dd924034b9`.
- Added bounded in-body dual-slot mutation journaling, torn-write recovery and compaction without sidecar files.

### Entry 008
- Base HEAD: `2eec4bfc7499b1ed78b883e0ead7918d0d6c6cb4`.
- Scoped incremental persistence to `persistAll()` so remote full-durability barriers remain intact.

### Entry 009
- Base HEAD: `dac581e521ddd7540f6045d2ba6d5b1508a49efd`.
- Added Memory-owned Experience with observation/action/outcome, lineage, hashes and validation history.

### Entry 010
- Base HEAD: `639b998e1f00d136ef7f83e43219cfd8e4ea81f7`.
- Added deterministic Experience recurrence -> candidate Memory Structure formation and provenance.

### Entry 011
- Base HEAD: `fec526b5fffa90515623d85895cb6dd1dff45a7d`.
- Added independent reality validation, failure/success evidence and promotion into validated Memory Structure.

### Entry 011 follow-up
- Base HEAD: `ef442054fe1436b4e9bf7f1eaac86bb5fec3fc34`.
- Preserved pending validation counters and witness provenance before promotion.

### Entry 012
- Base HEAD: `a16eca045dea12fb38bd8ba1c8a7985d21181b48`.
- Bound Experience/candidate/validation/validated Structure state into the reserved `__memoryai.growth.core` Memory record inside `memory.mem`; no second database or runtime sidecar.

### Entry 013
- Base HEAD: `b7ab15403c1319909f6eff84174b1719516f2b96`.
- Added executable `Program []Op` to `MemoryStructure`, plus `BindExecutableProgram` and `ExecutableMemory`. Existing Memory-native persistence carries Program through `memory.mem`.

### Entry 014
- Base HEAD: `f34783b2bfac0c2a50491b1745bbf7d760044ec7`.
- Added `PredictionError` to `MemoryExperience` and included it in canonical Experience hashing.
- Added `ExecuteMemoryStructure`: validated Structure -> existing executable Memory Fabric object -> existing canonical VM -> predicted outcome -> actual outcome -> exact prediction-error facts -> new Experience with parent lineage.
- No new executor, scheduler, scoring system, database or runtime artifact was introduced. Feedback remains inside the existing Memory Growth state and therefore inside `memory.mem`.
- Targeted tests cover exact matches, mismatches, missing results, outcome-field isolation and prediction-error hashing. Full repository Gate remains unreported because GitHub Actions runners remain unavailable.

### Entry 015
- Base HEAD: `6e0a8002fe15eb0af82a120ee3b5c7f576d82ead`.
- Added `ReconcileValidatedStructureFromExecution`: repeated PredictionError-backed Experience is fed back through the existing Experience -> Candidate -> independent Reality Validation chain.
- A corrected fact pattern must recur at least twice and pass independent validation before becoming the new validated Memory Structure.
- The old Structure is retained as historical Memory and marked `superseded`; the new Structure becomes the validated version.
- The existing executable `Program []Op` is explicitly preserved through `BindExecutableProgram`; Kernel never invents a new Program during self-revision.
- No new runtime artifact, database, executor, scheduler or sidecar was introduced. The complete state remains covered by the existing `memory.mem` Memory Growth persistence record.
- Added regression coverage for successful repeated-feedback revision and the no-mutation single-feedback case.
- Full repository Gate remains unreported because GitHub Actions runners remain unavailable.

### Entry 016
- Base HEAD: `f5f988f19a55f5c9a80b7f043b0e886c30c337e7`.
- Added deterministic validated-Structure recombination: two validated executable Structures can produce a recombination candidate by replacing one non-terminal Op at a deterministic boundary with an Op already present in the second parent.
- Recombination preserves both parent Structure IDs, source Experience provenance, executable Program and mutation index; no arbitrary program generation is introduced.
- Added a Memory-owned materialization bridge: after real execution creates an Experience, the recombination candidate adopts that real Experience pattern and enters the existing ordinary Candidate -> Reality Validation path.
- Added finalization that restores recombination lineage and executable Program only after existing validation has promoted the candidate; parent Structures remain intact.
- All new state is ordinary Memory Growth state and is covered by the existing `__memoryai.growth.core` record inside `memory.mem`; no database, sidecar, new executor, scheduler or ranking subsystem was introduced.
- Added targeted tests for deterministic mutation, parent lineage, real-experience materialization, finalization and rejection of unvalidated parents.
- Full repository Gate remains unreported because GitHub Actions runners remain unavailable.
- Next: connect repeated validated recombination candidates to autonomous self-growth scheduling and then build cross-Structure mutation selection from Memory-native evidence.
