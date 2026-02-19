package rollup

import (
	"bytes"
	"encoding/binary"
	"math/big"
	"testing"

	"github.com/eth2028/eth2028/core/types"
	"github.com/eth2028/eth2028/rlp"
)

// --- EXECUTE precompile tests ---

func TestExecutePrecompileBasic(t *testing.T) {
	precompile := &ExecutePrecompile{}

	// Build valid input with a simple block data payload.
	blockData := []byte{0xf8, 0x01, 0x02, 0x03} // minimal RLP-ish data
	input := buildExecuteInput(1, types.Hash{1}, blockData, nil, nil)

	output, err := precompile.Run(input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(output) != 81 {
		t.Fatalf("expected 81 bytes output, got %d", len(output))
	}

	// Check success byte.
	if output[80] != 1 {
		t.Error("expected success=true")
	}

	// Post-state root should be non-zero.
	var postState types.Hash
	copy(postState[:], output[0:32])
	if postState == (types.Hash{}) {
		t.Error("expected non-zero post-state root")
	}
}

func TestExecutePrecompileInvalidInput(t *testing.T) {
	precompile := &ExecutePrecompile{}

	tests := []struct {
		name  string
		input []byte
	}{
		{"nil input", nil},
		{"empty input", []byte{}},
		{"too short", make([]byte, 10)},
		{"header only, no data", make([]byte, 51)},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := precompile.Run(tt.input)
			if err == nil {
				t.Error("expected error for invalid input")
			}
		})
	}
}

func TestExecutePrecompileGas(t *testing.T) {
	precompile := &ExecutePrecompile{}

	tests := []struct {
		name        string
		blockSize   int
		expectedGas uint64
	}{
		{"empty block", 0, ExecuteBaseGas},
		{"small block", 100, ExecuteBaseGas + 100*ExecutePerByteGas},
		{"1KB block", 1024, ExecuteBaseGas + 1024*ExecutePerByteGas},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			blockData := make([]byte, tt.blockSize)
			input := buildExecuteInput(1, types.Hash{}, blockData, nil, nil)

			gas := precompile.RequiredGas(input)
			if gas != tt.expectedGas {
				t.Errorf("expected gas %d, got %d", tt.expectedGas, gas)
			}
		})
	}
}

func TestExecutePrecompileGasShortInput(t *testing.T) {
	precompile := &ExecutePrecompile{}
	// Short input should return base gas.
	gas := precompile.RequiredGas([]byte{1, 2, 3})
	if gas != ExecuteBaseGas {
		t.Errorf("expected base gas %d for short input, got %d", ExecuteBaseGas, gas)
	}
}

func TestExecutePrecompileBlobTxRejection(t *testing.T) {
	precompile := &ExecutePrecompile{}

	// Create block data containing a blob transaction type.
	// Encode a list of transactions where one has blob type prefix.
	blobTx := []byte{types.BlobTxType, 0x01, 0x02}
	normalTx := []byte{types.DynamicFeeTxType, 0x01, 0x02}
	txList := [][]byte{normalTx, blobTx}
	blockData, err := rlp.EncodeToBytes(txList)
	if err != nil {
		t.Fatalf("failed to encode tx list: %v", err)
	}

	input := buildExecuteInput(1, types.Hash{1}, blockData, nil, nil)
	output, runErr := precompile.Run(input)

	// The precompile returns a failure output (success=0) rather than an error.
	if runErr != nil {
		t.Fatalf("unexpected hard error: %v", runErr)
	}
	if output[80] != 0 {
		t.Error("expected success=false for blob tx rejection")
	}
}

