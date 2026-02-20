package core

import (
	"math/big"
	"testing"

	"github.com/eth2028/eth2028/core/types"
)

func TestNewStateTransition(t *testing.T) {
	cfg := TestConfig
	st := NewStateTransition(cfg)
	if st == nil {
		t.Fatal("NewStateTransition returned nil")
	}
}

func makeLegacyTx(nonce uint64, to *types.Address, value *big.Int, gas uint64, gasPrice *big.Int, data []byte) *types.Transaction {
	return types.NewTransaction(&types.LegacyTx{
		Nonce:    nonce,
		To:       to,
		Value:    value,
		Gas:      gas,
		GasPrice: gasPrice,
		Data:     data,
	})
}

func makeDynFeeTx(nonce uint64, to *types.Address, value *big.Int, gas uint64, feeCap, tipCap *big.Int, data []byte) *types.Transaction {
	return types.NewTransaction(&types.DynamicFeeTx{
		Nonce:     nonce,
		To:        to,
		Value:     value,
		Gas:       gas,
		GasFeeCap: feeCap,
		GasTipCap: tipCap,
		Data:      data,
	})
}

func TestTxIntrinsicGasSimpleTransfer(t *testing.T) {
	addr := types.BytesToAddress([]byte{0x01})
	tx := makeLegacyTx(0, &addr, big.NewInt(0), 21000, big.NewInt(1), nil)

	got := txIntrinsicGas(tx)
	if got != TxGas {
		t.Errorf("txIntrinsicGas(simple transfer) = %d, want %d", got, TxGas)
	}
}

func TestTxIntrinsicGasContractCreation(t *testing.T) {
	tx := makeLegacyTx(0, nil, big.NewInt(0), 100000, big.NewInt(1), nil)

	got := txIntrinsicGas(tx)
	expected := TxGas + TxCreateGas
	if got != expected {
		t.Errorf("txIntrinsicGas(create) = %d, want %d", got, expected)
	}
}

func TestTxIntrinsicGasWithData(t *testing.T) {
	addr := types.BytesToAddress([]byte{0x01})
	// 3 zero bytes, 2 non-zero bytes.
	data := []byte{0, 0, 0, 0xff, 0x01}
	tx := makeLegacyTx(0, &addr, big.NewInt(0), 100000, big.NewInt(1), data)

	got := txIntrinsicGas(tx)
	expected := TxGas + 3*TxDataZeroGas + 2*TxDataNonZeroGas
	if got != expected {
		t.Errorf("txIntrinsicGas(data) = %d, want %d", got, expected)
	}
}

func TestTxCostSimple(t *testing.T) {
	addr := types.BytesToAddress([]byte{0x01})
	tx := makeLegacyTx(0, &addr, big.NewInt(100), 21000, big.NewInt(10), nil)

	cost := TxCost(tx, big.NewInt(5))
	// Cost = value(100) + gas(21000)*gasPrice(10) = 100 + 210000 = 210100
	expected := big.NewInt(210100)
	if cost.Cmp(expected) != 0 {
		t.Errorf("TxCost = %s, want %s", cost, expected)
	}
}

func TestTxCostZeroValue(t *testing.T) {
	addr := types.BytesToAddress([]byte{0x01})
	tx := makeLegacyTx(0, &addr, big.NewInt(0), 21000, big.NewInt(1), nil)

	cost := TxCost(tx, big.NewInt(1))
	// Cost = value(0) + gas(21000)*gasPrice(1) = 21000
	expected := big.NewInt(21000)
	if cost.Cmp(expected) != 0 {
		t.Errorf("TxCost = %s, want %s", cost, expected)
	}
}

func TestEffectiveGasPriceLegacy(t *testing.T) {
	// Legacy tx: EffectiveGasPrice = GasPrice.
	addr := types.BytesToAddress([]byte{0x01})
	tx := makeLegacyTx(0, &addr, big.NewInt(0), 21000, big.NewInt(50), nil)

	got := EffectiveGasPrice(tx, nil)
	if got.Cmp(big.NewInt(50)) != 0 {
		t.Errorf("EffectiveGasPrice(legacy, nil basefee) = %s, want 50", got)
	}
}

