# Source Baseline Policy

Repository: `LeeYi-sen/MemoryAI` (private)

## Mandatory development rule

Every MemoryAI change must begin from the latest `main` HEAD of this repository. Local archives, chat attachments, `/mnt/data` work directories, or older release packages are never authoritative once a corresponding source revision exists in GitHub.

Required workflow:

1. Read/sync latest `main` HEAD.
2. Create the code change from that HEAD only.
3. Build and run the relevant architecture/runtime gates.
4. Commit the tested source change back to GitHub.
5. Use the resulting new HEAD as the next development baseline.

## Runtime artifact policy

`data/Memory.mem` is a large mutable runtime body and compiled Kernel/Gateway binaries are generated artifacts. They are not normal source files in Git.

Each release must bind generated artifacts to source through SHA-256 manifests and acceptance reports. A release is not considered frozen unless the exact Kernel hash, Memory.mem hash, ABI, test matrix and source commit SHA are recorded together.

## Recovered pre-GitHub baseline

The last complete pre-repository source artifact available at repository initialization was MemoryAI v27 repair_73_92. The local source-only recovery archive was:

- size: `199768` bytes
- SHA-256: `b42761d4d3bb8cd10da644c31df92f9b9fe3b569a1d119e71f22e2d9d5cea907`

This record is provenance only. It does not override GitHub. After source files are imported, GitHub `main` is the sole development authority.
