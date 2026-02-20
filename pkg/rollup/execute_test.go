package rollup

import (
	"encoding/binary"
	"testing"

	"github.com/eth2028/eth2028/core/types"
	"github.com/eth2028/eth2028/rlp"
)

func TestExecutePrecompileRequiredGasMinimum(t *testing.T) {
	ep := &ExecutePrecompile{}
	// Input shorter than minInputLen.
	gas := ep.RequiredGas(make([]byte, 10))
	if gas != ExecuteBaseGas {
		t.Errorf("RequiredGas(short) = %d, want %d", gas, ExecuteBaseGas)
	}
}

func TestExecutePrecompileRequiredGasWithBlockData(t *testing.T) {
	ep := &ExecutePrecompile{}
	// Craft an input with blockDataLen=100.
	input := make([]byte, minInputLen)
	binary.BigEndian.PutUint32(input[40:44], 100) // blockDataLen=100

	gas := ep.RequiredGas(input)
	expected := ExecuteBaseGas + 100*ExecutePerByteGas
	if gas != expected {
		t.Errorf("RequiredGas = %d, want %d", gas, expected)
	}
}

func TestExecutePrecompileRequiredGasLargeData(t *testing.T) {
	ep := &ExecutePrecompile{}
	input := make([]byte, minInputLen)
	binary.BigEndian.PutUint32(input[40:44], 10000)

	gas := ep.RequiredGas(input)
	expected := ExecuteBaseGas + 10000*ExecutePerByteGas
	if gas != expected {
		t.Errorf("RequiredGas = %d, want %d", gas, expected)
	}
}

func TestDecodeExecuteInputTooShort(t *testing.T) {
	_, err := decodeExecuteInput(make([]byte, 10))
	if err != ErrInputTooShort {
		t.Errorf("decodeExecuteInput(short) error = %v, want ErrInputTooShort", err)
	}
}

func TestDecodeExecuteInputValid(t *testing.T) {
	blockData := []byte{0xaa, 0xbb, 0xcc}
	witness := []byte{0xdd}
	anchor := []byte{0xee, 0xff}

	input := make([]byte, minInputLen+len(blockData)+len(witness)+len(anchor))
	binary.BigEndian.PutUint64(input[0:8], 42)    // chainID
	copy(input[8:40], types.BytesToHash([]byte{0x01}).Bytes()) // preStateRoot
	binary.BigEndian.PutUint32(input[40:44], uint32(len(blockData)))
	binary.BigEndian.PutUint32(input[44:48], uint32(len(witness)))
	binary.BigEndian.PutUint32(input[48:52], uint32(len(anchor)))
	offset := 52
	copy(input[offset:], blockData)
	offset += len(blockData)
	copy(input[offset:], witness)
	offset += len(witness)
	copy(input[offset:], anchor)

	result, err := decodeExecuteInput(input)
	if err != nil {
		t.Fatalf("decodeExecuteInput error: %v", err)
	}
	if result.ChainID != 42 {
		t.Errorf("ChainID = %d, want 42", result.ChainID)
	}
	if len(result.BlockData) != 3 {
		t.Errorf("BlockData length = %d, want 3", len(result.BlockData))
	}
	if len(result.Witness) != 1 {
		t.Errorf("Witness length = %d, want 1", len(result.Witness))
	}
	if len(result.AnchorData) != 2 {
		t.Errorf("AnchorData length = %d, want 2", len(result.AnchorData))
	}
}

func TestDecodeExecuteInputInsufficientData(t *testing.T) {
	// Header says blockDataLen=100 but actual data is too short.
	input := make([]byte, minInputLen)
	binary.BigEndian.PutUint32(input[40:44], 100)
	binary.BigEndian.PutUint32(input[44:48], 0)
	binary.BigEndian.PutUint32(input[48:52], 0)

	_, err := decodeExecuteInput(input)
	if err != ErrInputTooShort {
		t.Errorf("decodeExecuteInput(short data) error = %v, want ErrInputTooShort", err)
	}
}

