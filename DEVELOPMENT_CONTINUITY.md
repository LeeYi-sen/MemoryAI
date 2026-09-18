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

## Validation status
- Entry 027 completed the enforced native Linux AMD64 `restore_source -> gofmt -> vet -> test -> race -> body build -> FSCK -> daemon health/persist -> package smoke` Gate.
- Do not report a later full release Gate unless that complete chain is executed again after the later change.
- Focused local harnesses may validate a narrow physical boundary, but their scope must be stated explicitly and never be reported as a full repository/release Gate.

## Current development mode after Entry 028
- Development repository directly tracks canonical source + `Kernel/current-required-structures.json` + `data/Memory.mem`.
- Do not run `go build`, produce Kernel binaries or run release packaging during the current development phase unless the user explicitly re-enables release work.
- Direct runtime tests (`go test`, including targeted race tests when useful), architecture audits, source-format checks and `Memory.mem` verification remain required.

## Confirmed architecture drift / unfinished work from Entry 029 audit
1. Daemon has no autonomous idle ticker; Memory-owned curiosity/frontier/expansion/lifecycle/mesh/deadline programs only run when an idle event is explicitly fired.
2. External evidence/action requests are created as `io.external.request.pending`, but no production physical executor consumes them, calls the configured Source adapter, marks them done and emits `external.response` / `action.response`.
3. SELF_ONLY exists structurally, but ASSISTED_LEARNING is only implicit policy; there is no explicit mode Memory and no working autonomous Ollama/external-source request loop.
4. Current Memory seed calls three missing/obsolete Kernel opcodes: `cognition_concurrency_set`, `cognition_stats`, `mesh_route_cognition`; current physical replacements are not migrated into Memory yet.
5. v31 semantic Memories are disconnected from the live event graph (`experience.raw`, `semantic.ground.observed`, `semantic.relation.observed`, `interaction.raw`, `semantic.answer.request` have no internal producer).
6. Four semantic Memories call `emit_event` without declaring `event.emit`, so Kernel capability checks would reject them even if activated.
7. Natural-language reply path is incomplete: daemon input returns a Frame and the semantic expression path has no live request producer/presentation renderer.
8. Initial current Memory seed contains no CJK grounding/example content, so the prior requirement that initial Memory recognize Chinese is not yet satisfied.
9. Memory-owned executable mutation exists, but executable Structure recombination/fission is absent from current Memory and remains only in excluded `legacy/cognitive` Go history.
10. Physical `space_merge` exists, but no Memory-owned body-fusion decision/execution structure uses it.
11. Mesh global sync only observes shared-directory visibility; it does not fetch transient remote Memory and emit `mesh.memory.integrated`, so global cognitive integration is not closed.
12. Mesh digest conflict can return status `conflict`, but no physical path emits `mesh.version.conflict` into the Memory-owned reconciliation frontier.
13. Proposal replay receipts still have no ACK/GC lifecycle; completed receipts accumulate until the bounded replay ledger applies backpressure.
14. Executable Memory has Trigger/State/Budget/Program/Revision/Parents, but input pattern/output effect/success history/mutation variants are not yet standardized first-class structure fields.
15. Activation callers still use local variable name `topK` even though the runtime ABI is physical `pageCap`; this is vocabulary drift, not current ranking behavior.

## Open physical-kernel work after Entry 028
1. Replay-receipt ACK/GC remains intentionally bounded/backpressured. Add GC only if a proof/test shows restart replay fencing cannot be weakened.
2. Add a separate transfer-outcome reconciliation Memory only if a real ambiguous post-rename case remains after journal-aware verification; do not invent a second journal pre-emptively.
3. Real OpenCL device execution must be acceptance-tested on a Linux AMD64 host with an OpenCL device. CPU/GPU dispatch logic and CPU fallback are race-tested without claiming unavailable hardware execution.

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

### Entry 027 — complete private-repo release Gate and repair recovery/release contracts
- Base HEAD: `7684db168e0e1d9e279c1c3ccd0b2166e59e84b7` (`Entry 026: return cognition ownership to Memory`). Development was performed from a real local clone at `/Users/kama/MyCode/MemoryAI` after GitHub authentication and `main` synchronization.
- **Recovery overlay boundary:** `kernel_remote_body_txn_overlay.py` no longer assumes `close()` immediately follows `unmountSpace()`. It isolates the real top-level function boundary, so the Entry 026 source-restore chain works against the complete v27 bootstrap plus all preceding overlays.
- **Remote durability assertions:** journal-aware remote PUT/DELETE paths still require `persistEngineIncremental(sp)`, while the one legitimate local `unmountSpace` full-body durability barrier remains allowed. Audit/generation checks now scope forbidden full-body persistence to the remote handler and require exactly the permitted local occurrence after the mutation-journal overlay.
- **Deterministic Memory seed contract:** the reproducible v28 language migration deterministically produces SHA-256 `9223866b6921c4eefa486e3b34597fabe0b8c823eb79a04a465e441b50128b80` on both Python 3.14.2 and system Python 3.9.6. The v29 input contract and architecture audit now use that reproducible SHA instead of the stale historical `eb82f503...` value. Final v29 seed SHA remains `49c002cdfb99a6ae49213634be348fe83de126ad31b2a75fdcdeaa5245fa29b2` with 99 Memories.
- **Fabric fail-closed semantics:** `resolve()` now propagates non-EOF exact-ID errors instead of swallowing duplicate physical Fabric identity and degrading to tag lookup/not-found. Duplicate identity remains fail-closed for both read and write paths. Historical VM tests were updated to declare the already-required `memory.write` capability rather than weakening Kernel security.
- **Test/runtime compile repairs exposed by the first complete Gate:** fixed the speculative test nil-map fixture, `IndexedStore.Close()` no-return misuse, stale unused imports, unsafe `sync.Map` value-copy test isolation, generated startup formatting, and one tracked activation-runtime indentation drift. These were Gate blockers, not cognitive-policy additions.
- **Release control-path contract:** daemon `client` is a native Kernel control command and must be `Kernel client <socket> ...`; it intentionally does not load `Memory.mem`. `tools/build_release.py`, generated `start.sh`, generated `stop.sh`, and a new regression test now enforce this. The previous `Kernel Memory.mem client ...` form was the cause of the Entry 026 daemon-smoke failure.
- **New regression coverage:** `tools/test_kernel_remote_body_txn_overlay.py` exercises the real bootstrap + preceding overlay chain for unmount isolation, remote incremental durability, and the single allowed local full-body barrier; `tools/test_memory_seed_chain.py` locks the reproducible v28→v29 SHA chain; `tools/test_build_release_cli.py` locks native daemon-client invocation.
- **Complete Linux AMD64 release Gate actually executed:** an independent Colima `x86_64` QEMU-system VM was created so validation did not depend on unstable qemu-user binfmt. Inside native `Linux x86_64` with Go 1.22.12, the release builder completed `restore_source -> restore_source --check -> architecture audit -> gofmt -> go vet -> go test -> go test -race -> architecture audit -> Linux AMD64 go build -> current Memory seed -> Memory.mem build -> FSCK -> daemon health -> daemon persist -> release assembly` with exit code 0. `go test -race` passed; Kernel SHA-256 is `35f915938cb8600792f228ce267f2ede73eb0c185daf738de5000a00d338ede2`; Memory.mem SHA-256 is `e75e466dc26e6e0d3e89eaec4b95b8353b05763d79f2083ee09181e21d5487e7`.
- **Final package smoke:** the emitted release directory was copied onto the x86_64 VM native ext4 filesystem and independently ran `FSCK -> start.sh -> Kernel client health -> stop.sh`; output ended `RELEASE_PACKAGE_SMOKE_PASS`. Health reported `role=core`, `memory_count=99`, `kernel_policy=physical-only`, physical concurrency 4, OpenCL dynamic backend unavailable with CPU fallback. Unix sockets cannot be created inside the Mac→VM SSHFS mount, so package daemon smoke was correctly performed on native Linux filesystem rather than treating that mount limitation as a product failure.
- **Generated release artifact:** preserved outside the Git worktree at `/Users/kama/MyCode/MemoryAI-release/Entry-027-linux-amd64`. It is intentionally not committed because it is a generated package containing binaries, restored generated `kernel.go`, and `Memory.mem`; the source builder/tests/checksums are the reproducible committed contract.
- **Remaining physical acceptance item:** real OpenCL-device execution is still not claimed. The available x86_64 VM has no OpenCL device; CPU fallback, GPU dispatch boundaries, ordinary/race tests and dynamic-loader behavior are validated, but an actual Linux AMD64 OpenCL host is still required for hardware execution acceptance.

