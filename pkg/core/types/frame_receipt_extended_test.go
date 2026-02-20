package types

import (
	"math/big"
	"testing"
)

func makeTestFrameReceipt() *ExtendedFrameTxReceipt {
	return &ExtendedFrameTxReceipt{
		CumulativeGasUsed: 100000,
		Payer:             HexToAddress("0xdead"),
		EffectiveGasPrice: big.NewInt(20_000_000_000),
		FrameResults: []ExtendedFrameResult{
			{
				FrameIndex: 0,
				Status:     FrameStatusSuccess,
				GasUsed:    30000,
				GasBreakdown: FrameGasBreakdown{
					IntrinsicGas: 5000,
					ExecutionGas: 20000,
					CalldataGas:  4000,
					RefundGas:    1000,
				},
				Logs: []*Log{
					{Address: HexToAddress("0xaabb"), Topics: []Hash{HexToHash("0x01")}},
				},
				ReturnData: []byte{0x01},
				SubFrames: []SubFrameResult{
					{
						Target:     HexToAddress("0xccdd"),
						Status:     FrameStatusSuccess,
						GasUsed:    5000,
						ReturnData: []byte{0x02},
					},
				},
			},
			{
				FrameIndex: 1,
				Status:     FrameStatusRevert,
				GasUsed:    15000,
				GasBreakdown: FrameGasBreakdown{
					IntrinsicGas: 3000,
					ExecutionGas: 10000,
					CalldataGas:  2000,
					RefundGas:    0,
				},
				Logs:       []*Log{},
				ReturnData: []byte{0x08, 0xc3, 0x79, 0xa0}, // revert selector
			},
		},
	}
}

func TestExtendedFrameReceipt_TotalGasUsed(t *testing.T) {
	r := makeTestFrameReceipt()
	want := uint64(30000 + 15000)
	if got := r.TotalGasUsed(); got != want {
		t.Errorf("TotalGasUsed() = %d, want %d", got, want)
	}
}

func TestExtendedFrameReceipt_AllLogs(t *testing.T) {
	r := makeTestFrameReceipt()
	logs := r.AllLogs()
	if len(logs) != 1 {
		t.Errorf("AllLogs() count = %d, want 1", len(logs))
	}
}

func TestExtendedFrameReceipt_SuccessFailure(t *testing.T) {
	r := makeTestFrameReceipt()
	if r.SuccessCount() != 1 {
		t.Errorf("SuccessCount() = %d, want 1", r.SuccessCount())
	}
	if r.FailureCount() != 1 {
		t.Errorf("FailureCount() = %d, want 1", r.FailureCount())
	}
}

func TestExtendedFrameReceipt_FrameAt(t *testing.T) {
	r := makeTestFrameReceipt()

	fr, err := r.FrameAt(0)
	if err != nil {
		t.Fatalf("FrameAt(0) error: %v", err)
	}
	if fr.FrameIndex != 0 {
		t.Errorf("FrameAt(0).FrameIndex = %d, want 0", fr.FrameIndex)
	}

	_, err = r.FrameAt(-1)
	if err != ErrFrameResultInvalidIndex {
		t.Errorf("FrameAt(-1) should return ErrFrameResultInvalidIndex, got %v", err)
	}

	_, err = r.FrameAt(5)
	if err != ErrFrameResultInvalidIndex {
		t.Errorf("FrameAt(5) should return ErrFrameResultInvalidIndex, got %v", err)
	}
}

func TestExtendedFrameResult_Status(t *testing.T) {
	r := makeTestFrameReceipt()
	if !r.FrameResults[0].Succeeded() {
		t.Error("frame 0 should have succeeded")
	}
	if r.FrameResults[0].Reverted() {
		t.Error("frame 0 should not be reverted")
	}
	if r.FrameResults[1].Succeeded() {
		t.Error("frame 1 should not have succeeded")
	}
	if !r.FrameResults[1].Reverted() {
		t.Error("frame 1 should be reverted")
	}
}

func TestExtendedFrameResult_SubFrameGasUsed(t *testing.T) {
	r := makeTestFrameReceipt()
	if got := r.FrameResults[0].SubFrameGasUsed(); got != 5000 {
		t.Errorf("SubFrameGasUsed() = %d, want 5000", got)
	}
	if got := r.FrameResults[1].SubFrameGasUsed(); got != 0 {
		t.Errorf("SubFrameGasUsed() = %d, want 0", got)
	}
}

