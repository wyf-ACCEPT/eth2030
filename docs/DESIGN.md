# ETH2030 Execution Client -- Design Document

> A minimal, spec-compliant Ethereum execution client targeting the 2028 roadmap.
> Built in Go, referencing the L1 Strawmap by EF Protocol (Feb 11, 2026).
> Source: EF Architecture team (Ansgar, Barnabe, Francesco, Justin).

---

## 1. Architecture Overview

```
                     ┌──────────────────────────────┐
                     │     Consensus Client (CL)     │
                     └──────────┬───────────────────┘
                                │ Engine API (JSON-RPC)
                     ┌──────────▼───────────────────┐
                     │      Engine API Server         │
                     │  newPayloadV4/V5, fcuV3/V4    │
                     └──────────┬───────────────────┘
                                │
              ┌─────────────────┼─────────────────┐
              │                 │                  │
     ┌────────▼──────┐ ┌───────▼───────┐ ┌────────▼──────┐
     │  Block Builder │ │ Block Validator│ │ Payload Store │
     │   (miner)      │ │ (state proc)  │ │               │
     └────────┬──────┘ └───────┬───────┘ └───────────────┘
              │                 │
     ┌────────▼─────────────────▼──────┐
     │          State Processor         │
     │   Sequential -> Parallel (7928) │
     └────────┬────────────────────────┘
              │
     ┌────────▼──────────────────────┐
     │          EVM Interpreter       │
     │  Opcodes, Precompiles, Gas    │
     └────────┬──────────────────────┘
              │
     ┌────────▼──────────────────────┐
     │          StateDB               │
     │  Accounts, Storage, Code      │
     └────────┬──────────────────────┘
              │
     ┌────────▼──────────────────────┐
     │     Trie Database              │
     │  MPT -> Verkle (EIP-6800)     │
     └────────┬──────────────────────┘
              │
     ┌────────▼──────────────────────┐
     │     Key-Value Store            │
     │  (Pebble / LevelDB)          │
     └──────────────────────────────┘
```

---

## 2. L1 Strawmap Phase Mapping

The L1 Strawmap (strawmap.org) defines three layers with the following upgrade timeline:

### Upgrade Timeline

| Phase | Year | CL Headline | DL Headline | EL Headline |
|-------|------|-------------|-------------|-------------|
| **Glamsterdan** | 2026 | fast confirmation, ePBS, FOCIL | peerDAS | native AA, BALS, conversion repricing |
| **Hogota** | 2026-2027 | single slot, modernized beacon state | blob throughput increase, local blob reconstruction | Hogota repricing, payload shrinking, multidimensional pricing |
| **I+** | 2027 | 1-epoch finality, post-quantum custody | decrease sample size | announce binary tree, NII precompile(s), encrypted mempool |
| **J+** | 2027-2028 | 4-slot epochs, beacon & blob sync | short-dated blob futures | verkle/portal state, precompiles in eWASM, STF in eRISC |
| **K+** | 2028 | 6-sec slots, 1MiB attestor cap, KPS | 3-RPO slots increase | block in blobs, mandatory 3-of-5 proofs, canonical guest |
| **L+** | 2029 | endgame finality, LETHE insulation | L-RPO blob increase, post-quantum blobs | advance state, native rollups, proof aggregation |
| **M+** | 2029+ | fast L1 finality in seconds, post-quantum attestations | forward-cast blobs | canonical zxVM, long-dated gas futures, shared mempools, gigas L1 |
| **Longer term** | 2030++ | distributed block building, VDF | teradata L2, proof custody | exposed ELSA, post-quantum transactions, private L1 shielded compute |

---

## 3. Phase-by-Phase EL Implementation Details

### Phase 1: Glamsterdan (H1 2026)

The Glamsterdan fork is the first major post-Pectra upgrade, introducing the foundation for parallel execution and native account abstraction.

#### 3.1.1 EIP-7928: Block-Level Access Lists (BALs)

**Spec:** `refs/EIPs/EIPS/eip-7928.md`
**Consensus Spec:** `refs/consensus-specs/specs/_features/eip7928/beacon-chain.md`
**Engine API:** `refs/execution-apis/src/engine/amsterdam.md`
**Status:** Draft | Standards Track | Core

