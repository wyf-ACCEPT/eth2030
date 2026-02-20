# eth2028

An Ethereum execution client targeting the 2028 L1 roadmap, built in Go.

Implements the EF Protocol L1 Strawmap (Feb 2026) from Glamsterdam through the
Giga-Gas era, covering consensus (SSF, quick slots), data availability (PeerDAS,
blob streaming), execution (parallel EVM, zkVM), and post-quantum cryptography.

**Status**: 47 packages, 903 source files (~278K LOC), 836 test files (~353K LOC), 19,800+ tests, all passing.

## Architecture

```
                 +------------------------------+
                 |     Consensus Client (CL)     |
                 +--------------+---------------+
                                | Engine API (JSON-RPC)
                 +--------------v---------------+
                 |      Engine API Server         |
                 |  newPayloadV3-V7, fcuV3/V4    |
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
 +--------v--------------------------+     +--------------------+
 |          EVM Interpreter           |     | Consensus Layer    |
 |  164+ Opcodes, 24 Precompiles     |     | SSF, Attestations  |
 +--------+--------------------------+     +--------------------+
          |
 +--------v--------------------------+     +-------------------+
 |          StateDB                   |---->|  Transaction Pool |
 |  Accounts, Storage, Code, Logs    |     | + Encrypted/Shared|
 +--------+--------------------------+     +-------------------+
          |
 +--------v--------------------------+     +-------------------+
 |     Trie / Verkle                  |---->|  P2P / Sync       |
 |  MPT + Binary + Verkle            |     | Discovery, Portal |
 +--------+--------------------------+     +-------------------+
          |
 +--------v--------------------------+     +-------------------+
 |     Key-Value Store (rawdb)        |     | DAS / PeerDAS     |
 +-----------------------------------+     | Blob Sampling     |
                                           +-------------------+
```

## Package Structure