### Entry 028 — remove cognitive-shaped Activation ABI
- Base HEAD: `55bde5513a2fac2fc88f6701a20ac645c832fcbe` (`Entry 027: complete Linux AMD64 release gate`).
- **Physical Activation ABI:** `ActivationCandidate` no longer exposes `Score`; Kernel returns only physical Memory identity plus exact physical feature-hit metadata. `Activate` now uses a `pageCap` transport/resource boundary and stable Memory-ID order, with no score, relevance or cognitive rank contract.
- **Configuration vocabulary:** `MEMORYAI_ACTIVATION_TOPK` is intentionally ignored by the new runtime. Physical paging uses `MEMORYAI_ACTIVATION_PAGE_CAP`; runtime info exposes `default_page_cap` and `physical_page_cap_only` instead of Top-K/cognitive-ranking shaped keys.
- **Qualification contract:** full-Fabric qualification now compares exact candidate counts, exact stable physical page order and exact feature-hit counts. Historical `ExactTopK` and `MaxScoreDiff` fields are removed.
- **Regression Gate:** new `activation_boundary_test.go` locks the score-free JSON wire shape, physical page-cap metadata and non-effect of the legacy Top-K environment variable. `tools/architecture_audit.py` scans every production `activation*.go` and rejects `Score`, `topK`, old env/key names and old qualification symbols.
- **Local sandbox validation:** exact touched production sources were compiled with Go 1.23.2 Linux AMD64 in an isolated physical-Fabric harness; `go vet` PASS; `go test -race -count=30` PASS; architecture-audit Python syntax PASS; positive activation-boundary audit PASS; an injected `Score` negative probe was rejected as expected.
- **Validation scope:** this turn does not claim a new full repository/release Gate because the ChatGPT sandbox could not directly clone GitHub. The last complete release Gate remains Entry 027. Entry 028 changes only the Activation physical ABI/qualification/audit boundary; real OpenCL hardware acceptance remains outstanding.

### Entry 029 — direct-source development baseline and architecture drift audit
- Base HEAD: `dc2c3d64b75c3f7a3330a0cc52e02e19fe9bca6d` (`Entry 028: remove cognitive-shaped Activation ABI`).
- Repository switched from gzip/base64 bootstrap reconstruction to direct canonical Git source: tracked `Kernel/src/kernel.go`, tracked current 99-Memory seed `Kernel/current-required-structures.json`, and tracked `data/Memory.mem`.
- Removed v27 `.gz.b64.part*` payloads, bootstrap manifests, source reconstruction/overlay scripts, obsolete overlay tests, and the development release builder. `.gitignore` now rejects archives, split payloads and compiled binaries while permitting `data/Memory.mem`.
- Current Memory body SHA-256 remains `e75e466dc26e6e0d3e89eaec4b95b8353b05763d79f2083ee09181e21d5487e7`; current seed SHA-256 remains `49c002cdfb99a6ae49213634be348fe83de126ad31b2a75fdcdeaa5245fa29b2` with 99 Memories.
- Development workflow explicitly changed to direct tests/no release compilation: no `go build`, no Kernel binary generation, no release packaging. CI retains direct `go test` / targeted race tests and architecture/source/Memory validation.
- Fresh direct validation during this entry: repository-layout tests PASS; direct seed contract PASS; `Memory.mem --verify-only` PASS; architecture audit PASS; gofmt clean; full `GO111MODULE=off go test ./Kernel/src` PASS. No new release Gate is claimed.
- Architecture audit identified the open drift list recorded above; these findings are not silently treated as fixed. Subsequent development must close them from current Git `main`, with Memory-owned cognition preserved.
### Entry 030 — live architecture repair tracker
- Base HEAD: `35092d85314afe2bccabde28c314ef04165608ba` (`Entry 029: switch to direct-source development baseline`).
- Added `docs/ARCHITECTURE_REPAIR_TRACKER.md` as the live source-of-truth for the 26 confirmed architecture drift/unfinished items from the Entry 029 audit.
- Every repair commit must update the tracker status/commit/verification fields and this continuity file atomically.
- Repair order is dependency-driven: permanent ABI/capability gates first, then autonomous runtime, external evidence, semantic reply loop, self-growth/body fusion, Sovereign Mesh, and remaining Kernel boundary cleanup.
- Development remains direct-test only: no `go build`, Kernel binary generation or release packaging.

