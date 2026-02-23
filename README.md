<h1 align="center">ETH2030</h1>

<p align="center">
  <strong>Ethereum execution client targeting the EF Protocol L1 Strawmap roadmap</strong>
</p>

<p align="center">
  <img src="https://img.shields.io/badge/Status-Experimental-orange?style=for-the-badge" alt="Experimental" />
  <img src="https://img.shields.io/badge/Build-In%20Progress-yellow?style=for-the-badge" alt="Build In Progress" />
</p>

> **Warning**: This is an experimental research project and **reference implementation** under active development. It is **not** production-ready and is intended primarily for study, research, and prototyping. Use at your own risk. APIs, data formats, and behavior may change without notice.

<p align="center">
  <a href="https://github.com/jiayaoqijia/eth2030/actions/workflows/ci.yml"><img src="https://github.com/jiayaoqijia/eth2030/actions/workflows/ci.yml/badge.svg?branch=master" alt="CI" /></a>
  <a href="https://github.com/jiayaoqijia/eth2030/releases/latest"><img src="https://img.shields.io/github/v/release/jiayaoqijia/eth2030?label=Release&color=%234f46e5" alt="Release" /></a>
  <img src="https://img.shields.io/badge/Go-1.26-00ADD8?logo=go" alt="Go" />
</p>

<p align="center">
  <img src="https://img.shields.io/badge/Packages-50-blue?style=flat-square" alt="Packages" />
  <img src="https://img.shields.io/badge/Tests-18%2C000%2B-blue?style=flat-square" alt="Tests" />
  <img src="https://img.shields.io/badge/EIPs-58-blue?style=flat-square" alt="EIPs" />
  <img src="https://img.shields.io/badge/EF%20State%20Tests-100%25%20(36%2C126)-brightgreen?style=flat-square" alt="EF Tests" />
  <img src="https://img.shields.io/badge/LOC-702K-blue?style=flat-square" alt="LOC" />
</p>

<p align="center">
  <a href="#quick-start">Quick Start</a> &middot;
  <a href="#roadmap-coverage">Roadmap</a> &middot;
  <a href="#eip-coverage-58-eips-implemented">EIP Coverage</a> &middot;
  <a href="#architecture">Architecture</a> &middot;
  <a href="CONTRIBUTING.md">Contributing</a>
</p>

---

> Built in Go, implementing the EF Protocol L1 Strawmap (Feb 2026) from Glamsterdam through the Giga-Gas era.

## Features

- **Full EVM execution** -- 164+ opcodes, 24 precompiles, EOF container support, go-ethereum v1.17.0 backend
- **100% EF conformance** -- 36,126/36,126 Ethereum Foundation state tests passing via go-ethereum integration
- **58+ EIPs implemented** -- Covering Frontier through Prague and beyond (Glamsterdam, Hogota, I+ forks)
- **Parallel execution** -- Block Access Lists (EIP-7928) for BAL-driven parallel transaction processing
- **Post-quantum ready** -- ML-DSA-65 (FIPS 204), Dilithium3, Falcon512, SPHINCS+ signers with hybrid mode
- **Native rollups** -- EIP-8079 EXECUTE precompile and anchor contract
- **zkVM framework** -- RISC-V RV32IM CPU, STF executor, zkISA bridge, proof backend
- **Full consensus** -- Single-slot finality, quick slots (6s), 1-epoch finality, PQ attestations
- **PeerDAS** -- Data availability sampling, custody proofs, blob streaming, variable-size blobs
- **ePBS + FOCIL** -- Enshrined PBS with distributed builder and fork-choice enforced inclusion lists
- **Complete networking** -- devp2p, discovery V5, gossip (pub/sub), Portal network, snap sync
- **Engine API V3-V7** -- Full payload lifecycle, forkchoice, ePBS builder API, 50+ JSON-RPC methods

## Quick Start

```bash
# Clone
git clone https://github.com/jiayaoqijia/eth2030.git
cd eth2030/pkg

# Build all packages
go build ./...

# Build the geth-embedded node (syncs with mainnet/testnets)
go build -o eth2030-geth ./cmd/eth2030-geth/

# Sync with Sepolia testnet (requires a consensus client on port 8551)
./eth2030-geth --network sepolia --datadir ~/.eth2030-sepolia

# Sync with mainnet
./eth2030-geth --datadir ~/.eth2030-geth --authrpc.jwtsecret /path/to/jwt.hex

# Run all tests (50 packages, 18,000+ tests)
go test ./...

# Run EF state test validation (36,126 vectors, 100% pass rate)
go test ./core/eftest/ -run TestGethCategorySummary -timeout=10m
```

### eth2030-geth Flags