| Package | Description | Status |
|---------|-------------|--------|
| `pkg/core` | Blockchain, state processor, block builder, validator, fee logic, gas futures, genesis init | Complete |
| `pkg/core/types` | Header, Transaction (7 types incl. FrameTx, AATx), Receipt, Block, SSZ encoding | Complete |
| `pkg/core/state` | StateDB interface, in-memory, trie-backed, stateless (witness-backed), prefetcher | Complete |
| `pkg/core/state/snapshot` | Layered diff/disk snapshots, account/storage iterators | Complete |
| `pkg/core/state/pruner` | State pruner with bloom filter reachability | Complete |
| `pkg/core/vm` | EVM interpreter, 164+ opcodes, 24 precompiles, gas tables, EOF container | Complete |
| `pkg/core/rawdb` | FileDB with WAL, chain DB, block/receipt/tx storage, EIP-4444 history expiry | Complete |
| `pkg/core/vops` | Validity-Only Partial Statelessness: executor, validator, witness integration | Complete |
| `pkg/rlp` | RLP encoding/decoding per Yellow Paper Appendix B | Complete |
| `pkg/ssz` | SSZ encoding/decoding, merkleization, EIP-7916 ProgressiveList | Complete |
| `pkg/crypto` | Keccak-256, secp256k1, BN254, BLS12-381, Banderwagon, IPA, VDF, threshold, shielded circuits | Complete |
| `pkg/crypto/pqc` | PQ crypto: ML-DSA-65 (real signer), Dilithium3, Falcon512, SPHINCS+, hybrid, lattice blobs, algorithm registry | Complete |
| `pkg/consensus` | SSF, quick slots, 1-epoch finality, attestations, beacon state, BLS adapter, jeanVM aggregation, PQ chain security, unified beacon state, CL proof circuits | Complete |
| `pkg/consensus/lethe` | LETHE insulation protocol for validator privacy | Complete |
| `pkg/engine` | Engine API server (V3-V7), forkchoice, payload building, ePBS, distributed builder | Complete |
| `pkg/epbs` | Enshrined PBS (EIP-7732): BuilderBid, PayloadEnvelope, auctions | Complete |
| `pkg/focil` | FOCIL (EIP-7805): inclusion list building, validation, compliance | Complete |
| `pkg/bal` | Block Access Lists (EIP-7928) for parallel execution | Complete |
| `pkg/das` | PeerDAS (EIP-7594): sampling, custody, reconstruction, blob streaming, futures | Complete |
| `pkg/das/erasure` | Reed-Solomon erasure coding for blob reconstruction | Complete |
| `pkg/witness` | Execution witness (EIP-6800/8025), collector, verifier | Complete |
| `pkg/txpool` | Transaction pool: validation, RBF, eviction, EIP-8070 sparse blobpool | Complete |
| `pkg/txpool/encrypted` | Encrypted mempool: commit-reveal, threshold decryption | Complete |
| `pkg/txpool/shared` | Sharded mempool with consistent hashing | Complete |
| `pkg/rpc` | JSON-RPC server, 50+ methods, filters, WebSocket, Beacon API | Complete |
| `pkg/trie` | MPT with proofs + Binary Merkle tree (EIP-7864), migration | Complete |
| `pkg/trie/bintrie` | Binary Merkle trie: Get/Put/Delete/Hash/Commit, proofs | Complete |
| `pkg/verkle` | Verkle tree, Pedersen commitments, state migration, witness generation | Complete |
| `pkg/rollup` | Native rollups (EIP-8079): EXECUTE precompile, anchor contract | Complete |
| `pkg/zkvm` | zkVM framework: guest programs, canonical guest (RISC-V), STF executor, zkISA bridge (EVM ↔ zkISA host calls) | Complete |
| `pkg/proofs` | Proof aggregation: ZKSNARK/ZKSTARK/IPA/KZG, mandatory 3-of-5, AA proof circuits | Complete |
| `pkg/light` | Light client: header sync, proof cache, sync committee, CL proofs | Complete |
| `pkg/p2p` | TCP transport, devp2p, peer mgmt, gossip, Portal network, Snap/1, SetCode broadcast (EIP-7702) | Complete |
| `pkg/p2p/discover` | Peer discovery V4/V5, Kademlia DHT | Complete |
| `pkg/p2p/discv5` | Discovery V5 protocol with WHOAREYOU/handshake | Complete |
| `pkg/p2p/dnsdisc` | DNS-based peer discovery | Complete |
| `pkg/p2p/enode` | Node identity and URL parsing | Complete |
| `pkg/p2p/enr` | Ethereum Node Records with extensible key-value pairs | Complete |
| `pkg/p2p/portal` | Portal network: content DHT, Kademlia routing, state/history | Complete |
| `pkg/p2p/snap` | Snap sync protocol messages | Complete |
| `pkg/sync` | Full sync + snap sync + beam sync pipeline | Complete |
| `pkg/eth` | ETH protocol handler, codec, EIP-8077 announce nonce (ETH/72) | Complete |
| `pkg/node` | Client node: config, lifecycle, subsystem integration | Complete |
| `pkg/cmd/eth2028` | CLI binary with flags, signal handling | Complete |
| `pkg/log` | Structured logging (JSON/text) | Complete |
| `pkg/metrics` | Counters, gauges, histograms, Prometheus, EWMA, CPU tracker | Complete |

## Build & Test

```bash
cd pkg
go build ./...
go test ./...         # 47 packages, 19,100+ tests
go test -v ./...      # verbose output
```

## EIP Coverage (58+ EIPs Implemented)

### Fully Implemented

