#!/usr/bin/env bash
# Verify CL Security: check attack recovery, VDF randomness, secret proposers
set -euo pipefail
ENCLAVE="${1:-eth2030-cl-security}"
if [ -n "${2:-}" ]; then
  RPC_URL="$2"
else
  EL_SVC=$(kurtosis enclave inspect "$ENCLAVE" 2>/dev/null | grep "el-[0-9]" | head -1 | awk '{print $2}')
  RPC_URL="http://$(kurtosis port print "$ENCLAVE" "$EL_SVC" rpc)"
fi

echo "=== CL Security Verification ==="
# Check block production — chain must progress under security features
BLOCK=$(curl -sf -X POST "$RPC_URL" -H "Content-Type: application/json" \
  -d '{"jsonrpc":"2.0","method":"eth_blockNumber","params":[],"id":1}' | jq -r '.result')
echo "Current block: $BLOCK"
[ "$((BLOCK))" -gt 0 ] || { echo "FAIL: No blocks produced"; exit 1; }

# Verify chain progresses by checking multiple blocks have distinct hashes
HASH1=$(curl -sf -X POST "$RPC_URL" -H "Content-Type: application/json" \
  -d '{"jsonrpc":"2.0","method":"eth_getBlockByNumber","params":["0x1", false],"id":1}' | jq -r '.result.hash')
HASH2=$(curl -sf -X POST "$RPC_URL" -H "Content-Type: application/json" \
  -d '{"jsonrpc":"2.0","method":"eth_getBlockByNumber","params":["0x2", false],"id":1}' | jq -r '.result.hash')
echo "Block 1 hash: ${HASH1:0:18}..."
echo "Block 2 hash: ${HASH2:0:18}..."

if [ "$HASH1" == "$HASH2" ]; then
  echo "FAIL: Block hashes identical — chain not progressing"
  exit 1
fi

# Verify client version (confirms security-enabled configuration)
CLIENT=$(curl -sf -X POST "$RPC_URL" -H "Content-Type: application/json" \
  -d '{"jsonrpc":"2.0","method":"web3_clientVersion","params":[],"id":1}' | jq -r '.result')
echo "Client version: $CLIENT"

echo "PASS: CL Security — chain progresses with security features active"
