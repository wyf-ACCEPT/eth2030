// bid_escrow.go implements a collateral escrow system for ePBS builder bids
// per EIP-7732. Builders deposit collateral which is locked when placing bids.
// Collateral is released on successful delivery or slashed on failure.
package epbs

import (
	"errors"
	"fmt"
	"sync"
	"time"
)

// Bid escrow errors.
var (
	ErrEscrowNilBid            = errors.New("escrow: nil bid")
	ErrEscrowZeroDeposit       = errors.New("escrow: deposit amount must be > 0")
	ErrEscrowInsufficientFunds = errors.New("escrow: insufficient available balance")
	ErrEscrowDuplicateBid      = errors.New("escrow: bid already placed for slot")
	ErrEscrowNoBid             = errors.New("escrow: no bid found for slot")
	ErrEscrowAlreadyRevealed   = errors.New("escrow: payload already revealed for slot")
	ErrEscrowAlreadySettled    = errors.New("escrow: bid already settled for slot")
	ErrEscrowNoReveal          = errors.New("escrow: payload not yet revealed for slot")
	ErrEscrowNilPayload        = errors.New("escrow: nil payload envelope")
	ErrEscrowBuilderMismatch   = errors.New("escrow: builder ID does not match bid")
	ErrEscrowSlotMismatch      = errors.New("escrow: slot does not match bid")
	ErrEscrowPayloadMismatch   = errors.New("escrow: payload root does not match committed block hash")
	ErrEscrowZeroWithdraw      = errors.New("escrow: withdraw amount must be > 0")
	ErrEscrowUnknownBuilder    = errors.New("escrow: unknown builder")
	ErrEscrowZeroSlash         = errors.New("escrow: slash amount must be > 0")
)

// EscrowBidState tracks the lifecycle state of an escrowed bid.
type EscrowBidState uint8

const (
	// EscrowBidPending means the bid is placed and collateral is locked.
	EscrowBidPending EscrowBidState = iota
	// EscrowBidRevealed means the builder has revealed the payload.
	EscrowBidRevealed
	// EscrowBidSettledSuccess means collateral was released after successful delivery.
	EscrowBidSettledSuccess
	// EscrowBidSettledSlashed means collateral was slashed for failure.
	EscrowBidSettledSlashed
)

// String returns a human-readable name for EscrowBidState.
func (s EscrowBidState) String() string {
	switch s {
	case EscrowBidPending:
		return "pending"
	case EscrowBidRevealed:
		return "revealed"
	case EscrowBidSettledSuccess:
		return "settled_success"
	case EscrowBidSettledSlashed:
		return "settled_slashed"
	default:
		return "unknown"
	}
}

// SettlementResult records the outcome of settling a bid.
type SettlementResult struct {
	// Slot is the slot the bid was for.
	Slot uint64

	// BuilderID is the builder who placed the bid.
	BuilderID string

	// AmountReleased is the amount of collateral returned to the builder (Gwei).
	AmountReleased uint64

	// AmountSlashed is the amount of collateral slashed (Gwei).
	AmountSlashed uint64

	// Success indicates whether the builder delivered successfully.
	Success bool

	// Reason describes the settlement outcome.
	Reason string

	// SettledAt is the timestamp of settlement.
	SettledAt time.Time
}

// escrowBidEntry tracks a single bid in the escrow system.
type escrowBidEntry struct {
	bid        *BuilderBid
	builderID  string
	locked     uint64 // collateral locked for this bid (Gwei)
	state      EscrowBidState
	payload    *PayloadEnvelope
	placedAt   time.Time
	revealedAt time.Time
	settledAt  time.Time
}

// builderAccount tracks a builder's escrow balance.
type builderAccount struct {
	available uint64 // available (unlocked) balance in Gwei
	locked    uint64 // locked collateral in Gwei
}

