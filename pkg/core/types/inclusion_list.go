package types

// FOCIL (Fork-Choice enforced Inclusion Lists) types per EIP-7547/EIP-7805.
// These types define the Execution Layer data structures for inclusion lists,
// which ensure censorship resistance by requiring block builders to include
// certain transactions.

// MaxTransactionsPerInclusionList is the maximum number of transactions
// in a single inclusion list (2^4 = 16).
const MaxTransactionsPerInclusionList = 16

// MaxGasPerInclusionList is the maximum total gas for transactions in an
// inclusion list (2^21 = 2097152).
const MaxGasPerInclusionList = 1 << 21

// InclusionListEntry represents a single entry in an inclusion list summary.
// Each entry identifies a transaction by its sender address and gas limit,
// allowing the block builder to satisfy the constraint by including any valid
// transaction from that sender with at least the specified gas.
type InclusionListEntry struct {
	Address  Address // sender address
	GasLimit uint64  // gas limit for the transaction
}

// InclusionList represents a set of transactions that MUST be included in
// a subsequent block. Created by the slot N proposer and enforced in slot N+1.
//
// Per EIP-7805, the inclusion list is created by a committee of validators
// for a given slot and referenced by the committee root.
type InclusionList struct {
	Slot           uint64               // beacon slot this IL targets
	ValidatorIndex uint64               // index of the IL committee member
	CommitteeRoot  Hash                 // root of the inclusion list committee
	Transactions   [][]byte             // RLP-encoded transactions to be included
	Summary        []InclusionListEntry // summary entries (address + gas limit)
}

// SignedInclusionList wraps an InclusionList with a BLS signature
// from the validator who created it.
type SignedInclusionList struct {
	Message   *InclusionList
	Signature [96]byte // BLS signature
}

// InclusionListSatisfaction tracks whether a block satisfies the inclusion
// list constraints from the previous slot. Used by the fork-choice rule
// to determine block validity.
type InclusionListSatisfaction struct {
	Satisfied       bool     // true if all IL constraints are met
	MissingTxHashes []Hash   // tx hashes from the IL not found in the block
	ExcludedIndices []uint64 // indices of IL entries excluded (tx already in prev block)
}
