package vm

import (
	"errors"
	"math/big"

	"github.com/eth2028/eth2028/core/types"
	"github.com/eth2028/eth2028/crypto"
)

var (
	ErrNotImplemented        = errors.New("not implemented")
	ErrOutOfGas              = errors.New("out of gas")
	ErrStackOverflow         = errors.New("stack overflow")
	ErrStackUnderflow        = errors.New("stack underflow")
	ErrInvalidJump           = errors.New("invalid jump destination")
	ErrWriteProtection       = errors.New("write protection")
	ErrExecutionReverted     = errors.New("execution reverted")
	ErrMaxCallDepthExceeded  = errors.New("max call depth exceeded")
	ErrInvalidOpCode         = errors.New("invalid opcode")
	ErrReturnDataOutOfBounds = errors.New("return data out of bounds")
	ErrMaxInitCodeSizeExceeded = errors.New("max initcode size exceeded")
)

// GetHashFunc returns the hash of the block with the given number.
type GetHashFunc func(uint64) types.Hash

// BlockContext provides the EVM with block-level information.
type BlockContext struct {
	GetHash     GetHashFunc
	BlockNumber *big.Int
	Time        uint64
	Coinbase    types.Address
	GasLimit    uint64
	BaseFee     *big.Int
	PrevRandao  types.Hash
	BlobBaseFee *big.Int
}

// TxContext provides the EVM with transaction-level information.
type TxContext struct {
	Origin     types.Address
	GasPrice   *big.Int
	BlobHashes []types.Hash
}

// StateDB provides the EVM with access to Ethereum world state.
// This interface is defined in the vm package to avoid circular imports
// with core/state. Any implementation of core/state.StateDB satisfies it.
type StateDB interface {
	// Account operations
	CreateAccount(addr types.Address)
	GetBalance(addr types.Address) *big.Int
	AddBalance(addr types.Address, amount *big.Int)
	SubBalance(addr types.Address, amount *big.Int)
	GetNonce(addr types.Address) uint64
	SetNonce(addr types.Address, nonce uint64)
	GetCode(addr types.Address) []byte
	SetCode(addr types.Address, code []byte)
	GetCodeHash(addr types.Address) types.Hash

	GetCodeSize(addr types.Address) int

	// Storage
	GetState(addr types.Address, key types.Hash) types.Hash
	SetState(addr types.Address, key types.Hash, value types.Hash)
	GetCommittedState(addr types.Address, key types.Hash) types.Hash

	// Transient storage (EIP-1153)
	GetTransientState(addr types.Address, key types.Hash) types.Hash
	SetTransientState(addr types.Address, key types.Hash, value types.Hash)
	ClearTransientStorage()

	// Self-destruct
	SelfDestruct(addr types.Address)
	HasSelfDestructed(addr types.Address) bool

	// Account existence
	Exist(addr types.Address) bool
	Empty(addr types.Address) bool

	// Snapshot and revert
	Snapshot() int
	RevertToSnapshot(id int)

	// Logs
	AddLog(log *types.Log)

	// Refund counter (EIP-3529)
	AddRefund(gas uint64)
	SubRefund(gas uint64)
	GetRefund() uint64

	// Access list (EIP-2929 warm/cold tracking)
	AddAddressToAccessList(addr types.Address)
	AddSlotToAccessList(addr types.Address, slot types.Hash)
	AddressInAccessList(addr types.Address) bool
	SlotInAccessList(addr types.Address, slot types.Hash) (addressOk bool, slotOk bool)
}

// Config holds EVM configuration options.
type Config struct {
	Debug        bool
	Tracer       interface{}
	MaxCallDepth int
}

// EVM is the Ethereum Virtual Machine execution environment.
type EVM struct {
	Context     BlockContext
	TxContext   TxContext
	Config      Config
	StateDB     StateDB
	chainID     uint64
	depth       int
	readOnly    bool
	jumpTable   JumpTable
	precompiles map[types.Address]PrecompiledContract
	returnData  []byte // return data from the last CALL/CREATE
}

// NewEVM creates a new EVM instance.
func NewEVM(blockCtx BlockContext, txCtx TxContext, config Config) *EVM {
	if config.MaxCallDepth == 0 {
		config.MaxCallDepth = 1024
	}
	return &EVM{
		Context:   blockCtx,
		TxContext: txCtx,
		Config:    config,
		jumpTable: NewCancunJumpTable(),
	}
}