// BidEscrow manages collateral deposits, bid locking, and settlement for
// ePBS builder bids. All public methods are safe for concurrent use.
type BidEscrow struct {
	mu       sync.RWMutex
	accounts map[string]*builderAccount  // builderID -> account
	bids     map[uint64]*escrowBidEntry  // slot -> bid entry
	results  []*SettlementResult         // settlement history
	maxResults int
}

// NewBidEscrow creates a new bid escrow system with the given maximum
// settlement result retention. If maxResults <= 0, defaults to 1024.
func NewBidEscrow(maxResults int) *BidEscrow {
	if maxResults <= 0 {
		maxResults = 1024
	}
	return &BidEscrow{
		accounts:   make(map[string]*builderAccount),
		bids:       make(map[uint64]*escrowBidEntry),
		maxResults: maxResults,
	}
}

// Deposit adds collateral to a builder's escrow account. The deposited
// amount becomes available for locking against future bids.
func (be *BidEscrow) Deposit(builderID string, amount uint64) error {
	if amount == 0 {
		return ErrEscrowZeroDeposit
	}

	be.mu.Lock()
	defer be.mu.Unlock()

	acct := be.getOrCreateAccount(builderID)
	acct.available += amount
	return nil
}

// PlaceBid places a bid and locks collateral equal to the bid value. The
// builder must have sufficient available balance. Only one bid per slot
// is allowed.
func (be *BidEscrow) PlaceBid(bid *BuilderBid) error {
	if bid == nil {
		return ErrEscrowNilBid
	}

	builderID := fmt.Sprintf("%d", bid.BuilderIndex)

	be.mu.Lock()
	defer be.mu.Unlock()

	// Check for duplicate bid on this slot.
	if _, exists := be.bids[bid.Slot]; exists {
		return fmt.Errorf("%w: slot %d", ErrEscrowDuplicateBid, bid.Slot)
	}

	acct := be.getOrCreateAccount(builderID)

	// Verify sufficient available balance.
	if acct.available < bid.Value {
		return fmt.Errorf("%w: need %d Gwei, have %d available",
			ErrEscrowInsufficientFunds, bid.Value, acct.available)
	}

	// Lock collateral.
	acct.available -= bid.Value
	acct.locked += bid.Value

	be.bids[bid.Slot] = &escrowBidEntry{
		bid:       bid,
		builderID: builderID,
		locked:    bid.Value,
		state:     EscrowBidPending,
		placedAt:  time.Now(),
	}

	return nil
}

// RevealPayload records that a builder has revealed the execution payload
// for a bid. The payload must match the bid's slot and builder index, and
// the payload root must match the committed block hash.
func (be *BidEscrow) RevealPayload(slot uint64, builderID string, payload *PayloadEnvelope) error {
	if payload == nil {
		return ErrEscrowNilPayload
	}

	be.mu.Lock()
	defer be.mu.Unlock()

	entry, ok := be.bids[slot]
	if !ok {
		return fmt.Errorf("%w: slot %d", ErrEscrowNoBid, slot)
	}

	if entry.builderID != builderID {
		return fmt.Errorf("%w: expected %s, got %s",
			ErrEscrowBuilderMismatch, entry.builderID, builderID)
	}

	if entry.state == EscrowBidRevealed || entry.state == EscrowBidSettledSuccess || entry.state == EscrowBidSettledSlashed {
		return fmt.Errorf("%w: slot %d, state %s",
			ErrEscrowAlreadyRevealed, slot, entry.state)
	}

	// Verify slot consistency.
	if payload.Slot != entry.bid.Slot {
		return fmt.Errorf("%w: bid slot %d, payload slot %d",
			ErrEscrowSlotMismatch, entry.bid.Slot, payload.Slot)
	}

	// Verify builder consistency.
	if payload.BuilderIndex != entry.bid.BuilderIndex {
		return fmt.Errorf("%w: bid builder %d, payload builder %d",
			ErrEscrowBuilderMismatch, entry.bid.BuilderIndex, payload.BuilderIndex)
	}

	// Verify the payload root matches the committed block hash.
	if payload.PayloadRoot != entry.bid.BlockHash {
		return fmt.Errorf("%w: committed %s, revealed %s",
			ErrEscrowPayloadMismatch, entry.bid.BlockHash.Hex(), payload.PayloadRoot.Hex())
	}

	entry.payload = payload
	entry.state = EscrowBidRevealed
	entry.revealedAt = time.Now()

	return nil
}