### Entry 031 — enforce Memory/Kernel ABI and capability audit
- Base HEAD: `de55f11e322f7a13a78340d0b0acfc9131a3ed47` (Entry 030 tracker baseline).
- AR-023 DONE: `tools/architecture_audit.py` now parses the production `execPrimitive` switch and fails if any current Memory Program references an unsupported opcode.
- AR-024 DONE: the audit parses Kernel `privilegedOpCapability` and fails when a Memory Program uses a privileged opcode without the matching declared capability (or `kernel.admin`).
- TDD evidence: both regression tests first failed because the old audit returned success; after the Gate implementation both pass. The live audit now intentionally fails on the still-open obsolete-opcode drift, proving the Gate is active.
- No `go build` or release packaging was run.

### Entry 032 — repair current Memory ABI/schema/capability drift
- Base HEAD: `f340cb03fa303e52b5215b1645837ca0fea147d6` (Entry 031 ABI Gates).
- AR-002 DONE: migrated `cognition_concurrency_set` -> `physical_execution_concurrency_set`, `mesh_route_cognition` -> `mesh_route_execution`, and `cognition_stats` -> `physical_runtime_stats`. Lifecycle Memory now consumes generic `event_handler_runs` / `event_handler_failures`; Kernel supplies only physical counters.
- AR-007 DONE: every current Memory Program that emits an event now declares `event.emit`; the new full-seed capability Gate passes.
- AR-013 DONE: Kernel `Memory` now round-trips `executable`, `input_pattern`, and `output_effect`; structural equality includes these executable contract fields.
- AR-022 DONE: remaining CLI/daemon `topK` caller vocabulary was renamed to `pageCap`; architecture audit now scans callers as well as activation runtime sources.
- Canonical seed SHA-256 is now `ab90e81cf7ca55cfa404a350b5f386aa231c3bab291b39a8bdc99dc73aa4647e`; regenerated `data/Memory.mem` SHA-256 is `c569121ae4835d313e2e66975db11f93c1edade4c2189b99f04a313170805e78`, still 99 Memories.
- Validation: seed contract PASS; architecture audit PASS (132 Kernel opcodes / 99 Memory Programs audited); Memory.mem verify PASS; full direct `GO111MODULE=off go test ./Kernel/src` PASS; focused race for schema/physical runtime boundary PASS; no `go build` or release packaging.

### Entry 033 — autonomous idle and independent event concurrency
- Base HEAD: `4ca0ce1` (`Entry 032: repair Memory ABI and schema drift`).
- AR-001: daemon now runs a configurable physical-only idle ticker (`MEMORYAI_IDLE_INTERVAL_MS`, default 1s). It emits only `event:idle`; all meaning, goals and cognition remain Memory-owned.
- AR-005: top-level independent Event handlers execute concurrently through the existing speculative transaction scheduler, bounded by physical execution concurrency. Each handler gets the same physical event frame; ephemeral Frame deltas are merged deterministically in stable Memory-ID order.
- Handler failure is isolated: one failing Memory no longer suppresses unrelated handlers. Errors are aggregated after all matched handlers finish. Nested speculative/canonical event dispatch remains serial to avoid re-entering commit lanes.
- Fresh validation: RED tests first confirmed no autonomous idle, sibling suppression and speculative peak=1. GREEN focused tests PASS; focused `go test -race` PASS; repository layout, seed contract, architecture audit, Memory.mem verification and full direct `GO111MODULE=off go test ./Kernel/src` PASS. No `go build` or release packaging executed.

### Entry 034 — close assisted-learning physical I/O loop
- Base HEAD: `3d7404306e28601bbce074d417ca2e599b0eb6f0` (`Entry 033: add autonomous idle and event concurrency`).
- AR-004: physical Source adapter Memory now exposes `source.adapter.enabled` and optional `source.adapter.action` operational tags while retaining the generic `kernel.physical.source-adapter` identity. Default enabled state is explicit.
- AR-003: daemon physical idle loop consumes opaque `io.external.request.pending` envelopes. Lifecycle is persisted inside Memory.mem as pending -> executing -> response-ready+done -> done. External side effects are never automatically replayed from an ambiguous executing state; response-ready state can be redelivered without re-running the source.
- Memory authors each request's opaque `response_event`; Kernel does not decide evidence/action semantics. Request scalar state is forwarded as event metadata. Adapter request templates support per-request `{{input}}` / `{{input_json}}`.
- AR-028: physical Event variables are available both under their original names and `__event.<key>`, repairing the existing Memory event contract without interpreting variable meaning.
- AR-027: added Memory-owned `policy.learning.mode` (default `SELF_ONLY`) plus `learning.mode.control.parent`; `research.external.parent` enters external Source selection only under `ASSISTED_LEARNING`. Kernel remains unaware of mode semantics.
- Current canonical seed: 101 Memories, SHA-256 `80400b01cc2c78cc1a8f1cc64f7fe8097cc00d0b37df8ef2d82af2978f74c251`; Memory.mem SHA-256 `960c99d7273159bac45411d40dda499754349b5924d36ad183bd783819df2f22`.
- Fresh validation: learning-mode contract PASS; Memory-Kernel architecture audit PASS; Memory.mem verify PASS; source-selector/event-namespace/external-executor focused tests PASS; focused `go test -race` PASS; full direct `GO111MODULE=off go test ./Kernel/src` PASS. No `go build` or release packaging executed.

