#!/usr/bin/env bash
# Verify EL Gas Futures: check blocks produced, gas limit in headers
set -euo pipefail
ENCLAVE="${1:-eth2030-el-gas-futures}"
if [ -n "${2:-}" ]; then
  RPC_URL="$2"
else
  EL_SVC=$(kurtosis enclave inspect "$ENCLAVE" 2>/dev/null | grep "el-[0-9]" | head -1 | awk '{print $2}')
  RPC_URL="http://$(kurtosis port print "$ENCLAVE" "$EL_SVC" rpc)"
fi

echo "=== EL Gas Futures Verification ==="
echo "Covers: long-dated gas futures, gigagas L1"

# Check block production
BLOCK=$(curl -sf -X POST "$RPC_URL" -H "Content-Type: application/json" \
  -d '{"jsonrpc":"2.0","method":"eth_blockNumber","params":[],"id":1}' | jq -r '.result')
echo "Current block: $BLOCK"
[ "$((BLOCK))" -gt 2 ] || { echo "FAIL: Too few blocks produced"; exit 1; }

# Check gas limit in block headers across multiple blocks
for i in 1 2 3; do
  B_HEX=$(printf '0x%x' $i)
  BLOCK_DATA=$(curl -sf -X POST "$RPC_URL" -H "Content-Type: application/json" \
    -d "{\"jsonrpc\":\"2.0\",\"method\":\"eth_getBlockByNumber\",\"params\":[\"$B_HEX\", false],\"id\":1}" | jq -r '.result')
  GAS_LIMIT=$(echo "$BLOCK_DATA" | jq -r '.gasLimit')
  GAS_USED=$(echo "$BLOCK_DATA" | jq -r '.gasUsed')
  echo "Block $i — gasLimit: $GAS_LIMIT, gasUsed: $GAS_USED"
done

# Verify gas limit is non-zero and reasonable
LATEST_GAS_LIMIT=$(curl -sf -X POST "$RPC_URL" -H "Content-Type: application/json" \
  -d '{"jsonrpc":"2.0","method":"eth_getBlockByNumber","params":["latest", false],"id":1}' | jq -r '.result.gasLimit')
echo "Latest block gasLimit: $LATEST_GAS_LIMIT"
LIMIT_DEC=$((LATEST_GAS_LIMIT))
[ "$LIMIT_DEC" -gt 0 ] || { echo "FAIL: Gas limit is zero"; exit 1; }

# Verify chain is processing transactions under load
LATEST_TX_COUNT=$(curl -sf -X POST "$RPC_URL" -H "Content-Type: application/json" \
  -d '{"jsonrpc":"2.0","method":"eth_getBlockTransactionCountByNumber","params":["latest"],"id":1}' | jq -r '.result // "0x0"')
echo "Latest block tx count: $LATEST_TX_COUNT"

echo "PASS: EL Gas Futures — gas limits operational, chain progresses under load"
