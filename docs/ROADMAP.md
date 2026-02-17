# Ethereum 2028 Client Roadmap

> Derived from EF Protocol L1 Strawmap (Feb 2026) and community research.

## Timeline Overview

| Year | Phase | Feature | Category | Reference | Description |
|------|-------|---------|----------|-----------|-------------|
| 2026 | Glamsterdam (H1) | ePBS (Enshrined PBS) | Decentralization | EIP-7732 | Hardcodes block-building on-chain to kill MEV monopolies. |
| 2026 | Glamsterdam (H1) | Parallel Execution | Beast Mode (Offense) | EIP-7928 | Uses "Access Lists" to process non-conflicting transactions simultaneously. |
| 2026 | Hegota (H2) | Verkle Trees | Lean Mode (Efficiency) | EIP-6800 | Enables "Stateless Clients," allowing nodes to run on mobile/laptop hardware. |
| 2026 | Hegota (H2) | PeerDAS | Scaling | PeerDAS Research | Boosts L2 data capacity (Blobs) to handle higher transaction volumes. |
| 2027 | The Beam Chain | Consensus 2.0 | Lean Mode (Efficiency) | Beam Chain Specs | Rebuilds the consensus layer to be 100% ZK-native from day one. |
| 2027 | Beam Chain Phase | Single Slot Finality (SSF) | Fast Finality | SSF Research | Reduces transaction "irreversibility" time from 12 mins to 12 seconds. |
| 2027 | The Purge | History Expiry | Lean Mode (Efficiency) | EIP-4444 | Nodes delete data >1yr old, keeping the chain "Lean" and easy to sync. |
| 2027 | Native Rollup L0 | EXECUTE Precompile | Beast Mode (Offense) | Native Rollup Research | Moves rollup verification from smart contracts into the L1 protocol itself. |
| 2028 | BEAST MODE | Giga-Gas L1 | Scaling | SP1 / LeanVM | Targets 10,000+ TPS on L1 through real-time ZK-proving. |
| 2028 | TERA-GAS | 1M+ TPS Ecosystem | Scaling | Blob 2.0 | Unified L2 capacity reaching millions of TPS via Native & Based Rollups. |
| 2028 | FORT MODE | ML-DSA Signatures | Fort Mode (Defense) | NIST PQC Standards | Implements Lattice-based cryptography for native Quantum Resistance. |
| 2028 | Staking Reform | 1 ETH Solo Staking | Decentralization | EIP-7251 | Lowers validator bar from 32 ETH to 1 ETH via ZK-signature aggregation. |

## Strategic Categories

### Beast Mode (Offense) -- Raw Performance
- **Parallel Execution** (2026): EVM processes independent transactions concurrently
- **Giga-Gas L1** (2028): 1 Ggas/sec target via real-time ZK-proving (SP1/LeanVM)
- **Native Rollups** (2027): L1 natively verifies rollup execution, no smart contract overhead
- **Tera-Gas Ecosystem** (2028): L1 + L2 unified throughput reaching 1M+ TPS

### Lean Mode (Efficiency) -- Minimal Hardware Requirements
- **Verkle Trees** (2026): Replace Merkle-Patricia tries; enable stateless clients
- **History Expiry** (2027): Prune chain data older than 1 year
- **Beam Chain** (2027): ZK-native consensus layer rebuild from scratch

### Fort Mode (Defense) -- Quantum Resistance
- **ML-DSA Signatures** (2028): NIST post-quantum lattice-based cryptography
- **Post-quantum custody** (2027+): Replace BLS with quantum-resistant schemes
- **Post-quantum attestations** (2029+): Full CL quantum resistance

### Decentralization
- **ePBS** (2026): Enshrined proposer-builder separation, ending MEV centralization
- **1 ETH Staking** (2028): Lower solo staking from 32 ETH to 1 ETH
- **Distributed block building** (2030+): Remove single-builder bottleneck entirely

### Fast Finality
- **Single Slot Finality** (2027): 12-second irreversibility (down from ~12 minutes)
- **Endgame finality** (2029+): Sub-second finality

## Key EIPs

- **EIP-7732**: Enshrined Proposer-Builder Separation (ePBS)
- **EIP-7928**: Parallel EVM execution via access lists
- **EIP-6800**: Ethereum state using Verkle Trees
- **EIP-4444**: History expiry (bound historical data retrieval)
- **EIP-7251**: Increase MAX_EFFECTIVE_BALANCE (enables flexible staking)