### Entry 035 — close Memory-owned semantic and natural-language loop
- Base HEAD: `951bbd3740780728a4814d0120f00f7358ce847b` (`Entry 034: close assisted-learning physical I/O loop`).
- AR-006 DONE: added live Memory bridges from raw Experience into `experience.raw`, `interaction.raw`, grounding observation, semantic relation observation and semantic answer request. Kernel does not classify language or own semantic mappings.
- AR-008 DONE: semantic answer requests flow through the existing semantic expression frame, then `expression.surface.render.parent` resolves a stable learned Surface mapping, emits `reply.ready`, and returns `reply_text` on the live daemon Frame.
- AR-009 DONE: initial Chinese grounding for the self-identity query is ordinary evolvable Memory (`Surface -> Concept/query -> semantic relation -> Surface reply`). Chinese literals and identity wording are absent from production Go and can be replaced by later Memory evidence.
- Physical event correctness: nested event transport metadata is now stack-scoped. A child event cannot leak `__event` / `__subject` into a sibling handler selected by the parent physical event. The regression was RED before the generic event-runtime repair and GREEN afterward.
- Current canonical seed: 119 Memories, SHA-256 `60232e1ccb58506e89a29687135f6165a2f504e07e25f305dce00f98ab7d1ac8`; regenerated `data/Memory.mem` SHA-256 `f9712fdbe819a033ade0fb42cf341087600937e560709a3662e932112d2f39c2`.
- Fresh validation before commit: semantic graph contract PASS; live `你是谁 -> 我是MemoryAI` event-chain test PASS and x20 PASS; nested-event context focused race x20 PASS; Python regression suite 14 tests PASS; `go vet` PASS; full direct Go test PASS; architecture audit PASS (132 Kernel opcodes / 119 Memory Programs); Memory.mem verify PASS; `git diff --check` PASS. Full semantic `-race` is not claimed because race instrumentation exceeds the existing 300ms Memory physical execution budget; the production budget was intentionally not weakened. No `go build` or release packaging executed.

### Entry 036 — close self-growth and Memory-body evolution
- Base HEAD: `586aee345e639d5a5495010d099cb33d5e06c327` (`Entry 035: close semantic live reply loop`).
- AR-010 DONE: added Memory-owned executable recombination and fission. `evolution.recombine.parent` copies one executable parent and splices a bounded Program fragment from another via generic `program_insert_from`; `evolution.fission.parent` derives two executable children through generic Program deletion. Parent variant lineage is recorded by Memory.
- AR-014 DONE: executable Memory now has first-class `success_history`, `failure_history`, and `mutation_variants` fields. The generic `memory_history_append` primitive only appends Memory-authored references; it does not classify success or choose variants. `memory_copy` starts child histories empty so evidence is not inherited as if newly observed.
- AR-015 DONE: `execution.outcome.record.parent` converts explicit `structure.feedback` gain/cost evidence into persistent success/failure outcome Memories under mutable `policy.execution.outcome`; `RuntimeExecCount` remains physical telemetry and is not used as cognitive success evidence.
- AR-011 DONE: explicit Memory-body fusion is a four-stage Memory chain: discover -> evaluate physical reachability -> decide from mutable Memory policy -> execute. The Kernel does not initiate or approve fusion.
- AR-012 DONE: `space_merge` is now physical-only. Unique identities are copied, exact duplicates are counted, and divergent same-ID records are left untouched while opaque source/target JSON + digests are returned as conflict evidence for Memory-owned reconciliation. Kernel no longer calls semantic reconciliation from `mergeSpace` and no longer invents conflict branches there.
- Current canonical seed: 128 Memories, SHA-256 `f96050776140416a3cc4f3852509c64b6afd48649ec289c24752a8eab1c543b3`; regenerated `data/Memory.mem` SHA-256 `e64ce563d0c03f837b3dc59bca9175bda991eae34cb2fe81d3d016eb2bdf4371`.
- Fresh validation before commit: self-growth contract PASS; recombination/fission integration PASS; Memory-owned success/failure history PASS; live Body Fusion integration PASS; divergent-ID merge preservation PASS; Python regression suite 17 tests PASS; `go vet` PASS; full direct Go test PASS; focused `go test -race -count=10` PASS; architecture audit PASS (136 Kernel opcodes / 128 Memory Programs); Memory.mem verify PASS. No `go build` or release packaging executed.

### Entry 037 — close Sovereign Memory Mesh integration and replay lifecycle
- Base HEAD: `beb554b29441c95c8760f740fe5dc81127b6303d` (`Entry 036: close self-growth and body fusion`).
- AR-017 DONE: current Memory actively uses current Mesh execution primitives. `mesh.cognition.dispatch.parent` retains physical `mesh_route_execution`; `mesh.global.sync.parent` uses `mesh_shared_fetch`; `mesh.remote.execute.parent` provides explicit owner-side `mesh_structure_run` without importing the executable Memory.
- AR-016 DONE: global sync performs Sovereign-authorized `shared_search -> shared_fetch` and emits `mesh.memory.integrated` with transient remote JSON/provenance only. The integration Memory never imports, caches, tags or mutates remote Memory locally. `soft_fail=1` maps unreachable origins to `forgotten` without aborting autonomous idle.
- AR-018 DONE: physical `shared_reconcile` digest mismatch becomes an ordinary `cog.mesh.version.conflict` Memory and `mesh.version.conflict` event; the existing Memory consumer creates a research frontier. Kernel/authority reports mismatch but never chooses a winner.
- AR-019 DONE: proposals are durably spooled before transport; Sovereign returns `receipt_id`; the node atomically converts proposal to `shared_proposal_ack` before ACK transport. Sovereign persists `acked_through_revision` before GC of a `done` receipt. Executing receipt GC, future ACK, identity mismatch, and missing receipt without ACK fence fail closed; duplicate ACK after GC is idempotent.
- AR-025 DONE: `memory_copy` and all local VM mutation paths that advance Revision clear stale `CapabilitySig`; signed export regenerates the current signature.
- AR-026 DONE: `memoryJSONDigest` excludes `CapabilitySig` and `RuntimeExecCount` as physical metadata while structural/semantic state still affects identity.
- AR-029 DONE: proposal tags were present only in the Frame list while Memory policy consumed `proposal_tags`. The authority boundary now preserves exact tags for replay identity and exposes deterministic `proposal_tags` to Memory policy.
- Current canonical seed: 129 Memories, SHA-256 `5f8538b951029890d85b025572149fde0a5b7708bcbbd524b17a0979048c7bc4`; `data/Memory.mem` SHA-256 `88137fdca8562268a7c2a3e9c5de1ba49e7440971760810573d439b62655942b`.
- Fresh validation before commit: Mesh Memory contract 3 tests PASS; transient remote fetch/no-cache PASS; owner-side remote execution PASS; digest conflict -> Memory frontier PASS; ACK/GC restart/idempotency/executing/future/missing-fence tests PASS; durable proposal->ACK restart test PASS; real Sovereign receipt->ACK->GC PASS; stale CapabilitySig/digest regressions PASS; Python regression suite 20 tests PASS; `go vet` PASS; full direct Go test PASS; focused `go test -race -count=10` PASS; architecture audit PASS (136 Kernel opcodes / 129 Memory Programs); Memory.mem verify PASS. No `go build` or release packaging executed.