func TestExecutePrecompileWithAnchor(t *testing.T) {
	precompile := &ExecutePrecompile{}

	blockData := []byte{0x01, 0x02, 0x03}
	anchorData := EncodeAnchorData(AnchorState{
		LatestBlockHash: types.Hash{0xaa},
		LatestStateRoot: types.Hash{0xbb},
		BlockNumber:     100,
		Timestamp:       1700000000,
	})

	input := buildExecuteInput(42, types.Hash{1}, blockData, nil, anchorData)
	output, err := precompile.Run(input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if output[80] != 1 {
		t.Error("expected success=true with anchor data")
	}
}

func TestExecutePrecompileZeroChainID(t *testing.T) {
	precompile := &ExecutePrecompile{}

	blockData := []byte{0x01, 0x02, 0x03}
	input := buildExecuteInput(0, types.Hash{1}, blockData, nil, nil)
	output, err := precompile.Run(input)
	if err != nil {
		t.Fatalf("unexpected hard error: %v", err)
	}
	// Zero chain ID triggers STF failure -> success=false.
	if output[80] != 0 {
		t.Error("expected success=false for zero chain ID")
	}
}

func TestExecutePrecompileBlockTooLarge(t *testing.T) {
	precompile := &ExecutePrecompile{}

	// Build input claiming blockDataLen > MaxBlockDataSize.
	input := make([]byte, minInputLen)
	binary.BigEndian.PutUint64(input[0:8], 1)                        // chainID
	binary.BigEndian.PutUint32(input[40:44], uint32(MaxBlockDataSize+1)) // blockDataLen
	binary.BigEndian.PutUint32(input[44:48], 0)                      // witnessLen
	binary.BigEndian.PutUint32(input[48:52], 0)                      // anchorLen

	// Append the oversized data.
	data := make([]byte, MaxBlockDataSize+1)
	input = append(input, data...)

	_, err := precompile.Run(input)
	if err != ErrBlockDataTooLarge {
		t.Errorf("expected ErrBlockDataTooLarge, got %v", err)
	}
}

func TestExecutePrecompileDeterministicOutput(t *testing.T) {
	precompile := &ExecutePrecompile{}

	blockData := []byte{0xde, 0xad, 0xbe, 0xef}
	preState := types.Hash{0x11, 0x22, 0x33}

	input := buildExecuteInput(1, preState, blockData, nil, nil)

	// Run twice: output should be identical.
	out1, err1 := precompile.Run(input)
	out2, err2 := precompile.Run(input)

	if err1 != nil || err2 != nil {
		t.Fatalf("unexpected error: %v, %v", err1, err2)
	}
	if !bytes.Equal(out1, out2) {
		t.Error("expected deterministic output for identical inputs")
	}
}

func TestExecuteInputDecoding(t *testing.T) {
	chainID := uint64(42)
	preState := types.Hash{0xaa, 0xbb}
	blockData := []byte("block-data-here")
	witness := []byte("witness-here")
	anchor := []byte("anchor-data-here-padding-to-80b-aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa")

	input := buildExecuteInput(chainID, preState, blockData, witness, anchor)
	parsed, err := decodeExecuteInput(input)
	if err != nil {
		t.Fatalf("decode error: %v", err)
	}

	if parsed.ChainID != chainID {
		t.Errorf("chainID: got %d, want %d", parsed.ChainID, chainID)
	}
	if parsed.PreStateRoot != preState {
		t.Error("preStateRoot mismatch")
	}
	if !bytes.Equal(parsed.BlockData, blockData) {
		t.Error("blockData mismatch")
	}
	if !bytes.Equal(parsed.Witness, witness) {
		t.Error("witness mismatch")
	}
	if !bytes.Equal(parsed.AnchorData, anchor) {
		t.Error("anchorData mismatch")
	}
}

func TestExecuteOutputEncoding(t *testing.T) {
	output := &ExecuteOutput{
		PostStateRoot: types.Hash{0x01},
		ReceiptsRoot:  types.Hash{0x02},
		GasUsed:       21000,
		BurnedFees:    500,
		Success:       true,
	}

	encoded := encodeExecuteOutput(output)
	if len(encoded) != 81 {
		t.Fatalf("expected 81 bytes, got %d", len(encoded))
	}

	// Verify post-state root.
	if encoded[0] != 0x01 {
		t.Error("post-state root byte 0 mismatch")
	}
	// Verify receipts root.
	if encoded[32] != 0x02 {
		t.Error("receipts root byte 0 mismatch")
	}
	// Verify gasUsed.
	gasUsed := binary.BigEndian.Uint64(encoded[64:72])
	if gasUsed != 21000 {
		t.Errorf("gasUsed: got %d, want 21000", gasUsed)
	}
	// Verify burnedFees.
	burned := binary.BigEndian.Uint64(encoded[72:80])
	if burned != 500 {
		t.Errorf("burnedFees: got %d, want 500", burned)
	}
	// Verify success.
	if encoded[80] != 1 {
		t.Error("expected success byte = 1")
	}
}

// --- Anchor contract tests ---

func TestAnchorStateManagement(t *testing.T) {
	ac := NewAnchorContract()

	// Initial state should be empty.
	state := ac.GetLatestState()
	if state.BlockNumber != 0 {
		t.Errorf("expected initial block number 0, got %d", state.BlockNumber)
	}

	// Update state.
	err := ac.UpdateState(AnchorState{
		LatestBlockHash: types.Hash{0xaa},
		LatestStateRoot: types.Hash{0xbb},
		BlockNumber:     100,
		Timestamp:       1700000000,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	state = ac.GetLatestState()
	if state.BlockNumber != 100 {
		t.Errorf("expected block number 100, got %d", state.BlockNumber)
	}
	if state.LatestBlockHash != (types.Hash{0xaa}) {
		t.Error("block hash mismatch")
	}
}

func TestAnchorStaleBlock(t *testing.T) {
	ac := NewAnchorContract()

	// Set initial state at block 100.
	err := ac.UpdateState(AnchorState{BlockNumber: 100, Timestamp: 1})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Try to update with same block number: should fail.
	err = ac.UpdateState(AnchorState{BlockNumber: 100, Timestamp: 2})
	if err != ErrAnchorStaleBlock {
		t.Errorf("expected ErrAnchorStaleBlock, got %v", err)
	}

	// Try with lower block number: should fail.
	err = ac.UpdateState(AnchorState{BlockNumber: 50, Timestamp: 3})
	if err != ErrAnchorStaleBlock {
		t.Errorf("expected ErrAnchorStaleBlock, got %v", err)
	}

	// Higher block number should succeed.
	err = ac.UpdateState(AnchorState{BlockNumber: 101, Timestamp: 4})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestAnchorRingBuffer(t *testing.T) {
	ac := NewAnchorContract()

	// Store entries.
	for i := uint64(1); i <= 10; i++ {
		hash := types.Hash{}
		hash[0] = byte(i)
		err := ac.UpdateState(AnchorState{
			LatestBlockHash: hash,
			BlockNumber:     i,
			Timestamp:       i * 100,
		})
		if err != nil {
			t.Fatalf("unexpected error at block %d: %v", i, err)
		}
	}

	// Retrieve recent entries.
	for i := uint64(1); i <= 10; i++ {
		entry, ok := ac.GetAnchorByNumber(i)
		if !ok {
			t.Errorf("expected entry for block %d", i)
			continue
		}
		if entry.BlockHash[0] != byte(i) {
			t.Errorf("block %d: expected hash byte %d, got %d", i, i, entry.BlockHash[0])
		}
	}

	// Block 0 should not be retrievable.
	_, ok := ac.GetAnchorByNumber(0)
	if ok {
		t.Error("expected no entry for block 0")
	}

	// Future block should not be retrievable.
	_, ok = ac.GetAnchorByNumber(100)
	if ok {
		t.Error("expected no entry for future block")
	}
}

func TestAnchorProcessAnchorData(t *testing.T) {
	ac := NewAnchorContract()

	data := EncodeAnchorData(AnchorState{
		LatestBlockHash: types.Hash{0xdd},
		LatestStateRoot: types.Hash{0xee},
		BlockNumber:     42,
		Timestamp:       1700000000,
	})

	err := ac.ProcessAnchorData(data)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	state := ac.GetLatestState()
	if state.BlockNumber != 42 {
		t.Errorf("expected block 42, got %d", state.BlockNumber)
	}
	if state.LatestBlockHash != (types.Hash{0xdd}) {
		t.Error("block hash mismatch")
	}
	if state.LatestStateRoot != (types.Hash{0xee}) {
		t.Error("state root mismatch")
	}
}

func TestAnchorProcessAnchorDataTooShort(t *testing.T) {
	ac := NewAnchorContract()
	err := ac.ProcessAnchorData(make([]byte, 79))
	if err != ErrAnchorDataTooShort {
		t.Errorf("expected ErrAnchorDataTooShort, got %v", err)
	}
}

func TestAnchorEncodeDecodeRoundtrip(t *testing.T) {
	original := AnchorState{
		LatestBlockHash: types.Hash{0x11, 0x22, 0x33},
		LatestStateRoot: types.Hash{0x44, 0x55, 0x66},
		BlockNumber:     12345,
		Timestamp:       1700000000,
	}

	encoded := EncodeAnchorData(original)
	if len(encoded) != 80 {
		t.Fatalf("expected 80 bytes, got %d", len(encoded))
	}

	ac := NewAnchorContract()
	err := ac.ProcessAnchorData(encoded)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	decoded := ac.GetLatestState()
	if decoded.LatestBlockHash != original.LatestBlockHash {
		t.Error("block hash roundtrip mismatch")
	}
	if decoded.LatestStateRoot != original.LatestStateRoot {
		t.Error("state root roundtrip mismatch")
	}
	if decoded.BlockNumber != original.BlockNumber {
		t.Errorf("block number: got %d, want %d", decoded.BlockNumber, original.BlockNumber)
	}
	if decoded.Timestamp != original.Timestamp {
		t.Errorf("timestamp: got %d, want %d", decoded.Timestamp, original.Timestamp)
	}
}

// --- Type tests ---

func TestRollupConfigDefaults(t *testing.T) {
	cfg := RollupConfig{
		ChainID:          big.NewInt(42),
		AnchorAddress:    AnchorAddress,
		GenesisStateRoot: types.Hash{},
		GasLimit:         30_000_000,
		BaseFee:          big.NewInt(1_000_000_000),
		AllowBlobTx:      false,
	}

	if cfg.ChainID.Int64() != 42 {
		t.Errorf("expected chainID 42, got %d", cfg.ChainID.Int64())
	}
	if cfg.AllowBlobTx {
		t.Error("expected AllowBlobTx=false by default")
	}
}

func TestProofData(t *testing.T) {
	pd := ProofData{
		RollupID:      1,
		Proof:         []byte{0x01, 0x02, 0x03},
		PublicInputs:  []byte{0x04, 0x05, 0x06},
		PreStateRoot:  types.Hash{0xaa},
		PostStateRoot: types.Hash{0xbb},
	}

	if pd.RollupID != 1 {
		t.Errorf("expected rollupID 1, got %d", pd.RollupID)
	}
	if len(pd.Proof) != 3 {
		t.Errorf("expected 3 proof bytes, got %d", len(pd.Proof))
	}
}

func TestPrecompileAddresses(t *testing.T) {
	// EXECUTE precompile at 0x0101.
	if ExecutePrecompileAddress[18] != 0x01 || ExecutePrecompileAddress[19] != 0x01 {
		t.Error("unexpected EXECUTE precompile address")
	}

	// Anchor address at 0x0102.
	if AnchorAddress[18] != 0x01 || AnchorAddress[19] != 0x02 {
		t.Error("unexpected anchor address")
	}
}

// --- Helper ---

// buildExecuteInput constructs a valid EXECUTE precompile input.
func buildExecuteInput(chainID uint64, preState types.Hash, blockData, witness, anchor []byte) []byte {
	buf := make([]byte, minInputLen)
	binary.BigEndian.PutUint64(buf[0:8], chainID)
	copy(buf[8:40], preState[:])
	binary.BigEndian.PutUint32(buf[40:44], uint32(len(blockData)))
	binary.BigEndian.PutUint32(buf[44:48], uint32(len(witness)))
	binary.BigEndian.PutUint32(buf[48:52], uint32(len(anchor)))

	buf = append(buf, blockData...)
	buf = append(buf, witness...)
	buf = append(buf, anchor...)
	return buf
}