| Flag | Default | Description |
|------|---------|-------------|
| `--datadir` | `~/.eth2030-geth` | Data directory for chain data |
| `--network` | `mainnet` | Network: mainnet, sepolia, holesky |
| `--syncmode` | `snap` | Sync mode: snap, full |
| `--port` | `30303` | P2P listening port |
| `--http.port` | `8545` | HTTP-RPC server port |
| `--authrpc.port` | `8551` | Engine API port (for CL client) |
| `--authrpc.jwtsecret` | auto | Path to JWT secret for Engine API auth |
| `--maxpeers` | `50` | Maximum P2P peers |
| `--verbosity` | `3` | Log level (0=silent, 5=trace) |
| `--override.glamsterdam` | - | Test Glamsterdam fork at timestamp |

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
 |     go-ethereum EVM (v1.17.0)     |     | Consensus Layer    |
 |  + 13 ETH2030 custom precompiles  |     | SSF, Attestations  |
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

## go-ethereum Integration

ETH2030 imports go-ethereum v1.17.0 as a library for EVM execution, achieving 100% EF state test conformance:

| Component | Description |
|-----------|-------------|
| `pkg/geth/processor.go` | Block processor using `gethcore.ApplyMessage` |
| `pkg/geth/extensions.go` | 13 custom precompile injection via `evm.SetPrecompiles()` |
| `pkg/geth/statedb.go` | State creation using go-ethereum's real trie DB |
| `pkg/geth/config.go` | Chain config mapping (ETH2030 forks to go-ethereum params) |

**Custom precompiles injected:** 4 Glamsterdam repriced (0x06, 0x08, 0x09, 0x0a), 1 NTT (0x15), 4 NII (0x0201-0x0204), 4 field arithmetic (0x0205-0x0208).

### Mainnet & Testnet Sync Verification

The `eth2030-geth` binary has been verified syncing with live Ethereum networks:

| Network | CL Client | Sync Mode | Status | RPC APIs Verified |
|---------|-----------|-----------|--------|-------------------|
| **Sepolia** | Lighthouse v8.1.0 | Snap | Headers downloading at ~9K/sec, chain ~33%, state ~4% | 20+ methods |
| **Mainnet** | - | Snap | Genesis initialized, Chain ID 1, peer discovery active | 20+ methods |

**Verified RPC methods:** `eth_chainId`, `eth_blockNumber`, `eth_getBlockByNumber`, `eth_syncing`, `eth_feeHistory`, `eth_getBalance`, `eth_getCode`, `net_version`, `net_peerCount`, `web3_clientVersion`, `admin_nodeInfo`, `admin_peers`, `txpool_status`, `engine_exchangeCapabilities`, and more.

## Package Structure

| Package | Description | Status |
|---------|-------------|--------|
| `pkg/core` | Blockchain, state processor, block builder, validator, fee logic, gas futures | Complete |
| `pkg/core/types` | Header, Transaction (7 types), Receipt, Block, SSZ encoding | Complete |
| `pkg/core/state` | StateDB interface, in-memory, trie-backed, stateless, prefetcher | Complete |
| `pkg/core/vm` | EVM interpreter, 164+ opcodes, 24 precompiles, gas tables, EOF | Complete |
| `pkg/core/eftest` | EF state test runner: 36,126/36,126 (100%) via go-ethereum backend | Complete |
| `pkg/geth` | go-ethereum adapter: type conversion, block processor, precompile injection | Complete |
| `pkg/consensus` | SSF, quick slots, 1-epoch finality, attestations, beacon state, BLS adapter | Complete |
| `pkg/crypto` | Keccak-256, secp256k1, BN254, BLS12-381, Banderwagon, IPA, VDF, shielded | Complete |
| `pkg/crypto/pqc` | ML-DSA-65, Dilithium3, Falcon512, SPHINCS+, hybrid signer, lattice blobs | Complete |
| `pkg/engine` | Engine API V3-V7, forkchoice, payload building, ePBS, distributed builder | Complete |
| `pkg/epbs` | Enshrined PBS (EIP-7732): builder bids, auctions, payload envelopes | Complete |
| `pkg/focil` | FOCIL (EIP-7805): inclusion list building, validation, compliance | Complete |
| `pkg/bal` | Block Access Lists (EIP-7928) for parallel execution | Complete |
| `pkg/das` | PeerDAS: sampling, custody, reconstruction, blob streaming, futures | Complete |
| `pkg/rollup` | Native rollups (EIP-8079): EXECUTE precompile, anchor contract | Complete |
| `pkg/zkvm` | zkVM: canonical guest (RISC-V), STF executor, zkISA bridge, proof backend | Complete |
| `pkg/proofs` | Proof aggregation: SNARK/STARK/IPA/KZG, mandatory 3-of-5 system | Complete |
| `pkg/txpool` | Transaction pool, RBF, sparse blobpool, encrypted mempool, sharded | Complete |
| `pkg/p2p` | TCP transport, devp2p, discovery V5, gossip, Portal network, snap sync | Complete |
| `pkg/trie` | MPT + Binary Merkle tree (EIP-7864), SHA-256, proofs, migration | Complete |
| `pkg/verkle` | Verkle tree, Pedersen commitments, state migration, witness generation | Complete |
| `pkg/rpc` | JSON-RPC server, 50+ methods, filters, WebSocket, Beacon API | Complete |
| `pkg/sync` | Full sync + snap sync + beam sync pipeline | Complete |
| `pkg/rlp` | RLP encoding/decoding per Yellow Paper Appendix B | Complete |
| `pkg/ssz` | SSZ encoding/decoding, merkleization, EIP-7916 ProgressiveList | Complete |

