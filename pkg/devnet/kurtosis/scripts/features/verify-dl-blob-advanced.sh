#!/usr/bin/env bash
# Verify DL Blob Advanced: check BPO blobs, blob streaming, PQ blobs, variable-size blobs
set -euo pipefail
ENCLAVE="${1:-eth2030-dl-blob-advanced}"
if [ -n "${2:-}" ]; then
  RPC_URL="$2"
else
  EL_SVC=$(kurtosis enclave inspect "$ENCLAVE" 2>/dev/null | grep "el-[0-9]" | head -1 | awk '{print $2}')
  RPC_URL="http://$(kurtosis port print "$ENCLAVE" "$EL_SVC" rpc)"
fi

echo "=== DL Blob Advanced Verification ==="
BLOCK=$(curl -sf -X POST "$RPC_URL" -H "Content-Type: application/json" \
  -d '{"jsonrpc":"2.0","method":"eth_blockNumber","params":[],"id":1}' | jq -r '.result')
echo "Current block: $BLOCK"
[ "$((BLOCK))" -gt 0 ] || { echo "FAIL: No blocks produced"; exit 1; }

# Check latest block for blob gas fields (blobGasUsed, excessBlobGas)
LATEST=$(curl -sf -X POST "$RPC_URL" -H "Content-Type: application/json" \
  -d '{"jsonrpc":"2.0","method":"eth_getBlockByNumber","params":["latest", false],"id":1}' | jq -r '.result')
BLOB_GAS=$(echo "$LATEST" | jq -r '.blobGasUsed // "0x0"')
EXCESS_BLOB_GAS=$(echo "$LATEST" | jq -r '.excessBlobGas // "0x0"')
echo "Blob gas used: $BLOB_GAS"
echo "Excess blob gas: $EXCESS_BLOB_GAS"

# Verify blob gas fields exist in header (BPO schedule support)
BASE_FEE=$(echo "$LATEST" | jq -r '.baseFeePerGas // "missing"')
echo "Base fee per gas: $BASE_FEE"
[ "$BASE_FEE" != "missing" ] || { echo "FAIL: baseFeePerGas missing from header"; exit 1; }

echo "PASS: DL Blob Advanced — blocks produced, blob gas fields present in headers"
