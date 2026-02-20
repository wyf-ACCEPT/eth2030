// payment.go implements ePBS payment processing for EIP-7732.
//
// In enshrined PBS, builders bid for the right to construct execution payloads.
// The winning builder commits a payment to the proposer. This module handles:
//
//   - Payment escrow: funds are locked when a builder's bid wins.
//   - Conditional release: payment is transferred to the proposer only after
//     the builder reveals a valid execution payload.
//   - Slashing: if the builder fails to deliver or delivers an invalid payload,
//     the escrowed funds are slashed (partially burned, partially compensated
//     to the proposer).
//   - Payment tracking: all payments are recorded for auditability.
//
// The PaymentProcessor is designed for use by the EL ePBS subsystem. It
// operates on abstract balances (Gwei) and does not interact with the
// consensus state directly.
package epbs

import (
	"errors"
	"fmt"
	"math/big"
	"sync"
	"time"

	"github.com/eth2028/eth2028/core/types"
)

// Payment processing errors.
var (
	ErrPaymentNilBid            = errors.New("payment: nil bid")
	ErrPaymentZeroValue         = errors.New("payment: bid value must be > 0")
	ErrPaymentInsufficientFunds = errors.New("payment: insufficient builder funds")
	ErrPaymentAlreadyEscrowed   = errors.New("payment: slot already has escrowed payment")
	ErrPaymentNotEscrowed       = errors.New("payment: no escrowed payment for slot")
	ErrPaymentAlreadySettled    = errors.New("payment: slot already settled")
	ErrPaymentInvalidPayload    = errors.New("payment: payload does not match committed bid")
	ErrPaymentSlotExpired       = errors.New("payment: slot has expired for settlement")
)

// PaymentState tracks the lifecycle of a payment through escrow.
type PaymentState int

const (
	// PaymentPending means the bid was accepted but not yet escrowed.
	PaymentPending PaymentState = iota
	// PaymentEscrowed means funds are locked in escrow.
	PaymentEscrowed
	// PaymentReleased means the proposer has been paid.
	PaymentReleased
	// PaymentSlashed means the builder was penalized.
	PaymentSlashed
	// PaymentRefunded means the escrow was returned to the builder
	// (e.g., proposer missed their slot).
	PaymentRefunded
)

// String returns a human-readable name for the payment state.
func (s PaymentState) String() string {
	switch s {
	case PaymentPending:
		return "Pending"
	case PaymentEscrowed:
		return "Escrowed"
	case PaymentReleased:
		return "Released"
	case PaymentSlashed:
		return "Slashed"
	case PaymentRefunded:
		return "Refunded"
	default:
		return "Unknown"
	}
}

// EscrowRecord tracks a single payment through its lifecycle.
type EscrowRecord struct {
	Slot           uint64        `json:"slot"`
	BuilderAddr    types.Address `json:"builderAddr"`
	ProposerAddr   types.Address `json:"proposerAddr"`
	BidValue       uint64        `json:"bidValue"`       // total bid in Gwei
	PaymentAmount  uint64        `json:"paymentAmount"`  // amount to proposer in Gwei
	State          PaymentState  `json:"state"`
	BidHash        types.Hash    `json:"bidHash"`
	PayloadHash    types.Hash    `json:"payloadHash"`    // committed block hash
	EscrowedAt     time.Time     `json:"escrowedAt"`
	SettledAt      time.Time     `json:"settledAt"`
	SlashAmount    uint64        `json:"slashAmount"`    // amount slashed (0 if not slashed)
	BurnAmount     uint64        `json:"burnAmount"`     // amount burned on slash
	CompensationAmt uint64       `json:"compensationAmt"` // compensation to proposer on slash
}

