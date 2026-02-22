// one_eth_includers.go implements the 1 ETH includer mechanism (L+ era).
// This allows any Ethereum participant to stake exactly 1 ETH and serve as
// a transaction includer, democratising the block building process. Includers
// are selected pseudorandomly per slot to build an ordered list of transactions
// that must be included by the block proposer (FOCIL-adjacent).
package consensus

import (
	"encoding/binary"
	"errors"
	"math/big"
	"sync"

	"github.com/eth2030/eth2030/core/types"
	"github.com/eth2030/eth2030/crypto"
)

// 1 ETH includer errors.
var (
	ErrIncluderZeroAddress    = errors.New("includer: zero address")
	ErrIncluderWrongStake     = errors.New("includer: stake must be exactly 1 ETH (1e18 wei)")
	ErrIncluderAlreadyRegistered = errors.New("includer: already registered")
	ErrIncluderNotRegistered  = errors.New("includer: not registered")
	ErrIncluderPoolEmpty      = errors.New("includer: pool is empty")
	ErrIncluderAlreadySlashed = errors.New("includer: already slashed")
	ErrIncluderNilDuty        = errors.New("includer: nil duty")
	ErrIncluderInvalidSig     = errors.New("includer: invalid signature")
)

// OneETH is the required stake amount: 1 ETH = 1e18 wei.
var OneETH = new(big.Int).Exp(big.NewInt(10), big.NewInt(18), nil)

// Includer reward constants (in Gwei).
const (
	BaseIncluderReward    uint64 = 10_000  // 10,000 Gwei per slot
	IncluderRewardDecay   uint64 = 100     // decay per slot for late inclusion
	SlashPenaltyPercent   uint64 = 10      // 10% of stake slashed for misbehaviour
)

// IncluderStatus tracks the state of a registered includer.
type IncluderStatus uint8

const (
	IncluderActive  IncluderStatus = 0
	IncluderSlashed IncluderStatus = 1
	IncluderExited  IncluderStatus = 2
)

// String returns a human-readable status name.
func (s IncluderStatus) String() string {
	switch s {
	case IncluderActive:
		return "active"
	case IncluderSlashed:
		return "slashed"
	case IncluderExited:
		return "exited"
	default:
		return "unknown"
	}
}

// IncluderRecord holds the registration data for a single includer.
type IncluderRecord struct {
	Address      types.Address
	Stake        *big.Int
	Status       IncluderStatus
	SlashReason  string
	RegisteredAt uint64 // slot at which the includer registered
}

// IncluderDuty represents the assigned duty for an includer in a given slot.
type IncluderDuty struct {
	Slot       Slot
	Includer   types.Address
	TxListHash types.Hash
	Deadline   uint64 // unix timestamp by which the duty must be fulfilled
}

// Hash returns the canonical hash of the includer duty.
func (d *IncluderDuty) Hash() types.Hash {
	var buf []byte
	buf = binary.BigEndian.AppendUint64(buf, uint64(d.Slot))
	buf = append(buf, d.Includer[:]...)
	buf = append(buf, d.TxListHash[:]...)
	buf = binary.BigEndian.AppendUint64(buf, d.Deadline)
	return crypto.Keccak256Hash(buf)
}

// IncluderPool manages the set of registered 1 ETH includers.
// Thread-safe for concurrent access.
type IncluderPool struct {
	mu        sync.RWMutex
	includers map[types.Address]*IncluderRecord
	// ordered is a deterministic ordering of active includer addresses
	// used for slot selection. Rebuilt on registration/unregistration.
	ordered []types.Address
}

// NewIncluderPool creates a new empty includer pool.
func NewIncluderPool() *IncluderPool {
	return &IncluderPool{
		includers: make(map[types.Address]*IncluderRecord),
		ordered:   make([]types.Address, 0),
	}
}

// RegisterIncluder adds an includer to the pool. The stake must be exactly
// 1 ETH (1e18 wei). Returns an error if the address is zero, the stake is
// incorrect, or the includer is already registered.
func (p *IncluderPool) RegisterIncluder(addr types.Address, stake *big.Int) error {
	if addr.IsZero() {
		return ErrIncluderZeroAddress
	}
	if stake == nil || stake.Cmp(OneETH) != 0 {
		return ErrIncluderWrongStake
	}

	p.mu.Lock()
	defer p.mu.Unlock()

	if _, exists := p.includers[addr]; exists {
		return ErrIncluderAlreadyRegistered
	}

	p.includers[addr] = &IncluderRecord{
		Address: addr,
		Stake:   new(big.Int).Set(stake),
		Status:  IncluderActive,
	}
	p.ordered = append(p.ordered, addr)
	return nil
}

// UnregisterIncluder removes an includer from the pool and marks it as exited.
func (p *IncluderPool) UnregisterIncluder(addr types.Address) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	record, exists := p.includers[addr]
	if !exists {
		return ErrIncluderNotRegistered
	}

	record.Status = IncluderExited
	delete(p.includers, addr)
	p.rebuildOrderedLocked()
	return nil
}

