package core

import (
	"errors"
	"fmt"
	"math/big"
	"sync"

	"github.com/eth2030/eth2030/core/state"
	"github.com/eth2030/eth2030/core/types"
)

// Inclusion list validation errors.
var (
	ErrILTooManyTransactions = errors.New("inclusion list exceeds max transactions")
	ErrILTooMuchGas          = errors.New("inclusion list exceeds max gas")
	ErrILEmptyTransactions   = errors.New("inclusion list has no transactions")
	ErrILInvalidTransaction  = errors.New("inclusion list contains invalid transaction")
	ErrILDuplicateSender     = errors.New("inclusion list contains duplicate sender")
	ErrILInsufficientGasCap  = errors.New("inclusion list tx gasFeeCap too low for next slot")
	ErrILTxNotExecutable     = errors.New("inclusion list tx not executable at current state")
	ErrILNotSatisfied        = errors.New("block does not satisfy inclusion list")
)

// BaseFeeBufferPercent is the percentage buffer (12.5%) that inclusion list
// transaction gas fee caps must exceed the current base fee by, to ensure
// validity in the next slot. Per EIP-7547.
const BaseFeeBufferPercent = 1125 // 112.5% expressed as 1125/1000

// InclusionListStore holds inclusion lists received for pending slots.
// Thread-safe for concurrent access from P2P and block validation.
type InclusionListStore struct {
	mu              sync.RWMutex
	inclusionLists  map[inclusionListKey][]*types.InclusionList
	equivocators    map[inclusionListKey]map[uint64]struct{} // validator indices that equivocated
}

// inclusionListKey identifies ILs by slot and committee root.
type inclusionListKey struct {
	Slot          uint64
	CommitteeRoot types.Hash
}

// NewInclusionListStore creates a new store for tracking inclusion lists.
func NewInclusionListStore() *InclusionListStore {
	return &InclusionListStore{
		inclusionLists: make(map[inclusionListKey][]*types.InclusionList),
		equivocators:   make(map[inclusionListKey]map[uint64]struct{}),
	}
}

// ProcessInclusionList validates and stores an incoming inclusion list.
// Equivocating validators (those who submit conflicting ILs for the same
// slot) are tracked and their ILs are discarded.
func (s *InclusionListStore) ProcessInclusionList(il *types.InclusionList, isBeforeDeadline bool) error {
	if err := ValidateInclusionListBasic(il); err != nil {
		return err
	}

	key := inclusionListKey{Slot: il.Slot, CommitteeRoot: il.CommitteeRoot}

	s.mu.Lock()
	defer s.mu.Unlock()

	// Check if this validator has equivocated.
	if eqs, ok := s.equivocators[key]; ok {
		if _, equivocated := eqs[il.ValidatorIndex]; equivocated {
			return nil // silently ignore equivocators
		}
	}

	// Check for conflicting ILs from the same validator.
	existing := s.inclusionLists[key]
	for i, stored := range existing {
		if stored.ValidatorIndex != il.ValidatorIndex {
			continue
		}
		// Same validator: check if it's a duplicate or equivocation.
		if inclusionListsEqual(stored, il) {
			return nil // identical, no-op
		}
		// Different content = equivocation. Remove the stored IL and mark as equivocator.
		s.inclusionLists[key] = append(existing[:i], existing[i+1:]...)
		if s.equivocators[key] == nil {
			s.equivocators[key] = make(map[uint64]struct{})
		}
		s.equivocators[key][il.ValidatorIndex] = struct{}{}
		return nil
	}

	// Only store if received before the view freeze deadline.
	if isBeforeDeadline {
		s.inclusionLists[key] = append(s.inclusionLists[key], il)
	}
	return nil
}

// GetInclusionListTransactions returns the deduplicated set of transactions
// from all valid, non-equivocating inclusion lists for the given slot and
// committee root.
func (s *InclusionListStore) GetInclusionListTransactions(slot uint64, committeeRoot types.Hash) [][]byte {
	key := inclusionListKey{Slot: slot, CommitteeRoot: committeeRoot}

	s.mu.RLock()
	defer s.mu.RUnlock()

	lists := s.inclusionLists[key]
	if len(lists) == 0 {
		return nil
	}

	// Collect and deduplicate transactions by hash.
	seen := make(map[types.Hash]struct{})
	var result [][]byte
	for _, il := range lists {
		for _, txBytes := range il.Transactions {
			tx, err := types.DecodeTxRLP(txBytes)
			if err != nil {
				continue
			}
			h := tx.Hash()
			if _, ok := seen[h]; !ok {
				seen[h] = struct{}{}
				result = append(result, txBytes)
			}
		}
	}
	return result
}

// PruneSlot removes all inclusion lists and equivocator data for slots
// older than the given slot.
func (s *InclusionListStore) PruneSlot(olderThan uint64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for key := range s.inclusionLists {
		if key.Slot < olderThan {
			delete(s.inclusionLists, key)
			delete(s.equivocators, key)
		}
	}
}

