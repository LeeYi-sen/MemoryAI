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