**Abstract:** Introduces Block-Level Access Lists (BALs) that record all accounts and storage locations accessed during block execution, along with post-execution values. BALs enable parallel disk reads, parallel transaction validation, parallel state root computation, and executionless state updates.

**Key Constants:**
- `BlockAccessIndex`: `uint16` (0 = pre-execution, 1..n = tx indices, n+1 = post-execution)
- `block_access_list_hash`: `keccak256(rlp.encode(block_access_list))`
- Empty hash: `keccak256(rlp.encode([]))` = `0x1dcc4de8...`

**RLP Data Structures:**
```
AccountChanges = [
    Address,                    # 20-byte address
    List[SlotChanges],          # storage_changes [slot, [[block_access_index, new_value]]]
    List[StorageKey],           # storage_reads (read-only slots)
    List[BalanceChange],        # balance_changes [[block_access_index, post_balance]]
    List[NonceChange],          # nonce_changes [[block_access_index, new_nonce]]
    List[CodeChange]            # code_changes [[block_access_index, new_code]]
]
BlockAccessList = List[AccountChanges]
```

**Scope Rules (MUST include):**
- All addresses with state changes (storage, balance, nonce, code)
- Targets of BALANCE, EXTCODESIZE, EXTCODECOPY, EXTCODEHASH
- Targets of CALL, CALLCODE, DELEGATECALL, STATICCALL (even if reverted)
- Transaction sender/recipient, coinbase, precompile targets
- System contract addresses

**EL Implementation:**
1. Add `block_access_list_hash` to block header
2. Track all state accesses during block execution in `StateProcessor`
3. Build BAL from access tracking and compute hash
4. Validate BAL hash in `BlockValidator`
5. Add `blockAccessList` field to `ExecutionPayloadV4`
6. Implement `engine_newPayloadV5` validation
7. Implement `engine_getPayloadV6` BAL generation
8. Implement parallel execution scheduler using BAL dependency graph

**Parallel Execution Algorithm:**
```go
// ParallelScheduler partitions txs into groups with no state conflicts
func (s *ParallelScheduler) Schedule(bal *BlockAccessList) []ExecutionGroup {
    // Build conflict graph from BAL entries
    // Txs sharing addresses/slots are in same group
    // Independent txs can execute in parallel
}
```

**Current Status:** BAL types, tracker, hash computation, and integration tests implemented. Parallel scheduler needs completion.

#### 3.1.2 EIP-7702: Set Code for EOAs (Native Account Abstraction)

**Spec:** `refs/EIPs/EIPS/eip-7702.md`
**Status:** Final | Standards Track | Core

**Abstract:** Adds transaction type `0x04` that allows EOAs to set code in their account via authorization tuples. For each tuple, a delegation indicator `0xef0100 || address` is written to the authorizing account's code.

**Key Constants:**
| Parameter | Value |
|-----------|-------|
| `SET_CODE_TX_TYPE` | `0x04` |
| `MAGIC` | `0x05` |
| `PER_AUTH_BASE_COST` | `12500` |
| `PER_EMPTY_ACCOUNT_COST` | `25000` |

**Transaction Format:**
```
rlp([chain_id, nonce, max_priority_fee_per_gas, max_fee_per_gas, gas_limit,
     destination, value, data, access_list, authorization_list,
     signature_y_parity, signature_r, signature_s])

authorization_list = [[chain_id, address, nonce, y_parity, r, s], ...]
```

**EL Implementation:**
1. Define `SetCodeTx` (type 0x04) with authorization list field
2. Authorization processing: verify ECDSA signatures using `MAGIC || rlp([chain_id, address, nonce])`
3. Write delegation indicator `0xef0100 || target_address` to authorizer's code
4. During CALL: detect `0xef0100` prefix, load/execute delegated code
5. Gas: `PER_AUTH_BASE_COST` (12500) per authorization + `PER_EMPTY_ACCOUNT_COST` (25000) for new accounts
6. Nonce validation for authorization tuples
7. EVM changes: EXTCODESIZE/EXTCODECOPY/EXTCODEHASH follow delegation pointers

**Current Status:** EIP-7702 processing implemented in `core/eip7702.go`. Transaction type 0x04 defined in `core/types/`.

