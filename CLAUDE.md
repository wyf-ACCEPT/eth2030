# Repository Guidelines

- Project: eth2028 -- Ethereum client targeting the 2028 roadmap
- GitHub issues/comments/PR comments: use literal multiline strings or `-F - <<'EOF'` for real newlines; never embed "\\n".

## 2028 Roadmap (L1 Strawmap by EF Protocol)

Source: EF Architecture team (Ansgar, Barnabe, Francesco, Justin), updated Feb 2026.
Live at strawmap.org. Three layers, each with sub-tracks:

### Consensus Layer (CL)
- **Latency**: fast confirmation -> quick slots -> 1-epoch finality -> 4-slot epochs -> 6-sec slots (K+) -> endgame finality -> fast L1 finality in seconds (M+)
- **Accessibility**: ePBS -> FOCIL -> modernized beacon specs -> beacon & lean specs merge -> attester stake cap -> 128K attester cap -> APS -> 1 ETH includers -> tech debt reset -> post quantum attestations -> jeanVM aggregation -> 1M attestations/slot -> 51% attack auto-recovery -> distributed block building
- **Cryptography**: post quantum pubkey registry -> post quantum available chain -> real-time CL proofs -> post quantum L1 hash-based (M+) -> VDF randomness -> secret proposers

### Data Layer (DL)
- **Throughput**: sparse blobpool -> cell-level messages -> EIP-7702 broadcast -> Hogota BPO blobs increase -> local blob reconstruction -> decrease sample size -> J* BPO blobs increase -> L* BPO blobs increase -> teragas L2 (1 Gbyte/sec)
- **Types**: blob streaming -> short-dated blob futures -> post quantum blobs -> variable-size blobs -> proofs of custody

### Execution Layer (EL)
- **Throughput**: Glamsterdam repricing -> optional proofs -> Hogota repricing -> 3x/year gas limit -> multidimensional pricing -> payload chunking -> block in blobs -> announce nonce -> mandatory 3-of-5 proofs -> canonical guest -> canonical zkVM -> long-dated gas futures -> sharded mempool -> gigagas L1 (1 Ggas/sec)
- **Sustainability**: BALs -> binary tree -> announce nonce -> validity-only partial state -> endgame state
- **EVM**: native AA -> misc purges -> transaction assertions -> NTT precompile(s) -> precompiles in zkISA -> STF in zkISA -> native rollups -> proof aggregation -> exposed zkISA -> AA proofs
- **Cryptography**: encrypted mempool -> post quantum transactions -> private L1 shielded transfers

### Upgrade Timeline
- **Glamsterdam** (2026): fast confirmation, ePBS, FOCIL, sparse blobpool, native AA, BALs, repricing, optional proofs
- **Hogota** (2026-2027): blob throughput increase, local blob reconstruction, repricing, binary tree
- **I+** (2027): 1-epoch finality, post quantum custody, quick slots
- **J+** (2027-2028): 4-slot epochs, precompiles in zkISA, STF in zkISA, BPO blobs increase
- **K+** (2028): 6-sec slots, mandatory 3-of-5 proofs, canonical guest, announce nonce
- **L+** (2029): endgame finality, post quantum attestations, BPO blobs increase, validity-only state
- **M+** (2029+): fast L1 finality in seconds, post quantum L1, gigagas L1, canonical zkVM
- **Longer term** (2030++): distributed block building, VDF randomness, teragas L2, private L1 shielded transfers

### EIP Implementation Status
- **Complete**: EIP-1559, EIP-2718, EIP-2929, EIP-2930, EIP-2200, EIP-2537, EIP-3529, EIP-4844, EIP-4895, EIP-5656, EIP-7685, EIP-1153, EIP-150, EIP-152, EIP-196/197, EIP-7702, EIP-7904, EIP-7623, EIP-7928, EIP-2935, EIP-4788, EIP-7706, EIP-7547, EIP-4444, EIP-4762
- **Substantial**: EIP-7732 (ePBS: builder types, registry, bid management, commitment/reveal, API), EIP-6800 (Verkle: Banderwagon curve, IPA proofs, Pedersen commitments, types+keys), EIP-7864 (binary tree: SHA-256, iterator, proofs, MPT migration)
- **Partial**: EIP-8025 (witness collector only)
- **Planned**: EIP-8079 (EXECUTE precompile), EIP-8007 (Glamsterdam gas repricings), EIP-8037 (state growth/access separation), EIP-7778 (remove gas refunds), EIP-8125 (temporary storage)

## Project Structure & Module Organization