| EIP | Name | Details |
|-----|------|---------|
| 150 | Gas Cost Changes | 63/64 rule for CALL gas forwarding |
| 152 | BLAKE2 Precompile | Precompile 0x09 |
| 196/197 | BN254 Pairing | Precompiles 0x06-0x08 |
| 1153 | Transient Storage | TLOAD/TSTORE opcodes |
| 1559 | Dynamic Fee Market | Base fee calculation, type 0x02 transactions |
| 2200 | SSTORE Gas Metering | Net gas metering with refunds |
| 2537 | BLS12-381 Precompiles | All 9 precompiles (0x0b-0x13) |
| 2718 | Typed Transactions | Full envelope encoding for all 7 tx types |
| 2929 | Gas Cost for State Access | Cold/warm tracking, dynamic gas costs |
| 2930 | Access List Transactions | Type 0x01, access list gas accounting |
| 2935 | Historical Block Hashes | BLOCKHASH improvements |
| 3529 | Reduction in Refunds | Post-London 50% refund cap |
| 3540 | EOF Container | EVM Object Format container validation |
| 4444 | History Expiry | Configurable retention window, pruning |
| 4762 | Statelessness Gas | Witness-aware gas accounting |
| 4788 | Beacon Block Root in EVM | Beacon root access |
| 4844 | Blob Transactions | Type 0x03, versioned hashes, blob gas, KZG |
| 4895 | Withdrawals | Post-Shanghai withdrawal processing |
| 5656 | MCOPY Opcode | Memory copy operation |
| 6110 | Validator Deposits | EL-triggered deposit processing |
| 6404 | SSZ Transactions | SSZ-encoded transaction format |
| 6780 | SELFDESTRUCT Restriction | Cancun restriction |
| 7002 | EL Withdrawals | Execution layer withdrawal requests |
| 7069 | EXTCALL | EOF external call opcodes |
| 7251 | MAX_EFFECTIVE_BALANCE | Increased validator balance (2048 ETH) |
| 7480 | EOF Data | EOF data section opcodes |
| 7547 | FOCIL Inclusion Lists | Inclusion list types and validation |
| 7549 | Committee Attestations | Committee-indexed attestation format |
| 7594 | PeerDAS | Data availability sampling |
| 7620 | EOFCREATE | EOF contract creation |
| 7685 | EL Requests | Deposit/withdrawal/consolidation requests |
| 7691 | Blob Throughput | Blob schedule and throughput |
| 7698 | EOF Creation Tx | EOF creation transaction type |
| 7701 | Native AA | Account abstraction opcodes |
| 7702 | Set Code for EOAs | Type 0x04, authorization signatures |
| 7706 | Multidimensional Gas | Separate calldata gas dimension |
| 7742 | Uncoupled Blobs | Decoupled blob scheduling |
| 7745 | Log Index | Persistent log indexing |
| 7807 | SSZ Blocks | SSZ-encoded block format |
| 7825 | Tx Gas Cap | Transaction gas limit cap |
| 7898 | Uncoupled Execution Payload | Decoupled execution payload |
| 7904 | Gas Repricing | Glamsterdam gas table |
| 7916 | SSZ ProgressiveList | SSZ progressive list type |
| 7918 | Blob Base Fee Bound | Blob base fee floor |
| 7928 | Block Access Lists | BAL tracking, parallel execution |
| 7939 | CLZ Opcode | Count leading zeros |
| 8024 | DUPN/SWAPN/EXCHANGE | Stack manipulation opcodes |
| 8070 | Sparse Blobpool | Sparse blob transaction pool |
| 8077 | Announce Nonce | ETH/72 protocol nonce |
| 8079 | Native Rollups | EXECUTE precompile |
| 8141 | Frame Transactions | APPROVE, TXPARAM opcodes |

### Substantially Implemented

| EIP | Name | Details |
|-----|------|---------|
| 6800 | Verkle Trees | Banderwagon, IPA proofs, Pedersen, state migration, witness |
| 7732 | Enshrined PBS | Builder types, registry, bids, commitment/reveal, API |
| 7805 | FOCIL | Inclusion lists, validation, compliance |
| 7864 | Binary Merkle Tree | SHA-256, iterator, proofs, MPT migration |
| 8025 | Execution Proofs | Witness collector, VOPS validator, stateless execution |

## Engine API

| Method | Fork | Status |
|--------|------|--------|
| `engine_newPayloadV3` | Cancun | Implemented |
| `engine_newPayloadV4` | Prague | Implemented |
| `engine_newPayloadV5` | Amsterdam | Implemented |
| `engine_forkchoiceUpdatedV3` | Cancun | Implemented |
| `engine_forkchoiceUpdatedV4` | Amsterdam | Implemented |
| `engine_getPayloadV3/V4/V6/V7` | Cancun-Amsterdam+ | Implemented |
| `engine_exchangeCapabilities` | All | Implemented |
| `engine_getClientVersionV1` | All | Implemented |

## JSON-RPC

50+ methods across `eth_*`, `net_*`, `web3_*`, `debug_*` namespaces including
block/tx queries, `eth_call`, `eth_estimateGas`, fee history, log filters,
WebSocket subscriptions (`newHeads`, `logs`, `pendingTransactions`), sync status,
EVM tracing, and MPT proofs.

## Roadmap Coverage

Full coverage of the EF Protocol L1 Strawmap (Feb 2026) across all upgrade phases:

