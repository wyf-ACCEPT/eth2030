package witness

import (
	"bytes"
	"math/big"
	"testing"

	"github.com/eth2028/eth2028/core/types"
)

func TestNewBlockExecutionWitness(t *testing.T) {
	ph := types.HexToHash("0x1234")
	sr := types.HexToHash("0x5678")
	bew := NewBlockExecutionWitness(ph, sr, 100)

	if bew.ParentHash != ph {
		t.Errorf("ParentHash: got %v, want %v", bew.ParentHash, ph)
	}
	if bew.StateRoot != sr {
		t.Errorf("StateRoot: got %v, want %v", bew.StateRoot, sr)
	}
	if bew.BlockNum != 100 {
		t.Errorf("BlockNum: got %d, want 100", bew.BlockNum)
	}
	if bew.PreState == nil {
		t.Error("PreState should be initialized")
	}
	if bew.Codes == nil {
		t.Error("Codes should be initialized")
	}
}

func TestNewWitnessBuilder(t *testing.T) {
	ph := types.HexToHash("0xaaaa")
	sr := types.HexToHash("0xbbbb")
	wb := NewWitnessBuilder(ph, sr, 42)

	if wb.parentHash != ph {
		t.Errorf("parentHash mismatch")
	}
	if wb.stateRoot != sr {
		t.Errorf("stateRoot mismatch")
	}
	if wb.blockNum != 42 {
		t.Errorf("blockNum: got %d, want 42", wb.blockNum)
	}
}

func TestWitnessBuilderRecordRead(t *testing.T) {
	wb := NewWitnessBuilder(types.Hash{}, types.Hash{}, 1)

	addr := [20]byte{1, 2, 3}
	key := [32]byte{10, 20, 30}
	val := [32]byte{40, 50, 60}

	wb.RecordRead(addr, key, val)

	// Verify it was recorded.
	r, ok := wb.reads[types.Address(addr)]
	if !ok {
		t.Fatal("read not recorded")
	}
	stored, ok := r.storage[types.Hash(key)]
	if !ok {
		t.Fatal("storage slot not recorded")
	}
	if stored != types.Hash(val) {
		t.Errorf("stored value mismatch")
	}

	// Recording the same key again should not overwrite.
	newVal := [32]byte{99}
	wb.RecordRead(addr, key, newVal)
	stored2 := wb.reads[types.Address(addr)].storage[types.Hash(key)]
	if stored2 != types.Hash(val) {
		t.Errorf("second RecordRead overwrote first value")
	}
}

func TestWitnessBuilderRecordWrite(t *testing.T) {
	wb := NewWitnessBuilder(types.Hash{}, types.Hash{}, 1)

	addr := [20]byte{5}
	key := [32]byte{15}
	oldVal := [32]byte{25}
	newVal := [32]byte{35}

	wb.RecordWrite(addr, key, oldVal, newVal)

	// Check pre-state was recorded.
	r := wb.reads[types.Address(addr)]
	if r == nil {
		t.Fatal("read not created for write")
	}
	if r.storage[types.Hash(key)] != types.Hash(oldVal) {
		t.Error("pre-state not recorded")
	}

	// Check diff was recorded.
	w := wb.writes[types.Address(addr)]
	if w == nil {
		t.Fatal("write not created")
	}
	entry := w.storage[types.Hash(key)]
	if entry[0] != types.Hash(oldVal) {
		t.Error("diff old value mismatch")
	}
	if entry[1] != types.Hash(newVal) {
		t.Error("diff new value mismatch")
	}
}

func TestWitnessBuilderRecordCodeAccess(t *testing.T) {
	wb := NewWitnessBuilder(types.Hash{}, types.Hash{}, 1)

	addr := [20]byte{7}
	codeHash := [32]byte{8}
	code := []byte{0x60, 0x00, 0x60, 0x00, 0xFD}

	wb.RecordCodeAccess(addr, codeHash, code)

	// Verify code was stored.
	stored, ok := wb.codes[types.Hash(codeHash)]
	if !ok {
		t.Fatal("code not stored")
	}
	if !bytes.Equal(stored, code) {
		t.Error("stored code mismatch")
	}

	// Verify code hash was linked to address.
	ch, ok := wb.codeAddr[types.Address(addr)]
	if !ok || ch != types.Hash(codeHash) {
		t.Error("code hash not linked to address")
	}
}

