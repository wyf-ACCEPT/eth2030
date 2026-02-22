package vm

import (
	"math/big"

	"github.com/eth2030/eth2030/core/types"
)

// EIP-7708: ETH transfers and burns emit a log.
//
// All nonzero-value ETH transfers (via CALL, SELFDESTRUCT, or transaction-level)
// emit a LOG3 identical to an ERC-20 Transfer event. Burns emit a LOG2 with a
// Burn event. The log address is SYSTEM_ADDRESS (0xff...fe, EIP-4788).

var (
	// SystemAddress is the EIP-4788 system address used as the log emitter.
	SystemAddress = types.HexToAddress("0xfffffffffffffffffffffffffffffffffffffffe")

	// TransferEventTopic is keccak256("Transfer(address,address,uint256)").
	TransferEventTopic = types.HexToHash("0xddf252ad1be2c89b69c2b068fc378daa952ba7f163c4a11628f55a4df523b3ef")

	// BurnEventTopic is keccak256("Burn(address,uint256)").
	BurnEventTopic = types.HexToHash("0xcc16f5dbb4873280815c1ee09dbd06736cffcc184412cf7a71a0fdb75d397ca5")
)

// EmitTransferLog emits an EIP-7708 ETH transfer log (LOG3).
// It is called whenever nonzero ETH value is transferred between accounts.
// The log is emitted from SYSTEM_ADDRESS with the ERC-20 Transfer event signature.
func EmitTransferLog(statedb StateDB, from, to types.Address, amount *big.Int) {
	if statedb == nil || amount == nil || amount.Sign() <= 0 {
		return
	}

	// Encode amount as big-endian uint256 (32 bytes, left-padded).
	data := make([]byte, 32)
	amountBytes := amount.Bytes()
	copy(data[32-len(amountBytes):], amountBytes)

	statedb.AddLog(&types.Log{
		Address: SystemAddress,
		Topics: []types.Hash{
			TransferEventTopic,
			addressToTopic(from),
			addressToTopic(to),
		},
		Data: data,
	})
}

// EmitBurnLog emits an EIP-7708 ETH burn log (LOG2).
// It is called when ETH is destroyed (e.g., SELFDESTRUCT to self).
func EmitBurnLog(statedb StateDB, addr types.Address, amount *big.Int) {
	if statedb == nil || amount == nil || amount.Sign() <= 0 {
		return
	}

	data := make([]byte, 32)
	amountBytes := amount.Bytes()
	copy(data[32-len(amountBytes):], amountBytes)

	statedb.AddLog(&types.Log{
		Address: SystemAddress,
		Topics: []types.Hash{
			BurnEventTopic,
			addressToTopic(addr),
		},
		Data: data,
	})
}

// addressToTopic converts an address to a 32-byte topic (zero-padded on the left).
func addressToTopic(addr types.Address) types.Hash {
	var topic types.Hash
	copy(topic[12:], addr[:])
	return topic
}
