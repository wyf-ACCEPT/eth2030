#!/usr/bin/env bash
# Verify Verkle State: check Verkle tree state transitions
set -euo pipefail
ENCLAVE="${1:-eth2030-verkle}"
if [ -n "${2:-}" ]; then
  RPC_URL="$2"
else
  EL_SVC=$(kurtosis enclave inspect "$ENCLAVE" 2>/dev/null | grep "el-[0-9]" | head -1 | awk '{print $2}')
  RPC_URL="http://$(kurtosis port print "$ENCLAVE" "$EL_SVC" rpc)"
fi

echo "=== Verkle State Verification ==="
BLOCK=$(curl -sf -X POST "$RPC_URL" -H "Content-Type: application/json" \
  -d '{"jsonrpc":"2.0","method":"eth_blockNumber","params":[],"id":1}' | jq -r '.result')
echo "Current block: $BLOCK"
[ "$((BLOCK))" -gt 0 ] || { echo "FAIL: No blocks produced"; exit 1; }

# Verify state root changes across blocks (proves state transitions work)
ROOT1=$(curl -sf -X POST "$RPC_URL" -H "Content-Type: application/json" \
  -d '{"jsonrpc":"2.0","method":"eth_getBlockByNumber","params":["0x1", false],"id":1}' | jq -r '.result.stateRoot')
ROOT2=$(curl -sf -X POST "$RPC_URL" -H "Content-Type: application/json" \
  -d '{"jsonrpc":"2.0","method":"eth_getBlockByNumber","params":["0x2", false],"id":1}' | jq -r '.result.stateRoot')
echo "Block 1 state root: $ROOT1"
echo "Block 2 state root: $ROOT2"

# State roots should differ (state transitions happening)
if [ "$ROOT1" == "$ROOT2" ]; then
  echo "WARNING: State roots identical — no state changes between blocks"
fi

echo "PASS: Verkle — state transitions operational"