// NewEVMWithState creates a new EVM instance with state access.
func NewEVMWithState(blockCtx BlockContext, txCtx TxContext, config Config, stateDB StateDB) *EVM {
	evm := NewEVM(blockCtx, txCtx, config)
	evm.StateDB = stateDB
	return evm
}

// SetJumpTable replaces the EVM's jump table. Use SelectJumpTable to pick
// the correct table for a given fork.
func (evm *EVM) SetJumpTable(jt JumpTable) {
	evm.jumpTable = jt
}

// SetPrecompiles replaces the EVM's precompile map.
func (evm *EVM) SetPrecompiles(p map[types.Address]PrecompiledContract) {
	evm.precompiles = p
}

// precompile returns the precompiled contract at addr, falling back to the
// default Cancun precompile set if no custom map has been set.
func (evm *EVM) precompile(addr types.Address) (PrecompiledContract, bool) {
	m := evm.precompiles
	if m == nil {
		m = PrecompiledContractsCancun
	}
	p, ok := m[addr]
	return p, ok
}

// runPrecompile executes a precompiled contract and returns the output,
// remaining gas, and any error.
func runPrecompile(p PrecompiledContract, input []byte, gas uint64) ([]byte, uint64, error) {
	gasCost := p.RequiredGas(input)
	if gas < gasCost {
		return nil, 0, ErrOutOfGas
	}
	output, err := p.Run(input)
	return output, gas - gasCost, err
}

// ForkRules mirrors the chain configuration fork flags needed to select
// the correct jump table. The caller (processor) converts ChainConfig.Rules
// into this struct to avoid a circular import.
type ForkRules struct {
	IsGlamsterdan    bool
	IsPrague         bool
	IsCancun         bool
	IsShanghai       bool
	IsMerge          bool
	IsLondon         bool
	IsBerlin         bool
	IsIstanbul       bool
	IsConstantinople bool
	IsByzantium      bool
	IsHomestead      bool
}

// SelectPrecompiles returns the correct precompile map for the given fork rules.
func SelectPrecompiles(rules ForkRules) map[types.Address]PrecompiledContract {
	if rules.IsGlamsterdan {
		return PrecompiledContractsGlamsterdan
	}
	return PrecompiledContractsCancun
}

// SelectJumpTable returns the correct jump table for the given fork rules.
func SelectJumpTable(rules ForkRules) JumpTable {
	switch {
	case rules.IsGlamsterdan:
		return NewGlamsterdanJumpTable()
	case rules.IsPrague:
		return NewPragueJumpTable()
	case rules.IsCancun:
		return NewCancunJumpTable()
	case rules.IsShanghai:
		return NewShanghaiJumpTable()
	case rules.IsMerge:
		return NewMergeJumpTable()
	case rules.IsLondon:
		return NewLondonJumpTable()
	case rules.IsBerlin:
		return NewBerlinJumpTable()
	case rules.IsIstanbul:
		return NewIstanbulJumpTable()
	case rules.IsConstantinople:
		return NewConstantinopleJumpTable()
	case rules.IsByzantium:
		return NewByzantiumJumpTable()
	case rules.IsHomestead:
		return NewHomesteadJumpTable()
	default:
		return NewFrontierJumpTable()
	}
}

// Run executes the contract bytecode using the interpreter loop.
func (evm *EVM) Run(contract *Contract, input []byte) ([]byte, error) {
	contract.Input = input

	var (
		pc    uint64
		stack = NewStack()
		mem   = NewMemory()
	)

	for {
		op := contract.GetOp(pc)
		operation := evm.jumpTable[op]
		if operation == nil || operation.execute == nil {
			return nil, ErrInvalidOpCode
		}

		// Stack validation
		sLen := stack.Len()
		if sLen < operation.minStack {
			return nil, ErrStackUnderflow
		}
		if sLen > operation.maxStack {
			return nil, ErrStackOverflow
		}

		// Constant gas deduction
		if operation.constantGas > 0 {
			if !contract.UseGas(operation.constantGas) {
				return nil, ErrOutOfGas
			}
		}

		// Memory expansion (if operation defines memorySize)
		var memSize uint64
		if operation.memorySize != nil {
			memSize = operation.memorySize(stack)
			if memSize > 0 {
				// Check for overflow in word rounding.
				if memSize > (^uint64(0))-31 {
					return nil, ErrOutOfGas
				}
				words := (memSize + 31) / 32
				newSize := words * 32
				if uint64(mem.Len()) < newSize {
					// Charge memory expansion gas before resizing.
					expansionCost, ok := MemoryCost(uint64(mem.Len()), newSize)
					if !ok {
						return nil, ErrOutOfGas
					}
					if !contract.UseGas(expansionCost) {
						return nil, ErrOutOfGas
					}
					mem.Resize(newSize)
				}
			}
		}

		// Dynamic gas (EIP-2929 warm/cold checks, memory expansion, etc.)
		if operation.dynamicGas != nil {
			cost := operation.dynamicGas(evm, contract, stack, mem, memSize)
			if !contract.UseGas(cost) {
				return nil, ErrOutOfGas
			}
		}

		// Execute the opcode
		ret, err := operation.execute(&pc, evm, contract, mem, stack)

		if err != nil {
			if errors.Is(err, ErrExecutionReverted) {
				return ret, err
			}
			return nil, err
		}

		// Handle halting opcodes
		if operation.halts {
			return ret, nil
		}
		if operation.jumps {
			continue
		}

		pc++
	}
}