#### 3.1.3 EIP-7904: Gas Cost Repricing (Conversion Repricing)

**Spec:** `refs/EIPs/EIPS/eip-7904.md`
**Status:** Draft | Standards Track | Core

**Abstract:** Raises gas costs of 13 compute operations and precompiles performing below 60 Mgas/s target, enabling 3x block gas limit increase.

**Opcode Repricing:**
| Opcode | Current | New | Multiplier |
|--------|---------|-----|-----------|
| DIV | 5 | 15 | 3.0x |
| SDIV | 5 | 20 | 4.0x |
| MOD | 5 | 12 | 2.4x |
| MULMOD | 8 | 11 | 1.4x |
| KECCAK256 (constant) | 30 | 45 | 1.5x |

**Precompile Repricing:**
| Precompile | Current | New | Change |
|------------|---------|-----|--------|
| BLAKE2F (constant) | 0 | 170 | new |
| BLAKE2F (per round) | 1 | 2 | 2.0x |
| BLS12_G1ADD | 375 | 643 | 1.7x |
| BLS12_G2ADD | 600 | 765 | 1.3x |
| ECADD | 150 | 314 | 2.1x |
| POINT_EVALUATION | 50000 | 89363 | 1.8x |

**EL Implementation:**
1. Update jump table gas costs for Glamsterdan fork
2. Add new `GlamsterdanInstructionSet` in `core/vm/jump_table.go`
3. Update precompile gas costs in `core/vm/precompiles.go`
4. Fork-aware gas table: `IsGlamsterdan(time)` check

**Current Status:** Gas table structure exists. Need Glamsterdan-specific repricing.

#### 3.1.4 Engine API: Amsterdam Methods

**Spec:** `refs/execution-apis/src/engine/amsterdam.md`

**New Structures:**
- `ExecutionPayloadV4`: extends V3 with `blockAccessList` (RLP-encoded BAL) and `slotNumber`
- `ExecutionPayloadBodyV2`: extends V1 with `blockAccessList`
- `PayloadAttributesV4`: extends V3 with `slotNumber`

**New Methods:**
| Method | Description |
|--------|-------------|
| `engine_newPayloadV5` | Validates payload with BAL; returns INVALID if BAL mismatch |
| `engine_getPayloadV6` | Returns built payload with BAL populated |
| `engine_getPayloadBodiesByHashV2` | Returns bodies with BAL (null for pre-Amsterdam) |
| `engine_getPayloadBodiesByRangeV2` | Range query for bodies with BAL |
| `engine_forkchoiceUpdatedV4` | Fork choice with `PayloadAttributesV4` |

**Validation Rules:**
- MUST return `-38005: Unsupported fork` if timestamp outside Amsterdam
- MUST return `-32602: Invalid params` if `blockAccessList` missing
- MUST validate BAL by executing payload transactions
- INVALID status if computed BAL doesn't match provided BAL

---

### Phase 2: Hogota (H2 2026 - 2027)

#### 3.2.1 Hogota Repricing

Further gas cost adjustments based on empirical benchmarks across all 5 major clients (besu, erigon, geth, nethermind, reth). Targets operations still below the throughput target after Glamsterdan repricing.

**EL Implementation:**
1. Additional opcode gas cost updates
2. New `HogotaInstructionSet` in jump table
3. `IsHogota(time)` fork check

#### 3.2.2 Payload Shrinking

Reduces execution payload size by:
- Compressing repeated data (addresses, hashes)
- SSZ encoding for payloads (replacing RLP where applicable)
- Pruning redundant header fields

**EL Implementation:**
1. SSZ encoding support for execution payloads
2. Compression codec for Engine API payloads
3. Backward compatibility layer

#### 3.2.3 Multidimensional Gas Pricing

Extends EIP-1559 to multiple resource dimensions:
- Compute gas (CPU)
- Storage gas (disk I/O)
- Bandwidth gas (calldata/blobs)
- State growth gas (new accounts/storage)

Each dimension has independent base fees and targets.

**EL Implementation:**
1. Multi-dimensional gas tracking in EVM
2. Separate base fee calculations per dimension
3. Transaction gas specification per dimension
4. Block header fields for per-dimension gas used/limit

