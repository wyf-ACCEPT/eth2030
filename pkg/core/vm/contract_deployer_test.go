package vm

import (
	"math/big"
	"sync"
	"testing"

	"github.com/eth2030/eth2030/core/types"
	"github.com/eth2030/eth2030/crypto"
)

func TestNewContractDeployer(t *testing.T) {
	config := DefaultDeployConfig()
	cd := NewContractDeployer(config)
	if cd == nil {
		t.Fatal("expected non-nil deployer")
	}
	if cd.DeploymentCount() != 0 {
		t.Fatalf("expected 0 deployments, got %d", cd.DeploymentCount())
	}
}

func TestDeployBasic(t *testing.T) {
	cd := NewContractDeployer(DefaultDeployConfig())
	creator := types.HexToAddress("0xaaaa")
	code := []byte{0x60, 0x00, 0x60, 0x00, 0xf3} // PUSH 0, PUSH 0, RETURN

	result := cd.Deploy(creator, code, big.NewInt(0), 0)
	if !result.Success {
		t.Fatalf("deploy failed: %s", result.Error)
	}
	if result.Address.IsZero() {
		t.Fatal("expected non-zero address")
	}
	if result.CodeHash.IsZero() {
		t.Fatal("expected non-zero code hash")
	}
	if result.GasUsed == 0 {
		t.Fatal("expected non-zero gas used")
	}

	// Verify code hash matches keccak of code.
	expected := crypto.Keccak256Hash(code)
	if result.CodeHash != expected {
		t.Fatalf("code hash mismatch: got %s, want %s", result.CodeHash, expected)
	}
}

func TestDeployEmptyCode(t *testing.T) {
	cd := NewContractDeployer(DefaultDeployConfig())
	creator := types.HexToAddress("0xbbbb")

	result := cd.Deploy(creator, nil, big.NewInt(0), 0)
	if result.Success {
		t.Fatal("expected failure for nil code")
	}
	if result.Error != "empty code" {
		t.Fatalf("unexpected error: %s", result.Error)
	}

	result = cd.Deploy(creator, []byte{}, big.NewInt(0), 0)
	if result.Success {
		t.Fatal("expected failure for empty code")
	}
}

func TestDeployCodeSizeExceeded(t *testing.T) {
	config := DeployConfig{MaxCodeSize: 10, InitGasLimit: 10_000_000}
	cd := NewContractDeployer(config)
	creator := types.HexToAddress("0xcccc")
	code := make([]byte, 11)

	result := cd.Deploy(creator, code, big.NewInt(0), 0)
	if result.Success {
		t.Fatal("expected failure for oversized code")
	}
	if result.Error != "code size exceeds maximum" {
		t.Fatalf("unexpected error: %s", result.Error)
	}
}

func TestDeployGasLimitExceeded(t *testing.T) {
	config := DeployConfig{MaxCodeSize: 50000, InitGasLimit: 100}
	cd := NewContractDeployer(config)
	creator := types.HexToAddress("0xdddd")
	code := make([]byte, 10) // 32000 + 10*200 = 34000 > 100

	result := cd.Deploy(creator, code, big.NewInt(0), 0)
	if result.Success {
		t.Fatal("expected failure for gas limit exceeded")
	}
	if result.Error != "gas limit exceeded" {
		t.Fatalf("unexpected error: %s", result.Error)
	}
}

func TestDeployAddressCollision(t *testing.T) {
	cd := NewContractDeployer(DefaultDeployConfig())
	creator := types.HexToAddress("0xeeee")
	code := []byte{0x60, 0x00, 0xf3}

	// Deploy twice with same creator and nonce should collide.
	r1 := cd.Deploy(creator, code, big.NewInt(0), 0)
	if !r1.Success {
		t.Fatalf("first deploy failed: %s", r1.Error)
	}

	r2 := cd.Deploy(creator, code, big.NewInt(0), 0)
	if r2.Success {
		t.Fatal("expected address collision")
	}
	if r2.Error != "address collision" {
		t.Fatalf("unexpected error: %s", r2.Error)
	}
}

func TestDeployDifferentNonces(t *testing.T) {
	cd := NewContractDeployer(DefaultDeployConfig())
	creator := types.HexToAddress("0x1111")
	code := []byte{0x60, 0x00, 0xf3}

	r1 := cd.Deploy(creator, code, big.NewInt(0), 0)
	r2 := cd.Deploy(creator, code, big.NewInt(0), 1)
	r3 := cd.Deploy(creator, code, big.NewInt(0), 2)

	if !r1.Success || !r2.Success || !r3.Success {
		t.Fatal("expected all deployments to succeed")
	}

	if r1.Address == r2.Address || r2.Address == r3.Address {
		t.Fatal("expected different addresses for different nonces")
	}

	if cd.DeploymentCount() != 3 {
		t.Fatalf("expected 3 deployments, got %d", cd.DeploymentCount())
	}
}

