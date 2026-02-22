package engine

import (
	"math/big"
	"testing"

	"github.com/eth2030/eth2030/core/types"
)

// --- helpers ---

func makeValidV3PayloadForForkValidator() *ExecutionPayloadV3 {
	return &ExecutionPayloadV3{
		ExecutionPayloadV2: ExecutionPayloadV2{
			ExecutionPayloadV1: ExecutionPayloadV1{
				ParentHash:    types.HexToHash("0xaa"),
				FeeRecipient:  types.HexToAddress("0xbb"),
				StateRoot:     types.HexToHash("0xcc"),
				ReceiptsRoot:  types.HexToHash("0xdd"),
				BlockNumber:   100,
				GasLimit:      30_000_000,
				GasUsed:       15_000_000,
				Timestamp:     1700000012,
				BaseFeePerGas: big.NewInt(1_000_000_000),
				BlockHash:     types.HexToHash("0xee"),
				Transactions:  [][]byte{{0x02, 0x01}},
			},
			Withdrawals: []*Withdrawal{},
		},
		BlobGasUsed:   0,
		ExcessBlobGas: 0,
	}
}

func makeParentCtx() *ParentContext {
	return &ParentContext{
		Hash:          types.HexToHash("0xaa"),
		Timestamp:     1700000000,
		GasLimit:      30_000_000,
		GasUsed:       15_000_000,
		BaseFee:       big.NewInt(1_000_000_000),
		ExcessBlobGas: 0,
	}
}

// --- TestForkPayloadValidatorNilPayload ---

func TestForkPayloadValidatorNilPayload(t *testing.T) {
	v := NewForkPayloadValidator(ForkCancun)
	errs := v.ValidateNewPayload(nil, makeParentCtx())
	if len(errs) == 0 {
		t.Fatal("expected error for nil payload")
	}
}

// --- TestForkPayloadValidatorNilParent ---

func TestForkPayloadValidatorNilParent(t *testing.T) {
	v := NewForkPayloadValidator(ForkCancun)
	p := makeValidV3PayloadForForkValidator()
	errs := v.ValidateNewPayload(p, nil)
	if len(errs) == 0 {
		t.Fatal("expected error for nil parent context")
	}
}

// --- TestForkPayloadValidatorParentHashZero ---

func TestForkPayloadValidatorParentHashZero(t *testing.T) {
	v := NewForkPayloadValidator(ForkCancun)
	p := makeValidV3PayloadForForkValidator()
	p.ParentHash = types.Hash{}
	errs := v.ValidateNewPayload(p, makeParentCtx())
	if len(errs) == 0 {
		t.Fatal("expected error for zero parent hash")
	}
}

// --- TestForkPayloadValidatorParentHashMismatch ---

func TestForkPayloadValidatorParentHashMismatch(t *testing.T) {
	v := NewForkPayloadValidator(ForkCancun)
	p := makeValidV3PayloadForForkValidator()
	p.ParentHash = types.HexToHash("0xff")
	errs := v.ValidateNewPayload(p, makeParentCtx())
	if len(errs) == 0 {
		t.Fatal("expected error for parent hash mismatch")
	}
}

// --- TestForkPayloadValidatorTimestampNotAfterParent ---

func TestForkPayloadValidatorTimestampNotAfterParent(t *testing.T) {
	v := NewForkPayloadValidator(ForkCancun)
	p := makeValidV3PayloadForForkValidator()
	parent := makeParentCtx()
	p.Timestamp = parent.Timestamp // equal, not strictly after
	errs := v.ValidateNewPayload(p, parent)
	if len(errs) == 0 {
		t.Fatal("expected error for timestamp not after parent")
	}
}

// --- TestForkPayloadValidatorTimestampBeforeParent ---

func TestForkPayloadValidatorTimestampBeforeParent(t *testing.T) {
	v := NewForkPayloadValidator(ForkCancun)
	p := makeValidV3PayloadForForkValidator()
	parent := makeParentCtx()
	p.Timestamp = parent.Timestamp - 1
	errs := v.ValidateNewPayload(p, parent)
	if len(errs) == 0 {
		t.Fatal("expected error for timestamp before parent")
	}
}

// --- TestForkPayloadValidatorGasUsedExceedsLimit ---

