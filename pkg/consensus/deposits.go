package consensus

// EIP-6110: Supply Validator Deposits on Chain
// Moves validator deposit processing from the consensus layer to the
// execution layer, removing the need for Eth1Data voting and deposit
// contract Merkle proofs. Deposits are included directly in EL blocks
// and forwarded to the beacon state.

import (
	"bytes"
	"errors"
	"sync"

	"github.com/eth2030/eth2030/core/types"
	"github.com/eth2030/eth2030/crypto"
)

const (
	// BLSPubkeyLength is the length of a BLS12-381 public key.
	BLSPubkeyLength = 48

	// BLSSignatureLength is the length of a BLS12-381 signature.
	BLSSignatureLength = 96

	// MinDepositAmountGwei is the minimum deposit amount (32 ETH in Gwei).
	MinDepositAmountGwei uint64 = 32_000_000_000

	// DefaultMaxDepositsPerBlock is the max deposits that can be processed
	// in a single block.
	DefaultMaxDepositsPerBlock uint64 = 16

	// DefaultActivationDelay is the number of epochs a validator must wait
	// after meeting the deposit threshold before activation.
	DefaultActivationDelay uint64 = 4
)

var (
	ErrDepositZeroAmount    = errors.New("deposit: amount must be > 0")
	ErrInvalidPubkeyLength  = errors.New("deposit: invalid pubkey length")
	ErrInvalidSigLength     = errors.New("deposit: invalid signature length")
	ErrEmptyPubkey          = errors.New("deposit: pubkey is empty")
	ErrDepositAlreadyExists = errors.New("deposit: deposit index already processed")
)

// DepositConfig holds the configuration for the deposit processor.
type DepositConfig struct {
	// MinDepositAmount is the minimum deposit in Gwei (default: 32 ETH).
	MinDepositAmount uint64

	// MaxDepositsPerBlock is the maximum number of deposits per block.
	MaxDepositsPerBlock uint64

	// ActivationDelay is the number of epochs to wait before activation.
	ActivationDelay uint64
}

// DefaultDepositConfig returns the default deposit processing configuration.
func DefaultDepositConfig() DepositConfig {
	return DepositConfig{
		MinDepositAmount:    MinDepositAmountGwei,
		MaxDepositsPerBlock: DefaultMaxDepositsPerBlock,
		ActivationDelay:     DefaultActivationDelay,
	}
}

// ValidatorDeposit represents a deposit from the execution layer to
// register or top up a validator.
type ValidatorDeposit struct {
	Pubkey                []byte
	WithdrawalCredentials types.Hash
	Amount                uint64 // in Gwei
	Signature             []byte
	Index                 uint64 // deposit index (monotonically increasing)
}

// ActivatedValidator represents a validator that has been activated
// after meeting the deposit threshold and waiting the activation delay.
type ActivatedValidator struct {
	Pubkey           []byte
	Index            uint64
	EffectiveBalance uint64
	ActivationEpoch  uint64
}

// DepositProcessor processes validator deposits from the execution layer.
// It is fully thread-safe.
type DepositProcessor struct {
	mu            sync.RWMutex
	config        DepositConfig
	deposits      []*ValidatorDeposit          // all deposits in order
	depositsByIdx map[uint64]*ValidatorDeposit // index -> deposit
	byPubkey      map[string]*depositAccumulator // hex(pubkey) -> accumulator
	depositCount  uint64
	pending       []*ValidatorDeposit // deposits not yet activated
}

// depositAccumulator tracks the accumulated deposit amount for a single
// pubkey, since a validator can make multiple deposits.
type depositAccumulator struct {
	totalAmount     uint64
	latestDeposit   *ValidatorDeposit
	activated       bool
	activationEpoch uint64
}