---

### Phase 3: I+ (2027)

#### 3.3.1 Announce Binary Tree (Verkle Transition Preparation)

**Spec:** `refs/EIPs/EIPS/eip-6800.md`
**Feature Spec:** `refs/consensus-specs/specs/_features/eip6800/`

Announces the Verkle tree structure to prepare for state migration. The MPT state trie is frozen and a new Verkle tree grows alongside it.

**EL Implementation:**
1. Dual-tree state: frozen MPT + growing Verkle
2. Key derivation: `get_tree_key(address, tree_index, sub_index)`
3. Verkle node types: inner (branching factor 256), leaf
4. Pedersen commitment using Bandersnatch curve (IPA)
5. State migration: new writes go to Verkle, reads check both

#### 3.3.2 EIP-4762: Statelessness Gas Cost Changes

**Spec:** `refs/EIPs/EIPS/eip-4762.md`
**Status:** Draft | Standards Track | Core

**Abstract:** Changes gas schedule to reflect costs of creating execution witnesses for stateless validation. Restructures gas costs around Verkle tree leaf access patterns.

**Key Concepts:**
- Access events: `(address, sub_key, leaf_key)` tuples
- `BASIC_DATA_LEAF_KEY`: account header data
- `CODEHASH_LEAF_KEY`: code hash
- `CODE_OFFSET`, `HEADER_STORAGE_OFFSET`, `MAIN_STORAGE_OFFSET`: tree layout constants

**Gas Costs:**
| Event | Warm | Cold Write | Cold Read |
|-------|------|-----------|-----------|
| Account header | 0 | SUBTREE_EDIT_COST | WITNESS_BRANCH_COST + WITNESS_CHUNK_COST |
| Storage slot | 0 | SUBTREE_EDIT_COST | WITNESS_BRANCH_COST + WITNESS_CHUNK_COST |
| Code chunk | 0 | SUBTREE_EDIT_COST | WITNESS_BRANCH_COST + WITNESS_CHUNK_COST |

**EL Implementation:**
1. Verkle-aware gas table in `core/vm/gas_table.go`
2. Access event tracking during execution
3. Warm/cold distinction per Verkle leaf (not per account)
4. Code chunking: 31 bytes per chunk, tracked individually
5. `IsVerkleGas(time)` fork check

#### 3.3.3 NII Precompile(s) (Cryptography)

New precompiles for numeric/integer/infinite-precision arithmetic, supporting:
- Modular exponentiation improvements
- Pairing-friendly curve operations
- SHA-256 circuit support for ZK

**EL Implementation:**
1. New precompile addresses
2. Gas cost models
3. Integration with crypto package

#### 3.3.4 Encrypted Mempool

Privacy-preserving transaction pool using:
- Threshold encryption (TEE or MPC-based)
- Delayed decryption until block proposal
- MEV protection for users

**EL Implementation:**
1. Encrypted transaction wrapper type
2. Threshold decryption key management
3. Modified txpool to handle encrypted transactions

---

### Phase 4: J+ (2027-2028)

#### 3.4.1 Verkle/Portal State

Full Verkle tree state migration complete. MPT frozen, all reads/writes go through Verkle tree.

**EL Implementation:**
1. Complete MPT -> Verkle migration
2. Verkle proof generation for all state accesses
3. Portal Network integration for historical state
4. State pruning of MPT data

#### 3.4.2 Precompiles in eWASM

Move precompile implementations from native Go to eWASM (Ethereum WebAssembly):
- Enables on-chain upgradability
- Standard ABI for precompile calls
- WASM runtime in EVM

**EL Implementation:**
1. WASM interpreter integration
2. Precompile hosting in WASM
3. Gas metering for WASM execution
4. Migration path from native precompiles

#### 3.4.3 STF in eRISC

State Transition Function compilation to RISC-V for ZK-proving:
- EVM bytecode -> RISC-V translation
- Deterministic execution in zkVM (SP1, RISC Zero)
- Enables real-time ZK proofs of execution

**EL Implementation:**
1. EVM-to-RISC-V transpiler
2. zkVM integration interface
3. Proof generation pipeline
4. Proof verification precompile

---