// Call executes a message call to the given address with the given input, gas, and value.
func (evm *EVM) Call(caller types.Address, addr types.Address, input []byte, gas uint64, value *big.Int) ([]byte, uint64, error) {
	if evm.depth > evm.Config.MaxCallDepth {
		return nil, gas, ErrMaxCallDepthExceeded
	}

	// Check for precompiled contract
	if p, ok := evm.precompile(addr); ok {
		ret, gasLeft, err := runPrecompile(p, input, gas)
		return ret, gasLeft, err
	}

	if evm.StateDB == nil {
		return nil, gas, errors.New("no state database")
	}

	// Snapshot state for revert on failure
	snapshot := evm.StateDB.Snapshot()

	// Create account if it doesn't exist
	if !evm.StateDB.Exist(addr) {
		evm.StateDB.CreateAccount(addr)
	}

	// Transfer value
	if value != nil && value.Sign() > 0 {
		if evm.readOnly {
			return nil, gas, ErrWriteProtection
		}
		callerBalance := evm.StateDB.GetBalance(caller)
		if callerBalance.Cmp(value) < 0 {
			return nil, gas, errors.New("insufficient balance for transfer")
		}
		evm.StateDB.SubBalance(caller, value)
		evm.StateDB.AddBalance(addr, value)
	}

	// Get the code to execute
	code := evm.StateDB.GetCode(addr)
	if len(code) == 0 {
		// No code to execute, the call succeeds with no return data
		return nil, gas, nil
	}

	// Create the contract for execution
	contract := NewContract(caller, addr, value, gas)
	contract.Code = code
	contract.CodeHash = evm.StateDB.GetCodeHash(addr)

	// Execute
	evm.depth++
	ret, err := evm.Run(contract, input)
	evm.depth--

	gasLeft := contract.Gas

	if err != nil && !errors.Is(err, ErrExecutionReverted) {
		// On error (not revert), revert state changes and consume all gas
		evm.StateDB.RevertToSnapshot(snapshot)
		gasLeft = 0
	} else if errors.Is(err, ErrExecutionReverted) {
		// On revert, revert state changes but return remaining gas
		evm.StateDB.RevertToSnapshot(snapshot)
	}

	return ret, gasLeft, err
}

// CallCode executes a CALLCODE operation. Runs the callee's code in the caller's context.
func (evm *EVM) CallCode(caller types.Address, addr types.Address, input []byte, gas uint64, value *big.Int) ([]byte, uint64, error) {
	if evm.depth > evm.Config.MaxCallDepth {
		return nil, gas, ErrMaxCallDepthExceeded
	}

	if p, ok := evm.precompile(addr); ok {
		ret, gasLeft, err := runPrecompile(p, input, gas)
		return ret, gasLeft, err
	}

	if evm.StateDB == nil {
		return nil, gas, errors.New("no state database")
	}

	snapshot := evm.StateDB.Snapshot()

	// Get the code to execute from the target address
	code := evm.StateDB.GetCode(addr)
	if len(code) == 0 {
		return nil, gas, nil
	}

	// CALLCODE executes the callee's code but in the caller's context
	// (caller's address is used for storage and msg.sender)
	contract := NewContract(caller, caller, value, gas)
	contract.Code = code
	contract.CodeHash = evm.StateDB.GetCodeHash(addr)

	evm.depth++
	ret, err := evm.Run(contract, input)
	evm.depth--

	gasLeft := contract.Gas

	if err != nil && !errors.Is(err, ErrExecutionReverted) {
		evm.StateDB.RevertToSnapshot(snapshot)
		gasLeft = 0
	} else if errors.Is(err, ErrExecutionReverted) {
		evm.StateDB.RevertToSnapshot(snapshot)
	}

	return ret, gasLeft, err
}