func TestForkPayloadValidatorGasUsedExceedsLimit(t *testing.T) {
	v := NewForkPayloadValidator(ForkCancun)
	p := makeValidV3PayloadForForkValidator()
	p.GasUsed = p.GasLimit + 1
	errs := v.ValidateNewPayload(p, makeParentCtx())
	if len(errs) == 0 {
		t.Fatal("expected error for gas used exceeding limit")
	}
}

// --- TestForkPayloadValidatorBaseFeeNil ---

func TestForkPayloadValidatorBaseFeeNil(t *testing.T) {
	v := NewForkPayloadValidator(ForkCancun)
	p := makeValidV3PayloadForForkValidator()
	p.BaseFeePerGas = nil
	errs := v.ValidateNewPayload(p, makeParentCtx())
	if len(errs) == 0 {
		t.Fatal("expected error for nil base fee")
	}
}

// --- TestForkPayloadValidatorBaseFeeCorrect ---

func TestForkPayloadValidatorBaseFeeCorrect(t *testing.T) {
	v := NewForkPayloadValidator(ForkCancun)
	parent := makeParentCtx()

	// Compute expected base fee.
	parentGasTarget := parent.GasLimit / ElasticityMultiplier
	expected := CalcBaseFeeBig(parent.BaseFee, parent.GasUsed, parentGasTarget)

	p := makeValidV3PayloadForForkValidator()
	p.BaseFeePerGas = expected

	errs := v.ValidateNewPayload(p, parent)
	for _, e := range errs {
		if e != nil {
			t.Fatalf("unexpected error: %v", e)
		}
	}
}

// --- TestForkPayloadValidatorEmptyTransaction ---

func TestForkPayloadValidatorEmptyTransaction(t *testing.T) {
	v := NewForkPayloadValidator(ForkCancun)
	p := makeValidV3PayloadForForkValidator()
	p.Transactions = [][]byte{{}} // empty tx bytes

	parent := makeParentCtx()
	parentGasTarget := parent.GasLimit / ElasticityMultiplier
	p.BaseFeePerGas = CalcBaseFeeBig(parent.BaseFee, parent.GasUsed, parentGasTarget)

	errs := v.ValidateNewPayload(p, parent)
	if len(errs) == 0 {
		t.Fatal("expected error for empty transaction")
	}
}

// --- TestForkPayloadValidatorWithdrawalsNil ---

func TestForkPayloadValidatorWithdrawalsNil(t *testing.T) {
	v := NewForkPayloadValidator(ForkCancun)
	p := makeValidV3PayloadForForkValidator()
	p.Withdrawals = nil
	err := v.ValidateWithdrawals(p, nil)
	if err == nil {
		t.Fatal("expected error for nil withdrawals")
	}
}

// --- TestForkPayloadValidatorWithdrawalsCountMismatch ---

func TestForkPayloadValidatorWithdrawalsCountMismatch(t *testing.T) {
	v := NewForkPayloadValidator(ForkCancun)
	p := makeValidV3PayloadForForkValidator()
	p.Withdrawals = []*Withdrawal{{Index: 0, ValidatorIndex: 1, Address: types.HexToAddress("0x01"), Amount: 100}}

	expected := &ConsensusWithdrawals{
		Withdrawals: []*Withdrawal{
			{Index: 0, ValidatorIndex: 1, Address: types.HexToAddress("0x01"), Amount: 100},
			{Index: 1, ValidatorIndex: 2, Address: types.HexToAddress("0x02"), Amount: 200},
		},
	}
	err := v.ValidateWithdrawals(p, expected)
	if err == nil {
		t.Fatal("expected error for withdrawals count mismatch")
	}
}

// --- TestForkPayloadValidatorWithdrawalsMatch ---

