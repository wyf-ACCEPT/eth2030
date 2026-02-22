package vm

// eip7708_deep.go deepens EIP-7708: ETH transfers via system calls with
// detailed log event accounting, gas costs for system-initiated transfers,
// and transfer tracking for receipts and block processing.

import (
	"errors"
	"fmt"
	"math/big"

	"github.com/eth2030/eth2030/core/types"
)

// EIP-7708 gas constants for system-initiated transfers.
const (
	// GasTransferLog is the gas cost for emitting a Transfer log (LOG3).
	// base(375) + 3*topics(375) + 32 bytes data (8*32=256) = 1756.
	GasTransferLog uint64 = GasLog + 3*GasLogTopic + 32*GasLogData

	// GasTransferBurnLog is the gas cost for emitting a Burn log (LOG2).
	// base(375) + 2*topics(375) + 32 bytes data (8*32=256) = 1381.
	GasTransferBurnLog uint64 = GasLog + 2*GasLogTopic + 32*GasLogData

	// GasSystemTransferBase is the base gas cost for a system-initiated
	// transfer (e.g., block rewards, withdrawal processing). This covers
	// the state write to both the sender and recipient balance slots.
	GasSystemTransferBase uint64 = 6000

	// TransferLogDataSize is the size of the amount field in transfer logs.
	TransferLogDataSize = 32
)

// EIP-7708 errors.
var (
	ErrTransferInsufficientBalance = errors.New("eip7708: insufficient balance for transfer")
	ErrTransferNilState            = errors.New("eip7708: nil state database")
	ErrTransferZeroAmount          = errors.New("eip7708: zero amount transfer")
	ErrTransferSelfSend            = errors.New("eip7708: transfer to self")
)

// TransferDirection describes the direction of an ETH transfer.
type TransferDirection uint8

const (
	// TransferDirectionOut represents a transfer from the tracked address.
	TransferDirectionOut TransferDirection = iota
	// TransferDirectionIn represents a transfer to the tracked address.
	TransferDirectionIn
	// TransferDirectionBurn represents an ETH burn.
	TransferDirectionBurn
)

// String returns a human-readable direction label.
func (d TransferDirection) String() string {
	switch d {
	case TransferDirectionOut:
		return "out"
	case TransferDirectionIn:
		return "in"
	case TransferDirectionBurn:
		return "burn"
	default:
		return "unknown"
	}
}

// TransferEvent represents a recorded EIP-7708 transfer event.
type TransferEvent struct {
	From      types.Address     `json:"from"`
	To        types.Address     `json:"to"`
	Amount    *big.Int          `json:"amount"`
	Direction TransferDirection `json:"direction"`
	LogIndex  int               `json:"logIndex"`
	IsSystem  bool              `json:"isSystem"` // true if system-initiated
	IsBurn    bool              `json:"isBurn"`   // true for self-destruct to self
}

// String returns a formatted representation of the transfer event.
func (e *TransferEvent) String() string {
	if e.IsBurn {
		return fmt.Sprintf("Burn(%s, %s)", e.From.Hex(), e.Amount.String())
	}
	return fmt.Sprintf("Transfer(%s -> %s, %s)", e.From.Hex(), e.To.Hex(), e.Amount.String())
}

// TransferTracker collects EIP-7708 transfer events during block execution.
// It is used by the block processor to build receipts and for analytics.
type TransferTracker struct {
	events     []TransferEvent
	totalIn    *big.Int // total ETH received across all transfers
	totalOut   *big.Int // total ETH sent across all transfers
	totalBurns *big.Int // total ETH burned
}

// NewTransferTracker creates a new TransferTracker.
func NewTransferTracker() *TransferTracker {
	return &TransferTracker{
		totalIn:    new(big.Int),
		totalOut:   new(big.Int),
		totalBurns: new(big.Int),
	}
}

// RecordTransfer records an EIP-7708 transfer event.
func (t *TransferTracker) RecordTransfer(from, to types.Address, amount *big.Int, isSystem bool) {
	if amount == nil || amount.Sign() <= 0 {
		return
	}
	event := TransferEvent{
		From:      from,
		To:        to,
		Amount:    new(big.Int).Set(amount),
		Direction: TransferDirectionOut,
		LogIndex:  len(t.events),
		IsSystem:  isSystem,
	}
	t.events = append(t.events, event)
	t.totalOut.Add(t.totalOut, amount)
}

// RecordBurn records an EIP-7708 burn event.
func (t *TransferTracker) RecordBurn(addr types.Address, amount *big.Int) {
	if amount == nil || amount.Sign() <= 0 {
		return
	}
	event := TransferEvent{
		From:      addr,
		To:        types.Address{}, // zero address for burns
		Amount:    new(big.Int).Set(amount),
		Direction: TransferDirectionBurn,
		LogIndex:  len(t.events),
		IsBurn:    true,
	}
	t.events = append(t.events, event)
	t.totalBurns.Add(t.totalBurns, amount)
}

// Events returns all recorded transfer events.
func (t *TransferTracker) Events() []TransferEvent {
	return t.events
}

// EventCount returns the number of recorded events.
func (t *TransferTracker) EventCount() int {
	return len(t.events)
}

// TotalOut returns the total ETH sent in all recorded transfers.
func (t *TransferTracker) TotalOut() *big.Int {
	return new(big.Int).Set(t.totalOut)
}

// TotalBurns returns the total ETH burned.
func (t *TransferTracker) TotalBurns() *big.Int {
	return new(big.Int).Set(t.totalBurns)
}

