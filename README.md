# eth2028

An Ethereum execution client targeting the 2028 L1 roadmap, built in Go.

Implements the EF Protocol L1 Strawmap (Feb 2026) from Glamsterdam through the
Giga-Gas era, covering parallel execution (EIP-7928), ePBS (EIP-7732), Verkle
state (EIP-6800), stateless validation (EIP-8025), and post-quantum cryptography.

## Architecture

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
 └──────────────────────────────┘
```

## Package Structure

| Package | Description |
|---------|-------------|
| `pkg/core` | Blockchain, state processor, block builder, block validator, base fee logic |
| `pkg/core/types` | Core Ethereum types: Header, Transaction (5 types), Receipt, Block, Account |
| `pkg/core/state` | StateDB interface and in-memory implementation |
| `pkg/core/vm` | EVM interpreter, 140+ opcodes, 9 precompiles, gas tables (Frontier-Glamsterdan) |
| `pkg/core/rawdb` | Raw database access for blocks, receipts, tx lookups |
| `pkg/rlp` | RLP encoding/decoding per Yellow Paper Appendix B |
| `pkg/crypto` | Keccak-256 hashing, secp256k1 ECDSA |
| `pkg/engine` | Engine API server (V3-V6), forkchoice, payload building |
| `pkg/bal` | Block Access Lists (EIP-7928) for parallel execution |
| `pkg/witness` | Execution witness (EIP-6800/8025), collector, verifier |
| `pkg/txpool` | Transaction pool with validation, replace-by-fee, eviction |
| `pkg/rpc` | JSON-RPC server with eth_* namespace, filters, subscriptions |
| `pkg/trie` | Merkle Patricia Trie for state/tx/receipt roots |
| `pkg/p2p` | P2P networking: discovery, DEVp2p, ETH wire protocol |

## Build & Test

```bash
cd pkg
go build ./...
go test ./...
```

## EIP Coverage

### Implemented

| EIP | Name | Status |
|-----|------|--------|
| 7928 | Block Access Lists | Types, tracker, header hash, block builder integration |
| 7685 | EL Requests | Request processing, deposit/withdrawal/consolidation |
| 7702 | Set Code for EOAs | Transaction type 0x04 |
| 7904 | Gas Repricing | Glamsterdan jump table with updated gas costs |
| 4844 | Blob Transactions | Transaction type 0x03 |
| 1559 | Dynamic Fee Transactions | Transaction type 0x02, base fee calculation |
| 2930 | Access List Transactions | Transaction type 0x01 |
| 2929 | Gas Cost for State Access | Cold/warm access tracking, dynamic gas |
| 8025 | Execution Proofs | Witness collector, verifier interface, proof types |
| 6800 | Verkle Trees | Witness types, stateless validation framework |
| 7732 | Enshrined PBS | Engine API types V1-V5 |
| 150 | Gas Cost Changes (63/64 Rule) | CALL/CREATE gas forwarding |
| 2200 | SSTORE Gas Metering | Net gas metering with refunds |
| 3529 | Reduction in Refunds | Post-London refund cap |

### Planned

| EIP | Name | Target Phase |
|-----|------|-------------|
| 4762 | Statelessness Gas | Hogota |
| 8079 | EXECUTE Precompile | K+ |
| 4444 | History Expiry | Hogota |

## Engine API Methods

| Method | Fork | Status |
|--------|------|--------|
| `engine_newPayloadV3` | Cancun | Implemented |
| `engine_newPayloadV4` | Prague | Implemented |
| `engine_newPayloadV5` | Amsterdam | Implemented |
| `engine_forkchoiceUpdatedV3` | Cancun | Implemented |
| `engine_forkchoiceUpdatedV4` | Amsterdam | Implemented |
| `engine_getPayloadV3` | Cancun | Implemented |
| `engine_getPayloadV4` | Prague | Implemented |
| `engine_getPayloadV6` | Amsterdam | Implemented |
| `engine_exchangeCapabilities` | All | Implemented |
| `engine_getClientVersionV1` | All | Implemented |

## JSON-RPC Methods

| Method | Status |
|--------|--------|
| `eth_blockNumber` | Implemented |
| `eth_getBlockByNumber` | Implemented (with fullTx) |
| `eth_getBlockByHash` | Implemented (with fullTx) |
| `eth_getBalance` | Implemented |
| `eth_getTransactionCount` | Implemented |
| `eth_getCode` | Implemented |
| `eth_getStorageAt` | Implemented |
| `eth_call` | Implemented |
| `eth_estimateGas` | Implemented |
| `eth_gasPrice` | Implemented |
| `eth_maxPriorityFeePerGas` | Implemented |
| `eth_feeHistory` | Implemented |
| `eth_chainId` | Implemented |
| `eth_getTransactionByHash` | Implemented |
| `eth_getTransactionReceipt` | Implemented |
| `eth_getBlockReceipts` | Implemented |
| `eth_getLogs` | Implemented |
| `eth_newFilter` | Implemented |
| `eth_newBlockFilter` | Implemented |
| `eth_newPendingTransactionFilter` | Implemented |
| `eth_getFilterChanges` | Implemented |
| `eth_getFilterLogs` | Implemented |
| `eth_uninstallFilter` | Implemented |
| `eth_subscribe` | Implemented (newHeads, logs, pendingTx) |
| `eth_unsubscribe` | Implemented |
| `eth_syncing` | Implemented |
| `eth_createAccessList` | Implemented |
| `net_version` | Implemented |
| `net_peerCount` | Implemented |
| `web3_clientVersion` | Implemented |

## Roadmap

See [docs/ROADMAP.md](docs/ROADMAP.md) for the full timeline and
[docs/DESIGN.md](docs/DESIGN.md) for comprehensive implementation details
covering all L1 strawmap phases and EIP specifications.

## References

- `refs/` contains git submodules for all upstream specs and reference implementations
- [EF Protocol L1 Strawmap](https://strawmap.org) (Feb 2026)
- [Ethereum Yellow Paper](https://ethereum.github.io/yellowpaper/)
