package types

import (
	"errors"
	"fmt"

	"github.com/eth2028/eth2028/rlp"
	"golang.org/x/crypto/sha3"
)

// EIP-4895 beacon chain withdrawal constants and utility functions.
//
// The Withdrawal struct is defined in block.go:
//   type Withdrawal struct {
//       Index          uint64
//       ValidatorIndex uint64
//       Address        Address
//       Amount         uint64 // in Gwei
//   }

// MaxWithdrawalsPerPayload is the maximum number of withdrawals
// allowed per execution payload (EIP-4895).
const MaxWithdrawalsPerPayload = 16

var (
	errNilWithdrawal        = errors.New("withdrawal is nil")
	errZeroAddress          = errors.New("withdrawal address must not be zero")
	errTooManyWithdrawals   = errors.New("too many withdrawals in payload")
	errDuplicateWithdrawal  = errors.New("duplicate withdrawal index")
)

// withdrawalRLP is the RLP encoding layout for a Withdrawal.
type withdrawalRLP struct {
	Index          uint64
	ValidatorIndex uint64
	Address        Address
	Amount         uint64
}

// keccak256Hash computes keccak256 and returns a Hash (avoids import cycle with crypto pkg).
func keccak256Hash(data []byte) Hash {
	d := sha3.NewLegacyKeccak256()
	d.Write(data)
	var h Hash
	copy(h[:], d.Sum(nil))
	return h
}

// WithdrawalHash computes the keccak256 hash of a single withdrawal.
func WithdrawalHash(w *Withdrawal) Hash {
	encoded := EncodeWithdrawal(w)
	return keccak256Hash(encoded)
}

// WithdrawalsRoot computes the Merkle root (trie root) of a list of
// withdrawals by hashing the RLP-encoded concatenation. This produces a
// simple linear hash commitment over the ordered withdrawal list.
func WithdrawalsRoot(withdrawals []*Withdrawal) Hash {
	if len(withdrawals) == 0 {
		return EmptyRootHash
	}
	// Build a payload of individually RLP-encoded withdrawals keyed by index.
	// We use a simple approach: hash(RLP(list of encoded withdrawals)).
	var payload []byte
	for _, w := range withdrawals {
		encoded := EncodeWithdrawal(w)
		payload = append(payload, encoded...)
	}
	return keccak256Hash(payload)
}

// EncodeWithdrawal RLP-encodes a withdrawal to bytes.
func EncodeWithdrawal(w *Withdrawal) []byte {
	enc := withdrawalRLP{
		Index:          w.Index,
		ValidatorIndex: w.ValidatorIndex,
		Address:        w.Address,
		Amount:         w.Amount,
	}
	data, err := rlp.EncodeToBytes(enc)
	if err != nil {
		return nil
	}
	return data
}

// DecodeWithdrawal decodes a withdrawal from RLP-encoded bytes.
func DecodeWithdrawal(data []byte) (*Withdrawal, error) {
	var dec withdrawalRLP
	if err := rlp.DecodeBytes(data, &dec); err != nil {
		return nil, fmt.Errorf("decode withdrawal: %w", err)
	}
	return &Withdrawal{
		Index:          dec.Index,
		ValidatorIndex: dec.ValidatorIndex,
		Address:        dec.Address,
		Amount:         dec.Amount,
	}, nil
}

// ValidateWithdrawal checks that a withdrawal has valid fields.
func ValidateWithdrawal(w *Withdrawal) error {
	if w == nil {
		return errNilWithdrawal
	}
	if w.Address.IsZero() {
		return errZeroAddress
	}
	return nil
}

// ProcessWithdrawals processes a list of withdrawals and returns a credit
// map from address to total Gwei amount credited. It validates the list
// size and individual withdrawals, and checks for duplicate indices.
func ProcessWithdrawals(withdrawals []*Withdrawal) (map[Address]uint64, error) {
	if len(withdrawals) > MaxWithdrawalsPerPayload {
		return nil, errTooManyWithdrawals
	}

	seen := make(map[uint64]bool, len(withdrawals))
	credits := make(map[Address]uint64, len(withdrawals))

	for _, w := range withdrawals {
		if err := ValidateWithdrawal(w); err != nil {
			return nil, fmt.Errorf("withdrawal index %d: %w", w.Index, err)
		}
		if seen[w.Index] {
			return nil, fmt.Errorf("%w: %d", errDuplicateWithdrawal, w.Index)
		}
		seen[w.Index] = true
		credits[w.Address] += w.Amount
	}
	return credits, nil
}

// FilterByValidator returns all withdrawals for the given validator index.
func FilterByValidator(withdrawals []*Withdrawal, validatorIndex uint64) []*Withdrawal {
	var result []*Withdrawal
	for _, w := range withdrawals {
		if w.ValidatorIndex == validatorIndex {
			result = append(result, w)
		}
	}
	return result
}

// TotalWithdrawalAmount sums the Amount field of all withdrawals.
func TotalWithdrawalAmount(withdrawals []*Withdrawal) uint64 {
	var total uint64
	for _, w := range withdrawals {
		total += w.Amount
	}
	return total
}
