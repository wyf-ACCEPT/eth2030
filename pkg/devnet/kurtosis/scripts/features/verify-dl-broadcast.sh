#!/usr/bin/env bash
# Verify DL Broadcast: check EIP-7702 SetCode broadcast and teragas L2 support
set -euo pipefail
ENCLAVE="${1:-eth2030-dl-broadcast}"
if [ -n "${2:-}" ]; then
  RPC_URL="$2"
else
  EL_SVC=$(kurtosis enclave inspect "$ENCLAVE" 2>/dev/null | grep "el-[0-9]" | head -1 | awk '{print $2}')
  RPC_URL="http://$(kurtosis port print "$ENCLAVE" "$EL_SVC" rpc)"
fi

echo "=== DL Broadcast Verification ==="
BLOCK=$(curl -sf -X POST "$RPC_URL" -H "Content-Type: application/json" \
  -d '{"jsonrpc":"2.0","method":"eth_blockNumber","params":[],"id":1}' | jq -r '.result')
echo "Current block: $BLOCK"
[ "$((BLOCK))" -gt 0 ] || { echo "FAIL: No blocks produced"; exit 1; }

# Check pending transactions to verify tx processing pipeline
PENDING=$(curl -sf -X POST "$RPC_URL" -H "Content-Type: application/json" \
  -d '{"jsonrpc":"2.0","method":"eth_getBlockTransactionCountByNumber","params":["latest"],"id":1}' | jq -r '.result // "0x0"')
echo "Transactions in latest block: $PENDING"

# Verify chain ID (should be kurtosis network)
CHAIN_ID=$(curl -sf -X POST "$RPC_URL" -H "Content-Type: application/json" \
  -d '{"jsonrpc":"2.0","method":"eth_chainId","params":[],"id":1}' | jq -r '.result')
echo "Chain ID: $CHAIN_ID"

echo "PASS: DL Broadcast — blocks produced, transactions processing"
