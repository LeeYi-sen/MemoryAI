# MemoryAI Post-Repair Architecture Audit Tracker

> Fresh audit started after Entry 038. The frozen AR-001..AR-029 tracker remains closed; this file tracks newly discovered deviations from the current remote main.

## Execution rules
- Baseline: latest remote `main` only.
- Every implementation commit updates this tracker and `DEVELOPMENT_CONTINUITY.md` together.
- Kernel remains physical-only. Memory decides expansion, conflict meaning, reconciliation, learning and policy.
- Persistent AI state remains in Memory body files; no new JSON/DB/WAL sidecars.
- Behavior changes use RED -> GREEN -> regression -> race where applicable.

## Live findings

| ID | Priority | Status | Finding | Completion contract |
|---|---|---|---|---|
| PR-001 | P0 | DONE | `placeRuntimeMemory` can create an automatic shard when capacity is full, bypassing Memory-owned expansion decision | Kernel must fail closed on capacity exhaustion; only Memory-invoked `space_create` may create a shard; 5 GiB floor remains physical Kernel gate |
| PR-002 | P1 | DONE | Unused legacy `meshRuntime.flushJournal()` clears in-memory journal before transport and bypasses durable Memory spool semantics | Remove alternate non-durable implementation and permanently audit that only `flushMeshDeferredJournal` is active |
| PR-003 | P0 | DONE | `resolveRemoteID` silently returns the first reachable copy of a remote identity | Query reachable replicas; identical structural digests may collapse physically, divergent digests must fail closed without Kernel winner selection |
| PR-004 | P0 | DONE | mem-node remote storage protocol accepts unauthenticated mutation requests over raw TCP | Add authenticated transport boundary for remote storage requests; mutation without valid authorization must be rejected |
| PR-005 | P0 | DONE | mem-node `put` resolves same-ID conflict by generating a new ID and writing `merge_source_id` | Physical server must never invent semantic conflict branches; exact duplicate is idempotent, divergent duplicate returns conflict evidence, explicit replace remains caller-selected |
| PR-006 | P1 | DONE | Structural digest semantics diverge across `memoryJSONDigest`, `structuralMemoryDigest`, and mem-node `digest` | Use one structural digest definition that excludes RuntimeExecCount and CapabilitySig everywhere |
| PR-007 | P1 | DONE | mem-node `put/replace` bypasses bounded upsert/tag-index bookkeeping | Remote writes must use bounded explicit Memory write path and preserve tag/capacity invariants |
| PR-008 | P1 | DONE | Registered remote endpoint BodyID is not checked on direct read | If descriptor pins BodyID/ABI, direct remote read must reject endpoint identity drift |
| PR-009 | P0 | DONE | mem-node `put` can implicitly create a new storage body and remote `create` does not enforce the 5 GiB floor | Remote body creation must be an explicit Memory-invoked `remote_space_create` action and must pass the same >=5 GiB physical safety gate |
| PR-010 | P0 | DONE | Durable `space_copy/space_move` silently changes Memory ID on a divergent same-ID target; unused legacy `importMemory` contains the same policy | Explicit transfer must fail closed on divergent identity and preserve source/target unchanged; remove dead semantic branch implementation |
| PR-011 | P1 | DONE | Non-VM revision changes in external-request lifecycle and structure-sync can retain stale `CapabilitySig` | Any physical path that changes Revision/identity must clear stale signature unless it explicitly regenerates a valid current signature |
| PR-012 | P0 | DONE | `resolve` / `resolveMutable` silently choose the first sorted Memory when a tag matches multiple identities | Single-target primitives must fail closed on ambiguous tag lookup; Memory must use `tag_list` and provide an explicit ID for cognitive selection |
| PR-013 | P0 | DONE | Authenticated remote storage mutations have no compare-and-swap fence, so a captured/stale replace or delete can overwrite/delete newer state | Replace/delete must bind the observed structural digest; divergent/stale replacement must fail closed and revisions must not roll backward |
| PR-014 | P1 | DONE | `memory_new` silently ignores missing/conflicted parent resolution and can create dangling lineage | Every explicitly declared parent must resolve successfully before child creation; missing/forgotten/conflicted/integrity-failed parent aborts creation |


## Closure evidence

- PR-001 through PR-014 are DONE.
- All behavior changes were covered by focused RED -> GREEN regressions.
- Permanent architecture gates now cover Memory-owned expansion, durable Mesh journal ownership, remote identity conflicts, authenticated/TLS storage transport, unified structural digest semantics, bounded remote writes, endpoint BodyID/ABI pinning, explicit remote creation, transfer conflict fail-closed behavior, CapabilitySig invalidation, ambiguous-tag fail-closed resolution, remote mutation CAS fencing, and parent-lineage resolution.
- Final validation before Entry 039: architecture regressions 15/15 PASS; live architecture audit PASS; Python suite 32 tests PASS; go vet PASS; full Go suite PASS; focused race -count=20 PASS; Memory.mem verify PASS.

