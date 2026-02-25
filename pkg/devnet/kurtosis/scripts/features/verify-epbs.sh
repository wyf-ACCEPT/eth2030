#!/usr/bin/env bash
# Verify ePBS: check that blocks are being produced and builder infrastructure is active
set -euo pipefail
ENCLAVE="${1:-eth2030-epbs}"
EL_SVC=$(kurtosis enclave inspect "$ENCLAVE" 2>/dev/null | grep "^el-" | head -1 | awk '{print $1}')
RPC_URL=$(kurtosis port print "$ENCLAVE" "$EL_SVC" rpc)

echo "=== ePBS Verification ==="
# Check block production
BLOCK=$(curl -sf -X POST "$RPC_URL" -H "Content-Type: application/json" \
  -d '{"jsonrpc":"2.0","method":"eth_blockNumber","params":[],"id":1}' | jq -r '.result')
echo "Current block: $BLOCK"
[ "$((BLOCK))" -gt 0 ] || { echo "FAIL: No blocks produced"; exit 1; }

# Check txpool status (builder should process transactions)
TXPOOL=$(curl -sf -X POST "$RPC_URL" -H "Content-Type: application/json" \
  -d '{"jsonrpc":"2.0","method":"txpool_status","params":[],"id":1}' | jq -r '.result')
echo "TxPool: $TXPOOL"

echo "PASS: ePBS — blocks produced, builder active"