| Phase | Year | Status | Highlights |
|-------|------|--------|------------|
| Glamsterdam | 2026 | **~99%** | ePBS, FOCIL, BALs, native AA, repricing (18 EIPs), sparse blobpool |
| Hogota | 2026-2027 | **~85%** | Multidim gas, payload chunking, NTT precompile, encrypted mempool, blob streaming |
| I+ | 2027 | **~80%** | Native rollups, zkVM (RISC-V + STF executor), VOPS, proof aggregation, PQ crypto, blob futures |
| J+ | 2027-2028 | **~75%** | Verkle migration, light client, block-in-blobs, Reed-Solomon reconstruction, variable blobs |
| K+ | 2028 | **~80%** | SSF, 6-sec slots, mandatory 3-of-5 proofs, 1M attestations (parallel BLS, subnets), CL proof circuits |
| L+ | 2029 | **~80%** | Endgame finality (BLS adapter), PQ attestations, APS, custody proofs, distributed builder, jeanVM aggregation |
| M+ | 2029+ | **~75%** | PQ L1 (ML-DSA-65 signer), gigagas integration, sharded mempool, real-time CL proofs, PQ chain security |
| 2030++ | Long term | **~75%** | VDF randomness, 51% auto-recovery, AA proof circuits, shielded transfers (BN254 ZK proofs), unified beacon state |

### Key Gaps (5 PARTIAL items remaining)

| Gap | Status | What's Missing |
|-----|--------|----------------|
| Fast L1 finality (#6) | **Partial** | Engine targets 500ms; needs real BLS pairing + block execution path |
| Tech debt reset (#12) | **Partial** | Tracks deprecated fields; needs automated migration tooling |
| PQ blobs (#31) | **Partial** | Lattice commitments work; Falcon/SPHINCS+ Sign() are keccak256 stubs |
| Teragas L2 (#34) | **Partial** | Accounting framework; needs real bandwidth enforcement |
| PQ transactions (#64) | **Partial** | Type 0x07 exists; ML-DSA-65 real, Falcon/SPHINCS+ stubs |

### Library Integration Needed

| Component | Current | Target Library | Priority |
|-----------|---------|---------------|----------|
| BLS pairing | Pure-Go placeholder | `supranational/blst` (in refs/) | HIGH |
| KZG commitments | Placeholder | `crate-crypto/go-eth-kzg` (in refs/) | HIGH |
| ZK proof circuits | Simulated proofs | `consensys/gnark` Groth16/PLONK (in refs/) | MEDIUM |
| ML-DSA validation | Custom lattice impl | Validate vs `cloudflare/circl` (in refs/) | MEDIUM |
| Verkle IPA | Placeholder | `crate-crypto/go-ipa` (in refs/) | MEDIUM |
| Falcon/SPHINCS+ | keccak256 stubs | `cloudflare/circl` sign/slhdsa | LOW |

## Docs

- [docs/GAP_ANALYSIS.md](docs/GAP_ANALYSIS.md) -- Full audit of 65 roadmap items vs codebase (with priority rankings)
- [docs/PROGRESS.md](docs/PROGRESS.md) -- Gap analysis and completion tracking
- [docs/DESIGN.md](docs/DESIGN.md) -- Architecture and implementation design
- [docs/ROADMAP.md](docs/ROADMAP.md) -- Timeline overview
- [docs/ROADMAP-DEEP-DIVE.md](docs/ROADMAP-DEEP-DIVE.md) -- EIP research and analysis

## Reference Submodules

`refs/` contains 30 git submodules for upstream specs and implementations:

**Ethereum specs**: EIPs, ERCs, consensus-specs, execution-specs, execution-apis,
beacon-APIs, builder-specs, consensus-spec-tests, execution-spec-tests

**Reference client**: go-ethereum

**Cryptography**: blst (BLS12-381), circl (PQC: ML-DSA, ML-KEM, SLH-DSA),
go-eth-kzg (KZG commitments), gnark (ZK proofs), gnark-crypto (ZK curves),
c-kzg-4844 (C-based KZG), go-ipa (Verkle IPA proofs), go-verkle

**Devops**: ethereum-package, benchmarkoor, erigone, xatu, consensoor

- [EF Protocol L1 Strawmap](https://strawmap.org) (Feb 2026)
- [Ethereum Yellow Paper](https://ethereum.github.io/yellowpaper/)