## Continued audit after Entry 039

| ID | Priority | Status | Finding | Completion contract |
|---|---|---|---|---|
| PR-015 | P1 | DONE | `nextID()` derives Memory/Body identity only from `time.Now().UnixNano()` | Identity generation must include process-unique entropy plus an atomic in-process sequence; concurrent generation must be collision-free without relying on clock granularity |
| PR-016 | P1 | DONE | Initial empty-body `writeDetZip()` closes and renames without syncing the file and parent directory | Successful body creation must mean durable bytes: sync file, rename atomically, sync parent directory, and clean temporary file on failure |

| PR-017 | P0 | DONE | Raw TCP/TLS `physicalExchange()` uses unbounded `io.ReadAll`, so one primitive can allocate arbitrarily before VM budget enforcement | Raw exchange must use an overflow-detecting bounded physical read; operator tuning may adjust within a hard Kernel ceiling but never make it unbounded |
| PR-018 | P0 | DONE | `artifact_read/write/digest` can process arbitrarily large files/data inside one primitive before the post-primitive resource budget check | Artifact primitives must enforce a bounded physical byte ceiling before allocation/I/O; digest must stream rather than read the whole file into memory |
| PR-019 | P0 | DONE | Artifact workspace containment is lexical only; a symlink inside the workspace can redirect read/write/digest outside the sandbox | Resolve the configured workspace root physically and reject symlink components in every artifact-relative path before I/O |
| PR-020 | P0 | DONE | mem-node storage-root containment is lexical only; a symlink inside the root can redirect remote body operations outside the configured storage sandbox | Artifact and mem-node path resolution must share one real-root physical sandbox helper that rejects symlink components below the trusted root |
| PR-021 | P0 | DONE | Shared Mesh HMAC authenticates cluster membership but does not cryptographically bind a request to its claimed `OriginNode`; a member can request a Sovereign grant under another node label | Add per-node Ed25519 identity: registration binds node ID to public key, authority/direct RPC bodies are node-signed, grants bind origin public key, and public-key rebind fails closed |
| PR-022 | P0 | DONE | Memory.mem JSON ZIP metadata entries can be decompressed without a hard physical ceiling before VM/resource budgets exist | Bound metadata/opaque entry reads with overflow detection; fsck hashes large entries as a stream instead of allocating the whole entry |
| PR-023 | P0 | DONE | IndexedStore trusts index offset/length before allocation/read; a malformed record/tag entry can request huge memory or point outside its Stored section | Validate Stored section against actual body file bounds and validate every indexed slice + per-record physical ceiling before allocation |
| PR-024 | P1 | DONE | ZIP archives may contain duplicate entry names; current first-match lookup makes manifest/store/hash resolution ambiguous | Reject duplicate Memory.mem ZIP entry names and require unique lookup for metadata, store sections and fsck |
| PR-025 | P0 | DONE | Normal load trusts manifest-declared store bytes without enforcing their hashes; explicit fsck is the only whole-body integrity check | New index format must authenticate record/posting slices lazily via per-entry SHA-256 while load verifies genesis + index hashes; legacy index bodies remain fail-closed by verifying all declared store section hashes before use |

## Entry 040 closure evidence

- PR-015 through PR-025 are DONE.
- Identity allocation now uses process entropy + atomic sequence rather than wall-clock granularity.
- Initial body creation is durable across file sync, atomic rename and parent-directory sync.
- Raw exchange and artifact primitives have hard physical byte ceilings; artifact and mem-node paths share a symlink-safe physical sandbox.
- Sovereign Mesh now uses HMAC membership plus per-node Ed25519 identity. Grant v2 binds requester node ID and public key, and node public-key rebind fails closed.
- Memory body input rejects duplicate ZIP entries, oversized metadata, out-of-section indexed slices, and unauthenticated index corruption.
- Index v2 (`memoryai-index-v2-sha256`) stores SHA-256 per Memory record and tag posting. Load verifies genesis + id/tag indexes only; record/posting payloads are verified lazily when accessed. Legacy v1 indexes remain readable only after complete records/taglists hash verification.
- Canonical cognitive seed is unchanged: 129 Memories, seed SHA-256 `5f8538b951029890d85b025572149fde0a5b7708bcbbd524b17a0979048c7bc4`.
- Canonical physical body is now version `28.9.0-memory-fabric-sovereign`, SHA-256 `e30987f5febd8e1cd893c6b313725eedfd72c518490ad711cb7991c5178ef411`.
- Final validation: architecture regression suite 30/30 PASS; live architecture audit PASS; Python suite 47/47 PASS; `go vet` PASS; full direct Go suite PASS; focused post-repair `go test -race -count=20` PASS; `git diff --check` PASS; Memory.mem verify PASS. No `go build` or release packaging executed.

