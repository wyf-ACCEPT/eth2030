package vm

import (
	"math/big"
	"testing"

	"github.com/eth2028/eth2028/core/types"
)

func TestContractNew(t *testing.T) {
	caller := types.BytesToAddress([]byte{1})
	addr := types.BytesToAddress([]byte{2})
	c := NewContract(caller, addr, big.NewInt(100), 50000)

	if c.CallerAddress != caller {
		t.Errorf("CallerAddress = %v, want %v", c.CallerAddress, caller)
	}
	if c.Address != addr {
		t.Errorf("Address = %v, want %v", c.Address, addr)
	}
	if c.Value.Int64() != 100 {
		t.Errorf("Value = %d, want 100", c.Value.Int64())
	}
	if c.Gas != 50000 {
		t.Errorf("Gas = %d, want 50000", c.Gas)
	}
}

func TestContractUseGas(t *testing.T) {
	c := NewContract(types.Address{}, types.Address{}, big.NewInt(0), 1000)

	if !c.UseGas(500) {
		t.Error("UseGas(500) should succeed with 1000 gas")
	}
	if c.Gas != 500 {
		t.Errorf("Gas after UseGas(500) = %d, want 500", c.Gas)
	}

	if c.UseGas(501) {
		t.Error("UseGas(501) should fail with 500 gas remaining")
	}
	if c.Gas != 500 {
		t.Errorf("Gas should remain 500 after failed UseGas, got %d", c.Gas)
	}

	if !c.UseGas(500) {
		t.Error("UseGas(500) should succeed with 500 gas")
	}
	if c.Gas != 0 {
		t.Errorf("Gas after full consumption = %d, want 0", c.Gas)
	}
}

func TestContractGetOp(t *testing.T) {
	c := NewContract(types.Address{}, types.Address{}, big.NewInt(0), 1000)
	c.Code = []byte{byte(PUSH1), 0x42, byte(ADD), byte(STOP)}

	if c.GetOp(0) != PUSH1 {
		t.Errorf("GetOp(0) = %v, want PUSH1", c.GetOp(0))
	}
	if c.GetOp(2) != ADD {
		t.Errorf("GetOp(2) = %v, want ADD", c.GetOp(2))
	}
	if c.GetOp(3) != STOP {
		t.Errorf("GetOp(3) = %v, want STOP", c.GetOp(3))
	}
	// Out of bounds should return STOP
	if c.GetOp(100) != STOP {
		t.Errorf("GetOp(100) = %v, want STOP", c.GetOp(100))
	}
}

func TestContractSetCallCode(t *testing.T) {
	c := NewContract(types.Address{}, types.Address{}, big.NewInt(0), 1000)
	addr := types.BytesToAddress([]byte{0x42})
	hash := types.BytesToHash([]byte{0xab, 0xcd})
	code := []byte{byte(PUSH1), 0x01, byte(STOP)}

	c.SetCallCode(&addr, hash, code)

	if c.Address != addr {
		t.Errorf("Address = %v, want %v", c.Address, addr)
	}
	if c.CodeHash != hash {
		t.Errorf("CodeHash = %v, want %v", c.CodeHash, hash)
	}
	if len(c.Code) != 3 {
		t.Errorf("Code len = %d, want 3", len(c.Code))
	}
}