// NewDepositProcessor creates a new deposit processor.
func NewDepositProcessor(config DepositConfig) *DepositProcessor {
	return &DepositProcessor{
		config:        config,
		deposits:      make([]*ValidatorDeposit, 0),
		depositsByIdx: make(map[uint64]*ValidatorDeposit),
		byPubkey:      make(map[string]*depositAccumulator),
		pending:       make([]*ValidatorDeposit, 0),
	}
}

// ValidateDeposit checks whether a deposit has valid parameters.
func (dp *DepositProcessor) ValidateDeposit(deposit *ValidatorDeposit) error {
	if len(deposit.Pubkey) == 0 {
		return ErrEmptyPubkey
	}
	if len(deposit.Pubkey) != BLSPubkeyLength {
		return ErrInvalidPubkeyLength
	}
	if len(deposit.Signature) != 0 && len(deposit.Signature) != BLSSignatureLength {
		return ErrInvalidSigLength
	}
	if deposit.Amount == 0 {
		return ErrDepositZeroAmount
	}
	return nil
}

// ProcessDeposit validates and stores a new deposit.
func (dp *DepositProcessor) ProcessDeposit(deposit *ValidatorDeposit) error {
	if err := dp.ValidateDeposit(deposit); err != nil {
		return err
	}

	dp.mu.Lock()
	defer dp.mu.Unlock()

	// Reject duplicate deposit indices.
	if _, exists := dp.depositsByIdx[deposit.Index]; exists {
		return ErrDepositAlreadyExists
	}

	dp.deposits = append(dp.deposits, deposit)
	dp.depositsByIdx[deposit.Index] = deposit
	dp.depositCount++

	// Accumulate by pubkey.
	key := string(deposit.Pubkey)
	acc, exists := dp.byPubkey[key]
	if !exists {
		acc = &depositAccumulator{}
		dp.byPubkey[key] = acc
	}
	acc.totalAmount += deposit.Amount
	acc.latestDeposit = deposit

	// Add to pending if validator is not yet activated.
	if !acc.activated {
		dp.pending = append(dp.pending, deposit)
	}

	return nil
}

// GetPendingDeposits returns all deposits that have not yet been processed
// for activation.
func (dp *DepositProcessor) GetPendingDeposits() []*ValidatorDeposit {
	dp.mu.RLock()
	defer dp.mu.RUnlock()

	result := make([]*ValidatorDeposit, len(dp.pending))
	copy(result, dp.pending)
	return result
}

// GetDepositCount returns the total number of processed deposits.
func (dp *DepositProcessor) GetDepositCount() uint64 {
	dp.mu.RLock()
	defer dp.mu.RUnlock()
	return dp.depositCount
}

// GetDepositRoot computes the Merkle root of all processed deposits
// using Keccak-256 hashing. Returns the zero hash if there are no deposits.
func (dp *DepositProcessor) GetDepositRoot() types.Hash {
	dp.mu.RLock()
	defer dp.mu.RUnlock()

	if len(dp.deposits) == 0 {
		return types.Hash{}
	}

	// Build leaves: hash each deposit's fields.
	leaves := make([]types.Hash, len(dp.deposits))
	for i, d := range dp.deposits {
		leaves[i] = hashDeposit(d)
	}

	return computeMerkleRoot(leaves)
}

// hashDeposit produces a leaf hash from a deposit's fields.
func hashDeposit(d *ValidatorDeposit) types.Hash {
	// Concatenate pubkey + withdrawal credentials + amount (8 bytes LE) + index (8 bytes LE)
	buf := make([]byte, 0, len(d.Pubkey)+32+8+8)
	buf = append(buf, d.Pubkey...)
	buf = append(buf, d.WithdrawalCredentials[:]...)
	buf = appendUint64LE(buf, d.Amount)
	buf = appendUint64LE(buf, d.Index)
	return crypto.Keccak256Hash(buf)
}

// appendUint64LE appends a uint64 in little-endian byte order.
func appendUint64LE(buf []byte, v uint64) []byte {
	return append(buf,
		byte(v), byte(v>>8), byte(v>>16), byte(v>>24),
		byte(v>>32), byte(v>>40), byte(v>>48), byte(v>>56),
	)
}