// PaymentConfig configures the payment processor.
type PaymentConfig struct {
	// SlashFraction is the fraction of the bid value slashed on builder
	// failure, expressed as basis points (e.g., 5000 = 50%).
	SlashFraction uint64

	// BurnFraction is the fraction of the slashed amount that is burned
	// (removed from circulation), in basis points of the slash amount.
	// The remainder goes to the proposer as compensation.
	BurnFraction uint64

	// SettlementDeadline is the number of slots after the bid slot within
	// which the payment must be settled. After this, the payment is
	// eligible for refund.
	SettlementDeadline uint64

	// MaxEscrowRecords is the maximum number of records to retain.
	MaxEscrowRecords int
}

// DefaultPaymentConfig returns sensible production defaults.
func DefaultPaymentConfig() PaymentConfig {
	return PaymentConfig{
		SlashFraction:      5000, // 50%
		BurnFraction:       5000, // 50% of slashed amount burned
		SettlementDeadline: 32,   // ~6.4 minutes at 12s slots
		MaxEscrowRecords:   1024,
	}
}

// PaymentProcessor manages ePBS builder payments. Thread-safe.
type PaymentProcessor struct {
	mu       sync.RWMutex
	config   PaymentConfig
	escrows  map[uint64]*EscrowRecord     // slot -> escrow
	balances map[types.Address]*big.Int    // builder/proposer balances
	history  []*EscrowRecord              // settled records for auditing
}

// NewPaymentProcessor creates a new payment processor with the given config.
func NewPaymentProcessor(config PaymentConfig) *PaymentProcessor {
	if config.SlashFraction > 10000 {
		config.SlashFraction = 10000
	}
	if config.BurnFraction > 10000 {
		config.BurnFraction = 10000
	}
	if config.MaxEscrowRecords <= 0 {
		config.MaxEscrowRecords = 1024
	}
	return &PaymentProcessor{
		config:   config,
		escrows:  make(map[uint64]*EscrowRecord),
		balances: make(map[types.Address]*big.Int),
	}
}

// SetBalance sets the balance for an address. Used for initialization
// and testing.
func (pp *PaymentProcessor) SetBalance(addr types.Address, amount *big.Int) {
	pp.mu.Lock()
	defer pp.mu.Unlock()
	pp.balances[addr] = new(big.Int).Set(amount)
}

// GetBalance returns the current balance of an address.
func (pp *PaymentProcessor) GetBalance(addr types.Address) *big.Int {
	pp.mu.RLock()
	defer pp.mu.RUnlock()
	bal, ok := pp.balances[addr]
	if !ok {
		return big.NewInt(0)
	}
	return new(big.Int).Set(bal)
}

// Escrow locks the builder's bid value for the given slot. The builder
// must have sufficient balance. Returns the escrow record.
func (pp *PaymentProcessor) Escrow(
	slot uint64,
	builderAddr types.Address,
	proposerAddr types.Address,
	bidValue uint64,
	bidHash types.Hash,
	payloadHash types.Hash,
) (*EscrowRecord, error) {
	if bidValue == 0 {
		return nil, ErrPaymentZeroValue
	}

	pp.mu.Lock()
	defer pp.mu.Unlock()

	if _, exists := pp.escrows[slot]; exists {
		return nil, fmt.Errorf("%w: slot %d", ErrPaymentAlreadyEscrowed, slot)
	}

	// Check builder balance.
	builderBal := pp.getBalance(builderAddr)
	bidWei := gweiToWei(bidValue)
	if builderBal.Cmp(bidWei) < 0 {
		return nil, fmt.Errorf("%w: need %s wei, have %s",
			ErrPaymentInsufficientFunds, bidWei.String(), builderBal.String())
	}

	// Deduct from builder balance.
	builderBal.Sub(builderBal, bidWei)
	pp.balances[builderAddr] = builderBal

	record := &EscrowRecord{
		Slot:          slot,
		BuilderAddr:   builderAddr,
		ProposerAddr:  proposerAddr,
		BidValue:      bidValue,
		PaymentAmount: bidValue,
		State:         PaymentEscrowed,
		BidHash:       bidHash,
		PayloadHash:   payloadHash,
		EscrowedAt:    time.Now(),
	}
	pp.escrows[slot] = record

	return record, nil
}

