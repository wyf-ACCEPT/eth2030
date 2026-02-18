# eth2028

An Ethereum execution client targeting the 2028 L1 roadmap, built in Go.

Implements the EF Protocol L1 Strawmap (Feb 2026) from Glamsterdam through the
Giga-Gas era, covering parallel execution (EIP-7928), ePBS (EIP-7732), Verkle
state (EIP-6800), stateless validation (EIP-8025), and post-quantum cryptography.

**Status**: 22 packages, ~37K LOC source, ~69K LOC tests, 2900+ passing tests.

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
| `pkg/core` | Blockchain, state processor, block builder, validator, fee logic | Complete |
| `pkg/core/types` | Header, Transaction (5 types), Receipt, Block, Account | Complete |
| `pkg/core/state` | StateDB interface, in-memory impl, trie-backed state | Complete |
| `pkg/core/vm` | EVM interpreter, 140+ opcodes, 18 precompiles, gas tables | Complete |
| `pkg/core/rawdb` | FileDB with WAL, block/receipt/tx storage | Complete |
| `pkg/rlp` | RLP encoding/decoding per Yellow Paper Appendix B | Complete |
| `pkg/crypto` | Keccak-256, secp256k1, BN254, BLS12-381 | Complete |
| `pkg/engine` | Engine API server (V3-V6), forkchoice, payload building | Complete |
| `pkg/bal` | Block Access Lists (EIP-7928) for parallel execution | Complete |
| `pkg/witness` | Execution witness (EIP-6800/8025), collector, verifier | Framework |
| `pkg/txpool` | Transaction pool: validation, replace-by-fee, eviction | Complete |
| `pkg/rpc` | JSON-RPC server, 30+ eth_* methods, filters, subscriptions | Complete |
| `pkg/trie` | Merkle Patricia Trie with proofs | Complete |
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

### Partially Implemented

| EIP | Name | Gap |
|-----|------|-----|
| 4844 | KZG Point Evaluation | Format validation only; crypto verification stubbed |
| 6800 | Verkle Trees | Placeholder hashes; Banderwagon curve not implemented |
| 7732 | Enshrined PBS | Engine API types defined; builder consensus partial |
| 8025 | Execution Proofs | Witness collector complete; verification framework only |

### Planned

| EIP | Name | Target Phase |
|-----|------|-------------|
| 4762 | Statelessness Gas | Hogota |
| 8079 | EXECUTE Precompile | K+ |
| 4444 | History Expiry | Hogota |

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
