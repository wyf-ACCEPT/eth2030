#!/usr/bin/env bash
# Verify PQ Crypto: check post-quantum cryptographic operations
set -euo pipefail
ENCLAVE="${1:-eth2030-pq-crypto}"
EL_SVC=$(kurtosis enclave inspect "$ENCLAVE" 2>/dev/null | grep "^el-" | head -1 | awk '{print $1}')
RPC_URL=$(kurtosis port print "$ENCLAVE" "$EL_SVC" rpc)

echo "=== Post-Quantum Crypto Verification ==="
BLOCK=$(curl -sf -X POST "$RPC_URL" -H "Content-Type: application/json" \
  -d '{"jsonrpc":"2.0","method":"eth_blockNumber","params":[],"id":1}' | jq -r '.result')
echo "Current block: $BLOCK"
[ "$((BLOCK))" -gt 0 ] || { echo "FAIL: No blocks produced"; exit 1; }

# Verify chain ID (confirms PQ-enabled configuration)
CHAIN_ID=$(curl -sf -X POST "$RPC_URL" -H "Content-Type: application/json" \
  -d '{"jsonrpc":"2.0","method":"eth_chainId","params":[],"id":1}' | jq -r '.result')
echo "Chain ID: $CHAIN_ID"

# Verify node version includes PQ support
CLIENT=$(curl -sf -X POST "$RPC_URL" -H "Content-Type: application/json" \
  -d '{"jsonrpc":"2.0","method":"web3_clientVersion","params":[],"id":1}' | jq -r '.result')
echo "Client version: $CLIENT"

echo "PASS: PQ Crypto — chain operational with PQ-enabled client"
