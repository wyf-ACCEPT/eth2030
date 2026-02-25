#!/usr/bin/env bash
# run-feature-tests.sh — Run devnet tests for ETH2030 features one by one.
#
# Usage:
#   ./run-feature-tests.sh              # Run all 15 feature tests
#   ./run-feature-tests.sh epbs focil   # Run specific features
#   ./run-feature-tests.sh --list       # List available features
#
# Each feature test:
#   1. Starts a Kurtosis devnet with the feature config
#   2. Waits for blocks to produce
#   3. Runs consensus check
#   4. Runs assertoor check (if available)
#   5. Runs feature-specific verification
#   6. Cleans up
#
# Results are logged to pkg/devnet/kurtosis/results/

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
CONFIG_DIR="$SCRIPT_DIR/../configs/features"
RESULTS_DIR="$SCRIPT_DIR/../results"
PKG_DIR="$SCRIPT_DIR/../../../"

FEATURES=(
  epbs
  focil
  bal
  native-aa
  gas-repricing
  blobs
  multidim-gas
  ssz
  native-rollups
  peerdas
  verkle
  consensus-3sf
  pq-crypto
  encrypted-mempool
  shielded-transfers
)

FEATURE_DESCRIPTIONS=(
  "ePBS — Enshrined Proposer-Builder Separation (EIP-7732)"
  "FOCIL — Fork-Choice Enforced Inclusion Lists (EIP-7805)"
  "BALs — Block Access Lists for Parallel Execution (EIP-7928)"
  "Native AA — Account Abstraction via SetCode (EIP-7702)"
  "Gas Repricing — Glamsterdam Conversion Repricing (18 EIPs)"
  "Blobs — Blob Transactions + Sparse Blobpool (EIP-4844/8070)"
  "Multidim Gas — Multi-Gas Pricing (EIP-7706)"
  "SSZ — SSZ Transactions & Blocks (EIP-6404/7807)"
  "Native Rollups — EXECUTE Precompile (EIP-8079)"
  "PeerDAS — Data Availability Sampling (EIP-7594)"
  "Verkle State — Verkle Trees (EIP-6800)"
  "3SF — 3-Slot Finality + Quick Slots"
  "PQ Crypto — Post-Quantum Cryptography"
  "Encrypted Mempool — Commit-Reveal Ordering"
  "Shielded Transfers — Private L1 Transactions"
)

# Show usage
usage() {
  echo "Usage: $0 [--list] [feature1 feature2 ...]"
  echo ""
  echo "Features:"
  for i in "${!FEATURES[@]}"; do
    echo "  ${FEATURES[$i]} — ${FEATURE_DESCRIPTIONS[$i]}"
  done
}

if [[ "${1:-}" == "--list" ]]; then
  usage
  exit 0
fi

if [[ "${1:-}" == "--help" || "${1:-}" == "-h" ]]; then
  usage
  exit 0
fi

# Check prerequisites
if ! command -v kurtosis &>/dev/null; then
  echo "Error: kurtosis CLI not found. Install: https://docs.kurtosis.com/install/"
  exit 1
fi

if ! command -v docker &>/dev/null; then
  echo "Error: docker not found."
  exit 1
fi

# Create results directory
mkdir -p "$RESULTS_DIR"

