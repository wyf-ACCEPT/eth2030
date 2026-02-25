#!/usr/bin/env bash
ENCLAVE="${1:-eth2030-devnet}"
echo "Destroying enclave: $ENCLAVE"
kurtosis enclave rm -f "$ENCLAVE" 2>/dev/null || true
echo "Done."
