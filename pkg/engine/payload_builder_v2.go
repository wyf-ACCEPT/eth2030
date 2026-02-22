package engine

import (
	"context"
	"errors"
	"math/big"
	"sort"
	"sync"
	"time"

	"github.com/eth2030/eth2030/core/types"
)

// Payload builder V2 errors.
var (
	ErrBuildTimeout   = errors.New("payload builder: build timeout")
	ErrBuilderStopped = errors.New("payload builder: stopped")
	ErrNoTransactions = errors.New("payload builder: no transactions available")
)

// PayloadBuilderV2Config configures the enhanced payload builder.
type PayloadBuilderV2Config struct {
	BuildDeadline time.Duration // max time to spend building
	GasLimit      uint64        // block gas limit
	Coinbase      types.Address // fee recipient
	BaseFee       *big.Int      // current base fee
	BlobBaseFee   *big.Int      // current blob base fee
	ParentHash    types.Hash    // parent block hash
	ParentNumber  uint64        // parent block number
	Timestamp     uint64        // block timestamp
	PrevRandao    types.Hash    // prevRandao from CL
	Withdrawals   []*Withdrawal // EIP-4895 withdrawals
}

// PayloadResult holds the result of an async payload build.
type PayloadResult struct {
	Block          *types.Block
	Receipts       []*types.Receipt
	BlockValue     *big.Int
	BlobsBundle    *BlobsBundleV1
	GasUsed        uint64
	BlobGasUsed    uint64
	TxCount        int
	Elapsed        time.Duration
	StateRoot      types.Hash
	ReceiptsRoot   types.Hash
}

// txInclusion tracks a transaction's inclusion status during payload building.
type txInclusion struct {
	tx       *types.Transaction
	included bool
	reason   string // reason for exclusion, empty if included
}

// PayloadBuilderV2 builds execution payloads asynchronously with progressive
// improvement. Each build iteration tries to include more transactions and
// improve the block value (total fees).
type PayloadBuilderV2 struct {
	mu       sync.RWMutex
	config   PayloadBuilderV2Config
	result   *PayloadResult
	tracking []*txInclusion
	done     chan struct{}
	cancel   context.CancelFunc
	started  bool
	stopped  bool
}

// NewPayloadBuilderV2 creates a new enhanced payload builder.
func NewPayloadBuilderV2(config PayloadBuilderV2Config) *PayloadBuilderV2 {
	if config.BuildDeadline == 0 {
		config.BuildDeadline = 2 * time.Second
	}
	if config.GasLimit == 0 {
		config.GasLimit = DefaultGasElasticLimit
	}
	return &PayloadBuilderV2{
		config: config,
		done:   make(chan struct{}),
	}
}

// StartBuild begins asynchronous payload building with the given candidate
// transactions. Building continues until the deadline or Stop is called.
// Each iteration attempts to improve the payload by including higher-value
// transactions.
func (pb *PayloadBuilderV2) StartBuild(candidates []*types.Transaction) {
	pb.mu.Lock()
	if pb.started {
		pb.mu.Unlock()
		return
	}
	pb.started = true

	ctx, cancel := context.WithTimeout(context.Background(), pb.config.BuildDeadline)
	pb.cancel = cancel
	pb.mu.Unlock()

	go pb.buildLoop(ctx, candidates)
}

