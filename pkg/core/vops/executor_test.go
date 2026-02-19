package vops

import (
	"math/big"
	"testing"

	"github.com/eth2028/eth2028/core/types"
)

func makeTestState() *PartialState {
	ps := NewPartialState()
	sender := types.BytesToAddress([]byte{0x01})
	ps.SetAccount(sender, &AccountState{
		Nonce:    0,
		Balance:  big.NewInt(1_000_000_000),
		CodeHash: types.EmptyCodeHash,
	})

	recipient := types.BytesToAddress([]byte{0x02})
	ps.SetAccount(recipient, &AccountState{
		Nonce:    0,
		Balance:  big.NewInt(500),
		CodeHash: types.EmptyCodeHash,
	})
	return ps
}

func makeTestTx(sender, recipient types.Address, value int64, nonce uint64) *types.Transaction {
	to := recipient
	tx := types.NewTransaction(&types.LegacyTx{
		Nonce:    nonce,
		GasPrice: big.NewInt(1),
		Gas:      100000,
		To:       &to,
		Value:    big.NewInt(value),
	})
	tx.SetSender(sender)
	return tx
}

func makeTestHeader() *types.Header {
	return &types.Header{
		Number:   big.NewInt(1),
		Coinbase: types.BytesToAddress([]byte{0xff}),
	}
}

func TestExecuteSimpleTransfer(t *testing.T) {
	pe := NewPartialExecutor(DefaultVOPSConfig())
	ps := makeTestState()
	sender := types.BytesToAddress([]byte{0x01})
	recipient := types.BytesToAddress([]byte{0x02})
	header := makeTestHeader()
	tx := makeTestTx(sender, recipient, 100, 0)

	result, err := pe.Execute(tx, ps, header)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !result.Success {
		t.Error("expected successful execution")
	}
	if result.GasUsed != 21000 {
		t.Errorf("GasUsed = %d, want 21000", result.GasUsed)
	}

	// Verify sender balance decreased.
	postSender := result.PostState.GetAccount(sender)
	if postSender == nil {
		t.Fatal("sender missing from post state")
	}
	// Expected: initial(1000000000) - gasCost(100000*1) - value(100) + refund((100000-21000)*1)
	// = 1000000000 - 100100 + 79000 = 999978900
	expectedBal := big.NewInt(1_000_000_000 - 100 - 21000)
	if postSender.Balance.Cmp(expectedBal) != 0 {
		t.Errorf("sender balance = %s, want %s", postSender.Balance, expectedBal)
	}
	if postSender.Nonce != 1 {
		t.Errorf("sender nonce = %d, want 1", postSender.Nonce)
	}

	// Verify recipient balance increased.
	postRecipient := result.PostState.GetAccount(recipient)
	if postRecipient == nil {
		t.Fatal("recipient missing from post state")
	}
	if postRecipient.Balance.Int64() != 600 {
		t.Errorf("recipient balance = %d, want 600", postRecipient.Balance.Int64())
	}

	// Verify accessed keys are recorded.
	if len(result.AccessedKeys) == 0 {
		t.Error("expected accessed keys to be recorded")
	}
}

func TestExecuteNonceMismatch(t *testing.T) {
	pe := NewPartialExecutor(DefaultVOPSConfig())
	ps := makeTestState()
	sender := types.BytesToAddress([]byte{0x01})
	recipient := types.BytesToAddress([]byte{0x02})
	header := makeTestHeader()

	// Wrong nonce.
	tx := makeTestTx(sender, recipient, 100, 5)
	_, err := pe.Execute(tx, ps, header)
	if err != ErrNonceMismatch {
		t.Errorf("expected ErrNonceMismatch, got %v", err)
	}
}

func TestExecuteInsufficientBalance(t *testing.T) {
	pe := NewPartialExecutor(DefaultVOPSConfig())
	ps := makeTestState()
	sender := types.BytesToAddress([]byte{0x01})
	recipient := types.BytesToAddress([]byte{0x02})
	header := makeTestHeader()

	// Transfer more than balance.
	tx := makeTestTx(sender, recipient, 2_000_000_000, 0)
	_, err := pe.Execute(tx, ps, header)
	if err != ErrInsufficientBal {
		t.Errorf("expected ErrInsufficientBal, got %v", err)
	}
}

func TestExecuteMissingSender(t *testing.T) {
	pe := NewPartialExecutor(DefaultVOPSConfig())
	ps := NewPartialState()
	header := makeTestHeader()

	unknown := types.BytesToAddress([]byte{0xaa})
	recipient := types.BytesToAddress([]byte{0x02})
	tx := makeTestTx(unknown, recipient, 100, 0)

	_, err := pe.Execute(tx, ps, header)
	if err != ErrMissingSender {
		t.Errorf("expected ErrMissingSender, got %v", err)
	}
}