func TestWitnessBuilderRecordAccountAccess(t *testing.T) {
	wb := NewWitnessBuilder(types.Hash{}, types.Hash{}, 1)

	addr := [20]byte{9}
	balance := big.NewInt(1000).Bytes()

	wb.RecordAccountAccess(addr, 5, balance)

	r := wb.reads[types.Address(addr)]
	if r == nil {
		t.Fatal("account not recorded")
	}
	if r.nonce != 5 {
		t.Errorf("nonce: got %d, want 5", r.nonce)
	}
	if !bytes.Equal(r.balance, balance) {
		t.Error("balance mismatch")
	}
	if !r.exists {
		t.Error("exists should be true")
	}

	// Second access should not overwrite.
	wb.RecordAccountAccess(addr, 99, big.NewInt(9999).Bytes())
	if wb.reads[types.Address(addr)].nonce != 5 {
		t.Error("second RecordAccountAccess overwrote nonce")
	}
}

func TestWitnessBuilderBuild(t *testing.T) {
	ph := types.HexToHash("0xdead")
	sr := types.HexToHash("0xbeef")
	wb := NewWitnessBuilder(ph, sr, 10)

	addr := [20]byte{1}
	key := [32]byte{2}
	oldVal := [32]byte{3}
	newVal := [32]byte{4}
	code := []byte{0x60, 0x00}
	codeHash := [32]byte{5}

	wb.RecordAccountAccess(addr, 1, big.NewInt(100).Bytes())
	wb.RecordWrite(addr, key, oldVal, newVal)
	wb.RecordCodeAccess(addr, codeHash, code)
	wb.RecordBalanceChange(addr, big.NewInt(100), big.NewInt(50))
	wb.RecordNonceChange(addr, 1, 2)

	bew := wb.Build()

	if bew.ParentHash != ph {
		t.Error("ParentHash mismatch")
	}
	if bew.StateRoot != sr {
		t.Error("StateRoot mismatch")
	}
	if bew.BlockNum != 10 {
		t.Error("BlockNum mismatch")
	}

	// Check pre-state.
	psa, ok := bew.PreState[types.Address(addr)]
	if !ok {
		t.Fatal("address not in PreState")
	}
	if psa.Nonce != 1 {
		t.Errorf("PreState nonce: got %d, want 1", psa.Nonce)
	}
	if !psa.Exists {
		t.Error("PreState exists should be true")
	}

	// Check codes.
	if _, ok := bew.Codes[types.Hash(codeHash)]; !ok {
		t.Error("code not in witness")
	}

	// Check diffs.
	if len(bew.StateDiffs) != 1 {
		t.Fatalf("StateDiffs count: got %d, want 1", len(bew.StateDiffs))
	}
	sd := bew.StateDiffs[0]
	if sd.Address != addr {
		t.Error("diff address mismatch")
	}
	if !sd.BalanceDiff.Changed {
		t.Error("balance diff should be changed")
	}
	if !sd.NonceDiff.Changed {
		t.Error("nonce diff should be changed")
	}
	if sd.NonceDiff.OldNonce != 1 || sd.NonceDiff.NewNonce != 2 {
		t.Error("nonce diff values wrong")
	}
	if len(sd.StorageChanges) != 1 {
		t.Fatalf("storage changes: got %d, want 1", len(sd.StorageChanges))
	}
	if sd.StorageChanges[0].Key != key {
		t.Error("storage change key mismatch")
	}
	if sd.StorageChanges[0].OldValue != oldVal {
		t.Error("storage change old value mismatch")
	}
	if sd.StorageChanges[0].NewValue != newVal {
		t.Error("storage change new value mismatch")
	}
}

