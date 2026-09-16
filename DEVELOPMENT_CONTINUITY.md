# MemoryAI Continuous Development Log

> Mandatory recovery file. After interruption/context loss, read this file first, then sync current GitHub `main` before modifying code.

## Mandatory Git rule
Every commit to `main` must update this same file in the same commit. Never develop from an old chat/local copy without first syncing GitHub `main`.

### Mandatory stop rule
Before ending any MemoryAI development turn, all validated current code from that turn must be pushed to the GitHub repository and this `DEVELOPMENT_CONTINUITY.md` must be updated in the same atomic commit. A development turn must never end with valid current code existing only in a local/temp workspace. If a change is intentionally excluded, the record must state exactly why it was excluded.

## Frozen architecture invariants
- Memory is the AI; AI is Memory. Never convert the system into Agent/plugin architecture.
- Minimal immutable Kernel owns only physical primitives: atomic storage, time, quotas, scheduling, sandbox VM, I/O, security boundary and crash recovery.
- Kernel must not hard-code cognitive ranking, relevance, goals, drives, semantic dimensions, policy, learning strategy, fixed cognitive vectors, reward/fitness or Skill abstractions.
- Capability = Experience -> validated executable/reusable Memory Structure. Dynamic Belief State belongs to Memory.
- Sovereign Memory Mesh = one Sovereign AI + autonomous nodes + distributed Memory Fabric. Sharing requires Sovereign authorization; remote Memory is direct-read, never silently imported.
- One AI identity; Memory containers may expand/fuse. Automatic local expansion requires >=5 GiB free space.
- `memory.mem` is portable; Kernel is architecture-specific.
- Runtime AI persistence remains executable + `memory.mem`: no persistent WAL/delta/journal/database/JSON sidecars. PID/socket/log files are ephemeral operational state, never AI Memory.
- All Experience, Structure, Validation, Belief, Prediction, History, Growth, Lineage, Mutation, Recombination and contextual state ultimately persist inside `memory.mem`.

## Validation limitation
Never report CI PASS unless jobs actually execute. Full recovery -> gofmt -> vet -> test -> race remains required on a working complete checkout/runner. Entry 026 adds a release builder that enforces this Gate before it can package a release. During the connector-only repair session, private GitHub bootstrap bytes could be read only through the GitHub connector and not cloned/materialized as a complete checkout, so focused production compile/behavior/race harnesses plus deterministic migration/hash-chain verification are the available evidence until the repaired tree reaches a working runner.

## Open physical-kernel work after Entry 026
1. Run the newly enforced complete `restore_source -> gofmt -> vet -> test -> race -> body build -> FSCK -> daemon health/persist smoke` release Gate on a runner that can materialize the private repository; do not call CI PASS before this executes.
2. Replay-receipt ACK/GC remains intentionally bounded/backpressured. Add GC only if a proof/test shows restart replay fencing cannot be weakened.
3. Add a separate transfer-outcome reconciliation Memory only if a real ambiguous post-rename case remains after journal-aware verification; do not invent a second journal pre-emptively.
4. Real OpenCL device execution must be acceptance-tested on a Linux AMD64 host with an OpenCL device. CPU/GPU dispatch logic and CPU fallback are race-tested without claiming unavailable hardware execution.

## Development history
- Entries 001-008: recovery/frozen architecture; durable Mesh spool, negative ACK + target-first transfer, Sovereign replay fencing, quotas/rollback, removal of cognition-shaped execution APIs, in-body dual-slot mutation journal, incremental `persistAll()` with ACK-critical barriers retained.
- Entries 009-014: Memory-owned Experience ledger; repeated Experience -> candidate Structure; independent Reality Validation; Growth state inside `__memoryai.growth.core`; executable `Program []Op`; VM Reality -> PredictionError -> Experience.
- Entries 015-019: evidence-driven Structure revision/recombination/multi-generation Growth; grounded HTTP/TCP/TLS Reality with at-most-once receipt fence; factual Dynamic Belief without confidence/reward/ranking.
- Entry 020 (`5541088050acebfbf8e572f9023064a36649cfc3`): real Experience Context discovers branch discriminants; 2 formation facts + third independent Reality witness validate branches; parent retires only after all branches validate; contradiction can mark split contested.
- Entry 021 (`fd181a313a666aa52e23c1e9632e1f4c7c359eaa`): exact recursive Context activation resolves one validated leaf; missing/ambiguous Context refuses rather than guesses; VM Reality returns to leaf Experience/Belief; descendants receive fresh grounded action identity.
- Entry 022 (`4c362da0ccaaed1652df511092802ab9ae804e07`): Memory-authored `grounded_trial_series_id` creates deterministic Action Instances linked by Reality Experience; prepared/result-persisted/feedback-committed fencing preserves at-most-once restart behavior; PredictionError does not control continuation.
- Entry 023 (`a226c418ae47bebb459e1f1b2a1b5165814a0cab`): Sovereign-authorized transient direct reads for Experience/Belief/Context Split/Action Instance; no requester cache/upsert/import; disconnect means forgotten; provenance carries origin/backing revision/grant/read time/payload digest.
- Entry 024 (`ccb26329ee086473ffbce01e2aba144a4a90314b`): validated Memory Structure controls remote-Evidence intake; raw remote payload remains transient; capability mapping is physical-only.
- Entry 025 (`dc0c6a4e4406358c9fe32cc5e7b5d005a2584415`): one Memory Program receives ordered multi-node Evidence in one transient VM episode; Kernel does no voting/ranking; dead `routeCognition()` was physically removed.