// computeMerkleRoot builds a binary Merkle tree from the given leaves.
// If the number of leaves is odd, the last leaf is duplicated.
func computeMerkleRoot(leaves []types.Hash) types.Hash {
	if len(leaves) == 0 {
		return types.Hash{}
	}
	if len(leaves) == 1 {
		return leaves[0]
	}

	// Work upward through tree levels.
	current := make([]types.Hash, len(leaves))
	copy(current, leaves)

	for len(current) > 1 {
		// If odd, duplicate the last element.
		if len(current)%2 != 0 {
			current = append(current, current[len(current)-1])
		}

		next := make([]types.Hash, len(current)/2)
		for i := 0; i < len(current); i += 2 {
			combined := make([]byte, 64)
			copy(combined[:32], current[i][:])
			copy(combined[32:], current[i+1][:])
			next[i/2] = crypto.Keccak256Hash(combined)
		}
		current = next
	}

	return current[0]
}

// ActivateValidators finds all validators with sufficient accumulated
// deposits and activates them. Returns the list of newly activated
// validators. The activation epoch is set to epoch + ActivationDelay.
func (dp *DepositProcessor) ActivateValidators(epoch uint64) []*ActivatedValidator {
	dp.mu.Lock()
	defer dp.mu.Unlock()

	var activated []*ActivatedValidator
	var remaining []*ValidatorDeposit

	// Track which pubkeys we activate in this call.
	activatedKeys := make(map[string]bool)

	for _, d := range dp.pending {
		key := string(d.Pubkey)
		acc, ok := dp.byPubkey[key]
		if !ok {
			continue
		}

		// Skip already activated or already handled in this batch.
		if acc.activated || activatedKeys[key] {
			continue
		}

		if acc.totalAmount >= dp.config.MinDepositAmount {
			activationEpoch := epoch + dp.config.ActivationDelay

			// Cap effective balance at the accumulated total, rounded down
			// to the nearest Gwei increment.
			effBalance := acc.totalAmount

			acc.activated = true
			acc.activationEpoch = activationEpoch
			activatedKeys[key] = true

			activated = append(activated, &ActivatedValidator{
				Pubkey:           d.Pubkey,
				Index:            d.Index,
				EffectiveBalance: effBalance,
				ActivationEpoch:  activationEpoch,
			})
		} else {
			// Still pending, keep in the list.
			remaining = append(remaining, d)
		}
	}

	dp.pending = remaining
	return activated
}

// GetValidatorByPubkey looks up a validator's latest deposit by public key.
func (dp *DepositProcessor) GetValidatorByPubkey(pubkey []byte) (*ValidatorDeposit, bool) {
	dp.mu.RLock()
	defer dp.mu.RUnlock()

	key := string(pubkey)
	acc, ok := dp.byPubkey[key]
	if !ok {
		return nil, false
	}
	return acc.latestDeposit, true
}

// GetValidatorBalance returns the accumulated deposit balance for a pubkey.
func (dp *DepositProcessor) GetValidatorBalance(pubkey []byte) (uint64, bool) {
	dp.mu.RLock()
	defer dp.mu.RUnlock()

	key := string(pubkey)
	acc, ok := dp.byPubkey[key]
	if !ok {
		return 0, false
	}
	return acc.totalAmount, true
}

// IsActivated returns whether a validator identified by pubkey has been
// activated.
func (dp *DepositProcessor) IsActivated(pubkey []byte) bool {
	dp.mu.RLock()
	defer dp.mu.RUnlock()

	key := string(pubkey)
	acc, ok := dp.byPubkey[key]
	if !ok {
		return false
	}
	return acc.activated
}

// pubkeysEqual returns whether two pubkey byte slices are equal.
func pubkeysEqual(a, b []byte) bool {
	return bytes.Equal(a, b)
}
