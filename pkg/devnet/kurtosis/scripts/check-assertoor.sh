#!/usr/bin/env bash
set -euo pipefail

ENCLAVE="${1:-eth2030-devnet}"
TIMEOUT="${2:-300}"  # 5 minutes default

ASSERTOOR_URL=$(kurtosis port print "$ENCLAVE" "assertoor" http 2>/dev/null)
if [ -z "$ASSERTOOR_URL" ]; then
    echo "Error: Assertoor not running in enclave $ENCLAVE"
    exit 1
fi

echo "=== Checking Assertoor test results ==="
echo "URL: $ASSERTOOR_URL"
echo "Timeout: ${TIMEOUT}s"
echo ""

ELAPSED=0
while [ "$ELAPSED" -lt "$TIMEOUT" ]; do
    RESULT=$(curl -sf "$ASSERTOOR_URL/api/v1/test_status" 2>/dev/null || echo '{}')

    TOTAL=$(echo "$RESULT" | jq -r '.total // 0')
    PASSED=$(echo "$RESULT" | jq -r '.passed // 0')
    FAILED=$(echo "$RESULT" | jq -r '.failed // 0')
    RUNNING=$(echo "$RESULT" | jq -r '.running // 0')

    echo "  [${ELAPSED}s] Total: $TOTAL, Passed: $PASSED, Failed: $FAILED, Running: $RUNNING"

    if [ "$FAILED" -gt 0 ]; then
        echo ""
        echo "ASSERTOOR FAILED: $FAILED test(s) failed"
        curl -sf "$ASSERTOOR_URL/api/v1/test_status" | jq '.tests[] | select(.status == "failed")' 2>/dev/null || true
        exit 1
    fi

    if [ "$TOTAL" -gt 0 ] && [ "$RUNNING" -eq 0 ]; then
        echo ""
        echo "ASSERTOOR PASSED: All $PASSED test(s) passed"
        exit 0
    fi

    sleep 10
    ELAPSED=$((ELAPSED + 10))
done

echo ""
echo "ASSERTOOR TIMEOUT: Tests did not complete within ${TIMEOUT}s"
exit 1