func TestForkPayloadValidatorWithdrawalsMatch(t *testing.T) {
	v := NewForkPayloadValidator(ForkCancun)
	w := &Withdrawal{Index: 0, ValidatorIndex: 1, Address: types.HexToAddress("0x01"), Amount: 100}
	p := makeValidV3PayloadForForkValidator()
	p.Withdrawals = []*Withdrawal{w}

	expected := &ConsensusWithdrawals{Withdrawals: []*Withdrawal{w}}
	err := v.ValidateWithdrawals(p, expected)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

// --- TestForkPayloadValidatorBlobGasExceedsMax ---

func TestForkPayloadValidatorBlobGasExceedsMax(t *testing.T) {
	v := NewForkPayloadValidator(ForkCancun)
	p := makeValidV3PayloadForForkValidator()
	p.BlobGasUsed = v.maxBlobGasPerBlock + 1
	err := v.ValidateBlobGas(p, makeParentCtx())
	if err == nil {
		t.Fatal("expected error for blob gas exceeding max")
	}
}

// --- TestForkPayloadValidatorBlobGasExcessCorrect ---

func TestForkPayloadValidatorBlobGasExcessCorrect(t *testing.T) {
	v := NewForkPayloadValidator(ForkCancun)
	parent := makeParentCtx()
	parent.ExcessBlobGas = 100000
	p := makeValidV3PayloadForForkValidator()
	p.BlobGasUsed = 0
	targetBlobGas := v.targetBlobsPerBlock * v.blobGasPerBlob
	expectedExcess := calcExcessBlobGas(parent.ExcessBlobGas, 0, targetBlobGas)
	p.ExcessBlobGas = expectedExcess

	err := v.ValidateBlobGas(p, parent)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

// --- TestForkPayloadValidatorRequestsNilPrague ---

func TestForkPayloadValidatorRequestsNilPrague(t *testing.T) {
	v := NewForkPayloadValidator(ForkPrague)
	err := v.ValidateRequests(nil)
	if err == nil {
		t.Fatal("expected error for nil requests in Prague")
	}
}

// --- TestForkPayloadValidatorRequestsNotRequiredCancun ---

func TestForkPayloadValidatorRequestsNotRequiredCancun(t *testing.T) {
	v := NewForkPayloadValidator(ForkCancun)
	err := v.ValidateRequests(nil)
	if err != nil {
		t.Fatalf("unexpected error: %v (requests not required pre-Prague)", err)
	}
}

// --- TestForkPayloadValidatorValidateForForkPrague ---

func TestForkPayloadValidatorValidateForForkPrague(t *testing.T) {
	v := NewForkPayloadValidator(ForkPrague)
	p := &ExecutionPayloadV4{
		ExecutionPayloadV3: *makeValidV3PayloadForForkValidator(),
		ExecutionRequests:  nil,
	}
	err := v.ValidateForFork(p)
	if err == nil {
		t.Fatal("expected error for missing requests in Prague fork")
	}

	// With requests present.
	p.ExecutionRequests = [][]byte{}
	err = v.ValidateForFork(p)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

// --- TestForkPayloadValidatorValidateForForkUnknown ---

func TestForkPayloadValidatorValidateForForkUnknown(t *testing.T) {
	v := NewForkPayloadValidator("unknown_fork")
	p := &ExecutionPayloadV4{ExecutionPayloadV3: *makeValidV3PayloadForForkValidator()}
	err := v.ValidateForFork(p)
	if err == nil {
		t.Fatal("expected error for unknown fork")
	}
}

// --- TestComputeTransactionsRoot ---

func TestComputeTransactionsRootFork(t *testing.T) {
	root := ComputeTransactionsRoot(nil)
	if root != (types.Hash{}) {
		t.Fatal("expected zero hash for nil transactions")
	}

	root2 := ComputeTransactionsRoot([][]byte{{0x01, 0x02}, {0x03}})
	if root2 == (types.Hash{}) {
		t.Fatal("expected non-zero hash for non-empty transactions")
	}
}

// --- TestCalcExcessBlobGas ---

func TestCalcExcessBlobGasHelper(t *testing.T) {
	// When sum < target, excess should be 0.
	result := calcExcessBlobGas(0, 0, 100)
	if result != 0 {
		t.Fatalf("expected 0, got %d", result)
	}

	// When sum > target, excess should be sum - target.
	result = calcExcessBlobGas(200, 100, 150)
	if result != 150 {
		t.Fatalf("expected 150, got %d", result)
	}
}

// --- TestForkPayloadValidatorWithdrawalsAmountMismatch ---

func TestForkPayloadValidatorWithdrawalsAmountMismatch(t *testing.T) {
	v := NewForkPayloadValidator(ForkCancun)
	p := makeValidV3PayloadForForkValidator()
	p.Withdrawals = []*Withdrawal{
		{Index: 0, ValidatorIndex: 1, Address: types.HexToAddress("0x01"), Amount: 100},
	}
	expected := &ConsensusWithdrawals{
		Withdrawals: []*Withdrawal{
			{Index: 0, ValidatorIndex: 1, Address: types.HexToAddress("0x01"), Amount: 200},
		},
	}
	err := v.ValidateWithdrawals(p, expected)
	if err == nil {
		t.Fatal("expected error for withdrawal amount mismatch")
	}
}
