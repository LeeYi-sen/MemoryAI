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
