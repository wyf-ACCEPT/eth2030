---
name: team
description: Launch a team of 4 parallel agents to add depth across multiple packages. Each agent creates 2 source files + 2 test files in non-overlapping packages. All files build and pass tests before committing.
---

# Agent Team Skill

## Overview

Launch a coordinated team of 4 background agents to add implementation depth across the eth2028 codebase. Each agent works on non-overlapping packages to avoid conflicts.

## Inputs

- Target: package names or "auto" to pick packages needing depth
- Count: number of files per agent (default: 4 = 2 source + 2 test)
- Focus: "gap" (fix PARTIAL gaps), "depth" (add new implementations), or "both"

## Workflow

1. **Analyze** - Check git status, identify packages needing depth, review gap analysis
2. **Plan** - Assign non-overlapping packages to 4 agents (2 packages per agent)
3. **Launch** - Start 4 background agents in parallel via Task tool with `run_in_background: true`
4. **Monitor** - Wait for all agents to complete, check for build/test failures
5. **Verify** - Run `go build ./...` and `go test ./...` on all affected packages
6. **Commit** - Stage new files, commit with descriptive message, update README stats
7. **Push** - Push to remote

## Agent Assignment Rules

- Each agent gets exactly 2 packages (non-overlapping with other agents)
- Each agent creates 2 source files + 2 test files (4 files total)
- Agents ONLY create NEW files, NEVER modify existing files
- All files must be under 500 lines
- All files must build and pass tests before the agent reports completion
- Each test file must have 15+ test functions
- Use `subagent_type: "general-purpose"` and `mode: "bypassPermissions"`

## Package Priority (2026-2028 roadmap)

High priority (Glamsterdam/Hogota 2026-2027):
- `consensus/` - SSF, quick slots, attestations, finality
- `core/types/` - Transaction types, SSZ encoding
- `core/vm/` - EVM opcodes, precompiles, gas tables
- `core/state/` - StateDB, access events, prefetcher
- `das/` - PeerDAS, blob sampling, custody
- `engine/` - Engine API, payload building, forkchoice
- `p2p/` - Gossip, discovery, sync protocols

Medium priority (I+/J+ 2027-2028):
- `zkvm/` - RISC-V CPU, STF executor, proof backend
- `proofs/` - Proof aggregation, mandatory proofs
- `rollup/` - Native rollups, anchor contracts
- `trie/` - Binary trie, MPT migration
- `verkle/` - Verkle tree, state migration

Lower priority (K+/L+ 2028+):
- `crypto/` - BLS, KZG, PQC, threshold
- `txpool/` - Validation, sharding, encrypted
- `light/` - Header sync, proof cache
- `witness/` - Execution witness, collector

## Commit Format

```
depth: add N files across M packages for <package-list>

New implementations:
- <package>: <description> (<key features>)
- <package>: <description> (<key features>)
...
```

## Example Invocation

User: `/team`
Agent: Launches 4 agents creating 16 files across 8 packages, waits, verifies, commits, pushes.

User: `/team consensus das engine`
Agent: Focuses agents on specified packages.

## Guardrails

- Never modify existing files (agents create only)
- Never switch branches or create stashes
- Always verify build + tests pass before committing
- Scope commits to new files only
- Check for name collisions with existing types/functions before creating files
- Maximum 500 LOC per file