### Entry 038 — close remaining Kernel boundary cleanup
- Base HEAD: `67f11999a0f7b269cde300e41383d684401070ef` (`Entry 037: close Sovereign Mesh integration`).
- AR-020 DONE: Kernel execution trace is now unconditional physical telemetry. Production Kernel no longer inspects `cog.surface.*` or `cog.input.*` to decide whether an execution is trace-visible. A permanent architecture-audit gate rejects those cognition-shaped namespaces in production Kernel source.
- AR-021 DONE: active remote-storage metadata now uses `physical.storage.remote.endpoint` and `physical.storage.replica.of.<memory-id>`. Production Kernel no longer consumes `cog.storage.*`; the only remaining legacy strings are confined to one-way migration code.
- Compatibility migration: startup migration rewrites historical `cog.storage.remote.endpoint` and `cog.storage.replica.of.*` tags to the physical namespace, clears stale capability signatures, advances revision, persists into `Memory.mem`, and survives restart. Existing remote descriptors therefore remain discoverable after upgrade.
- Permanent gates: architecture audit now rejects `cog.surface.*` / `cog.input.*` cognition interpretation anywhere in production Kernel and rejects active `cog.storage.*` usage outside the legacy migration unit.
- Canonical seed/body unchanged from Entry 037: 129 Memories; seed SHA-256 `5f8538b951029890d85b025572149fde0a5b7708bcbbd524b17a0979048c7bc4`; `data/Memory.mem` SHA-256 `88137fdca8562268a7c2a3e9c5de1ba49e7440971760810573d439b62655942b`.
- Fresh validation before commit: new architecture-audit boundary regressions RED then GREEN; tag-agnostic trace runtime PASS; legacy storage namespace migration + restart PASS; Python regression suite 22 tests PASS; `go vet` PASS; full direct Go test PASS; focused `go test -race -count=20` PASS; architecture audit PASS; Memory.mem verify PASS. No `go build` or release packaging executed.
- Frozen architecture repair tracker status after this entry: AR-001 through AR-029 are all DONE; next work starts from a fresh post-repair audit rather than reusing the closed defect list.

### Entry 039 — post-repair physical boundary hardening
- Base HEAD: `b62477b488e282d775f6f70c9f668ca8c5f0a573` (`Entry 038: close Kernel boundary cleanup`).
- Fresh post-repair audit created `docs/POST_REPAIR_AUDIT_TRACKER.md`; PR-001 through PR-014 are all DONE.
- Memory-owned expansion: Kernel no longer creates a storage shard from `placeRuntimeMemory` when capacity is exhausted. Memory must explicitly create/select storage; Kernel retains only the physical free-space fence, never lower than 5 GiB.
- Mesh durability cleanup: removed the unused non-durable `meshRuntime.flushJournal` alternate path and permanently reject its reintroduction.
- Remote identity integrity: direct registered reads pin requested Memory ID, BodyID and ABI. Reachable replicas with the same structural digest collapse physically; divergent same-ID replicas fail closed instead of first-wins.
- Remote storage security: mem-node request/response envelopes are HMAC authenticated. Loopback may use authenticated plaintext; non-loopback listeners require TLS. Transport-key/TLS configuration and invariants are documented in `docs/REMOTE_STORAGE_SECURITY.md`.
- Remote mutation safety: `put` never implicitly creates a body, same-ID divergence remains conflict evidence, bounded/tag-safe upsert is used, and the Kernel never invents a branch ID. Replace/delete now use structural-digest compare-and-swap fences; replace revisions must advance monotonically.
- Structural digest: persistence, structure exchange and mem-node digest now share `memoryJSONDigest`, excluding `RuntimeExecCount` and `CapabilitySig` physical metadata.
- Transfer conflict: durable `space_copy/space_move` now fail closed on divergent same-ID targets; obsolete semantic branch creation helpers were removed.
- Signature integrity: external-request lifecycle and structure-sync revision bumps clear stale `CapabilitySig`; architecture audit now rejects direct `Revision++` without nearby signature invalidation and rejects structure-sync revision bump without invalidation.
- Single-target resolution: exact ID remains direct; a tag with one identity resolves; a tag with multiple identities is ambiguous and fails closed. Memory must use `tag_list` and select an explicit ID. `resolveExecutable` preserves the ambiguity error instead of rewriting it as not-found.
- Lineage integrity: `memory_new` now resolves every explicitly declared parent before child creation; missing, forgotten, conflicted or integrity-failed parents abort creation, preventing dangling lineage.
- Canonical Memory remains unchanged: 129 Memories; seed SHA-256 `5f8538b951029890d85b025572149fde0a5b7708bcbbd524b17a0979048c7bc4`; `data/Memory.mem` SHA-256 `88137fdca8562268a7c2a3e9c5de1ba49e7440971760810573d439b62655942b`.
- Final validation: architecture regression suite 15/15 PASS; live architecture audit PASS; Python suite 32 tests PASS; `go vet` PASS; full direct Go suite PASS; focused post-repair `go test -race -count=20` PASS; `git diff --check` PASS; Memory.mem verify PASS. No `go build` or release packaging executed.