- `pkg/` - Go module root (`github.com/eth2028/eth2028`, go.mod here)
  - `core/types/` - Core types: Header, Transaction (5 types), Receipt, Block, Account
  - `core/state/` - StateDB interface, in-memory and trie-backed implementations
  - `core/vm/` - EVM interpreter, 140+ opcodes, 19 precompiles (incl. 9 BLS12-381), gas tables, EIP-4762 statelessness gas
  - `core/rawdb/` - FileDB with WAL, block/receipt/tx storage, EIP-4444 history expiry
  - `rlp/` - RLP encoding/decoding
  - `crypto/` - Keccak-256, secp256k1 ECDSA, BN254, BLS12-381, Banderwagon, IPA proofs
  - `engine/` - Engine API server (V3-V6), forkchoice, payload building, ePBS builder API
  - `trie/` - Binary Merkle tree (EIP-7864), SHA-256 hashing, proofs, MPT migration
  - `bal/` - Block Access Lists (EIP-7928) for parallel execution
  - `witness/` - Execution witness (EIP-6800/8025), collector, verifier
  - `txpool/` - Transaction pool with validation, replace-by-fee, eviction
  - `p2p/` - P2P peer management, ETH wire protocol, discovery
  - `sync/` - Full sync + snap sync pipeline
  - `rpc/` - JSON-RPC server, 50+ methods, filters, subscriptions, WebSocket
  - `eth/` - ETH protocol handler and codec
  - `node/` - Client node: config, lifecycle, subsystem integration
  - `verkle/` - Verkle tree types and key derivation (stub)
  - `log/` - Structured logging (JSON/text)
  - `metrics/` - Counters, gauges, histograms, Prometheus export
- `cmd/eth2028/` - CLI binary with flags, signal handling
- `internal/testutil/` - Shared test utilities
- `refs/` - Reference submodules (read-only, do NOT modify)
  - **Ethereum core**: go-ethereum, EIPs, ERCs, consensus-specs, execution-apis, execution-spec-tests, beacon-APIs, builder-specs
  - **Utilities**: eth-utils, go-verkle, web3.py
  - **Governance**: pm (project management), eip-review-bot, iptf-pocs
  - **Devops**: benchmarkoor, benchmarkoor-tests, ethereum-package, erigone, xatu, execution-processor, consensoor
- `tools/` - Research and data fetching tools
- `data/` - Downloaded research data (gitignored)
- `docs/` - Design docs, roadmap, deep-dive
- `.claude/` - Claude Code skills and settings

## Build, Test, and Development Commands

```bash
# Build all packages
cd pkg && go build ./...

# Run all tests
cd pkg && go test ./...

# Run tests for a specific package
cd pkg && go test ./core/types/...
cd pkg && go test ./rlp/...
cd pkg && go test ./crypto/...
cd pkg && go test ./engine/...
cd pkg && go test ./bal/...
cd pkg && go test ./witness/...
cd pkg && go test ./core/state/...
cd pkg && go test ./core/vm/...

# Run tests with verbose output
cd pkg && go test -v ./...

# Run fuzz tests (RLP decoder)
cd pkg && go test -fuzz=FuzzDecode ./rlp/ -fuzztime=30s
```

NOTE: The go.mod is in pkg/ (not project root) to avoid module conflicts with refs/ submodules.

## Coding Style & Naming Conventions

- Prefer strict typing; avoid loose types.
- Add brief code comments for tricky or non-obvious logic.
- Keep files concise; aim for under ~500 LOC.

## Testing Guidelines

- Naming: match source names with corresponding test files.
- Run tests before pushing when you touch logic.

## Commit Rules

- **Never add Co-Authored-By lines for Claude or any AI assistant in commits.** All commits are authored solely by the human committer.
- Follow concise, action-oriented commit messages (e.g., `evm: add EOF support`).
- Group related changes; avoid bundling unrelated refactors.
- Lint/format churn: auto-resolve formatting-only diffs without asking.

## Security & Configuration Tips

- Never commit or publish real private keys, mnemonics, or live configuration values.
- Use obviously fake placeholders in docs, tests, and examples.
- Environment variables for secrets; use cloud secrets managers in production.

## Agent-Specific Notes

- When answering questions, respond with high-confidence answers only: verify in code; do not guess.
- **Multi-agent safety:** do **not** create/apply/drop `git stash` entries unless explicitly requested.
- **Multi-agent safety:** do **not** switch branches unless explicitly requested.
- **Multi-agent safety:** scope commits to your own changes only.
- **Multi-agent safety:** when you see unrecognized files, keep going; focus on your changes.