// SettleBid settles the bid for a slot. If the payload was revealed
// successfully, collateral is released. If not, collateral is slashed.
func (be *BidEscrow) SettleBid(slot uint64) (*SettlementResult, error) {
	be.mu.Lock()
	defer be.mu.Unlock()

	entry, ok := be.bids[slot]
	if !ok {
		return nil, fmt.Errorf("%w: slot %d", ErrEscrowNoBid, slot)
	}

	if entry.state == EscrowBidSettledSuccess || entry.state == EscrowBidSettledSlashed {
		return nil, fmt.Errorf("%w: slot %d, state %s",
			ErrEscrowAlreadySettled, slot, entry.state)
	}

	acct := be.getOrCreateAccount(entry.builderID)
	now := time.Now()
	var result *SettlementResult

	if entry.state == EscrowBidRevealed {
		// Successful delivery: release collateral.
		acct.locked -= entry.locked
		acct.available += entry.locked

		entry.state = EscrowBidSettledSuccess
		entry.settledAt = now

		result = &SettlementResult{
			Slot:           slot,
			BuilderID:      entry.builderID,
			AmountReleased: entry.locked,
			AmountSlashed:  0,
			Success:        true,
			Reason:         "payload revealed and validated",
			SettledAt:      now,
		}
	} else {
		// Failed delivery: slash collateral.
		slashedAmount := entry.locked
		acct.locked -= slashedAmount

		entry.state = EscrowBidSettledSlashed
		entry.settledAt = now

		result = &SettlementResult{
			Slot:           slot,
			BuilderID:      entry.builderID,
			AmountReleased: 0,
			AmountSlashed:  slashedAmount,
			Success:        false,
			Reason:         "payload not revealed before settlement",
			SettledAt:      now,
		}
	}

	be.results = append(be.results, result)
	be.trimResults()

	return result, nil
}

// GetBalance returns the available (unlocked) balance for a builder.
func (be *BidEscrow) GetBalance(builderID string) uint64 {
	be.mu.RLock()
	defer be.mu.RUnlock()

	acct, ok := be.accounts[builderID]
	if !ok {
		return 0
	}
	return acct.available
}

// GetLockedBalance returns the locked collateral for a builder.
func (be *BidEscrow) GetLockedBalance(builderID string) uint64 {
	be.mu.RLock()
	defer be.mu.RUnlock()

	acct, ok := be.accounts[builderID]
	if !ok {
		return 0
	}
	return acct.locked
}

// SlashBuilder manually slashes a builder's available balance for protocol
// violations. The slashed amount is deducted from the available balance;
// if insufficient, the remaining is taken from locked balance.
func (be *BidEscrow) SlashBuilder(builderID string, amount uint64, reason string) error {
	if amount == 0 {
		return ErrEscrowZeroSlash
	}

	be.mu.Lock()
	defer be.mu.Unlock()

	acct, ok := be.accounts[builderID]
	if !ok {
		return fmt.Errorf("%w: %s", ErrEscrowUnknownBuilder, builderID)
	}

	totalBalance := acct.available + acct.locked
	if totalBalance == 0 {
		return fmt.Errorf("%w: %s has zero total balance",
			ErrEscrowInsufficientFunds, builderID)
	}

	// Slash from available first, then locked if needed.
	if acct.available >= amount {
		acct.available -= amount
	} else {
		remaining := amount - acct.available
		acct.available = 0
		if acct.locked >= remaining {
			acct.locked -= remaining
		} else {
			acct.locked = 0
		}
	}

	// Record as a settlement result for audit trail.
	result := &SettlementResult{
		Slot:          0, // manual slash, not slot-specific
		BuilderID:     builderID,
		AmountSlashed: amount,
		Success:       false,
		Reason:        fmt.Sprintf("manual slash: %s", reason),
		SettledAt:     time.Now(),
	}
	be.results = append(be.results, result)
	be.trimResults()

	return nil
}

