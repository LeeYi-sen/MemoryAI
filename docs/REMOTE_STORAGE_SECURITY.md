# Remote Memory Storage Security Boundary

This document defines the physical transport and mutation invariants for `Kernel mem-node <storage-dir> <listen-address>`.

## Authentication

Every request and response is wrapped in an HMAC-authenticated storage envelope.

The transport key is resolved in this order:

1. `MEMORYAI_STORAGE_CAPABILITY_KEY`
2. `MEMORYAI_MESH_CAPABILITY_KEY` as the shared physical-transport fallback

A mem-node refuses to start when neither key is available.

## Transport encryption

Loopback listeners may use plaintext TCP because traffic never leaves the host, but HMAC authentication is still mandatory.

A non-loopback listener requires both:

- `MEMORYAI_STORAGE_TLS_CERT`
- `MEMORYAI_STORAGE_TLS_KEY`

Clients use TLS for non-loopback targets. Optional trust configuration:

- `MEMORYAI_STORAGE_TLS_CA`: PEM CA bundle appended to the system trust pool.
- `MEMORYAI_STORAGE_TLS_SERVER_NAME`: explicit TLS server name when it differs from the host string.

Cross-host plaintext remote storage is rejected.

## Mutation consistency

Remote `put` without replace is idempotent only for the same structural Memory digest. A divergent same-ID Memory is returned as conflict evidence; the Kernel never invents a new identity or chooses a winner.

Remote replace is compare-and-swap:

- the caller first observes the current structural digest;
- the replace request carries that digest as `expected_digest`;
- the server verifies it again under the serialized physical-body transaction;
- replacement revision must advance monotonically;
- stale or divergent replacement fails closed.

Remote delete is also compare-and-swap and requires `expected_digest`. If the target changed after observation, deletion fails closed.

## Body creation and capacity

`put` never creates a missing remote body. Body creation must be an explicit `create` request initiated by Memory through the corresponding physical primitive.

Creation is allowed only when the target filesystem satisfies the Memory expansion free-space floor. The floor is never lower than 5 GiB; `MEMORYAI_SHARD_MIN_FREE_BYTES` may raise it but cannot lower it.

## Identity and read semantics

Registered remote Memory endpoints pin physical `BodyID` and Memory ABI. Direct registered reads reject BodyID/ABI drift and reject a response whose Memory ID differs from the requested ID.

If multiple reachable replicas expose the same Memory ID, identical structural digests collapse physically. Divergent digests are an explicit conflict and the Kernel does not select a winner.

Remote Memory remains direct-read only: it is not silently imported into local `Memory.mem`. An unreachable origin is forgotten until it becomes reachable again.

## Physical transport limits

Remote storage envelopes are bounded by `MEMORYAI_STORAGE_MAX_BYTES`: 20 MiB by default and 32 MiB maximum. Both request and response bodies are encoded/decoded through explicit overflow-detecting physical ceilings.

Oversized replies are converted to a small HMAC-authenticated failure envelope instead of being silently truncated or partially materialized as an unbounded JSON object.

`MEMORYAI_STORAGE_TIMEOUT_MS` controls the per-connection deadline: 30 seconds by default and 120 seconds maximum.

`MEMORYAI_STORAGE_MAX_CONCURRENT` controls admitted mem-node handlers: 32 by default and 256 maximum. Connections above that ceiling are closed before a handler goroutine is created.

The signed inner JSON is canonicalized to the exact bytes embedded in the outer `RawMessage`; transport framing newlines are excluded from the signed body so envelope re-encoding cannot invalidate HMAC verification.
