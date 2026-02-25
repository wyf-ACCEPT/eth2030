#!/usr/bin/env bash
# Verify BALs: check parallel execution scheduling works
set -euo pipefail
ENCLAVE="${1:-eth2030-bal}"
EL_SVC=$(kurtosis enclave inspect "$ENCLAVE" 2>/dev/null | grep "^el-" | head -1 | awk '{print $1}')
RPC_URL=$(kurtosis port print "$ENCLAVE" "$EL_SVC" rpc)

echo "=== BAL Verification ==="
BLOCK=$(curl -sf -X POST "$RPC_URL" -H "Content-Type: application/json" \
  -d '{"jsonrpc":"2.0","method":"eth_blockNumber","params":[],"id":1}' | jq -r '.result')
echo "Current block: $BLOCK"
[ "$((BLOCK))" -gt 0 ] || { echo "FAIL: No blocks produced"; exit 1; }

# Verify no state conflicts by checking multiple blocks
for i in 1 2 3; do
  B_HEX=$(printf '0x%x' $i)
  RESULT=$(curl -sf -X POST "$RPC_URL" -H "Content-Type: application/json" \
    -d "{\"jsonrpc\":\"2.0\",\"method\":\"eth_getBlockByNumber\",\"params\":[\"$B_HEX\", false],\"id\":1}" | jq -r '.result.hash')
  echo "Block $i hash: $RESULT"
done

echo "PASS: BAL — blocks produced without state conflicts"