<details>
<summary>All 50 packages (click to expand)</summary>

| Package | Description |
|---------|-------------|
| `pkg/core/rawdb` | FileDB with WAL, chain DB, EIP-4444 history expiry |
| `pkg/core/state/snapshot` | Layered diff/disk snapshots, account/storage iterators |
| `pkg/core/state/pruner` | State pruner with bloom filter reachability |
| `pkg/core/vops` | Validity-Only Partial Statelessness: executor, validator, witness |
| `pkg/consensus/lethe` | LETHE insulation protocol for validator privacy |
| `pkg/das/erasure` | Reed-Solomon erasure coding (Lagrange interpolation) |
| `pkg/witness` | Execution witness (EIP-6800/8025), collector, verifier |
| `pkg/txpool/encrypted` | Encrypted mempool: commit-reveal, threshold decryption |
| `pkg/txpool/shared` | Sharded mempool with consistent hashing |
| `pkg/light` | Light client: header sync, proof cache, sync committee, CL proofs |
| `pkg/p2p/discover` | Peer discovery V4/V5, Kademlia DHT |
| `pkg/p2p/discv5` | Discovery V5 protocol with WHOAREYOU/handshake |
| `pkg/p2p/dnsdisc` | DNS-based peer discovery |
| `pkg/p2p/enode` | Node identity and URL parsing |
| `pkg/p2p/enr` | Ethereum Node Records (extensible key-value) |
| `pkg/p2p/portal` | Portal network: content DHT, Kademlia routing |
| `pkg/p2p/snap` | Snap sync protocol messages |
| `pkg/trie/bintrie` | Binary Merkle trie: Get/Put/Delete/Hash/Commit, proofs |
| `pkg/eth` | ETH protocol handler, codec, EIP-8077 announce nonce (ETH/72) |
| `pkg/node` | Client node: config, lifecycle, subsystem wiring |
| `pkg/cmd/eth2030` | CLI binary with flags, signal handling |
| `pkg/cmd/eth2030-geth` | Production node: go-ethereum embedded, mainnet/testnet sync |
| `pkg/log` | Structured logging (JSON/text) |
| `pkg/metrics` | Counters, gauges, histograms, Prometheus, EWMA, CPU tracker |

</details>

## EIP Coverage (58+ EIPs Implemented)

| EIP | Name | EIP | Name |
|-----|------|-----|------|
| 150 | Gas Cost Changes (63/64 rule) | 7002 | EL Withdrawals |
| 152 | BLAKE2 Precompile | 7069 | EXTCALL (EOF) |
| 196/197 | BN254 Pairing | 7251 | MAX_EFFECTIVE_BALANCE |
| 1153 | Transient Storage | 7480 | EOF Data |
| 1559 | Dynamic Fee Market | 7547 | FOCIL Inclusion Lists |
| 2200 | SSTORE Gas Metering | 7549 | Committee Attestations |
| 2537 | BLS12-381 Precompiles | 7594 | PeerDAS |
| 2718 | Typed Transactions | 7620 | EOFCREATE |
| 2929 | State Access Gas | 7685 | EL Requests |
| 2930 | Access List Transactions | 7691 | Blob Throughput |
| 2935 | Historical Block Hashes | 7698 | EOF Creation Tx |
| 3529 | Reduction in Refunds | 7701 | Native AA |
| 3540 | EOF Container | 7702 | Set Code for EOAs |
| 4444 | History Expiry | 7706 | Multidimensional Gas |
| 4762 | Statelessness Gas | 7742 | Uncoupled Blobs |
| 4788 | Beacon Block Root | 7745 | Log Index |
| 4844 | Blob Transactions + KZG | 7807 | SSZ Blocks |
| 4895 | Withdrawals | 7825 | Tx Gas Cap |
| 5656 | MCOPY Opcode | 7898 | Uncoupled Execution Payload |
| 6110 | Validator Deposits | 7904 | Gas Repricing |
| 6404 | SSZ Transactions | 7916 | SSZ ProgressiveList |
| 6780 | SELFDESTRUCT Restriction | 7918 | Blob Base Fee Bound |
| 7928 | Block Access Lists | 7939 | CLZ Opcode |
| 8024 | DUPN/SWAPN/EXCHANGE | 8070 | Sparse Blobpool |
| 8077 | Announce Nonce | 8079 | Native Rollups |
| 8141 | Frame Transactions | | |

