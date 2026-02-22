// gas_market.go implements a long-dated gas futures market (M+ upgrade).
// It allows buyers and sellers to trade future gas capacity at a fixed
// price per unit, settling at maturity based on the actual gas price.
package core

import (
	"encoding/binary"
	"errors"
	"math/big"
	"sync"

	"github.com/eth2030/eth2030/core/types"
	"github.com/eth2030/eth2030/crypto"
)

// Contract status lifecycle: Open -> Filled -> Settled, or Open -> Expired.
const (
	ContractOpen    uint8 = 0
	ContractFilled  uint8 = 1
	ContractSettled uint8 = 2
	ContractExpired uint8 = 3
)

var (
	ErrZeroGasAmount      = errors.New("gas_market: gas amount must be > 0")
	ErrZeroPricePerGas    = errors.New("gas_market: price per gas must be > 0")
	ErrZeroSettlementSlot = errors.New("gas_market: settlement slot must be > 0")
	ErrExpiryBeforeSettle = errors.New("gas_market: expiry slot must be > settlement slot")
	ErrContractNotFound   = errors.New("gas_market: contract not found")
	ErrContractNotOpen    = errors.New("gas_market: contract is not open")
	ErrContractNotFilled  = errors.New("gas_market: contract is not filled")
	ErrSettlementTooEarly = errors.New("gas_market: current slot < settlement slot")
)

// FuturesContract represents a single gas futures agreement between a buyer
// and a seller. The buyer locks in a gas price; the seller commits to deliver.
type FuturesContract struct {
	ID             types.Hash
	Buyer          types.Address
	Seller         types.Address
	GasAmount      uint64
	PricePerGas    *big.Int
	SettlementSlot uint64
	ExpirySlot     uint64
	Status         uint8
}

// FuturesMarket manages the set of gas futures contracts. All operations
// are thread-safe via an internal RWMutex.
type FuturesMarket struct {
	mu        sync.RWMutex
	contracts map[types.Hash]*FuturesContract
	nonce     uint64
}

// NewFuturesMarket creates an empty gas futures market.
func NewFuturesMarket() *FuturesMarket {
	return &FuturesMarket{
		contracts: make(map[types.Hash]*FuturesContract),
	}
}

// CreateContract creates a new open futures contract for the buyer.
// The contract ID is derived from Keccak256(buyer || gasAmount || settlementSlot || nonce).
func (fm *FuturesMarket) CreateContract(buyer types.Address, gasAmount uint64, pricePerGas *big.Int, settlementSlot, expirySlot uint64) (*FuturesContract, error) {
	if gasAmount == 0 {
		return nil, ErrZeroGasAmount
	}
	if pricePerGas == nil || pricePerGas.Sign() <= 0 {
		return nil, ErrZeroPricePerGas
	}
	if settlementSlot == 0 {
		return nil, ErrZeroSettlementSlot
	}
	if expirySlot <= settlementSlot {
		return nil, ErrExpiryBeforeSettle
	}

	fm.mu.Lock()
	defer fm.mu.Unlock()

	// Derive a unique contract ID.
	id := deriveContractID(buyer, gasAmount, settlementSlot, fm.nonce)
	fm.nonce++

	contract := &FuturesContract{
		ID:             id,
		Buyer:          buyer,
		GasAmount:      gasAmount,
		PricePerGas:    new(big.Int).Set(pricePerGas),
		SettlementSlot: settlementSlot,
		ExpirySlot:     expirySlot,
		Status:         ContractOpen,
	}
	fm.contracts[id] = contract

	return contract, nil
}

// FillContract allows a seller to fill an open contract.
func (fm *FuturesMarket) FillContract(contractID types.Hash, seller types.Address) error {
	fm.mu.Lock()
	defer fm.mu.Unlock()

	c, ok := fm.contracts[contractID]
	if !ok {
		return ErrContractNotFound
	}
	if c.Status != ContractOpen {
		return ErrContractNotOpen
	}

	c.Seller = seller
	c.Status = ContractFilled
	return nil
}

// SettleContract settles a filled contract at maturity. The settlement amount
// is (actualGasPrice - contractPrice) * gasAmount. A positive result means
// the seller pays the buyer; negative means the buyer pays the seller.
func (fm *FuturesMarket) SettleContract(contractID types.Hash, currentSlot uint64, actualGasPrice *big.Int) (*big.Int, error) {
	fm.mu.Lock()
	defer fm.mu.Unlock()

	c, ok := fm.contracts[contractID]
	if !ok {
		return nil, ErrContractNotFound
	}
	if c.Status != ContractFilled {
		return nil, ErrContractNotFilled
	}
	if currentSlot < c.SettlementSlot {
		return nil, ErrSettlementTooEarly
	}

	// settlement = (actualGasPrice - contractPrice) * gasAmount
	diff := new(big.Int).Sub(actualGasPrice, c.PricePerGas)
	settlement := new(big.Int).Mul(diff, new(big.Int).SetUint64(c.GasAmount))

	c.Status = ContractSettled
	return settlement, nil
}

// ExpireContracts marks all open (unfilled) contracts whose expiry slot
// has passed as expired. Returns the count of expired contracts.
func (fm *FuturesMarket) ExpireContracts(currentSlot uint64) int {
	fm.mu.Lock()
	defer fm.mu.Unlock()

	count := 0
	for _, c := range fm.contracts {
		if c.Status == ContractOpen && currentSlot >= c.ExpirySlot {
			c.Status = ContractExpired
			count++
		}
	}
	return count
}

// GetContract returns the contract with the given ID, or nil if not found.
func (fm *FuturesMarket) GetContract(id types.Hash) *FuturesContract {
	fm.mu.RLock()
	defer fm.mu.RUnlock()
	return fm.contracts[id]
}

// OpenContracts returns all contracts currently in Open status.
func (fm *FuturesMarket) OpenContracts() []*FuturesContract {
	fm.mu.RLock()
	defer fm.mu.RUnlock()

	var result []*FuturesContract
	for _, c := range fm.contracts {
		if c.Status == ContractOpen {
			result = append(result, c)
		}
	}
	return result
}

// MarketStats returns the count of contracts in each status.
func (fm *FuturesMarket) MarketStats() (open, filled, settled, expired int) {
	fm.mu.RLock()
	defer fm.mu.RUnlock()

	for _, c := range fm.contracts {
		switch c.Status {
		case ContractOpen:
			open++
		case ContractFilled:
			filled++
		case ContractSettled:
			settled++
		case ContractExpired:
			expired++
		}
	}
	return
}

// deriveContractID computes Keccak256(buyer || gasAmount || settlementSlot || nonce).
func deriveContractID(buyer types.Address, gasAmount, settlementSlot, nonce uint64) types.Hash {
	// 20 (address) + 8 (gasAmount) + 8 (settlementSlot) + 8 (nonce) = 44 bytes
	data := make([]byte, 0, 44)
	data = append(data, buyer[:]...)
	var tmp [8]byte
	binary.BigEndian.PutUint64(tmp[:], gasAmount)
	data = append(data, tmp[:]...)
	binary.BigEndian.PutUint64(tmp[:], settlementSlot)
	data = append(data, tmp[:]...)
	binary.BigEndian.PutUint64(tmp[:], nonce)
	data = append(data, tmp[:]...)
	return crypto.Keccak256Hash(data)
}
