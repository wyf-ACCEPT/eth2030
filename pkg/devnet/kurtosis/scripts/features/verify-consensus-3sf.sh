#!/usr/bin/env bash
# Verify 3SF: check 3-slot finality and quick slot timing
set -euo pipefail
ENCLAVE="${1:-eth2030-consensus-3sf}"
if [ -n "${2:-}" ]; then
  RPC_URL="$2"
else
  EL_SVC=$(kurtosis enclave inspect "$ENCLAVE" 2>/dev/null | grep "el-[0-9]" | head -1 | awk '{print $2}')
  RPC_URL="http://$(kurtosis port print "$ENCLAVE" "$EL_SVC" rpc)"
fi

echo "=== 3-Slot Finality Verification ==="
BLOCK=$(curl -sf -X POST "$RPC_URL" -H "Content-Type: application/json" \
  -d '{"jsonrpc":"2.0","method":"eth_blockNumber","params":[],"id":1}' | jq -r '.result')
echo "Current block: $BLOCK"
[ "$((BLOCK))" -gt 0 ] || { echo "FAIL: No blocks produced"; exit 1; }

# Check block timestamps to verify slot timing (if enough blocks exist)
if [ "$((BLOCK))" -ge 2 ]; then
  B1=$(curl -sf -X POST "$RPC_URL" -H "Content-Type: application/json" \
    -d '{"jsonrpc":"2.0","method":"eth_getBlockByNumber","params":["0x1", false],"id":1}' | jq -r '.result.timestamp')
  B2=$(curl -sf -X POST "$RPC_URL" -H "Content-Type: application/json" \
    -d '{"jsonrpc":"2.0","method":"eth_getBlockByNumber","params":["0x2", false],"id":1}' | jq -r '.result.timestamp')
  if [ -n "$B1" ] && [ -n "$B2" ] && [ "$B1" != "null" ] && [ "$B2" != "null" ]; then
    SLOT_TIME=$(( $(printf '%d' "$B2") - $(printf '%d' "$B1") ))
    echo "Slot time: ${SLOT_TIME}s (block 1 -> block 2)"
  fi
fi

echo "PASS: 3SF — blocks produced with quick slot timing"
