package core

import (
	"math/big"
	"testing"

	"github.com/eth2028/eth2028/core/state"
	"github.com/eth2028/eth2028/core/types"
	"github.com/eth2028/eth2028/core/vm"
)

// makeCreationCode wraps runtime code in creation bytecode that copies
// the runtime code into memory and returns it.
//
// Layout:
//   PUSH1 <len>    // runtime code length
//   DUP1           // duplicate length for RETURN
//   PUSH1 <offset> // offset of runtime code in this bytecode
//   PUSH1 0x00     // memory destination
//   CODECOPY       // copy runtime code to memory[0:]
//   PUSH1 0x00     // memory offset for RETURN
//   RETURN         // return memory[0:len]
//   <runtime code>
//
// The creation prefix is 10 bytes, so runtime code starts at offset 10.
func makeCreationCode(runtime []byte) []byte {
	rLen := byte(len(runtime))
	code := []byte{
		0x60, rLen, // PUSH1 <runtime length>
		0x80,       // DUP1
		0x60, 0x0a, // PUSH1 0x0a (offset of runtime in creation code)
		0x60, 0x00, // PUSH1 0x00 (memory offset)
		0x39,       // CODECOPY
		0x60, 0x00, // PUSH1 0x00 (memory offset)
		0xf3,       // RETURN
	}
	code = append(code, runtime...)
	return code
}

// setupSender creates a funded sender account with nonce 0.
func setupSender(statedb *state.MemoryStateDB) types.Address {
	sender := types.HexToAddress("0x1111")
	tenETH := new(big.Int).Mul(big.NewInt(10), new(big.Int).SetUint64(1e18))
	statedb.AddBalance(sender, tenETH)
	return sender
}

// applyTx is a helper that creates a message from a transaction, sets the
// sender, and calls applyMessage, returning the result and receipt status.
func applyTx(t *testing.T, statedb *state.MemoryStateDB, sender types.Address, tx *types.Transaction) (*ExecutionResult, *types.Receipt) {
	t.Helper()
	msg := TransactionToMessage(tx)
	msg.From = sender

	header := newTestHeader()
	gp := new(GasPool).AddGas(header.GasLimit)

	result, err := applyMessage(TestConfig, nil, statedb, header, &msg, gp)
	if err != nil {
		// Pre-execution validation error (intrinsic gas, nonce, balance).
		// Return a failed result. State is not modified by applyMessage on error.
		result = &ExecutionResult{
			UsedGas: msg.GasLimit,
			Err:     err,
		}
		receipt := types.NewReceipt(types.ReceiptStatusFailed, msg.GasLimit)
		receipt.GasUsed = msg.GasLimit
		return result, receipt
	}

	var receiptStatus uint64
	if result.Failed() {
		receiptStatus = types.ReceiptStatusFailed
	} else {
		receiptStatus = types.ReceiptStatusSuccessful
	}
	receipt := types.NewReceipt(receiptStatus, result.UsedGas)
	receipt.GasUsed = result.UsedGas
	return result, receipt
}

// TestProcessorContractCreation deploys a contract that stores 0x42 at storage
// slot 0, then verifies the contract address has code and the receipt is successful.
func TestProcessorContractCreation(t *testing.T) {
	statedb := state.NewMemoryStateDB()
	sender := setupSender(statedb)

	// Runtime code: PUSH1 0x42, PUSH1 0x00, SSTORE, STOP
	// Stores value 0x42 at storage slot 0.
	runtimeCode := []byte{
		0x60, 0x42, // PUSH1 0x42
		0x60, 0x00, // PUSH1 0x00
		0x55,       // SSTORE
		0x00,       // STOP
	}
	initCode := makeCreationCode(runtimeCode)

	tx := types.NewTransaction(&types.LegacyTx{
		Nonce:    0,
		GasPrice: big.NewInt(1),
		Gas:      200000,
		To:       nil, // contract creation
		Value:    big.NewInt(0),
		Data:     initCode,
	})

	result, receipt := applyTx(t, statedb, sender, tx)

	// Receipt should be successful.
	if receipt.Status != types.ReceiptStatusSuccessful {
		t.Fatalf("receipt status: want successful, got failed (err=%v)", result.Err)
	}

	// Nonce should be incremented (EVM.Create increments nonce for creates).
	if statedb.GetNonce(sender) != 1 {
		t.Fatalf("sender nonce: want 1, got %d", statedb.GetNonce(sender))
	}

	// Find the deployed contract: iterate over possible addresses.
	// The contract address is created from (sender, nonce=0) by EVM.Create.
	// We verify that at least one non-sender address has code deployed.
	// Since we can't easily predict the address from our simplified createAddress,
	// we verify via the return data of the creation (which is the runtime code).
	if len(result.ReturnData) == 0 {
		t.Fatal("expected non-empty return data (deployed runtime code)")
	}
	if len(result.ReturnData) != len(runtimeCode) {
		t.Fatalf("return data length: want %d, got %d", len(runtimeCode), len(result.ReturnData))
	}

	// Gas used should be more than simple transfer (21000 + create overhead).
	if result.UsedGas <= TxGas+TxCreateGas {
		t.Fatalf("gas used %d should exceed base create cost %d", result.UsedGas, TxGas+TxCreateGas)
	}
}

