# Runtime Resource and Transport Boundaries

This document freezes physical execution and transport limits enforced by the Kernel.

## Memory execution budgets

`ResourceBudget` is owned by each executable Memory and interpreted only as a physical quota.

The Kernel checks `MaxOps` before the next primitive executes. A primitive beyond the operation ceiling never runs.

Potential Memory-write primitives reserve one `MaxMemoryWrites` slot before execution. If no slot remains, the primitive is rejected before changing Memory state.

`emit_event` reserves one `MaxEvents` slot before the event is appended or dispatched. An event beyond the ceiling is never emitted.

Nested `call` execution enters the callee's own resource scope and restores the caller scope on return. Caller and callee quotas therefore do not become cognitive policy or leak into each other.

CPU time, total allocation and heap-growth limits remain physical runtime sampling gates. They stop subsequent execution when exceeded; they are not semantic reward/fitness signals.

## Daemon Unix socket

Daemon transport has hard Kernel ceilings even when deployment environment variables request larger values.

- `MEMORYAI_DAEMON_MAX_BYTES`: request/response byte ceiling. Default 1 MiB; hard maximum 8 MiB.
- `MEMORYAI_DAEMON_TIMEOUT_MS`: per-connection deadline. Default 15 s; hard maximum 60 s.
- `MEMORYAI_DAEMON_MAX_CONCURRENT`: simultaneous accepted handlers. Default 32; hard maximum 256.

Oversized requests fail closed. Responses are encoded through a bounded streaming JSON writer; an oversized response is converted into a bounded failure response instead of allocating or transmitting the full payload.

The daemon socket remains ephemeral runtime state and is not persistent Memory state.

## Mesh HTTP transport

- `MEMORYAI_MESH_MAX_BYTES`: request/response byte ceiling. Default 2 MiB; hard maximum 16 MiB.
- Non-loopback endpoints still require TLS.
- Requests remain protected by Mesh HMAC membership authentication plus Ed25519 node identity.

The Mesh HTTP server enforces fixed physical server limits:

- read-header timeout: 5 s
- read timeout: 10 s
- write timeout: 10 s
- idle timeout: 30 s
- maximum HTTP header bytes: 32 KiB

Both inbound requests and outbound responses use overflow-detecting physical byte ceilings. The previous truncating `LimitReader(2 MiB)` behavior is forbidden.

## Architecture rule

These controls are physical safety boundaries only. They must never rank Memory, infer utility, choose goals, alter confidence, or become reward/fitness functions.

## Event fan-out and parallel calls

Top-level physical event dispatch never creates one goroutine per matched handler. Exact handler matches are processed in bounded batches through the existing physical worker pool.

The event batch hard ceiling is 256 handlers; the normal batch size is derived from physical worker concurrency and never from semantic relevance or priority.

`call_parallel` has a separate physical cardinality ceiling. `MEMORYAI_PARALLEL_FANOUT_MAX_TARGETS` defaults to 1024 and is hard-capped by the Kernel at 65536 targets.

If a Memory requests a larger fan-out, execution fails before copying the target list, allocating result/status arrays, or running child Memories. Memory may explicitly batch larger work itself.