// buildLoop is the main async build goroutine.
func (pb *PayloadBuilderV2) buildLoop(ctx context.Context, candidates []*types.Transaction) {
	defer close(pb.done)

	start := time.Now()
	baseFee := pb.config.BaseFee

	// Sort by effective gas price descending.
	sorted := make([]*types.Transaction, len(candidates))
	copy(sorted, candidates)
	sort.Slice(sorted, func(i, j int) bool {
		pi := effectiveGasPriceForAssembly(sorted[i], baseFee)
		pj := effectiveGasPriceForAssembly(sorted[j], baseFee)
		return pi.Cmp(pj) > 0
	})

	// Build the payload.
	var (
		included []*types.Transaction
		tracking []*txInclusion
		gasUsed  uint64
		blobGas  uint64
		reward   = new(big.Int)
	)

	for _, tx := range sorted {
		select {
		case <-ctx.Done():
			goto finalize
		default:
		}

		if tx == nil {
			continue
		}

		track := &txInclusion{tx: tx}

		// Check fee cap against base fee.
		if baseFee != nil {
			feeCap := tx.GasFeeCap()
			if feeCap == nil {
				feeCap = tx.GasPrice()
			}
			if feeCap != nil && feeCap.Cmp(baseFee) < 0 {
				track.reason = "fee cap below base fee"
				tracking = append(tracking, track)
				continue
			}
		}

		// Check gas limit.
		if gasUsed+tx.Gas() > pb.config.GasLimit {
			track.reason = "exceeds gas limit"
			tracking = append(tracking, track)
			continue
		}

		// Check blob gas limit.
		if tx.Type() == types.BlobTxType {
			txBlobGas := tx.BlobGas()
			if blobGas+txBlobGas > MaxBlobGasPerAssembly {
				track.reason = "exceeds blob gas limit"
				tracking = append(tracking, track)
				continue
			}
			if pb.config.BlobBaseFee != nil && tx.BlobGasFeeCap() != nil {
				if tx.BlobGasFeeCap().Cmp(pb.config.BlobBaseFee) < 0 {
					track.reason = "blob fee cap below blob base fee"
					tracking = append(tracking, track)
					continue
				}
			}
			blobGas += txBlobGas
		}

		// Include the transaction.
		included = append(included, tx)
		gasUsed += tx.Gas()
		track.included = true
		tracking = append(tracking, track)

		// Accumulate tip.
		tip := calcTipForAssembly(tx, baseFee)
		if tip.Sign() > 0 {
			tipTotal := new(big.Int).Mul(tip, new(big.Int).SetUint64(tx.Gas()))
			reward.Add(reward, tipTotal)
		}
	}

finalize:
	elapsed := time.Since(start)

	// Build the block header.
	header := &types.Header{
		ParentHash: pb.config.ParentHash,
		Number:     new(big.Int).SetUint64(pb.config.ParentNumber + 1),
		GasLimit:   pb.config.GasLimit,
		GasUsed:    gasUsed,
		Time:       pb.config.Timestamp,
		Coinbase:   pb.config.Coinbase,
		Difficulty: new(big.Int),
		MixDigest:  pb.config.PrevRandao,
		UncleHash:  types.EmptyUncleHash,
	}
	if baseFee != nil {
		header.BaseFee = new(big.Int).Set(baseFee)
	}
	if blobGas > 0 {
		header.BlobGasUsed = &blobGas
	}

	// Process withdrawals (EIP-4895).
	var withdrawals []*types.Withdrawal
	if pb.config.Withdrawals != nil {
		withdrawals = make([]*types.Withdrawal, len(pb.config.Withdrawals))
		for i, w := range pb.config.Withdrawals {
			withdrawals[i] = &types.Withdrawal{
				Index:          w.Index,
				ValidatorIndex: w.ValidatorIndex,
				Address:        w.Address,
				Amount:         w.Amount,
			}
		}
	}

	body := &types.Body{
		Transactions: included,
		Withdrawals:  withdrawals,
	}
	block := types.NewBlock(header, body)

	// Compute state root and receipts root (simplified; in production these
	// come from actual state execution).
	stateRoot := block.Root()
	receiptsRoot := block.ReceiptHash()

	// Build simple receipts.
	receipts := buildSimpleReceipts(included, gasUsed)

	result := &PayloadResult{
		Block:        block,
		Receipts:     receipts,
		BlockValue:   new(big.Int).Set(reward),
		BlobsBundle:  &BlobsBundleV1{},
		GasUsed:      gasUsed,
		BlobGasUsed:  blobGas,
		TxCount:      len(included),
		Elapsed:      elapsed,
		StateRoot:    stateRoot,
		ReceiptsRoot: receiptsRoot,
	}

	pb.mu.Lock()
	pb.result = result
	pb.tracking = tracking
	pb.mu.Unlock()
}