func TestBlockExecutionWitnessEncodeDecode(t *testing.T) {
	bew := NewBlockExecutionWitness(
		types.HexToHash("0xaaaa"),
		types.HexToHash("0xbbbb"),
		42,
	)

	addr := types.HexToAddress("0x1234567890abcdef1234")
	bew.PreState[addr] = &PreStateAccount{
		Nonce:    10,
		Balance:  big.NewInt(5000).Bytes(),
		CodeHash: types.HexToHash("0xcccc"),
		Storage: map[types.Hash]types.Hash{
			types.HexToHash("0x01"): types.HexToHash("0x02"),
		},
		Exists: true,
	}
	bew.Codes[types.HexToHash("0xcccc")] = []byte{0x60, 0x00, 0x55}
	bew.StateDiffs = []StateDiff{
		{
			Address: [20]byte{0x12, 0x34},
			BalanceDiff: BalanceDiff{
				OldBalance: big.NewInt(5000).Bytes(),
				NewBalance: big.NewInt(4000).Bytes(),
				Changed:    true,
			},
			NonceDiff: NonceDiff{
				OldNonce: 10,
				NewNonce: 11,
				Changed:  true,
			},
			StorageChanges: []StorageChange{
				{
					Key:      [32]byte{1},
					OldValue: [32]byte{2},
					NewValue: [32]byte{3},
				},
			},
		},
	}

	encoded, err := bew.Encode()
	if err != nil {
		t.Fatalf("Encode failed: %v", err)
	}

	decoded := &BlockExecutionWitness{}
	err = decoded.Decode(encoded)
	if err != nil {
		t.Fatalf("Decode failed: %v", err)
	}

	if decoded.ParentHash != bew.ParentHash {
		t.Error("ParentHash mismatch after roundtrip")
	}
	if decoded.StateRoot != bew.StateRoot {
		t.Error("StateRoot mismatch after roundtrip")
	}
	if decoded.BlockNum != bew.BlockNum {
		t.Errorf("BlockNum: got %d, want %d", decoded.BlockNum, bew.BlockNum)
	}

	// Verify pre-state.
	psa, ok := decoded.PreState[addr]
	if !ok {
		t.Fatal("address not in decoded PreState")
	}
	if psa.Nonce != 10 {
		t.Errorf("decoded nonce: got %d, want 10", psa.Nonce)
	}
	if !psa.Exists {
		t.Error("decoded exists should be true")
	}

	// Verify codes.
	code, ok := decoded.Codes[types.HexToHash("0xcccc")]
	if !ok {
		t.Fatal("code not in decoded witness")
	}
	if !bytes.Equal(code, []byte{0x60, 0x00, 0x55}) {
		t.Error("decoded code mismatch")
	}

	// Verify diffs.
	if len(decoded.StateDiffs) != 1 {
		t.Fatalf("decoded diffs: got %d, want 1", len(decoded.StateDiffs))
	}
	sd := decoded.StateDiffs[0]
	if !sd.BalanceDiff.Changed {
		t.Error("decoded balance diff should be changed")
	}
	if sd.NonceDiff.OldNonce != 10 || sd.NonceDiff.NewNonce != 11 {
		t.Error("decoded nonce diff values wrong")
	}
	if len(sd.StorageChanges) != 1 {
		t.Fatalf("decoded storage changes: got %d, want 1", len(sd.StorageChanges))
	}
}

func TestEncodeNilWitness(t *testing.T) {
	var bew *BlockExecutionWitness
	_, err := bew.Encode()
	if err != ErrWitnessEmpty {
		t.Errorf("expected ErrWitnessEmpty, got %v", err)
	}
}

func TestDecodeInvalidData(t *testing.T) {
	tests := []struct {
		name string
		data []byte
		err  error
	}{
		{"too short", []byte{1, 2, 3}, ErrWitnessDecodeShort},
		{"bad magic", make([]byte, 100), ErrWitnessDecodeBadMagic},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			bew := &BlockExecutionWitness{}
			err := bew.Decode(tt.data)
			if err != tt.err {
				t.Errorf("expected %v, got %v", tt.err, err)
			}
		})
	}
}

