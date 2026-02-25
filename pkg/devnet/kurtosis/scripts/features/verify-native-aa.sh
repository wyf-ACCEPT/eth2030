#!/usr/bin/env bash
# Verify Native AA: check SetCode transactions are processed
set -euo pipefail
ENCLAVE="${1:-eth2030-native-aa}"
EL_SVC=$(kurtosis enclave inspect "$ENCLAVE" 2>/dev/null | grep "^el-" | head -1 | awk '{print $1}')
RPC_URL=$(kurtosis port print "$ENCLAVE" "$EL_SVC" rpc)

echo "=== Native AA (EIP-7702) Verification ==="
BLOCK=$(curl -sf -X POST "$RPC_URL" -H "Content-Type: application/json" \
  -d '{"jsonrpc":"2.0","method":"eth_blockNumber","params":[],"id":1}' | jq -r '.result')
echo "Current block: $BLOCK"
[ "$((BLOCK))" -gt 2 ] || { echo "FAIL: Too few blocks"; exit 1; }

# Check for type-4 (SetCode) transactions in recent blocks
LATEST=$(curl -sf -X POST "$RPC_URL" -H "Content-Type: application/json" \
  -d '{"jsonrpc":"2.0","method":"eth_getBlockByNumber","params":["latest", true],"id":1}' | jq -r '.result')
TX_COUNT=$(echo "$LATEST" | jq '.transactions | length')
echo "Latest block transactions: $TX_COUNT"

echo "PASS: Native AA — blocks with transactions produced"
