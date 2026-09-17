# MemoryAI Architecture Repair Tracker

> This is the live source-of-truth for the Entry 029 architecture audit. Every repair commit must update this file and `DEVELOPMENT_CONTINUITY.md` in the same commit.

## Execution rules

- Baseline: latest GitHub `main` only.
- Development mode: direct tests only; do **not** run `go build`, generate Kernel binaries, or package releases.
- Status lifecycle: `OPEN -> IN_PROGRESS -> DONE`. A row is `DONE` only when its implementation, regression test, focused verification, commit, and push are complete.
- Cognition ownership: Kernel stays physical-only; all semantics, goals, drives, learning policy, conflict choice and fusion choice remain Memory-owned.
- Persistence: AI state stays inside `Memory.mem`; no new persistent sidecar database/WAL/JSON state.
- Testing: behavior change follows RED -> GREEN -> regression verification.

## Live status

| ID | Priority | Status | Issue | Primary area | Completion contract | Commit | Verification |
|---|---|---|---|---|---|---|---|
| AR-001 | P0 | DONE | Daemon lacks autonomous idle heartbeat | `Kernel/src/daemon_runtime.go` | Add physical idle ticker that only emits idle; cadence is physical/configurable, cognition remains Memory-owned. | Entry 033 | autonomous idle test PASS; focused race PASS; full go test PASS |
| AR-002 | P0 | DONE | Memory seed uses obsolete opcodes | `Kernel/current-required-structures.json; Kernel/src/*` | Migrate cognition_concurrency_set, cognition_stats, mesh_route_cognition to current physical ABI without moving policy into Kernel. | Entry 032 | architecture audit PASS; full go test PASS; focused race PASS |
| AR-003 | P0 | DONE | Pending external requests have no physical executor | `Kernel/src/source_adapter_runtime.go; daemon/event runtime` | Consume io.external.request.pending, execute adapter, persist done state, emit external.response/action.response. | Entry 034 | HTTP executor lifecycle PASS; restart no-replay PASS; focused race PASS |
| AR-004 | P0 | DONE | Source adapter tags do not match Memory selectors | `Kernel/src/source_adapter_runtime.go; current seed` | Align generic physical adapter metadata with Memory-owned enabled/action selection. | Entry 034 | source selector tag regression PASS |
| AR-005 | P0 | DONE | Event cognition handlers are serialized/fail-fast | `Kernel/src/event_runtime.go; scheduler runtime` | Run independent handlers concurrently where physically safe and isolate one handler failure from unrelated cognition chains. | Entry 033 | failure isolation + parallel peak tests PASS; focused race PASS; full go test PASS |
| AR-006 | P1 | OPEN | v31 semantic event graph is disconnected | `Kernel/current-required-structures.json` | Connect live Experience/semantic events without hard-coded language semantics in Kernel. | - | - |
| AR-007 | P0 | DONE | Semantic Memories miss event.emit capability | `Kernel/current-required-structures.json` | Add required capabilities and permanent capability audit. | Entry 032 | 99-Memory capability audit PASS |
| AR-008 | P1 | OPEN | Natural-language reply path is incomplete | `Current seed; daemon response boundary` | Close cognition -> expression frame -> learned surface rendering -> reply result. | - | - |
| AR-009 | P1 | OPEN | Initial Memory has no Chinese grounding basis | `Current seed` | Provide Memory-owned Chinese bootstrap grounding/evidence without Kernel language tables. | - | - |
| AR-010 | P1 | OPEN | Executable mutation exists but recombination/fission is absent | `Current seed; generic VM program mutation primitives` | Implement Memory-owned structure recombination/fission using generic program splice primitives. | - | - |
| AR-011 | P1 | OPEN | Memory body fusion decision chain is absent | `Current seed; space primitives` | Add Memory-owned discover/evaluate/decide/execute body-fusion flow. | - | - |
| AR-012 | P1 | OPEN | Kernel space_merge contains reconciliation policy | `Kernel/src/kernel.go` | Reduce merge to physical primitives; preserve conflicts for Memory-owned resolution. | - | - |
| AR-013 | P0 | DONE | Kernel Memory schema drops input_pattern/output_effect/executable | `Kernel/src/kernel.go; persistence tests` | Make required executable-structure fields round-trip safely. | Entry 032 | schema round-trip test PASS; focused race PASS |
| AR-014 | P1 | OPEN | Executable Memory lacks standardized success history / mutation variants | `Kernel schema + current seed` | Standardize persistent first-class execution history and variant lineage fields. | - | - |
| AR-015 | P2 | OPEN | RuntimeExecCount is not success history | `Current seed / execution feedback` | Keep telemetry physical and create Memory-owned success/failure outcome history. | - | - |
| AR-016 | P1 | OPEN | Mesh global sync does not integrate remote Memory | `Current seed; mesh runtime` | Perform authorized transient direct reads and emit mesh.memory.integrated without local caching/import. | - | - |
| AR-017 | P0 | OPEN | Memory does not use current Mesh execution primitives | `Current seed` | Migrate to mesh_route_execution / mesh_shared_fetch / mesh_structure_run as appropriate. | - | - |
| AR-018 | P1 | OPEN | Mesh digest conflicts do not enter Memory reconciliation | `Mesh authority/runtime + current seed` | Convert physical conflict result into Memory-visible conflict event/frontier. | - | - |
| AR-019 | P1 | OPEN | Proposal replay receipts lack ACK + safe GC | `Kernel/src/mesh_proposal_replay_runtime.go; journal runtime` | Add receipt ACK, slot fence proof and restart-safe GC; fail closed on ambiguous state. | - | - |
| AR-020 | P1 | OPEN | Kernel recognizes cog.surface/cog.input for trace policy | `Kernel/src/kernel.go` | Remove cognition-label interpretation from Kernel tracing. | - | - |
| AR-021 | P2 | OPEN | Physical storage metadata uses cog.storage namespace | `Kernel/src/kernel.go; current seed if referenced` | Rename to physical storage namespace with compatibility migration. | - | - |
| AR-022 | P2 | DONE | Activation callers retain topK vocabulary | `Kernel/src/kernel.go; daemon_runtime.go` | Rename caller variables/arguments to pageCap and expand audit scope. | Entry 032 | topK caller audit regression PASS; production scan clean |
| AR-023 | P0 | DONE | Architecture audit does not validate Memory-Kernel opcode ABI | `tools/architecture_audit.py` | Fail when seed uses unsupported/obsolete opcode. | Entry 031 | `test_architecture_audit.py` PASS; live audit rejects AR-002 obsolete opcodes |
| AR-024 | P0 | DONE | Architecture audit does not validate capability requirements | `tools/architecture_audit.py` | Fail on privileged opcode without declared capability; lock semantic regression. | Entry 031 | synthetic missing-capability regression PASS |
| AR-025 | P2 | OPEN | memory_copy can retain stale CapabilitySig | `Kernel/src/kernel.go; security tests` | Clear or regenerate signature after structural identity/program mutation. | - | - |
| AR-026 | P2 | OPEN | Structural digest includes CapabilitySig | `Kernel/src/structure_runtime.go` | Exclude physical security signature from structural Memory identity digest. | - | - |
| AR-027 | P0 | DONE | SELF_ONLY / ASSISTED_LEARNING mode is only implicit | `Kernel/current-required-structures.json` | Add explicit Memory-owned learning mode; default SELF_ONLY and gate external research on ASSISTED_LEARNING. | Entry 034 | learning mode contract PASS; default SELF_ONLY locked |
| AR-028 | P0 | DONE | Event payload vars are not exposed under `__event.*` contract used by Memory | `Kernel/src/event_runtime.go` | Mirror opaque event vars into `__event.<key>` without semantic interpretation. | Entry 034 | namespaced event variable regression PASS |

