package rollup

import (
	"errors"
	"math/big"
	"sync"

	"github.com/eth2028/eth2028/core/types"
	"github.com/eth2028/eth2028/crypto"
)

// Deposit and withdrawal status constants.
const (
	StatusPending   = 0
	StatusConfirmed = 1
	StatusFinalized = 2
	StatusProven    = 3
)

// Bridge errors.
var (
	ErrDepositZeroAmount    = errors.New("bridge: deposit amount must be positive")
	ErrWithdrawalZeroAmount = errors.New("bridge: withdrawal amount must be positive")
	ErrMaxPendingDeposits   = errors.New("bridge: maximum pending deposits reached")
	ErrWithdrawalNotFound   = errors.New("bridge: withdrawal not found")
	ErrWithdrawalNotProven  = errors.New("bridge: withdrawal not proven")
	ErrWithdrawalAlready    = errors.New("bridge: withdrawal already finalized")
	ErrDepositNotFound      = errors.New("bridge: deposit not found")
	ErrProofEmpty           = errors.New("bridge: proof data is empty")
)

// BridgeConfig controls the L1-L2 bridge behavior.
type BridgeConfig struct {
	// L1ContractAddr is the bridge contract address on L1.
	L1ContractAddr types.Address

	// L2ContractAddr is the bridge contract address on L2.
	L2ContractAddr types.Address

	// ConfirmationBlocks is the number of L1 blocks required to confirm a deposit.
	ConfirmationBlocks uint64

	// MaxPendingDeposits is the maximum number of unconfirmed deposits allowed.
	MaxPendingDeposits int
}

// DefaultBridgeConfig returns a BridgeConfig with sensible defaults.
func DefaultBridgeConfig() BridgeConfig {
	return BridgeConfig{
		ConfirmationBlocks: 64,
		MaxPendingDeposits: 256,
	}
}

// BridgeDeposit represents an L1->L2 deposit.
type BridgeDeposit struct {
	// ID is the unique identifier for this deposit.
	ID types.Hash

	// From is the sender address on L1.
	From types.Address

	// To is the recipient address on L2.
	To types.Address

	// Amount is the deposit value in wei.
	Amount *big.Int

	// L1Block is the L1 block number at which the deposit was initiated.
	L1Block uint64

	// Status tracks the deposit lifecycle.
	Status int
}

// BridgeWithdrawal represents an L2->L1 withdrawal.
type BridgeWithdrawal struct {
	// ID is the unique identifier for this withdrawal.
	ID types.Hash

	// From is the sender address on L2.
	From types.Address

	// To is the recipient address on L1.
	To types.Address

	// Amount is the withdrawal value in wei.
	Amount *big.Int

	// ProofData holds the submitted withdrawal proof.
	ProofData []byte

	// Status tracks the withdrawal lifecycle.
	Status int
}

// Bridge manages L1<->L2 deposits and withdrawals for a native rollup.
type Bridge struct {
	mu          sync.Mutex
	config      BridgeConfig
	deposits    map[types.Hash]*BridgeDeposit
	withdrawals map[types.Hash]*BridgeWithdrawal
	depositSeq  uint64
	withdrawSeq uint64
}

// NewBridge creates a new Bridge with the given configuration.
func NewBridge(config BridgeConfig) *Bridge {
	return &Bridge{
		config:      config,
		deposits:    make(map[types.Hash]*BridgeDeposit),
		withdrawals: make(map[types.Hash]*BridgeWithdrawal),
	}
}

// Deposit initiates an L1->L2 deposit.
func (b *Bridge) Deposit(from, to types.Address, amount *big.Int, l1Block uint64) (*BridgeDeposit, error) {
	if amount == nil || amount.Sign() <= 0 {
		return nil, ErrDepositZeroAmount
	}

	b.mu.Lock()
	defer b.mu.Unlock()

	// Check pending deposit limit.
	pending := 0
	for _, d := range b.deposits {
		if d.Status == StatusPending {
			pending++
		}
	}
	if pending >= b.config.MaxPendingDeposits {
		return nil, ErrMaxPendingDeposits
	}

	b.depositSeq++
	id := computeDepositID(from, to, amount, l1Block, b.depositSeq)

	dep := &BridgeDeposit{
		ID:      id,
		From:    from,
		To:      to,
		Amount:  new(big.Int).Set(amount),
		L1Block: l1Block,
		Status:  StatusPending,
	}
	b.deposits[id] = dep

	return dep, nil
}

