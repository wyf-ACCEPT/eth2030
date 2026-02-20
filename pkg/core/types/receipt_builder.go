package types

import "math/big"

// ReceiptBuilder constructs receipts step-by-step after transaction execution.
// It accumulates fields and computes the bloom filter on Build().
type ReceiptBuilder struct {
	status           uint64
	gasUsed          uint64
	cumulativeGas    uint64
	logs             []*Log
	contractAddress  Address
	txHash           Hash
	blockHash        Hash
	blockNumber      uint64
	transactionIndex uint
	txType           uint8
	effectiveGasPrice *big.Int
	blobGasUsed      uint64
	blobGasPrice     *big.Int

	// Track which fields were explicitly set.
	hasStatus      bool
	hasBlockNumber bool
}

// NewReceiptBuilder creates a new ReceiptBuilder with zero-value defaults.
func NewReceiptBuilder() *ReceiptBuilder {
	return &ReceiptBuilder{}
}

// SetStatus sets the receipt status code (0=fail, 1=success).
func (rb *ReceiptBuilder) SetStatus(status uint64) *ReceiptBuilder {
	rb.status = status
	rb.hasStatus = true
	return rb
}

// SetGasUsed sets the gas consumed by this transaction.
func (rb *ReceiptBuilder) SetGasUsed(gas uint64) *ReceiptBuilder {
	rb.gasUsed = gas
	return rb
}

// SetCumulativeGasUsed sets the cumulative gas used in the block up to
// and including this transaction.
func (rb *ReceiptBuilder) SetCumulativeGasUsed(gas uint64) *ReceiptBuilder {
	rb.cumulativeGas = gas
	return rb
}

// AddLog appends a log entry to the receipt. Nil logs are ignored.
func (rb *ReceiptBuilder) AddLog(log *Log) *ReceiptBuilder {
	if log != nil {
		rb.logs = append(rb.logs, log)
	}
	return rb
}

// SetContractAddress sets the contract address for contract creation txs.
func (rb *ReceiptBuilder) SetContractAddress(addr Address) *ReceiptBuilder {
	rb.contractAddress = addr
	return rb
}

// SetTxHash sets the transaction hash on the receipt.
func (rb *ReceiptBuilder) SetTxHash(hash Hash) *ReceiptBuilder {
	rb.txHash = hash
	return rb
}

// SetBlockHash sets the block hash on the receipt.
func (rb *ReceiptBuilder) SetBlockHash(hash Hash) *ReceiptBuilder {
	rb.blockHash = hash
	return rb
}

// SetBlockNumber sets the block number on the receipt.
func (rb *ReceiptBuilder) SetBlockNumber(num uint64) *ReceiptBuilder {
	rb.blockNumber = num
	rb.hasBlockNumber = true
	return rb
}

// SetTransactionIndex sets the index of the transaction within the block.
func (rb *ReceiptBuilder) SetTransactionIndex(idx uint) *ReceiptBuilder {
	rb.transactionIndex = idx
	return rb
}

// SetType sets the transaction type on the receipt (e.g., 0=legacy, 2=EIP-1559).
func (rb *ReceiptBuilder) SetType(txType uint8) *ReceiptBuilder {
	rb.txType = txType
	return rb
}

// SetEffectiveGasPrice sets the effective gas price for gas cost calculation.
func (rb *ReceiptBuilder) SetEffectiveGasPrice(price *big.Int) *ReceiptBuilder {
	rb.effectiveGasPrice = price
	return rb
}

// SetBlobGasUsed sets the blob gas used (EIP-4844).
func (rb *ReceiptBuilder) SetBlobGasUsed(gas uint64) *ReceiptBuilder {
	rb.blobGasUsed = gas
	return rb
}

// SetBlobGasPrice sets the blob gas price (EIP-4844).
func (rb *ReceiptBuilder) SetBlobGasPrice(price *big.Int) *ReceiptBuilder {
	rb.blobGasPrice = price
	return rb
}

// Build assembles the final Receipt, computing the bloom filter from logs.
func (rb *ReceiptBuilder) Build() *Receipt {
	receipt := &Receipt{
		Type:              rb.txType,
		Status:            rb.status,
		CumulativeGasUsed: rb.cumulativeGas,
		Logs:              rb.logs,
		TxHash:            rb.txHash,
		ContractAddress:   rb.contractAddress,
		GasUsed:           rb.gasUsed,
		EffectiveGasPrice: rb.effectiveGasPrice,
		BlobGasUsed:       rb.blobGasUsed,
		BlobGasPrice:      rb.blobGasPrice,
		BlockHash:         rb.blockHash,
		TransactionIndex:  rb.transactionIndex,
	}

	if rb.hasBlockNumber {
		receipt.BlockNumber = new(big.Int).SetUint64(rb.blockNumber)
	}

	// Compute bloom filter from logs.
	if len(rb.logs) > 0 {
		receipt.Bloom = ComputeReceiptBloom(rb.logs)
	}

	return receipt
}

// ComputeReceiptBloom computes a bloom filter from a slice of logs.
// For each log, it adds the emitting address and every topic to the bloom.
func ComputeReceiptBloom(logs []*Log) Bloom {
	return LogsBloom(logs)
}