func TestDeployCreate2(t *testing.T) {
	cd := NewContractDeployer(DefaultDeployConfig())
	creator := types.HexToAddress("0x2222")
	code := []byte{0x60, 0x00, 0xf3}
	salt := types.HexToHash("0xabcdef")

	result := cd.DeployCreate2(creator, code, salt)
	if !result.Success {
		t.Fatalf("create2 deploy failed: %s", result.Error)
	}
	if result.Address.IsZero() {
		t.Fatal("expected non-zero address")
	}

	// Verify address matches expected CREATE2 derivation.
	initCodeHash := crypto.Keccak256Hash(code)
	expected := ComputeCreate2Address(creator, salt, initCodeHash)
	if result.Address != expected {
		t.Fatalf("address mismatch: got %s, want %s", result.Address, expected)
	}
}

func TestDeployCreate2EmptyCode(t *testing.T) {
	cd := NewContractDeployer(DefaultDeployConfig())
	creator := types.HexToAddress("0x3333")
	salt := types.HexToHash("0x01")

	result := cd.DeployCreate2(creator, nil, salt)
	if result.Success {
		t.Fatal("expected failure for nil code")
	}
}

func TestDeployCreate2CodeSizeExceeded(t *testing.T) {
	config := DeployConfig{MaxCodeSize: 5, InitGasLimit: 10_000_000}
	cd := NewContractDeployer(config)
	creator := types.HexToAddress("0x4444")
	code := make([]byte, 6)

	result := cd.DeployCreate2(creator, code, types.Hash{})
	if result.Success {
		t.Fatal("expected failure for oversized code")
	}
}

func TestDeployCreate2Collision(t *testing.T) {
	cd := NewContractDeployer(DefaultDeployConfig())
	creator := types.HexToAddress("0x5555")
	code := []byte{0x60, 0x00, 0xf3}
	salt := types.HexToHash("0x99")

	r1 := cd.DeployCreate2(creator, code, salt)
	if !r1.Success {
		t.Fatalf("first deploy failed: %s", r1.Error)
	}

	r2 := cd.DeployCreate2(creator, code, salt)
	if r2.Success {
		t.Fatal("expected collision on same creator+salt+code")
	}
}

func TestDeployCreate2DifferentSalts(t *testing.T) {
	cd := NewContractDeployer(DefaultDeployConfig())
	creator := types.HexToAddress("0x6666")
	code := []byte{0x60, 0x00, 0xf3}

	r1 := cd.DeployCreate2(creator, code, types.HexToHash("0x01"))
	r2 := cd.DeployCreate2(creator, code, types.HexToHash("0x02"))
	if !r1.Success || !r2.Success {
		t.Fatal("expected both deployments to succeed")
	}
	if r1.Address == r2.Address {
		t.Fatal("expected different addresses for different salts")
	}
}

func TestGetDeployment(t *testing.T) {
	cd := NewContractDeployer(DefaultDeployConfig())
	creator := types.HexToAddress("0x7777")
	code := []byte{0x60, 0x00, 0xf3}

	result := cd.Deploy(creator, code, big.NewInt(0), 0)
	if !result.Success {
		t.Fatalf("deploy failed: %s", result.Error)
	}

	got := cd.GetDeployment(result.Address)
	if got == nil {
		t.Fatal("expected deployment to be found")
	}
	if got.Address != result.Address {
		t.Fatal("address mismatch")
	}

	// Non-existent address.
	unknown := cd.GetDeployment(types.HexToAddress("0xdead"))
	if unknown != nil {
		t.Fatal("expected nil for unknown address")
	}
}

func TestDeploymentsByCreator(t *testing.T) {
	cd := NewContractDeployer(DefaultDeployConfig())
	creator := types.HexToAddress("0x8888")
	code := []byte{0x60, 0x00, 0xf3}

	cd.Deploy(creator, code, big.NewInt(0), 0)
	cd.Deploy(creator, code, big.NewInt(0), 1)
	cd.Deploy(creator, code, big.NewInt(0), 2)

	results := cd.DeploymentsByCreator(creator)
	if len(results) != 3 {
		t.Fatalf("expected 3 deployments, got %d", len(results))
	}
	for _, r := range results {
		if r.Creator != creator {
			t.Fatalf("expected creator %s, got %s", creator, r.Creator)
		}
	}

	// Unknown creator returns nil.
	none := cd.DeploymentsByCreator(types.HexToAddress("0xbeef"))
	if none != nil {
		t.Fatal("expected nil for unknown creator")
	}
}

func TestComputeCreateAddress(t *testing.T) {
	// Well-known test vector: Vitalik's first contract creation address.
	// Creator: 0xd8dA6BF26964aF9D7eEd9e03E53415D37aA96045, nonce: 0
	// We just verify determinism and non-zero output here.
	creator := types.HexToAddress("0xd8dA6BF26964aF9D7eEd9e03E53415D37aA96045")
	addr0 := ComputeCreateAddress(creator, 0)
	addr1 := ComputeCreateAddress(creator, 1)

	if addr0.IsZero() || addr1.IsZero() {
		t.Fatal("expected non-zero addresses")
	}
	if addr0 == addr1 {
		t.Fatal("expected different addresses for different nonces")
	}

	// Determinism check: same inputs produce same output.
	addr0Again := ComputeCreateAddress(creator, 0)
	if addr0 != addr0Again {
		t.Fatal("ComputeCreateAddress is not deterministic")
	}
}