// Reset clears all recorded events.
func (t *TransferTracker) Reset() {
	t.events = t.events[:0]
	t.totalIn.SetUint64(0)
	t.totalOut.SetUint64(0)
	t.totalBurns.SetUint64(0)
}

// SystemTransfer executes a system-initiated ETH transfer with EIP-7708
// logging. System transfers are used for block rewards, withdrawals, and
// other protocol-level ETH movements. Returns the gas used and any error.
func SystemTransfer(statedb StateDB, from, to types.Address, amount *big.Int, rules ForkRules) (uint64, error) {
	if statedb == nil {
		return 0, ErrTransferNilState
	}
	if amount == nil || amount.Sign() <= 0 {
		return 0, nil // no-op for zero/nil amounts
	}

	// Check sender balance.
	balance := statedb.GetBalance(from)
	if balance.Cmp(amount) < 0 {
		return 0, fmt.Errorf("%w: %s has %s, needs %s",
			ErrTransferInsufficientBalance, from.Hex(), balance.String(), amount.String())
	}

	// Execute the transfer.
	statedb.SubBalance(from, amount)
	statedb.AddBalance(to, amount)

	// Emit the EIP-7708 log if the fork is active.
	var gasUsed uint64
	if rules.IsEIP7708 {
		if from == to {
			// Self-transfer: no log (ETH didn't actually move).
			gasUsed = GasSystemTransferBase
		} else {
			EmitTransferLog(statedb, from, to, amount)
			gasUsed = GasSystemTransferBase + GasTransferLog
		}
	} else {
		gasUsed = GasSystemTransferBase
	}

	return gasUsed, nil
}

// SystemBurn executes a system-initiated ETH burn with EIP-7708 logging.
// Burns destroy ETH from an address (e.g., EIP-1559 base fee burning).
func SystemBurn(statedb StateDB, addr types.Address, amount *big.Int, rules ForkRules) (uint64, error) {
	if statedb == nil {
		return 0, ErrTransferNilState
	}
	if amount == nil || amount.Sign() <= 0 {
		return 0, nil
	}

	balance := statedb.GetBalance(addr)
	if balance.Cmp(amount) < 0 {
		return 0, fmt.Errorf("%w: %s has %s, needs %s",
			ErrTransferInsufficientBalance, addr.Hex(), balance.String(), amount.String())
	}

	statedb.SubBalance(addr, amount)

	var gasUsed uint64
	if rules.IsEIP7708 {
		EmitBurnLog(statedb, addr, amount)
		gasUsed = GasSystemTransferBase + GasTransferBurnLog
	} else {
		gasUsed = GasSystemTransferBase
	}

	return gasUsed, nil
}

// TransferLogGasCost returns the gas cost of emitting an EIP-7708 transfer
// or burn log. If the fork is not active, returns 0.
func TransferLogGasCost(isBurn bool, rules ForkRules) uint64 {
	if !rules.IsEIP7708 {
		return 0
	}
	if isBurn {
		return GasTransferBurnLog
	}
	return GasTransferLog
}

// DecodeTransferLog decodes an EIP-7708 transfer log back into its
// constituent fields. Returns an error if the log is not a valid transfer log.
func DecodeTransferLog(log *types.Log) (from, to types.Address, amount *big.Int, err error) {
	if log == nil {
		return types.Address{}, types.Address{}, nil, errors.New("nil log")
	}
	if log.Address != SystemAddress {
		return types.Address{}, types.Address{}, nil, errors.New("not a system log")
	}
	if len(log.Topics) != 3 || log.Topics[0] != TransferEventTopic {
		return types.Address{}, types.Address{}, nil, errors.New("not a Transfer log")
	}
	if len(log.Data) != TransferLogDataSize {
		return types.Address{}, types.Address{}, nil, fmt.Errorf("invalid data size: %d", len(log.Data))
	}

	// Topic 1: from (last 20 bytes of 32-byte topic).
	copy(from[:], log.Topics[1][12:])
	// Topic 2: to.
	copy(to[:], log.Topics[2][12:])
	// Data: amount (big-endian uint256).
	amount = new(big.Int).SetBytes(log.Data)

	return from, to, amount, nil
}

// DecodeBurnLog decodes an EIP-7708 burn log.
func DecodeBurnLog(log *types.Log) (addr types.Address, amount *big.Int, err error) {
	if log == nil {
		return types.Address{}, nil, errors.New("nil log")
	}
	if log.Address != SystemAddress {
		return types.Address{}, nil, errors.New("not a system log")
	}
	if len(log.Topics) != 2 || log.Topics[0] != BurnEventTopic {
		return types.Address{}, nil, errors.New("not a Burn log")
	}
	if len(log.Data) != TransferLogDataSize {
		return types.Address{}, nil, fmt.Errorf("invalid data size: %d", len(log.Data))
	}

	copy(addr[:], log.Topics[1][12:])
	amount = new(big.Int).SetBytes(log.Data)

	return addr, amount, nil
}

// IsTransferLog returns true if the given log is an EIP-7708 transfer log.
func IsTransferLog(log *types.Log) bool {
	return log != nil &&
		log.Address == SystemAddress &&
		len(log.Topics) == 3 &&
		log.Topics[0] == TransferEventTopic
}

// IsBurnLog returns true if the given log is an EIP-7708 burn log.
func IsBurnLog(log *types.Log) bool {
	return log != nil &&
		log.Address == SystemAddress &&
		len(log.Topics) == 2 &&
		log.Topics[0] == BurnEventTopic
}
