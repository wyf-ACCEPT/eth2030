# eth2028

An Ethereum execution client targeting the 2028 L1 roadmap, built in Go.

Implements the EF Protocol L1 Strawmap (Feb 2026) from Glamsterdam through the
Giga-Gas era, covering parallel execution (EIP-7928), ePBS (EIP-7732), Verkle
state (EIP-6800), stateless validation (EIP-8025), and post-quantum cryptography.

**Status**: 22 packages, ~41K LOC source, ~79K LOC tests, all passing.

## Architecture

```
                 +------------------------------+
                 |     Consensus Client (CL)     |
                 +--------------+---------------+
                                | Engine API (JSON-RPC)
                 +--------------v---------------+
                 |      Engine API Server         |
                 |  newPayloadV3-V5, fcuV3/V4    |
                 +--------------+---------------+
                                |
          +---------------------+------------------+
          |                     |                   |
 +--------v------+  +----------v--------+  +-------v-------+
 |  Block Builder |  |  Block Validator  |  | Payload Store |
 +--------+------+  +----------+--------+  +---------------+
          |                     |
 +--------v---------------------v------+
 |          State Processor             |
 |   Sequential -> Parallel (EIP-7928) |
 +--------+----------------------------+
          |
 +--------v--------------------------+
 |          EVM Interpreter           |
 |  140+ Opcodes, 18 Precompiles     |
 +--------+--------------------------+
          |
 +--------v--------------------------+     +-------------------+
 |          StateDB                   |---->|  Transaction Pool |
 |  Accounts, Storage, Code, Logs    |     +-------------------+
 +--------+--------------------------+
          |
 +--------v--------------------------+     +-------------------+
 |     Trie / Verkle                  |---->|  P2P / Sync       |
 |  MPT + Verkle (EIP-6800)          |     +-------------------+
 +--------+--------------------------+
          |
 +--------v--------------------------+
 |     Key-Value Store (rawdb)        |
 +-----------------------------------+
```

## Package Structure

| Package | Description | Status |
|---------|-------------|--------|
| `pkg/core` | Blockchain, state processor, block builder, validator, fee logic, FOCIL, calldata gas | Complete |
| `pkg/core/types` | Header, Transaction (5 types), Receipt, Block, Account, InclusionList | Complete |
| `pkg/core/state` | StateDB interface, in-memory impl, trie-backed state | Complete |
| `pkg/core/vm` | EVM interpreter, 140+ opcodes, 19 precompiles, gas tables, EIP-4762 statelessness gas | Complete |
| `pkg/core/rawdb` | FileDB with WAL, block/receipt/tx storage, EIP-4444 history expiry | Complete |
| `pkg/rlp` | RLP encoding/decoding per Yellow Paper Appendix B | Complete |
| `pkg/crypto` | Keccak-256, secp256k1, BN254, BLS12-381, KZG, Banderwagon, IPA proofs | Complete |
| `pkg/engine` | Engine API server (V3-V6), forkchoice, payload building, ePBS builder API | Complete |
| `pkg/bal` | Block Access Lists (EIP-7928) for parallel execution | Complete |
| `pkg/witness` | Execution witness (EIP-6800/8025), collector, verifier | Framework |
| `pkg/txpool` | Transaction pool: validation, replace-by-fee, eviction | Complete |
| `pkg/rpc` | JSON-RPC server, 50+ eth_* methods, filters, subscriptions | Complete |
| `pkg/trie` | MPT with proofs + Binary Merkle tree (EIP-7864), MPT migration | Complete |
| `pkg/verkle` | Verkle tree types, key derivation (EIP-6800) | Stub |
| `pkg/p2p` | TCP transport, devp2p handshake, peer management, server | In Progress |
| `pkg/sync` | Full sync + snap sync pipeline, header-first strategy | Framework |
| `pkg/eth` | ETH protocol handler, codec, message types | Complete |
| `pkg/node` | Client node: config, lifecycle, subsystem integration | Complete |
| `pkg/cmd/eth2028` | CLI binary with flags, signal handling | Complete |
| `pkg/log` | Structured logging (JSON/text) | Complete |
| `pkg/metrics` | Counters, gauges, histograms, Prometheus export | Complete |

## Build & Test

```bash
cd pkg
go build ./...
go test ./...
```

## EIP Coverage

### Fully Implemented

