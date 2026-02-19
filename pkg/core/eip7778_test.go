package core

import (
	"math/big"
	"testing"

	"github.com/eth2028/eth2028/core/state"
	"github.com/eth2028/eth2028/core/types"
	"github.com/eth2028/eth2028/core/vm"
)

// TestEIP7778BlockGasAccountingNoRefunds verifies that under Glamsterdam,
// block gas accounting uses pre-refund gas (EIP-7778), while user gas
// still receives refunds.
func TestEIP7778BlockGasAccountingNoRefunds(t *testing.T) {
	statedb := state.NewMemoryStateDB()

	sender := types.HexToAddress("0x1111111111111111111111111111111111111111")
	statedb.CreateAccount(sender)
	statedb.AddBalance(sender, new(big.Int).Mul(big.NewInt(1e18), big.NewInt(100)))
	statedb.SetNonce(sender, 0)

	to := types.HexToAddress("0x2222222222222222222222222222222222222222")
	statedb.CreateAccount(to)

	// Simple value transfer under Glamsterdam.
	msg := &Message{
		From:     sender,
		To:       &to,
		GasLimit: 100000,
		GasPrice: big.NewInt(1),
		Value:    big.NewInt(1),
		Nonce:    0,
	}

	header := &types.Header{
		Number:   big.NewInt(100),
		GasLimit: 60_000_000,
		BaseFee:  big.NewInt(1),
		Time:     100,
	}

	gp := new(GasPool).AddGas(header.GasLimit)

	result, err := applyMessage(TestConfigGlamsterdan, nil, statedb, header, msg, gp)
	if err != nil {
		t.Fatalf("applyMessage failed: %v", err)
	}

	// Under Glamsterdam, BlockGasUsed should be >= UsedGas (pre-refund).
	if result.BlockGasUsed < result.UsedGas {
		t.Errorf("BlockGasUsed (%d) < UsedGas (%d); EIP-7778 requires block gas >= user gas",
			result.BlockGasUsed, result.UsedGas)
	}
}

// TestEIP7778GlamsterdamConstants verifies the EIP-7778 related constants.
func TestEIP7778GlamsterdamConstants(t *testing.T) {
	if vm.MaxRefundQuotient != 5 {
		t.Errorf("MaxRefundQuotient = %d, want 5", vm.MaxRefundQuotient)
	}
}

// TestEIP7778BlockGasPoolCorrectness verifies that under Glamsterdam the gas pool
// accounts for pre-refund gas, preventing block gas limit circumvention.
func TestEIP7778BlockGasPoolCorrectness(t *testing.T) {
	statedb := state.NewMemoryStateDB()

	sender := types.HexToAddress("0xaaaa")
	statedb.CreateAccount(sender)
	statedb.AddBalance(sender, new(big.Int).Mul(big.NewInt(1e18), big.NewInt(100)))

	to := types.HexToAddress("0xbbbb")
	statedb.CreateAccount(to)

	msg := &Message{
		From:     sender,
		To:       &to,
		GasLimit: 100000,
		GasPrice: big.NewInt(1),
		Value:    big.NewInt(1),
		Nonce:    0,
	}

	header := &types.Header{
		Number:   big.NewInt(1),
		GasLimit: 60_000_000,
		BaseFee:  big.NewInt(1),
		Time:     100,
	}

	initialPoolGas := header.GasLimit
	gp := new(GasPool).AddGas(initialPoolGas)

	result, err := applyMessage(TestConfigGlamsterdan, nil, statedb, header, msg, gp)
	if err != nil {
		t.Fatalf("applyMessage failed: %v", err)
	}

	// Under Glamsterdam, gas consumed from pool = BlockGasUsed (pre-refund).
	poolAfter := gp.Gas()
	poolConsumed := initialPoolGas - poolAfter
	if poolConsumed != result.BlockGasUsed {
		t.Errorf("gas pool consumed %d, want BlockGasUsed %d", poolConsumed, result.BlockGasUsed)
	}
}
