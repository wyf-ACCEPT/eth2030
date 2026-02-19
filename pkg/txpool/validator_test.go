package txpool

import (
	"math/big"
	"sync"
	"testing"

	"github.com/eth2028/eth2028/core/types"
)

// makeValidatorTx creates a valid legacy transaction for validator tests.
func makeValidatorTx(nonce uint64, gasPrice int64, gas uint64, data []byte, value *big.Int) *types.Transaction {
	to := types.BytesToAddress([]byte{0xaa, 0xbb})
	tx := types.NewTransaction(&types.LegacyTx{
		Nonce:    nonce,
		GasPrice: big.NewInt(gasPrice),
		Gas:      gas,
		To:       &to,
		Value:    value,
		Data:     data,
		V:        big.NewInt(27),
		R:        big.NewInt(1),
		S:        big.NewInt(1),
	})
	return tx
}

// makeValidatorDynTx creates a valid EIP-1559 transaction for validator tests.
func makeValidatorDynTx(nonce uint64, tipCap, feeCap int64, gas uint64, chainID uint64) *types.Transaction {
	to := types.BytesToAddress([]byte{0xaa, 0xbb})
	tx := types.NewTransaction(&types.DynamicFeeTx{
		ChainID:   big.NewInt(int64(chainID)),
		Nonce:     nonce,
		GasTipCap: big.NewInt(tipCap),
		GasFeeCap: big.NewInt(feeCap),
		Gas:       gas,
		To:        &to,
		Value:     big.NewInt(0),
		V:         big.NewInt(0),
		R:         big.NewInt(1),
		S:         big.NewInt(1),
	})
	return tx
}

func TestTxValidator_ValidTransaction(t *testing.T) {
	v := NewTxValidator(TxValidationConfig{ChainID: 1})
	// 2 Gwei gas price, 21000 gas, zero value
	tx := makeValidatorTx(0, 2_000_000_000, 21000, nil, big.NewInt(0))
	result := v.ValidateTx(tx)
	if !result.Valid {
		t.Fatalf("expected valid, got error: %v", result.Error)
	}
	// Should pass basic, gas, size, and signature checks.
	// ChainID is skipped for pre-EIP-155 legacy txs (V=27, chainID=0).
	expectedChecks := []string{"basic", "gas", "size", "chainid", "signature"}
	if len(result.Checks) != len(expectedChecks) {
		t.Fatalf("expected %d checks, got %d: %v", len(expectedChecks), len(result.Checks), result.Checks)
	}
	for i, check := range expectedChecks {
		if result.Checks[i] != check {
			t.Errorf("check %d: expected %q, got %q", i, check, result.Checks[i])
		}
	}
}

func TestTxValidator_GasTooLow(t *testing.T) {
	v := NewTxValidator(DefaultTxValidationConfig())
	// Gas price of 1 wei, below the 1 Gwei default minimum.
	tx := makeValidatorTx(0, 1, 21000, nil, big.NewInt(0))
	result := v.ValidateTx(tx)
	if result.Valid {
		t.Fatal("expected invalid for gas price too low")
	}
	if result.Error != ErrTxGasTooLow {
		t.Fatalf("expected ErrTxGasTooLow, got %v", result.Error)
	}
}

func TestTxValidator_GasTooHigh(t *testing.T) {
	v := NewTxValidator(DefaultTxValidationConfig())
	// Gas limit of 50M, above the 30M default maximum.
	tx := makeValidatorTx(0, 2_000_000_000, 50_000_000, nil, big.NewInt(0))
	result := v.ValidateTx(tx)
	if result.Valid {
		t.Fatal("expected invalid for gas limit too high")
	}
	if result.Error != ErrTxGasTooHigh {
		t.Fatalf("expected ErrTxGasTooHigh, got %v", result.Error)
	}
}

func TestTxValidator_ZeroGas(t *testing.T) {
	v := NewTxValidator(DefaultTxValidationConfig())
	tx := makeValidatorTx(0, 2_000_000_000, 0, nil, big.NewInt(0))
	result := v.ValidateTx(tx)
	if result.Valid {
		t.Fatal("expected invalid for zero gas")
	}
	if result.Error != ErrTxGasTooLow {
		t.Fatalf("expected ErrTxGasTooLow, got %v", result.Error)
	}
}

func TestTxValidator_DataTooLarge(t *testing.T) {
	v := NewTxValidator(TxValidationConfig{
		MaxDataSize: 100,
		ChainID:     1,
	})
	data := make([]byte, 200)
	tx := makeValidatorTx(0, 2_000_000_000, 100000, data, big.NewInt(0))
	result := v.ValidateTx(tx)
	if result.Valid {
		t.Fatal("expected invalid for oversized data")
	}
	if result.Error != ErrTxDataTooLarge {
		t.Fatalf("expected ErrTxDataTooLarge, got %v", result.Error)
	}
}

