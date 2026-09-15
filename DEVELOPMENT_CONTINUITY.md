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
- Runtime persistence must preserve the two-file product principle: do not require persistent WAL/delta sidecar files next to `Memory.mem`.

## Source / recovery model
Historical `Kernel/src/kernel.go` is retained as exact deterministic v27 bootstrap chunks. `tools/restore_source.py` verifies historical bytes then applies ordered forward overlays. Never mutate historical bootstrap bytes for new behavior.

Exact historical raw SHA-256: `8b43be53777e2964297a4f0c1f1df061c06d2a7f8e515636ff2367681420018e`.

Current forward work includes ABI/cognitive-shape cleanup, persisted activation, lazy speculative Fabric transactions, resource/parallel/scheduler/event/structure/source-adapter/daemon/Mesh runtimes, bounded physical shards, Store lifetime guards, durable Mesh deferred spool, Sovereign grants/replay fencing, remote physical-body locking, remote mutation ACK durability, loss-averse transfer ordering/rollback, and in-body bounded mutation journaling.

## Validation limitation
GitHub Actions has repeatedly returned platform-level `startup_failure` / zero-job runs. Never report CI PASS unless jobs actually execute. Local/static targeted validation is used meanwhile; full `restore_source -> gofmt -> go vet -> go test -> go test -race` remains required on a working runner.

## Open defects / next exact work
1. Remove the now-unreachable historical `routeCognition` source helper from `mesh_runtime.go`; generated production VM is already cognition-name free, but dead source should also be removed.
2. Make remote/move physical verification journal-aware before considering migration of those ACK-critical full-persist barriers to incremental persistence. Do not switch them early.
3. Re-run the complete recovery/package/race Gate when an executable runner is available.
4. Add safe replay-receipt ACK/GC only if delayed node-spool replay cannot reopen duplicate Memory-event execution; until then bounded ledger exhaustion must backpressure rather than evict.
5. Consider a durable transfer-outcome journal only if automatic reconciliation of ambiguous post-rename finalization failures becomes necessary; current semantics prefer duplicates over Memory loss.

## Commit entries

### Entry 001 — mandatory continuity log
- **Base HEAD:** `f4e1fd55d8500f71e7616e9864c2a319ed2dc09f`.
- Created this mandatory recovery file and froze the Git/main + Memory-is-AI development rules.

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
- Renamed physical concurrency/telemetry and Mesh reachability APIs so production VM no longer exposes `cognition_stats`, `cognition_concurrency_set`, `mesh_route_cognition`, `setCognitionConcurrency` or `cognitionInfo` as Kernel-owned concepts.
- Added deterministic `routeExecution`, physical boundary tests and workflow. Fixed score6 remains removed. Local signature-compatible package passed `GO111MODULE=off go test` and `go test -race`; exact v27 overlay output was gofmt-parseable.

### Entry 007 — bounded in-body mutation journal removes normal persist O(N) rewrite
- **Base HEAD:** `02cfdae62461dd2e33d082045ad487dd924034b9`.
- **Defect:** generated `persistAll()` still routed every dirty primary/mounted body through full `saveBody`, which enumerates all local IDs and rebuilds records + ID/tag/physical-feature indexes even when one Memory changed. With the bounded shard default this was finite but still O(records-per-body) write amplification for ordinary durable mutation.
- **Changed:** added `Kernel/src/mutation_journal_runtime.go` and `Kernel/src/mutation_journal_runtime_test.go`; added `tools/kernel_mutation_journal_overlay.py`; appended it last in `tools/restore_source.py`; added `.github/workflows/mutation-journal-integrity.yml`.
- **Physical format:** full-body `saveBody` now reserves exactly 65,535 bytes in the ZIP comment. The region contains two fixed journal slots. Each incremental commit writes only the inactive slot with monotonically increasing sequence, payload length, SHA-256 and full current mutation overlay, then fsyncs the same `.mem` inode. A torn newer slot fails hash validation and restart falls back to the previous valid slot. No persistent `.wal`, `.delta` or `.journal` sidecar is created.
- **Recovery:** each journal payload is bound to the physical base body manifest/store fingerprint. Restart via generated Engine loads restores changed/new/deleted Memory into the existing cache + dirty/new/deleted/tag overlay. If the base body has advanced while a journal slot remains, the slot is discarded only when every mutation is already reflected by the new base; otherwise load fails closed instead of replaying stale state onto a newer body.
- **Compaction/boundedness:** `MEMORYAI_MUTATION_JOURNAL_MAX_ENTRIES` defaults to 96 and the slot payload has a hard fixed-size ceiling. Legacy bodies without the fixed comment, oversized payloads, or too many mutation IDs fall back to existing full `saveBody` compaction. Full compaction writes a fresh zeroed journal region, preserving one `Memory.mem` physical container and the two-file product principle.
- **Routing:** generated `persistAll()` primary and mounted-body hot paths now call `persistEngineIncremental`; direct durability barriers outside generated `persistAll` remain on the pre-existing full path until separately reviewed so remote/move ACK guarantees are not weakened accidentally. Generated Engine opens are routed through `loadEngineWithMutationJournal`; generated full save uses `writeDetZipWithMutationJournal`.
- **Validation before commit:** new Go runtime is gofmt-clean and passed signature-compatible `GO111MODULE=off go test`/`go vet`. Dual-slot torn-write fallback and no-sidecar tests passed locally under `go test` and `go test -race`. Overlay passes Python syntax and synthetic boundary replacement. Repository workflow will run restart/compaction/torn-slot/no-sidecar regressions once Actions jobs execute; full repository Gate is not reported PASS while platform jobs remain unavailable.
- **Next:** remove dead `routeCognition` source helper, review remaining direct full-persist callers, then continue full-package/race validation and any further bounded persistence tuning.

