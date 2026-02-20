package types

import (
	"errors"
	"fmt"
	"sort"

	"github.com/eth2028/eth2028/rlp"
	"golang.org/x/crypto/sha3"
)

// FOCIL (Fork-Choice enforced Inclusion Lists) extended types and validation.

// Inclusion list constants.
const (
	// MaxILCommitteeSize is the maximum number of validators in the IL committee.
	MaxILCommitteeSize = 16

	// ILExpirySlots is the number of slots before an inclusion list expires.
	ILExpirySlots = 2

	// MaxILPerSlot is the maximum number of inclusion lists per slot
	// (one per committee member).
	MaxILPerSlot = MaxILCommitteeSize
)

// Inclusion list errors.
var (
	ErrILNil                = errors.New("inclusion list: nil list")
	ErrILTooManyTransactions = errors.New("inclusion list: too many transactions")
	ErrILGasExceedsMax      = errors.New("inclusion list: total gas exceeds maximum")
	ErrILSummaryMismatch    = errors.New("inclusion list: summary does not match transactions")
	ErrILEmptyTransactions  = errors.New("inclusion list: transactions list is empty")
	ErrILInvalidSlot        = errors.New("inclusion list: invalid slot number")
	ErrILInvalidSignature   = errors.New("inclusion list: invalid BLS signature")
	ErrILExpired            = errors.New("inclusion list: inclusion list has expired")
	ErrILDuplicateSender    = errors.New("inclusion list: duplicate sender in summary")
)

// InclusionListAggregate combines multiple signed inclusion lists from
// different committee members for a given slot.
type InclusionListAggregate struct {
	Slot        uint64
	Lists       []*SignedInclusionList
	MergedSummary []InclusionListEntry
}

// InclusionListComplianceResult holds the detailed result of checking
// whether a block satisfies the inclusion list constraints.
type InclusionListComplianceResult struct {
	Compliant   bool
	TotalRequired int
	TotalSatisfied int
	MissingSenders []Address
	MissingGas     map[Address]uint64 // gas shortfall per missing sender
}

// --- Inclusion list summary ---

// ComputeILSummary computes the summary entries from a list of raw
// RLP-encoded transactions. Each summary entry contains the sender address
// and gas limit. The sender is recovered from each transaction.
func ComputeILSummary(txs [][]byte, signer Signer) ([]InclusionListEntry, error) {
	entries := make([]InclusionListEntry, 0, len(txs))
	for i, raw := range txs {
		tx, err := decodeILTransaction(raw)
		if err != nil {
			return nil, fmt.Errorf("il tx %d: %w", i, err)
		}
		sender, err := signer.Sender(tx)
		if err != nil {
			return nil, fmt.Errorf("il tx %d sender: %w", i, err)
		}
		entries = append(entries, InclusionListEntry{
			Address:  sender,
			GasLimit: tx.Gas(),
		})
	}
	return entries, nil
}

// decodeILTransaction decodes an RLP-encoded transaction from inclusion list bytes.
func decodeILTransaction(data []byte) (*Transaction, error) {
	if len(data) == 0 {
		return nil, errors.New("empty transaction data")
	}
	// Check if typed transaction (first byte < 0x7f is type prefix).
	if data[0] < 0x7f {
		// For IL purposes, we only need to decode enough to get gas and sender.
		// Delegate to the standard decoder.
		return ilDecodeTypedTx(data)
	}
	// Legacy transaction.
	return ilDecodeLegacyTx(data)
}

// ilDecodeTypedTx decodes a typed (EIP-2718) transaction from its envelope.
func ilDecodeTypedTx(data []byte) (*Transaction, error) {
	if len(data) < 2 {
		return nil, errors.New("typed tx too short")
	}
	txType := data[0]
	var inner TxData

	switch txType {
	case DynamicFeeTxType:
		var tx DynamicFeeTx
		if err := rlp.DecodeBytes(data[1:], &tx); err != nil {
			return nil, err
		}
		inner = &tx
	case AccessListTxType:
		var tx AccessListTx
		if err := rlp.DecodeBytes(data[1:], &tx); err != nil {
			return nil, err
		}
		inner = &tx
	default:
		return nil, fmt.Errorf("unsupported IL tx type: 0x%02x", txType)
	}

	return NewTransaction(inner), nil
}

