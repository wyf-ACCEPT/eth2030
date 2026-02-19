// subaccounts.go implements the 1M Subaccounts system from the Ethereum 2028
// roadmap (CL Accessibility track). It allows a main account to manage up to
// 1 million deterministically derived subaccounts, enabling rich delegation
// and distributed block building patterns.
package state

import (
	"encoding/binary"
	"errors"
	"fmt"
	"sync"

	"github.com/eth2028/eth2028/core/types"
	"github.com/eth2028/eth2028/crypto"
)

// MaxSubaccountsPerParent is the maximum number of subaccounts a single
// parent account may create (1 million, per the 2028 roadmap spec).
const MaxSubaccountsPerParent = 1_000_000

var (
	ErrMaxSubaccountsReached = errors.New("subaccounts: maximum subaccounts reached for parent")
	ErrSubaccountExists      = errors.New("subaccounts: subaccount already exists")
	ErrSubaccountNotFound    = errors.New("subaccounts: subaccount not found")
	ErrNotSubaccount         = errors.New("subaccounts: address is not a subaccount")
	ErrSameAccount           = errors.New("subaccounts: from and to addresses are the same")
	ErrInsufficientBalance   = errors.New("subaccounts: insufficient balance for transfer")
	ErrDifferentParent       = errors.New("subaccounts: accounts do not share the same parent")
	ErrIndexOutOfRange       = errors.New("subaccounts: index exceeds maximum")
)

// SubaccountState holds the full state of a single subaccount.
type SubaccountState struct {
	Address       types.Address
	ParentAddress types.Address
	Index         uint32
	Balance       uint64
	Nonce         uint64
}

// SubaccountManager manages the lifecycle and transfers of subaccounts.
// It is safe for concurrent use.
type SubaccountManager struct {
	mu sync.RWMutex

	// parent -> list of derived subaccount addresses (in creation order)
	children map[types.Address][]types.Address
	// subaccount address -> parent address
	parentOf map[types.Address]types.Address
	// subaccount address -> full state
	states map[types.Address]*SubaccountState
}

// NewSubaccountManager creates an empty SubaccountManager.
func NewSubaccountManager() *SubaccountManager {
	return &SubaccountManager{
		children: make(map[types.Address][]types.Address),
		parentOf: make(map[types.Address]types.Address),
		states:   make(map[types.Address]*SubaccountState),
	}
}

// DeriveSubaccountAddress deterministically derives a subaccount address from
// the parent address and a uint32 index using keccak256(parent || index).
// The result is truncated to 20 bytes (standard Ethereum address).
func (sm *SubaccountManager) DeriveSubaccountAddress(parent types.Address, index uint32) types.Address {
	var buf [types.AddressLength + 4]byte
	copy(buf[:types.AddressLength], parent[:])
	binary.BigEndian.PutUint32(buf[types.AddressLength:], index)
	hash := crypto.Keccak256(buf[:])
	return types.BytesToAddress(hash[12:]) // last 20 bytes, matching Ethereum convention
}

// CreateSubaccount creates a new subaccount under parent at the given index.
// Returns the derived address or an error if the limit is reached or the
// subaccount already exists.
func (sm *SubaccountManager) CreateSubaccount(parent types.Address, index uint32) (types.Address, error) {
	if index >= MaxSubaccountsPerParent {
		return types.Address{}, ErrIndexOutOfRange
	}

	addr := sm.DeriveSubaccountAddress(parent, index)

	sm.mu.Lock()
	defer sm.mu.Unlock()

	// Check per-parent limit.
	if len(sm.children[parent]) >= MaxSubaccountsPerParent {
		return types.Address{}, ErrMaxSubaccountsReached
	}

	// Reject duplicates.
	if _, exists := sm.states[addr]; exists {
		return types.Address{}, ErrSubaccountExists
	}

	st := &SubaccountState{
		Address:       addr,
		ParentAddress: parent,
		Index:         index,
		Balance:       0,
		Nonce:         0,
	}
	sm.states[addr] = st
	sm.parentOf[addr] = parent
	sm.children[parent] = append(sm.children[parent], addr)

	return addr, nil
}

// GetParent returns the parent address for a subaccount.
// The second return value is false when addr is not a known subaccount.
func (sm *SubaccountManager) GetParent(subaccount types.Address) (types.Address, bool) {
	sm.mu.RLock()
	defer sm.mu.RUnlock()
	p, ok := sm.parentOf[subaccount]
	return p, ok
}

// ListSubaccounts returns all subaccount addresses belonging to parent,
// in creation order. Returns nil if parent has no subaccounts.
func (sm *SubaccountManager) ListSubaccounts(parent types.Address) []types.Address {
	sm.mu.RLock()
	defer sm.mu.RUnlock()
	children := sm.children[parent]
	if len(children) == 0 {
		return nil
	}
	out := make([]types.Address, len(children))
	copy(out, children)
	return out
}

// IsSubaccount reports whether addr is a registered subaccount.
func (sm *SubaccountManager) IsSubaccount(addr types.Address) bool {
	sm.mu.RLock()
	defer sm.mu.RUnlock()
	_, ok := sm.states[addr]
	return ok
}

// TransferBetweenSubaccounts atomically transfers amount from one subaccount
// to another. Both must exist and share the same parent.
func (sm *SubaccountManager) TransferBetweenSubaccounts(from, to types.Address, amount uint64) error {
	if from == to {
		return ErrSameAccount
	}

	sm.mu.Lock()
	defer sm.mu.Unlock()

	fromState, ok := sm.states[from]
	if !ok {
		return fmt.Errorf("%w: from %s", ErrSubaccountNotFound, from.Hex())
	}
	toState, ok := sm.states[to]
	if !ok {
		return fmt.Errorf("%w: to %s", ErrSubaccountNotFound, to.Hex())
	}

	if fromState.ParentAddress != toState.ParentAddress {
		return ErrDifferentParent
	}

	if fromState.Balance < amount {
		return ErrInsufficientBalance
	}

	fromState.Balance -= amount
	toState.Balance += amount
	return nil
}

// GetSubaccountState returns a copy of the subaccount state for addr.
func (sm *SubaccountManager) GetSubaccountState(addr types.Address) (*SubaccountState, error) {
	sm.mu.RLock()
	defer sm.mu.RUnlock()
	st, ok := sm.states[addr]
	if !ok {
		return nil, ErrSubaccountNotFound
	}
	// Return a copy so callers cannot mutate internal state.
	cp := *st
	return &cp, nil
}