### Phase 5: K+ (2028)

#### 3.5.1 Mandatory 3-of-5 Proofs

**Spec:** `refs/consensus-specs/specs/_features/eip8025/`

Every block must include at least 3 valid execution proofs from 5 different prover backends. This ensures no single ZK system is a single point of failure.

**Prover Backends:**
1. SP1 (Succinct)
2. RISC Zero
3. ZisK
4. Jolt
5. OpenVM

**EL Implementation:**
1. Proof verification engine with multiple backend support
2. `ExecutionProof` SSZ container (max 300 KiB)
3. Proof submission via Engine API
4. Block validity requires 3/5 valid proofs
5. Proof aggregation for bandwidth efficiency

#### 3.5.2 Canonical Guest (zkVM)

A canonical RISC-V guest program for the EVM state transition, enabling:
- Deterministic execution proof generation
- Cross-client compatibility via shared guest binary
- Formal verification of the STF

**EL Implementation:**
1. Canonical guest binary format
2. Guest program version management
3. Integration with proof verification engine

#### 3.5.3 Block in Blobs

Execute blocks whose data is committed in blobs rather than calldata:
- Reduces L1 block size
- Leverages blob data availability
- Supports large block payloads via DAS

**EL Implementation:**
1. Block payload committed as blob data
2. Blob reconstruction for block execution
3. Modified block validation with blob references

#### 3.5.4 EIP-8079: Native Rollups (EXECUTE Precompile)

**Spec:** `refs/EIPs/EIPS/eip-8079.md`
**Status:** Draft | Standards Track | Core

**Abstract:** Exposes Ethereum's state transition function as an `EXECUTE` precompile, allowing rollups to reuse L1's EVM verification infrastructure directly.

**Key Parameters:**
| Constant | Value |
|----------|-------|
| `PROOF_TX_TYPE` | TBD |
| `EXECUTE_PRECOMPILE_ADDRESS` | TBD |
| `ANCHOR_ADDRESS` | TBD |

**Precompile Logic:**
```python
def execute(input: Bytes) -> Bytes:
    chain = input[...]    # chain state reference
    block = input[...]    # block to verify
    anchor = input[...]   # L1->L2 messaging data

    # Blob transactions not supported in rollup context
    for tx in transactions:
        if isinstance(tx, BlobTransaction):
            raise ExecuteError

    # Perform anchoring for L1->L2 messaging
    process_unchecked_system_transaction(block_env, ANCHOR_ADDRESS, anchor)

    # Execute the state transition
    state_transition(chain, block)
```

**Header Extension:** New `burned_fees` field (uint64) for base fee tracking.

**EL Implementation:**
1. EXECUTE precompile at designated address
2. Nested EVM execution context
3. Anchoring for L1->L2 message passing
4. Proof-carrying transaction type
5. Burned fees header field
6. Separate EIP-1559 gas target for rollup execution

---

### Phase 6: L+ / M+ (2029+)

#### 3.6.1 Gigas L1 (1 Ggas/sec)

Target: 1 billion gas per second, enabling ~10,000+ TPS on L1.

Requirements:
- Real-time ZK proving of all blocks
- Parallel execution via BALs
- Optimized state access patterns
- Pipelined block production

#### 3.6.2 Canonical zxVM

A canonical zero-knowledge virtual machine replacing the EVM interpreter:
- Native ZK-friendly instruction set
- Direct proof generation during execution
- No separate proving step needed

#### 3.6.3 Post-Quantum Transactions

Replace secp256k1 ECDSA with post-quantum signature schemes:
- ML-DSA (FIPS 204, formerly Dilithium)
- Falcon-512 (EIP-7619)
- Hash-based signatures (SPHINCS+)

**EIP-7619: Falcon-512 Precompile:**
**Spec:** `refs/EIPs/EIPS/eip-7619.md`

**EL Implementation:**
1. New transaction signature type for post-quantum
2. Falcon-512 verification precompile
3. Hybrid signatures (ECDSA + PQ) during transition
4. Key size accommodations (PQ keys are larger)

#### 3.6.4 Shared Mempools

Cross-node transaction pool sharing for reduced latency:
- Cryptographic commitments to pool contents
- Efficient diff-based synchronization
- Privacy-preserving pool queries

