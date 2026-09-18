# Sovereign Memory Mesh Security Boundary

This document defines the physical authentication boundary for Sovereign Memory Mesh traffic.

## Two separate authentication layers

Mesh transport has two independent proofs:

1. **Cluster membership / channel authentication** — `MEMORYAI_MESH_CAPABILITY_KEY` signs the raw request body with HMAC.
2. **Concrete node identity** — every remote Mesh request is also signed by the requester's Ed25519 node private key.

Possessing the shared Mesh HMAC key is therefore not sufficient to impersonate another registered node.

## Node identity keys

Sovereign:
- `MEMORYAI_MESH_SOVEREIGN_PRIVATE_KEY_B64` — Sovereign Ed25519 private key. It also serves as the Sovereign node identity key.
- `MEMORYAI_MESH_SOVEREIGN_PUBLIC_KEY_B64` — Sovereign public key distributed to nodes for grant verification.

Ordinary node:
- `MEMORYAI_MESH_NODE_PRIVATE_KEY_B64` — node-specific Ed25519 private key. It must not be shared with another node.

The node public key is derived from the private key and carried during registration.

## Registration and persistence

A node registration request must:
- pass Mesh HMAC verification;
- carry the same node ID in the signed transport identity and registration record;
- prove possession of the private key corresponding to the advertised public key.

The Sovereign persists `node_id -> public_key` inside the normal Sovereign Mesh directory Memory in `Memory.mem`.

Once a node ID has a non-empty public key, a registration attempt with a different public key fails closed. There is no silent key rotation or identity rebind.

Legacy directory records that predate node public keys may bind a public key on their first authenticated post-upgrade registration.

## Authority requests

All remote authority operations are node-signed. The Sovereign verifies the signature against the public key already bound to the requester's node ID.

`shared_grant` additionally requires:
- authenticated requester node ID equals the request's `OriginNode`;
- requester public key is present in the Sovereign directory.

## Direct node-to-node requests

Sovereign grants are version 2 and bind:
- operation;
- Memory ID;
- origin node ID;
- origin node public key;
- target node ID;
- expiry;
- nonce.

The target node validates both:
- the Sovereign signature on the grant;
- the requester's Ed25519 signature against the origin public key embedded in that Sovereign-signed grant.

Therefore another Mesh member cannot reuse its own private key while claiming the grant's origin node.

Side-effect grants remain one-shot and are durably fenced in `Memory.mem` before execution.

## Transport encryption

Non-loopback Mesh endpoints require HTTPS. Loopback HTTP is allowed for same-host development/testing.

The shared HMAC key, node identity private keys and Sovereign private key are deployment secrets and are not written to repository files or persistent sidecars.