func TestVerifyPreState(t *testing.T) {
	bew := NewBlockExecutionWitness(
		types.HexToHash("0x1111"),
		types.HexToHash("0x2222"),
		5,
	)

	// Empty pre-state should pass.
	err := bew.VerifyPreState([32]byte{})
	if err != nil {
		t.Errorf("empty pre-state should pass: %v", err)
	}

	// Add a pre-state entry.
	addr := types.HexToAddress("0xabcd")
	bew.PreState[addr] = &PreStateAccount{
		Nonce:    1,
		Balance:  big.NewInt(100).Bytes(),
		CodeHash: types.EmptyCodeHash,
		Storage:  map[types.Hash]types.Hash{},
		Exists:   true,
	}

	// Verification with any state root should not error (the binding
	// is self-consistent).
	err = bew.VerifyPreState([32]byte{1, 2, 3})
	if err != nil {
		t.Errorf("pre-state verification should pass: %v", err)
	}
}

func TestVerifyPreStateNilWitness(t *testing.T) {
	var bew *BlockExecutionWitness
	err := bew.VerifyPreState([32]byte{})
	if err != ErrWitnessEmpty {
		t.Errorf("expected ErrWitnessEmpty, got %v", err)
	}
}

func TestWitnessBuilderConcurrentAccess(t *testing.T) {
	wb := NewWitnessBuilder(types.Hash{}, types.Hash{}, 1)

	done := make(chan struct{})
	for i := 0; i < 10; i++ {
		go func(idx int) {
			defer func() { done <- struct{}{} }()
			addr := [20]byte{byte(idx)}
			key := [32]byte{byte(idx)}
			val := [32]byte{byte(idx + 100)}

			wb.RecordRead(addr, key, val)
			wb.RecordAccountAccess(addr, uint64(idx), big.NewInt(int64(idx*100)).Bytes())
			wb.RecordWrite(addr, key, val, [32]byte{byte(idx + 200)})
		}(i)
	}
	for i := 0; i < 10; i++ {
		<-done
	}

	bew := wb.Build()
	if len(bew.PreState) != 10 {
		t.Errorf("pre-state accounts: got %d, want 10", len(bew.PreState))
	}
	if len(bew.StateDiffs) != 10 {
		t.Errorf("state diffs: got %d, want 10", len(bew.StateDiffs))
	}
}

func TestEncodeDecodeEmptyDiffs(t *testing.T) {
	bew := NewBlockExecutionWitness(
		types.HexToHash("0xf00d"),
		types.HexToHash("0xbabe"),
		99,
	)
	// No pre-state, codes, or diffs -- just header.

	encoded, err := bew.Encode()
	if err != nil {
		t.Fatalf("Encode empty witness failed: %v", err)
	}

	decoded := &BlockExecutionWitness{}
	err = decoded.Decode(encoded)
	if err != nil {
		t.Fatalf("Decode empty witness failed: %v", err)
	}

	if decoded.BlockNum != 99 {
		t.Errorf("BlockNum: got %d, want 99", decoded.BlockNum)
	}
	if len(decoded.PreState) != 0 {
		t.Errorf("PreState should be empty, got %d", len(decoded.PreState))
	}
	if len(decoded.StateDiffs) != 0 {
		t.Errorf("StateDiffs should be empty, got %d", len(decoded.StateDiffs))
	}
}

func TestStateDiffNoChanges(t *testing.T) {
	wb := NewWitnessBuilder(types.Hash{}, types.Hash{}, 1)

	addr := [20]byte{1}
	wb.RecordAccountAccess(addr, 0, big.NewInt(0).Bytes())
	// Only read, no write.
	wb.RecordRead(addr, [32]byte{2}, [32]byte{3})

	bew := wb.Build()
	// No writes -> no diffs.
	if len(bew.StateDiffs) != 0 {
		t.Errorf("expected 0 state diffs for read-only, got %d", len(bew.StateDiffs))
	}
	// But pre-state should be present.
	if len(bew.PreState) != 1 {
		t.Errorf("expected 1 pre-state account, got %d", len(bew.PreState))
	}
}
