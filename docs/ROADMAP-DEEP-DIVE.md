# Ethereum 2028 Client Roadmap: Deep Dive

> Synthesized from EIP specs, consensus-specs repo, go-ethereum codebase analysis,
> ethereum-magicians.org discussions, ethresear.ch research posts, and web sources.
> Last updated: 2026-02-17.

---

## Table of Contents

1. [Executive Summary](#executive-summary)
2. [2026 Forks: Glamsterdam & Hegota](#2026-forks-glamsterdam--hegota)
3. [Beast Mode -- Raw Performance](#beast-mode----raw-performance)
   - [Parallel Execution (EIP-7928)](#parallel-execution-eip-7928)
   - [ePBS (EIP-7732)](#epbs-eip-7732)
   - [PeerDAS (EIP-7594)](#peerdas-eip-7594)
   - [Native Rollups & EXECUTE Precompile (EIP-8079)](#native-rollups--execute-precompile-eip-8079)
   - [Giga-Gas & Real-Time ZK Proving](#giga-gas--real-time-zk-proving)
   - [RISC-V & LeanVM](#risc-v--leanvm)
4. [Lean Mode -- Efficiency](#lean-mode----efficiency)
   - [History Expiry (EIP-4444)](#history-expiry-eip-4444)
   - [Beam Chain / Lean Consensus](#beam-chain--lean-consensus)
5. [Fort Mode -- Quantum Resistance](#fort-mode----quantum-resistance)
   - [ML-DSA Signatures](#ml-dsa-signatures)
   - [BLS Replacement & leanSig](#bls-replacement--leansig)
6. [Decentralization](#decentralization)
   - [1 ETH Solo Staking (EIP-7251)](#1-eth-solo-staking-eip-7251)
7. [Fast Finality](#fast-finality)
   - [Single Slot Finality / 3-Slot Finality](#single-slot-finality--3-slot-finality)
8. [Consensus Specs Fork Progression](#consensus-specs-fork-progression)
9. [Go-Ethereum Architecture for Client Developers](#go-ethereum-architecture-for-client-developers)
10. [Community Discussion Summary](#community-discussion-summary)
11. [Implementation Priority Matrix](#implementation-priority-matrix)

---

## Executive Summary

The [L1 Strawmap](https://strawmap.org/) executes a transformation from a 30 TPS chain into a
1 Ggas/s L1 powering a 1M+ TPS ecosystem. The strategy has five pillars:

| Pillar | Target | Key Mechanism |
|--------|--------|---------------|
| **Beast Mode** | 10,000+ TPS on L1 | Parallel execution, real-time ZK proving, native rollups |
| **Lean Mode** | Run nodes on phones | Binary trees, history expiry, ZK-native consensus |
| **Fort Mode** | Quantum-safe by 2029 | ML-DSA signatures, XMSS+STARK aggregation |
| **Decentralization** | 1 ETH solo staking | MaxEB increase + ZK signature aggregation |
| **Fast Finality** | 12-second irreversibility | 3-Slot Finality protocol replacing Gasper |

The execution is phased across named forks:

```
2025: Pectra (shipped) -> Fusaka
2026: Glamsterdam (H1) -> Hegota (H2)
2027: Beam Chain Phase + The Purge
2028: BEAST MODE + FORT MODE
```

---

## 2026 Forks: Glamsterdam & Hegota

### Glamsterdam (Mid-2026, Target: June)

The first major 2026 fork focuses on execution scalability and PBS reform.

**Confirmed/Leading EIPs:**
- **EIP-7732** (ePBS): Enshrined proposer-builder separation
- **EIP-7928** (Block Access Lists): Parallel EVM execution
- **EIP-7904**: Benchmarked gas repricing to align resource costs

**Gas Limit Trajectory:**
- Current: 60M gas (post-Fusaka)
- H1 2026 target: 100M gas (Tomasz Stanczak, EF co-director)
- Post-Glamsterdam: 200M gas (after ePBS + parallel execution)
- Aspirational: 300M gas by end of 2026

**Community Status (from ethereum-magicians.org):**
- EIP-7928 is the most actively discussed 2030 roadmap item (35+ forum posts)
- 12+ breakout sessions documented for Block Access Lists
- Glamsterdam stakeholder feedback thread: 26 posts

### Hegota (Late 2026)

**Considered Features (EIP-8081 meta-EIP, draft):**
- **EIP-7805** (FOCIL): Fork-choice enforced inclusion lists for censorship resistance
- **LUCID**: Encrypted mempool (4 forum posts)
- **2D PeerDAS**: Partial reconstruction for data availability
- **EIP-7782**: 2x shorter slot times

**Status:** Early candidate selection phase (Jan-Feb 2026 proposal window).

---

## Beast Mode -- Raw Performance

### Parallel Execution (EIP-7928)

**Status:** Draft | **Fork:** Glamsterdam | **Category:** Core

Block-Level Access Lists enable parallel EVM execution by recording all state
accesses per block, allowing non-conflicting transactions to execute concurrently.

**Technical Specification:**
- Adds `block_access_list_hash` to block header (Keccak-256 of RLP-encoded BAL)
- Records per transaction: addresses, storage slots, balance/nonce/code changes
- `BlockAccessIndex`: 0 for pre-execution, 1..n for transactions, n+1 for post-execution
- Average BAL size: ~72.4 KiB (compressed)
- Gas validation split into pre-state and post-state phases

**Engine API Changes:**
- `ExecutionPayloadV4` with `blockAccessList` field
- `engine_newPayloadV5` and `engine_getPayloadV6` methods

**Execution Model:**
```
Without BAL: sequential IO + sequential EVM
With BAL:    parallel IO + parallel EVM
```

**Consensus Specs (from `_features/eip7928/`):**
- `BlockAccessList` type added to `ExecutionPayload`
- Beacon chain spec tracks BAL in execution payload

**Client Implementation Notes:**
- Must track all state accesses (addresses, storage slots, balance/nonce/code changes)
- Strict lexicographic ordering for addresses, ascending for indices
- geth's current `state_prefetcher.go` already does parallel prefetching (4/5 of CPU cores)
  but actual execution remains sequential -- EIP-7928 makes execution parallel too

---

### ePBS (EIP-7732)

**Status:** Draft | **Fork:** Glamsterdam (Gloas in consensus specs) | **Category:** Core

Enshrined Proposer-Builder Separation moves PBS from MEV-Boost's off-chain relay
infrastructure into the consensus protocol itself.

**Technical Specification:**
- Removes `ExecutionPayload` from `BeaconBlockBody`
- Replaces with `SignedExecutionPayloadBid` (commitment scheme)
- Introduces **Builders** as staked entities (minimum 1 ETH)
- Creates **Payload Timeliness Committee (PTC)**: 512 validators attesting to payload availability
- Defers execution validation to next beacon block (6-9 second window)
- Trustless payment: builder's value deducted from stake, proposer receives via withdrawals

**Consensus Specs (from `specs/gloas/`):**
- `beacon-chain.md`: 1,602 lines -- builder registry, payload bids, PTC
- `builder.md`: 271 lines -- builder lifecycle
- `fork-choice.md`: 879 lines -- handling Full/Empty/Skipped slots

**Why It Matters for ZK:**
- Decouples block validation from execution
- Gives attesters more time to receive ZK proofs
- Gives provers more time to generate proofs
- Justin Drake estimates ~10% of validators will switch to ZK verification after ePBS
- Enables further gas limit increases

**Community Status:** 11 discussions on ethereum-magicians.org, 13+ breakout sessions.
ePBS-FOCIL compatibility discussion (July 2025) explores integration.

---

### PeerDAS (EIP-7594)

**Status:** Final | **Fork:** Fulu (consensus specs) | **Category:** Core

Peer Data Availability Sampling scales blob capacity without requiring all nodes
to download all data.

**Technical Specification:**
- One-dimensional erasure coding extension on blobs
- Cells: smallest authenticated data units
- Columns distributed on gossip subnets based on node ID
- Nodes need only 50% of columns to validate
- Recovery via peer requests for missing columns
- 6 blobs per transaction limit

**Consensus Specs (from `specs/fulu/`):**
- `das-core.md`: 319 lines -- DataColumnSidecar, custody sampling
- `polynomial-commitments-sampling.md`: 817 lines -- KZG cell proofs
- `p2p-interface.md`: 663 lines -- column gossip and reconstruction

**Blob Schedule (Fulu):**
- Dynamic scaling: Epochs 412672 (15 blobs), 419072 (21 blobs max)
- Future: potentially 72+ blobs per block

**Execution API (from `execution-apis/src/engine/osaka.md`):**
- `engine_getPayloadV5`: Returns `BlobsBundleV2` with cell proofs
- `engine_getBlobsV2/V3`: Cell-based blob retrieval
- `CELLS_PER_EXT_BLOB = 128`

---

### Native Rollups & EXECUTE Precompile (EIP-8079)

**Status:** Draft | **Target:** 2028 (Phase 3 of ZK rollout)

Native Rollups use Ethereum's own execution environment for rollup state transition
verification, eliminating custom proof systems.

**The EXECUTE Precompile:**
```
EXECUTE(pre_state_root, post_state_root, trace, gas_used) -> bool
```
- `trace` contains L2 transactions + stateless Merkle proofs
- Performs recursive call to Ethereum's execution environment
- Returns `true` if stateless execution produces the expected state root
- "Bug-free by construction" -- any bug is also an L1 bug, fixed by hard forks

**Key Technical Details:**
- Blob-carrying transactions blocked within native rollup execution
- `burned_fees` 64-bit field added to block headers
- Proof-carrying transaction type (`PROOF_TX_TYPE`)
- Separate EIP-1559-style cumulative gas target to prevent L1 overload

**Connection to L1 ZK Proving:**
The same prover infrastructure that verifies L1 blocks powers EXECUTE for L2
verification. L1-zkEVM first breakout workshop held February 11, 2026.

**Community Discussion (ethresear.ch):**
- "Native rollups -- superpowers from L1 execution" (42 posts, most active topic)
- "Native Rollup: a new paradigm of zk-Rollup" (8 posts)
- "Native Rollup for 3SF" (1 post, linking native rollups to finality)

---

### Giga-Gas & Real-Time ZK Proving

**Target:** 1 Ggas/s on L1 (~10,000 TPS) by 2028

The shift from "Execute Blocks" to "Verify Proofs" enables dramatic gas limit increases.

**Real-Time Proving Breakthrough:**
- ZK proof for a full Ethereum block in <12 seconds (slot time)
- Practical target: 10 seconds (allowing 1.5s for propagation)
- SP1 Hypercube (Succinct): First to prove Ethereum in real time (May 2025)
  - 93% of blocks proved in <12s across 200 GPUs
  - Proving time: 16 min -> 16 sec (2025), costs dropped 45x
- ZisK: 7.4 seconds using 24 GPUs
- Multiple zkVMs now prove 99% of mainnet blocks in <10 seconds

**EF Standards for Proofs:**
- Security: 128 bits (minimum 100 initially)
- Max proof size: 300 KiB
- No trusted setups
- Hardware CapEx: <$100K USD
- Power: <10 kW (residential feasibility)
- All code fully open source

**Phased Rollout:**

| Phase | Year | Description |
|-------|------|-------------|
| 0 | 2025 | Optional ZK clients; altruistic proving |
| 1 | 2026 | Delayed proofs; ~10% validators switch to ZK; gas limit increases |
| 2 | 2027 | Mandatory proofs via fork-choice rule; rational proving incentives |
| 3 | 2028 | Enshrined proofs; EXECUTE precompile; native rollups |

**Consensus Specs (EIP-8025, from `_features/eip8025/`):**
- `beacon-chain.md`: ExecutionProof containers
- `proof-engine.md`: Proof verification engine
- `prover.md`: Prover interface spec
- Max proof size: 300 KiB
- `ProofType` and `ExecutionProof` SSZ containers

**Community Discussion (ethresear.ch):**
- 6 topics on "giga gas ZK proving"
- Lower forum visibility but high development activity

---

### RISC-V & LeanVM

**Status:** Research / Proposal phase

**Vitalik's RISC-V Proposal (April 2025):**
- ~59% of zkEVM proving time spent on EVM interpretation overhead
- RISC-V could yield 50-100x proving efficiency improvement
- Phased approach:
  1. New precompiles written in RISC-V
  2. RISC-V as contract deployment option alongside EVM
  3. Replace EVM precompiles with RISC-V implementations
- Backward compatibility via EVM-in-RISC-V interpreter

**LeanVM (Vitalik, September 2025):**
- Minimal zkVM: 4-instruction ISA
- Uses multilinear STARKs and logup lookups
- Optimized for XMSS signature aggregation and proof recursion
- Part of Beam Chain "Lean Cryptography" workstream

**Community Discussion (ethresear.ch):**
- "Why RISC-V Is Not a Good Choice for an L1 Delivery ISA, and Why WASM Is a Better One"
  (14 posts) -- active debate on ISA selection
- Community split between RISC-V and WASM advocates

---

## Lean Mode -- Efficiency

### History Expiry (EIP-4444)

**Status:** Stagnant (spec) | **Category:** Networking

Prunes historical data older than one year from the P2P network.

**Technical Specification:**
- `HISTORY_PRUNE_EPOCHS = 82125` (~1 Earth year)
- Clients SHOULD NOT serve headers/bodies/receipts older than threshold
- Clients MAY locally prune historical data
- Requires Checkpoint Sync via Weak Subjectivity Checkpoints

**Alternative Data Sources:**
Portal Network, The Graph, IPFS, torrent magnet links

**Community Discussion:** 46 posts on ethereum-magicians.org (highly engaged).

---

### Beam Chain / Lean Consensus

**Status:** Specification phase (2025-2026) | **Target:** Mainnet 2029-2030

Complete redesign of Ethereum's consensus layer, proposed by Justin Drake (Devcon
Bangkok, November 2024). Rebranded to "Lean Consensus" (trademark clash).

**Three Pillars:** Security, Simplicity, Optimality
**Three Tracks:** Lean Consensus (C), Lean Data (D), Lean Execution (E)

**Key Changes:**
- 4-second slot times (down from 12)
- ZK-native: entire consensus state transition provable via SNARK
- Hash-based post-quantum signatures (XMSS variants) replacing BLS
- Attestor-Proposer Separation (APS) for decentralized block building
- Rainbow Staking: tiered roles with different capital/hardware requirements
- Minimum stake: 1 ETH (down from 32 ETH)

**Lean Consensus Track:**
- 3-Slot Finality (3SF): <400 LOC reference implementation
- Post-Quantum Signatures: XMSS+STARK aggregation
- Whisk SSLE (EIP-7441): Single-slot leader election
- APS: Remove proposer/relay roles

**Lean Data Track:**
- Full-Chain Sampling (FCS): PeerDAS applied to ALL L1 data
- Unified blob/calldata model with erasure coding
- Trusted-setup-free commitments (Poseidon over binary fields)
- Target: 10-1000x gas throughput increase

**Lean Execution Track:**
- ZK-friendly ISA (RISC-V zkEVM): ~100x speedup
- Native rollup support
- Horizontal scaling

**Unified Cryptography:**
- Single hash function (Poseidon) for SSZ, state root, DAS, PQ signatures, zkEVM

**Verification Target:** Validate blocks on $7 Raspberry Pi Pico (SNARK verification only)

**Formal Verification:** $20M, 3-year Lean 4 proof effort

**Development Teams (6 implementations):**
- Ream (Rust), Zeam (Zig), Qlean-mini (C++), Lantern, Lighthouse (Rust), ethlambda

**Devnet Progress:**
- pq-devnet-0 (October 2025): Framework and multi-client coordination
- pq-devnet-1 (December 2025): Post-quantum signature integration
- pq-devnet-2 (January 2026): Signature aggregation

**Timeline:**

| Phase | Year | Activity |
|-------|------|----------|
| Speccing | 2025 | Executable specs (~1,000 lines Python) |
| Building | 2026 | Client implementation |
| Testing | 2027 | Multi-client testnets, audits |
| Mainnet | 2029-2030 | Deployment |

**Tech Debt Slated for Removal:**
Sync committees, slot committees, deposit contract quirks, withdrawal credential
variants, entire blob subsystem (migrated to FCS), legacy EVM interpreter code.

---

## Fort Mode -- Quantum Resistance

### ML-DSA Signatures

**NIST Standards (Finalized August 2024):**
- **FIPS 204 (ML-DSA)**: Lattice-based digital signatures (primary ECDSA replacement)
  - ML-DSA-44: 1,312-byte pubkey, 2,420-byte signature
  - ML-DSA-65: 1,952-byte pubkey, 3,309-byte signature (recommended)
  - ML-DSA-87: 2,592-byte pubkey, 4,627-byte signature
- **FIPS 203 (ML-KEM)**: Key encapsulation (lattice-based)
- **FIPS 205 (SLH-DSA)**: Stateless hash-based signatures (conservative)

**Ethereum's Vulnerability:**
- ECDSA (secp256k1): Breakable by Shor's algorithm
- BLS12-381: Validator attestations -- quantum-vulnerable
- KZG commitments: Blob verification -- quantum-vulnerable
- Vitalik estimates ~20% chance quantum breaks EC crypto before 2030

**EIP-7619 (Falcon-512 Precompile):**
- Precompile at address `0x65`
- Input: pubkey (897 bytes) + signature (up to 666 bytes) + message
- Gas: 1465 base + 6 per word
- NIST-compliant, security level I

**Near-term Path:** Account abstraction (ERC-4337) allows opt-in PQ signature schemes
without protocol changes.

**Community Discussion (ethresear.ch, most active PQ topics):**
- "So you wanna Post-Quantum Ethereum transaction signature" (26 posts)
- "The road to PQ Ethereum transaction is paved with Account Abstraction" (21 posts)
- "Tasklist for post-quantum ETH" (14 posts)
- "Deprecating BLS: PQ Recovery via Deposit Address" (10 posts)
- "Migration Strategies for EOAs under the Quantum Threat" (3 posts, Jan 2026)

### BLS Replacement & leanSig

**Two Approaches for Consensus Signatures:**

1. **Hash-based + STARK aggregation (preferred):**
   - XMSS-variant signatures aggregated via STARKs
   - STARKs are quantum-resistant (hash-based, not EC-based)
   - "Quantum-safe all the way down"
   - Lambda Class: detailed "leanSig" technical explainer

2. **Lattice-based:**
   - ML-DSA, but aggregation less efficient than BLS
   - Still being evaluated

**Timeline:**

| Timeframe | Milestone |
|-----------|-----------|
| 2020-2025 | BLS signatures (current) |
| 2025-2027 | Hybrid: BLS + leanSig research; PQ devnets running |
| 2027-2030 | leanSig deployment with SNARK aggregation |
| 2029-2030 | Beam Chain mainnet with full PQ consensus |

**NIST Deprecation:** 2030 (classical asymmetric deprecation begins), 2035 (full disallowance).

---

## Decentralization

### 1 ETH Solo Staking (EIP-7251)

**Status:** Final (shipped in Pectra, May 2025) | **Category:** Core

EIP-7251 raises `MAX_EFFECTIVE_BALANCE` from 32 ETH to 2048 ETH, enabling validator
consolidation. Combined with ZK signature aggregation in Beam Chain, this paves
the path to 1 ETH solo staking.

**Technical Specification:**
- `MAX_EFFECTIVE_BALANCE_ELECTRA = 2048 ETH`
- `MIN_ACTIVATION_BALANCE = 32 ETH` (unchanged for now)
- `COMPOUNDING_WITHDRAWAL_PREFIX = 0x02` (auto-compounding)
- Consolidation request predeploy at `0x0000BBdDc7CE488642fb579F8B00f3a590007251`
- Dynamic fee system for consolidation requests

**Path to 1 ETH:**
1. **Pectra (2025):** MaxEB increase -- validators can hold 2048 ETH
2. **Beam Chain (2027-2030):** ZK signature aggregation reduces validator overhead
3. **Target:** MIN_ACTIVATION_BALANCE reduced to 1 ETH

---

## Fast Finality

### Single Slot Finality / 3-Slot Finality

**Status:** Research phase | **Not yet scheduled for specific fork**

**Current Problem:** Gasper finalizes in 64-95 blocks (~15 minutes), enabling reorgs and MEV.

**Proposed Mechanisms:**

**Orbit SSF:**
- Smaller validator subset via slowly rotating "orbit" window
- Depends on EIP-7251 (MaxEB) -- already shipped
- Consolidation incentives reduce active validator count

**3-Slot Finality (3SF):**
- Francesco D'Amato, Roberto Saltini, Thanh-Hai Tran, Luca Zanolini
- Finalizes honest-proposer blocks within 3 slots
- Only one voting phase per slot
- Monotonicity slashing: justified targets must advance monotonically
- Academic SoK paper (December 2025, arXiv:2512.20715)

**Fast Confirmation Rule (Near-Term, 2026):**
- 15-30 second strong probabilistic security
- ~98% latency reduction for UX
- Bridge until full 3SF deployment

**Community Discussion (ethresear.ch, most active category with 15+ topics):**
- "Sticking to 8192 signatures per slot post-SSF" (43 posts)
- "Unbundling staking: Towards rainbow staking" (13 posts)
- "Orbit SSF: solo-staking-friendly validator set management" (4 posts)
- "3-Slot-Finality: SSF is not about 'Single' Slot" (2 posts)
- "Paths to SSF revisited" (March 2025)

---

## Consensus Specs Fork Progression

Current progression from the `consensus-specs` repository:

```
Phase0 -> Altair -> Bellatrix -> Capella -> Deneb -> Electra -> Fulu (stable) -> Gloas (in-dev)
```

| Fork | Epoch | Key Features |
|------|-------|--------------|
| Phase0 | 0 | Initial beacon chain |
| Altair | 74,240 | Sync committees |
| Bellatrix | 144,896 | The Merge (PoS) |
| Capella | 194,048 | Staking withdrawals |
| Deneb | 269,568 | Proto-Danksharding (EIP-4844) |
| Electra | 364,032 | MaxEB, execution requests |
| **Fulu** | 411,392 | PeerDAS, blob scheduling, EIP-7917 |
| **Gloas** | TBD | ePBS (EIP-7732), builder registry |

**Feature Specs (in `_features/`):**
- `eip6914/` -- Validator Index Reuse
- `eip7441/` -- Whisk (Single-Slot Leader Election)
- `eip7805/` -- FOCIL (Inclusion Lists)
- `eip7928/` -- Block Access Lists
- `eip8025/` -- Execution Proofs (Stateless Validation)

**Execution API Progression:**
- Prague: `engine_newPayloadV4` (execution requests)
- Osaka: `engine_getPayloadV5` (blob cell proofs)
- Amsterdam: `engine_newPayloadV5` (block access lists)

---

## Go-Ethereum Architecture for Client Developers

### Package Overview

| Package | Purpose |
|---------|---------|
| `core/` | Consensus logic, block validation, EVM, state management |
| `core/vm/` | Bytecode VM, opcode handlers, precompiled contracts |
| `core/state/` | StateDB (accounts/storage), trie prefetcher, access events |
| `core/stateless/` | Witness generation for stateless execution |
| `eth/` | P2P protocol, syncing, mining orchestration |
| `eth/catalyst/` | Engine API implementation (CL-EL bridge) |
| `beacon/` | CL client integration, light sync |
| `trie/` | Merkle Patricia trie + transition trie |
| `triedb/` | Hash-based and path-based trie backends |
| `crypto/` | secp256k1, BLS, KZG, Keccak, Ziren zkVM |
| `consensus/` | Pluggable consensus engines (PoS, PoW, PoA) |

### Key Roadmap-Relevant Code

**Parallel Execution Foundation:**
- `core/state_prefetcher.go`: Already runs parallel prefetching (4/5 CPU cores)
- `core/state/trie_prefetcher.go`: Parallel trie node fetching
- Sequential execution in `core/state_processor.go` -- to be parallelized by EIP-7928

**Stateless:**
- `core/stateless/witness.go`: Witness data structure
- `core/state/access_events.go`: Tracks all state accesses
- `eth/catalyst/witness.go`: `ForkchoiceUpdatedWithWitnessV1/V2/V3`

**ZK Integration (Ziren):**
- `crypto/keccak_ziren.go`: ZK-optimized Keccak via `zkvm_runtime.Keccak256`
  - Build tag: `go build -tags=ziren`
  - Uses `github.com/ProjectZKM/Ziren/crates/go-runtime/zkvm_runtime`
- `cmd/keeper/getpayload_ziren.go`: ZK proof generation tooling

**Transaction Types:**
- Legacy, dynamic fee (EIP-1559), blob (EIP-4844), setcode (EIP-7702)
- EIP-7702 `SET_CODE_TX_TYPE = 0x04` for EOA code delegation

**Block Processing Data Flow:**
```
Block Arrival -> P2P Handler (eth/)
  -> HeaderChain.InsertHeaderChain() [quick validation]
  -> BlockValidator.ValidateBody() [full validation]
  -> StateProcessor.Process()
     -> For each tx: StateTransition -> EVM.Call()
     -> Consensus.Finalize() [rewards, withdrawals]
     -> StateDB.Commit()
  -> TrieDB.Commit() [persist to disk]
  -> Blockchain.InsertChain() [canonical chain]
  -> RPC Events [NewHeadEvent]
  -> Beacon Client (Engine API)
```

---

## Community Discussion Summary

### Ethereum Magicians (ethereum-magicians.org)

**Most Active 2028-Relevant Topics:**
1. EIP-4444: History Expiry -- 46 posts
2. EIP-7928: Block Access Lists -- 35+ posts
3. Glamsterdam Stakeholder Feedback -- 26 posts
4. ePBS (EIP-7732) -- 14 posts + 13 breakout sessions

**Discussion Patterns:**
- Regular All Core Devs calls (21+ documented)
- Dedicated breakout sessions per major feature
- Stateless Implementers Calls for state expiry
- Fork-specific proposal threads (Glamsterdam, Hegota)

### Ethresear.ch

**Most Active 2028-Relevant Topics:**
1. "Sticking to 8192 signatures post-SSF" -- 43 posts
2. "Native rollups -- superpowers from L1 execution" -- 42 posts
3. "So you wanna PQ Ethereum transaction signature" -- 26 posts
4. "PQ Ethereum via Account Abstraction" -- 21 posts
5. "Why RISC-V Is Not a Good Choice" -- 14 posts

**Research Themes:**
- SSF/3SF: Most active research area (15+ topics)
- Post-Quantum: Second most active (11 topics), accelerating in 2025-2026
- Native Rollups: High engagement on Justin Drake's proposal
- RISC-V vs WASM: Active debate on L1 ISA selection

---

## Implementation Priority Matrix

For client developers building toward 2028:

### Must Implement (2026)

| Priority | Feature | EIP | Fork |
|----------|---------|-----|------|
| P0 | Block Access Lists | EIP-7928 | Glamsterdam |
| P0 | Enshrined PBS | EIP-7732 | Glamsterdam |
| P0 | Gas repricing | EIP-7904 | Glamsterdam |
| P1 | PeerDAS (if not in Fulu) | EIP-7594 | Fulu/Glamsterdam |
| P1 | FOCIL Inclusion Lists | EIP-7805 | Hegota |
| P1 | ExecutionWitness generation | -- | Ongoing |

### Should Implement (2027)

| Priority | Feature | EIP | Fork |
|----------|---------|-----|------|
| P1 | History Expiry | EIP-4444 | The Purge |
| P1 | ZK proof verification (optional) | EIP-8025 | Phase 1 |
| P2 | Stateless client mode | -- | With Binary Tree |
| P2 | Execution Proofs engine | EIP-8025 | Phase 2 |

### Plan For (2028)

| Priority | Feature | EIP | Fork |
|----------|---------|-----|------|
| P1 | Mandatory ZK proofs | EIP-8025 | Phase 2-3 |
| P1 | EXECUTE precompile | EIP-8079 | Phase 3 |
| P2 | ML-DSA transaction signatures | -- | Fort Mode |
| P2 | Post-quantum consensus sigs | -- | Beam Chain |
| P2 | RISC-V/LeanVM support | -- | Lean Execution |
| P3 | 3-Slot Finality | -- | Beam Chain |

### Track / Research

| Feature | Notes |
|---------|-------|
| Beam Chain / Lean Consensus | 6 client implementations active; spec phase |
| leanSig (XMSS+STARK) | PQ devnets running |
| Full-Chain Sampling | Depends on PeerDAS maturity |
| Unified Poseidon crypto | Transition research |
| Rainbow Staking | Linked to Beam Chain |

---

## Key References

**EIP Specs:**
- `refs/EIPs/EIPS/eip-7732.md` -- ePBS
- `refs/EIPs/EIPS/eip-7928.md` -- Block Access Lists
- `refs/EIPs/EIPS/eip-4444.md` -- History Expiry
- `refs/EIPs/EIPS/eip-7251.md` -- MaxEB / Flexible Staking
- `refs/EIPs/EIPS/eip-7594.md` -- PeerDAS
- `refs/EIPs/EIPS/eip-7702.md` -- Set Code for EOAs
- `refs/EIPs/EIPS/eip-7685.md` -- EL Requests Framework
- `refs/EIPs/EIPS/eip-7619.md` -- Falcon-512 Precompile

**Consensus Specs:**
- `refs/consensus-specs/specs/fulu/` -- PeerDAS, blob scheduling
- `refs/consensus-specs/specs/gloas/` -- ePBS, builder registry
- `refs/consensus-specs/specs/_features/` -- FOCIL, Block Access Lists, Execution Proofs

**Execution APIs:**
- `refs/execution-apis/src/engine/prague.md` -- Execution requests
- `refs/execution-apis/src/engine/osaka.md` -- Blob cell proofs
- `refs/execution-apis/src/engine/amsterdam.md` -- Block access lists

**External:**
- [Lean Consensus Roadmap](https://leanroadmap.org/)
- [EF Real-Time Proving Blog](https://blog.ethereum.org/2025/07/10/realtime-proving)
- [EF zkEVM Portal](https://zkevm.ethereum.foundation/)
- [Native Rollups (L2Beat)](https://native-rollups.l2beat.com/)
- [3SF Paper (arXiv)](https://arxiv.org/abs/2512.20715)
- [NIST PQC Standards](https://csrc.nist.gov/projects/post-quantum-cryptography)
- [Lambda Class leanSig Explainer](https://blog.lambdaclass.com/ethereum-signature-schemes-explained-ecdsa-bls-xmss-and-post-quantum-leansig-with-rust-code-examples/)