// ilDecodeLegacyTx decodes a legacy (type 0x00) RLP transaction.
func ilDecodeLegacyTx(data []byte) (*Transaction, error) {
	var tx LegacyTx
	if err := rlp.DecodeBytes(data, &tx); err != nil {
		return nil, err
	}
	return NewTransaction(&tx), nil
}

// --- Validation ---

// ValidateInclusionList performs structural validation on an inclusion list.
func ValidateInclusionList(il *InclusionList) error {
	if il == nil {
		return ErrILNil
	}
	if len(il.Transactions) == 0 {
		return ErrILEmptyTransactions
	}
	if len(il.Transactions) > MaxTransactionsPerInclusionList {
		return fmt.Errorf("%w: got %d, max %d",
			ErrILTooManyTransactions, len(il.Transactions), MaxTransactionsPerInclusionList)
	}

	// Check that summary entries match transaction count.
	if len(il.Summary) != len(il.Transactions) {
		return fmt.Errorf("%w: %d summary entries vs %d transactions",
			ErrILSummaryMismatch, len(il.Summary), len(il.Transactions))
	}

	// Validate total gas does not exceed maximum.
	var totalGas uint64
	for _, entry := range il.Summary {
		totalGas += entry.GasLimit
	}
	if totalGas > MaxGasPerInclusionList {
		return fmt.Errorf("%w: %d > %d", ErrILGasExceedsMax, totalGas, MaxGasPerInclusionList)
	}

	// Check for duplicate senders.
	seen := make(map[Address]bool)
	for _, entry := range il.Summary {
		if seen[entry.Address] {
			return fmt.Errorf("%w: %s", ErrILDuplicateSender, entry.Address.Hex())
		}
		seen[entry.Address] = true
	}

	return nil
}

// ValidateSignedInclusionList validates the structure and BLS signature stub.
// Full BLS verification requires the validator's public key, which is
// performed at a higher layer; this only checks structural validity.
func ValidateSignedInclusionList(sil *SignedInclusionList) error {
	if sil == nil || sil.Message == nil {
		return ErrILNil
	}
	if err := ValidateInclusionList(sil.Message); err != nil {
		return err
	}
	// Check that the signature is not all zeros (structural check only).
	var zeroSig [96]byte
	if sil.Signature == zeroSig {
		return ErrILInvalidSignature
	}
	return nil
}

// IsILExpired checks whether an inclusion list has expired based on the
// current slot number.
func IsILExpired(il *InclusionList, currentSlot uint64) bool {
	if il == nil {
		return true
	}
	return currentSlot > il.Slot+ILExpirySlots
}

// --- Compliance checking ---

// CheckBlockCompliance checks whether a block's transactions satisfy the
// inclusion list constraints. A block is compliant if for every inclusion
// list entry, there is a transaction in the block from the same sender
// with at least the specified gas limit.
func CheckBlockCompliance(il *InclusionList, blockTxSenders []Address, blockTxGas []uint64) *InclusionListComplianceResult {
	result := &InclusionListComplianceResult{
		TotalRequired: len(il.Summary),
		MissingGas:    make(map[Address]uint64),
	}

	// Build a map of sender -> max gas from block transactions.
	senderGas := make(map[Address]uint64)
	for i, sender := range blockTxSenders {
		gas := uint64(0)
		if i < len(blockTxGas) {
			gas = blockTxGas[i]
		}
		if gas > senderGas[sender] {
			senderGas[sender] = gas
		}
	}

	// Check each IL entry.
	for _, entry := range il.Summary {
		maxGas, found := senderGas[entry.Address]
		if !found || maxGas < entry.GasLimit {
			result.MissingSenders = append(result.MissingSenders, entry.Address)
			if found {
				result.MissingGas[entry.Address] = entry.GasLimit - maxGas
			} else {
				result.MissingGas[entry.Address] = entry.GasLimit
			}
		} else {
			result.TotalSatisfied++
		}
	}

	result.Compliant = result.TotalSatisfied == result.TotalRequired
	return result
}

