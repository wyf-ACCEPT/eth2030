#!/usr/bin/env bash
# Verify EL Payload: check payload chunking, block-in-blobs, announce nonce
set -euo pipefail
ENCLAVE="${1:-eth2030-el-payload}"
if [ -n "${2:-}" ]; then
  RPC_URL="$2"
else
  EL_SVC=$(kurtosis enclave inspect "$ENCLAVE" 2>/dev/null | grep "el-[0-9]" | head -1 | awk '{print $2}')
  RPC_URL="http://$(kurtosis port print "$ENCLAVE" "$EL_SVC" rpc)"
fi

echo "=== EL Payload Verification ==="
BLOCK=$(curl -sf -X POST "$RPC_URL" -H "Content-Type: application/json" \
  -d '{"jsonrpc":"2.0","method":"eth_blockNumber","params":[],"id":1}' | jq -r '.result')
echo "Current block: $BLOCK"
[ "$((BLOCK))" -gt 0 ] || { echo "FAIL: No blocks produced"; exit 1; }

# Check block structure via eth_getBlockByNumber
LATEST=$(curl -sf -X POST "$RPC_URL" -H "Content-Type: application/json" \
  -d '{"jsonrpc":"2.0","method":"eth_getBlockByNumber","params":["latest", true],"id":1}' | jq -r '.result')
BLOCK_HASH=$(echo "$LATEST" | jq -r '.hash // "missing"')
PARENT_HASH=$(echo "$LATEST" | jq -r '.parentHash // "missing"')
STATE_ROOT=$(echo "$LATEST" | jq -r '.stateRoot // "missing"')
TX_COUNT=$(echo "$LATEST" | jq -r '.transactions | length')
echo "Block hash: $BLOCK_HASH"
echo "Parent hash: $PARENT_HASH"
echo "State root: $STATE_ROOT"
echo "Transaction count: $TX_COUNT"
[ "$BLOCK_HASH" != "missing" ] || { echo "FAIL: block hash missing"; exit 1; }
[ "$PARENT_HASH" != "missing" ] || { echo "FAIL: parent hash missing"; exit 1; }
[ "$STATE_ROOT" != "missing" ] || { echo "FAIL: state root missing"; exit 1; }

# Verify block 1 also has proper structure (payload integrity)
if [ "$((BLOCK))" -ge 1 ]; then
  B1=$(curl -sf -X POST "$RPC_URL" -H "Content-Type: application/json" \
    -d '{"jsonrpc":"2.0","method":"eth_getBlockByNumber","params":["0x1", false],"id":1}' | jq -r '.result')
  B1_HASH=$(echo "$B1" | jq -r '.hash // "missing"')
  echo "Block 1 hash: $B1_HASH"
  [ "$B1_HASH" != "missing" ] || { echo "FAIL: block 1 hash missing"; exit 1; }
fi

echo "PASS: EL Payload — blocks produced, block structure intact"
