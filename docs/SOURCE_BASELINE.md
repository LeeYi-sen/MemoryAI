# Source Baseline Policy

Repository: `LeeYi-sen/MemoryAI` (private)

## Mandatory development rule

Every MemoryAI change must begin from the latest `main` HEAD of this repository. Local archives, chat attachments, `/mnt/data` work directories, or older release packages are never authoritative once a corresponding source revision exists in GitHub.

Required workflow:

1. Read/sync latest `main` HEAD.
2. Create the code change from that HEAD only.
3. Run direct source/runtime tests and architecture gates; development does not build release binaries.
4. Commit the tested source change back to GitHub.
5. Use the resulting new HEAD as the next development baseline.

## Repository artifact policy

The repository directly tracks canonical source plus `data/Memory.mem`. Compiled Kernel/Gateway binaries, archives, compressed bootstrap payloads, base64 chunks and release packages are generated artifacts and must not be committed.

`Kernel/src/kernel.go` and `Kernel/current-required-structures.json` are canonical Git source inputs. The historical gzip/base64 bootstrap reconstruction pipeline is retired. Release binary packaging is outside the current development phase. Development commits canonical source plus the tracked `data/Memory.mem` only.

## Recovered pre-GitHub baseline

The last complete pre-repository source artifact available at repository initialization was MemoryAI v27 repair_73_92. The local source-only recovery archive was:

- size: `199768` bytes
- SHA-256: `b42761d4d3bb8cd10da644c31df92f9b9fe3b569a1d119e71f22e2d9d5cea907`

This record is provenance only. It does not override GitHub. After source files are imported, GitHub `main` is the sole development authority.
