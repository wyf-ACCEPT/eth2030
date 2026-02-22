package core

import (
	"encoding/hex"

	"github.com/eth2030/eth2030/core/state"
	"github.com/eth2030/eth2030/core/types"
)

// EIP-7997: Deterministic Factory Predeploy.
//
// A minimal CREATE2 factory is inserted as a system contract at address 0x12
// (in the precompile range). When called, it invokes CREATE2 with:
//   - salt = first 32 bytes of input
//   - initcode = remaining input bytes
//   - value = call value
//
// If input is < 32 bytes, the call reverts.

// FactoryAddress is the predeploy address for the EIP-7997 CREATE2 factory.
var FactoryAddress = types.HexToAddress("0x0000000000000000000000000000000000000012")

// factoryCodeHex is the bytecode from the EIP-7997 specification.
// It implements a minimal CREATE2 factory that takes salt(32 bytes) || initcode.
const factoryCodeHex = "60203610602f5760003560203603806020600037600034f5806026573d600060003e3d6000fd5b60005260206000f35b60006000fd"

// FactoryCode is the decoded bytecode for the EIP-7997 CREATE2 factory.
var FactoryCode []byte

func init() {
	var err error
	FactoryCode, err = hex.DecodeString(factoryCodeHex)
	if err != nil {
		panic("eip7997: invalid factory bytecode hex: " + err.Error())
	}
}

// ApplyEIP7997 deploys the deterministic CREATE2 factory at FactoryAddress.
// This should be called at Glamsterdam fork activation (genesis or hard fork).
// If the account already has code, it is a no-op.
func ApplyEIP7997(statedb state.StateDB) {
	if statedb.GetCodeSize(FactoryAddress) > 0 {
		return
	}
	if !statedb.Exist(FactoryAddress) {
		statedb.CreateAccount(FactoryAddress)
	}
	statedb.SetCode(FactoryAddress, FactoryCode)
}
