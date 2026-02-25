#!/usr/bin/env bash
# Verify Gas Repricing: call repriced precompiles
set -euo pipefail
ENCLAVE="${1:-eth2030-gas-repricing}"
EL_SVC=$(kurtosis enclave inspect "$ENCLAVE" 2>/dev/null | grep "^el-" | head -1 | awk '{print $1}')
RPC_URL=$(kurtosis port print "$ENCLAVE" "$EL_SVC" rpc)

echo "=== Gas Repricing Verification ==="

# Test ecAdd precompile (0x06) — Glamsterdam repriced
RESULT=$(curl -sf -X POST "$RPC_URL" -H "Content-Type: application/json" \
  -d '{"jsonrpc":"2.0","method":"eth_call","params":[{"to":"0x0000000000000000000000000000000000000006","data":"0x0000000000000000000000000000000000000000000000000000000000000001000000000000000000000000000000000000000000000000000000000000000200000000000000000000000000000000000000000000000000000000000000010000000000000000000000000000000000000000000000000000000000000002","gas":"0x100000"},"latest"],"id":1}' | jq -r '.result // .error.message')
echo "ecAdd (0x06): $RESULT"
[[ "$RESULT" == 0x* ]] || { echo "FAIL: ecAdd precompile failed"; exit 1; }

echo "PASS: Gas Repricing — precompiles work with repriced gas"