### Entry 026 — architecture closeout of the 14-point audit
- Base HEAD: `dc0c6a4e4406358c9fe32cc5e7b5d005a2584415`.
- **Cognitive ownership:** every historical `Kernel/src/memory_growth_*` implementation/test is removed from the default production package and retained byte-for-byte under `legacy/cognitive/` only. Production daemon no longer calls RemoteEvidence/GroundedAction/ContextBranching/Growth Go cycles or a Go Context resolver. Completed user activity emits one physical `memory.activity` event; the executable Memory lifecycle decides what activates next.
- **Current Memory seed:** production cognition is deterministically rebuilt from immutable v27 provenance through the accepted v28 migration chain and new v29 ownership migration. `memory.lifecycle.dispatch.parent` owns the activity lifecycle. Drive is restored to exactly Curiosity + Conflict + Uncertainty + Goal + Novelty; PredictionError remains evidence, not a sixth Drive factor.
- **Growth-state migration:** legacy `__memoryai.growth.core` is a one-way compatibility input only. Startup decomposes Experience, order links, validation facts, candidates, Structures and validated-candidate facts into discrete ordinary Memories, persists the new Memories, then deletes the monolithic record. The migration marker contains only scalar format/count metadata.
- **No Mesh sidecars:** deferred proposals and Sovereign proposal replay receipts are ordinary Memories. Historical `Memory.mesh-journal.*.json` and `Memory.mesh-proposal-replay.*.json` are accepted only by one-way migration; active runtime never writes them.
- **Sovereign durability:** node directory and shared-authorization records are ordinary Memories and are recovered on restart. Side-effect Mesh grants persist a consumed-grant Memory fence before external execution, so replay denial survives process restart. Read-only `shared_fetch` remains transient.
- **Remote Memory semantics:** remote Evidence is a generic transient serialized Memory envelope. Kernel no longer imports/decodes Experience/Belief/Context/Action cognitive types. Every read remains freshly Sovereign-authorized; requester-side persistent cache/import is absent.
- **Journal-aware ACK/move:** target verification reads the physical logical state `base indexed store + newest valid in-body mutation-journal slot`. Target commits and source deletes use incremental persistence; old ACK-critical full-body barriers are removed only after this verification path exists. Base-fingerprint mismatch fails closed; delete/upsert overlays are honored.
- **Automatic expansion:** creating a new local shard requires >=5 GiB free on the current Memory path. `MEMORYAI_SHARD_MIN_FREE_BYTES` may raise this floor but cannot lower it.
- **CPU/GPU physical acceleration:** Linux+cgo dynamically loads OpenCL at runtime for large Dot batches, can run CPU and GPU portions concurrently, and recomputes via CPU on any GPU init/dispatch failure. Non-Linux or `CGO_ENABLED=0` builds use the CPU stub. No semantic lanes, ranking or cognitive policy enter the accelerator.
- **Deterministic body/release engineering:** `tools/build_memory_body.py` builds `memoryai-body-v2` with indexed records, semantic + exact physical-feature postings, verified section hashes and the 65,535-byte in-body dual-slot journal reserve. Two builds from the same seed are byte-identical in the local deterministic smoke. `tools/build_release.py` enforces restore/audit/gofmt/vet/test/race, builds Linux AMD64 Kernel + universal `data/Memory.mem`, detects the real Kernel CLI, requires FSCK + daemon health + persist smoke, then generates `start.sh`, `stop.sh`, checksums and release manifest.
- **CI/Gates:** all six architecture workflows are rewritten for the current physical naming/ownership model; obsolete cognition-shaped route/concurrency and sidecar assertions are removed. `tools/architecture_audit.py` additionally rejects any production Growth filename or old Go cognitive-growth symbol, non-generic Remote Evidence, active Mesh sidecar, sub-5-GiB expansion, non-journal-aware remote verification, or a release builder that can skip race.
- Focused fresh evidence completed before commit: architecture audit PASS; Mesh Memory-native journal/replay/Sovereign recovery ordinary+race PASS; daemon ordinary+race PASS; generic Evidence ordinary+race PASS; durable grant replay ordinary+race PASS; legacy Growth decomposition ordinary+race PASS; remote journal-aware verification ordinary+race PASS; GPU ordinary+race+CGO-disabled PASS; deterministic body builder produced two byte-identical files and verified all internal hashes/indexes/journal reserve; v27→v28.7 migration SHA contracts form one continuous chain and v29 accepts exactly the v28.7 terminal SHA.
- Not falsely claimed: complete private-repo recovery/package/race CI PASS or real OpenCL-device execution. The committed release builder makes those mandatory before a commercial package is emitted.
- Final Git assembly caught one stale `mesh_proposal_replay_runtime_test.go` callsite left on the pre-receipt-revision API. Production code was already on the durable `receiptRevision` boundary; only the historical test signature was stale. Test callsites were corrected to pass the Engine and durable receipt revision, then the Mesh ordinary/race harness and explicit signature-call audit were rerun PASS before tree creation.