func TestComputeCreate2Address(t *testing.T) {
	creator := types.HexToAddress("0x0000000000000000000000000000000000000000")
	salt := types.Hash{}
	initCodeHash := crypto.Keccak256Hash([]byte{})

	addr := ComputeCreate2Address(creator, salt, initCodeHash)
	if addr.IsZero() {
		t.Fatal("expected non-zero address")
	}

	// Determinism.
	addr2 := ComputeCreate2Address(creator, salt, initCodeHash)
	if addr != addr2 {
		t.Fatal("ComputeCreate2Address is not deterministic")
	}

	// Different salt should produce different address.
	salt2 := types.HexToHash("0x01")
	addr3 := ComputeCreate2Address(creator, salt2, initCodeHash)
	if addr == addr3 {
		t.Fatal("expected different address for different salt")
	}
}

func TestComputeCreate2AddressKnownVector(t *testing.T) {
	// EIP-1014 test vector:
	// address: 0x0000000000000000000000000000000000000000
	// salt: 0x0000...0000
	// init_code: 0xdeadbeef
	// Expected: keccak256(0xff ++ address ++ salt ++ keccak256(init_code))[12:]
	creator := types.HexToAddress("0x0000000000000000000000000000000000000000")
	salt := types.Hash{}
	initCode := []byte{0xde, 0xad, 0xbe, 0xef}
	initCodeHash := crypto.Keccak256Hash(initCode)

	addr := ComputeCreate2Address(creator, salt, initCodeHash)

	// Manually compute expected address.
	data := make([]byte, 85)
	data[0] = 0xff
	copy(data[1:21], creator[:])
	copy(data[21:53], salt[:])
	copy(data[53:85], initCodeHash[:])
	expected := types.BytesToAddress(crypto.Keccak256(data)[12:])

	if addr != expected {
		t.Fatalf("CREATE2 address mismatch: got %s, want %s", addr, expected)
	}
}

func TestDeployerGasCost(t *testing.T) {
	cd := NewContractDeployer(DefaultDeployConfig())
	creator := types.HexToAddress("0x9999")
	code := make([]byte, 100)

	result := cd.Deploy(creator, code, big.NewInt(0), 0)
	if !result.Success {
		t.Fatalf("deploy failed: %s", result.Error)
	}

	// Expected gas: 32000 base + 100 * 200 = 52000
	expectedGas := uint64(32000 + 100*200)
	if result.GasUsed != expectedGas {
		t.Fatalf("gas mismatch: got %d, want %d", result.GasUsed, expectedGas)
	}
}

func TestDeployCreate2GasCost(t *testing.T) {
	cd := NewContractDeployer(DefaultDeployConfig())
	creator := types.HexToAddress("0xaaab")
	code := make([]byte, 64) // 2 words exactly

	result := cd.DeployCreate2(creator, code, types.Hash{})
	if !result.Success {
		t.Fatalf("deploy failed: %s", result.Error)
	}

	// Expected gas: 32000 base + 64*200 + (64/32)*6 = 32000 + 12800 + 12 = 44812
	expectedGas := uint64(32000 + 64*200 + 2*6)
	if result.GasUsed != expectedGas {
		t.Fatalf("gas mismatch: got %d, want %d", result.GasUsed, expectedGas)
	}
}

func TestDeployerConcurrency(t *testing.T) {
	cd := NewContractDeployer(DefaultDeployConfig())
	code := []byte{0x60, 0x00, 0xf3}

	var wg sync.WaitGroup
	const goroutines = 50

	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			// Each goroutine uses a unique creator to avoid collisions.
			var creator types.Address
			creator[0] = byte(n >> 8)
			creator[1] = byte(n)
			cd.Deploy(creator, code, big.NewInt(0), 0)
		}(i)
	}
	wg.Wait()

	if cd.DeploymentCount() != goroutines {
		t.Fatalf("expected %d deployments, got %d", goroutines, cd.DeploymentCount())
	}
}

func TestDeployCreate2GasLimitExceeded(t *testing.T) {
	config := DeployConfig{MaxCodeSize: 50000, InitGasLimit: 100}
	cd := NewContractDeployer(config)
	creator := types.HexToAddress("0xfeed")
	code := make([]byte, 10)

	result := cd.DeployCreate2(creator, code, types.Hash{})
	if result.Success {
		t.Fatal("expected failure for gas limit exceeded")
	}
	if result.Error != "gas limit exceeded" {
		t.Fatalf("unexpected error: %s", result.Error)
	}
}

func TestDefaultDeployConfig(t *testing.T) {
	config := DefaultDeployConfig()
	if config.MaxCodeSize != 24576 {
		t.Fatalf("expected MaxCodeSize 24576, got %d", config.MaxCodeSize)
	}
	if config.InitGasLimit != 10_000_000 {
		t.Fatalf("expected InitGasLimit 10000000, got %d", config.InitGasLimit)
	}
	if config.AllowSelfDestruct {
		t.Fatal("expected AllowSelfDestruct to be false")
	}
}