// ConfirmDeposits confirms all pending deposits that have enough L1 confirmations.
// Returns the number of deposits confirmed.
func (b *Bridge) ConfirmDeposits(l1Block uint64) int {
	b.mu.Lock()
	defer b.mu.Unlock()

	confirmed := 0
	for _, d := range b.deposits {
		if d.Status == StatusPending && l1Block >= d.L1Block+b.config.ConfirmationBlocks {
			d.Status = StatusConfirmed
			confirmed++
		}
	}
	return confirmed
}

// InitiateWithdrawal starts an L2->L1 withdrawal.
func (b *Bridge) InitiateWithdrawal(from, to types.Address, amount *big.Int) (*BridgeWithdrawal, error) {
	if amount == nil || amount.Sign() <= 0 {
		return nil, ErrWithdrawalZeroAmount
	}

	b.mu.Lock()
	defer b.mu.Unlock()

	b.withdrawSeq++
	id := computeWithdrawalID(from, to, amount, b.withdrawSeq)

	w := &BridgeWithdrawal{
		ID:     id,
		From:   from,
		To:     to,
		Amount: new(big.Int).Set(amount),
		Status: StatusPending,
	}
	b.withdrawals[id] = w

	return w, nil
}

// ProveWithdrawal submits a proof for a pending withdrawal.
func (b *Bridge) ProveWithdrawal(id types.Hash, proofData []byte) error {
	if len(proofData) == 0 {
		return ErrProofEmpty
	}

	b.mu.Lock()
	defer b.mu.Unlock()

	w, ok := b.withdrawals[id]
	if !ok {
		return ErrWithdrawalNotFound
	}
	if w.Status == StatusFinalized {
		return ErrWithdrawalAlready
	}

	proof := make([]byte, len(proofData))
	copy(proof, proofData)
	w.ProofData = proof
	w.Status = StatusProven

	return nil
}

// FinalizeWithdrawal finalizes a proven withdrawal for L1 release.
func (b *Bridge) FinalizeWithdrawal(id types.Hash) error {
	b.mu.Lock()
	defer b.mu.Unlock()

	w, ok := b.withdrawals[id]
	if !ok {
		return ErrWithdrawalNotFound
	}
	if w.Status != StatusProven {
		return ErrWithdrawalNotProven
	}

	w.Status = StatusFinalized
	return nil
}

// PendingDeposits returns all deposits with Pending status.
func (b *Bridge) PendingDeposits() []*BridgeDeposit {
	b.mu.Lock()
	defer b.mu.Unlock()

	var result []*BridgeDeposit
	for _, d := range b.deposits {
		if d.Status == StatusPending {
			result = append(result, d)
		}
	}
	return result
}

// PendingWithdrawals returns all withdrawals with Pending status.
func (b *Bridge) PendingWithdrawals() []*BridgeWithdrawal {
	b.mu.Lock()
	defer b.mu.Unlock()

	var result []*BridgeWithdrawal
	for _, w := range b.withdrawals {
		if w.Status == StatusPending {
			result = append(result, w)
		}
	}
	return result
}

// computeDepositID derives a deterministic deposit ID from its parameters.
func computeDepositID(from, to types.Address, amount *big.Int, l1Block, seq uint64) types.Hash {
	var data []byte
	data = append(data, from[:]...)
	data = append(data, to[:]...)
	data = append(data, amount.Bytes()...)
	data = append(data, byte(l1Block>>56), byte(l1Block>>48), byte(l1Block>>40), byte(l1Block>>32))
	data = append(data, byte(l1Block>>24), byte(l1Block>>16), byte(l1Block>>8), byte(l1Block))
	data = append(data, byte(seq>>56), byte(seq>>48), byte(seq>>40), byte(seq>>32))
	data = append(data, byte(seq>>24), byte(seq>>16), byte(seq>>8), byte(seq))
	return crypto.Keccak256Hash(data)
}

// computeWithdrawalID derives a deterministic withdrawal ID from its parameters.
func computeWithdrawalID(from, to types.Address, amount *big.Int, seq uint64) types.Hash {
	var data []byte
	data = append(data, from[:]...)
	data = append(data, to[:]...)
	data = append(data, amount.Bytes()...)
	data = append(data, byte(seq>>56), byte(seq>>48), byte(seq>>40), byte(seq>>32))
	data = append(data, byte(seq>>24), byte(seq>>16), byte(seq>>8), byte(seq))
	return crypto.Keccak256Hash(data)
}