### Entry 040 — physical identity, sandbox, Mesh node identity and Memory body integrity
- Base HEAD: `bef2a76c983d4c2707b1eda3ee068d9572c6b70c` (`Entry 039: harden post-repair physical boundaries`).
- Continued post-repair audit closed PR-015 through PR-025; `docs/POST_REPAIR_AUDIT_TRACKER.md` now has 25 DONE / 0 OPEN findings.
- Physical identity allocation no longer depends on wall-clock granularity. `nextID()` now uses a process-unique entropy nonce plus an atomic in-process sequence.
- Initial Memory body creation now has durable file semantics: temp cleanup on failure, file sync, atomic rename and parent-directory sync.
- Single-primitive physical I/O is bounded. Raw TCP/TLS exchange has request/response byte ceilings with hard Kernel maxima; source-adapter HTTP refuses oversized request/response bodies rather than silently truncating them; artifact read/write/digest enforce bounded physical bytes and digest streams from disk.
- Artifact workspace and mem-node storage paths now share `physicalSandboxPath()`: the configured root is resolved physically and symlink components below that trusted root are rejected, preventing sandbox/root escape.
- Sovereign Memory Mesh node identity is now cryptographically bound. The shared Mesh HMAC remains membership/channel authentication, while every remote Mesh RPC additionally carries a per-node Ed25519 signature. Ordinary nodes use `MEMORYAI_MESH_NODE_PRIVATE_KEY_B64`; the Sovereign uses its Sovereign Ed25519 private key as its node identity key.
- Sovereign directory records now persist each node public key inside normal Memory state. A node ID with an existing non-empty public key cannot silently rebind to another key. Legacy empty-key records can bind on their first authenticated post-upgrade registration.
- MeshGrant version advanced to v2. Sovereign grants bind operation, Memory ID, origin node ID, origin public key, target node, expiry and nonce. Direct node-to-node requests must prove possession of the private key matching the Sovereign-signed origin public key. Side-effect grants retain durable one-shot replay receipts in `Memory.mem`.
- Memory body input is hardened against oversized metadata, duplicate ZIP entry names, manifest path aliasing, Store sections that escape the physical body file, malformed index cardinality, oversized record/posting slices and out-of-section offsets.
- Physical store index advanced from legacy 24-byte entries to `memoryai-index-v2-sha256` 56-byte entries. Each ID/tag index entry carries SHA-256 for its referenced Memory record/posting payload.
- Normal v2 load verifies genesis + ID index + tag index hashes, then validates individual Memory records and posting payloads lazily on access. It intentionally does not scan every Memory record at startup. Legacy v1 index bodies remain readable only after complete manifest-declared records/taglists/index hash verification and are rewritten to v2 on the next persistence.
- Image version advanced to `28.9.0-memory-fabric-sovereign`; Memory ABI remains `memoryai-memory-abi-v1` because the cognitive Memory schema is unchanged.
- Canonical cognitive seed remains unchanged: 129 Memories; seed SHA-256 `5f8538b951029890d85b025572149fde0a5b7708bcbbd524b17a0979048c7bc4`.
- Canonical `data/Memory.mem` was regenerated only for the new physical index/image format. New SHA-256: `e30987f5febd8e1cd893c6b313725eedfd72c518490ad711cb7991c5178ef411`.
- New security/integrity documentation: `docs/MESH_SECURITY.md` and `docs/MEMORY_BODY_INTEGRITY.md`. Remote storage transport constraints remain documented in `docs/REMOTE_STORAGE_SECURITY.md`.
- Final validation before commit: architecture regression suite 30/30 PASS; live architecture audit PASS; Python suite 47/47 PASS; `go vet` PASS; full direct Go suite PASS; focused post-repair `go test -race -count=20` PASS; `git diff --check` PASS; canonical Memory verify PASS.
- No `go build`, binary release build, archive packaging or release artifact generation was executed.

### Entry 041 — pre-side-effect budgets and bounded daemon/Mesh transport
- Base HEAD: `7e4074f422d039c2af2a4358b4db5eef875b5531` (`Entry 040: harden physical identity and Memory integrity`).
- Continued post-repair audit closed PR-026 through PR-028; `docs/POST_REPAIR_AUDIT_TRACKER.md` now has 28 DONE / 0 OPEN findings.
- ResourceBudget side-effect ordering was corrected. `MaxOps` is checked before the next primitive executes. Potential Memory-write primitives reserve a `MaxMemoryWrites` slot before mutation, and `emit_event` reserves a `MaxEvents` slot before append/dispatch. A primitive/event beyond the quota never performs the side effect and then reports failure afterward.
- Resource scope is per executing Memory. Nested `call` enters the callee budget scope and restores the caller scope on return; caller/callee physical quotas do not leak into each other or become cognitive selection policy.
- CPU, total-allocation and heap-growth fields remain physical runtime sampling gates only. They are not interpreted as reward, fitness, confidence, relevance or goal priority.
- Daemon Unix-socket transport is physically bounded: `MEMORYAI_DAEMON_MAX_BYTES` defaults to 1 MiB with 8 MiB hard max; `MEMORYAI_DAEMON_TIMEOUT_MS` defaults to 15 s with 60 s hard max; `MEMORYAI_DAEMON_MAX_CONCURRENT` defaults to 32 with 256 hard max. Oversized requests fail closed and handler concurrency is capped.
- Daemon responses use a bounded streaming JSON encoder. Oversized response objects are converted to a bounded failure response instead of first materializing/transmitting an unbounded JSON payload.
- Mesh HTTP transport is physically bounded by `MEMORYAI_MESH_MAX_BYTES` (2 MiB default, 16 MiB hard max). Inbound and outbound bodies use overflow-detecting ceilings instead of truncating `LimitReader(2 MiB)` behavior.
- Mesh HTTP server now enforces 5 s read-header timeout, 10 s read timeout, 10 s write timeout, 30 s idle timeout and 32 KiB maximum header bytes. Existing TLS, HMAC membership and Ed25519 node-identity boundaries remain unchanged.
- New documentation: `docs/RUNTIME_RESOURCE_BOUNDARIES.md`.
- Canonical cognitive seed remains 129 Memories with SHA-256 `5f8538b951029890d85b025572149fde0a5b7708bcbbd524b17a0979048c7bc4`.
- Canonical `data/Memory.mem` remains unchanged from Entry 040 with SHA-256 `e30987f5febd8e1cd893c6b313725eedfd72c518490ad711cb7991c5178ef411`; image version remains `28.9.0-memory-fabric-sovereign`; Memory ABI remains `memoryai-memory-abi-v1`.
- Final validation: architecture regression suite 33/33 PASS; live architecture audit PASS; Python suite 50/50 PASS; `go vet` PASS; full direct Go suite PASS; focused resource/transport `go test -race -count=20` PASS; `git diff --check` PASS; canonical Memory verify PASS.
- No `go build`, binary release build, archive packaging or release artifact generation is permitted for this development entry.