// DelegateCall executes a DELEGATECALL operation.
// Like CALLCODE but preserves the original caller and value.
func (evm *EVM) DelegateCall(caller types.Address, addr types.Address, input []byte, gas uint64) ([]byte, uint64, error) {
	if evm.depth > evm.Config.MaxCallDepth {
		return nil, gas, ErrMaxCallDepthExceeded
	}

	if p, ok := evm.precompile(addr); ok {
		ret, gasLeft, err := runPrecompile(p, input, gas)
		return ret, gasLeft, err
	}

	if evm.StateDB == nil {
		return nil, gas, errors.New("no state database")
	}

	snapshot := evm.StateDB.Snapshot()

	code := evm.StateDB.GetCode(addr)
	if len(code) == 0 {
		return nil, gas, nil
	}

	// DELEGATECALL preserves the caller (msg.sender) and value from the current context.
	// Storage operations happen on the caller's storage, not the callee's.
	contract := NewContract(caller, caller, nil, gas)
	contract.Code = code
	contract.CodeHash = evm.StateDB.GetCodeHash(addr)

	evm.depth++
	ret, err := evm.Run(contract, input)
	evm.depth--

	gasLeft := contract.Gas

	if err != nil && !errors.Is(err, ErrExecutionReverted) {
		evm.StateDB.RevertToSnapshot(snapshot)
		gasLeft = 0
	} else if errors.Is(err, ErrExecutionReverted) {
		evm.StateDB.RevertToSnapshot(snapshot)
	}

	return ret, gasLeft, err
}

// StaticCall executes a read-only message call. Any state modifications will cause an error.
func (evm *EVM) StaticCall(caller types.Address, addr types.Address, input []byte, gas uint64) ([]byte, uint64, error) {
	if evm.depth > evm.Config.MaxCallDepth {
		return nil, gas, ErrMaxCallDepthExceeded
	}

	if p, ok := evm.precompile(addr); ok {
		ret, gasLeft, err := runPrecompile(p, input, gas)
		return ret, gasLeft, err
	}

	if evm.StateDB == nil {
		return nil, gas, errors.New("no state database")
	}

	snapshot := evm.StateDB.Snapshot()

	code := evm.StateDB.GetCode(addr)
	if len(code) == 0 {
		return nil, gas, nil
	}

	contract := NewContract(caller, addr, new(big.Int), gas)
	contract.Code = code
	contract.CodeHash = evm.StateDB.GetCodeHash(addr)

	// Set readOnly mode for the duration of this call
	prevReadOnly := evm.readOnly
	evm.readOnly = true

	evm.depth++
	ret, err := evm.Run(contract, input)
	evm.depth--

	evm.readOnly = prevReadOnly

	gasLeft := contract.Gas

	if err != nil && !errors.Is(err, ErrExecutionReverted) {
		evm.StateDB.RevertToSnapshot(snapshot)
		gasLeft = 0
	} else if errors.Is(err, ErrExecutionReverted) {
		evm.StateDB.RevertToSnapshot(snapshot)
	}

	return ret, gasLeft, err
}

// createAddress computes the address of a contract created with CREATE.
// Per the Yellow Paper: addr = keccak256(rlp([sender, nonce]))[12:]
func createAddress(caller types.Address, nonce uint64) types.Address {
	// RLP-encode the list [sender_address, nonce].
	// sender_address is a 20-byte string, nonce is an integer.
	addrEnc := encodeRLPBytes(caller[:])
	nonceEnc := encodeRLPUint(nonce)

	// Wrap both items in an RLP list.
	payload := append(addrEnc, nonceEnc...)
	data := wrapRLPList(payload)

	hash := crypto.Keccak256(data)
	return types.BytesToAddress(hash[12:])
}

// encodeRLPBytes encodes a byte slice as an RLP string.
func encodeRLPBytes(b []byte) []byte {
	if len(b) == 1 && b[0] < 0x80 {
		return b
	}
	if len(b) < 56 {
		return append([]byte{byte(0x80 + len(b))}, b...)
	}
	lenBytes := uintToMinBytes(uint64(len(b)))
	header := append([]byte{byte(0xb7 + len(lenBytes))}, lenBytes...)
	return append(header, b...)
}

