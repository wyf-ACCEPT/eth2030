# eth2028 Execution Client -- Design Document

> A minimal, spec-compliant Ethereum execution client targeting the 2028 roadmap.
> Built in Go, referencing the L1 Strawmap by EF Protocol (Feb 11, 2026).

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

## 2. Strawmap Phase Mapping to Implementation

### Phase 1: Glamsterdam (H1 2026) -- EL Components

| Feature | EIP | Package | Priority | Status |
|---------|-----|---------|----------|--------|
| Core types (Block, Tx, Header) | -- | `core/types` | P0 | Foundation |
| RLP encoding/decoding | -- | `rlp` | P0 | Foundation |
| EVM interpreter & opcodes | -- | `core/vm` | P0 | Foundation |
| StateDB & account model | -- | `core/state` | P0 | Foundation |
| Engine API types & server | -- | `engine` | P0 | Foundation |
| Block Access Lists | EIP-7928 | `bal` | P0 | Glamsterdam |
| ePBS payload handling | EIP-7732 | `engine` | P0 | Glamsterdam |
| Gas repricing | EIP-7904 | `core/vm` | P1 | Glamsterdam |
| Native Account Abstraction | EIP-7702 | `core/types` | P1 | Glamsterdam |
| Conversion repricing (BALs) | -- | `core/vm` | P1 | Glamsterdam |

### Phase 2: Hegota (H2 2026) -- EL Components

| Feature | EIP | Package | Priority |
|---------|-----|---------|----------|
| Payload shrinking | -- | `engine` | P1 |
| Hegota repricing | -- | `core/vm` | P1 |
| Multidimensional gas pricing | -- | `core/vm` | P2 |
| Blob throughput increase | -- | `engine` | P1 |

### Phase 3: I+ / J+ (2027) -- EL Components

| Feature | EIP | Package | Priority |
|---------|-----|---------|----------|
| Verkle/binary tree state | EIP-6800 | `core/state` | P1 |
| Announce binary tree | -- | `core/state` | P1 |
| Precompiles in eWASM | -- | `core/vm` | P2 |
| STF in eRISC | -- | `core/vm` | P2 |
| History expiry | EIP-4444 | `core/rawdb` | P1 |
| Stateless gas costs | EIP-4762 | `core/vm` | P1 |

### Phase 4: K+ (2028) -- EL Components

| Feature | EIP | Package | Priority |
|---------|-----|---------|----------|
| Mandatory 3-of-5 proofs | EIP-8025 | `witness` | P1 |
| Canonical guest (zkVM) | -- | `core/vm` | P1 |
| Native rollups (EXECUTE) | EIP-8079 | `core/vm` | P2 |
| Block in blobs | -- | `engine` | P2 |
| Advance state | -- | `core/state` | P2 |

### Phase 5: L+ / M+ (2029+) -- EL Components

| Feature | EIP | Package | Priority |
|---------|-----|---------|----------|
| Canonical zkVM | -- | `core/vm` | P2 |
| Gigas L1 (1 Ggas/sec) | -- | `core/vm` | P3 |
| Post-quantum transactions | -- | `crypto` | P2 |
| Proof aggregation | -- | `witness` | P2 |
| Exposed ELSA | -- | `engine` | P3 |
| Shared mempools | -- | `txpool` | P3 |
| Long-dated gas futures | -- | `core/vm` | P3 |
| Private L1 shielded compute | -- | `core/vm` | P3 |

---

## 3. Package Design

### `pkg/core/types` -- Core Ethereum Types

Defines all fundamental data structures.

```go
// Header represents a block header per the Yellow Paper + EIP extensions.
type Header struct {
    ParentHash       Hash
    UncleHash        Hash
    Coinbase         Address
    Root             Hash           // state trie root
    TxHash           Hash           // transactions trie root
    ReceiptHash      Hash           // receipts trie root
    Bloom            Bloom          // 256-byte bloom filter
    Difficulty       *uint256.Int   // legacy, always 0 post-merge
    Number           *uint256.Int
    GasLimit         uint64
    GasUsed          uint64
    Time             uint64
    Extra            []byte
    MixDigest        Hash           // prev_randao post-merge
    Nonce            BlockNonce     // legacy
    BaseFee          *uint256.Int   // EIP-1559
    WithdrawalsHash  *Hash          // EIP-4895
    BlobGasUsed      *uint64        // EIP-4844
    ExcessBlobGas    *uint64        // EIP-4844
    ParentBeaconRoot *Hash          // EIP-4788
    RequestsHash     *Hash          // EIP-7685
    // EIP-7928: Block Access List commitment
    BlockAccessListHash *Hash
}

// Transaction types:
//   0x00: Legacy
//   0x01: EIP-2930 access list
//   0x02: EIP-1559 dynamic fee
//   0x03: EIP-4844 blob
//   0x04: EIP-7702 set code
type Transaction struct {
    Type     uint8
    inner    TxData // interface for polymorphic tx types
}

// TxData is the interface for all transaction types.
type TxData interface {
    txType() uint8
    chainID() *uint256.Int
    accessList() AccessList
    data() []byte
    gas() uint64
    gasPrice() *uint256.Int
    gasTipCap() *uint256.Int
    gasFeeCap() *uint256.Int
    value() *uint256.Int
    nonce() uint64
    to() *Address
}
```

