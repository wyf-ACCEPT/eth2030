#!/usr/bin/env bash
# Fetch research data from Ethereum community forums.
#
# Usage:
#   ./tools/fetch-all.sh                    # full fetch (slow, thousands of topics)
#   ./tools/fetch-all.sh --topics-only      # fast: topic listings only, no post bodies
#   ./tools/fetch-all.sh --limit 50         # limit to 50 topics per category
#   ./tools/fetch-all.sh --search "verkle"  # search both sites for a keyword

set -euo pipefail
cd "$(dirname "$0")/.."

ARGS=("$@")

echo "=== Ethereum Magicians (EIP/ERC discussions) ==="
python3 tools/fetch-magicians.py "${ARGS[@]}"

echo ""
echo "=== Ethereum Research (ethresear.ch) ==="
python3 tools/fetch-ethresearch.py "${ARGS[@]}"

echo ""
echo "Done. Data saved to data/magicians/ and data/ethresearch/"
