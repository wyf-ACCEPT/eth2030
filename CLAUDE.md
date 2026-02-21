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

### Upgrade Timeline (implementation status)
- **Glamsterdam** (2026) ~99%: fast confirmation, ePBS, FOCIL, sparse blobpool (EIP-8070), native AA (EIP-7702+7701), BALs (EIP-7928), repricing (18 EIPs), EIP-7708 ETH logs, EIP-7685 requests, EIP-8141 frame transactions (APPROVE, TXPARAM opcodes), EIP-7918 blob base fee bound
- **Hogota** (2026-2027) ~85%: BPO blob schedules (BPO1/BPO2), EIP-7742 uncoupled blobs, EIP-7898 uncoupled execution payload, multidim gas (EIP-7706), gas limit schedule, binary tree, payload chunking, block-in-blobs, tx assertions, NTT precompile, SSZ transactions (EIP-6404), EIP-7807 SSZ blocks, EIP-7745 log index, EIP-7916 SSZ ProgressiveList, EIP-8077 announce nonce, encrypted mempool (threshold crypto + ordering), blob streaming, cell-level messages
- **I+** (2027) ~80%: binary trie, state access events, verkle gas, native rollups (EIP-8079), zkVM framework + STF executor + RISC-V CPU + zkISA bridge, VOPS (partial statelessness + complete validator), proof aggregation, post-quantum crypto (ML-DSA-65 real signer, algorithm registry), EIP-7251 MAX_EFFECTIVE_BALANCE, validator consolidation, CL proof generator, stateless execution, beam sync, blob futures, sample optimization
- **J+** (2027-2028) ~75%: STF in zkISA framework, light client (proof cache + sync committee), verkle state migration, variable-size blobs, Reed-Solomon blob reconstruction, block-in-blobs encoding
- **K+** (2028) ~80%: 6-sec slots (quick slots framework), SSF (single-slot finality), 4-slot epochs, 1-epoch finality, mandatory 3-of-5 proofs, canonical guest (RISC-V CPU), announce nonce, 1M attestations (parallel BLS, 64 subnets, batch verifier, scaler), CL proof circuits
- **L+** (2029) ~80%: endgame finality (BLS adapter with real pairing hooks), PQ attestations (Dilithium + PQ chain security), BPO blobs increase, validity-only state, attester stake cap, APS (committee selection), blob streaming, custody proofs, cell gossip, distributed block builder, 1 ETH includers, jeanVM aggregation (Groth16 ZK-circuit BLS), secret proposers (VRF election)
- **M+** (2029+) ~75%: fast L1 finality in seconds, PQ L1 hash-based (unified hash signer XMSS/WOTS+), gigagas L1 (work-stealing + sharded state + integration tests), canonical zkVM (RISC-V RV32IM + witness + proof backend), gas futures market (contracts + settlement), PQ blob commitments (MLWE lattice), sharded mempool, proof aggregation, real-time CL proofs
- **Longer term** (2030++) ~75%: distributed block building, VDF randomness (Wesolowski scheme), teragas L2 (bandwidth enforcer + streaming pipeline), private L1 shielded transfers (BN254 Pedersen commitments + nullifier set + ZK circuits), 51% attack auto-recovery, AA proof circuits (nonce/sig/gas constraints + Groth16), exposed zkISA (bridge with 9 op selectors), unified beacon state

