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
| `pkg/core/types` | Core Ethereum types: Header, Transaction (5 types), Receipt, Block, Account |
| `pkg/core/state` | StateDB interface and in-memory implementation |
| `pkg/core/vm` | EVM interpreter, opcodes, stack, memory, jump table |
| `pkg/rlp` | RLP encoding/decoding per Yellow Paper Appendix B |
| `pkg/crypto` | Keccak-256 hashing, secp256k1 ECDSA |
| `pkg/engine` | Engine API types (V1-V5), forkchoice, payload status |
| `pkg/bal` | Block Access Lists (EIP-7928) for parallel execution |
| `pkg/witness` | Execution witness (EIP-6800/8025) and ZK proof types |
| `pkg/txpool` | Transaction pool (planned) |
| `pkg/p2p` | P2P networking (planned) |

## Build & Test

```bash
cd pkg
go build ./...
go test ./...
```

## EIP Coverage

| EIP | Name | Status |
|-----|------|--------|
| 7928 | Block Access Lists (parallel execution) | Types + tracker implemented |
| 7732 | Enshrined PBS | Engine API types |
| 6800 | Verkle Trees | Witness types |
| 8025 | Execution Proofs | Proof types + verifier interface |
| 7685 | EL Requests | Header field |
| 7702 | Set Code for EOAs | Transaction type 0x04 |
| 4844 | Blob Transactions | Transaction type 0x03 |
| 1559 | Dynamic Fee Transactions | Transaction type 0x02 |
| 2930 | Access List Transactions | Transaction type 0x01 |
| 4444 | History Expiry | Planned |
| 7904 | Gas Repricing | Planned |
| 8079 | EXECUTE Precompile | Planned |

## Roadmap

See [docs/ROADMAP.md](docs/ROADMAP.md) for the full timeline and
[docs/DESIGN.md](docs/DESIGN.md) for implementation details.

## References

- `refs/` contains git submodules for all upstream specs and reference implementations
- [EF Protocol L1 Strawmap](https://strawmap.org) (Feb 2026)
- [Ethereum Yellow Paper](https://ethereum.github.io/yellowpaper/)
