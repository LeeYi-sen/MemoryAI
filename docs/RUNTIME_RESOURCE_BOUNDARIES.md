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

## External I/O timeout ceilings

Memory-provided timeout values are physical execution parameters only and are hard-capped before blocking I/O begins.

- Raw `physical_exchange`: 5 s default, 60 s hard maximum.
- Remote-storage client requests: 5 s default, 120 s hard maximum.
- Source adapters: 15 s default, 120 s hard maximum.

Raw exchange and remote-storage requests clamp again at the actual I/O function boundary, so a direct internal caller cannot bypass parser-level limits. Oversized integer values are capped before `time.Duration` multiplication to avoid overflow.

Timeout ceilings never rank Memory, infer utility, choose targets, or alter cognitive policy.

## Frame list cardinality

Frame list expansion is physically bounded by `MEMORYAI_FRAME_LIST_MAX_ITEMS`.

- default: 8192 items
- hard Kernel maximum: 65536 items

The Kernel never silently truncates a cognitive result set. A primitive that would exceed the physical cardinality returns an error before the bounded output mutation/allocation; Memory must explicitly split or batch larger work.

## Frame scalar and output bounds

Frame scalar growth and emitted output are bounded before the relevant allocation/mutation:

- `MEMORYAI_FRAME_VALUE_MAX_BYTES`: 16 MiB default, 64 MiB hard maximum.
- `MEMORYAI_FRAME_OUTPUT_MAX_ITEMS`: 4096 default, 32768 hard maximum.
- `MEMORYAI_FRAME_OUTPUT_MAX_BYTES`: 16 MiB default, 64 MiB hard maximum.

`str_join` checks the combined byte requirement before concatenation. `emit` checks the individual value, output item count and aggregate output bytes before append. The Kernel never truncates cognitive output to fit these physical ceilings.
