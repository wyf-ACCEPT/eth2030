# eth2028 Progress Report

> Last updated: 2026-02-18

## Summary

| Metric | Value |
|--------|-------|
| Packages | 22 |
| Source LOC | ~37,000 |
| Test LOC | ~69,000 |
| Passing tests | 2,900+ |
| Test files | 129 |
| Overall completion | ~74% mainnet-equivalent |

## Package Completion

| Package | LOC | Completion | Notes |
|---------|-----|------------|-------|
| core | 17K | 85% | Blockchain, block builder, validator, state processor |
| core/types | 5K | 95% | All 5 tx types, header, receipt, block |
| core/state | 4.5K | 90% | MemoryStateDB, trie-backed, snapshots, access lists |
| core/vm | 16K | 90% | 140+ opcodes, 18 precompiles, Frontier-Glamsterdan gas |
| core/rawdb | 2K | 85% | FileDB with WAL, block/receipt/tx storage |
| rlp | 1.4K | 100% | Full Yellow Paper Appendix B |
| crypto | 6K | 95% | Keccak, secp256k1, BN254, BLS12-381 |
| engine | 5K | 95% | V3-V6 payload/forkchoice APIs |
| bal | 1.2K | 90% | BAL tracking, hashing, parallel conflict detection |
| witness | 2.5K | 75% | Collector complete, verification framework only |
| txpool | 3K | 85% | Validation, pricing, eviction, blob gas |
| p2p | 7.5K | 70% | TCP transport, devp2p handshake, peer mgmt, server lifecycle |
| rpc | 5.6K | 90% | 30+ methods, filters, WebSocket subscriptions |
| sync | 5K | 70% | Full + snap sync pipeline, no peer integration |
| trie | 4K | 85% | MPT with proofs and persistence |
| verkle | 1.8K | 30% | Placeholder hashes, no Banderwagon curve |
| eth | 1.3K | 80% | Protocol handler, codec, message types |
| node | 1.5K | 85% | Config, lifecycle, subsystem wiring |
| cmd/eth2028 | 0.4K | 95% | CLI flags, signal handling, startup |
| log | 0.3K | 95% | Structured JSON/text logging |
| metrics | 0.6K | 85% | Counters, gauges, histograms, Prometheus |

## Top 5 Gaps for Mainnet

### 1. Verkle Tree Cryptography (CRITICAL)

**Current**: Placeholder using SHA256 hashes instead of Banderwagon curve points.
**Needed**: Banderwagon elliptic curve, IPA proof generation/verification,
proper commitment computation, integration into state root calculation.
**Blocks**: Stateless validation, witness verification.

### 2. KZG Proof Verification (DONE)

**Current**: Full pairing-based KZG verification with BLS12-381 optimal ate pairing.
Commit/proof/verify pipeline, G1 compress/decompress (ZCash format), trusted setup.
**Remaining**: Load production trusted setup from Ethereum KZG ceremony output.

### 3. Production Networking (MEDIUM)

**Current**: TCP transport, devp2p handshake, server lifecycle, peer management.
**Needed**: RLPx encryption, peer discovery (discv4/v5), NAT traversal,
production connection management.

### 4. Witness Verification (MEDIUM)

**Current**: Collection framework complete; verification is framework only.
**Needed**: State diff verification against Verkle proofs, ZK proof framework.
**Blocked by**: Verkle implementation (#1).

### 5. Sync Peer Integration (MEDIUM)

**Current**: Full + snap sync state machines work; no real peer communication.
**Needed**: Peer selection, request distribution, network I/O integration.
**Blocked by**: Production networking (#3).

## EIP Implementation Status

### Complete (18 EIPs)

- **EIP-1559**: Dynamic fee market, base fee calculation
- **EIP-2718**: Typed transaction envelopes (all 5 types)
- **EIP-2929**: Cold/warm state access gas costs
- **EIP-2930**: Access list transactions (type 0x01)
- **EIP-2200**: SSTORE net gas metering
- **EIP-2537**: BLS12-381 precompiles (9 precompiles, 0x0b-0x13)
- **EIP-3529**: Post-London 50% refund cap
- **EIP-4844**: Blob transactions (type 0x03), blob gas accounting
- **EIP-4895**: Post-Shanghai withdrawals
- **EIP-5656**: MCOPY opcode
- **EIP-7685**: EL requests (deposits, withdrawals, consolidations)
- **EIP-7702**: Set code for EOAs (type 0x04)
- **EIP-7904**: Glamsterdan gas repricing
- **EIP-7928**: Block access lists, parallel execution
- **EIP-1153**: Transient storage (TLOAD/TSTORE)
- **EIP-150**: 63/64 gas forwarding rule
- **EIP-152**: BLAKE2 precompile
- **EIP-196/197**: BN254 pairing precompiles
- **EIP-4844 KZG**: Full pairing-based KZG verification (BLS12-381 optimal ate)

### Partial (3 EIPs)

- **EIP-6800**: Verkle types and key derivation; no Banderwagon curve
- **EIP-7732**: Engine API types V1-V5; builder consensus integration partial
- **EIP-8025**: Witness collector complete; verification framework only

### Planned (3 EIPs)

- **EIP-4762**: Statelessness gas costs (Hogota)
- **EIP-8079**: EXECUTE precompile (K+)
- **EIP-4444**: History expiry (Hogota)

## Completion by Category

| Category | Completion | Details |
|----------|------------|---------|
| EVM & Execution | 92% | Opcodes, gas, precompiles all working |
| Transaction Pool | 88% | Validation, pricing, eviction complete |
| State Management | 87% | In-memory perfect, Verkle is gap |
| RPC API | 90% | 30+ methods, filters, subscriptions |
| Block Validation | 90% | Header, body, execution, receipt validation |
| Engine API | 95% | V3-V6 payload and forkchoice |
| Cryptography | 90% | BN254, BLS12-381, KZG pairing done; Verkle is gap |
| P2P Networking | 70% | TCP transport, handshake, server; needs RLPx, discovery |
| Sync | 70% | State machine complete, no peer integration |
| Database | 85% | FileDB works, no RocksDB/LevelDB |

## Test Quality

| Category | Files | Quality |
|----------|-------|---------|
| Core (blockchain, blocks) | 22 | Excellent |
| Types (tx, header, receipt) | 11 | Very Good |
| State (statedb, snapshots) | 7 | Very Good |
| VM (EVM, opcodes, gas) | 19 | Excellent |
| E2E integration | 3 | Excellent |
| Crypto (BN254, BLS12-381) | 8 | Excellent |
| Engine API | 6 | Good |
| RPC | 4 | Good |
| P2P (transport, handshake) | 5 | Good |
| Sync | 3 | Fair |
| Witness | 2 | Very Good |
| Verkle | 2 | Limited |