func TestTxValidator_ValueTooHigh(t *testing.T) {
	v := NewTxValidator(TxValidationConfig{
		MaxValueWei: big.NewInt(1_000_000), // 1M wei max
		ChainID:     1,
	})
	tx := makeValidatorTx(0, 2_000_000_000, 21000, nil, big.NewInt(2_000_000))
	result := v.ValidateTx(tx)
	if result.Valid {
		t.Fatal("expected invalid for value too high")
	}
	if result.Error != ErrTxValueTooHigh {
		t.Fatalf("expected ErrTxValueTooHigh, got %v", result.Error)
	}
}

func TestTxValidator_MissingSignature(t *testing.T) {
	v := NewTxValidator(TxValidationConfig{ChainID: 1})
	to := types.BytesToAddress([]byte{0xaa, 0xbb})
	// No V, R, S set (all nil).
	tx := types.NewTransaction(&types.LegacyTx{
		Nonce:    0,
		GasPrice: big.NewInt(2_000_000_000),
		Gas:      21000,
		To:       &to,
		Value:    big.NewInt(0),
	})
	result := v.ValidateTx(tx)
	if result.Valid {
		t.Fatal("expected invalid for missing signature")
	}
	if result.Error != ErrTxNoSignature {
		t.Fatalf("expected ErrTxNoSignature, got %v", result.Error)
	}
}

func TestTxValidator_ZeroSignatureValues(t *testing.T) {
	// Use ChainID=0 to skip chain ID check, so we hit the signature check.
	v := NewTxValidator(TxValidationConfig{ChainID: 0})
	to := types.BytesToAddress([]byte{0xaa, 0xbb})
	// V is set but R and S are zero -- should be rejected.
	tx := types.NewTransaction(&types.LegacyTx{
		Nonce:    0,
		GasPrice: big.NewInt(2_000_000_000),
		Gas:      21000,
		To:       &to,
		Value:    big.NewInt(0),
		V:        big.NewInt(27),
		R:        big.NewInt(0),
		S:        big.NewInt(0),
	})
	result := v.ValidateTx(tx)
	if result.Valid {
		t.Fatal("expected invalid for zero R,S")
	}
	if result.Error != ErrTxNoSignature {
		t.Fatalf("expected ErrTxNoSignature, got %v", result.Error)
	}
}

func TestTxValidator_WrongChainID(t *testing.T) {
	v := NewTxValidator(TxValidationConfig{ChainID: 1})
	// EIP-1559 tx with chain ID 5 (Goerli).
	tx := makeValidatorDynTx(0, 1_000_000_000, 2_000_000_000, 21000, 5)
	result := v.ValidateTx(tx)
	if result.Valid {
		t.Fatal("expected invalid for wrong chain ID")
	}
	if result.Error != ErrTxBadChainID {
		t.Fatalf("expected ErrTxBadChainID, got %v", result.Error)
	}
}

func TestTxValidator_CorrectChainID(t *testing.T) {
	v := NewTxValidator(TxValidationConfig{ChainID: 1})
	tx := makeValidatorDynTx(0, 1_000_000_000, 2_000_000_000, 21000, 1)
	result := v.ValidateTx(tx)
	if !result.Valid {
		t.Fatalf("expected valid, got error: %v", result.Error)
	}
}

func TestTxValidator_BatchValidation(t *testing.T) {
	v := NewTxValidator(TxValidationConfig{ChainID: 1})

	txs := []*types.Transaction{
		// Valid tx.
		makeValidatorTx(0, 2_000_000_000, 21000, nil, big.NewInt(0)),
		// Invalid: zero gas.
		makeValidatorTx(1, 2_000_000_000, 0, nil, big.NewInt(0)),
		// Valid tx.
		makeValidatorDynTx(2, 1_000_000_000, 2_000_000_000, 21000, 1),
	}

	results := v.ValidateBatch(txs)
	if len(results) != 3 {
		t.Fatalf("expected 3 results, got %d", len(results))
	}
	if !results[0].Valid {
		t.Errorf("tx[0] should be valid, got error: %v", results[0].Error)
	}
	if results[1].Valid {
		t.Error("tx[1] should be invalid (zero gas)")
	}
	if results[1].Error != ErrTxGasTooLow {
		t.Errorf("tx[1] expected ErrTxGasTooLow, got %v", results[1].Error)
	}
	if !results[2].Valid {
		t.Errorf("tx[2] should be valid, got error: %v", results[2].Error)
	}
}