### Entry 008 — isolate incremental persistAll from remote full-durability barriers
- **Base HEAD:** `2eec4bfc7499b1ed78b883e0ead7918d0d6c6cb4`.
- **Defect:** Entry 007's mutation-journal overlay used global occurrence assertions for `persistEngineIfDirty(e/sp)`. The earlier remote-durability overlay intentionally injects two additional `persistEngineIfDirty(sp)` calls for mem-node mutation ACKs, so the complete ordered recovery chain contains three `sp` calls before the journal overlay. Entry 007 therefore could fail during full source restoration even though its isolated/synthetic one-call test passed. The workflow also incorrectly rejected every remaining full-persist `sp` call, contradicting the documented remote durability boundary.
- **Changed:** `tools/kernel_mutation_journal_overlay.py` now isolates the generated `persistAll()` top-level function and replaces exactly one primary + one mounted-body call only inside that block. Full-durability calls outside `persistAll` are preserved, and the current chain explicitly requires the two remote mem-node barriers to remain. `.github/workflows/mutation-journal-integrity.yml` now inspects only the `persistAll()` block for incremental routing, separately asserts the two remote full-durability barriers, and runs a synthetic three-`sp`-call fixture through the overlay to prevent recurrence.
- **Architecture / safety:** ordinary periodic persistence remains bounded/incremental, while remote ACK and loss-averse transfer verification keep their full physical commit semantics until their disk probes become journal-aware. This fixes recovery-chain composition without weakening Memory durability.
- **Validation before commit:** updated Python overlay passes syntax compilation. A local synthetic generated source containing one mounted `persistAll` call plus two remote full-persist calls passed: one primary + one mounted call migrated to incremental, both remote full barriers survived, canonical Engine loads migrated, and the full-save writer became journal-capable. This explicitly reproduces the composition that broke Entry 007. Full repository recovery/test/race is still not reported PASS because Actions runners remain unavailable.
- **Next:** remove dead `routeCognition` source helper; then design journal-aware physical verification for remote/move barriers before any further write-amplification optimization.

### Entry 009 — Memory Growth Core: first-class Experience ledger
- **Base HEAD:** `dac581e521ddd7540f6045d2ba6d5b1508a49efd`.
- **Architecture gap addressed:** the repository had strong physical Memory Fabric, executable Memory, event transport and durable persistence, but no explicit first-class Experience model carrying observation/action/outcome, provenance and validation history as the raw material for Memory self-growth.
- **Changed:** added `Kernel/src/memory_growth_experience.go` and `Kernel/src/memory_growth_experience_test.go`. The new Memory-owned ledger records immutable Experience facts, explicit parent/child lineage, canonical content hashes, prediction provenance via `PredictionHash`, and repeated reality-validation history with success/failure counters.
- **Boundary:** this ledger does not rank relevance, assign goals, choose rewards, infer semantics or create an external learning engine. It only preserves the factual substrate from which later Memory-owned structure formation can emerge. The source location remains under the existing Kernel build package, but the implementation is explicitly Memory-owned and exposes no Kernel cognitive primitive.
- **Validation:** the new files were formatted with `gofmt` and passed targeted `go test` as a standalone Go package. Full repository Gate remains unreported because the GitHub Actions runner limitation remains in force.
- **Next:** bind the Experience ledger to actual Memory persistence/runtime state, then add Memory-owned Experience comparison and candidate Memory Structure formation without introducing a separate learning/skill subsystem.

