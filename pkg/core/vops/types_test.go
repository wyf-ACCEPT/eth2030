package vops

import (
	"math/big"
	"testing"

	"github.com/eth2028/eth2028/core/types"
)

func TestNewPartialState(t *testing.T) {
	ps := NewPartialState()
	if ps.Accounts == nil {
		t.Fatal("Accounts map should not be nil")
	}
	if ps.Storage == nil {
		t.Fatal("Storage map should not be nil")
	}
	if ps.Code == nil {
		t.Fatal("Code map should not be nil")
	}
}

func TestPartialStateAccountOps(t *testing.T) {
	ps := NewPartialState()
	addr := types.BytesToAddress([]byte{0x01})

	if ps.GetAccount(addr) != nil {
		t.Error("expected nil for non-existent account")
	}

	acct := &AccountState{
		Nonce:    5,
		Balance:  big.NewInt(1000),
		CodeHash: types.HexToHash("0xdead"),
	}
	ps.SetAccount(addr, acct)

	got := ps.GetAccount(addr)
	if got == nil {
		t.Fatal("expected account after set")
	}
	if got.Nonce != 5 {
		t.Errorf("nonce = %d, want 5", got.Nonce)
	}
	if got.Balance.Int64() != 1000 {
		t.Errorf("balance = %d, want 1000", got.Balance.Int64())
	}
}

func TestPartialStateStorageOps(t *testing.T) {
	ps := NewPartialState()
	addr := types.BytesToAddress([]byte{0x01})
	key := types.HexToHash("0x01")
	val := types.HexToHash("0xaa")

	// Get non-existent storage.
	got := ps.GetStorage(addr, key)
	if !got.IsZero() {
		t.Error("expected zero hash for non-existent storage")
	}

	ps.SetStorage(addr, key, val)

	got = ps.GetStorage(addr, key)
	if got != val {
		t.Errorf("storage = %x, want %x", got, val)
	}
}

func TestDefaultVOPSConfig(t *testing.T) {
	cfg := DefaultVOPSConfig()
	if cfg.MaxStateSize != 10000 {
		t.Errorf("MaxStateSize = %d, want 10000", cfg.MaxStateSize)
	}
}

func TestExecutionResultFields(t *testing.T) {
	result := &ExecutionResult{
		GasUsed: 21000,
		Success: true,
	}
	if result.GasUsed != 21000 {
		t.Errorf("GasUsed = %d, want 21000", result.GasUsed)
	}
	if !result.Success {
		t.Error("Success should be true")
	}
}

func TestValidityProofFields(t *testing.T) {
	proof := &ValidityProof{
		PreStateRoot:  types.HexToHash("0x01"),
		PostStateRoot: types.HexToHash("0x02"),
		AccessedKeys:  [][]byte{{0x01}},
		ProofData:     []byte{0x03},
	}
	if proof.PreStateRoot.IsZero() {
		t.Error("PreStateRoot should not be zero")
	}
	if proof.PostStateRoot.IsZero() {
		t.Error("PostStateRoot should not be zero")
	}
}
