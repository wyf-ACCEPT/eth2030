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

	// EIP-7706 calldata gas fields
	CalldataGasUsed  uint64
	CalldataGasPrice *big.Int

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

// Succeeded returns true if the receipt indicates a successful transaction
// (post-Byzantium status field equals 1).
func (r *Receipt) Succeeded() bool {
	return r.Status == ReceiptStatusSuccessful
}

// DeriveReceiptFields populates the derived fields on a list of receipts
// after block processing. It sets the cumulative gas, block context fields,
// and per-log indices for each receipt in the block.
func DeriveReceiptFields(receipts []*Receipt, blockHash Hash, blockNumber uint64, baseFee *big.Int, txs []*Transaction) {
	var logIndex uint

	for i, receipt := range receipts {
		receipt.BlockHash = blockHash
		receipt.BlockNumber = new(big.Int).SetUint64(blockNumber)
		receipt.TransactionIndex = uint(i)

		if i < len(txs) {
			receipt.TxHash = txs[i].Hash()
		}

		// Populate log context fields and assign global log indices.
		for _, log := range receipt.Logs {
			log.BlockHash = blockHash
			log.BlockNumber = blockNumber
			log.TxIndex = uint(i)
			log.Index = logIndex
			if i < len(txs) {
				log.TxHash = txs[i].Hash()
			}
			logIndex++
		}
	}
}