func TestFrameGasBreakdown_TotalConsumed(t *testing.T) {
	tests := []struct {
		name string
		gb   FrameGasBreakdown
		want uint64
	}{
		{
			name: "normal breakdown",
			gb:   FrameGasBreakdown{IntrinsicGas: 5000, ExecutionGas: 20000, CalldataGas: 4000, RefundGas: 1000},
			want: 28000,
		},
		{
			name: "zero refund",
			gb:   FrameGasBreakdown{IntrinsicGas: 3000, ExecutionGas: 10000, CalldataGas: 2000, RefundGas: 0},
			want: 15000,
		},
		{
			name: "all zeros",
			gb:   FrameGasBreakdown{},
			want: 0,
		},
		{
			name: "refund exceeds total clamps to zero",
			gb:   FrameGasBreakdown{IntrinsicGas: 100, ExecutionGas: 100, CalldataGas: 0, RefundGas: 500},
			want: 0,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.gb.TotalConsumed(); got != tt.want {
				t.Errorf("TotalConsumed() = %d, want %d", got, tt.want)
			}
		})
	}
}

func TestExtendedFrameReceipt_TotalCalldataGas(t *testing.T) {
	r := makeTestFrameReceipt()
	want := uint64(4000 + 2000)
	if got := r.TotalCalldataGas(); got != want {
		t.Errorf("TotalCalldataGas() = %d, want %d", got, want)
	}
}

func TestExtendedFrameReceipt_TotalRefund(t *testing.T) {
	r := makeTestFrameReceipt()
	want := uint64(1000)
	if got := r.TotalRefund(); got != want {
		t.Errorf("TotalRefund() = %d, want %d", got, want)
	}
}

func TestExtendedFrameReceipt_ComputeBloom(t *testing.T) {
	r := makeTestFrameReceipt()
	bloom := r.ComputeBloom()
	// The bloom should contain the log address from frame 0.
	addr := HexToAddress("0xaabb")
	if !BloomContains(bloom, addr.Bytes()) {
		t.Error("bloom should contain log address")
	}
}

func TestExtendedFrameReceipt_RollupFields(t *testing.T) {
	r := makeTestFrameReceipt()
	if r.HasRollupFields() {
		t.Error("should not have rollup fields")
	}
	if r.L1Cost().Sign() != 0 {
		t.Error("L1Cost should be zero without rollup fields")
	}

	r.RollupFields = &RollupFrameFields{
		L1GasUsed:     5000,
		L1GasPrice:    big.NewInt(30_000_000_000),
		L1Fee:         big.NewInt(150_000_000_000_000),
		FeeScalar:     big.NewInt(1_000_000),
		SequencerAddr: HexToAddress("0xseq1"),
	}
	if !r.HasRollupFields() {
		t.Error("should have rollup fields")
	}
	if r.L1Cost().Cmp(big.NewInt(150_000_000_000_000)) != 0 {
		t.Errorf("L1Cost() = %s, want 150000000000000", r.L1Cost())
	}
}

func TestValidateFrameReceipt(t *testing.T) {
	// nil receipt
	if err := ValidateFrameReceipt(nil); err != ErrFrameReceiptNil {
		t.Errorf("nil receipt: want ErrFrameReceiptNil, got %v", err)
	}

	// empty frame results
	r := &ExtendedFrameTxReceipt{CumulativeGasUsed: 100}
	if err := ValidateFrameReceipt(r); err != ErrFrameResultsEmpty {
		t.Errorf("empty results: want ErrFrameResultsEmpty, got %v", err)
	}

	// gas exceeds cumulative
	r = &ExtendedFrameTxReceipt{
		CumulativeGasUsed: 10,
		FrameResults: []ExtendedFrameResult{
			{GasUsed: 20},
		},
	}
	if err := ValidateFrameReceipt(r); err != ErrFrameGasExceedsCumul {
		t.Errorf("gas exceeds: want ErrFrameGasExceedsCumul, got %v", err)
	}

	// too many sub-frames
	subs := make([]SubFrameResult, MaxSubFrames+1)
	r = &ExtendedFrameTxReceipt{
		CumulativeGasUsed: 1000,
		FrameResults: []ExtendedFrameResult{
			{GasUsed: 100, SubFrames: subs},
		},
	}
	if err := ValidateFrameReceipt(r); err != ErrTooManySubFrames {
		t.Errorf("too many sub-frames: want ErrTooManySubFrames, got %v", err)
	}

	// valid receipt
	r = makeTestFrameReceipt()
	if err := ValidateFrameReceipt(r); err != nil {
		t.Errorf("valid receipt: unexpected error %v", err)
	}
}