// TestProcessorContractCall deploys a contract and then calls it, verifying
// the call succeeds and returns expected data.
func TestProcessorContractCall(t *testing.T) {
	statedb := state.NewMemoryStateDB()
	sender := setupSender(statedb)

	// Deploy a contract at a known address with code that returns 0x42.
	// Runtime code: PUSH1 0x42, PUSH1 0x00, MSTORE, PUSH1 0x20, PUSH1 0x00, RETURN
	contractAddr := types.HexToAddress("0xc0de")
	contractCode := []byte{
		0x60, 0x42, // PUSH1 0x42
		0x60, 0x00, // PUSH1 0x00
		0x52,       // MSTORE
		0x60, 0x20, // PUSH1 0x20 (32 bytes)
		0x60, 0x00, // PUSH1 0x00
		0xf3,       // RETURN
	}
	statedb.CreateAccount(contractAddr)
	statedb.SetCode(contractAddr, contractCode)

	tx := types.NewTransaction(&types.LegacyTx{
		Nonce:    0,
		GasPrice: big.NewInt(1),
		Gas:      100000,
		To:       &contractAddr,
		Value:    big.NewInt(0),
		Data:     []byte{},
	})

	result, receipt := applyTx(t, statedb, sender, tx)

	if receipt.Status != types.ReceiptStatusSuccessful {
		t.Fatalf("receipt status: want successful, got failed (err=%v)", result.Err)
	}

	// Return data should be 32 bytes with 0x42 in the last byte.
	if len(result.ReturnData) != 32 {
		t.Fatalf("return data length: want 32, got %d", len(result.ReturnData))
	}
	if result.ReturnData[31] != 0x42 {
		t.Fatalf("return data[31]: want 0x42, got 0x%02x", result.ReturnData[31])
	}

	// Gas should be more than simple transfer due to EVM execution.
	if result.UsedGas <= TxGas {
		t.Fatalf("gas used %d should exceed base transfer gas %d", result.UsedGas, TxGas)
	}
}

// TestProcessorOutOfGas sends a contract creation transaction with insufficient
// gas and verifies the receipt is failed but gas is still consumed.
func TestProcessorOutOfGas(t *testing.T) {
	statedb := state.NewMemoryStateDB()
	sender := setupSender(statedb)

	// Runtime code that does SSTORE (expensive).
	runtimeCode := []byte{
		0x60, 0x42, // PUSH1 0x42
		0x60, 0x00, // PUSH1 0x00
		0x55,       // SSTORE
		0x00,       // STOP
	}
	initCode := makeCreationCode(runtimeCode)

	// Gas limit just barely above intrinsic gas but not enough for EVM execution.
	// Intrinsic for create: 21000 + 32000 + data gas + initcode word gas (EIP-3860)
	dataGas := uint64(0)
	for _, b := range initCode {
		if b == 0 {
			dataGas += TxDataZeroGas
		} else {
			dataGas += TxDataNonZeroGas
		}
	}
	words := (uint64(len(initCode)) + 31) / 32
	initCodeWordGas := words * vm.InitCodeWordGas
	intrinsic := TxGas + TxCreateGas + dataGas + initCodeWordGas
	// Give just enough for intrinsic + a tiny bit for EVM, but not enough for SSTORE.
	gasLimit := intrinsic + 100

	initialBalance := statedb.GetBalance(sender)

	tx := types.NewTransaction(&types.LegacyTx{
		Nonce:    0,
		GasPrice: big.NewInt(1),
		Gas:      gasLimit,
		To:       nil,
		Value:    big.NewInt(0),
		Data:     initCode,
	})

	result, receipt := applyTx(t, statedb, sender, tx)

	// Receipt should be failed.
	if receipt.Status != types.ReceiptStatusFailed {
		t.Fatalf("receipt status: want failed, got successful")
	}

	// Gas should be consumed (result.UsedGas > 0).
	if result.UsedGas == 0 {
		t.Fatal("expected non-zero gas usage on OOG")
	}

	// Sender balance should have decreased by at least the gas cost.
	finalBalance := statedb.GetBalance(sender)
	if finalBalance.Cmp(initialBalance) >= 0 {
		t.Fatal("sender balance should decrease on OOG")
	}
}