// ReleasePayment transfers the escrowed payment to the proposer after the
// builder delivers a valid payload. The deliveredPayloadHash must match
// the committed payload hash in the escrow.
func (pp *PaymentProcessor) ReleasePayment(slot uint64, deliveredPayloadHash types.Hash) (*EscrowRecord, error) {
	pp.mu.Lock()
	defer pp.mu.Unlock()

	record, ok := pp.escrows[slot]
	if !ok {
		return nil, fmt.Errorf("%w: slot %d", ErrPaymentNotEscrowed, slot)
	}
	if record.State != PaymentEscrowed {
		return nil, fmt.Errorf("%w: slot %d, state %s",
			ErrPaymentAlreadySettled, slot, record.State)
	}

	// Verify the delivered payload matches the committed hash.
	if deliveredPayloadHash != record.PayloadHash {
		return nil, fmt.Errorf("%w: committed %s, delivered %s",
			ErrPaymentInvalidPayload, record.PayloadHash.Hex(), deliveredPayloadHash.Hex())
	}

	// Credit the proposer.
	proposerBal := pp.getBalance(record.ProposerAddr)
	paymentWei := gweiToWei(record.PaymentAmount)
	proposerBal.Add(proposerBal, paymentWei)
	pp.balances[record.ProposerAddr] = proposerBal

	record.State = PaymentReleased
	record.SettledAt = time.Now()

	pp.archiveRecord(record)
	return record, nil
}

// SlashBuilder penalizes the builder for failing to deliver a valid payload.
// A fraction of the escrowed amount is burned, and the remainder is
// compensated to the proposer.
func (pp *PaymentProcessor) SlashBuilder(slot uint64) (*EscrowRecord, error) {
	pp.mu.Lock()
	defer pp.mu.Unlock()

	record, ok := pp.escrows[slot]
	if !ok {
		return nil, fmt.Errorf("%w: slot %d", ErrPaymentNotEscrowed, slot)
	}
	if record.State != PaymentEscrowed {
		return nil, fmt.Errorf("%w: slot %d, state %s",
			ErrPaymentAlreadySettled, slot, record.State)
	}

	bidWei := gweiToWei(record.BidValue)

	// Compute slash amount: (bidValue * slashFraction) / 10000.
	slashWei := new(big.Int).Mul(bidWei, big.NewInt(int64(pp.config.SlashFraction)))
	slashWei.Div(slashWei, big.NewInt(10000))

	// Compute burn amount: (slashAmount * burnFraction) / 10000.
	burnWei := new(big.Int).Mul(slashWei, big.NewInt(int64(pp.config.BurnFraction)))
	burnWei.Div(burnWei, big.NewInt(10000))

	// Compensation to proposer: slash - burn.
	compensationWei := new(big.Int).Sub(slashWei, burnWei)

	// Credit proposer with compensation.
	proposerBal := pp.getBalance(record.ProposerAddr)
	proposerBal.Add(proposerBal, compensationWei)
	pp.balances[record.ProposerAddr] = proposerBal

	// Return unslashed portion to builder.
	refundWei := new(big.Int).Sub(bidWei, slashWei)
	if refundWei.Sign() > 0 {
		builderBal := pp.getBalance(record.BuilderAddr)
		builderBal.Add(builderBal, refundWei)
		pp.balances[record.BuilderAddr] = builderBal
	}

	record.State = PaymentSlashed
	record.SettledAt = time.Now()
	record.SlashAmount = weiToGwei(slashWei)
	record.BurnAmount = weiToGwei(burnWei)
	record.CompensationAmt = weiToGwei(compensationWei)

	pp.archiveRecord(record)
	return record, nil
}

