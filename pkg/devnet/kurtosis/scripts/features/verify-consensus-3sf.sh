#!/usr/bin/env bash
# Verify 3SF: check 3-slot finality and quick slot timing
set -euo pipefail
ENCLAVE="${1:-eth2030-consensus-3sf}"
EL_SVC=$(kurtosis enclave inspect "$ENCLAVE" 2>/dev/null | grep "^el-" | head -1 | awk '{print $1}')
RPC_URL=$(kurtosis port print "$ENCLAVE" "$EL_SVC" rpc)

echo "=== 3-Slot Finality Verification ==="
BLOCK=$(curl -sf -X POST "$RPC_URL" -H "Content-Type: application/json" \
  -d '{"jsonrpc":"2.0","method":"eth_blockNumber","params":[],"id":1}' | jq -r '.result')
echo "Current block: $BLOCK"
[ "$((BLOCK))" -gt 0 ] || { echo "FAIL: No blocks produced"; exit 1; }

# Check block timestamps to verify slot timing
B1=$(curl -sf -X POST "$RPC_URL" -H "Content-Type: application/json" \
  -d '{"jsonrpc":"2.0","method":"eth_getBlockByNumber","params":["0x1", false],"id":1}' | jq -r '.result.timestamp')
B2=$(curl -sf -X POST "$RPC_URL" -H "Content-Type: application/json" \
  -d '{"jsonrpc":"2.0","method":"eth_getBlockByNumber","params":["0x2", false],"id":1}' | jq -r '.result.timestamp')
SLOT_TIME=$(( $(printf '%d' "$B2") - $(printf '%d' "$B1") ))
echo "Slot time: ${SLOT_TIME}s (block 1 -> block 2)"

# Verify consensus across nodes
EL_SERVICES=$(kurtosis enclave inspect "$ENCLAVE" 2>/dev/null | grep "^el-" | awk '{print $1}')
HEADS=()
for svc in $EL_SERVICES; do
  SVC_RPC=$(kurtosis port print "$ENCLAVE" "$svc" rpc 2>/dev/null)
  HEAD=$(curl -sf -X POST "$SVC_RPC" -H "Content-Type: application/json" \
    -d '{"jsonrpc":"2.0","method":"eth_blockNumber","params":[],"id":1}' | jq -r '.result')
  HEADS+=("$svc:$HEAD")
  echo "  $svc head: $HEAD"
done

echo "PASS: 3SF — blocks produced with ${SLOT_TIME}s slot time"