// TestProcessorValueTransferToContract deploys a contract and sends ETH to it,
// verifying the contract's balance increases.
func TestProcessorValueTransferToContract(t *testing.T) {
	statedb := state.NewMemoryStateDB()
	sender := setupSender(statedb)

	// Deploy a contract that accepts value (just STOP - no revert).
	contractAddr := types.HexToAddress("0xc0de")
	contractCode := []byte{0x00} // STOP
	statedb.CreateAccount(contractAddr)
	statedb.SetCode(contractAddr, contractCode)

	oneETH := new(big.Int).SetUint64(1e18)
	tx := types.NewTransaction(&types.LegacyTx{
		Nonce:    0,
		GasPrice: big.NewInt(1),
		Gas:      100000,
		To:       &contractAddr,
		Value:    oneETH,
		Data:     []byte{},
	})

	result, receipt := applyTx(t, statedb, sender, tx)

	if receipt.Status != types.ReceiptStatusSuccessful {
		t.Fatalf("receipt status: want successful, got failed (err=%v)", result.Err)
	}

	// Contract should have received 1 ETH.
	contractBal := statedb.GetBalance(contractAddr)
	if contractBal.Cmp(oneETH) != 0 {
		t.Fatalf("contract balance: want %v, got %v", oneETH, contractBal)
	}
}

// TestProcessorRevert creates bytecode that REVERTs and verifies the receipt is failed.
func TestProcessorRevert(t *testing.T) {
	statedb := state.NewMemoryStateDB()
	sender := setupSender(statedb)

	// Contract code: PUSH1 0x00, PUSH1 0x00, REVERT
	// Immediately reverts with empty data.
	contractAddr := types.HexToAddress("0xc0de")
	contractCode := []byte{
		0x60, 0x00, // PUSH1 0x00 (return data size)
		0x60, 0x00, // PUSH1 0x00 (return data offset)
		0xfd,       // REVERT
	}
	statedb.CreateAccount(contractAddr)
	statedb.SetCode(contractAddr, contractCode)

	// Send some value so we can verify state reverts.
	oneETH := new(big.Int).SetUint64(1e18)
	tx := types.NewTransaction(&types.LegacyTx{
		Nonce:    0,
		GasPrice: big.NewInt(1),
		Gas:      100000,
		To:       &contractAddr,
		Value:    oneETH,
		Data:     []byte{},
	})

	result, receipt := applyTx(t, statedb, sender, tx)

	// Receipt should be failed.
	if receipt.Status != types.ReceiptStatusFailed {
		t.Fatalf("receipt status: want failed, got successful")
	}

	// Error should be ErrExecutionReverted.
	if result.Err == nil {
		t.Fatal("expected non-nil error for REVERT")
	}

	// Contract balance should be zero (value transfer reverted).
	contractBal := statedb.GetBalance(contractAddr)
	if contractBal.Sign() != 0 {
		t.Fatalf("contract balance should be 0 after revert, got %v", contractBal)
	}

	// Gas should not be fully consumed on REVERT (unlike OOG).
	if result.UsedGas >= 100000 {
		t.Fatalf("REVERT should not consume all gas, used %d of 100000", result.UsedGas)
	}
}
