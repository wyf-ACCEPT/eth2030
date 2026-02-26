# Uniswap 数据集性能基准测试

使用真实 Uniswap V2 Swap 交易负载，测量 EVM 执行性能。

## 数据集

`data/uniswap-pf-t1k-c0.json.zip` — 包含 1,000 笔 Uniswap V2 `swapExactTokensForTokens` 交易，涉及 4,005 个预置账户（交易对合约、代币合约、Router 合约等）。原始 JSON 约 39 MB，由于合约字节码大量重复，压缩后仅 ~0.9 MB（压缩率 98%）。

| 属性 | 值 |
|------|-----|
| 交易数 | 1,000 |
| 预置账户数 | 4,005 |
| 硬分叉 | Cancun |
| 冲突等级 | 0（每笔交易独立发送者，无状态冲突） |
| 区块 Gas 上限 | 100,000,000 |
| 单笔 Tx Gas 上限 | 1,000,000 |
| 数据格式 | Altius `pf`（prefilled state test） |

## 快速开始

### 1. 解压数据集

ETH2030 的测试在首次运行时会自动解压 zip。也可以手动解压：

```bash
cd data
unzip uniswap-pf-t1k-c0.json.zip
```

### 2. 运行 ETH2030 基准测试

通过 `gethcore.ApplyMessage()`（内嵌 go-ethereum v1.17.0 作为库）执行全部 1,000 笔交易，输出 TPS / Mgas/s：

```bash
cd pkg
go test -v -run TestDatasetBenchmark -timeout 5m ./core/eftest/
```

使用 Go 内置 benchmark 框架（多次迭代取平均）：

```bash
cd pkg
go test -bench=BenchmarkDatasetExecution -benchtime=5x -timeout 10m ./core/eftest/
```

指定自定义数据集文件：

```bash
cd pkg
DATASET_PATH=/path/to/other-dataset.json go test -v -run TestDatasetBenchmarkCustomPath ./core/eftest/
```

### 3. 运行原版 geth `evm t8n` 基准测试

使用上游 go-ethereum 的 `evm` CLI 工具作为对照。

**编译 `evm` 二进制文件**（需要先拉取 go-ethereum 子模块）：

```bash
git submodule update --init refs/go-ethereum
cd refs/go-ethereum
go build -o /tmp/geth-evm ./cmd/evm/
```

**转换数据集**为 `evm t8n` 格式（alloc.json + env.json + txs.json）：

```bash
python3 tools/convert_dataset.py data/uniswap-pf-t1k-c0.json /tmp/t8n-input
```

**执行状态转换：**

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

> **注意：** 默认区块 Gas 上限（100M）只够容纳约 870 笔交易。若要执行全部 1,000 笔，需编辑 `/tmp/t8n-input/env.json`，将 `"currentGasLimit"` 改为 `"0xbebc200"`（200M）。

查看结果：

```bash
python3 -c "
import json
with open('/tmp/t8n-output/result.json') as f:
    r = json.load(f)
receipts = r.get('receipts', [])
total_gas = sum(int(rx.get('gasUsed', '0x0'), 16) for rx in receipts)
print(f'已处理: {len(receipts)} 笔, 总 Gas: {total_gas}')
"
```

## 测试结果

测试环境：Apple M4 Max, macOS 15.3.2, Go 1.26.0。数据集：`uniswap-pf-t1k-c0.json`（1,000 笔交易）。

### ETH2030（内嵌 go-ethereum v1.17.0，纯 EVM 执行）

| 轮次 | 交易数 | Gas 消耗 | 耗时 | TPS | Mgas/s | Ggas/s |
|------|-------|----------|------|-----|--------|--------|
| 1 | 1,000 | 115,239,204 | 108.1 ms | 9,254 | 1,066 | 1.07 |
| 2 | 1,000 | 115,239,204 | 108.4 ms | 9,227 | 1,063 | 1.06 |
| 3 | 1,000 | 115,239,204 | 112.4 ms | 8,899 | 1,026 | 1.03 |
| **平均** | **1,000** | **115,239,204** | **109.6 ms** | **9,127** | **1,052** | **1.05** |

### 原版 geth `evm t8n`（端到端：JSON 读写 + ECDSA 签名 + EVM 执行 + State Trie 导出）

| 轮次 | 交易数 | Gas 消耗 | Wall Time | User Time |
|------|-------|----------|-----------|-----------|
| 1 | 1,000 | 141,081,208 | 1.20 s | 1.27 s |
| 2 | 1,000 | 141,081,208 | 1.12 s | 1.26 s |
| 3 | 1,000 | 141,081,208 | 1.23 s | 1.30 s |
| **平均** | **1,000** | **141,081,208** | **1.18 s** | **1.28 s** |

由平均 Wall Time 推算：**~847 TPS，~120 Mgas/s**。

> **Wall Time** 是程序从启动到退出的真实经过时间（挂钟时间）。**User Time** 是 CPU 在用户态实际执行的时间总和；由于多核并行，User Time 可能大于 Wall Time。基准测试应以 Wall Time 为准。

### 数据解读

| | ETH2030 | geth `evm t8n` |
|---|---------|----------------|
| **测量范围** | 仅 EVM 执行 | 完整流程：JSON 解析 → ECDSA 签名 → EVM 执行 → State Trie 导出 → JSON 输出 |
| **ECDSA 签名** | 跳过（直接设置发送者地址） | 每笔交易用 `secretKey` 完整签名 |
| **状态存储** | 内存 Trie | 内存 Trie + 导出为 JSON（~110 ms） |
| **文件 I/O** | 无（Go test 内部执行） | 读取 ~40 MB 输入 + 写入输出文件 |
| **go-ethereum 版本** | v1.17.0（作为库引入） | master（~v1.17-dev） |

两者使用**完全相同**的 go-ethereum EVM 内核（`gethcore.ApplyMessage`）。约 10 倍的 Wall Time 差异主要来自 ECDSA 签名运算和文件 I/O，而非 EVM 执行本身。

Gas 总量不同（115M vs 141M）是因为两个 go-ethereum 版本的 gas 定价存在细微差异。

## 文件结构

```
data/
  uniswap-pf-t1k-c0.json.zip   # 压缩数据集（纳入 git 跟踪）
  uniswap-pf-t1k-c0.json       # 运行时自动解压（已 gitignore）

pkg/core/eftest/
  dataset_bench.go              # 数据集加载器 + 执行器（自动解压 zip）
  dataset_bench_test.go         # 测试和 benchmark 入口

tools/
  convert_dataset.py            # 转换为 geth evm t8n 格式
```