func TestEncodeExecuteOutput(t *testing.T) {
	output := &ExecuteOutput{
		PostStateRoot: types.BytesToHash([]byte{0x01}),
		ReceiptsRoot:  types.BytesToHash([]byte{0x02}),
		GasUsed:       21000,
		BurnedFees:    1000,
		Success:       true,
	}

	encoded := encodeExecuteOutput(output)
	if len(encoded) != 81 {
		t.Fatalf("encoded length = %d, want 81", len(encoded))
	}

	// Verify gas used.
	gasUsed := binary.BigEndian.Uint64(encoded[64:72])
	if gasUsed != 21000 {
		t.Errorf("encoded gasUsed = %d, want 21000", gasUsed)
	}

	// Verify burned fees.
	burned := binary.BigEndian.Uint64(encoded[72:80])
	if burned != 1000 {
		t.Errorf("encoded burnedFees = %d, want 1000", burned)
	}

	// Verify success byte.
	if encoded[80] != 1 {
		t.Errorf("success byte = %d, want 1", encoded[80])
	}
}

func TestEncodeExecuteOutputFailure(t *testing.T) {
	output := &ExecuteOutput{
		Success: false,
	}
	encoded := encodeExecuteOutput(output)
	if encoded[80] != 0 {
		t.Errorf("success byte = %d, want 0 for failure", encoded[80])
	}
}

func TestExecutePrecompileRunInputTooShort(t *testing.T) {
	ep := &ExecutePrecompile{}
	_, err := ep.Run(make([]byte, 10))
	if err != ErrInputTooShort {
		t.Errorf("Run(short) error = %v, want ErrInputTooShort", err)
	}
}

func TestExecutePrecompileRunInvalidChainID(t *testing.T) {
	ep := &ExecutePrecompile{}
	// Craft input with ChainID=0, which should fail.
	blockData := []byte{0x01}
	input := make([]byte, minInputLen+len(blockData))
	binary.BigEndian.PutUint64(input[0:8], 0) // chainID=0
	copy(input[8:40], types.BytesToHash([]byte{0x01}).Bytes())
	binary.BigEndian.PutUint32(input[40:44], uint32(len(blockData)))

	offset := 52
	copy(input[offset:], blockData)

	result, err := ep.Run(input)
	if err != nil {
		t.Fatalf("Run error (should return failure output): %v", err)
	}
	// The precompile returns a failure output rather than error.
	if result[80] != 0 {
		t.Error("expected success=false for chainID=0")
	}
}

func TestExecutePrecompileRunValidBlock(t *testing.T) {
	ep := &ExecutePrecompile{}

	// Create valid block data (simple non-empty bytes).
	blockData := []byte{0x01, 0x02, 0x03}

	input := make([]byte, minInputLen+len(blockData))
	binary.BigEndian.PutUint64(input[0:8], 1)                                    // chainID=1
	copy(input[8:40], types.BytesToHash([]byte{0x01}).Bytes())                     // preStateRoot
	binary.BigEndian.PutUint32(input[40:44], uint32(len(blockData)))              // blockDataLen
	binary.BigEndian.PutUint32(input[44:48], 0)                                    // witnessLen
	binary.BigEndian.PutUint32(input[48:52], 0)                                    // anchorDataLen
	copy(input[52:], blockData)

	result, err := ep.Run(input)
	if err != nil {
		t.Fatalf("Run error: %v", err)
	}
	if len(result) != 81 {
		t.Fatalf("result length = %d, want 81", len(result))
	}
	// Should succeed.
	if result[80] != 1 {
		t.Error("expected success=true for valid block")
	}
}

func TestCheckNoBlobTransactionsClean(t *testing.T) {
	// Normal RLP list of transactions without blob type.
	txList := [][]byte{
		{0x02, 0x01, 0x02}, // type 2 (EIP-1559)
		{0x01, 0x03, 0x04}, // type 1 (access list)
	}
	encoded, _ := rlp.EncodeToBytes(txList)
	if err := checkNoBlobTransactions(encoded); err != nil {
		t.Errorf("checkNoBlobTransactions(clean) = %v, want nil", err)
	}
}