// encodeRLPUint encodes a uint64 as an RLP integer.
func encodeRLPUint(v uint64) []byte {
	if v == 0 {
		return []byte{0x80}
	}
	if v < 128 {
		return []byte{byte(v)}
	}
	b := uintToMinBytes(v)
	return append([]byte{byte(0x80 + len(b))}, b...)
}

// wrapRLPList wraps payload bytes in an RLP list header.
func wrapRLPList(payload []byte) []byte {
	if len(payload) < 56 {
		return append([]byte{byte(0xc0 + len(payload))}, payload...)
	}
	lenBytes := uintToMinBytes(uint64(len(payload)))
	header := append([]byte{byte(0xf7 + len(lenBytes))}, lenBytes...)
	return append(header, payload...)
}

// uintToMinBytes encodes a uint64 as big-endian bytes with no leading zeros.
func uintToMinBytes(v uint64) []byte {
	if v == 0 {
		return nil
	}
	var buf [8]byte
	n := 0
	for i := 7; i >= 0; i-- {
		buf[i] = byte(v)
		v >>= 8
		if buf[i] != 0 || n > 0 {
			n = 8 - i
		}
	}
	return buf[8-n:]
}

// create2Address computes the address of a contract created with CREATE2.
func create2Address(caller types.Address, salt *big.Int, initCodeHash []byte) types.Address {
	// CREATE2 address = keccak256(0xff + caller + salt + keccak256(initCode))[12:]
	saltBytes := make([]byte, 32)
	if salt != nil {
		b := salt.Bytes()
		copy(saltBytes[32-len(b):], b)
	}
	data := make([]byte, 0, 85)
	data = append(data, 0xff)
	data = append(data, caller[:]...)
	data = append(data, saltBytes...)
	data = append(data, initCodeHash...)
	hash := crypto.Keccak256(data)
	return types.BytesToAddress(hash[12:])
}

// Create creates a new contract with the given code.
func (evm *EVM) Create(caller types.Address, code []byte, gas uint64, value *big.Int) ([]byte, types.Address, uint64, error) {
	if evm.depth > evm.Config.MaxCallDepth {
		return nil, types.Address{}, gas, ErrMaxCallDepthExceeded
	}
	if evm.readOnly {
		return nil, types.Address{}, gas, ErrWriteProtection
	}
	if evm.StateDB == nil {
		return nil, types.Address{}, gas, errors.New("no state database")
	}

	// Compute the new contract address
	nonce := evm.StateDB.GetNonce(caller)
	evm.StateDB.SetNonce(caller, nonce+1)
	contractAddr := createAddress(caller, nonce)

	return evm.create(caller, code, gas, value, contractAddr)
}

// Create2 creates a new contract using CREATE2 with the given salt.
func (evm *EVM) Create2(caller types.Address, code []byte, gas uint64, endowment *big.Int, salt *big.Int) ([]byte, types.Address, uint64, error) {
	if evm.depth > evm.Config.MaxCallDepth {
		return nil, types.Address{}, gas, ErrMaxCallDepthExceeded
	}
	if evm.readOnly {
		return nil, types.Address{}, gas, ErrWriteProtection
	}
	if evm.StateDB == nil {
		return nil, types.Address{}, gas, errors.New("no state database")
	}

	initCodeHash := crypto.Keccak256(code)
	contractAddr := create2Address(caller, salt, initCodeHash)

	return evm.create(caller, code, gas, endowment, contractAddr)
}

// PreWarmAccessList pre-warms the access list with the sender, recipient, and
// all precompile addresses (0x01-0x0a) per EIP-2929.
func (evm *EVM) PreWarmAccessList(sender types.Address, to *types.Address) {
	if evm.StateDB == nil {
		return
	}
	// Warm the sender.
	evm.StateDB.AddAddressToAccessList(sender)
	// Warm the recipient (if non-nil, i.e. not a contract creation).
	if to != nil {
		evm.StateDB.AddAddressToAccessList(*to)
	}
	// Warm all precompile addresses (0x01 through 0x13).
	// Includes: ecrecover(1), sha256(2), ripemd160(3), identity(4),
	// modexp(5), bn254add(6), bn254mul(7), bn254pairing(8),
	// blake2f(9), kzg(10), and EIP-2537 BLS12-381 (11-19).
	for i := 1; i <= 0x13; i++ {
		evm.StateDB.AddAddressToAccessList(types.BytesToAddress([]byte{byte(i)}))
	}
}

