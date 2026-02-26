#!/usr/bin/env python3
"""Convert Altius/Uniswap dataset JSON to geth evm t8n format.

Input:  uniswap-pf-t1k-c0.json (single file, custom format)
Output: alloc.json, env.json, txs.json (for `evm t8n`)
"""

import json
import sys
import os

def pad_hex32(val):
    """Pad a hex value to 32 bytes (66 chars including 0x prefix)."""
    v = val.removeprefix("0x").removeprefix("0X")
    return "0x" + v.zfill(64)

def clean_hex(val):
    """Strip leading zeros from hex value (geth rejects them)."""
    if not val or val in ("0x", "0x0", "0x00"):
        return "0x0"
    v = val.removeprefix("0x").removeprefix("0X").lstrip("0")
    if not v:
        return "0x0"
    return "0x" + v

def convert(input_path, output_dir):
    with open(input_path) as f:
        data = json.load(f)

    # Extract the first (and usually only) test entry.
    test_name = list(data.keys())[0]
    test = data[test_name]

    # --- alloc.json ---
    # The t8n tool expects GenesisAlloc format:
    # { "0xaddr": { "balance": "0x...", "nonce": "0x...", "code": "0x...", "storage": {...} } }
    alloc = {}
    for addr, acct in test["pre"].items():
        entry = {}
        if acct.get("balance") and acct["balance"] not in ("", "0x", "0x0", "0x00"):
            entry["balance"] = acct["balance"]
        else:
            entry["balance"] = "0x0"
        if acct.get("nonce") and acct["nonce"] not in ("", "0x", "0x0", "0x00"):
            entry["nonce"] = acct["nonce"]
        if acct.get("code") and acct["code"] not in ("", "0x"):
            entry["code"] = acct["code"]
        if acct.get("storage"):
            # geth requires storage keys/values to be 32-byte padded hex
            entry["storage"] = {
                pad_hex32(k): pad_hex32(v)
                for k, v in acct["storage"].items()
            }
        alloc[addr] = entry

    alloc_path = os.path.join(output_dir, "alloc.json")
    with open(alloc_path, "w") as f:
        json.dump(alloc, f)
    print(f"Wrote {alloc_path} ({len(alloc)} accounts)")

    # --- env.json ---
    env_src = test["env"]
    env = {
        "currentCoinbase": env_src["currentCoinbase"],
        "currentGasLimit": env_src["currentGasLimit"],
        "currentNumber": env_src["currentNumber"],
        "currentTimestamp": env_src["currentTimestamp"],
        "currentRandom": env_src.get("currentRandom", "0x0000000000000000000000000000000000000000000000000000000000000000"),
        "currentDifficulty": "0x0",  # post-merge
        "parentBeaconBlockRoot": "0x0000000000000000000000000000000000000000000000000000000000000000",
        "withdrawals": [],
    }
    if env_src.get("currentBaseFee"):
        env["currentBaseFee"] = env_src["currentBaseFee"]
    if env_src.get("currentExcessBlobGas"):
        env["currentExcessBlobGas"] = env_src["currentExcessBlobGas"]
    if env_src.get("parentBlobGasUsed"):
        env["parentBlobGasUsed"] = env_src["parentBlobGasUsed"]

    env_path = os.path.join(output_dir, "env.json")
    with open(env_path, "w") as f:
        json.dump(env, f, indent=2)
    print(f"Wrote {env_path}")

    # --- txs.json ---
    # The t8n tool expects go-ethereum Transaction JSON format:
    # [{ "input": "0x...", "gas": "0x...", "gasPrice": "0x...", "nonce": "0x...",
    #    "to": "0x...", "value": "0x...", "secretKey": "0x...", "v": "0x0", "r": "0x0", "s": "0x0" }]
    txs_src = test["transaction"]
    txs = []
    for tx in txs_src:
        t8n_tx = {
            "input": tx.get("data", "0x"),
            "gas": clean_hex(tx["gasLimit"]),
            "nonce": clean_hex(tx["nonce"]),
            "to": tx["to"],
            "value": clean_hex(tx.get("value", "0x0")),
            "secretKey": tx["secretKey"],
            "v": "0x0",
            "r": "0x0",
            "s": "0x0",
            "gasPrice": clean_hex(tx.get("gasPrice", "0x0")),
        }
        txs.append(t8n_tx)

    txs_path = os.path.join(output_dir, "txs.json")
    with open(txs_path, "w") as f:
        json.dump(txs, f)
    print(f"Wrote {txs_path} ({len(txs)} transactions)")

if __name__ == "__main__":
    if len(sys.argv) < 2:
        print(f"Usage: {sys.argv[0]} <dataset.json> [output_dir]")
        sys.exit(1)

    input_path = sys.argv[1]
    output_dir = sys.argv[2] if len(sys.argv) > 2 else "/tmp/t8n-input"
    os.makedirs(output_dir, exist_ok=True)
    convert(input_path, output_dir)
