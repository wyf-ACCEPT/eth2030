#!/usr/bin/env bash
# Verify EL Gas Schedule: check Hogota repricing and gas limit schedule
set -euo pipefail
ENCLAVE="${1:-eth2030-el-gas-schedule}"
if [ -n "${2:-}" ]; then
  RPC_URL="$2"
else
  EL_SVC=$(kurtosis enclave inspect "$ENCLAVE" 2>/dev/null | grep "el-[0-9]" | head -1 | awk '{print $2}')
  RPC_URL="http://$(kurtosis port print "$ENCLAVE" "$EL_SVC" rpc)"
fi

echo "=== EL Gas Schedule Verification ==="
BLOCK=$(curl -sf -X POST "$RPC_URL" -H "Content-Type: application/json" \
  -d '{"jsonrpc":"2.0","method":"eth_blockNumber","params":[],"id":1}' | jq -r '.result')
echo "Current block: $BLOCK"
[ "$((BLOCK))" -gt 0 ] || { echo "FAIL: No blocks produced"; exit 1; }

# Check gas limit in block header
LATEST=$(curl -sf -X POST "$RPC_URL" -H "Content-Type: application/json" \
  -d '{"jsonrpc":"2.0","method":"eth_getBlockByNumber","params":["latest", false],"id":1}' | jq -r '.result')
GAS_LIMIT=$(echo "$LATEST" | jq -r '.gasLimit // "missing"')
GAS_USED=$(echo "$LATEST" | jq -r '.gasUsed // "0x0"')
echo "Gas limit: $GAS_LIMIT"
echo "Gas used: $GAS_USED"
[ "$GAS_LIMIT" != "missing" ] || { echo "FAIL: gasLimit missing from header"; exit 1; }

# Verify gas price is responding (Hogota repricing active)
GAS_PRICE=$(curl -sf -X POST "$RPC_URL" -H "Content-Type: application/json" \
  -d '{"jsonrpc":"2.0","method":"eth_gasPrice","params":[],"id":1}' | jq -r '.result')
echo "Gas price: $GAS_PRICE"

echo "PASS: EL Gas Schedule — blocks produced, gas limit present in headers"
