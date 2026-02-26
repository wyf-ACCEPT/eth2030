# Uniswap Dataset Benchmark

Benchmark EVM execution performance using a real-world Uniswap V2 swap workload.

## Dataset

`data/uniswap-pf-t1k-c0.json.zip` — 1,000 Uniswap V2 `swapExactTokensForTokens` transactions across 4,005 pre-state accounts (pair contracts, token contracts, routers). The raw JSON is ~39 MB; the zip is ~0.9 MB (98% compression due to repeated contract bytecode).

| Property | Value |
|----------|-------|
| Transactions | 1,000 |
| Pre-state accounts | 4,005 |
| Fork | Cancun |
| Contention level | 0 (independent senders) |
| Block gas limit | 100,000,000 |
| Tx gas limit (each) | 1,000,000 |
| Source format | Altius `pf` (prefilled state test) |

## Quick Start

### 1. Extract the dataset

The ETH2030 benchmark auto-extracts on first run. To extract manually:

```bash
cd data
unzip uniswap-pf-t1k-c0.json.zip
```

### 2. Run the ETH2030 benchmark

This executes all 1,000 transactions via `gethcore.ApplyMessage()` (go-ethereum v1.17.0 as library) and reports TPS / Mgas/s:

```bash
cd pkg
go test -v -run TestDatasetBenchmark -timeout 5m ./core/eftest/
```

For Go's built-in benchmark framework (multiple iterations, averaged):

```bash
cd pkg
go test -bench=BenchmarkDatasetExecution -benchtime=5x -timeout 10m ./core/eftest/
```

For a custom dataset file:

```bash
cd pkg
DATASET_PATH=/path/to/other-dataset.json go test -v -run TestDatasetBenchmarkCustomPath ./core/eftest/
```

### 3. Run the original geth `evm t8n` benchmark

This uses the upstream go-ethereum `evm` CLI tool for comparison.

**Build the `evm` binary** (requires the go-ethereum submodule):

```bash
git submodule update --init refs/go-ethereum
cd refs/go-ethereum
go build -o /tmp/geth-evm ./cmd/evm/
```

**Convert the dataset** to `evm t8n` format (alloc.json + env.json + txs.json):

```bash
python3 tools/convert_dataset.py data/uniswap-pf-t1k-c0.json /tmp/t8n-input
```

**Run the transition:**

```bash
mkdir -p /tmp/t8n-output
time /tmp/geth-evm t8n \
  --input.alloc=/tmp/t8n-input/alloc.json \
  --input.env=/tmp/t8n-input/env.json \
  --input.txs=/tmp/t8n-input/txs.json \
  --state.fork=Cancun \
  --output.basedir=/tmp/t8n-output \
  --output.alloc=alloc.json \
  --output.result=result.json
```

> **Note:** The default block gas limit (100M) only fits ~870 txs. To process all 1,000, edit `/tmp/t8n-input/env.json` and set `"currentGasLimit": "0xbebc200"` (200M).

Inspect results:

```bash
python3 -c "
import json
with open('/tmp/t8n-output/result.json') as f:
    r = json.load(f)
receipts = r.get('receipts', [])
total_gas = sum(int(rx.get('gasUsed', '0x0'), 16) for rx in receipts)
print(f'Processed: {len(receipts)}, Gas: {total_gas}')
"
```

## Results

Measured on Apple M4 Max, macOS 15.3.2, Go 1.26.0. Dataset: `uniswap-pf-t1k-c0.json` (1,000 txs).

### ETH2030 (go-ethereum v1.17.0 as library, pure EVM execution)

| Run | Txs | Gas Used | Duration | TPS | Mgas/s | Ggas/s |
|-----|-----|----------|----------|-----|--------|--------|
| 1 | 1,000 | 115,239,204 | 108.1 ms | 9,254 | 1,066 | 1.07 |
| 2 | 1,000 | 115,239,204 | 108.4 ms | 9,227 | 1,063 | 1.06 |
| 3 | 1,000 | 115,239,204 | 112.4 ms | 8,899 | 1,026 | 1.03 |
| **Avg** | **1,000** | **115,239,204** | **109.6 ms** | **9,127** | **1,052** | **1.05** |

### Original geth `evm t8n` (end-to-end: JSON I/O + ECDSA signing + EVM + state dump)

| Run | Txs | Gas Used | Wall Time | User Time |
|-----|-----|----------|-----------|-----------|
| 1 | 1,000 | 141,081,208 | 1.20 s | 1.27 s |
| 2 | 1,000 | 141,081,208 | 1.12 s | 1.26 s |
| 3 | 1,000 | 141,081,208 | 1.23 s | 1.30 s |
| **Avg** | **1,000** | **141,081,208** | **1.18 s** | **1.28 s** |

Derived from avg wall time: **~847 TPS, ~120 Mgas/s**.

### What the numbers mean

| | ETH2030 | geth `evm t8n` |
|---|---------|----------------|
| **Measures** | Pure EVM execution only | Full pipeline: JSON parsing, ECDSA signing, EVM execution, state trie dump, JSON output |
| **ECDSA** | Skipped (sender set directly) | Full sign per tx from `secretKey` |
| **State I/O** | In-memory trie | In-memory trie + dump to JSON (~110 ms) |
| **File I/O** | None (Go test harness) | Read ~40 MB input + write output |
| **go-ethereum version** | v1.17.0 (library) | master (~v1.17-dev) |

Both use the same go-ethereum EVM core (`gethcore.ApplyMessage`). The ~10x wall-time difference is primarily from ECDSA operations and file I/O, not from EVM execution itself.

The gas total differs (115M vs 141M) because the two go-ethereum versions have slightly different gas schedules.

## File Layout

```
data/
  uniswap-pf-t1k-c0.json.zip   # Compressed dataset (tracked in git)
  uniswap-pf-t1k-c0.json       # Extracted at runtime (gitignored)

pkg/core/eftest/
  dataset_bench.go              # Loader + executor (auto-extracts zip)
  dataset_bench_test.go         # Test and benchmark entry points

tools/
  convert_dataset.py            # Convert to geth evm t8n format
```
