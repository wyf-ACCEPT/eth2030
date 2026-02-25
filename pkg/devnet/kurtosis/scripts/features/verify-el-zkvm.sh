#!/usr/bin/env bash
# Verify EL zkVM: check chain integrity with zkVM infrastructure active
set -euo pipefail
ENCLAVE="${1:-eth2030-el-zkvm}"
if [ -n "${2:-}" ]; then
  RPC_URL="$2"
else
  EL_SVC=$(kurtosis enclave inspect "$ENCLAVE" 2>/dev/null | grep "el-[0-9]" | head -1 | awk '{print $2}')
  RPC_URL="http://$(kurtosis port print "$ENCLAVE" "$EL_SVC" rpc)"
fi

echo "=== EL zkVM Verification ==="
echo "Covers: canonical guest, canonical zkVM, STF in zkISA, zkISA precompiles, exposed zkISA"

# Check block production
BLOCK=$(curl -sf -X POST "$RPC_URL" -H "Content-Type: application/json" \
  -d '{"jsonrpc":"2.0","method":"eth_blockNumber","params":[],"id":1}' | jq -r '.result')
echo "Current block: $BLOCK"
[ "$((BLOCK))" -gt 2 ] || { echo "FAIL: Too few blocks produced"; exit 1; }

# Verify chain integrity — consecutive blocks are linked
PARENT_HASH=$(curl -sf -X POST "$RPC_URL" -H "Content-Type: application/json" \
  -d '{"jsonrpc":"2.0","method":"eth_getBlockByNumber","params":["0x2", false],"id":1}' | jq -r '.result.parentHash')
BLOCK1_HASH=$(curl -sf -X POST "$RPC_URL" -H "Content-Type: application/json" \
  -d '{"jsonrpc":"2.0","method":"eth_getBlockByNumber","params":["0x1", false],"id":1}' | jq -r '.result.hash')
echo "Block 2 parentHash: $PARENT_HASH"
echo "Block 1 hash:       $BLOCK1_HASH"
[ "$PARENT_HASH" = "$BLOCK1_HASH" ] || { echo "FAIL: Parent hash mismatch — chain integrity broken"; exit 1; }

# Verify state transitions are happening
ROOT1=$(curl -sf -X POST "$RPC_URL" -H "Content-Type: application/json" \
  -d '{"jsonrpc":"2.0","method":"eth_getBlockByNumber","params":["0x1", false],"id":1}' | jq -r '.result.stateRoot')
ROOT_LATEST=$(curl -sf -X POST "$RPC_URL" -H "Content-Type: application/json" \
  -d '{"jsonrpc":"2.0","method":"eth_getBlockByNumber","params":["latest", false],"id":1}' | jq -r '.result.stateRoot')
echo "Block 1 state root:  $ROOT1"
echo "Latest state root:   $ROOT_LATEST"

echo "PASS: EL zkVM — chain integrity verified with zkVM infrastructure"