func TestTxValidator_IndividualMethods(t *testing.T) {
	v := NewTxValidator(DefaultTxValidationConfig())

	// ValidateBasic with zero gas.
	tx := makeValidatorTx(0, 2_000_000_000, 0, nil, big.NewInt(0))
	if err := v.ValidateBasic(tx); err != ErrTxGasTooLow {
		t.Errorf("ValidateBasic: expected ErrTxGasTooLow, got %v", err)
	}

	// ValidateGas with gas too high.
	tx = makeValidatorTx(0, 2_000_000_000, 50_000_000, nil, big.NewInt(0))
	if err := v.ValidateGas(tx); err != ErrTxGasTooHigh {
		t.Errorf("ValidateGas: expected ErrTxGasTooHigh, got %v", err)
	}

	// ValidateSize with oversized data.
	v2 := NewTxValidator(TxValidationConfig{MaxDataSize: 10})
	data := make([]byte, 20)
	tx = makeValidatorTx(0, 2_000_000_000, 21000, data, big.NewInt(0))
	if err := v2.ValidateSize(tx); err != ErrTxDataTooLarge {
		t.Errorf("ValidateSize: expected ErrTxDataTooLarge, got %v", err)
	}

	// ValidateChainID mismatch.
	tx = makeValidatorDynTx(0, 1_000_000_000, 2_000_000_000, 21000, 5)
	if err := v.ValidateChainID(tx, 1); err != ErrTxBadChainID {
		t.Errorf("ValidateChainID: expected ErrTxBadChainID, got %v", err)
	}

	// ValidateSignature with no signature.
	to := types.BytesToAddress([]byte{0xaa})
	tx = types.NewTransaction(&types.LegacyTx{
		Nonce: 0, GasPrice: big.NewInt(2e9), Gas: 21000, To: &to, Value: big.NewInt(0),
	})
	if err := v.ValidateSignature(tx); err != ErrTxNoSignature {
		t.Errorf("ValidateSignature: expected ErrTxNoSignature, got %v", err)
	}
}

func TestTxValidator_DefaultConfig(t *testing.T) {
	cfg := DefaultTxValidationConfig()
	if cfg.MinGasPrice.Cmp(oneGwei) != 0 {
		t.Errorf("expected MinGasPrice=%v, got %v", oneGwei, cfg.MinGasPrice)
	}
	if cfg.MaxGasLimit != 30_000_000 {
		t.Errorf("expected MaxGasLimit=30000000, got %d", cfg.MaxGasLimit)
	}
	if cfg.MaxDataSize != 128*1024 {
		t.Errorf("expected MaxDataSize=%d, got %d", 128*1024, cfg.MaxDataSize)
	}
	if cfg.ChainID != 1 {
		t.Errorf("expected ChainID=1, got %d", cfg.ChainID)
	}
}

func TestTxValidator_NilFieldsDefaults(t *testing.T) {
	// Creating a validator with zero-value config should populate defaults.
	v := NewTxValidator(TxValidationConfig{})
	tx := makeValidatorTx(0, 2_000_000_000, 21000, nil, big.NewInt(0))
	// With ChainID=0, chain ID check is skipped.
	result := v.ValidateTx(tx)
	if !result.Valid {
		t.Fatalf("expected valid tx with default config, got: %v", result.Error)
	}
}

func TestTxValidator_ConcurrentAccess(t *testing.T) {
	v := NewTxValidator(DefaultTxValidationConfig())
	tx := makeValidatorTx(0, 2_000_000_000, 21000, nil, big.NewInt(0))

	var wg sync.WaitGroup
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			result := v.ValidateTx(tx)
			if !result.Valid {
				t.Errorf("concurrent validation failed: %v", result.Error)
			}
		}()
	}
	wg.Wait()
}

func TestTxValidator_NoChainIDCheck(t *testing.T) {
	// ChainID=0 in config means skip chain ID validation.
	v := NewTxValidator(TxValidationConfig{ChainID: 0})
	tx := makeValidatorDynTx(0, 1_000_000_000, 2_000_000_000, 21000, 999)
	result := v.ValidateTx(tx)
	if !result.Valid {
		t.Fatalf("expected valid when ChainID check is disabled, got: %v", result.Error)
	}
	// "chainid" check should not appear in passed checks.
	for _, check := range result.Checks {
		if check == "chainid" {
			t.Error("chainid check should be skipped when config ChainID is 0")
		}
	}
}