func TestEncodeDecodeExtendedFrameReceipt(t *testing.T) {
	r := makeTestFrameReceipt()
	encoded, err := EncodeExtendedFrameReceipt(r)
	if err != nil {
		t.Fatalf("encode error: %v", err)
	}
	if len(encoded) == 0 {
		t.Fatal("encoded data should not be empty")
	}

	decoded, err := DecodeExtendedFrameResults(encoded)
	if err != nil {
		t.Fatalf("decode error: %v", err)
	}
	if len(decoded) != len(r.FrameResults) {
		t.Fatalf("decoded %d results, want %d", len(decoded), len(r.FrameResults))
	}
	for i, fr := range decoded {
		orig := r.FrameResults[i]
		if fr.FrameIndex != orig.FrameIndex {
			t.Errorf("result %d: FrameIndex = %d, want %d", i, fr.FrameIndex, orig.FrameIndex)
		}
		if fr.Status != orig.Status {
			t.Errorf("result %d: Status = %d, want %d", i, fr.Status, orig.Status)
		}
		if fr.GasUsed != orig.GasUsed {
			t.Errorf("result %d: GasUsed = %d, want %d", i, fr.GasUsed, orig.GasUsed)
		}
		if fr.GasBreakdown.IntrinsicGas != orig.GasBreakdown.IntrinsicGas {
			t.Errorf("result %d: IntrinsicGas = %d, want %d", i, fr.GasBreakdown.IntrinsicGas, orig.GasBreakdown.IntrinsicGas)
		}
		if fr.GasBreakdown.ExecutionGas != orig.GasBreakdown.ExecutionGas {
			t.Errorf("result %d: ExecutionGas = %d, want %d", i, fr.GasBreakdown.ExecutionGas, orig.GasBreakdown.ExecutionGas)
		}
		if len(fr.SubFrames) != len(orig.SubFrames) {
			t.Errorf("result %d: SubFrames count = %d, want %d", i, len(fr.SubFrames), len(orig.SubFrames))
		}
	}
}

func TestEncodeExtendedFrameReceiptNil(t *testing.T) {
	_, err := EncodeExtendedFrameReceipt(nil)
	if err != ErrFrameReceiptNil {
		t.Errorf("nil encode: want ErrFrameReceiptNil, got %v", err)
	}
}

func TestComputeL1Fee(t *testing.T) {
	tests := []struct {
		name       string
		l1GasUsed  uint64
		l1GasPrice *big.Int
		feeScalar  *big.Int
		want       *big.Int
	}{
		{
			name:       "basic computation",
			l1GasUsed:  1000,
			l1GasPrice: big.NewInt(30_000_000_000),
			feeScalar:  big.NewInt(1_000_000), // 1.0 scalar
			want:       big.NewInt(30_000_000_000_000),
		},
		{
			name:       "nil gas price",
			l1GasUsed:  1000,
			l1GasPrice: nil,
			feeScalar:  big.NewInt(1_000_000),
			want:       big.NewInt(0),
		},
		{
			name:       "nil fee scalar",
			l1GasUsed:  1000,
			l1GasPrice: big.NewInt(30_000_000_000),
			feeScalar:  nil,
			want:       big.NewInt(0),
		},
		{
			name:       "zero gas used",
			l1GasUsed:  0,
			l1GasPrice: big.NewInt(30_000_000_000),
			feeScalar:  big.NewInt(1_000_000),
			want:       big.NewInt(0),
		},
		{
			name:       "half scalar",
			l1GasUsed:  2000,
			l1GasPrice: big.NewInt(10_000_000_000),
			feeScalar:  big.NewInt(500_000), // 0.5 scalar
			want:       big.NewInt(10_000_000_000_000),
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ComputeL1Fee(tt.l1GasUsed, tt.l1GasPrice, tt.feeScalar)
			if got.Cmp(tt.want) != 0 {
				t.Errorf("ComputeL1Fee() = %s, want %s", got, tt.want)
			}
		})
	}
}

func TestFrameStatusConstants(t *testing.T) {
	if FrameStatusSuccess != 1 {
		t.Errorf("FrameStatusSuccess = %d, want 1", FrameStatusSuccess)
	}
	if FrameStatusRevert != 0 {
		t.Errorf("FrameStatusRevert = %d, want 0", FrameStatusRevert)
	}
	if FrameStatusHalt != 2 {
		t.Errorf("FrameStatusHalt = %d, want 2", FrameStatusHalt)
	}
}

func TestExtendedFrameReceipt_BlobFields(t *testing.T) {
	r := makeTestFrameReceipt()
	r.BlobGasUsed = 131072
	r.BlobGasPrice = big.NewInt(1000)
	if r.BlobGasUsed != 131072 {
		t.Errorf("BlobGasUsed = %d, want 131072", r.BlobGasUsed)
	}
	if r.BlobGasPrice.Int64() != 1000 {
		t.Errorf("BlobGasPrice = %s, want 1000", r.BlobGasPrice)
	}
}
