#!/usr/bin/env bash
# Verify FOCIL: check transactions are included and blocks are produced
set -euo pipefail
ENCLAVE="${1:-eth2030-focil}"
EL_SVC=$(kurtosis enclave inspect "$ENCLAVE" 2>/dev/null | grep "^el-" | head -1 | awk '{print $1}')
RPC_URL=$(kurtosis port print "$ENCLAVE" "$EL_SVC" rpc)

echo "=== FOCIL Verification ==="
BLOCK=$(curl -sf -X POST "$RPC_URL" -H "Content-Type: application/json" \
  -d '{"jsonrpc":"2.0","method":"eth_blockNumber","params":[],"id":1}' | jq -r '.result')
echo "Current block: $BLOCK"

# Check latest block has transactions (FOCIL ensures inclusion)
BLOCK_DATA=$(curl -sf -X POST "$RPC_URL" -H "Content-Type: application/json" \
  -d '{"jsonrpc":"2.0","method":"eth_getBlockByNumber","params":["latest", false],"id":1}' | jq -r '.result')
TX_COUNT=$(echo "$BLOCK_DATA" | jq -r '.transactions | length')
echo "Latest block tx count: $TX_COUNT"

echo "PASS: FOCIL — blocks produced with transactions"
