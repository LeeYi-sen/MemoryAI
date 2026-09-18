# Memory Body Integrity Boundary

This document freezes the physical integrity rules for `Memory.mem`.

## Canonical format

Current image version:

- `28.9.0-memory-fabric-sovereign`

Memory ABI remains:

- `memoryai-memory-abi-v1`

The cognitive Memory schema did not change in this revision. The physical store index changed to:

- `memoryai-index-v2-sha256`

## ZIP boundary

A Memory body is rejected when:

- ZIP entry names are duplicated;
- a required manifest/store path is empty;
- two required manifest fields alias the same physical ZIP entry;
- a required manifest hash is missing;
- metadata decompression exceeds the Kernel physical byte ceiling;
- a Stored index section escapes the physical body file;
- the ID index cardinality does not exactly match `memory_count`.

Duplicate-name lookup is never first-wins.

## Index v2

Each ID/tag index entry is 56 bytes:

- 8 bytes: FNV-1a lookup hash
- 8 bytes: payload offset
- 4 bytes: payload length
- 4 bytes: posting count
- 32 bytes: SHA-256 of the referenced payload

ID index entries authenticate individual Memory records.

Tag index entries authenticate individual tag / physical-feature posting payloads.

The index itself is authenticated by the manifest hash before the store becomes available.

## Scalable load integrity

Normal v2 load intentionally does **not** scan all Memory records.

At load time the Kernel verifies:

- genesis entry hash;
- ID index hash;
- tag index hash.

When a specific Memory is read:

1. the verified ID index resolves its offset/length;
2. bounds and per-record physical byte limits are checked;
3. only that record is read;
4. SHA-256 is compared with the digest stored in the authenticated index;
5. JSON is decoded only after the digest passes.

When a tag/physical activation posting is read, the same process applies to the corresponding tag-list payload.

This keeps integrity fail-closed without reintroducing an O(N) semantic Memory scan at startup.

## Legacy index compatibility

A body without `index_format` is treated as the legacy 24-byte index format.

Legacy indexes do not carry per-record SHA-256. Therefore the Kernel verifies the complete manifest-declared:

- records section;
- ID index;
- tag index;
- tag-lists section;
- genesis;

before the legacy store is exposed.

Any subsequent persistence rewrites the body using index v2.

## fsck

Explicit fsck remains stronger than normal v2 startup: it streams every manifest-declared entry through SHA-256 and validates the indexed store.

fsck does not allocate entire large ZIP entries solely to hash them.

## Canonical Memory

Entry 040 canonical values:

- Memory count: 129
- cognitive seed SHA-256: `5f8538b951029890d85b025572149fde0a5b7708bcbbd524b17a0979048c7bc4`
- Memory body SHA-256: `e30987f5febd8e1cd893c6b313725eedfd72c518490ad711cb7991c5178ef411`

The seed digest is unchanged from Entry 039; the body digest changed because the physical index format and image version changed.
