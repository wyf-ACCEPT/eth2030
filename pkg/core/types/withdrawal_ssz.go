// withdrawal_ssz.go implements SSZ encoding, decoding, and Merkleization
// for EIP-4895 Withdrawal types. Complements withdrawal.go (which provides
// RLP encoding) and block_ssz.go (which encodes withdrawals within blocks).
//
// This file adds:
//   - WithdrawalList with SSZ list semantics (max length, mix-in-length)
//   - Per-withdrawal SSZ hash tree root as a container
//   - Batch SSZ hash tree root for a list of withdrawals
//   - SSZ marshal/unmarshal for standalone Withdrawal values
package types

import (
	"encoding/binary"
	"errors"
	"fmt"

	"github.com/eth2030/eth2030/ssz"
)

// Withdrawal SSZ constants.
const (
	// WithdrawalSSZSize is the fixed SSZ size of a single Withdrawal.
	// index(8) + validatorIndex(8) + address(20) + amount(8) = 44 bytes.
	WithdrawalSSZSize = 44

	// MaxWithdrawalsPerPayloadSSZ is the SSZ list limit for the withdrawals
	// field in an ExecutionPayload. Per consensus specs this is 1 << 4 = 16.
	MaxWithdrawalsPerPayloadSSZ = 16
)

// Withdrawal SSZ-specific errors.
var (
	ErrWithdrawalSSZSize = errors.New("ssz: invalid withdrawal data size")
)

// WithdrawalList is a typed list of Withdrawal pointers with SSZ list
// semantics. The list has a maximum capacity of MaxWithdrawalsPerPayloadSSZ.
type WithdrawalList []*Withdrawal

// Len returns the number of withdrawals in the list.
func (wl WithdrawalList) Len() int { return len(wl) }

// SizeSSZ returns the total SSZ encoded size of the withdrawal list.
func (wl WithdrawalList) SizeSSZ() int {
	return len(wl) * WithdrawalSSZSize
}

// MarshalSSZ serializes the withdrawal list to SSZ bytes.
// Each withdrawal is a fixed-size container, so the list is a simple
// concatenation of all withdrawals.
func (wl WithdrawalList) MarshalSSZ() ([]byte, error) {
	if len(wl) > MaxWithdrawalsPerPayloadSSZ {
		return nil, fmt.Errorf("ssz: withdrawal list too long: %d > %d",
			len(wl), MaxWithdrawalsPerPayloadSSZ)
	}
	buf := make([]byte, 0, len(wl)*WithdrawalSSZSize)
	for _, w := range wl {
		buf = append(buf, MarshalWithdrawalSSZ(w)...)
	}
	return buf, nil
}

// UnmarshalSSZ deserializes a withdrawal list from SSZ bytes.
func (wl *WithdrawalList) UnmarshalSSZ(data []byte) error {
	if len(data)%WithdrawalSSZSize != 0 {
		return ErrWithdrawalSSZSize
	}
	count := len(data) / WithdrawalSSZSize
	if count > MaxWithdrawalsPerPayloadSSZ {
		return fmt.Errorf("ssz: too many withdrawals: %d > %d",
			count, MaxWithdrawalsPerPayloadSSZ)
	}
	list := make([]*Withdrawal, count)
	for i := 0; i < count; i++ {
		off := i * WithdrawalSSZSize
		w, err := UnmarshalWithdrawalSSZ(data[off : off+WithdrawalSSZSize])
		if err != nil {
			return fmt.Errorf("ssz: withdrawal %d: %w", i, err)
		}
		list[i] = w
	}
	*wl = list
	return nil
}

// HashTreeRoot computes the SSZ hash tree root for the withdrawal list.
// Per SSZ spec, lists are Merkleized with the max capacity as limit,
// then mixed in with the actual length.
func (wl WithdrawalList) HashTreeRoot() ([32]byte, error) {
	roots := make([][32]byte, len(wl))
	for i, w := range wl {
		roots[i] = WithdrawalHashTreeRoot(w)
	}
	return ssz.HashTreeRootList(roots, MaxWithdrawalsPerPayloadSSZ), nil
}

// MarshalWithdrawalSSZ serializes a single Withdrawal to its SSZ encoding.
// Layout (fixed-size container, 44 bytes):
//
//	index(8) || validatorIndex(8) || address(20) || amount(8)
func MarshalWithdrawalSSZ(w *Withdrawal) []byte {
	var buf [WithdrawalSSZSize]byte
	binary.LittleEndian.PutUint64(buf[0:8], w.Index)
	binary.LittleEndian.PutUint64(buf[8:16], w.ValidatorIndex)
	copy(buf[16:36], w.Address[:])
	binary.LittleEndian.PutUint64(buf[36:44], w.Amount)
	return buf[:]
}

// UnmarshalWithdrawalSSZ deserializes a Withdrawal from SSZ bytes.
func UnmarshalWithdrawalSSZ(data []byte) (*Withdrawal, error) {
	if len(data) != WithdrawalSSZSize {
		return nil, fmt.Errorf("%w: got %d bytes, want %d",
			ErrWithdrawalSSZSize, len(data), WithdrawalSSZSize)
	}
	w := &Withdrawal{
		Index:          binary.LittleEndian.Uint64(data[0:8]),
		ValidatorIndex: binary.LittleEndian.Uint64(data[8:16]),
		Amount:         binary.LittleEndian.Uint64(data[36:44]),
	}
	copy(w.Address[:], data[16:36])
	return w, nil
}

// WithdrawalHashTreeRoot computes the SSZ hash tree root for a single
// Withdrawal, treating it as a fixed-size container with 4 fields:
//
//	index: uint64
//	validator_index: uint64
//	address: Bytes20 (Vector[byte, 20])
//	amount: uint64
//
// Per SSZ spec, the container root is Merkleize([field_0_root, ..., field_N_root]).
func WithdrawalHashTreeRoot(w *Withdrawal) [32]byte {
	fieldRoots := [4][32]byte{
		ssz.HashTreeRootUint64(w.Index),
		ssz.HashTreeRootUint64(w.ValidatorIndex),
		ssz.HashTreeRootAddress(w.Address),
		ssz.HashTreeRootUint64(w.Amount),
	}
	return ssz.HashTreeRootContainer(fieldRoots[:])
}

// WithdrawalsHashTreeRoot computes the SSZ hash tree root for a slice
// of withdrawals treated as List[Withdrawal, MAX_WITHDRAWALS_PER_PAYLOAD].
func WithdrawalsHashTreeRoot(withdrawals []*Withdrawal) [32]byte {
	roots := make([][32]byte, len(withdrawals))
	for i, w := range withdrawals {
		roots[i] = WithdrawalHashTreeRoot(w)
	}
	return ssz.HashTreeRootList(roots, MaxWithdrawalsPerPayloadSSZ)
}