### Entry 010 — Memory Growth Core: Experience recurrence -> candidate Memory Structure
- **Base HEAD:** `639b998e1f00d136ef7d83e43219cfd8e4ea81f7`.
- **Architecture gap addressed:** Experience now existed as first-class factual memory, but there was no deterministic bridge from repeated Experience into a first-class Memory Structure candidate.
- **Changed:** added `Kernel/src/memory_growth_structure.go` and `Kernel/src/memory_growth_structure_test.go`. The formation primitive canonicalizes Context/Observation/Action/Outcome/PredictionHash into a stable factual pattern hash, ignores identity/time/lineage for pattern matching, requires at least two occurrences, and creates a stable candidate ID with explicit source Experience provenance. Repeated formation is idempotent and accumulates new evidence rather than duplicating candidates.
- **Boundary:** this is not a semantic learner, relevance ranker, goal engine or Skill subsystem. It only promotes exact recurring factual patterns into `candidate` state; validation, execution, mutation, recombination and belief updates remain later Memory-owned stages.
- **Validation:** targeted tests cover recurrence gating, stable identity, provenance accumulation, idempotent rebuilding and lineage-independent factual hashing. Full repository recovery/race Gate is still not reported PASS because the Actions runner limitation remains in force.
- **Next:** connect Experience + candidate Structure state to durable `Memory.mem` runtime state, then add reality validation and promotion from candidate Structure to executable/reusable Memory Structure.

### Entry 011 — Memory Growth Core: candidate Structure -> independent reality validation -> validated Memory Structure
- **Base HEAD:** `fec526b5fffa90515623d85895cb6dd1dff45a7d`.
- **Architecture gap addressed:** Entry 010 could form a candidate from recurrence, but there was no independent post-formation evidence path that could reject a candidate, accumulate failure/success history, and promote it into a first-class validated Memory Structure.
- **Changed:** added `Kernel/src/memory_growth_validation.go` and `Kernel/src/memory_growth_validation_test.go`. Validation requires a source Experience belonging to candidate provenance plus a distinct post-formation witness Experience. A witness is recorded as success only when its canonical factual pattern matches the candidate pattern; failures remain evidence and never count toward promotion. Duplicate witness IDs and source-as-witness self-validation are rejected.
- **Promotion:** after a configurable minimum number of independent successful validations, the ledger materializes a stable `MemoryStructure` ID from the candidate pattern. The promoted structure preserves source provenance, independent validation provenance, Context/Observation/Action, expected Outcome, PredictionHash, state and validation counters. Pending validation state is retained internally so failures are not lost before promotion.
- **Boundary:** this is evidence-based structural validation, not semantic learning, relevance ranking, goal management, policy, execution orchestration or a Skill subsystem. Exact pattern matching is intentionally the temporary reality gate; the next stage must validate an executable structure through action -> real outcome -> new Experience rather than treating repeated static facts as execution competence.
- **Validation:** targeted tests cover independent-evidence enforcement, success-threshold promotion, failure preservation, provenance preservation and duplicate-witness rejection. Full repository recovery/race Gate remains unreported because GitHub Actions runners remain unavailable.
- **Next:** bind Experience/candidate/validated Structure ledgers into durable `Memory.mem` runtime state, then add executable Structure invocation with predicted outcome capture so validation is driven by actual action -> reality -> Experience feedback.

### Entry 011 follow-up — preserve pre-promotion validation counters
- **Base HEAD:** `ef442054fe1436b4e9bf7f1eaac86bb5fec3fc34`.
- **Correction:** the first Entry 011 implementation only retained promoted structures, which could discard failure/success counters collected before the promotion threshold. This would violate the requirement that validation history remain complete.
- **Changed:** `memory_growth_validation.go` now retains a pending `MemoryStructure` accumulator for every candidate under validation, while exposing it through `GetValidated` only after the success threshold is reached. Failure counts, success counts and independent witness provenance therefore survive across multiple validation calls and are included in the promoted structure.
- **Validation:** the existing Entry 011 tests explicitly require one failure + two independent successes to produce counts `(ValidationCount=3, SuccessCount=2, FailureCount=1)` and preserve both validation witness IDs.
- **Next:** continue with durable `Memory.mem` binding and then real executable Structure invocation -> predicted outcome -> new Experience feedback.