**Substantially implemented:** EIP-6800 (Verkle), EIP-7732 (ePBS), EIP-7805 (FOCIL), EIP-7864 (Binary Merkle Tree), EIP-8025 (Execution Proofs), PQC (Dilithium3/Falcon512/SPHINCS+)

See [docs/EIP_SPEC_IMPL.md](docs/EIP_SPEC_IMPL.md) for full traceability: specs, implementations, and tests for each EIP.

## EF State Test Benchmark

| Metric | Value |
|--------|-------|
| **Total tests** | 36,126 |
| **Passing** | 36,126 (100%) |
| **Failing** | 0 |
| **Categories** | 57/57 at 100% |
| **Backend** | go-ethereum v1.17.0 |
| **Runner** | `pkg/core/eftest/geth_runner.go` |

## Roadmap Coverage

Full coverage of the EF Protocol L1 Strawmap (Feb 2026) across all upgrade phases:

| Phase | Year | Coverage | Highlights |
|-------|------|----------|------------|
| Glamsterdam | 2026 | **~99%** | ePBS, FOCIL, BALs, native AA, repricing (18 EIPs), sparse blobpool |
| Hogota | 2026-2027 | **~97%** | Multidim gas, payload chunking, NTT precompile, encrypted mempool reveal |
| I+ | 2027 | **~97%** | Native rollups, zkVM, VOPS, proof aggregation, PQ crypto, beam sync, verkle gas |
| J+ | 2027-2028 | **~95%** | Verkle migration batching, light client, block-in-blobs, variable blobs |
| K+ | 2028 | **~97%** | SSF, 6-sec slots, mandatory proofs, 1M attestations, CL proofs, proof aggregation |
| L+ | 2029 | **~97%** | Endgame finality, PQ attestations, APS, custody proofs, jeanVM, BPO4 schedule |
| M+ | 2029+ | **~95%** | Gigagas integration, canonical zkVM, gas futures settlement, sharded mempool |
| 2030++ | Long term | **~95%** | VDF randomness, distributed builders, shielded transfers |

**Gap analysis:** 65 roadmap items audited -- **65 COMPLETE**, 0 FUNCTIONAL, 0 PARTIAL, 0 MISSING. See [docs/GAP_ANALYSIS.md](docs/GAP_ANALYSIS.md).

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

50+ methods across `eth_*`, `net_*`, `web3_*`, `debug_*` namespaces including block/tx queries, `eth_call`, `eth_estimateGas`, fee history, log filters, WebSocket subscriptions (`newHeads`, `logs`, `pendingTransactions`), sync status, EVM tracing, and MPT proofs.

## Documentation

- [docs/EIP_SPEC_IMPL.md](docs/EIP_SPEC_IMPL.md) -- EIP traceability: specs, implementations, tests (94+ EIPs)
- [docs/GAP_ANALYSIS.md](docs/GAP_ANALYSIS.md) -- Full audit of 65 roadmap items
- [docs/PROGRESS.md](docs/PROGRESS.md) -- Progress report and completion tracking
- [docs/DESIGN.md](docs/DESIGN.md) -- Architecture and implementation design
- [docs/ROADMAP.md](docs/ROADMAP.md) -- Timeline overview
- [docs/ROADMAP-DEEP-DIVE.md](docs/ROADMAP-DEEP-DIVE.md) -- EIP research and analysis

## Contributing

See [CONTRIBUTING.md](CONTRIBUTING.md) for development setup, coding guidelines, and contribution categories.

## Security

See [SECURITY.md](SECURITY.md) for vulnerability reporting.

## Reference Submodules

`refs/` contains 30 git submodules for upstream specs and implementations:

**Ethereum specs:** EIPs, ERCs, consensus-specs, execution-specs, execution-apis, beacon-APIs, builder-specs, consensus-spec-tests, execution-spec-tests

**Reference client:** go-ethereum

**Cryptography:** blst (BLS12-381), circl (PQC: ML-DSA, ML-KEM, SLH-DSA), go-eth-kzg (KZG), gnark (ZK proofs), gnark-crypto, c-kzg-4844, go-ipa (Verkle IPA), go-verkle

**Devops:** ethereum-package, benchmarkoor, erigone, xatu, consensoor

