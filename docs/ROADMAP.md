# L1 Strawmap — ETH2030 Implementation

> Derived from the [L1 Strawmap](https://strawmap.org/) — the EF Architecture team's Ethereum protocol roadmap.
> Last updated: 2026-02-25

## Timeline Overview

| Year | Phase | Key Features | Status |
|------|-------|-------------|--------|
| 2026 | Glamsterdam | ePBS, FOCIL, BALs, native AA, repricing, sparse blobpool | ~99% |
| 2026-2027 | Hegotá | Blob throughput, multidim gas, SSZ blocks/tx, payload chunking | ~97% |
| 2027 | I+ | Native rollups, zkVM, VOPS, proof aggregation, PQ crypto | ~97% |
| 2027-2028 | J+ | Encrypted mempool, light client, variable blobs | ~95% |
| 2028 | K+ | 3SF, quick slots, mandatory proofs, canonical guest | ~97% |
| 2029 | L+ | Endgame finality, PQ attestations, blob streaming | ~97% |
| 2029+ | M+ | Gigagas, canonical zkVM, gas futures, PQ transactions | ~95% |
| 2030++ | Long term | VDF, distributed builders, shielded transfers | ~95% |

## Strawmap Layers

### Consensus Layer (CL)

| Track | Feature | Phase | Status |
|-------|---------|-------|--------|
| Latency | Fast confirmation | Glamsterdam | Done |
| Latency | Single-slot finality | K+ | Done |
| Latency | 1-epoch finality | K+ | Done |
| Latency | 4-slot epochs | K+ | Done |
| Latency | 6-sec slots (quick slots) | K+ | Done |
| Latency | Endgame finality | L+ | Done |
| Latency | Fast L1 finality in seconds | M+ | Done |
| Accessibility | ePBS | Glamsterdam | Done |
| Accessibility | FOCIL | Glamsterdam | Done |
| Accessibility | Modernized beacon state | Hegotá | Done |
| Accessibility | Attester stake cap | L+ | Done |
| Accessibility | 1 ETH includers | L+ | Done |
| Accessibility | APS (committee selection) | L+ | Done |
| Accessibility | PQ attestations | L+ | Done |
| Accessibility | Distributed block building | 2030++ | Done |
| Cryptography | PQ custody replacer | I+ | Done |
| Cryptography | PQ signature share | L+ | Done |
| Cryptography | Real-time CL proofs | L+ | Done |
| Cryptography | PQ L1 hash-based | M+ | Done |
| Cryptography | VDF randomness | 2030++ | Done |

### Data Layer (DL)

| Track | Feature | Phase | Status |
|-------|---------|-------|--------|
| Throughput | PeerDAS | Glamsterdam | Done |
| Throughput | Sparse blobpool (EIP-8070) | Glamsterdam | Done |
| Throughput | Blob throughput increase | Hegotá | Done |
| Throughput | Local blob reconstruction | Hegotá | Done |
| Throughput | Decrease sample size | I+ | Done |
| Throughput | PQ blobs | M+ | Done |
| Throughput | Teradata L2 | 2030++ | Done |
| Types | Blob streaming | L+ | Done |
| Types | Short-dated blob futures | L+ | Done |
| Types | Variable-size blobs | I+ | Done |
| Types | Custody proofs | L+ | Done |
| Types | Forward-cast blobs | M+ | Done |

### Execution Layer (EL)

| Track | Feature | Phase | Status |
|-------|---------|-------|--------|
| Throughput | Conversion repricing | Glamsterdam | Done |
| Throughput | Natural gas limit | Hegotá | Done |
| Throughput | Access gas limit | Hegotá | Done |
| Throughput | Multidimensional pricing | Hegotá | Done |
| Throughput | Block in blobs | K+ | Done |
| Throughput | Mandatory 3-of-5 proofs | K+ | Done |
| Throughput | Canonical guest | K+ | Done |
| Throughput | Canonical zkVM | M+ | Done |
| Throughput | Gas futures | M+ | Done |
| Throughput | Shared mempools | M+ | Done |
| Throughput | Gigagas L1 (1 Ggas/sec) | M+ | Done |
| Sustainability | BALS | Glamsterdam | Done |
| Sustainability | Binary tree | Hegotá | Done |
| Sustainability | Payload shrinking | Hegotá | Done |
| Sustainability | Advance state | L+ | Done |
| Sustainability | Native rollups | L+ | Done |
| Sustainability | Exposed ELSA | 2030++ | Done |
| EVM | Native AA | Glamsterdam | Done |
| EVM | Misc purges | Hegotá | Done |
| EVM | Transaction assertions | Hegotá | Done |
| EVM | NTT precompile(s) | I+ | Done |
| EVM | Precompiles in zkISA | J+ | Done |
| EVM | STF in zkISA | J+ | Done |
| EVM | Proof aggregation | L+ | Done |
| EVM | PQ transactions | M+ | Done |
| EVM | AA proofs | M+ | Done |
| Cryptography | Encrypted mempool | I+ | Done |
| Cryptography | NII precompile | I+ | Done |

## Key EIPs

- **EIP-7732**: Enshrined Proposer-Builder Separation (ePBS)
- **EIP-7928**: Parallel EVM execution via access lists
- **EIP-4844**: Blob transactions with KZG commitments
- **EIP-7594**: PeerDAS (data availability sampling)
- **EIP-7702**: Set code for EOAs (native account abstraction)
- **EIP-7805**: FOCIL (fork-choice enforced inclusion lists)
- **EIP-8079**: Native rollups (EXECUTE precompile)
- **EIP-7251**: Increase MAX_EFFECTIVE_BALANCE (flexible staking)
- **EIP-4444**: History expiry (bound historical data retrieval)

## Project Stats

| Metric | Value |
|--------|-------|
| Packages | 48 |
| Source files | 991 |
| Test files | 918 |
| Source LOC | ~316,000 |
| Test LOC | ~397,000 |
| Passing tests | 18,257 |
| EIPs implemented | 58+ (complete), 5 (substantial) |
| Roadmap items | 65/65 COMPLETE |
| EF State Tests | 36,126/36,126 (100%) |
| Reference submodules | 30 |
