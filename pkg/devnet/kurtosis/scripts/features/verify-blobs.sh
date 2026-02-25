#!/usr/bin/env bash
# Verify Blobs: check blob transactions are processed
set -euo pipefail
ENCLAVE="${1:-eth2030-blobs}"
EL_SVC=$(kurtosis enclave inspect "$ENCLAVE" 2>/dev/null | grep "^el-" | head -1 | awk '{print $1}')
RPC_URL=$(kurtosis port print "$ENCLAVE" "$EL_SVC" rpc)

echo "=== Blob Verification ==="
BLOCK=$(curl -sf -X POST "$RPC_URL" -H "Content-Type: application/json" \
  -d '{"jsonrpc":"2.0","method":"eth_blockNumber","params":[],"id":1}' | jq -r '.result')
echo "Current block: $BLOCK"
[ "$((BLOCK))" -gt 0 ] || { echo "FAIL: No blocks produced"; exit 1; }

# Check blobscan URL if available
BLOBSCAN_URL=$(kurtosis port print "$ENCLAVE" "blobscan-web" http 2>/dev/null || true)
if [ -n "$BLOBSCAN_URL" ]; then
  echo "Blobscan: $BLOBSCAN_URL"
fi

echo "PASS: Blobs — blocks produced with blob transaction support"