**Spec References:**
- `refs/EIPs/EIPS/eip-1559.md` -- Dynamic fee transactions
- `refs/EIPs/EIPS/eip-4844.md` -- Blob transactions
- `refs/EIPs/EIPS/eip-7702.md` -- Set code transactions
- `refs/EIPs/EIPS/eip-7685.md` -- Execution layer requests
- `refs/EIPs/EIPS/eip-7928.md` -- Block access lists

### `pkg/core/types` -- Common Types

```go
const (
    HashLength    = 32
    AddressLength = 20
)

type Hash [HashLength]byte
type Address [AddressLength]byte
type Bloom [256]byte
type BlockNonce [8]byte

// Account represents an Ethereum account in the state trie.
type Account struct {
    Nonce    uint64
    Balance  *uint256.Int
    Root     Hash   // storage trie root
    CodeHash []byte // keccak256 of code
}

// Log represents a contract log event.
type Log struct {
    Address Address
    Topics  []Hash
    Data    []byte
    // derived fields
    BlockNumber uint64
    TxHash      Hash
    TxIndex     uint
    BlockHash   Hash
    Index       uint
}

// Receipt represents the result of a transaction.
type Receipt struct {
    Type              uint8
    PostState         []byte
    Status            uint64
    CumulativeGasUsed uint64
    Bloom             Bloom
    Logs              []*Log
    TxHash            Hash
    ContractAddress   Address
    GasUsed           uint64
    EffectiveGasPrice *uint256.Int
    BlobGasUsed       uint64
    BlobGasPrice      *uint256.Int
}
```

### `pkg/rlp` -- RLP Encoding

Implements Recursive Length Prefix encoding per the Yellow Paper Appendix B.

```go
// Encode serializes a Go value to RLP bytes.
func Encode(val interface{}) ([]byte, error)

// Decode deserializes RLP bytes into a Go value.
func Decode(input []byte, val interface{}) error

// EncodeToBytes is a convenience wrapper.
func EncodeToBytes(val interface{}) ([]byte, error)
```

**Spec:** Ethereum Yellow Paper, Appendix B.

### `pkg/core/vm` -- EVM Interpreter

```go
// EVM provides the execution environment for EVM bytecode.
type EVM struct {
    Context    BlockContext
    TxContext  TxContext
    StateDB   StateDB
    Config     Config
    chainRules ChainRules
    depth      int
}

// BlockContext provides block-level information to the EVM.
type BlockContext struct {
    BlockNumber *uint256.Int
    Time        uint64
    Coinbase    Address
    GasLimit    uint64
    BaseFee     *uint256.Int
    PrevRandao  Hash
    BlobBaseFee *uint256.Int
}

// Opcode dispatch via jump table.
type operation struct {
    execute     executionFunc
    constantGas uint64
    dynamicGas  gasFunc
    minStack    int
    maxStack    int
}

type JumpTable [256]*operation
```

**Spec References:**
- `refs/EIPs/EIPS/eip-7928.md` -- Access list tracking during execution
- `refs/EIPs/EIPS/eip-7904.md` -- Gas cost changes
- `refs/EIPs/EIPS/eip-4762.md` -- Statelessness gas costs

### `pkg/core/state` -- State Database

