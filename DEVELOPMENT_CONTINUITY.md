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
Never report CI PASS unless jobs actually execute. Full recovery -> gofmt -> vet -> test -> race remains required on a working runner; targeted local compile/behavior/race harnesses are used while GitHub runners produce zero-job/startup failures.

## Open physical-kernel work
1. Make remote/move physical verification journal-aware before migrating ACK-critical full-persist barriers.
2. Re-run complete recovery/package/race Gate when a working runner is available.
3. Add replay-receipt ACK/GC only if safely proven; otherwise retain bounded-ledger backpressure.
4. Add transfer-outcome journal only if ambiguous post-rename reconciliation truly requires it.

## Development history
- Entries 001-008: recovery/frozen architecture; durable Mesh spool, negative ACK + target-first transfer, Sovereign replay fencing, quotas/rollback, removal of cognition-shaped execution APIs, in-body dual-slot mutation journal, incremental `persistAll()` with ACK-critical barriers retained.
- Entries 009-014: Memory-owned Experience ledger; repeated Experience -> candidate Structure; independent Reality Validation; Growth state inside `__memoryai.growth.core`; executable `Program []Op`; VM Reality -> PredictionError -> Experience.
- Entries 015-019: evidence-driven Structure revision/recombination/multi-generation Growth; grounded HTTP/TCP/TLS Reality with at-most-once receipt fence; factual Dynamic Belief without confidence/reward/ranking.
- Entry 020 (`5541088050acebfbf8e572f9023064a36649cfc3`): real Experience Context discovers branch discriminants; 2 formation facts + third independent Reality witness validate branches; parent retires only after all branches validate; contradiction can mark split contested.
- Entry 021 (`fd181a313a666aa52e23c1e9632e1f4c7c359eaa`): exact recursive Context activation resolves one validated leaf; missing/ambiguous Context refuses rather than guesses; VM Reality returns to leaf Experience/Belief; descendants receive fresh grounded action identity.
- Entry 022 (`4c362da0ccaaed1652df511092802ab9ae804e07`): Memory-authored `grounded_trial_series_id` creates deterministic Action Instances linked by Reality Experience; prepared/result-persisted/feedback-committed fencing preserves at-most-once restart behavior; PredictionError does not control continuation.
- Entry 023 (`a226c418ae47bebb459e1f1b2a1b5165814a0cab`): Sovereign-authorized transient direct reads for Experience/Belief/Context Split/Action Instance; no requester cache/upsert/import; disconnect means forgotten; provenance carries origin/backing revision/grant/read time/payload digest.

### Entry 024
- HEAD: `ccb26329ee086473ffbce01e2aba144a4a90314b`; base Entry 023.
- A validated Structure opts into one remote Evidence through exact Action facts. Every opportunity performs a fresh Sovereign-authorized direct read before local decision dedupe.
- Transient Evidence is projected into the existing VM Frame. Only the Memory Program can commit a new local Experience via `memory_remote_evidence_commit=1` and Memory-authored Context/Observation/Action/Outcome/PredictionError fields. Kernel never auto-imports the raw remote payload.
- Local Experience stores factual remote provenance and enters existing Belief/Context Split/Growth. `memory-remote-evidence-intake-fact` stores only provenance fingerprint + contract digest + decision; changed remote revision/digest or Memory contract can be reconsidered.
- Fixed physical capability drift so current `mesh_route_execution` and `physical_execution_concurrency_set` map to physical capabilities; legacy cognition-shaped names are inactive.

### Entry 025
- Base HEAD: `ccb26329ee086473ffbce01e2aba144a4a90314b`.
- Extended Remote Evidence intake to one ordered multi-evidence reasoning episode through Memory-authored `remote_evidence_inputs` JSON (`[{"kind":"...","id":"..."}]`). Existing single-evidence contracts remain source-compatible.
- Kernel preserves Memory-authored order, performs a fresh independently authorized direct read for every member and exposes all values together to one existing VM Program as indexed `remote_evidence.<index>.*` Frame data. Any unavailable/mismatched member fails the whole episode closed; already-read members are not used as a partial substitute.
- Per-episode fan-in is physically bounded at 32 for resource safety only. Kernel performs no evidence discovery, majority vote, confidence, ranking, truth adjudication or semantic weighting.
- Stable episode identity = ordered stable provenance (kind/id/origin/backing Memory/revision/payload digest) + Memory contract digest. Read time/grant nonce are excluded from identity, so every opportunity freshly reads remote Memory while unchanged bodies remain decision-idempotent; any member revision/digest change creates a new episode.
- One Memory Program makes one final commit/reject decision for the whole set. Commit creates exactly one local Experience with indexed provenance and only Memory-selected cognitive fields. Raw remote payload remains transient and is never copied into the Experience or episode fact.
- Added `memory-remote-evidence-episode-fact` with provenance-only fingerprints/evidence count/episode digest. Targeted coverage verifies two-node conflict in one VM episode, all-or-nothing disconnect, Memory rejection of an apparent 2-to-1 majority, fresh-read dedupe, and one-member revision change.
- The existing live Remote Evidence opportunity now runs a multi-evidence episode first and falls back to Entry 024 single-evidence intake when no multi contract is active, so no daemon ABI or background scheduler was added.
- Source-safely split the historical Mesh monolith into transport/core, authority, shared-access and observability files, then physically removed unreachable `routeCognition()`. Active physical routing remains `routeExecution()` / `mesh_route_execution`; cognition-shaped routing helper/error text is absent from intended compiled source.
- Final local checks: gofmt clean; production compile contract; multi-evidence behavior tests; `go vet`; targeted `go test -race`; live-intake wrapper compile; Mesh post-removal compile/vet/residue scan. Full repository CI PASS remains unreported until real jobs execute.
- Next: make remote/move ACK-critical physical verification mutation-journal-aware before considering full-persist barrier migration; then audit replay-receipt ACK/GC only if bounded correctness can be proven.
