package engine

import (
	"context"
	"math/big"
	"sort"
	"sync"
	"time"

	"github.com/eth2028/eth2028/core/types"
)

// Assembly constants.
const (
	// DefaultAssemblyTimeout is the default deadline for block assembly.
	DefaultAssemblyTimeout = 2 * time.Second

	// DefaultGasTarget is the default gas target (half of the gas limit,
	// per EIP-1559 elasticity).
	DefaultGasTarget = 15_000_000

	// DefaultGasElasticLimit is the default elastic gas limit (2x target).
	DefaultGasElasticLimit = 30_000_000

	// BlobGasPerBlob is the gas consumed by a single blob (2^17).
	BlobGasPerBlob = 131072

	// MaxBlobGasPerAssembly is the maximum blob gas per block (6 blobs).
	MaxBlobGasPerAssembly = 786432

	// BaseFeeChangeDenom is the EIP-1559 base fee change denominator.
	BaseFeeChangeDenom = 8

	// ElasticityMul is the EIP-1559 elasticity multiplier.
	ElasticityMul = 2
)

// BlockAssemblerConfig configures the block assembler.
type BlockAssemblerConfig struct {
	GasLimit    uint64        // block gas limit
	Timeout     time.Duration // assembly deadline
	Coinbase    types.Address // fee recipient
	BaseFee     *big.Int      // current base fee
	BlobBaseFee *big.Int      // current blob base fee (EIP-4844)
}

// AssembledBlock holds the result of block assembly.
type AssembledBlock struct {
	Transactions []*types.Transaction
	GasUsed      uint64
	BlobGasUsed  uint64
	CoinbaseReward *big.Int // total tips accumulated for coinbase
	TxCount      int
	Elapsed      time.Duration
	TimedOut     bool
}

// BlockAssembler selects and orders transactions for inclusion in a new block.
// It tracks gas consumption, blob gas, and computes coinbase rewards.
type BlockAssembler struct {
	mu     sync.Mutex
	config BlockAssemblerConfig

	included []*types.Transaction
	gasUsed  uint64
	blobGas  uint64
	reward   *big.Int
}

// NewBlockAssembler creates a new block assembler with the given configuration.
func NewBlockAssembler(config BlockAssemblerConfig) *BlockAssembler {
	if config.Timeout == 0 {
		config.Timeout = DefaultAssemblyTimeout
	}
	if config.GasLimit == 0 {
		config.GasLimit = DefaultGasElasticLimit
	}
	return &BlockAssembler{
		config: config,
		reward: new(big.Int),
	}
}

// Assemble selects transactions from the candidate pool ordered by effective
// gas price (descending) and packs them into a block until the gas limit is
// reached or the timeout expires. Returns the assembled block result.
func (ba *BlockAssembler) Assemble(candidates []*types.Transaction) *AssembledBlock {
	ctx, cancel := context.WithTimeout(context.Background(), ba.config.Timeout)
	defer cancel()

	return ba.assembleWithContext(ctx, candidates)
}

// assembleWithContext performs the core assembly loop with a context deadline.
func (ba *BlockAssembler) assembleWithContext(ctx context.Context, candidates []*types.Transaction) *AssembledBlock {
	start := time.Now()

	ba.mu.Lock()
	defer ba.mu.Unlock()

	// Reset state for new assembly.
	ba.included = nil
	ba.gasUsed = 0
	ba.blobGas = 0
	ba.reward = new(big.Int)

	baseFee := ba.config.BaseFee

	// Sort candidates by effective gas price descending.
	sorted := make([]*types.Transaction, len(candidates))
	copy(sorted, candidates)
	sort.Slice(sorted, func(i, j int) bool {
		pi := effectiveGasPriceForAssembly(sorted[i], baseFee)
		pj := effectiveGasPriceForAssembly(sorted[j], baseFee)
		return pi.Cmp(pj) > 0
	})

	timedOut := false
	for _, tx := range sorted {
		// Check deadline.
		select {
		case <-ctx.Done():
			timedOut = true
			goto done
		default:
		}

		if tx == nil {
			continue
		}

		// Skip transactions whose fee cap is below the base fee.
		if baseFee != nil {
			feeCap := tx.GasFeeCap()
			if feeCap == nil {
				feeCap = tx.GasPrice()
			}
			if feeCap != nil && feeCap.Cmp(baseFee) < 0 {
				continue
			}
		}

		txGas := tx.Gas()
		// Check gas limit.
		if ba.gasUsed+txGas > ba.config.GasLimit {
			continue
		}

		// Check blob gas limit for blob transactions.
		if tx.Type() == types.BlobTxType {
			txBlobGas := tx.BlobGas()
			if ba.blobGas+txBlobGas > MaxBlobGasPerAssembly {
				continue
			}
			// Check blob fee cap against blob base fee.
			if ba.config.BlobBaseFee != nil && tx.BlobGasFeeCap() != nil {
				if tx.BlobGasFeeCap().Cmp(ba.config.BlobBaseFee) < 0 {
					continue
				}
			}
			ba.blobGas += txBlobGas
		}

		ba.included = append(ba.included, tx)
		ba.gasUsed += txGas

		// Accumulate coinbase reward (tip portion).
		tip := calcTipForAssembly(tx, baseFee)
		if tip.Sign() > 0 {
			tipTotal := new(big.Int).Mul(tip, new(big.Int).SetUint64(txGas))
			ba.reward.Add(ba.reward, tipTotal)
		}
	}

done:
	elapsed := time.Since(start)

	return &AssembledBlock{
		Transactions:   ba.included,
		GasUsed:        ba.gasUsed,
		BlobGasUsed:    ba.blobGas,
		CoinbaseReward: new(big.Int).Set(ba.reward),
		TxCount:        len(ba.included),
		Elapsed:        elapsed,
		TimedOut:       timedOut,
	}
}

