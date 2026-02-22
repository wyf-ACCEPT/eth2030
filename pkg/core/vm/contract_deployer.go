package vm

import (
	"math/big"
	"sync"

	"github.com/eth2030/eth2030/core/types"
	"github.com/eth2030/eth2030/crypto"
	"github.com/eth2030/eth2030/rlp"
)

// DeployConfig holds configuration for the contract deployer.
type DeployConfig struct {
	// MaxCodeSize is the maximum allowed contract bytecode size in bytes.
	MaxCodeSize uint64
	// InitGasLimit is the maximum gas available for contract initialization.
	InitGasLimit uint64
	// AllowSelfDestruct controls whether SELFDESTRUCT is permitted.
	AllowSelfDestruct bool
}

// DefaultDeployConfig returns a DeployConfig with EIP-170 defaults.
func DefaultDeployConfig() DeployConfig {
	return DeployConfig{
		MaxCodeSize:       24576, // EIP-170: 24 KiB
		InitGasLimit:      10_000_000,
		AllowSelfDestruct: false,
	}
}

// DeployResult contains the outcome of a contract deployment.
type DeployResult struct {
	Address  types.Address
	CodeHash types.Hash
	GasUsed  uint64
	Success  bool
	Error    string
	Creator  types.Address
}

// ContractDeployer manages contract deployments and tracks deployed contracts.
type ContractDeployer struct {
	mu          sync.RWMutex
	config      DeployConfig
	deployments map[types.Address]*DeployResult
	byCreator   map[types.Address][]types.Address
}

// NewContractDeployer creates a new ContractDeployer with the given config.
func NewContractDeployer(config DeployConfig) *ContractDeployer {
	return &ContractDeployer{
		config:      config,
		deployments: make(map[types.Address]*DeployResult),
		byCreator:   make(map[types.Address][]types.Address),
	}
}

// Deploy deploys a contract using the CREATE opcode address derivation:
// address = keccak256(rlp([creator, nonce]))[12:]
func (cd *ContractDeployer) Deploy(creator types.Address, code []byte, value *big.Int, nonce uint64) *DeployResult {
	result := &DeployResult{Creator: creator}

	// Validate code size.
	if uint64(len(code)) > cd.config.MaxCodeSize {
		result.Error = "code size exceeds maximum"
		return result
	}

	if len(code) == 0 {
		result.Error = "empty code"
		return result
	}

	// Compute the CREATE address.
	addr := ComputeCreateAddress(creator, nonce)
	result.Address = addr

	// Check for address collision.
	cd.mu.RLock()
	_, exists := cd.deployments[addr]
	cd.mu.RUnlock()
	if exists {
		result.Error = "address collision"
		return result
	}

	// Compute code hash and gas cost.
	codeHash := crypto.Keccak256Hash(code)
	result.CodeHash = codeHash

	// Gas cost: 200 per byte (EIP-3860 initcode metering) + 32000 base.
	gasUsed := uint64(32000) + uint64(len(code))*200
	if gasUsed > cd.config.InitGasLimit {
		result.Error = "gas limit exceeded"
		result.GasUsed = gasUsed
		return result
	}
	result.GasUsed = gasUsed
	result.Success = true

	// Record deployment.
	cd.mu.Lock()
	cd.deployments[addr] = result
	cd.byCreator[creator] = append(cd.byCreator[creator], addr)
	cd.mu.Unlock()

	return result
}

// DeployCreate2 deploys a contract using the CREATE2 address derivation:
// address = keccak256(0xff ++ creator ++ salt ++ keccak256(code))[12:]
func (cd *ContractDeployer) DeployCreate2(creator types.Address, code []byte, salt types.Hash) *DeployResult {
	result := &DeployResult{Creator: creator}

	// Validate code size.
	if uint64(len(code)) > cd.config.MaxCodeSize {
		result.Error = "code size exceeds maximum"
		return result
	}

	if len(code) == 0 {
		result.Error = "empty code"
		return result
	}

	// Compute the CREATE2 address.
	initCodeHash := crypto.Keccak256Hash(code)
	addr := ComputeCreate2Address(creator, salt, initCodeHash)
	result.Address = addr

	// Check for address collision.
	cd.mu.RLock()
	_, exists := cd.deployments[addr]
	cd.mu.RUnlock()
	if exists {
		result.Error = "address collision"
		return result
	}

	// Compute code hash and gas cost.
	result.CodeHash = initCodeHash

	// Gas cost: 200 per byte (EIP-3860) + 32000 base + 6 per word for hashing.
	words := (uint64(len(code)) + 31) / 32
	gasUsed := uint64(32000) + uint64(len(code))*200 + words*6
	if gasUsed > cd.config.InitGasLimit {
		result.Error = "gas limit exceeded"
		result.GasUsed = gasUsed
		return result
	}
	result.GasUsed = gasUsed
	result.Success = true

	// Record deployment.
	cd.mu.Lock()
	cd.deployments[addr] = result
	cd.byCreator[creator] = append(cd.byCreator[creator], addr)
	cd.mu.Unlock()

	return result
}

// GetDeployment returns the deployment result for an address, or nil if not found.
func (cd *ContractDeployer) GetDeployment(addr types.Address) *DeployResult {
	cd.mu.RLock()
	defer cd.mu.RUnlock()
	return cd.deployments[addr]
}

// DeploymentCount returns the total number of successful deployments.
func (cd *ContractDeployer) DeploymentCount() int {
	cd.mu.RLock()
	defer cd.mu.RUnlock()
	return len(cd.deployments)
}

// DeploymentsByCreator returns all deployment results for a given creator address.
func (cd *ContractDeployer) DeploymentsByCreator(creator types.Address) []DeployResult {
	cd.mu.RLock()
	defer cd.mu.RUnlock()

	addrs := cd.byCreator[creator]
	if len(addrs) == 0 {
		return nil
	}

	results := make([]DeployResult, 0, len(addrs))
	for _, addr := range addrs {
		if d := cd.deployments[addr]; d != nil {
			results = append(results, *d)
		}
	}
	return results
}

// ComputeCreateAddress computes the contract address for a CREATE deployment.
// The address is derived as: keccak256(rlp([sender, nonce]))[12:]
func ComputeCreateAddress(creator types.Address, nonce uint64) types.Address {
	// RLP encode [address, nonce] as a list.
	data, _ := rlp.EncodeToBytes([]interface{}{creator, nonce})
	hash := crypto.Keccak256(data)
	return types.BytesToAddress(hash[12:])
}

// ComputeCreate2Address computes the contract address for a CREATE2 deployment.
// The address is derived as: keccak256(0xff ++ sender ++ salt ++ keccak256(initCode))[12:]
func ComputeCreate2Address(creator types.Address, salt types.Hash, initCodeHash types.Hash) types.Address {
	// 1 byte prefix + 20 byte address + 32 byte salt + 32 byte hash = 85 bytes
	data := make([]byte, 1+types.AddressLength+types.HashLength+types.HashLength)
	data[0] = 0xff
	copy(data[1:], creator[:])
	copy(data[1+types.AddressLength:], salt[:])
	copy(data[1+types.AddressLength+types.HashLength:], initCodeHash[:])
	hash := crypto.Keccak256(data)
	return types.BytesToAddress(hash[12:])
}