// --- Serialization ---

// inclusionListRLP is the RLP encoding layout for an InclusionList.
type inclusionListRLP struct {
	Slot           uint64
	ValidatorIndex uint64
	CommitteeRoot  Hash
	Transactions   [][]byte
	Summary        []ilEntryRLP
}

// ilEntryRLP is the RLP encoding for a single InclusionListEntry.
type ilEntryRLP struct {
	Address  Address
	GasLimit uint64
}

// EncodeInclusionList RLP-encodes an inclusion list.
func EncodeInclusionList(il *InclusionList) ([]byte, error) {
	if il == nil {
		return nil, ErrILNil
	}
	summary := make([]ilEntryRLP, len(il.Summary))
	for i, e := range il.Summary {
		summary[i] = ilEntryRLP{Address: e.Address, GasLimit: e.GasLimit}
	}
	enc := inclusionListRLP{
		Slot:           il.Slot,
		ValidatorIndex: il.ValidatorIndex,
		CommitteeRoot:  il.CommitteeRoot,
		Transactions:   il.Transactions,
		Summary:        summary,
	}
	return rlp.EncodeToBytes(enc)
}

// DecodeInclusionList decodes an RLP-encoded inclusion list.
func DecodeInclusionList(data []byte) (*InclusionList, error) {
	var dec inclusionListRLP
	if err := rlp.DecodeBytes(data, &dec); err != nil {
		return nil, fmt.Errorf("decode inclusion list: %w", err)
	}
	summary := make([]InclusionListEntry, len(dec.Summary))
	for i, e := range dec.Summary {
		summary[i] = InclusionListEntry{Address: e.Address, GasLimit: e.GasLimit}
	}
	return &InclusionList{
		Slot:           dec.Slot,
		ValidatorIndex: dec.ValidatorIndex,
		CommitteeRoot:  dec.CommitteeRoot,
		Transactions:   dec.Transactions,
		Summary:        summary,
	}, nil
}

// InclusionListHash computes the keccak256 hash of the RLP-encoded inclusion list.
func InclusionListHash(il *InclusionList) Hash {
	encoded, err := EncodeInclusionList(il)
	if err != nil {
		return Hash{}
	}
	d := sha3.NewLegacyKeccak256()
	d.Write(encoded)
	var h Hash
	copy(h[:], d.Sum(nil))
	return h
}

// --- Aggregate operations ---

// MergeInclusionLists merges multiple inclusion lists into an aggregate.
// Entries with the same sender address take the maximum gas limit.
func MergeInclusionLists(lists []*SignedInclusionList) *InclusionListAggregate {
	if len(lists) == 0 {
		return &InclusionListAggregate{}
	}

	mergedMap := make(map[Address]uint64)
	slot := lists[0].Message.Slot

	for _, sil := range lists {
		if sil == nil || sil.Message == nil {
			continue
		}
		for _, entry := range sil.Message.Summary {
			if entry.GasLimit > mergedMap[entry.Address] {
				mergedMap[entry.Address] = entry.GasLimit
			}
		}
	}

	// Convert map to sorted list for deterministic ordering.
	merged := make([]InclusionListEntry, 0, len(mergedMap))
	for addr, gas := range mergedMap {
		merged = append(merged, InclusionListEntry{Address: addr, GasLimit: gas})
	}
	sort.Slice(merged, func(i, j int) bool {
		return string(merged[i].Address[:]) < string(merged[j].Address[:])
	})

	return &InclusionListAggregate{
		Slot:          slot,
		Lists:         lists,
		MergedSummary: merged,
	}
}

// SummaryTotalGas returns the total gas across all summary entries.
func SummaryTotalGas(entries []InclusionListEntry) uint64 {
	var total uint64
	for _, e := range entries {
		total += e.GasLimit
	}
	return total
}