```go
// StateDB provides access to Ethereum world state.
type StateDB interface {
    // Account operations
    CreateAccount(addr Address)
    SubBalance(addr Address, amount *uint256.Int)
    AddBalance(addr Address, amount *uint256.Int)
    GetBalance(addr Address) *uint256.Int
    GetNonce(addr Address) uint64
    SetNonce(addr Address, nonce uint64)
    GetCode(addr Address) []byte
    SetCode(addr Address, code []byte)
    GetCodeHash(addr Address) Hash
    GetCodeSize(addr Address) int

    // Storage operations
    GetState(addr Address, key Hash) Hash
    SetState(addr Address, key Hash, value Hash)

    // Account existence
    Exist(addr Address) bool
    Empty(addr Address) bool

    // Snapshot and revert for tx-level atomicity
    Snapshot() int
    RevertToSnapshot(id int)

    // Commit writes all dirty state to the trie.
    Commit() (Hash, error)

    // EIP-7928: access tracking
    AddAccessedAddress(addr Address)
    AddAccessedSlot(addr Address, slot Hash)
    GetAccessList() *AccessList
}
```

### `pkg/bal` -- Block Access Lists (EIP-7928)

```go
// BlockAccessList records all state accesses during block execution.
// Used for parallel execution and stateless validation.
type BlockAccessList struct {
    Entries []AccessEntry
}

// AccessEntry records accesses for a single address.
type AccessEntry struct {
    Address        Address
    AccessIndex    uint64 // 0=pre-exec, 1..n=tx index, n+1=post-exec
    StorageReads   []StorageAccess
    StorageChanges []StorageChange
    BalanceChange  *BalanceChange
    NonceChange    *NonceChange
    CodeChange     *CodeChange
}

// StorageAccess records a storage slot read.
type StorageAccess struct {
    Slot  Hash
    Value Hash
}

// StorageChange records a storage slot write.
type StorageChange struct {
    Slot     Hash
    OldValue Hash
    NewValue Hash
}

// Encode serializes the BAL to RLP for header commitment.
func (bal *BlockAccessList) Encode() ([]byte, error)

// Hash computes the Keccak-256 hash of the RLP-encoded BAL.
func (bal *BlockAccessList) Hash() Hash

// ComputeParallelSets determines which transactions can execute concurrently.
func (bal *BlockAccessList) ComputeParallelSets() [][]int
```

**Spec:** `refs/EIPs/EIPS/eip-7928.md`
**Consensus Spec:** `refs/consensus-specs/specs/_features/eip7928/beacon-chain.md`

### `pkg/engine` -- Engine API

```go
// ExecutionPayloadV4 adds Block Access List support (Amsterdam/Glamsterdam).
type ExecutionPayloadV4 struct {
    ParentHash       Hash
    FeeRecipient     Address
    StateRoot        Hash
    ReceiptsRoot     Hash
    LogsBloom        Bloom
    PrevRandao       Hash
    BlockNumber      uint64
    GasLimit         uint64
    GasUsed          uint64
    Timestamp        uint64
    ExtraData        []byte
    BaseFeePerGas    *uint256.Int
    BlockHash        Hash
    Transactions     [][]byte
    Withdrawals      []*Withdrawal
    BlobGasUsed      uint64
    ExcessBlobGas    uint64
    ExecutionRequests [][]byte
    // EIP-7928
    BlockAccessList  *BlockAccessList
}

// ForkchoiceStateV1 represents the fork choice state from the CL.
type ForkchoiceStateV1 struct {
    HeadBlockHash      Hash
    SafeBlockHash      Hash
    FinalizedBlockHash Hash
}

// PayloadStatusV1 is the response to engine_newPayload.
type PayloadStatusV1 struct {
    Status          string // VALID, INVALID, SYNCING, ACCEPTED
    LatestValidHash *Hash
    ValidationError *string
}

// Engine API methods:
//   engine_newPayloadV4(payload, expectedBlobVersionedHashes, parentBeaconRoot, executionRequests)
//   engine_newPayloadV5(payload) -- adds BlockAccessList
//   engine_forkchoiceUpdatedV3(state, payloadAttributes)
//   engine_getPayloadV4(payloadId)
//   engine_getPayloadV5(payloadId) -- returns BlobsBundleV2 with cell proofs
//   engine_getPayloadV6(payloadId) -- returns BAL
```

**Spec References:**
- `refs/execution-apis/src/engine/prague.md` -- V4 methods
- `refs/execution-apis/src/engine/osaka.md` -- V5 (cell proofs)
- `refs/execution-apis/src/engine/amsterdam.md` -- V5/V6 (BAL)

### `pkg/witness` -- Execution Witness (EIP-8025)