#### 3.6.5 Exposed ELSA

Execution Layer State Access interface for external provers and validators:
- Standardized state query API
- Proof-carrying state responses
- Streaming state updates

---

## 4. Spec File Index

### EIP Specifications (refs/EIPs/EIPS/)

| EIP | File | Title | Status | Phase |
|-----|------|-------|--------|-------|
| 1153 | `eip-1153.md` | Transient Storage (TSTORE/TLOAD) | Final | Shanghai |
| 1559 | `eip-1559.md` | Dynamic Fee Transactions | Final | London |
| 2929 | `eip-2929.md` | Gas Cost Increases for State Access | Final | Berlin |
| 2930 | `eip-2930.md` | Access List Transactions (Type 1) | Final | Berlin |
| 3198 | `eip-3198.md` | BASEFEE Opcode | Final | London |
| 3529 | `eip-3529.md` | Reduction in Gas Refunds | Final | London |
| 3855 | `eip-3855.md` | PUSH0 Instruction | Final | Shanghai |
| 3860 | `eip-3860.md` | Limit and Meter Initcode | Final | Shanghai |
| 4444 | `eip-4444.md` | History Expiry | Draft | Purge |
| 4762 | `eip-4762.md` | Statelessness Gas Cost Changes | Draft | I+ |
| 4788 | `eip-4788.md` | Beacon Block Root in EVM | Final | Cancun |
| 4844 | `eip-4844.md` | Blob Transactions (Type 3) | Final | Cancun |
| 4895 | `eip-4895.md` | Beacon Chain Withdrawals | Final | Shanghai |
| 5656 | `eip-5656.md` | MCOPY Instruction | Final | Cancun |
| 6780 | `eip-6780.md` | SELFDESTRUCT Restriction | Final | Cancun |
| 6800 | `eip-6800.md` | Ethereum State Using Verkle Trees | Draft | J+ |
| 7251 | `eip-7251.md` | Increase MAX_EFFECTIVE_BALANCE | Final | Electra |
| 7594 | `eip-7594.md` | PeerDAS | Draft | Glamsterdan |
| 7619 | `eip-7619.md` | Falcon-512 Precompile | Draft | M+ |
| 7685 | `eip-7685.md` | Execution Layer Requests | Final | Prague |
| 7702 | `eip-7702.md` | Set Code for EOAs (Type 4) | Final | Glamsterdan |
| 7732 | `eip-7732.md` | Enshrined PBS | Draft | Glamsterdan |
| 7805 | `eip-7805.md` | FOCIL (Fork-Choice IL) | Draft | Glamsterdan |
| 7840 | `eip-7840.md` | Blob Schedule Configuration | Draft | Cancun+ |
| 7904 | `eip-7904.md` | Gas Cost Repricing | Draft | Glamsterdan |
| 7928 | `eip-7928.md` | Block-Level Access Lists | Draft | Glamsterdan |
| 8079 | `eip-8079.md` | Native Rollups (EXECUTE) | Draft | K+ |

### Engine API Specifications (refs/execution-apis/src/engine/)

| File | Fork | Key Methods |
|------|------|-------------|
| `paris.md` | Paris | newPayloadV1, forkchoiceUpdatedV1 |
| `shanghai.md` | Shanghai | getPayloadBodiesByHashV1 |
| `cancun.md` | Cancun | newPayloadV3, getPayloadV3, getBlobsV1 |
| `prague.md` | Prague | newPayloadV4, getPayloadV4 (+executionRequests) |
| `osaka.md` | Osaka | getPayloadV5 (+BlobsBundleV2/cell proofs), getBlobsV2/V3 |
| `amsterdam.md` | Amsterdam | newPayloadV5 (+BAL), getPayloadV6, forkchoiceUpdatedV4, PayloadAttributesV4 |

### Consensus Specifications (refs/consensus-specs/specs/)

| Directory | Description |
|-----------|-------------|
| `fulu/` | PeerDAS, blob scheduling, polynomial commitments |
| `gloas/` | ePBS, builder registry |
| `_features/eip6800/` | Verkle Trees (beacon-chain integration) |
| `_features/eip7805/` | FOCIL (fork-choice enforced inclusion lists) |
| `_features/eip7928/` | Block Access Lists (CL integration) |
| `_features/eip8025/` | Execution Proofs (proof engine, prover interface) |

