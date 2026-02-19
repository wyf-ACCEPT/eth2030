package vm

import (
	"math/big"
	"testing"

	"github.com/eth2028/eth2028/core/state"
	"github.com/eth2028/eth2028/core/types"
)

// TestTransferEventTopic verifies the Transfer event topic matches the
// keccak256 of the ERC-20 Transfer event signature.
func TestTransferEventTopic(t *testing.T) {
	// keccak256("Transfer(address,address,uint256)")
	expected := types.HexToHash("0xddf252ad1be2c89b69c2b068fc378daa952ba7f163c4a11628f55a4df523b3ef")
	if TransferEventTopic != expected {
		t.Errorf("TransferEventTopic = %s, want %s", TransferEventTopic.Hex(), expected.Hex())
	}
}

// TestEmitTransferLog verifies that EmitTransferLog creates a log with the
// correct address, topics, and data.
func TestEmitTransferLog(t *testing.T) {
	statedb := state.NewMemoryStateDB()
	from := types.HexToAddress("0xaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa")
	to := types.HexToAddress("0xbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb")
	amount := big.NewInt(1000)

	EmitTransferLog(statedb, from, to, amount)

	logs := statedb.GetLogs(types.Hash{})
	if len(logs) != 1 {
		t.Fatalf("expected 1 log, got %d", len(logs))
	}

	log := logs[0]

	// Log address should be SystemAddress.
	if log.Address != SystemAddress {
		t.Errorf("log address = %s, want SystemAddress %s", log.Address.Hex(), SystemAddress.Hex())
	}

	// Should have 3 topics: Transfer event sig, from, to.
	if len(log.Topics) != 3 {
		t.Fatalf("expected 3 topics, got %d", len(log.Topics))
	}
	if log.Topics[0] != TransferEventTopic {
		t.Errorf("topic[0] = %s, want TransferEventTopic", log.Topics[0].Hex())
	}

	// Topic 1: from address, zero-padded.
	expectedFrom := addressToTopic(from)
	if log.Topics[1] != expectedFrom {
		t.Errorf("topic[1] = %s, want %s", log.Topics[1].Hex(), expectedFrom.Hex())
	}

	// Topic 2: to address, zero-padded.
	expectedTo := addressToTopic(to)
	if log.Topics[2] != expectedTo {
		t.Errorf("topic[2] = %s, want %s", log.Topics[2].Hex(), expectedTo.Hex())
	}

	// Data: 32-byte big-endian uint256 of the amount.
	if len(log.Data) != 32 {
		t.Fatalf("log data length = %d, want 32", len(log.Data))
	}
	logAmount := new(big.Int).SetBytes(log.Data)
	if logAmount.Cmp(amount) != 0 {
		t.Errorf("log data amount = %s, want %s", logAmount.String(), amount.String())
	}
}

// TestEmitTransferLogZeroAmount ensures no log is emitted for zero-value transfers.
func TestEmitTransferLogZeroAmount(t *testing.T) {
	statedb := state.NewMemoryStateDB()
	from := types.HexToAddress("0xaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa")
	to := types.HexToAddress("0xbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb")

	EmitTransferLog(statedb, from, to, big.NewInt(0))
	EmitTransferLog(statedb, from, to, nil)

	logs := statedb.GetLogs(types.Hash{})
	if len(logs) != 0 {
		t.Errorf("expected 0 logs for zero/nil amount, got %d", len(logs))
	}
}

// TestEmitBurnLog verifies that EmitBurnLog creates a log with the correct
// event signature and data.
func TestEmitBurnLog(t *testing.T) {
	statedb := state.NewMemoryStateDB()
	addr := types.HexToAddress("0xcccccccccccccccccccccccccccccccccccccccc")
	amount := big.NewInt(500)

	EmitBurnLog(statedb, addr, amount)

	logs := statedb.GetLogs(types.Hash{})
	if len(logs) != 1 {
		t.Fatalf("expected 1 log, got %d", len(logs))
	}

	log := logs[0]

	if log.Address != SystemAddress {
		t.Errorf("log address = %s, want SystemAddress", log.Address.Hex())
	}

	if len(log.Topics) != 2 {
		t.Fatalf("expected 2 topics, got %d", len(log.Topics))
	}
	if log.Topics[0] != BurnEventTopic {
		t.Errorf("topic[0] = %s, want BurnEventTopic", log.Topics[0].Hex())
	}

	expectedAddr := addressToTopic(addr)
	if log.Topics[1] != expectedAddr {
		t.Errorf("topic[1] = %s, want %s", log.Topics[1].Hex(), expectedAddr.Hex())
	}

	logAmount := new(big.Int).SetBytes(log.Data)
	if logAmount.Cmp(amount) != 0 {
		t.Errorf("burn amount = %s, want %s", logAmount.String(), amount.String())
	}
}