func TestCheckNoBlobTransactionsWithBlob(t *testing.T) {
	txList := [][]byte{
		{0x02, 0x01, 0x02},                  // type 2 (EIP-1559)
		{types.BlobTxType, 0x01, 0x02, 0x03}, // blob tx
	}
	encoded, _ := rlp.EncodeToBytes(txList)
	err := checkNoBlobTransactions(encoded)
	if err != ErrBlobTxNotAllowed {
		t.Errorf("checkNoBlobTransactions(blob) = %v, want ErrBlobTxNotAllowed", err)
	}
}

func TestCheckNoBlobTransactionsInvalidRLP(t *testing.T) {
	// If data is not valid RLP list, should return nil (no error).
	err := checkNoBlobTransactions([]byte{0xff, 0xfe})
	if err != nil {
		t.Errorf("checkNoBlobTransactions(invalid rlp) = %v, want nil", err)
	}
}

func TestExecuteConstants(t *testing.T) {
	if ExecuteBaseGas != 100_000 {
		t.Errorf("ExecuteBaseGas = %d, want 100000", ExecuteBaseGas)
	}
	if ExecutePerByteGas != 16 {
		t.Errorf("ExecutePerByteGas = %d, want 16", ExecutePerByteGas)
	}
	if MaxBlockDataSize != 1<<20 {
		t.Errorf("MaxBlockDataSize = %d, want %d", MaxBlockDataSize, 1<<20)
	}
	if minInputLen != 52 {
		t.Errorf("minInputLen = %d, want 52", minInputLen)
	}
}

func TestExecutePrecompileRunBlockDataTooLarge(t *testing.T) {
	ep := &ExecutePrecompile{}

	// Create input declaring block data > MaxBlockDataSize.
	largeSize := uint32(MaxBlockDataSize + 1)
	blockData := make([]byte, largeSize)

	input := make([]byte, minInputLen+len(blockData))
	binary.BigEndian.PutUint64(input[0:8], 1)
	copy(input[8:40], types.BytesToHash([]byte{0x01}).Bytes())
	binary.BigEndian.PutUint32(input[40:44], largeSize)
	binary.BigEndian.PutUint32(input[44:48], 0)
	binary.BigEndian.PutUint32(input[48:52], 0)
	copy(input[52:], blockData)

	_, err := ep.Run(input)
	if err != ErrBlockDataTooLarge {
		t.Errorf("Run(too large) error = %v, want ErrBlockDataTooLarge", err)
	}
}

func TestComputeSimulatedStateRootDeterministic(t *testing.T) {
	preState := types.BytesToHash([]byte{0x01})
	blockData := []byte{0xaa, 0xbb}

	root1 := computeSimulatedStateRoot(preState, blockData)
	root2 := computeSimulatedStateRoot(preState, blockData)

	if root1 != root2 {
		t.Error("computeSimulatedStateRoot should be deterministic")
	}
}

func TestComputeSimulatedStateRootDifferentInputs(t *testing.T) {
	preState1 := types.BytesToHash([]byte{0x01})
	preState2 := types.BytesToHash([]byte{0x02})
	blockData := []byte{0xaa}

	root1 := computeSimulatedStateRoot(preState1, blockData)
	root2 := computeSimulatedStateRoot(preState2, blockData)

	if root1 == root2 {
		t.Error("different preStateRoots should produce different outputs")
	}
}

func TestComputeSimulatedReceiptsRootDeterministic(t *testing.T) {
	data := []byte{0x01, 0x02, 0x03}
	root1 := computeSimulatedReceiptsRoot(data)
	root2 := computeSimulatedReceiptsRoot(data)
	if root1 != root2 {
		t.Error("computeSimulatedReceiptsRoot should be deterministic")
	}
}

func TestExecuteErrorSentinels(t *testing.T) {
	errors := []error{
		ErrInputTooShort,
		ErrBlockDataTooLarge,
		ErrBlobTxNotAllowed,
		ErrInvalidBlockData,
		ErrSTFailed,
		ErrInvalidChainID,
		ErrAnchorFailed,
	}
	seen := make(map[string]bool)
	for _, e := range errors {
		if e.Error() == "" {
			t.Errorf("error should have non-empty message")
		}
		if seen[e.Error()] {
			t.Errorf("duplicate error message: %s", e.Error())
		}
		seen[e.Error()] = true
	}
}