```go
// ExecutionWitness contains all data needed for stateless block validation.
type ExecutionWitness struct {
    State     []StemStateDiff
    ParentRoot Hash
}

// StemStateDiff captures state diffs at a Verkle tree stem.
type StemStateDiff struct {
    Stem    [31]byte
    Suffixes []SuffixStateDiff
}

// SuffixStateDiff captures individual leaf-level diffs.
type SuffixStateDiff struct {
    Suffix   byte
    Current  *[32]byte // nil if not accessed
    New      *[32]byte // nil if not modified
}

// ExecutionProof is the ZK proof of correct execution.
type ExecutionProof struct {
    ProofType  uint8
    ProofBytes []byte // max 300 KiB
}

// Constants
const MaxProofSize = 300 * 1024 // 300 KiB
```

**Spec References:**
- `refs/consensus-specs/specs/_features/eip8025/beacon-chain.md`
- `refs/consensus-specs/specs/_features/eip8025/proof-engine.md`
- `refs/consensus-specs/specs/_features/eip8025/prover.md`
- `refs/consensus-specs/specs/_features/eip6800/beacon-chain.md`

### `pkg/crypto` -- Cryptographic Primitives

```go
// Keccak256 computes the Keccak-256 hash.
func Keccak256(data ...[]byte) []byte

// Keccak256Hash computes Keccak-256 and returns a Hash.
func Keccak256Hash(data ...[]byte) Hash

// Ecrecover recovers the public key from a signature.
func Ecrecover(hash []byte, sig []byte) ([]byte, error)

// Sign signs the hash with the given private key (secp256k1).
func Sign(hash []byte, prv *ecdsa.PrivateKey) ([]byte, error)

// ValidateSignature validates an ECDSA signature.
func ValidateSignature(pubkey, hash, sig []byte) bool

// Future: ML-DSA (FIPS 204) for post-quantum signatures
// Future: Falcon-512 (EIP-7619) precompile support
```

---

## 4. EIP-by-EIP Implementation Details

### EIP-7928: Block Access Lists (Parallel Execution)

**Spec:** `refs/EIPs/EIPS/eip-7928.md`

**Implementation Steps:**
1. Define `BlockAccessList` data structure with RLP encoding
2. Modify `StateProcessor.Process()` to record all accesses during execution
3. After sequential execution, build the BAL and compute `block_access_list_hash`
4. Add `block_access_list_hash` to `Header`
5. Implement parallel execution scheduler using BAL dependency graph
6. Add `BlockAccessList` field to `ExecutionPayloadV4`
7. Implement `engine_newPayloadV5` and `engine_getPayloadV6`

**Dependency Graph for Parallel Execution:**
```go
// ParallelScheduler determines safe parallel execution groups.
type ParallelScheduler struct {
    bal *BlockAccessList
}

// Schedule partitions transactions into parallel execution groups.
// Transactions in the same group have no state conflicts.
func (s *ParallelScheduler) Schedule(txCount int) []ExecutionGroup

type ExecutionGroup struct {
    TxIndices []int // indices of transactions that can run in parallel
}
```

### EIP-7732: ePBS (Execution Layer Side)

**Spec:** `refs/EIPs/EIPS/eip-7732.md`
**Consensus Spec:** `refs/consensus-specs/specs/gloas/`

**EL Changes:**
1. Payload is now a commitment (bid) rather than full payload in beacon block
2. Execution validation deferred to next slot
3. Builder bids contain execution payload commitment
4. EL must support split validation: validate payload separately from consensus

**Implementation Steps:**
1. Add `PayloadBid` type to engine package
2. Modify Engine API to support deferred payload delivery
3. Implement payload commitment verification
4. Add builder payment processing logic

### EIP-6800: Verkle Trees

**Spec:** `refs/EIPs/EIPS/eip-6800.md`
**Feature Spec:** `refs/consensus-specs/specs/_features/eip6800/`

**Implementation Steps:**
1. Implement Verkle tree node types (inner, leaf)
2. Pedersen commitment computation using Bandersnatch curve
3. Key derivation: `get_tree_key(address, tree_index, sub_index)`
4. Dual-tree state: frozen Patricia + growing Verkle
5. Witness generation from Verkle proofs
6. State migration from MPT to Verkle

### EIP-4444: History Expiry

**Spec:** `refs/EIPs/EIPS/eip-4444.md`

**Implementation Steps:**
1. Track chain age per block
2. Implement pruning for blocks older than `HISTORY_PRUNE_EPOCHS`
3. Add checkpoint sync from weak subjectivity point
4. Modify P2P to refuse serving pruned data
5. Add JSON-RPC compatibility layer (return errors for pruned data)

### EIP-7685: Execution Layer Requests

**Spec:** `refs/EIPs/EIPS/eip-7685.md`