// TestEmitTransferLogNilStateDB ensures EmitTransferLog handles nil StateDB gracefully.
func TestEmitTransferLogNilStateDB(t *testing.T) {
	// Should not panic.
	EmitTransferLog(nil, types.Address{}, types.Address{}, big.NewInt(100))
}

// TestEIP7708CallTransferEmitsLog verifies that EVM.Call emits a transfer log
// when EIP-7708 is active and value is nonzero.
func TestEIP7708CallTransferEmitsLog(t *testing.T) {
	statedb := state.NewMemoryStateDB()
	caller := types.HexToAddress("0x1111111111111111111111111111111111111111")
	callee := types.HexToAddress("0x2222222222222222222222222222222222222222")

	statedb.CreateAccount(caller)
	statedb.AddBalance(caller, big.NewInt(10000))
	statedb.CreateAccount(callee)

	evm := NewEVM(BlockContext{}, TxContext{}, Config{})
	evm.StateDB = statedb
	evm.forkRules.IsEIP7708 = true

	value := big.NewInt(500)
	_, _, err := evm.Call(caller, callee, nil, 100000, value)
	if err != nil {
		t.Fatalf("Call failed: %v", err)
	}

	logs := statedb.GetLogs(types.Hash{})
	found := false
	for _, log := range logs {
		if log.Address == SystemAddress && len(log.Topics) >= 1 && log.Topics[0] == TransferEventTopic {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected EIP-7708 transfer log from Call, but none found")
	}
}

// TestEIP7708CallNoLogWhenDisabled verifies no transfer log is emitted
// when EIP-7708 is not active.
func TestEIP7708CallNoLogWhenDisabled(t *testing.T) {
	statedb := state.NewMemoryStateDB()
	caller := types.HexToAddress("0x1111111111111111111111111111111111111111")
	callee := types.HexToAddress("0x2222222222222222222222222222222222222222")

	statedb.CreateAccount(caller)
	statedb.AddBalance(caller, big.NewInt(10000))
	statedb.CreateAccount(callee)

	evm := NewEVM(BlockContext{}, TxContext{}, Config{})
	evm.StateDB = statedb
	// EIP-7708 is NOT active (default false).

	value := big.NewInt(500)
	evm.Call(caller, callee, nil, 100000, value)

	logs := statedb.GetLogs(types.Hash{})
	for _, log := range logs {
		if log.Address == SystemAddress && len(log.Topics) >= 1 && log.Topics[0] == TransferEventTopic {
			t.Error("should not emit EIP-7708 transfer log when fork is not active")
		}
	}
}

// TestEIP7708CallNoLogZeroValue verifies no transfer log is emitted
// when value is zero (even if EIP-7708 is active).
func TestEIP7708CallNoLogZeroValue(t *testing.T) {
	statedb := state.NewMemoryStateDB()
	caller := types.HexToAddress("0x1111111111111111111111111111111111111111")
	callee := types.HexToAddress("0x2222222222222222222222222222222222222222")

	statedb.CreateAccount(caller)
	statedb.CreateAccount(callee)

	evm := NewEVM(BlockContext{}, TxContext{}, Config{})
	evm.StateDB = statedb
	evm.forkRules.IsEIP7708 = true

	evm.Call(caller, callee, nil, 100000, big.NewInt(0))

	logs := statedb.GetLogs(types.Hash{})
	for _, log := range logs {
		if log.Address == SystemAddress && len(log.Topics) >= 1 && log.Topics[0] == TransferEventTopic {
			t.Error("should not emit transfer log for zero-value call")
		}
	}
}

// TestEIP7708SelfdestructEmitsTransferLog verifies SELFDESTRUCT emits a transfer
// log when sending balance to a different beneficiary.
func TestEIP7708SelfdestructEmitsTransferLog(t *testing.T) {
	statedb := state.NewMemoryStateDB()
	contractAddr := types.HexToAddress("0xaaaa")
	beneficiary := types.HexToAddress("0xbbbb")

	statedb.CreateAccount(contractAddr)
	statedb.AddBalance(contractAddr, big.NewInt(2000))
	statedb.CreateAccount(beneficiary)

	evm := NewEVM(BlockContext{}, TxContext{}, Config{})
	evm.StateDB = statedb
	evm.forkRules.IsEIP7708 = true

	contract := NewContract(types.Address{}, contractAddr, big.NewInt(0), 1000000)
	mem := NewMemory()
	st := NewStack()
	pc := uint64(0)

	st.Push(new(big.Int).SetBytes(beneficiary[:]))
	_, err := opSelfdestruct(&pc, evm, contract, mem, st)
	if err != nil {
		t.Fatalf("opSelfdestruct: %v", err)
	}

	logs := statedb.GetLogs(types.Hash{})
	found := false
	for _, log := range logs {
		if log.Address == SystemAddress && len(log.Topics) == 3 && log.Topics[0] == TransferEventTopic {
			found = true
			logAmount := new(big.Int).SetBytes(log.Data)
			if logAmount.Cmp(big.NewInt(2000)) != 0 {
				t.Errorf("transfer amount = %s, want 2000", logAmount.String())
			}
		}
	}
	if !found {
		t.Error("expected EIP-7708 transfer log from SELFDESTRUCT")
	}
}

// TestEIP7708SelfdestructToSelfEmitsBurnLog verifies SELFDESTRUCT to self
// emits a burn log.
func TestEIP7708SelfdestructToSelfEmitsBurnLog(t *testing.T) {
	statedb := state.NewMemoryStateDB()
	contractAddr := types.HexToAddress("0xaaaa")

	statedb.CreateAccount(contractAddr)
	statedb.AddBalance(contractAddr, big.NewInt(3000))

	evm := NewEVM(BlockContext{}, TxContext{}, Config{})
	evm.StateDB = statedb
	evm.forkRules.IsEIP7708 = true

	contract := NewContract(types.Address{}, contractAddr, big.NewInt(0), 1000000)
	mem := NewMemory()
	st := NewStack()
	pc := uint64(0)

	// Send to self.
	st.Push(new(big.Int).SetBytes(contractAddr[:]))
	_, err := opSelfdestruct(&pc, evm, contract, mem, st)
	if err != nil {
		t.Fatalf("opSelfdestruct: %v", err)
	}

	logs := statedb.GetLogs(types.Hash{})
	found := false
	for _, log := range logs {
		if log.Address == SystemAddress && len(log.Topics) == 2 && log.Topics[0] == BurnEventTopic {
			found = true
			logAmount := new(big.Int).SetBytes(log.Data)
			if logAmount.Cmp(big.NewInt(3000)) != 0 {
				t.Errorf("burn amount = %s, want 3000", logAmount.String())
			}
		}
	}
	if !found {
		t.Error("expected EIP-7708 burn log from SELFDESTRUCT to self")
	}

	// Balance should remain 3000 (sub then add to self).
	bal := statedb.GetBalance(contractAddr)
	if bal.Cmp(big.NewInt(3000)) != 0 {
		t.Errorf("balance after self-destruct to self = %s, want 3000", bal.String())
	}
}

// TestAddressToTopic verifies address-to-topic conversion pads correctly.
func TestAddressToTopic(t *testing.T) {
	addr := types.HexToAddress("0xaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa")
	topic := addressToTopic(addr)

	// First 12 bytes should be zero.
	for i := 0; i < 12; i++ {
		if topic[i] != 0 {
			t.Errorf("topic byte %d = %x, want 0", i, topic[i])
		}
	}

	// Last 20 bytes should be the address.
	for i := 0; i < 20; i++ {
		if topic[12+i] != addr[i] {
			t.Errorf("topic byte %d = %x, want %x", 12+i, topic[12+i], addr[i])
		}
	}
}

// TestEIP7708CreateTransferEmitsLog verifies that EVM.Create emits a transfer
// log when EIP-7708 is active and value is nonzero.
func TestEIP7708CreateTransferEmitsLog(t *testing.T) {
	statedb := state.NewMemoryStateDB()
	caller := types.HexToAddress("0x1111111111111111111111111111111111111111")

	statedb.CreateAccount(caller)
	statedb.AddBalance(caller, big.NewInt(10000))
	statedb.SetNonce(caller, 1)

	evm := NewEVM(BlockContext{}, TxContext{}, Config{})
	evm.StateDB = statedb
	evm.forkRules.IsEIP7708 = true
	evm.SetJumpTable(NewCancunJumpTable())

	// Deploy a contract that just returns (STOP).
	initCode := []byte{byte(STOP)}
	value := big.NewInt(500)

	_, _, _, err := evm.Create(caller, initCode, 100000, value)
	if err != nil {
		t.Fatalf("Create failed: %v", err)
	}

	logs := statedb.GetLogs(types.Hash{})
	found := false
	for _, log := range logs {
		if log.Address == SystemAddress && len(log.Topics) == 3 && log.Topics[0] == TransferEventTopic {
			found = true
			logAmount := new(big.Int).SetBytes(log.Data)
			if logAmount.Cmp(value) != 0 {
				t.Errorf("transfer amount = %s, want %s", logAmount.String(), value.String())
			}
		}
	}
	if !found {
		t.Error("expected EIP-7708 transfer log from Create")
	}
}