// gasEIP2929AccountCheck checks whether addr is warm. If cold, it warms the
// address and returns the extra cold gas (ColdAccountAccessCost - WarmStorageReadCost).
// If warm, it returns 0. The caller is expected to charge WarmStorageReadCost
// as the constant gas.
func gasEIP2929AccountCheck(evm *EVM, addr types.Address) uint64 {
	if evm.StateDB == nil {
		return 0
	}
	if evm.StateDB.AddressInAccessList(addr) {
		return 0
	}
	evm.StateDB.AddAddressToAccessList(addr)
	return ColdAccountAccessCost - WarmStorageReadCost
}

// gasEIP2929SlotCheck checks whether (addr, slot) is warm. If cold, it warms
// the slot and returns the extra cold gas (ColdSloadCost - WarmStorageReadCost).
// If warm, it returns 0. The caller is expected to charge WarmStorageReadCost
// as the constant gas.
func gasEIP2929SlotCheck(evm *EVM, addr types.Address, slot types.Hash) uint64 {
	if evm.StateDB == nil {
		return 0
	}
	_, slotWarm := evm.StateDB.SlotInAccessList(addr, slot)
	if slotWarm {
		return 0
	}
	evm.StateDB.AddSlotToAccessList(addr, slot)
	return ColdSloadCost - WarmStorageReadCost
}

// create is the shared implementation for Create and Create2.
func (evm *EVM) create(caller types.Address, code []byte, gas uint64, value *big.Int, contractAddr types.Address) ([]byte, types.Address, uint64, error) {
	// EIP-3860: max init code size check.
	if len(code) > MaxInitCodeSize {
		return nil, types.Address{}, gas, ErrMaxInitCodeSizeExceeded
	}

	snapshot := evm.StateDB.Snapshot()

	// Create the account
	evm.StateDB.CreateAccount(contractAddr)

	// Transfer value
	if value != nil && value.Sign() > 0 {
		callerBalance := evm.StateDB.GetBalance(caller)
		if callerBalance.Cmp(value) < 0 {
			return nil, types.Address{}, gas, errors.New("insufficient balance for transfer")
		}
		evm.StateDB.SubBalance(caller, value)
		evm.StateDB.AddBalance(contractAddr, value)
	}

	// Deduct CREATE base gas.
	if gas < GasCreate {
		return nil, types.Address{}, 0, ErrOutOfGas
	}
	gas -= GasCreate

	// EIP-3860: charge initcode word gas (2 per 32-byte word).
	if len(code) > 0 {
		words := toWordSize(uint64(len(code)))
		initCodeGas := InitCodeWordGas * words
		if gas < initCodeGas {
			return nil, types.Address{}, 0, ErrOutOfGas
		}
		gas -= initCodeGas
	}

	// Apply the 63/64 rule (EIP-150) to gas available for init code.
	callGas := gas - gas/CallGasFraction
	gas -= callGas

	// Create the contract for init code execution
	contract := NewContract(caller, contractAddr, value, callGas)
	contract.Code = code

	// Execute init code
	evm.depth++
	ret, err := evm.Run(contract, nil)
	evm.depth--

	// Return unused gas from subcall.
	gas += contract.Gas

	if err != nil {
		// On any error, revert state and return
		evm.StateDB.RevertToSnapshot(snapshot)
		if !errors.Is(err, ErrExecutionReverted) {
			// On non-revert error, all gas sent to subcall is consumed.
			gas += 0 // contract.Gas already added above, but subcall failed
		}
		return ret, types.Address{}, gas, err
	}

	// Code deposit cost: 200 per byte of deployed code.
	if len(ret) > 0 {
		// EIP-170: max contract code size.
		if len(ret) > MaxCodeSize {
			evm.StateDB.RevertToSnapshot(snapshot)
			return nil, types.Address{}, 0, errors.New("max code size exceeded")
		}
		depositCost := uint64(len(ret)) * CreateDataGas
		if gas < depositCost {
			evm.StateDB.RevertToSnapshot(snapshot)
			return nil, types.Address{}, 0, ErrOutOfGas
		}
		gas -= depositCost
		evm.StateDB.SetCode(contractAddr, ret)
	}

	return ret, contractAddr, gas, nil
}
