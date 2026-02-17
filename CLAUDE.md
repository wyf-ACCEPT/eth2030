# Repository Guidelines

- Project: eth2028 -- Ethereum client targeting the 2028 roadmap
- GitHub issues/comments/PR comments: use literal multiline strings or `-F - <<'EOF'` for real newlines; never embed "\\n".

## 2028 Roadmap (L1 Strawmap by EF Protocol)

Source: EF Architecture team (Ansgar, Barnabe, Francesco, Justin), updated Feb 2026.
Live at strawmap.org. Three layers, each with sub-tracks:

### Consensus Layer (CL)
- **Latency**: fast confirmation -> single-slot finality -> 1-epoch finality -> 4-slot epochs -> 6-sec slots (K+) -> endgame finality in seconds (M+)
- **Accessibility**: ePBS -> FOCIL -> modernized beacon state -> beacon & blob sync revamp -> 1MiB attestor cap -> KPS -> rich data smart -> LETHE insulation -> post quantum attestations -> 1M subaccounts, distributed block building
- **Cryptography**: post quantum custody replacer -> post quantum signature share -> real-time CL proofs -> post quantum L1 hash-based (M+) -> VDF, secure prequorum

### Data Layer (DL)
- **Throughput**: peerDAS -> EIP-7702 precompile -> blob throughput increase -> local blob reconstruction -> 3-RPO slots increase -> L-RPO blob increase -> post quantum blobs -> teradata L2, proof custody
- **Types**: blob streaming -> short-dated blob futures -> decrease sample size -> post quantum custody -> forward-cast blobs

### Execution Layer (EL)
- **Throughput**: conversion repricing -> natural gas limit -> access gas limit -> multidimensional pricing -> block in blobs -> mandatory 3-of-5 proofs -> canonical guest -> canonical zxVM -> long-dated gas futures -> shared mempools -> gigas L1 (1 Ggas/sec)
- **Sustainability**: BALS -> Hogota repricing -> payload shrinking -> announce binary tree -> verkle/portal state -> advance state -> native rollups -> exposed ELSA -> proofs
- **EVM**: native AA -> more precompiles in eWASM -> STF in eRISC -> native rollups -> proof aggregation -> post quantum transactions -> exposed ELSA
- **Cryptography**: NII precompile(s) -> encrypted mempool

### Upgrade Timeline
- **Glamsterdan** (2026): fast confirmation, ePBS, FOCIL, peerDAS, native AA, BALS
- **Hogota** (2026-2027): blob throughput, local blob reconstruction, repricing
- **I+** (2027): 1-epoch finality, post quantum custody
- **J+** (2027-2028): 4-slot epochs, precompiles in eWASM, STF in eRISC
- **K+** (2028): 6-sec slots, mandatory proofs, canonical guest
- **L+** (2029): endgame finality, LETHE insulation, post quantum attestations
- **M+** (2029+): fast L1 finality in seconds, post quantum L1, gigas L1, canonical zxVM
- **Longer term** (2030++): distributed block building, VDF, teradata L2, private L1 shielded compute

## Project Structure & Module Organization

- `pkg/` - Go module root (`github.com/eth2028/eth2028`, go.mod here)
  - `core/types/` - Core types: Header, Transaction, Receipt, Block, Account
  - `core/state/` - StateDB interface and in-memory implementation
  - `core/vm/` - EVM interpreter, opcodes, stack, memory, gas
  - `core/rawdb/` - Raw database access (planned)
  - `rlp/` - RLP encoding/decoding
  - `crypto/` - Keccak-256, secp256k1 ECDSA
  - `engine/` - Engine API types (V1-V5), forkchoice, payload status
  - `bal/` - Block Access Lists (EIP-7928)
  - `witness/` - Execution witness and ZK proof types
  - `txpool/` - Transaction pool (planned)
  - `p2p/` - P2P networking (planned)
- `cmd/eth2028/` - Main binary entry point (planned)
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