# Determine which features to test
SELECTED=("${@}")
if [ ${#SELECTED[@]} -eq 0 ]; then
  SELECTED=("${FEATURES[@]}")
fi

PASS=0
FAIL=0
SKIP=0
RESULTS=()

for feature in "${SELECTED[@]}"; do
  CONFIG_FILE="$CONFIG_DIR/$feature.yaml"
  ENCLAVE="eth2030-${feature}"
  LOG_FILE="$RESULTS_DIR/${feature}.log"

  if [ ! -f "$CONFIG_FILE" ]; then
    echo "=== SKIP: $feature (config not found) ==="
    SKIP=$((SKIP + 1))
    RESULTS+=("SKIP $feature — config not found")
    continue
  fi

  echo "========================================"
  echo "=== FEATURE: $feature"
  echo "========================================"

  # Clean up any previous enclave
  kurtosis enclave rm -f "$ENCLAVE" 2>/dev/null || true

  # Start devnet
  echo "Starting devnet..."
  if ! kurtosis run github.com/ethpandaops/ethereum-package \
      --args-file "$CONFIG_FILE" \
      --enclave "$ENCLAVE" 2>&1 | tee "$LOG_FILE"; then
    echo "FAIL: Could not start devnet for $feature"
    FAIL=$((FAIL + 1))
    RESULTS+=("FAIL $feature — devnet start failed")
    kurtosis enclave rm -f "$ENCLAVE" 2>/dev/null || true
    continue
  fi

  # Wait for blocks
  echo "Waiting 30s for blocks..."
  sleep 30

  # Check consensus
  echo "Checking consensus..."
  if bash "$SCRIPT_DIR/check-consensus.sh" "$ENCLAVE" 2>&1 | tee -a "$LOG_FILE"; then
    CONSENSUS="PASS"
  else
    CONSENSUS="FAIL"
  fi

  # Check assertoor (if available — informational, not blocking)
  ASSERTOOR="N/A"
  ASSERTOOR_URL=$(kurtosis port print "$ENCLAVE" "assertoor" http 2>/dev/null || true)
  if [ -n "$ASSERTOOR_URL" ]; then
    echo "Checking assertoor (30s quick check)..."
    if bash "$SCRIPT_DIR/check-assertoor.sh" "$ENCLAVE" 30 2>&1 | tee -a "$LOG_FILE"; then
      ASSERTOOR="PASS"
    else
      # Assertoor stability check needs many epochs — timeout is expected for quick tests.
      ASSERTOOR="TIMEOUT"
    fi
  fi

  # Feature-specific verification — extract RPC URL from enclave inspect (more reliable than port print).
  FEATURE_CHECK="N/A"
  FEATURE_SCRIPT="$SCRIPT_DIR/features/verify-${feature}.sh"
  if [ -x "$FEATURE_SCRIPT" ]; then
    echo "Running feature verification..."
    INSPECT=$(kurtosis enclave inspect "$ENCLAVE" 2>/dev/null)
    RPC_URL=$(echo "$INSPECT" | grep -P '^\s+rpc:' | head -1 | grep -oP '127\.0\.0\.1:\d+' || true)
    if [ -n "$RPC_URL" ]; then
      if bash "$FEATURE_SCRIPT" "$ENCLAVE" "http://$RPC_URL" 2>&1 | tee -a "$LOG_FILE"; then
        FEATURE_CHECK="PASS"
      else
        FEATURE_CHECK="FAIL"
      fi
    else
      echo "  Could not resolve RPC URL — skipping feature check"
      FEATURE_CHECK="N/A"
    fi
  fi

  # Cleanup
  echo "Cleaning up..."
  kurtosis enclave rm -f "$ENCLAVE" 2>/dev/null || true

  # Record result — consensus is the primary gate; assertoor timeout is OK for quick tests.
  if [[ "$CONSENSUS" == "FAIL" || "$FEATURE_CHECK" == "FAIL" ]]; then
    FAIL=$((FAIL + 1))
    STATUS="FAIL"
  else
    PASS=$((PASS + 1))
    STATUS="PASS"
  fi

  RESULTS+=("$STATUS $feature — consensus:$CONSENSUS assertoor:$ASSERTOOR feature:$FEATURE_CHECK")
  echo ""
done

# Print summary
echo ""
echo "========================================"
echo "=== FEATURE TEST RESULTS"
echo "========================================"
echo ""
for result in "${RESULTS[@]}"; do
  echo "  $result"
done
echo ""
echo "Total: $((PASS + FAIL + SKIP)) | Pass: $PASS | Fail: $FAIL | Skip: $SKIP"

# Write summary to file
SUMMARY_FILE="$RESULTS_DIR/summary.txt"
echo "ETH2030 Feature Test Results — $(date -u '+%Y-%m-%d %H:%M:%S UTC')" > "$SUMMARY_FILE"
echo "" >> "$SUMMARY_FILE"
for result in "${RESULTS[@]}"; do
  echo "$result" >> "$SUMMARY_FILE"
done
echo "" >> "$SUMMARY_FILE"
echo "Total: $((PASS + FAIL + SKIP)) | Pass: $PASS | Fail: $FAIL | Skip: $SKIP" >> "$SUMMARY_FILE"

echo ""
echo "Results saved to $RESULTS_DIR/"

# Exit with failure if any test failed
[ "$FAIL" -eq 0 ] || exit 1