func TestExecuteStateTooLarge(t *testing.T) {
	cfg := VOPSConfig{MaxStateSize: 1}
	pe := NewPartialExecutor(cfg)

	ps := NewPartialState()
	a1 := types.BytesToAddress([]byte{0x01})
	a2 := types.BytesToAddress([]byte{0x02})
	ps.SetAccount(a1, &AccountState{Balance: big.NewInt(1000)})
	ps.SetAccount(a2, &AccountState{Balance: big.NewInt(500)})

	header := makeTestHeader()
	tx := makeTestTx(a1, a2, 100, 0)

	_, err := pe.Execute(tx, ps, header)
	if err != ErrStateTooLarge {
		t.Errorf("expected ErrStateTooLarge, got %v", err)
	}
}

func TestExecuteContractCreation(t *testing.T) {
	pe := NewPartialExecutor(DefaultVOPSConfig())
	ps := NewPartialState()
	sender := types.BytesToAddress([]byte{0x01})
	ps.SetAccount(sender, &AccountState{
		Nonce:    0,
		Balance:  big.NewInt(10_000_000),
		CodeHash: types.EmptyCodeHash,
	})

	header := makeTestHeader()
	tx := types.NewTransaction(&types.LegacyTx{
		Nonce:    0,
		GasPrice: big.NewInt(1),
		Gas:      200000,
		To:       nil,
		Value:    big.NewInt(0),
		Data:     []byte{0x60, 0x00, 0x60, 0x00},
	})
	tx.SetSender(sender)

	result, err := pe.Execute(tx, ps, header)
	if err != nil {
		t.Fatalf("Execute contract creation: %v", err)
	}
	if !result.Success {
		t.Error("contract creation should succeed")
	}
	if result.GasUsed == 0 {
		t.Error("gas used should be > 0")
	}
}

func TestExecuteNewRecipient(t *testing.T) {
	pe := NewPartialExecutor(DefaultVOPSConfig())
	ps := NewPartialState()
	sender := types.BytesToAddress([]byte{0x01})
	ps.SetAccount(sender, &AccountState{
		Nonce:    0,
		Balance:  big.NewInt(10_000_000),
		CodeHash: types.EmptyCodeHash,
	})

	// Recipient not in partial state.
	recipient := types.BytesToAddress([]byte{0xcc})
	header := makeTestHeader()
	tx := makeTestTx(sender, recipient, 100, 0)

	result, err := pe.Execute(tx, ps, header)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !result.Success {
		t.Error("should succeed creating new recipient")
	}

	// Recipient should exist in post-state.
	postRecipient := result.PostState.GetAccount(recipient)
	if postRecipient == nil {
		t.Fatal("new recipient missing from post state")
	}
	if postRecipient.Balance.Int64() != 100 {
		t.Errorf("new recipient balance = %d, want 100", postRecipient.Balance.Int64())
	}
}

func TestExecuteCoinbasePaid(t *testing.T) {
	pe := NewPartialExecutor(DefaultVOPSConfig())
	ps := makeTestState()
	sender := types.BytesToAddress([]byte{0x01})
	recipient := types.BytesToAddress([]byte{0x02})
	header := makeTestHeader()
	tx := makeTestTx(sender, recipient, 100, 0)

	result, err := pe.Execute(tx, ps, header)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}

	coinbase := result.PostState.GetAccount(header.Coinbase)
	if coinbase == nil {
		t.Fatal("coinbase missing from post state")
	}
	// Fee = gasUsed * gasPrice = 21000 * 1 = 21000
	if coinbase.Balance.Int64() != 21000 {
		t.Errorf("coinbase balance = %d, want 21000", coinbase.Balance.Int64())
	}
}

func TestClonePartialState(t *testing.T) {
	ps := makeTestState()
	addr := types.BytesToAddress([]byte{0x01})
	key := types.HexToHash("0x01")
	val := types.HexToHash("0xaa")
	ps.SetStorage(addr, key, val)
	ps.Code[addr] = []byte{0x60, 0x00}

	clone := clonePartialState(ps)

	// Verify independence.
	clone.GetAccount(addr).Balance = big.NewInt(999)
	if ps.GetAccount(addr).Balance.Int64() == 999 {
		t.Error("clone should be independent from original")
	}

	// Verify storage was cloned.
	if clone.GetStorage(addr, key) != val {
		t.Error("storage not cloned correctly")
	}

	// Verify code was cloned.
	if len(clone.Code[addr]) != 2 {
		t.Error("code not cloned correctly")
	}
}
