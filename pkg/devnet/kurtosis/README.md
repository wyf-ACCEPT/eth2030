# ETH2030 Kurtosis Devnet

Local multi-client Ethereum devnets using [Kurtosis](https://www.kurtosis.com/) and the [ethpandaops/ethereum-package](https://github.com/ethpandaops/ethereum-package).

## Prerequisites

- **Docker**: Running and accessible (`docker info` should succeed)
- **Kurtosis CLI**: Install from https://docs.kurtosis.com/install/

Start the Kurtosis engine before first use:

```bash
kurtosis engine start
```

## Quick Start

Build the Docker image and launch a single-client devnet:

```bash
# Using the helper script
./scripts/run-devnet.sh single-client

# Or manually
docker build -t eth2030:local ../../
kurtosis run github.com/ethpandaops/ethereum-package \
    --args-file configs/single-client.yaml \
    --enclave eth2030-devnet
```

## Feature-by-Feature Testing

Test each ETH2030 roadmap feature with its own devnet config:

```bash
# Run ALL 15 feature tests sequentially
./scripts/run-feature-tests.sh

# Run specific features
./scripts/run-feature-tests.sh epbs focil native-aa blobs

# List available features
./scripts/run-feature-tests.sh --list
```

### Feature Test Matrix

| # | Feature | Config | EIP(s) | Nodes | Key Tools |
|---|---------|--------|--------|-------|-----------|
| 1 | ePBS | `features/epbs.yaml` | EIP-7732 | 4 | spamoor (eoatx + uniswap), assertoor, prometheus |
| 2 | FOCIL | `features/focil.yaml` | EIP-7805 | 4 | spamoor, assertoor, forkmon |
| 3 | BALs | `features/bal.yaml` | EIP-7928 | 2 | spamoor (500 tps), assertoor |
| 4 | Native AA | `features/native-aa.yaml` | EIP-7702 | 4 | spamoor (setcodetx), rakoon (fuzz), assertoor |
| 5 | Gas Repricing | `features/gas-repricing.yaml` | 18 EIPs | 2 | spamoor, assertoor |
| 6 | Blobs | `features/blobs.yaml` | EIP-4844/8070 | 4 | spamoor (blob-combined), blobscan, assertoor |
| 7 | Multidim Gas | `features/multidim-gas.yaml` | EIP-7706 | 4 | spamoor (eoatx + blob), prometheus, grafana |
| 8 | SSZ | `features/ssz.yaml` | EIP-6404/7807 | 2 | spamoor (eoatx + blob), assertoor |
| 9 | Native Rollups | `features/native-rollups.yaml` | EIP-8079 | 2 | spamoor, assertoor |
| 10 | PeerDAS | `features/peerdas.yaml` | EIP-7594 | 8 | spamoor (6 sidecars), blobscan, prometheus |
| 11 | Verkle | `features/verkle.yaml` | EIP-6800 | 2 | spamoor (300 tps), assertoor |
| 12 | 3SF/Quick Slots | `features/consensus-3sf.yaml` | — | 4 | forky, assertoor, prometheus (6s slots) |
| 13 | PQ Crypto | `features/pq-crypto.yaml` | — | 2 | spamoor, assertoor |
| 14 | Encrypted Mempool | `features/encrypted-mempool.yaml` | — | 4 | spamoor, assertoor |
| 15 | Shielded Transfers | `features/shielded-transfers.yaml` | — | 2 | spamoor, assertoor |

Each test runs: devnet start → wait for blocks → consensus check → assertoor check → feature-specific verification → cleanup.

## General Configs

| Config | Description |
|--------|-------------|
| `single-client` | 2 eth2030-geth + 2 Lighthouse, assertoor stability checks |
| `multi-client` | eth2030-geth + standard geth, Lighthouse + Prysm, cross-client consensus |
| `stress-test` | 4 nodes, spamoor (500 eoatx + 50 blob + 100 setcodetx), prometheus + grafana |
| `blob-test` | 4 nodes, heavy blob spam, blobscan, assertoor blob tests |
| `eip7702-test` | 2 nodes, rakoon EIP-7702 fuzzing |
| `full-feature` | 4 nodes, all 10 ethpandaops tools enabled |

## Scripts

| Script | Purpose |
|--------|---------|
| `scripts/run-feature-tests.sh [features...]` | Run per-feature devnet tests |
| `scripts/run-devnet.sh [config] [enclave]` | Build image and launch devnet |
| `scripts/check-consensus.sh [enclave]` | Verify EL nodes agree on head block |
| `scripts/check-assertoor.sh [enclave] [timeout]` | Poll assertoor until tests pass/fail |
| `scripts/test-precompiles.sh [enclave]` | Smoke-test custom precompiles via RPC |
| `scripts/cleanup.sh [enclave]` | Destroy the devnet enclave |

## ethpandaops Tools

| Tool | Description |
|------|-------------|
| **Assertoor** | Automated test runner — stability, block proposals, transactions, blobs, opcodes |
| **Spamoor** | Multi-scenario tx spammer — eoatx, blob-combined, setcodetx, uniswap-swaps |
| **Rakoon** | EIP-7702 fuzzer — SetCode auth fuzzing with configurable workers |
| **Dora** | Beacon chain explorer — slots, validators, attestations |
| **Blobscan** | Blob transaction explorer — EIP-4844 blob analysis |
| **Forky** | Fork choice visualizer — consensus stability monitoring |
| **Tracoor** | Trace aggregator — execution and beacon event debugging |
| **Forkmon** | EL fork monitor — detect execution-layer forks |
| **Prometheus** | Metrics collection and storage |
| **Grafana** | Visualization dashboards for Prometheus metrics |

## Tool Access

After launching a devnet, get service URLs:

```bash
kurtosis enclave inspect eth2030-devnet
kurtosis port print eth2030-devnet dora http
kurtosis port print eth2030-devnet grafana http
kurtosis port print eth2030-devnet assertoor http
kurtosis service logs eth2030-devnet el-1-geth-lighthouse
```

## Troubleshooting

**Kurtosis engine not running**: `kurtosis engine start`

**Docker image not found**: The `run-devnet.sh` script builds automatically. For manual: `cd pkg && docker build -t eth2030:local .`

**Enclave already exists**: `./scripts/cleanup.sh eth2030-devnet`

**Nodes not producing blocks**: Check logs: `kurtosis service logs eth2030-devnet el-1-geth-lighthouse`

**Port conflicts**: Kurtosis maps random host ports. Use `kurtosis port print` to find actual ports.