func TestEffectiveGasPriceEIP1559(t *testing.T) {
	// EIP-1559 tx: effective = min(feeCap, baseFee + tipCap).
	addr := types.BytesToAddress([]byte{0x01})
	tx := makeDynFeeTx(0, &addr, big.NewInt(0), 21000, big.NewInt(100), big.NewInt(10), nil)

	baseFee := big.NewInt(80)
	got := EffectiveGasPrice(tx, baseFee)
	// effective = min(100, 80+10=90) = 90
	expected := big.NewInt(90)
	if got.Cmp(expected) != 0 {
		t.Errorf("EffectiveGasPrice(EIP1559) = %s, want %s", got, expected)
	}
}

func TestEffectiveGasPriceFeeCapLimits(t *testing.T) {
	// When baseFee + tipCap > feeCap, effective is capped at feeCap.
	addr := types.BytesToAddress([]byte{0x01})
	tx := makeDynFeeTx(0, &addr, big.NewInt(0), 21000, big.NewInt(50), big.NewInt(30), nil)

	baseFee := big.NewInt(40)
	got := EffectiveGasPrice(tx, baseFee)
	// effective = min(50, 40+30=70) = 50
	expected := big.NewInt(50)
	if got.Cmp(expected) != 0 {
		t.Errorf("EffectiveGasPrice(feeCap limit) = %s, want %s", got, expected)
	}
}

func TestBlockRewardPostMerge(t *testing.T) {
	cfg := &ChainConfig{
		ChainID:                 big.NewInt(1),
		TerminalTotalDifficulty: big.NewInt(0),
	}
	header := &types.Header{Number: big.NewInt(100)}
	reward := BlockReward(cfg, header)
	if reward.Sign() != 0 {
		t.Errorf("BlockReward post-merge = %s, want 0", reward)
	}
}

func TestBlockRewardPreMerge(t *testing.T) {
	cfg := &ChainConfig{
		ChainID: big.NewInt(1),
		// No TTD set -> pre-merge.
	}
	header := &types.Header{Number: big.NewInt(100)}
	reward := BlockReward(cfg, header)
	// 2 ETH = 2 * 10^18
	expected := new(big.Int).Mul(big.NewInt(2), new(big.Int).SetUint64(1e18))
	if reward.Cmp(expected) != 0 {
		t.Errorf("BlockReward pre-merge = %s, want %s", reward, expected)
	}
}

func TestValidatePostBlockMatch(t *testing.T) {
	root := types.BytesToHash([]byte{0x01})
	bloom := types.Bloom{}
	header := &types.Header{
		GasUsed: 21000,
		Root:    root,
		Bloom:   bloom,
	}
	result := &TransitionResult{
		GasUsed:   21000,
		StateRoot: root,
		LogsBloom: bloom,
	}
	if err := ValidatePostBlock(header, result); err != nil {
		t.Errorf("ValidatePostBlock(matching) = %v, want nil", err)
	}
}

func TestValidatePostBlockGasMismatch(t *testing.T) {
	root := types.BytesToHash([]byte{0x01})
	header := &types.Header{
		GasUsed: 21000,
		Root:    root,
	}
	result := &TransitionResult{
		GasUsed:   42000,
		StateRoot: root,
	}
	err := ValidatePostBlock(header, result)
	if err == nil {
		t.Fatal("ValidatePostBlock should fail on gas mismatch")
	}
}

func TestValidatePostBlockStateMismatch(t *testing.T) {
	header := &types.Header{
		GasUsed: 21000,
		Root:    types.BytesToHash([]byte{0x01}),
	}
	result := &TransitionResult{
		GasUsed:   21000,
		StateRoot: types.BytesToHash([]byte{0x02}),
	}
	err := ValidatePostBlock(header, result)
	if err == nil {
		t.Fatal("ValidatePostBlock should fail on state root mismatch")
	}
}

func TestValidatePostBlockBloomMismatch(t *testing.T) {
	root := types.BytesToHash([]byte{0x01})
	header := &types.Header{
		GasUsed: 21000,
		Root:    root,
		Bloom:   types.Bloom{1},
	}
	result := &TransitionResult{
		GasUsed:   21000,
		StateRoot: root,
		LogsBloom: types.Bloom{2},
	}
	err := ValidatePostBlock(header, result)
	if err == nil {
		t.Fatal("ValidatePostBlock should fail on bloom mismatch")
	}
}

