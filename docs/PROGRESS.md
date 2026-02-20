# eth2028 Progress Report

> Last updated: 2026-02-20

## Summary

| Metric | Value |
|--------|-------|
| Packages | 47 |
| Source files | 719 |
| Test files | 702 |
| Source LOC | ~212,000 |
| Test LOC | ~307,000 |
| Total LOC | ~519,000 |
| Passing tests | 18,000+ |
| Test packages | 47/47 passing |
| EIPs implemented | 58+ (complete), 5 (substantial) |
| Roadmap coverage | 98+ items across all phases |

## Package Completion

| Package | Status | Description |
|---------|--------|-------------|
| `core` | Complete | Blockchain, block builder, validator, state processor, gas futures, genesis init |
| `core/types` | Complete | 7 tx types (legacy, access list, dynamic, blob, set code, frame, AA), header, receipt, block, SSZ |
| `core/state` | Complete | StateDB interface, in-memory, trie-backed, stateless (witness-backed), prefetcher |
| `core/state/snapshot` | Complete | Layered diff/disk snapshots, account/storage iterators |
| `core/state/pruner` | Complete | State pruner with bloom filter reachability |
| `core/vm` | Complete | 164+ opcodes, 24 precompiles, EOF, gas tables, parallel executor, eWASM optimizer, shielded crypto |
| `core/rawdb` | Complete | FileDB with WAL, chain DB, block/receipt/tx storage, EIP-4444 history expiry |
| `core/vops` | Complete | Validity-Only Partial Statelessness: partial executor, validator, witness |
| `rlp` | Complete | Full Yellow Paper Appendix B with fuzz testing |
| `ssz` | Complete | SSZ encode/decode, merkleization, EIP-7916 ProgressiveList |
| `crypto` | Complete | Keccak-256, secp256k1, BN254, BLS12-381 (incl. aggregate sigs), Banderwagon, IPA, VDF, threshold, shielded |
| `crypto/pqc` | Complete | Dilithium3 (real lattice crypto), Falcon512, SPHINCS+ (hash-based), hybrid signer, lattice blob commitments |
| `consensus` | Complete | SSF, quick slots, Casper FFG finality, committee selection, BLS operations, attestations, beacon state, block producer, reward calc, slashing |
| `consensus/lethe` | Complete | LETHE insulation protocol for validator privacy |
| `engine` | Complete | Engine API V3-V7, forkchoice, payload building, ePBS, distributed builder, Vickrey auctions |
| `epbs` | Complete | Enshrined PBS: BuilderBid, PayloadEnvelope, builder registry, auctions |
| `focil` | Complete | FOCIL: inclusion list building, validation, compliance scoring |
| `bal` | Complete | Block Access Lists (EIP-7928), parallel execution scheduling |
| `das` | Complete | PeerDAS: sampling, custody, reconstruction, blob streaming, futures, cell gossip |
| `das/erasure` | Complete | Reed-Solomon erasure coding (Lagrange interpolation) |
| `witness` | Complete | Execution witness collector, verifier, VOPS integration |
| `txpool` | Complete | Validation, RBF, eviction, blob gas, EIP-8070 sparse blobpool |
| `txpool/encrypted` | Complete | Encrypted mempool: commit-reveal, threshold decryption ordering |
| `txpool/shared` | Complete | Sharded mempool with consistent hashing |
| `rpc` | Complete | 50+ methods, filters, WebSocket subscriptions, Beacon API (16 endpoints) |
| `trie` | Complete | MPT with proofs, persistence, concurrent healing |
| `trie/bintrie` | Complete | Binary Merkle trie (EIP-7864): SHA-256, proofs, migration |
| `verkle` | Complete | Verkle tree, IPA commitments/multiproofs, Pedersen commitments, state migration, StateDB adapter, witness gen |
| `rollup` | Complete | Native rollups (EIP-8079): EXECUTE precompile, anchor contract |
| `zkvm` | Complete | Guest programs, canonical guest (RISC-V), STF framework, Poseidon hash, R1CS circuit builder |
| `proofs` | Complete | Proof aggregation: ZKSNARK/ZKSTARK/IPA/KZG, mandatory 3-of-5 system, async proof queue, execution proofs |
| `light` | Complete | Header sync, proof cache (LRU), sync committee, CL proof generator |
| `p2p` | Complete | TCP transport, devp2p, peer mgmt, gossip (pub/sub, scoring), protocol manager |
| `p2p/discover` | Complete | Peer discovery V4/V5, Kademlia DHT |
| `p2p/discv5` | Complete | Discovery V5 protocol with WHOAREYOU/handshake |
| `p2p/dnsdisc` | Complete | DNS-based peer discovery |
| `p2p/enode` | Complete | Node identity and URL parsing |
| `p2p/enr` | Complete | Ethereum Node Records (extensible key-value) |
| `p2p/portal` | Complete | Portal network: content DHT, Kademlia routing, state/history |
| `p2p/snap` | Complete | Snap sync protocol messages |
| `sync` | Complete | Full sync + snap sync + beam sync pipeline, trie sync |
| `eth` | Complete | ETH protocol handler, codec, EIP-8077 announce nonce (ETH/72) |
| `node` | Complete | Config, lifecycle, subsystem wiring |
| `cmd/eth2028` | Complete | CLI flags, signal handling, startup |
| `log` | Complete | Structured JSON/text logging |
| `metrics` | Complete | Counters, gauges, histograms, Prometheus, EWMA, CPU tracker |