// GetResult returns the current best payload result. Returns ErrPayloadNotReady
// if the build has not yet produced a result.
func (pb *PayloadBuilderV2) GetResult() (*PayloadResult, error) {
	pb.mu.RLock()
	defer pb.mu.RUnlock()

	if pb.result == nil {
		return nil, ErrPayloadNotReady
	}
	return pb.result, nil
}

// WaitResult blocks until the payload build completes and returns the result.
func (pb *PayloadBuilderV2) WaitResult() (*PayloadResult, error) {
	<-pb.done

	pb.mu.RLock()
	defer pb.mu.RUnlock()

	if pb.result == nil {
		return nil, ErrNoTransactions
	}
	return pb.result, nil
}

// Stop cancels the build process.
func (pb *PayloadBuilderV2) Stop() {
	pb.mu.Lock()
	defer pb.mu.Unlock()

	if pb.cancel != nil && !pb.stopped {
		pb.cancel()
		pb.stopped = true
	}
}

// InclusionTracking returns the inclusion/exclusion status of each candidate
// transaction. Only available after the build completes.
func (pb *PayloadBuilderV2) InclusionTracking() []*txInclusion {
	pb.mu.RLock()
	defer pb.mu.RUnlock()
	return pb.tracking
}

// IncludedCount returns the number of transactions included in the payload.
func (pb *PayloadBuilderV2) IncludedCount() int {
	pb.mu.RLock()
	defer pb.mu.RUnlock()
	count := 0
	for _, t := range pb.tracking {
		if t.included {
			count++
		}
	}
	return count
}

// ExcludedCount returns the number of candidate transactions excluded.
func (pb *PayloadBuilderV2) ExcludedCount() int {
	pb.mu.RLock()
	defer pb.mu.RUnlock()
	count := 0
	for _, t := range pb.tracking {
		if !t.included {
			count++
		}
	}
	return count
}

// PayloadValue returns the total block value (sum of tips).
func (pb *PayloadBuilderV2) PayloadValue() *big.Int {
	pb.mu.RLock()
	defer pb.mu.RUnlock()
	if pb.result == nil {
		return new(big.Int)
	}
	return new(big.Int).Set(pb.result.BlockValue)
}

// IsComplete returns true if the build process has finished.
func (pb *PayloadBuilderV2) IsComplete() bool {
	select {
	case <-pb.done:
		return true
	default:
		return false
	}
}

// buildSimpleReceipts creates basic receipts for included transactions.
// In a full implementation, these would come from actual EVM execution.
func buildSimpleReceipts(txs []*types.Transaction, totalGasUsed uint64) []*types.Receipt {
	if len(txs) == 0 {
		return nil
	}
	receipts := make([]*types.Receipt, len(txs))
	var cumGas uint64
	for i, tx := range txs {
		cumGas += tx.Gas()
		receipts[i] = &types.Receipt{
			Type:              tx.Type(),
			Status:            types.ReceiptStatusSuccessful,
			CumulativeGasUsed: cumGas,
			GasUsed:           tx.Gas(),
			TxHash:            tx.Hash(),
			TransactionIndex:  uint(i),
		}
	}
	return receipts
}

// CalcPayloadValue computes the total block value from transactions and
// the base fee. Block value = sum of (effectiveGasPrice - baseFee) * gasUsed
// for each transaction.
func CalcPayloadValue(txs []*types.Transaction, baseFee *big.Int) *big.Int {
	total := new(big.Int)
	if baseFee == nil {
		return total
	}
	for _, tx := range txs {
		effectivePrice := effectiveGasPriceForAssembly(tx, baseFee)
		tip := new(big.Int).Sub(effectivePrice, baseFee)
		if tip.Sign() <= 0 {
			continue
		}
		tipTotal := new(big.Int).Mul(tip, new(big.Int).SetUint64(tx.Gas()))
		total.Add(total, tipTotal)
	}
	return total
}
