#!/usr/bin/env bash
set -euo pipefail

ENCLAVE="${1:-eth2030-devnet}"
TOLERANCE="${2:-1}"

echo "=== Checking EL head block consensus ==="

# Parse EL service names and RPC ports from enclave inspect output.
INSPECT=$(kurtosis enclave inspect "$ENCLAVE" 2>/dev/null)

# Extract EL service names (e.g., el-1-geth-lighthouse).
EL_SERVICES=$(echo "$INSPECT" | grep "el-[0-9]" | awk '{print $2}')

if [ -z "$EL_SERVICES" ]; then
    echo "Error: No EL services found in enclave $ENCLAVE"
    exit 1
fi

declare -A BLOCKS
MAX_BLOCK=0
MIN_BLOCK=999999999

for svc in $EL_SERVICES; do
    # Extract the rpc port from inspect output (look for lines after the service name with "rpc:" but not "engine-rpc:").
    RPC_PORT=$(echo "$INSPECT" | awk "/$svc/,/^[a-f0-9]/" | grep -P '^\s+rpc:' | head -1 | grep -oP '127\.0\.0\.1:\d+' || true)
    if [ -z "$RPC_PORT" ]; then
        # Fallback to kurtosis port print.
        RPC_PORT=$(kurtosis port print "$ENCLAVE" "$svc" rpc 2>/dev/null || true)
    fi
    if [ -z "$RPC_PORT" ]; then
        echo "  $svc: could not resolve RPC port"
        continue
    fi
    RPC_URL="http://$RPC_PORT"
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