// RefundEscrow returns escrowed funds to the builder (e.g., when the
// proposer missed their slot and the settlement deadline has passed).
func (pp *PaymentProcessor) RefundEscrow(slot uint64, currentSlot uint64) (*EscrowRecord, error) {
	pp.mu.Lock()
	defer pp.mu.Unlock()

	record, ok := pp.escrows[slot]
	if !ok {
		return nil, fmt.Errorf("%w: slot %d", ErrPaymentNotEscrowed, slot)
	}
	if record.State != PaymentEscrowed {
		return nil, fmt.Errorf("%w: slot %d, state %s",
			ErrPaymentAlreadySettled, slot, record.State)
	}

	// Only allow refund after the settlement deadline.
	if currentSlot < slot+pp.config.SettlementDeadline {
		return nil, fmt.Errorf("%w: current slot %d, deadline slot %d",
			ErrPaymentSlotExpired, currentSlot, slot+pp.config.SettlementDeadline)
	}

	// Return full amount to builder.
	builderBal := pp.getBalance(record.BuilderAddr)
	bidWei := gweiToWei(record.BidValue)
	builderBal.Add(builderBal, bidWei)
	pp.balances[record.BuilderAddr] = builderBal

	record.State = PaymentRefunded
	record.SettledAt = time.Now()

	pp.archiveRecord(record)
	return record, nil
}

// GetEscrow returns the escrow record for a slot, or nil if none exists.
func (pp *PaymentProcessor) GetEscrow(slot uint64) *EscrowRecord {
	pp.mu.RLock()
	defer pp.mu.RUnlock()
	return pp.escrows[slot]
}

// ActiveEscrowCount returns the number of currently escrowed payments.
func (pp *PaymentProcessor) ActiveEscrowCount() int {
	pp.mu.RLock()
	defer pp.mu.RUnlock()
	count := 0
	for _, r := range pp.escrows {
		if r.State == PaymentEscrowed {
			count++
		}
	}
	return count
}

// History returns the last n settled escrow records for auditing.
func (pp *PaymentProcessor) History(n int) []*EscrowRecord {
	pp.mu.RLock()
	defer pp.mu.RUnlock()

	if n <= 0 || len(pp.history) == 0 {
		return nil
	}
	if n > len(pp.history) {
		n = len(pp.history)
	}
	result := make([]*EscrowRecord, n)
	copy(result, pp.history[len(pp.history)-n:])
	return result
}

// PruneBefore removes escrow records for slots before the given slot.
func (pp *PaymentProcessor) PruneBefore(slot uint64) int {
	pp.mu.Lock()
	defer pp.mu.Unlock()

	pruned := 0
	for s, record := range pp.escrows {
		if s < slot && record.State != PaymentEscrowed {
			delete(pp.escrows, s)
			pruned++
		}
	}
	return pruned
}

// getBalance returns the balance for an address, initializing to zero
// if not present. Caller must hold pp.mu.
func (pp *PaymentProcessor) getBalance(addr types.Address) *big.Int {
	bal, ok := pp.balances[addr]
	if !ok {
		bal = big.NewInt(0)
		pp.balances[addr] = bal
	}
	return bal
}

// archiveRecord moves a settled record to the history. Caller must hold pp.mu.
func (pp *PaymentProcessor) archiveRecord(record *EscrowRecord) {
	pp.history = append(pp.history, record)
	if len(pp.history) > pp.config.MaxEscrowRecords {
		pp.history = pp.history[len(pp.history)-pp.config.MaxEscrowRecords:]
	}
}

// gweiToWei converts Gwei to Wei (1 Gwei = 10^9 Wei).
func gweiToWei(gwei uint64) *big.Int {
	return new(big.Int).Mul(
		new(big.Int).SetUint64(gwei),
		big.NewInt(1_000_000_000),
	)
}

// weiToGwei converts Wei to Gwei (integer division, truncating).
func weiToGwei(wei *big.Int) uint64 {
	if wei == nil || wei.Sign() <= 0 {
		return 0
	}
	gwei := new(big.Int).Div(wei, big.NewInt(1_000_000_000))
	return gwei.Uint64()
}