### Entry 042 — bounded fan-out and remote storage transport
- Base HEAD: `e830b45163cc0f1736f75c2a38753ed4bb86cae2` (`Entry 041: enforce runtime resource and transport bounds`).
- Continued post-repair audit closed PR-029 through PR-031; `docs/POST_REPAIR_AUDIT_TRACKER.md` now has 31 DONE / 0 OPEN findings.
- Top-level event dispatch no longer creates one goroutine per exact handler match. It uses bounded batches through the existing physical worker pool; temporary goroutines and result storage scale with physical batch/concurrency rather than total handler cardinality.
- Event dispatch preserves stable Memory-ID merge order and introduces no semantic priority/ranking. The hard physical batch ceiling is 256 handlers.
- `call_parallel` now rejects oversized target lists before copying the list, allocating result/status arrays or executing children. `MEMORYAI_PARALLEL_FANOUT_MAX_TARGETS` defaults to 1024 and is hard-capped at 65536.
- Remote storage request/response envelopes now use `MEMORYAI_STORAGE_MAX_BYTES`: 20 MiB default, 32 MiB hard Kernel maximum. The previous fixed truncating 4 MiB decoder path is removed.
- Oversized mem-node responses become a small bounded HMAC-authenticated error envelope rather than silent truncation or unbounded response materialization.
- mem-node connection deadlines are controlled by `MEMORYAI_STORAGE_TIMEOUT_MS`: 30 s default, 120 s hard maximum.
- mem-node admitted handler concurrency is controlled by `MEMORYAI_STORAGE_MAX_CONCURRENT`: 32 default, 256 hard maximum. Excess connections are closed before a handler goroutine is created.
- Storage envelope signing remains exact-byte stable after bounded JSON encoding: the `Encoder.Encode` framing newline is removed before the inner `RawMessage` is signed/embedded, preventing outer JSON normalization from invalidating the HMAC.
- Architecture audit now permanently rejects goroutine-per-handler event fan-out, missing `call_parallel` cardinality ceilings, legacy 4 MiB storage readers, missing mem-node timeout/concurrency boundaries, and bypass of bounded storage response fallback.
- Canonical cognitive seed remains unchanged: 129 Memories; seed SHA-256 `5f8538b951029890d85b025572149fde0a5b7708bcbbd524b17a0979048c7bc4`.
- Canonical `data/Memory.mem` remains unchanged from Entry 040/041 with SHA-256 `e30987f5febd8e1cd893c6b313725eedfd72c518490ad711cb7991c5178ef411`; image version remains `28.9.0-memory-fabric-sovereign`; Memory ABI remains `memoryai-memory-abi-v1`.
- Final validation: architecture regression suite 38/38 PASS; live architecture audit PASS; Python suite 55/55 PASS; `go vet` PASS; full direct Go suite PASS; focused PR-029..031 `go test -race -count=20` PASS; `git diff --check` PASS; canonical Memory verify PASS.
- No `go build`, binary release build, archive packaging or release artifact generation was executed.

### Entry 043 — hard external I/O timeout ceilings
- Base HEAD: `3bf6825c6f6e73e9eb8ace21bf79552caeb435b2` (`Entry 042: bound fanout and remote storage transport`).
- Continued post-repair audit closed PR-032; `docs/POST_REPAIR_AUDIT_TRACKER.md` now has 32 DONE / 0 OPEN findings.
- Memory-controlled blocking I/O timeouts are now hard-capped before blocking system calls begin. Raw `physical_exchange` uses a 5 s default and 60 s hard Kernel maximum.
- Raw exchange timeout parsing uses overflow-safe `ParseInt` before duration multiplication, and `physicalExchange()` clamps again internally so direct callers cannot bypass the parser-level ceiling.
- Remote-storage client timeout parsing is overflow-safe, defaults to 5 s and is hard-capped by the existing 120 s storage maximum. `remoteSpaceRequest()` clamps direct duration callers again at the I/O boundary.
- Source adapters now use a 15 s default and 120 s hard maximum for both Go duration strings and integer-second timeout inputs. Oversized integers are capped before `time.Duration` multiplication.
- Architecture audit permanently requires the raw-exchange and remote-storage I/O-boundary clamps plus source-adapter hard cap; regression suite is 41/41 PASS.
- These timeout ceilings remain physical execution safety only. They do not rank Memory, infer utility, alter confidence, choose targets or become cognitive scheduling policy.
- Canonical cognitive seed remains unchanged: 129 Memories; seed SHA-256 `5f8538b951029890d85b025572149fde0a5b7708bcbbd524b17a0979048c7bc4`.
- Canonical `data/Memory.mem` remains unchanged with SHA-256 `e30987f5febd8e1cd893c6b313725eedfd72c518490ad711cb7991c5178ef411`; image version remains `28.9.0-memory-fabric-sovereign`; Memory ABI remains `memoryai-memory-abi-v1`.
- Final validation: architecture regression suite 41/41 PASS; live architecture audit PASS; Python suite 58/58 PASS; `go vet` PASS; full direct Go suite PASS; focused PR-032 `go test -race -count=20` PASS; `git diff --check` PASS; canonical Memory verify PASS.
- No `go build`, binary release build, archive packaging or release artifact generation was executed.

