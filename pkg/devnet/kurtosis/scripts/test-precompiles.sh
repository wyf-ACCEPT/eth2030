#!/usr/bin/env bash
set -euo pipefail

ENCLAVE="${1:-eth2030-devnet}"

# Get first EL RPC endpoint
EL_SVC=$(kurtosis enclave inspect "$ENCLAVE" 2>/dev/null | grep "^el-" | head -1 | awk '{print $1}')
RPC_URL=$(kurtosis port print "$ENCLAVE" "$EL_SVC" rpc 2>/dev/null)

echo "=== Testing ETH2030 Custom Precompiles ==="
echo "RPC: $RPC_URL"
echo ""

PASS=0
FAIL=0

# Test ecAdd (address 0x06) — Glamsterdam repriced
echo "Testing ecAdd (0x06)..."
RESULT=$(curl -sf -X POST "$RPC_URL" \
    -H "Content-Type: application/json" \
    -d '{"jsonrpc":"2.0","method":"eth_call","params":[{"to":"0x0000000000000000000000000000000000000006","data":"0x0000000000000000000000000000000000000000000000000000000000000001000000000000000000000000000000000000000000000000000000000000000200000000000000000000000000000000000000000000000000000000000000010000000000000000000000000000000000000000000000000000000000000002"},"latest"],"id":1}' \
    | jq -r '.result // .error.message')

if [[ "$RESULT" == 0x* ]] && [ ${#RESULT} -gt 10 ]; then
    echo "  PASS: ecAdd returned valid result"
    PASS=$((PASS + 1))
else
    echo "  FAIL: ecAdd returned: $RESULT"
    FAIL=$((FAIL + 1))
fi

# Test ecMul (address 0x07) — standard precompile
echo "Testing ecMul (0x07)..."
RESULT=$(curl -sf -X POST "$RPC_URL" \
    -H "Content-Type: application/json" \
    -d '{"jsonrpc":"2.0","method":"eth_call","params":[{"to":"0x0000000000000000000000000000000000000007","data":"0x000000000000000000000000000000000000000000000000000000000000000100000000000000000000000000000000000000000000000000000000000000020000000000000000000000000000000000000000000000000000000000000002"},"latest"],"id":1}' \
    | jq -r '.result // .error.message')

if [[ "$RESULT" == 0x* ]] && [ ${#RESULT} -gt 10 ]; then
    echo "  PASS: ecMul returned valid result"
    PASS=$((PASS + 1))
else
    echo "  FAIL: ecMul returned: $RESULT"
    FAIL=$((FAIL + 1))
fi

echo ""
echo "Results: $PASS passed, $FAIL failed"
[ "$FAIL" -eq 0 ] && exit 0 || exit 1
