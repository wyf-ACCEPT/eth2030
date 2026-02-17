package types

import "math/big"

// Receipt status values.
const (
	ReceiptStatusFailed     = uint64(0)
	ReceiptStatusSuccessful = uint64(1)
)

// Receipt represents the results of a transaction.
type Receipt struct {
	// Consensus fields
	Type              uint8
	PostState         []byte
	Status            uint64
	CumulativeGasUsed uint64
	Bloom             Bloom
	Logs              []*Log

	// Derived fields (filled in by node)
	TxHash            Hash
	ContractAddress   Address
	GasUsed           uint64
	EffectiveGasPrice *big.Int

	// EIP-4844 blob transaction fields
	BlobGasUsed  uint64
	BlobGasPrice *big.Int

	// Inclusion information
	BlockHash        Hash
	BlockNumber      *big.Int
	TransactionIndex uint
}

// NewReceipt creates a new receipt with the given status and cumulative gas.
func NewReceipt(status uint64, cumulativeGasUsed uint64) *Receipt {
	return &Receipt{
		Status:            status,
		CumulativeGasUsed: cumulativeGasUsed,
	}
}