// WithdrawBalance withdraws available (unlocked) balance from a builder's
// escrow account.
func (be *BidEscrow) WithdrawBalance(builderID string, amount uint64) error {
	if amount == 0 {
		return ErrEscrowZeroWithdraw
	}

	be.mu.Lock()
	defer be.mu.Unlock()

	acct, ok := be.accounts[builderID]
	if !ok {
		return fmt.Errorf("%w: %s", ErrEscrowUnknownBuilder, builderID)
	}

	if acct.available < amount {
		return fmt.Errorf("%w: want to withdraw %d Gwei, have %d available",
			ErrEscrowInsufficientFunds, amount, acct.available)
	}

	acct.available -= amount
	return nil
}

// GetBidState returns the escrow state for a bid at the given slot.
// Returns EscrowBidPending and false if no bid is found.
func (be *BidEscrow) GetBidState(slot uint64) (EscrowBidState, bool) {
	be.mu.RLock()
	defer be.mu.RUnlock()

	entry, ok := be.bids[slot]
	if !ok {
		return EscrowBidPending, false
	}
	return entry.state, true
}

// GetBid returns the builder bid for a given slot. Returns nil if not found.
func (be *BidEscrow) GetBid(slot uint64) *BuilderBid {
	be.mu.RLock()
	defer be.mu.RUnlock()

	entry, ok := be.bids[slot]
	if !ok {
		return nil
	}
	// Return a copy to prevent external mutation.
	cp := *entry.bid
	return &cp
}

// ActiveBidCount returns the number of bids that are pending or revealed
// but not yet settled.
func (be *BidEscrow) ActiveBidCount() int {
	be.mu.RLock()
	defer be.mu.RUnlock()

	count := 0
	for _, entry := range be.bids {
		if entry.state == EscrowBidPending || entry.state == EscrowBidRevealed {
			count++
		}
	}
	return count
}

// SettlementHistory returns the last n settlement results for auditing.
func (be *BidEscrow) SettlementHistory(n int) []*SettlementResult {
	be.mu.RLock()
	defer be.mu.RUnlock()

	if n <= 0 || len(be.results) == 0 {
		return nil
	}
	if n > len(be.results) {
		n = len(be.results)
	}
	result := make([]*SettlementResult, n)
	copy(result, be.results[len(be.results)-n:])
	return result
}

// PruneBefore removes bid entries for slots before the given slot.
// Only settled bids are pruned; active bids are retained. Returns the
// number of entries pruned.
func (be *BidEscrow) PruneBefore(slot uint64) int {
	be.mu.Lock()
	defer be.mu.Unlock()

	pruned := 0
	for s, entry := range be.bids {
		if s < slot && (entry.state == EscrowBidSettledSuccess || entry.state == EscrowBidSettledSlashed) {
			delete(be.bids, s)
			pruned++
		}
	}
	return pruned
}

// --- Internal helpers ---

// getOrCreateAccount returns the builder's account, creating one if needed.
// Caller must hold be.mu.
func (be *BidEscrow) getOrCreateAccount(builderID string) *builderAccount {
	acct, ok := be.accounts[builderID]
	if !ok {
		acct = &builderAccount{}
		be.accounts[builderID] = acct
	}
	return acct
}

// trimResults trims the results list to maxResults. Caller must hold be.mu.
func (be *BidEscrow) trimResults() {
	if len(be.results) > be.maxResults {
		be.results = be.results[len(be.results)-be.maxResults:]
	}
}