// GasRemaining returns the remaining gas budget in the block.
func (ba *BlockAssembler) GasRemaining() uint64 {
	ba.mu.Lock()
	defer ba.mu.Unlock()
	if ba.gasUsed >= ba.config.GasLimit {
		return 0
	}
	return ba.config.GasLimit - ba.gasUsed
}

// CalcNextBaseFee computes the EIP-1559 base fee for the next block given
// the parent's base fee, gas limit, and gas used.
func CalcNextBaseFee(parentBaseFee *big.Int, parentGasLimit, parentGasUsed uint64) *big.Int {
	if parentBaseFee == nil {
		return big.NewInt(1_000_000_000) // initial base fee: 1 Gwei
	}

	parentGasTarget := parentGasLimit / ElasticityMul

	// At target: base fee unchanged.
	if parentGasUsed == parentGasTarget {
		return new(big.Int).Set(parentBaseFee)
	}

	if parentGasUsed > parentGasTarget {
		// Above target: increase base fee.
		delta := new(big.Int).SetUint64(parentGasUsed - parentGasTarget)
		delta.Mul(delta, parentBaseFee)
		delta.Div(delta, new(big.Int).SetUint64(parentGasTarget))
		delta.Div(delta, new(big.Int).SetUint64(BaseFeeChangeDenom))
		if delta.Sign() == 0 {
			delta.SetInt64(1)
		}
		return new(big.Int).Add(parentBaseFee, delta)
	}

	// Below target: decrease base fee.
	delta := new(big.Int).SetUint64(parentGasTarget - parentGasUsed)
	delta.Mul(delta, parentBaseFee)
	delta.Div(delta, new(big.Int).SetUint64(parentGasTarget))
	delta.Div(delta, new(big.Int).SetUint64(BaseFeeChangeDenom))

	baseFee := new(big.Int).Sub(parentBaseFee, delta)
	if baseFee.Sign() < 0 {
		baseFee.SetInt64(0)
	}
	// Minimum base fee of 7 wei (EIP-4844 era).
	if baseFee.Cmp(big.NewInt(7)) < 0 {
		baseFee.SetInt64(7)
	}
	return baseFee
}

// CalcBlobGasUsed computes the total blob gas consumed by a set of transactions.
func CalcBlobGasUsed(txs []*types.Transaction) uint64 {
	var total uint64
	for _, tx := range txs {
		if tx.Type() == types.BlobTxType {
			total += tx.BlobGas()
		}
	}
	return total
}

// effectiveGasPriceForAssembly computes the effective gas price for sorting.
func effectiveGasPriceForAssembly(tx *types.Transaction, baseFee *big.Int) *big.Int {
	if baseFee == nil || tx.Type() == types.LegacyTxType || tx.Type() == types.AccessListTxType {
		gp := tx.GasPrice()
		if gp == nil {
			return new(big.Int)
		}
		return new(big.Int).Set(gp)
	}
	// EIP-1559: effective = min(feeCap, baseFee + tipCap)
	feeCap := tx.GasFeeCap()
	tipCap := tx.GasTipCap()
	if feeCap == nil {
		return new(big.Int)
	}
	if tipCap == nil {
		tipCap = new(big.Int)
	}
	eff := new(big.Int).Add(baseFee, tipCap)
	if eff.Cmp(feeCap) > 0 {
		return new(big.Int).Set(feeCap)
	}
	return eff
}

// calcTipForAssembly computes the per-gas tip paid by a transaction.
func calcTipForAssembly(tx *types.Transaction, baseFee *big.Int) *big.Int {
	if baseFee == nil {
		gp := tx.GasPrice()
		if gp == nil {
			return new(big.Int)
		}
		return new(big.Int).Set(gp)
	}
	effectivePrice := effectiveGasPriceForAssembly(tx, baseFee)
	tip := new(big.Int).Sub(effectivePrice, baseFee)
	if tip.Sign() < 0 {
		tip.SetInt64(0)
	}
	return tip
}