| EIP | Name | Details |
|-----|------|---------|
| 1559 | Dynamic Fee Market | Base fee calculation, type 0x02 transactions |
| 2718 | Typed Transactions | Full envelope encoding for all 5 tx types |
| 2929 | Gas Cost for State Access | Cold/warm tracking, dynamic gas costs |
| 2930 | Access List Transactions | Type 0x01, access list gas accounting |
| 2200 | SSTORE Gas Metering | Net gas metering with refunds |
| 2537 | BLS12-381 Precompiles | All 9 precompiles (0x0b-0x13) |
| 3529 | Reduction in Refunds | Post-London 50% refund cap |
| 4844 | Blob Transactions | Type 0x03, versioned hashes, blob gas |
| 4895 | Withdrawals | Post-Shanghai withdrawal processing |
| 5656 | MCOPY Opcode | Memory copy operation |
| 7685 | EL Requests | Deposit/withdrawal/consolidation requests |
| 7702 | Set Code for EOAs | Type 0x04, authorization signatures |
| 7904 | Gas Repricing | Glamsterdan jump table |
| 7928 | Block Access Lists | Tracking, hashing, parallel execution |
| 1153 | Transient Storage | TLOAD/TSTORE opcodes |
| 150 | Gas Cost Changes | 63/64 rule for CALL gas forwarding |
| 152 | BLAKE2 Precompile | Precompile 0x09 |
| 196/197 | BN254 Pairing | Precompiles 0x06-0x08 |
| 4844 | KZG Point Evaluation | BLS12-381 pairing-based verification |
| 7706 | Multidimensional Gas | Separate calldata gas dimension, header fields, EIP-1559 mechanics |
| 7547 | FOCIL Inclusion Lists | Inclusion list types, validation, engine API integration |
| 4444 | History Expiry | Configurable retention window, block body/receipt pruning |
| 4762 | Statelessness Gas | Witness-aware gas accounting for state access operations |

### Substantially Implemented

| EIP | Name | Details |
|-----|------|---------|
| 6800 | Verkle Trees | Banderwagon curve, IPA proofs, Pedersen commitments, types+keys |
| 7732 | Enshrined PBS | Builder types, registry, bid management, commitment/reveal, API |
| 7864 | Binary Merkle Tree | SHA-256 hashing, iterator, proofs, verification, MPT migration |

### Partially Implemented

| EIP | Name | Gap |
|-----|------|-----|
| 8025 | Execution Proofs | Witness collector complete; verification framework only |

### Planned

| EIP | Name | Target Phase |
|-----|------|-------------|
| 8079 | EXECUTE Precompile | K+ |
| 8007 | Glamsterdam Gas Repricings | Glamsterdam |
| 8037 | State Growth/Access Separation | Hogota |
| 7778 | Remove Gas Refunds | Glamsterdam |
| 8125 | Temporary Storage | Hogota |

## Engine API

| Method | Fork | Status |
|--------|------|--------|
| `engine_newPayloadV3` | Cancun | Implemented |
| `engine_newPayloadV4` | Prague | Implemented |
| `engine_newPayloadV5` | Amsterdam | Implemented |
| `engine_forkchoiceUpdatedV3` | Cancun | Implemented |
| `engine_forkchoiceUpdatedV4` | Amsterdam | Implemented |
| `engine_getPayloadV3/V4/V6` | Cancun-Amsterdam | Implemented |
| `engine_exchangeCapabilities` | All | Implemented |
| `engine_getClientVersionV1` | All | Implemented |

## JSON-RPC

30+ methods across `eth_*`, `net_*`, `web3_*` namespaces including block/tx
queries, `eth_call`, `eth_estimateGas`, fee history, log filters, WebSocket
subscriptions (`newHeads`, `logs`, `pendingTransactions`), and sync status.

## Progress

See [docs/PROGRESS.md](docs/PROGRESS.md) for a detailed gap analysis and
completion status for each package.

## Docs

- [docs/PROGRESS.md](docs/PROGRESS.md) -- Gap analysis and completion tracking
- [docs/DESIGN.md](docs/DESIGN.md) -- Architecture and implementation design
- [docs/ROADMAP.md](docs/ROADMAP.md) -- Timeline overview
- [docs/ROADMAP-DEEP-DIVE.md](docs/ROADMAP-DEEP-DIVE.md) -- EIP research and analysis

## References

- `refs/` contains git submodules for all upstream specs and reference implementations
- [EF Protocol L1 Strawmap](https://strawmap.org) (Feb 2026)
- [Ethereum Yellow Paper](https://ethereum.github.io/yellowpaper/)
