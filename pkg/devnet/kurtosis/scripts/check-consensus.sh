#!/usr/bin/env bash
set -euo pipefail

ENCLAVE="${1:-eth2030-devnet}"
TOLERANCE="${2:-1}"

echo "=== Checking EL head block consensus ==="

# Get all EL services
EL_SERVICES=$(kurtosis enclave inspect "$ENCLAVE" 2>/dev/null | grep "^el-" | awk '{print $1}')

if [ -z "$EL_SERVICES" ]; then
    echo "Error: No EL services found in enclave $ENCLAVE"
    exit 1
fi

declare -A BLOCKS
MAX_BLOCK=0
MIN_BLOCK=999999999

for svc in $EL_SERVICES; do
    RPC_URL=$(kurtosis port print "$ENCLAVE" "$svc" rpc 2>/dev/null)
    BLOCK_HEX=$(curl -sf -X POST "$RPC_URL" \
        -H "Content-Type: application/json" \
        -d '{"jsonrpc":"2.0","method":"eth_blockNumber","params":[],"id":1}' \
        | jq -r '.result // "0x0"')
    BLOCK=$((BLOCK_HEX))
    BLOCKS[$svc]=$BLOCK
    echo "  $svc: block $BLOCK"
    [ "$BLOCK" -gt "$MAX_BLOCK" ] && MAX_BLOCK=$BLOCK
    [ "$BLOCK" -lt "$MIN_BLOCK" ] && MIN_BLOCK=$BLOCK
done

DIFF=$((MAX_BLOCK - MIN_BLOCK))
echo ""
if [ "$DIFF" -le "$TOLERANCE" ]; then
    echo "CONSENSUS OK (block spread: $DIFF, tolerance: $TOLERANCE)"
    exit 0
else
    echo "FORK DETECTED (block spread: $DIFF, tolerance: $TOLERANCE)"
    exit 1
fi