## Repair order / implementation batches

### Batch A — ABI and permanent gates
AR-023 -> AR-024 -> AR-002 -> AR-007 -> AR-013 -> AR-022. This batch prevents the same Kernel/Memory drift from recurring before runtime work continues.

### Batch B — autonomous runtime and concurrency
AR-001 -> AR-005 -> lifecycle physical-health input required by AR-002. This makes Memory-owned idle cognition actually run continuously without one handler aborting unrelated chains.

### Batch C — external evidence and assisted learning
AR-004 -> AR-003 -> AR-028 -> AR-027 -> external response ingestion. Ollama remains an optional external evidence source, never the AI.

### Batch D — semantic / natural-language loop
AR-006 -> AR-008 -> AR-009. Connect existing Memory semantic structures to the live graph, then produce learned natural-language output and seed Chinese grounding in Memory.

### Batch E — self-growth and Memory-body evolution
AR-010 -> AR-014 -> AR-015 -> AR-011 -> AR-012. Recombination, persistent outcome history and Memory-owned fusion become executable Memory behavior.

### Batch F — Sovereign Memory Mesh durability/integration
AR-017 -> AR-016 -> AR-018 -> AR-019 -> AR-025 -> AR-026. Close remote direct-read integration, conflict feedback and safe replay receipt lifecycle.

### Batch G — remaining Kernel boundary cleanup
AR-020 -> AR-021. Remove remaining cognition-shaped namespace/policy leakage from the physical Kernel.

## Update log

- 2026-09-17: Tracker created from the complete post-Entry-029 audit. All 26 confirmed items start as `OPEN`; implementation begins with Batch A.
- 2026-09-17 / Entry 031: AR-023 and AR-024 DONE. Audit now validates every Memory opcode against `execPrimitive` and every privileged opcode against declared capability. Live audit correctly blocks the still-open AR-002/AR-007 drift.
- 2026-09-17 / Entry 032: AR-002, AR-007, AR-013 and AR-022 DONE. Current seed uses only supported physical opcodes, semantic emitters declare `event.emit`, executable contract fields round-trip through Kernel Memory schema, lifecycle reads generic physical handler counters, and activation caller vocabulary is pageCap-only.

- 2026-09-17 / Entry 033: AR-001 and AR-005 DONE. Daemon now emits configurable physical idle ticks autonomously; top-level independent Event handlers run concurrently through the physical speculative scheduler, handler errors are aggregated after unrelated handlers finish, and nested canonical/speculative dispatch remains serial for physical safety.

- 2026-09-17 / Entry 034: AR-003, AR-004, AR-027 and AR-028 DONE. Source adapters expose operational selector tags, Memory-owned learning mode defaults to SELF_ONLY and gates external research, physical I/O requests persist executing/response-ready/done state inside Memory.mem, completed requests are not replayed after restart, and opaque event vars are mirrored into the `__event.*` namespace used by executable Memory.