func TestNextBlockBaseFee(t *testing.T) {
	parent := &types.Header{
		GasLimit: 30_000_000,
		GasUsed:  15_000_000,
		BaseFee:  big.NewInt(1000),
	}
	got := NextBlockBaseFee(parent)
	expected := CalcBaseFee(parent)
	if got.Cmp(expected) != 0 {
		t.Errorf("NextBlockBaseFee = %s, want %s", got, expected)
	}
}

func TestTransitionResultFields(t *testing.T) {
	result := &TransitionResult{
		Receipts:    []*types.Receipt{{Status: 1}},
		GasUsed:     21000,
		BlobGasUsed: 131072,
		LogsBloom:   types.Bloom{},
		StateRoot:   types.BytesToHash([]byte{0xaa}),
	}
	if len(result.Receipts) != 1 {
		t.Errorf("Receipts count = %d, want 1", len(result.Receipts))
	}
	if result.GasUsed != 21000 {
		t.Errorf("GasUsed = %d, want 21000", result.GasUsed)
	}
	if result.BlobGasUsed != 131072 {
		t.Errorf("BlobGasUsed = %d, want 131072", result.BlobGasUsed)
	}
}

func TestSTErrorSentinels(t *testing.T) {
	// Verify error sentinels are distinct.
	errors := []error{
		ErrSTBlobGasExceeded,
		ErrSTBlobGasUsedInvalid,
		ErrSTStateRootMismatch,
		ErrSTReceiptRootMismatch,
		ErrSTBloomMismatch,
		ErrSTGasUsedMismatch,
		ErrSTInvalidSender,
		ErrSTMaxBlobGas,
	}
	seen := make(map[string]bool)
	for _, e := range errors {
		if seen[e.Error()] {
			t.Errorf("duplicate error message: %s", e.Error())
		}
		seen[e.Error()] = true
	}
}

func TestValidateTransactionNilSender(t *testing.T) {
	addr := types.BytesToAddress([]byte{0x01})
	// Transaction without sender set.
	tx := makeLegacyTx(0, &addr, big.NewInt(0), 21000, big.NewInt(1), nil)
	header := &types.Header{
		Number:   big.NewInt(1),
		GasLimit: 30_000_000,
	}

	err := ValidateTransaction(tx, nil, header, nil)
	if err == nil {
		t.Fatal("ValidateTransaction should fail with nil sender")
	}
}

func TestEffectiveGasPriceZeroBaseFee(t *testing.T) {
	addr := types.BytesToAddress([]byte{0x01})
	tx := makeLegacyTx(0, &addr, big.NewInt(0), 21000, big.NewInt(100), nil)

	got := EffectiveGasPrice(tx, big.NewInt(0))
	// Zero base fee -> returns GasPrice.
	if got.Cmp(big.NewInt(100)) != 0 {
		t.Errorf("EffectiveGasPrice(zero basefee) = %s, want 100", got)
	}
}

func TestBlockRewardNilConfig(t *testing.T) {
	header := &types.Header{Number: big.NewInt(1)}
	// nil config -> pre-merge behavior.
	reward := BlockReward(nil, header)
	expected := new(big.Int).Mul(big.NewInt(2), new(big.Int).SetUint64(1e18))
	if reward.Cmp(expected) != 0 {
		t.Errorf("BlockReward(nil config) = %s, want %s", reward, expected)
	}
}

func TestTxCostWithNilValue(t *testing.T) {
	// Transaction with nil value.
	tx := types.NewTransaction(&types.LegacyTx{
		Nonce:    0,
		To:       nil,
		Value:    nil,
		Gas:      21000,
		GasPrice: big.NewInt(1),
	})
	cost := TxCost(tx, nil)
	// Cost = 0 + gas(21000)*gasPrice(1) = 21000
	if cost.Cmp(big.NewInt(21000)) != 0 {
		t.Errorf("TxCost(nil value) = %s, want 21000", cost)
	}
}