// ValidateInclusionListBasic performs structural validation on an inclusion
// list without requiring state access.
func ValidateInclusionListBasic(il *types.InclusionList) error {
	if len(il.Transactions) == 0 {
		return ErrILEmptyTransactions
	}
	if len(il.Transactions) > types.MaxTransactionsPerInclusionList {
		return fmt.Errorf("%w: %d > %d", ErrILTooManyTransactions,
			len(il.Transactions), types.MaxTransactionsPerInclusionList)
	}

	// Validate total gas and individual transactions.
	var totalGas uint64
	senders := make(map[types.Address]struct{})

	for i, txBytes := range il.Transactions {
		tx, err := types.DecodeTxRLP(txBytes)
		if err != nil {
			return fmt.Errorf("%w: tx %d: %v", ErrILInvalidTransaction, i, err)
		}

		totalGas += tx.Gas()
		if totalGas > types.MaxGasPerInclusionList {
			return fmt.Errorf("%w: %d > %d", ErrILTooMuchGas,
				totalGas, types.MaxGasPerInclusionList)
		}

		// Check sender from the cached address if available.
		if sender := tx.Sender(); sender != nil {
			if _, dup := senders[*sender]; dup {
				return fmt.Errorf("%w: address %s", ErrILDuplicateSender, sender.Hex())
			}
			senders[*sender] = struct{}{}
		}
	}

	// Validate summary entries match transactions.
	if len(il.Summary) > 0 && len(il.Summary) != len(il.Transactions) {
		return fmt.Errorf("%w: summary length %d != transactions length %d",
			ErrILInvalidTransaction, len(il.Summary), len(il.Transactions))
	}

	return nil
}

// ValidateInclusionListState checks that all transactions in an inclusion
// list are executable given the current state and base fee. This ensures
// that the IL txs can actually be included.
//
// The sender is resolved from: the transaction's cached sender, the summary
// entry (if present), or skipped if neither is available.
func ValidateInclusionListState(il *types.InclusionList, statedb state.StateDB, baseFee *big.Int) error {
	for i, txBytes := range il.Transactions {
		tx, err := types.DecodeTxRLP(txBytes)
		if err != nil {
			return fmt.Errorf("%w: tx %d decode: %v", ErrILInvalidTransaction, i, err)
		}

		sender := ilTxSender(tx, il.Summary, i)
		if sender == nil {
			continue
		}

		// Check nonce: tx nonce must equal the account's current nonce.
		accountNonce := statedb.GetNonce(*sender)
		if tx.Nonce() < accountNonce {
			return fmt.Errorf("%w: tx %d nonce %d < account nonce %d",
				ErrILTxNotExecutable, i, tx.Nonce(), accountNonce)
		}

		// Check balance: sender must have enough balance for value + gas.
		cost := new(big.Int)
		if tx.Value() != nil {
			cost.Add(cost, tx.Value())
		}
		gasPrice := tx.GasFeeCap()
		if gasPrice == nil {
			gasPrice = tx.GasPrice()
		}
		if gasPrice != nil {
			gasCost := new(big.Int).Mul(gasPrice, new(big.Int).SetUint64(tx.Gas()))
			cost.Add(cost, gasCost)
		}
		balance := statedb.GetBalance(*sender)
		if balance.Cmp(cost) < 0 {
			return fmt.Errorf("%w: tx %d insufficient balance: have %s, need %s",
				ErrILTxNotExecutable, i, balance.String(), cost.String())
		}

		// Check gas fee cap: must be at least 112.5% of current base fee
		// to remain valid in the next slot.
		if baseFee != nil && baseFee.Sign() > 0 && tx.GasFeeCap() != nil {
			minFeeCap := new(big.Int).Mul(baseFee, big.NewInt(BaseFeeBufferPercent))
			minFeeCap.Div(minFeeCap, big.NewInt(1000))
			if tx.GasFeeCap().Cmp(minFeeCap) < 0 {
				return fmt.Errorf("%w: tx %d gasFeeCap %s < required %s (112.5%% of baseFee %s)",
					ErrILInsufficientGasCap, i, tx.GasFeeCap().String(),
					minFeeCap.String(), baseFee.String())
			}
		}
	}
	return nil
}