### Entry 044 — bound Frame-list cardinality
- Base HEAD: `6394478025ea40206109948cae1e52f859d13c21` (`Entry 043: hard-cap external I/O timeouts`).
- Continued post-repair audit closed PR-033; tracker now has 33 DONE / 0 OPEN findings.
- Frame list expansion now has a configurable physical ceiling via `MEMORYAI_FRAME_LIST_MAX_ITEMS`: default 8192 items, hard Kernel maximum 65536 items. Operator configuration cannot exceed the hard ceiling.
- `unicode_windows` rejects a Memory-requested output limit above the physical ceiling before allocating its result slice.
- `regex_all` no longer performs unbounded `FindAllStringSubmatch(..., -1)`; it requests at most `max+1` matches and fails closed on overflow.
- `list_append` and `list_unique_append` reject overflow before mutating the Frame list.
- `utf8_bytes`, `unicode_runes`, `json_keys` and `json_array_strings` also enforce the same physical result-cardinality boundary before bounded output expansion/copy.
- The Kernel never truncates or ranks overflowing cognitive results; Memory must explicitly batch larger work.
- Canonical cognitive seed and body are unchanged: 129 Memories; seed SHA-256 `5f8538b951029890d85b025572149fde0a5b7708bcbbd524b17a0979048c7bc4`; `data/Memory.mem` SHA-256 `e30987f5febd8e1cd893c6b313725eedfd72c518490ad711cb7991c5178ef411`.
- Final validation: architecture regression suite 42/42 PASS; live architecture audit PASS; Python suite 59/59 PASS; `go vet` PASS; full direct Go suite PASS; focused PR-033 `go test -race -count=20` PASS; `git diff --check` PASS; canonical Memory verify PASS.
- No `go build`, binary release build, archive packaging or release artifact generation was executed.

### Entry 045 — bound Frame scalar and output growth
- Base HEAD: `f336f4d36d696cf12067d8d8f75f80f2003ea16b` (`Entry 044: bound Frame list cardinality`).
- Continued post-repair audit closed PR-034; tracker now has 34 DONE / 0 OPEN findings.
- Frame scalar growth is physically bounded by `MEMORYAI_FRAME_VALUE_MAX_BYTES`: default 16 MiB, hard Kernel maximum 64 MiB.
- Frame Output has independent item and aggregate-byte ceilings: `MEMORYAI_FRAME_OUTPUT_MAX_ITEMS` defaults to 4096 with a 32768 hard maximum; `MEMORYAI_FRAME_OUTPUT_MAX_BYTES` defaults to 16 MiB with a 64 MiB hard maximum.
- `str_join` checks the combined byte requirement before concatenation, preventing repeated exponential joins from allocating an oversized Frame value before the post-primitive allocator sample.
- `emit` validates individual value size, output cardinality and aggregate output bytes before appending to `Frame.Output`.
- The Kernel never truncates, ranks or semantically filters oversized Frame/output content; Memory must explicitly split larger work.
- Canonical cognitive seed and body are unchanged: 129 Memories; seed SHA-256 `5f8538b951029890d85b025572149fde0a5b7708bcbbd524b17a0979048c7bc4`; `data/Memory.mem` SHA-256 `e30987f5febd8e1cd893c6b313725eedfd72c518490ad711cb7991c5178ef411`.
- Final validation: architecture regression suite 44/44 PASS; live architecture audit PASS; Python suite 61/61 PASS; `go vet` PASS; full direct Go suite PASS; focused PR-034 `go test -race -count=20` PASS; `git diff --check` PASS; canonical Memory verify PASS.
- No `go build`, binary release build, archive packaging or release artifact generation was executed.

### Entry 046 — bound executable Program growth
- Base HEAD: `72cebb26e5273f51511ce7aeb5f2b2098445d05f` (`Entry 045: bound Frame scalar and output growth`).
- Continued post-repair audit closed PR-035; tracker now has 35 DONE / 0 OPEN findings.
- Executable Memory Programs now have physical operation-count and serialized-byte ceilings. `MEMORYAI_PROGRAM_MAX_OPS` defaults to 4096 with a 65536 hard Kernel maximum; `MEMORYAI_PROGRAM_MAX_BYTES` defaults to 4 MiB with a 16 MiB hard maximum.
- `program_import` rejects oversized input before JSON decode and validates decoded Programs before replacement.
- `program_set_field`, `program_set_var_ref`, `program_insert_from`, and `program_replace_from` validate candidate Program shape before committing target mutation; splice paths preflight cardinality before slice growth.
- `program_export` is also checked against Program bounds and the existing Frame-value ceiling before publishing its serialized result into the Frame.
- The boundary is physical-only: Kernel does not truncate Program content, choose mutation variants, score instructions, or decide cognitive evolution. Memory must explicitly split or restructure growth that exceeds the physical envelope.
- Canonical cognitive seed and body remain unchanged: 129 Memories; seed SHA-256 `5f8538b951029890d85b025572149fde0a5b7708bcbbd524b17a0979048c7bc4`; `data/Memory.mem` SHA-256 `e30987f5febd8e1cd893c6b313725eedfd72c518490ad711cb7991c5178ef411`.
- Final validation: architecture regression suite 46/46 PASS; live architecture audit PASS; Python suite 63/63 PASS; `go vet` PASS; full direct Go suite PASS; focused PR-035 `go test -race -count=20` PASS; `git diff --check` PASS; canonical Memory verify PASS.
- No `go build`, binary release build, archive packaging or release artifact generation was executed.

### Entry 047 — bound VM template expansion
- Base HEAD: `d29fab483506ed28afaf4d9c579abd5ceb9a771e` (`Entry 046: bound executable Program growth`).
- Continued post-repair audit closed PR-036; tracker now has 36 DONE / 0 OPEN findings.
- VM template expansion now uses `expandFrameValueBounded` and the existing `MEMORYAI_FRAME_VALUE_MAX_BYTES` physical ceiling for initial input and every replacement pass.
- Replacement output length is preflighted before `strings.ReplaceAll`, so direct and recursive template growth cannot allocate an oversized Frame scalar before primitive execution or post-primitive telemetry sampling.
- `run()` executable-target expansion uses the same bounded path.
- Expansion overflow fails as a normal VM error before Frame, Memory, transport, storage, or event side effects. Unexpected panics remain visible and are rethrown.
- The Kernel does not truncate templates, select substitutions, rank content, or assign semantic meaning; Memory retains cognitive control inside the physical envelope.
- Canonical cognitive seed and body remain unchanged: 129 Memories; seed SHA-256 `5f8538b951029890d85b025572149fde0a5b7708bcbbd524b17a0979048c7bc4`; `data/Memory.mem` SHA-256 `e30987f5febd8e1cd893c6b313725eedfd72c518490ad711cb7991c5178ef411`.
- Final validation: architecture regression suite 47/47 PASS; live architecture audit PASS; Python suite 64/64 PASS; `go vet` PASS; full direct Go suite PASS; focused PR-036 `go test -race -count=20` PASS; `git diff --check` PASS; canonical Memory verify PASS.
- No `go build`, binary release build, archive packaging or release artifact generation was executed.