---

## 5. Implementation Status

### Summary (as of 2026-02-22)

| Metric | Value |
|--------|-------|
| Packages | 49 (all passing) |
| Source files | 986 |
| Test files | 916 |
| Source LOC | ~310,000 |
| Test LOC | ~392,000 |
| Total LOC | ~702,000 |
| Passing tests | 18,000+ |
| EIPs complete | 58+ (6 substantial) |
| EF State Tests | 36,126/36,126 (100%) |

### Package Completeness

All 47 packages are complete and passing tests. Key packages:

| Package | Status | Description |
|---------|--------|-------------|
| `core/types` | COMPLETE | 7 tx types (incl. FrameTx, AATx), SSZ encoding |
| `core/state` | COMPLETE | In-memory, trie-backed, stateless StateDB, snapshots, pruner |
| `core/vm` | COMPLETE | 164+ opcodes, 24 precompiles, EOF, gas tables |
| `core/` | COMPLETE | Blockchain, processor, validator, gas futures, genesis init |
| `consensus` | COMPLETE | SSF, quick slots, attestations, beacon state, block producer |
| `crypto` | COMPLETE | Keccak, secp256k1, BN254, BLS12-381, Banderwagon, VDF |
| `crypto/pqc` | COMPLETE | Dilithium3, Falcon512, SPHINCS+, hybrid signer |
| `engine` | COMPLETE | Engine API V3-V7, forkchoice, ePBS, distributed builder |
| `das` | COMPLETE | PeerDAS, sampling, custody, blob streaming, futures |
| `p2p` | COMPLETE | TCP, devp2p, discovery V5, gossip, Portal network |
| `zkvm` | COMPLETE | Guest programs, canonical guest (RISC-V), STF |
| `proofs` | COMPLETE | Proof aggregation, mandatory 3-of-5 system |

### go-ethereum Integration

ETH2030 imports go-ethereum v1.17.0 as a Go module dependency. The `pkg/geth/` adapter package bridges ETH2030 types to go-ethereum's EVM and state transition engine, achieving 100% EF state test pass rate (36,126/36,126).

**Key components:**
- `GethBlockProcessor` — executes blocks via `gethcore.ApplyMessage`
- `PrecompileAdapter` — wraps ETH2030 precompiles for go-ethereum's interface
- `InjectCustomPrecompiles` — injects 13 custom precompiles via `evm.SetPrecompiles()`
- `MakePreState` — creates go-ethereum `state.StateDB` backed by real trie DB

**Custom precompiles injected:** Glamsterdam repricing (4), NTT (1), NII (4), Field arithmetic (4).

### Remaining Gaps for Production

1. **Real crypto backends** - Wire blst/circl/go-ipa/gnark submodules as backends (BLS12-381 and KZG adapters already wired)
2. **RLPx encryption** - Production P2P encryption layer
3. **Database backend** - LevelDB/Pebble for production performance
4. **Conformance testing** - EF state tests: 36,126/36,126 (100%) passing via go-ethereum backend

---

## 6. Testing Strategy

### Unit Tests
- Every type has `_test.go` with table-driven tests
- RLP round-trip encoding for all types
- Hash computation verification against known vectors
- Gas calculation tests per opcode

### Integration Tests
- Block processing: build block -> validate -> apply state
- Engine API: mock CL client -> send payloads -> verify responses
- Parallel execution: verify BAL correctness, parallel results match sequential
- Receipt and log persistence across multiple blocks

### End-to-End Tests
- Multi-block chain construction with transactions
- Fork transitions (Glamsterdan, Hogota activation)
- Reorg handling with state rollback
- Contract deployment and interaction (LOG, SSTORE, CALL)

### Conformance Tests
- Use `refs/execution-spec-tests/` for EVM conformance
- Verify against Ethereum JSON test vectors
- Compare state roots against reference implementations

### Fuzzing
- RLP decoder fuzzing (`go test -fuzz=FuzzDecode ./rlp/`)
- Transaction signature recovery fuzzing
- EVM opcode fuzzing