### Gap Summary (see docs/GAP_ANALYSIS.md for full audit of 65 roadmap items)
- **COMPLETE**: 12 items (sparse blobpool, Glamsterdam repricing, BALs, binary tree, etc.)
- **FUNCTIONAL**: 48 items (most roadmap items have working implementations)
- **PARTIAL** (5 remaining):
  1. Fast L1 finality (#6) - Engine targets 500ms; needs real BLS + block execution
  2. Tech debt reset (#12) - Tracks deprecated fields only; needs automated migration
  3. PQ blobs (#31) - Lattice commitments work; Falcon/SPHINCS+ Sign() are keccak256 stubs
  4. Teragas L2 (#34) - Accounting framework; needs real bandwidth enforcement
  5. PQ transactions (#64) - Type 0x07 exists; ML-DSA-65 real, Falcon/SPHINCS+ stubs
- **MISSING**: 0
- **STUB**: 0

### Statistics
- **Packages**: 48 (all passing)
- **Source**: 978 files, ~308K LOC
- **Tests**: 913 files, ~391K LOC, 21,000+ tests
- **Total**: 1,891 files, ~699K LOC
- **EIPs**: 58 complete, 6 substantial
- **EF State Tests**: 9,884/36,126 passing (27.4%) via `pkg/core/eftest/`

### EIP Implementation Status
- **Complete** (58): EIP-150, EIP-152, EIP-196/197, EIP-1153, EIP-1559, EIP-2200, EIP-2537, EIP-2718, EIP-2929, EIP-2930, EIP-2935, EIP-3529, EIP-3540, EIP-4444, EIP-4762, EIP-4788, EIP-4844 (incl. KZG), EIP-4895, EIP-5656, EIP-6110, EIP-6404, EIP-6780, EIP-7002, EIP-7069, EIP-7251, EIP-7480, EIP-7547, EIP-7549, EIP-7594, EIP-7620, EIP-7685, EIP-7691, EIP-7698, EIP-7701, EIP-7702, EIP-7706, EIP-7742, EIP-7745, EIP-7807, EIP-7825, EIP-7898, EIP-7904, EIP-7916, EIP-7918, EIP-7928, EIP-7939, EIP-8024, EIP-8070, EIP-8077, EIP-8079, EIP-8141
- **Substantial** (6): EIP-7732 (ePBS), EIP-6800 (Verkle), EIP-7864 (binary tree), EIP-7805 (FOCIL), EIP-8025 (execution proofs), PQC (Dilithium3/Falcon512/SPHINCS+)

## Project Structure & Module Organization

- `pkg/` - Go module root (`github.com/eth2028/eth2028`, go.mod here)
  - `core/types/` - Core types: Header, Transaction (7 types incl. FrameTx, AATx), Receipt, Block, Account, SSZ encoding (EIP-6404/7807), SetCode auth (EIP-7702), tx assertions, EL requests (EIP-7685), log index (EIP-7745), EIP-4844 blob tx utilities, EIP-4895 withdrawals
  - `core/state/` - StateDB interface, in-memory and trie-backed implementations, access events (EIP-4762), stateless StateDB (witness-backed), state prefetcher
  - `core/vm/` - EVM interpreter, 164+ opcodes (incl. CLZ, DUPN/SWAPN/EXCHANGE, APPROVE, TXPARAM*, CURRENT_ROLE, ACCEPT_ROLE, EOF: EXTCALL/EXTDELEGATECALL/EXTSTATICCALL/RETURNDATALOAD/DATALOAD/DATALOADN/DATASIZE/DATACOPY/EOFCREATE/RETURNCONTRACT), 24 precompiles (incl. 9 BLS12-381, NTT, NII: modexp/field-mul/field-inv/batch-verify), gas tables, EIP-4762 statelessness gas, EIP-7708 ETH transfer logs, EIP-8141 frame opcodes, EIP-7701 AA opcodes, EOF container (EIP-3540)
  - `core/rawdb/` - FileDB with WAL, block/receipt/tx storage, EIP-4444 history expiry
  - `core/` - State transition, gas repricing (18 EIPs), multidim gas (EIP-7706/7999), blob gas (BPO1/BPO2 + EIP-7918 base fee floor), gas limit schedule, payload chunking, block-in-blobs, frame execution (EIP-8141), EIP-6110 deposits, EIP-7002 withdrawals, EIP-7825 gas cap, EIP-7691 blob schedule, gas futures market (contracts + settlement), gigagas infrastructure, MEV protection (commit-reveal ordering), chain config extensions (fork ordering, validation, rules)
  - `core/state/snapshot/` - State snapshots: layered diff/disk architecture, account/storage iterators, pruner
  - `core/state/pruner/` - State pruner with bloom filter reachability
  - `core/vops/` - Validity-Only Partial Statelessness: partial executor, validator, witness integration, complete VOPS validator (access lists, storage proofs)
  - `core/eftest/` - EF state test runner: JSON parser, fixture loader, batch execution against official test vectors (9,884/36,126 passing = 27.4%)
  - `rlp/` - RLP encoding/decoding
  - `consensus/` - Consensus layer: SSF (single-slot finality), quick slots (6s, 4-slot epochs), 1-epoch finality, EIP-7251 validator balance (2048 ETH max EB), consolidation, APS (committee selection), EIP-7549 attestations, PQ attestations (Dilithium), attester stake cap, endgame finality, UnifiedBeaconState (merged v1/v2/modern), finality BLS adapter, CL proof circuits (SHA-256 Merkle), jeanVM aggregation (Groth16 ZK-circuit BLS), PQ chain security (SHA-3 fork choice)
  - `ssz/` - SSZ encoding/decoding, merkleization, EIP-7916 ProgressiveList
  - `crypto/` - Keccak-256, secp256k1 ECDSA, BN254, BLS12-381, Banderwagon, IPA proofs, VDF (Wesolowski), shielded transfers (Pedersen commitments + ZK circuit), threshold crypto (Shamir SSS, Feldman VSS, ElGamal encryption)
  - `crypto/pqc/` - Post-quantum crypto: ML-DSA-65 (real lattice signer, FIPS 204), Dilithium3 (stub), Falcon512, SPHINCS+SHA256, unified hash signer (XMSS/WOTS+), hybrid signer, PQ algorithm registry, lattice-based blob commitments
  - `engine/` - Engine API server (V3-V7), forkchoice, payload building, ePBS builder API, EIP-7898 uncoupled payload, distributed block builder (registration, bids, auctions), Vickrey builder auction (second-price sealed-bid, slashing)
  - `trie/` - Binary Merkle tree (EIP-7864), SHA-256 hashing, proofs, MPT migration
  - `trie/bintrie/` - Binary Merkle trie (from go-ethereum), Get/Put/Delete/Hash/Commit, proofs
  - `bal/` - Block Access Lists (EIP-7928) for parallel execution
  - `witness/` - Execution witness (EIP-6800/8025), collector, verifier
  - `epbs/` - Enshrined Proposer-Builder Separation (EIP-7732): BuilderBid, PayloadEnvelope, auctions
  - `focil/` - Fork-Choice Enforced Inclusion Lists (EIP-7805): building, validation, compliance
  - `das/` - PeerDAS (EIP-7594): DataColumn, ColumnSidecar, sampling, custody, reconstruction, BLS12-381 field arithmetic, variable-size blobs, Reed-Solomon Lagrange interpolation, blob streaming, blob futures, custody proofs, cell gossip
  - `das/erasure/` - Reed-Solomon erasure coding for blob reconstruction
  - `rollup/` - Native rollups (EIP-8079): EXECUTE precompile, anchor contract
  - `zkvm/` - zkVM framework: guest programs, verification keys, prover backend, canonical guest (RISC-V executor, guest registry, precompile), STF executor (state transition proofs for zkISA), zkISA bridge (host ABI, EVM translation)
  - `proofs/` - Proof aggregation framework: ZKSNARK, ZKSTARK, IPA, KZG registry and aggregator, mandatory 3-of-5 proof system (prover assignment, submission, verification, penalties), AA proof circuits
  - `light/` - Light client: header sync, checkpoint store, verification, proof cache (LRU), sync committee verification, CL proof generator (state root, validator, balance proofs)
  - `txpool/` - Transaction pool with validation, replace-by-fee, eviction, EIP-8070 sparse blobpool (custody, WAL, price eviction), sharded mempool (consistent hashing)
  - `txpool/encrypted/` - Encrypted mempool: commit-reveal scheme, threshold decryption ordering
  - `p2p/` - P2P peer management, ETH wire protocol, discovery (V5 Kademlia DHT), ENR, enode, DNS discovery, Snap/1 protocol, Portal network (content DHT, Kademlia routing, history, state), gossip protocol (pub/sub, peer scoring, banning, deduplication), SetCode broadcast (EIP-7702 auth list gossip)
  - `sync/` - Full sync + snap sync pipeline, beam sync (stateless), trie sync (concurrent healing)
  - `rpc/` - JSON-RPC server, 50+ methods, filters, subscriptions, WebSocket, Beacon API (16 endpoints)
  - `eth/` - ETH protocol handler and codec, EIP-8077 announce nonce (ETH/72)
  - `node/` - Client node: config, lifecycle, subsystem integration
  - `verkle/` - Verkle tree types, key derivation, Pedersen commitments, state migration, StateDB adapter, witness generation
  - `log/` - Structured logging (JSON/text)
  - `metrics/` - Counters, gauges, histograms, Prometheus export, EWMA, meter, CPU tracker
- `cmd/eth2028/` - CLI binary with flags, signal handling
- `internal/testutil/` - Shared test utilities
- `refs/` - Reference submodules (30 total, read-only, do NOT modify). Main upstream: https://github.com/orgs/ethereum/repositories
  - **Ethereum specs**: consensus-specs, execution-specs, consensus-spec-tests, execution-spec-tests, execution-apis, beacon-APIs, builder-specs, EIPs, ERCs
  - **Ethereum core**: go-ethereum
  - **Cryptography**: blst (BLS12-381), circl (PQC: ML-DSA/ML-KEM/SLH-DSA), go-eth-kzg (KZG commitments), gnark (ZK proofs), gnark-crypto (ZK curves), c-kzg-4844 (C-based KZG), go-ipa (Verkle IPA proofs), go-verkle
  - **Utilities**: eth-utils, web3.py
  - **Governance**: pm, eip-review-bot, iptf-pocs
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
cd pkg && go test ./epbs/...
cd pkg && go test ./focil/...
cd pkg && go test ./das/...
cd pkg && go test ./rollup/...
cd pkg && go test ./zkvm/...
cd pkg && go test ./trie/bintrie/...
cd pkg && go test ./core/vops/...
cd pkg && go test ./crypto/pqc/...
cd pkg && go test ./light/...
cd pkg && go test ./proofs/...
cd pkg && go test ./txpool/encrypted/...
cd pkg && go test ./verkle/...
cd pkg && go test ./consensus/...
cd pkg && go test ./ssz/...

# Run tests with verbose output
cd pkg && go test -v ./...

# Run fuzz tests (38+ fuzz targets across 8 packages)
cd pkg && go test -fuzz=FuzzDecode ./rlp/ -fuzztime=30s
cd pkg && go test -fuzz=FuzzKeccak256 ./crypto/ -fuzztime=30s
cd pkg && go test -fuzz=FuzzArithmeticOps ./core/vm/ -fuzztime=30s
cd pkg && go test -fuzz=FuzzTransactionRLPRoundtrip ./core/types/ -fuzztime=30s
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

## Reusable Libraries (refs/ submodules & ecosystem)

When implementing missing functionality, check these production-grade Go libraries first before writing from scratch. All are in `refs/` or available as Go module dependencies.

### How to find reusable code for missing libs
1. Check `refs/` submodules first (already cloned, read-only): `ls refs/` then explore relevant dirs
2. Search GitHub orgs: ethereum, consensys, ethpandaops, prysmaticlabs, cloudflare, supranational
3. Check go-ethereum's `go.sum` for vetted dependencies: `grep <lib> refs/go-ethereum/go.sum`
4. Validate license compatibility (Apache-2.0, MIT, BSD-3 are OK; GPL/AGPL are NOT)
5. Prefer pure-Go (no CGO) when possible for simpler builds

### Tier 1 -- Core (in refs/, production-ready)

| Library | Module | CGO | License | Use for |
|---------|--------|-----|---------|---------|
| **blst** | `github.com/supranational/blst` | Yes | Apache-2.0 | BLS12-381 signatures, pairing, aggregate verify |
| **go-eth-kzg** | `github.com/crate-crypto/go-eth-kzg` | No | Apache-2.0 | KZG commitments (EIP-4844 + EIP-7594 PeerDAS) |
| **c-kzg-4844** | `github.com/ethereum/c-kzg-4844/v2` | Yes | Apache-2.0 | KZG commitments (C, audited, faster) |
| **gnark** | `github.com/consensys/gnark` | No | Apache-2.0 | ZK-SNARK circuits (Groth16, PLONK) |
| **gnark-crypto** | `github.com/consensys/gnark-crypto` | No | Apache-2.0 | BLS12-381/BN254 field arithmetic, curves |
| **circl** | `github.com/cloudflare/circl` | No | BSD-3 | PQC: ML-DSA (FIPS 204), ML-KEM, SLH-DSA |
| **go-verkle** | `github.com/ethereum/go-verkle` | No | Unlicense | Verkle tree (Insert/Get/Commit/Prove) |
| **go-ipa** | `github.com/crate-crypto/go-ipa` | No | MIT/Apache | IPA proofs, Banderwagon, Bandersnatch |

### Tier 2 -- Strong candidates

| Library | Module | CGO | License | Use for |
|---------|--------|-----|---------|---------|
| **fastssz** | `github.com/ferranbt/fastssz` | No | MIT | SSZ codec + code generator |
| **uint256** | `github.com/holiman/uint256` | No | BSD-3 | 256-bit math (replaces big.Int) |
| **gohashtree** | `github.com/prysmaticlabs/gohashtree` | No | MIT | Fast SHA256 for Merkle trees (2-4x) |
| **go-libp2p** | `github.com/libp2p/go-libp2p` | No | MIT | CL gossipsub, peer discovery |
| **go-eth2-client** | `github.com/attestantio/go-eth2-client` | No | Apache-2.0 | Beacon API client |
| **zrnt** | `github.com/protolambda/zrnt` | No | MIT | Executable consensus spec (reference) |
| **liboqs-go** | `github.com/open-quantum-safe/liboqs-go` | Yes | MIT | Broader PQC algorithm coverage |

### Current gaps where libs would help
- **BLS pairing**: pkg/crypto/ uses pure-Go placeholder → replace with `blst` for correct aggregate verify
- **KZG proofs**: pkg/crypto/kzg.go is placeholder → replace with `go-eth-kzg`
- **ML-DSA validation**: pkg/crypto/pqc/mldsa_signer.go has custom impl → validate against `circl/sign/mldsa`
- **ZK circuits**: pkg/zkvm/ has simulated proofs → integrate `gnark` for real Groth16/PLONK
- **Verkle IPA**: pkg/crypto/ipa.go is placeholder → replace with `go-ipa`

## Agent-Specific Notes

- When answering questions, respond with high-confidence answers only: verify in code; do not guess.
- **Multi-agent safety:** do **not** create/apply/drop `git stash` entries unless explicitly requested.
- **Multi-agent safety:** do **not** switch branches unless explicitly requested.
- **Multi-agent safety:** scope commits to your own changes only.
- **Multi-agent safety:** when you see unrecognized files, keep going; focus on your changes.