// SelectIncluder deterministically selects an active includer for the given
// slot using the provided random seed. Selection is based on:
//
//	index = H(slot || seed) mod len(activeIncluders)
func (p *IncluderPool) SelectIncluder(slot Slot, randomSeed types.Hash) (types.Address, error) {
	p.mu.RLock()
	defer p.mu.RUnlock()

	active := p.activeIncludersLocked()
	if len(active) == 0 {
		return types.Address{}, ErrIncluderPoolEmpty
	}

	// Compute selection index: H(slot || seed).
	var buf []byte
	buf = binary.BigEndian.AppendUint64(buf, uint64(slot))
	buf = append(buf, randomSeed[:]...)
	h := crypto.Keccak256(buf)

	// Use the first 8 bytes as a uint64 index.
	idx := binary.BigEndian.Uint64(h[:8]) % uint64(len(active))
	return active[idx], nil
}

// VerifyIncluderSignature verifies that a duty was signed by the assigned
// includer. The signature is a 65-byte ECDSA signature [R || S || V].
func VerifyIncluderSignature(duty *IncluderDuty, sig []byte) bool {
	if duty == nil || len(sig) != 65 {
		return false
	}

	dutyHash := duty.Hash()
	pubkey, err := crypto.Ecrecover(dutyHash[:], sig)
	if err != nil {
		return false
	}

	// Derive address from recovered public key.
	addrHash := crypto.Keccak256(pubkey[1:])
	var recovered types.Address
	copy(recovered[:], addrHash[12:])

	return recovered == duty.Includer
}

// IncluderReward calculates the reward for a correctly fulfilled includer
// duty at the given slot. Later slots receive a decayed reward.
func IncluderReward(slot Slot) uint64 {
	base := BaseIncluderReward
	// Apply slot-proportional decay (capped so reward doesn't go negative).
	decay := uint64(slot) % 32 * IncluderRewardDecay
	if decay >= base {
		return 1 // minimum reward of 1 Gwei
	}
	return base - decay
}

// SlashIncluder marks an includer as slashed for the given reason. The
// includer is removed from the active set but remains in the pool with
// SlashedStatus for accountability.
func (p *IncluderPool) SlashIncluder(addr types.Address, reason string) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	record, exists := p.includers[addr]
	if !exists {
		return ErrIncluderNotRegistered
	}
	if record.Status == IncluderSlashed {
		return ErrIncluderAlreadySlashed
	}

	record.Status = IncluderSlashed
	record.SlashReason = reason

	// Reduce stake by SlashPenaltyPercent.
	penalty := new(big.Int).Div(record.Stake, big.NewInt(int64(100/SlashPenaltyPercent)))
	record.Stake.Sub(record.Stake, penalty)

	p.rebuildOrderedLocked()
	return nil
}

// GetIncluder returns the record for an includer. Returns nil if not found.
func (p *IncluderPool) GetIncluder(addr types.Address) *IncluderRecord {
	p.mu.RLock()
	defer p.mu.RUnlock()

	record, exists := p.includers[addr]
	if !exists {
		return nil
	}
	// Return a copy.
	cp := *record
	cp.Stake = new(big.Int).Set(record.Stake)
	return &cp
}

// ActiveCount returns the number of currently active includers.
func (p *IncluderPool) ActiveCount() int {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return len(p.activeIncludersLocked())
}

// TotalCount returns the total number of includers (including slashed).
func (p *IncluderPool) TotalCount() int {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return len(p.includers)
}

// --- Internal helpers ---

// activeIncludersLocked returns addresses of all active (non-slashed) includers.
// Must be called with at least a read lock held.
func (p *IncluderPool) activeIncludersLocked() []types.Address {
	var active []types.Address
	for _, addr := range p.ordered {
		if r, ok := p.includers[addr]; ok && r.Status == IncluderActive {
			active = append(active, addr)
		}
	}
	return active
}

// rebuildOrderedLocked rebuilds the deterministic ordering of includers.
// Must be called with a write lock held.
func (p *IncluderPool) rebuildOrderedLocked() {
	p.ordered = make([]types.Address, 0, len(p.includers))
	for addr, r := range p.includers {
		if r.Status == IncluderActive {
			p.ordered = append(p.ordered, addr)
		}
	}
	// Sort by address bytes for determinism.
	sortAddresses(p.ordered)
}

// sortAddresses sorts a slice of addresses lexicographically.
func sortAddresses(addrs []types.Address) {
	for i := 1; i < len(addrs); i++ {
		for j := i; j > 0 && addrLess(addrs[j], addrs[j-1]); j-- {
			addrs[j], addrs[j-1] = addrs[j-1], addrs[j]
		}
	}
}

// addrLess returns true if a < b lexicographically.
func addrLess(a, b types.Address) bool {
	for i := 0; i < types.AddressLength; i++ {
		if a[i] != b[i] {
			return a[i] < b[i]
		}
	}
	return false
}