## Continued audit after Entry 040

| ID | Priority | Status | Finding | Completion contract |
|---|---|---|---|---|
| PR-026 | P0 | DONE | Canonical `run()` checks write/event/op budgets only after a primitive executes; a budget-exceeding Memory mutation can remain applied even though execution returns an error | Install run-scoped physical ceilings and reject the next potentially mutating/event primitive before execution once the ceiling is exhausted; keep post-sampling as a backstop for CPU/allocation telemetry |
| PR-027 | P0 | DONE | Daemon Unix-socket JSON accepts unbounded input with no per-connection deadline or concurrency cap | Bound daemon request/response bytes, set physical connection deadlines, and cap concurrent daemon handlers |
| PR-028 | P0 | DONE | Mesh HTTP request/response uses truncating `LimitReader(2 MiB)` and the server has no read/write/header/idle deadlines | Reject oversized Mesh payloads explicitly before auth/JSON, bound outbound requests/responses, and configure HTTP server transport timeouts/header ceiling |

## Entry 041 closure evidence

- PR-026 through PR-028 are DONE.
- Memory write/event/op quotas now fail closed before the budget-exceeding primitive creates its side effect. Nested calls execute under the callee resource scope and restore the caller scope on return.
- Daemon Unix-socket requests/responses are byte-bounded; connections have deadlines; handler concurrency is hard-capped.
- Mesh HTTP requests/responses use overflow-detecting byte ceilings instead of truncating readers, and the HTTP server has read-header/read/write/idle timeouts plus a header-size ceiling.
- Bounded JSON response encoding is streaming and stops once the physical ceiling is reached instead of first materializing an unbounded JSON payload.
- Final architecture regression suite for this entry is 33/33 PASS.

## Continued audit after Entry 041

| ID | Priority | Status | Finding | Completion contract |
|---|---|---|---|---|
| PR-029 | P0 | DONE | Top-level event fan-out creates one goroutine per matched handler before the semaphore, so a large exact trigger set can exhaust memory even when physical execution concurrency is small | Dispatch handlers through a bounded worker/batch path; queued goroutines and temporary result storage must scale with the physical concurrency/batch ceiling rather than total matched handlers |
| PR-030 | P0 | DONE | `call_parallel` allocates result/status arrays and schedules every target in an unbounded Frame list; worker count is bounded but fan-out cardinality is not | Add a configurable physical fan-out ceiling with a hard Kernel maximum; oversized fan-out must fail closed before child execution/allocation so Memory can explicitly batch the work |
| PR-031 | P0 | DONE | Remote storage transport still uses a fixed truncating 4 MiB decoder, unbounded JSON response materialization and one goroutine per accepted connection | Add hard-bounded storage request/response envelopes, explicit overflow rejection, connection deadlines and handler concurrency ceiling; oversized list/get/put traffic must fail without partial writes or silent truncation |

## Entry 042 closure evidence

- PR-029 through PR-031 are DONE.
- Event fan-out no longer creates one goroutine per exact handler match. Top-level independent handlers are processed in bounded batches through the existing physical worker pool; temporary goroutines/results scale with the batch/concurrency ceiling rather than total match cardinality.
- `call_parallel` now fails closed before target-copy/result allocation/child execution when the Memory-provided target list exceeds the physical fan-out ceiling. Default ceiling is 1024; hard Kernel ceiling is 65536 via `MEMORYAI_PARALLEL_FANOUT_MAX_TARGETS`.
- Remote storage envelopes now use `MEMORYAI_STORAGE_MAX_BYTES` with a 20 MiB default and 32 MiB hard Kernel maximum. Oversized requests/responses fail explicitly; response fallback is itself a bounded authenticated envelope.
- mem-node connection timeout is configurable with `MEMORYAI_STORAGE_TIMEOUT_MS` (30 s default, 120 s hard max). Handler concurrency is bounded by `MEMORYAI_STORAGE_MAX_CONCURRENT` (32 default, 256 hard max). Excess connections are closed before a handler goroutine is created.
- Storage envelope signing remains byte-stable: the `Encoder.Encode` framing newline is removed before signing/embedding the `RawMessage`, preventing outer JSON normalization from invalidating HMAC verification.
- Final validation: architecture regression suite 38/38 PASS; live architecture audit PASS; Python suite 55/55 PASS; `go vet` PASS; full direct Go suite PASS; focused PR-029..031 `go test -race -count=20` PASS; `git diff --check` PASS; canonical Memory verify PASS.