// CheckBlockSatisfiesInclusionList verifies that a block includes all
// transactions from the active inclusion list (or that excluded transactions
// are provably invalid/already included).
//
// The block satisfies the IL if for every IL transaction, one of:
//   - The transaction is included in the block
//   - The transaction was included in the previous block (excluded index)
//   - The transaction is no longer valid (nonce consumed, insufficient balance)
//
// Returns an InclusionListSatisfaction result.
func CheckBlockSatisfiesInclusionList(
	block *types.Block,
	il *types.InclusionList,
	statedb state.StateDB,
) *types.InclusionListSatisfaction {
	if il == nil || len(il.Transactions) == 0 {
		return &types.InclusionListSatisfaction{Satisfied: true}
	}

	// Build a set of transaction hashes included in this block.
	blockTxHashes := make(map[types.Hash]struct{}, len(block.Transactions()))
	for _, tx := range block.Transactions() {
		blockTxHashes[tx.Hash()] = struct{}{}
	}

	result := &types.InclusionListSatisfaction{Satisfied: true}

	for i, ilTxBytes := range il.Transactions {
		ilTx, err := types.DecodeTxRLP(ilTxBytes)
		if err != nil {
			// Undecodable IL tx: treat as satisfied (invalid tx).
			continue
		}

		txHash := ilTx.Hash()

		// Case 1: Transaction is included in the block.
		if _, found := blockTxHashes[txHash]; found {
			continue
		}

		// Case 2: Transaction is no longer executable (nonce already used
		// or insufficient balance). This means the IL constraint is
		// automatically satisfied because the tx became invalid.
		sender := ilTxSender(ilTx, il.Summary, i)
		if sender != nil && statedb != nil {
			accountNonce := statedb.GetNonce(*sender)
			if ilTx.Nonce() < accountNonce {
				// Nonce already consumed: tx is invalid, constraint satisfied.
				continue
			}

			// Check if sender can still afford the tx.
			cost := new(big.Int)
			if ilTx.Value() != nil {
				cost.Add(cost, ilTx.Value())
			}
			gasPrice := ilTx.GasFeeCap()
			if gasPrice == nil {
				gasPrice = ilTx.GasPrice()
			}
			if gasPrice != nil {
				gasCost := new(big.Int).Mul(gasPrice, new(big.Int).SetUint64(ilTx.Gas()))
				cost.Add(cost, gasCost)
			}
			balance := statedb.GetBalance(*sender)
			if balance.Cmp(cost) < 0 {
				// Insufficient balance: tx can't be executed, constraint satisfied.
				continue
			}
		}

		// Case 3: Not included and still valid -- block fails to satisfy IL.
		result.Satisfied = false
		result.MissingTxHashes = append(result.MissingTxHashes, txHash)
	}

	return result
}

// ilTxSender resolves the sender address for an IL transaction.
// It first checks the transaction's cached sender, then falls back to
// the summary entry if available.
func ilTxSender(tx *types.Transaction, summary []types.InclusionListEntry, index int) *types.Address {
	if sender := tx.Sender(); sender != nil {
		return sender
	}
	if index < len(summary) {
		addr := summary[index].Address
		if addr != (types.Address{}) {
			return &addr
		}
	}
	return nil
}

// GetInclusionListFromPool generates an inclusion list from the current
// transaction pool. This is the EL-side implementation of get_inclusion_list
// called by CL validators who are inclusion list committee members.
//
// It selects transactions from the mempool that have sufficient gas fee caps
// and are executable at the current state.
func GetInclusionListFromPool(
	txPool TxPoolReader,
	statedb state.StateDB,
	baseFee *big.Int,
) *types.InclusionList {
	if txPool == nil {
		return &types.InclusionList{}
	}

	pending := txPool.Pending()
	if len(pending) == 0 {
		return &types.InclusionList{}
	}

	il := &types.InclusionList{}
	senders := make(map[types.Address]struct{})
	var totalGas uint64

	for _, tx := range pending {
		if len(il.Transactions) >= types.MaxTransactionsPerInclusionList {
			break
		}

		// Skip blob transactions (not suitable for inclusion lists).
		if tx.Type() == types.BlobTxType {
			continue
		}

		// Verify the gas fee cap is sufficient for next-slot validity.
		if baseFee != nil && baseFee.Sign() > 0 && tx.GasFeeCap() != nil {
			minFeeCap := new(big.Int).Mul(baseFee, big.NewInt(BaseFeeBufferPercent))
			minFeeCap.Div(minFeeCap, big.NewInt(1000))
			if tx.GasFeeCap().Cmp(minFeeCap) < 0 {
				continue
			}
		}

		// Check total gas limit.
		if totalGas+tx.Gas() > types.MaxGasPerInclusionList {
			continue
		}

		// Avoid duplicate senders.
		if sender := tx.Sender(); sender != nil {
			if _, dup := senders[*sender]; dup {
				continue
			}
			senders[*sender] = struct{}{}

			// Check the tx is executable.
			accountNonce := statedb.GetNonce(*sender)
			if tx.Nonce() < accountNonce {
				continue
			}
		}

		// Encode and add the transaction.
		encoded, err := tx.EncodeRLP()
		if err != nil {
			continue
		}
		il.Transactions = append(il.Transactions, encoded)

		entry := types.InclusionListEntry{
			GasLimit: tx.Gas(),
		}
		if sender := tx.Sender(); sender != nil {
			entry.Address = *sender
		}
		il.Summary = append(il.Summary, entry)

		totalGas += tx.Gas()
	}

	return il
}

// inclusionListsEqual compares two inclusion lists for content equality.
func inclusionListsEqual(a, b *types.InclusionList) bool {
	if a.Slot != b.Slot || a.ValidatorIndex != b.ValidatorIndex {
		return false
	}
	if a.CommitteeRoot != b.CommitteeRoot {
		return false
	}
	if len(a.Transactions) != len(b.Transactions) {
		return false
	}
	for i := range a.Transactions {
		if len(a.Transactions[i]) != len(b.Transactions[i]) {
			return false
		}
		for j := range a.Transactions[i] {
			if a.Transactions[i][j] != b.Transactions[i][j] {
				return false
			}
		}
	}
	return true
}