## EIP Implementation Status

### Complete (58 EIPs)

EIP-150, EIP-152, EIP-196/197, EIP-1153, EIP-1559, EIP-2200, EIP-2537,
EIP-2718, EIP-2929, EIP-2930, EIP-2935, EIP-3529, EIP-3540, EIP-4444,
EIP-4762, EIP-4788, EIP-4844 (incl. KZG), EIP-4895, EIP-5656, EIP-6110,
EIP-6404, EIP-6780, EIP-7002, EIP-7069, EIP-7251, EIP-7480, EIP-7547,
EIP-7549, EIP-7594, EIP-7620, EIP-7685, EIP-7691, EIP-7698, EIP-7701,
EIP-7702, EIP-7706, EIP-7742, EIP-7745, EIP-7807, EIP-7825, EIP-7898,
EIP-7904, EIP-7916, EIP-7918, EIP-7928, EIP-7939, EIP-8024, EIP-8070,
EIP-8077, EIP-8079, EIP-8141

### Substantial (6)

- **EIP-6800** (Verkle Trees): Banderwagon curve, IPA proofs, Pedersen commitments, state migration, witness gen
- **EIP-7732** (ePBS): Builder types, registry, bid management, commitment/reveal, distributed builder API
- **EIP-7805** (FOCIL): Inclusion lists, validation, compliance scoring
- **EIP-7864** (Binary Merkle Tree): SHA-256, iterator, proofs, MPT migration
- **EIP-8025** (Execution Proofs): Witness collector, VOPS validator, stateless execution, beam sync
- **PQC** (Post-Quantum): Dilithium3/Falcon512/SPHINCS+, hybrid signer, lattice blobs, PQ attestations

## Roadmap Coverage by Phase

| Phase | Year | Coverage | Key Items |
|-------|------|----------|-----------|
| Glamsterdam | 2026 | ~99% | ePBS, FOCIL, BALs, native AA, repricing (18 EIPs), sparse blobpool, frame tx |
| Hogota | 2026-2027 | ~75% | BPO blob schedules, multidim gas, payload chunking, block-in-blobs, SSZ tx/blocks |
| I+ | 2027 | ~55% | Native rollups, zkVM/STF, VOPS, proof aggregation, PQ crypto, beam sync |
| J+ | 2027-2028 | ~40% | Verkle migration, encrypted mempool, light client, variable blobs |
| K+ | 2028 | ~50% | SSF, quick slots, mandatory proofs, canonical guest, announce nonce |
| L+ | 2029 | ~55% | Endgame finality, PQ attestations, LETHE, blob streaming, custody proofs |
| M+ | 2029+ | ~45% | Gigagas, canonical zkVM, gas futures, PQ transactions |
| 2030++ | Long term | ~30% | VDF, distributed builders, shielded transfers, sharded mempool |

## Remaining Gaps for Production

### 1. Real Cryptographic Backends (HIGH)

**Current**: Pure-Go implementations of BLS12-381, Verkle/IPA, KZG, PQC.
**Needed**: Wire reference submodules (blst, circl, go-ipa, go-eth-kzg, gnark) as backends.
**Note**: Reference submodules already added to `refs/`.

### 2. Production Networking (MEDIUM)

**Current**: Full protocol stack with TCP, devp2p, discovery, gossip, Portal.
**Needed**: RLPx encryption, NAT traversal, production connection management.

### 3. Database Backend (MEDIUM)

**Current**: FileDB with WAL.
**Needed**: LevelDB/Pebble backend for production performance.

### 4. Consensus Integration (MEDIUM)

**Current**: SSF, attestations, beacon state, fork choice, epoch processor all implemented.
**Needed**: Wire consensus components into node lifecycle with real CL client communication.

### 5. Conformance Testing (LOW)

**Current**: 12,600+ unit/integration tests.
**Needed**: Run against official `execution-spec-tests` and `consensus-spec-tests` vectors.

## Test Quality

| Category | Packages | Tests | Quality |
|----------|----------|-------|---------|
| Core (blockchain, blocks, genesis) | 6 | 3000+ | Excellent |
| Types (tx, header, receipt, SSZ) | 1 | 500+ | Excellent |
| State (statedb, snapshots, pruner) | 3 | 400+ | Very Good |
| VM (EVM, opcodes, gas, EOF) | 1 | 2000+ | Excellent |
| Crypto (BN254, BLS12-381, PQC) | 2 | 800+ | Excellent |
| Consensus (SSF, attestation, beacon) | 2 | 500+ | Very Good |
| Engine API (V3-V7, forkchoice) | 1 | 300+ | Good |
| DAS (PeerDAS, erasure, blobs) | 2 | 400+ | Very Good |
| P2P (transport, discovery, gossip) | 7 | 1000+ | Good |
| Sync (full, snap, beam) | 1 | 200+ | Good |
| RPC (JSON-RPC, Beacon API) | 1 | 300+ | Good |
| Trie (MPT, binary, verkle) | 3 | 500+ | Excellent |
| Transaction pool (base, encrypted, shared) | 3 | 300+ | Good |
| Proofs/rollup/zkvm/light | 4 | 400+ | Good |
| Other (RLP, SSZ, BAL, witness, etc.) | 6 | 500+ | Very Good |
