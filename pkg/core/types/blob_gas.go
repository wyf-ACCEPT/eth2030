package types

import "math/big"

// EIP-4844 blob gas constants.
const (
	BlobTxBlobGasPerBlob         = 1 << 17 // 131072
	MaxBlobGasPerBlock           = 786432
	TargetBlobGasPerBlock        = 393216
	BlobTxMinBlobGasprice        = 1
	BlobBaseFeeUpdateFraction    = 3338477
	VersionedHashVersionKZG byte = 0x01
)

// CalcExcessBlobGas calculates the excess blob gas for a block given
// the parent's excess blob gas and blob gas used.
func CalcExcessBlobGas(parentExcessBlobGas, parentBlobGasUsed uint64) uint64 {
	if parentExcessBlobGas+parentBlobGasUsed < TargetBlobGasPerBlock {
		return 0
	}
	return parentExcessBlobGas + parentBlobGasUsed - TargetBlobGasPerBlock
}

// CalcBlobFee calculates the blob gas price from the excess blob gas
// using the fake_exponential formula from EIP-4844.
func CalcBlobFee(excessBlobGas uint64) *big.Int {
	return fakeExponential(
		big.NewInt(BlobTxMinBlobGasprice),
		new(big.Int).SetUint64(excessBlobGas),
		big.NewInt(BlobBaseFeeUpdateFraction),
	)
}

// GetBlobGasUsed returns the total blob gas consumed by numBlobs blobs.
func GetBlobGasUsed(numBlobs int) uint64 {
	return uint64(numBlobs) * BlobTxBlobGasPerBlob
}

// fakeExponential approximates factor * e ** (numerator / denominator)
// using a Taylor expansion, as specified in EIP-4844.
func fakeExponential(factor, numerator, denominator *big.Int) *big.Int {
	i := new(big.Int).SetUint64(1)
	output := new(big.Int)
	numeratorAccum := new(big.Int).Mul(factor, denominator)
	// Temporary values for the loop.
	tmp := new(big.Int)
	denom := new(big.Int)
	for numeratorAccum.Sign() > 0 {
		output.Add(output, numeratorAccum)
		// numeratorAccum = numeratorAccum * numerator / (denominator * i)
		tmp.Mul(numeratorAccum, numerator)
		denom.Mul(denominator, i)
		numeratorAccum.Div(tmp, denom)
		i.Add(i, big.NewInt(1))
	}
	output.Div(output, denominator)
	return output
}