**Implementation Steps:**
1. Define `Request` type: `request_type || request_data`
2. Implement `compute_requests_hash()` using SHA-256
3. Add `requests_hash` to block header
4. Process requests during block finalization
5. Forward requests to CL via Engine API

### EIP-7702: Set Code for EOAs

**Spec:** `refs/EIPs/EIPS/eip-7702.md`

**Implementation Steps:**
1. Define `SetCodeTx` (type 0x04) with authorization list
2. Implement authorization signature verification
3. Write delegation indicator `0xef0100 || address` to account code
4. Modify EVM to follow delegation pointers during CALL
5. Gas accounting: `PER_AUTH_BASE_COST = 12500`

### EIP-8079: Native Rollups (EXECUTE Precompile)

**Spec:** `refs/EIPs/EIPS/eip-8079.md` (draft)

**Implementation Steps:**
1. Define EXECUTE precompile interface
2. Implement stateless execution within precompile
3. Add `burned_fees` field to block header
4. Implement proof-carrying transaction type
5. Separate EIP-1559 gas target for rollup execution

### EIP-8025: Execution Proofs

**Spec:** `refs/consensus-specs/specs/_features/eip8025/`

**Implementation Steps:**
1. Define ExecutionProof SSZ containers
2. Implement proof verification engine
3. Add prover interface for ZK backend integration
4. Support multiple proof types (SP1, ZisK, etc.)
5. Integrate with Engine API for proof submission

---

## 5. Testing Strategy

### Unit Tests
- Every type has `_test.go` with table-driven tests
- RLP round-trip encoding for all types
- Hash computation verification against known vectors
- Gas calculation tests per opcode

### Integration Tests
- Block processing: build block -> validate -> apply state
- Engine API: mock CL client -> send payloads -> verify responses
- Parallel execution: verify BAL correctness and parallel results match sequential

### Conformance Tests
- Use `refs/execution-spec-tests/` for EVM conformance
- Verify against Ethereum JSON test vectors
- Compare state roots against reference implementations

### Fuzzing
- RLP decoder fuzzing
- Transaction signature recovery fuzzing
- EVM opcode fuzzing

---

## 6. Spec File Index

### EIP Specifications
| EIP | File | Description |
|-----|------|-------------|
| 7928 | `refs/EIPs/EIPS/eip-7928.md` | Block Access Lists |
| 7732 | `refs/EIPs/EIPS/eip-7732.md` | Enshrined PBS |
| 6800 | `refs/EIPs/EIPS/eip-6800.md` | Verkle Trees |
| 4444 | `refs/EIPs/EIPS/eip-4444.md` | History Expiry |
| 7594 | `refs/EIPs/EIPS/eip-7594.md` | PeerDAS |
| 7251 | `refs/EIPs/EIPS/eip-7251.md` | MaxEB |
| 7702 | `refs/EIPs/EIPS/eip-7702.md` | Set Code for EOAs |
| 7685 | `refs/EIPs/EIPS/eip-7685.md` | EL Requests |
| 7619 | `refs/EIPs/EIPS/eip-7619.md` | Falcon-512 Precompile |
| 7805 | `refs/EIPs/EIPS/eip-7805.md` | FOCIL |
| 4762 | `refs/EIPs/EIPS/eip-4762.md` | Statelessness Gas |
| 7840 | `refs/EIPs/EIPS/eip-7840.md` | Blob Schedule Config |
| 7904 | `refs/EIPs/EIPS/eip-7904.md` | Gas Repricing |
| 8079 | (draft) | EXECUTE Precompile |
| 8025 | (feature spec) | Execution Proofs |

### Consensus Specs
| Dir | Description |
|-----|-------------|
| `refs/consensus-specs/specs/fulu/` | PeerDAS, blob scheduling |
| `refs/consensus-specs/specs/gloas/` | ePBS, builder registry |
| `refs/consensus-specs/specs/_features/eip6800/` | Verkle Trees |
| `refs/consensus-specs/specs/_features/eip7805/` | FOCIL |
| `refs/consensus-specs/specs/_features/eip7928/` | Block Access Lists |
| `refs/consensus-specs/specs/_features/eip8025/` | Execution Proofs |

### Execution APIs
| File | Description |
|------|-------------|
| `refs/execution-apis/src/engine/prague.md` | V4 methods |
| `refs/execution-apis/src/engine/osaka.md` | V5 cell proofs |
| `refs/execution-apis/src/engine/amsterdam.md` | V5/V6 BAL |
