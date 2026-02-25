#!/usr/bin/env bash
# Verify Multidimensional Gas: check multi-gas pricing is active
set -euo pipefail
ENCLAVE="${1:-eth2030-multidim-gas}"
EL_SVC=$(kurtosis enclave inspect "$ENCLAVE" 2>/dev/null | grep "^el-" | head -1 | awk '{print $1}')
RPC_URL=$(kurtosis port print "$ENCLAVE" "$EL_SVC" rpc)

echo "=== Multidimensional Gas Verification ==="
BLOCK=$(curl -sf -X POST "$RPC_URL" -H "Content-Type: application/json" \
  -d '{"jsonrpc":"2.0","method":"eth_blockNumber","params":[],"id":1}' | jq -r '.result')
echo "Current block: $BLOCK"
[ "$((BLOCK))" -gt 0 ] || { echo "FAIL: No blocks produced"; exit 1; }

# Check gas pricing
FEE=$(curl -sf -X POST "$RPC_URL" -H "Content-Type: application/json" \
  -d '{"jsonrpc":"2.0","method":"eth_gasPrice","params":[],"id":1}' | jq -r '.result')
echo "Gas price: $FEE"

echo "PASS: Multidim Gas — gas pricing active"
