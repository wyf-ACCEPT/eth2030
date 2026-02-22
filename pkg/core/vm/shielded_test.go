package vm

import (
	"sync"
	"testing"

	"github.com/eth2030/eth2030/core/types"
	"github.com/eth2030/eth2030/crypto"
)

func makeShieldedTx(sender types.Address, gas uint64, nonce uint64) *ShieldedTx {
	return &ShieldedTx{
		Sender:         sender,
		EncryptedInput: []byte{0x01, 0x02, 0x03, 0x04},
		GasLimit:       gas,
		Nonce:          nonce,
		ShieldingKey:   []byte{0xAA, 0xBB, 0xCC},
	}
}

func TestNewShieldedVM_Defaults(t *testing.T) {
	svm := NewShieldedVM(ShieldedConfig{})
	if svm.config.MaxShieldedGas != 10_000_000 {
		t.Errorf("max gas = %d, want 10000000", svm.config.MaxShieldedGas)
	}
	if svm.config.NullifierSetSize != 1_000_000 {
		t.Errorf("nullifier set = %d, want 1000000", svm.config.NullifierSetSize)
	}
}

func TestNewShieldedVM_Custom(t *testing.T) {
	svm := NewShieldedVM(ShieldedConfig{
		MaxShieldedGas:   5_000_000,
		NullifierSetSize: 500,
		EnablePrivacy:    true,
	})
	if svm.config.MaxShieldedGas != 5_000_000 {
		t.Errorf("max gas = %d, want 5000000", svm.config.MaxShieldedGas)
	}
	if svm.config.NullifierSetSize != 500 {
		t.Errorf("nullifier set = %d, want 500", svm.config.NullifierSetSize)
	}
}

func TestDefaultShieldedConfig(t *testing.T) {
	cfg := DefaultShieldedConfig()
	if cfg.MaxShieldedGas == 0 {
		t.Error("max gas should not be zero")
	}
	if !cfg.EnablePrivacy {
		t.Error("privacy should be enabled by default")
	}
}

func TestExecuteShielded_Disabled(t *testing.T) {
	svm := NewShieldedVM(ShieldedConfig{EnablePrivacy: false})
	tx := makeShieldedTx(types.HexToAddress("0x01"), 100_000, 0)
	_, err := svm.ExecuteShielded(tx)
	if err != ErrShieldedDisabled {
		t.Errorf("expected ErrShieldedDisabled, got %v", err)
	}
}

func TestExecuteShielded_NilTx(t *testing.T) {
	svm := NewShieldedVM(DefaultShieldedConfig())
	_, err := svm.ExecuteShielded(nil)
	if err != ErrNilShieldedTx {
		t.Errorf("expected ErrNilShieldedTx, got %v", err)
	}
}

func TestExecuteShielded_ZeroGas(t *testing.T) {
	svm := NewShieldedVM(DefaultShieldedConfig())
	tx := makeShieldedTx(types.HexToAddress("0x01"), 0, 0)
	_, err := svm.ExecuteShielded(tx)
	if err != ErrShieldedGasZero {
		t.Errorf("expected ErrShieldedGasZero, got %v", err)
	}
}

func TestExecuteShielded_GasExceeded(t *testing.T) {
	svm := NewShieldedVM(ShieldedConfig{
		MaxShieldedGas: 1000,
		EnablePrivacy:  true,
	})
	tx := makeShieldedTx(types.HexToAddress("0x01"), 2000, 0)
	_, err := svm.ExecuteShielded(tx)
	if err != ErrShieldedGasExceeded {
		t.Errorf("expected ErrShieldedGasExceeded, got %v", err)
	}
}

func TestExecuteShielded_EmptyInput(t *testing.T) {
	svm := NewShieldedVM(DefaultShieldedConfig())
	tx := &ShieldedTx{
		Sender:         types.HexToAddress("0x01"),
		EncryptedInput: nil,
		GasLimit:       100_000,
		ShieldingKey:   []byte{0x01},
	}
	_, err := svm.ExecuteShielded(tx)
	if err != ErrEmptyEncryptedInput {
		t.Errorf("expected ErrEmptyEncryptedInput, got %v", err)
	}
}

func TestExecuteShielded_EmptyKey(t *testing.T) {
	svm := NewShieldedVM(DefaultShieldedConfig())
	tx := &ShieldedTx{
		Sender:         types.HexToAddress("0x01"),
		EncryptedInput: []byte{0x01},
		GasLimit:       100_000,
		ShieldingKey:   nil,
	}
	_, err := svm.ExecuteShielded(tx)
	if err != ErrEmptyShieldingKey {
		t.Errorf("expected ErrEmptyShieldingKey, got %v", err)
	}
}

func TestExecuteShielded_Success(t *testing.T) {
	svm := NewShieldedVM(DefaultShieldedConfig())
	tx := makeShieldedTx(types.HexToAddress("0x01"), 100_000, 0)

	result, err := svm.ExecuteShielded(tx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.Success {
		t.Error("execution should succeed")
	}
	if result.OutputCommitment.IsZero() {
		t.Error("output commitment should not be zero")
	}
	if result.NullifierHash.IsZero() {
		t.Error("nullifier should not be zero")
	}
	if result.GasUsed == 0 {
		t.Error("gas used should not be zero")
	}
	if len(result.EncryptedOutput) == 0 {
		t.Error("encrypted output should not be empty")
	}
}

func TestExecuteShielded_GasAccounting(t *testing.T) {
	svm := NewShieldedVM(DefaultShieldedConfig())
	input := make([]byte, 100)
	for i := range input {
		input[i] = byte(i)
	}
	tx := &ShieldedTx{
		Sender:         types.HexToAddress("0x01"),
		EncryptedInput: input,
		GasLimit:       100_000,
		Nonce:          0,
		ShieldingKey:   []byte{0x01},
	}

	result, err := svm.ExecuteShielded(tx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Expected: 21000 base + 100 * 16 = 22600.
	expectedGas := uint64(21_000 + 100*16)
	if result.GasUsed != expectedGas {
		t.Errorf("gas used = %d, want %d", result.GasUsed, expectedGas)
	}
}

func TestExecuteShielded_GasCapped(t *testing.T) {
	svm := NewShieldedVM(ShieldedConfig{
		MaxShieldedGas: 100_000,
		EnablePrivacy:  true,
	})
	// Large input that would exceed gas limit.
	input := make([]byte, 5000)
	tx := &ShieldedTx{
		Sender:         types.HexToAddress("0x01"),
		EncryptedInput: input,
		GasLimit:       50_000, // 21000 + 5000*16 = 101000, capped at 50000
		Nonce:          0,
		ShieldingKey:   []byte{0x01},
	}

	result, err := svm.ExecuteShielded(tx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.GasUsed != 50_000 {
		t.Errorf("gas used = %d, want 50000 (capped)", result.GasUsed)
	}
}

func TestExecuteShielded_DoubleSpend(t *testing.T) {
	svm := NewShieldedVM(DefaultShieldedConfig())
	tx := makeShieldedTx(types.HexToAddress("0x01"), 100_000, 0)

	_, err := svm.ExecuteShielded(tx)
	if err != nil {
		t.Fatalf("first execute: %v", err)
	}

	// Same tx again (same input + nonce = same nullifier).
	_, err = svm.ExecuteShielded(tx)
	if err != ErrNullifierUsed {
		t.Errorf("expected ErrNullifierUsed, got %v", err)
	}
}

func TestExecuteShielded_DifferentNonce(t *testing.T) {
	svm := NewShieldedVM(DefaultShieldedConfig())
	tx1 := makeShieldedTx(types.HexToAddress("0x01"), 100_000, 0)
	tx2 := makeShieldedTx(types.HexToAddress("0x01"), 100_000, 1)

	r1, err := svm.ExecuteShielded(tx1)
	if err != nil {
		t.Fatalf("tx1: %v", err)
	}
	r2, err := svm.ExecuteShielded(tx2)
	if err != nil {
		t.Fatalf("tx2: %v", err)
	}

	if r1.NullifierHash == r2.NullifierHash {
		t.Error("different nonces should produce different nullifiers")
	}
	if r1.OutputCommitment == r2.OutputCommitment {
		t.Error("different nonces should produce different output commitments")
	}
}

func TestExecuteShielded_NullifierSetFull(t *testing.T) {
	svm := NewShieldedVM(ShieldedConfig{
		MaxShieldedGas:   10_000_000,
		NullifierSetSize: 2,
		EnablePrivacy:    true,
	})

	for i := uint64(0); i < 2; i++ {
		tx := makeShieldedTx(types.HexToAddress("0x01"), 100_000, i)
		if _, err := svm.ExecuteShielded(tx); err != nil {
			t.Fatalf("execute %d: %v", i, err)
		}
	}

	tx := makeShieldedTx(types.HexToAddress("0x01"), 100_000, 99)
	_, err := svm.ExecuteShielded(tx)
	if err != ErrNullifierSetFull {
		t.Errorf("expected ErrNullifierSetFull, got %v", err)
	}
}

func TestGenerateNullifier_Deterministic(t *testing.T) {
	input := []byte{0x01, 0x02, 0x03}
	n1 := GenerateNullifier(input, 42)
	n2 := GenerateNullifier(input, 42)
	if n1 != n2 {
		t.Error("nullifier should be deterministic")
	}

	n3 := GenerateNullifier(input, 43)
	if n1 == n3 {
		t.Error("different nonce should produce different nullifier")
	}

	n4 := GenerateNullifier([]byte{0x04}, 42)
	if n1 == n4 {
		t.Error("different input should produce different nullifier")
	}
}

func TestVerifyNullifier(t *testing.T) {
	svm := NewShieldedVM(DefaultShieldedConfig())

	nullifier := GenerateNullifier([]byte{0x01}, 0)

	// Should be fresh (unspent).
	if !svm.VerifyNullifier(nullifier) {
		t.Error("fresh nullifier should pass verification")
	}

	// Execute a tx that uses this nullifier.
	tx := &ShieldedTx{
		Sender:         types.HexToAddress("0x01"),
		EncryptedInput: []byte{0x01},
		GasLimit:       100_000,
		Nonce:          0,
		ShieldingKey:   []byte{0x01},
	}
	_, err := svm.ExecuteShielded(tx)
	if err != nil {
		t.Fatalf("execute: %v", err)
	}

	// Now it should be spent.
	if svm.VerifyNullifier(nullifier) {
		t.Error("spent nullifier should fail verification")
	}
}

func TestCreateShieldedNote_Disabled(t *testing.T) {
	svm := NewShieldedVM(ShieldedConfig{EnablePrivacy: false})
	_, err := svm.CreateShieldedNote(100, types.HexToAddress("0x01"))
	if err != ErrShieldedDisabled {
		t.Errorf("expected ErrShieldedDisabled, got %v", err)
	}
}

func TestCreateShieldedNote_ZeroValue(t *testing.T) {
	svm := NewShieldedVM(DefaultShieldedConfig())
	_, err := svm.CreateShieldedNote(0, types.HexToAddress("0x01"))
	if err != ErrZeroValue {
		t.Errorf("expected ErrZeroValue, got %v", err)
	}
}

func TestCreateShieldedNote_ZeroRecipient(t *testing.T) {
	svm := NewShieldedVM(DefaultShieldedConfig())
	_, err := svm.CreateShieldedNote(100, types.Address{})
	if err != ErrZeroRecipient {
		t.Errorf("expected ErrZeroRecipient, got %v", err)
	}
}

func TestCreateShieldedNote_Success(t *testing.T) {
	svm := NewShieldedVM(DefaultShieldedConfig())
	recipient := types.HexToAddress("0xDEADBEEF")

	note, err := svm.CreateShieldedNote(1000, recipient)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if note.Commitment.IsZero() {
		t.Error("commitment should not be zero")
	}
	if note.Value != 1000 {
		t.Errorf("value = %d, want 1000", note.Value)
	}
	if note.Recipient != recipient {
		t.Error("recipient mismatch")
	}
	if len(note.Randomness) == 0 {
		t.Error("randomness should not be empty")
	}
	if svm.NoteCount() != 1 {
		t.Errorf("note count = %d, want 1", svm.NoteCount())
	}
}

func TestCreateShieldedNote_Deterministic(t *testing.T) {
	svm := NewShieldedVM(DefaultShieldedConfig())
	recipient := types.HexToAddress("0xAB")

	note1, _ := svm.CreateShieldedNote(100, recipient)
	// Creating a second note with same params overwrites the map entry
	// (same commitment), so note count stays at 1.
	note2, _ := svm.CreateShieldedNote(100, recipient)

	if note1.Commitment != note2.Commitment {
		t.Error("same params should produce same commitment")
	}
}

func TestCreateShieldedNote_DifferentValues(t *testing.T) {
	svm := NewShieldedVM(DefaultShieldedConfig())
	recipient := types.HexToAddress("0xAB")

	note1, _ := svm.CreateShieldedNote(100, recipient)
	note2, _ := svm.CreateShieldedNote(200, recipient)

	if note1.Commitment == note2.Commitment {
		t.Error("different values should produce different commitments")
	}
}

func TestSpendNote_Disabled(t *testing.T) {
	svm := NewShieldedVM(ShieldedConfig{EnablePrivacy: false})
	note := &ShieldedNote{Commitment: types.HexToHash("0x01")}
	_, err := svm.SpendNote(note, []byte{0x01})
	if err != ErrShieldedDisabled {
		t.Errorf("expected ErrShieldedDisabled, got %v", err)
	}
}

func TestSpendNote_NilNote(t *testing.T) {
	svm := NewShieldedVM(DefaultShieldedConfig())
	_, err := svm.SpendNote(nil, []byte{0x01})
	if err != ErrNilNote {
		t.Errorf("expected ErrNilNote, got %v", err)
	}
}

func TestSpendNote_EmptyProof(t *testing.T) {
	svm := NewShieldedVM(DefaultShieldedConfig())
	note := &ShieldedNote{Commitment: types.HexToHash("0x01")}
	_, err := svm.SpendNote(note, nil)
	if err != ErrEmptyProof {
		t.Errorf("expected ErrEmptyProof, got %v", err)
	}
}

func TestSpendNote_InvalidProof(t *testing.T) {
	svm := NewShieldedVM(DefaultShieldedConfig())
	note, err := svm.CreateShieldedNote(100, types.HexToAddress("0x01"))
	if err != nil {
		t.Fatal(err)
	}
	// Bad proof: too short.
	_, err = svm.SpendNote(note, []byte{0x01})
	if err != ErrInvalidProof {
		t.Errorf("expected ErrInvalidProof, got %v", err)
	}
	// Bad proof: wrong bytes.
	badProof := make([]byte, 32)
	_, err = svm.SpendNote(note, badProof)
	if err != ErrInvalidProof {
		t.Errorf("expected ErrInvalidProof for wrong proof, got %v", err)
	}
}

func TestSpendNote_Success(t *testing.T) {
	svm := NewShieldedVM(DefaultShieldedConfig())
	recipient := types.HexToAddress("0x01")

	note, err := svm.CreateShieldedNote(500, recipient)
	if err != nil {
		t.Fatal(err)
	}
	if svm.NoteCount() != 1 {
		t.Fatalf("note count = %d, want 1", svm.NoteCount())
	}

	proof := MakeSpendProof(note)
	result, err := svm.SpendNote(note, proof)
	if err != nil {
		t.Fatalf("spend failed: %v", err)
	}
	if !result.Success {
		t.Error("spend should succeed")
	}
	if result.OutputCommitment.IsZero() {
		t.Error("output commitment should not be zero")
	}
	if result.NullifierHash.IsZero() {
		t.Error("nullifier should not be zero")
	}
	if result.GasUsed == 0 {
		t.Error("gas used should not be zero")
	}

	// Note should be removed.
	if svm.NoteCount() != 0 {
		t.Errorf("note count = %d, want 0 after spend", svm.NoteCount())
	}
	// Nullifier should be recorded.
	if svm.NullifierCount() != 1 {
		t.Errorf("nullifier count = %d, want 1", svm.NullifierCount())
	}
}

func TestSpendNote_DoubleSpend(t *testing.T) {
	svm := NewShieldedVM(DefaultShieldedConfig())
	recipient := types.HexToAddress("0x01")

	note, _ := svm.CreateShieldedNote(500, recipient)
	proof := MakeSpendProof(note)

	_, err := svm.SpendNote(note, proof)
	if err != nil {
		t.Fatalf("first spend: %v", err)
	}

	// Trying to spend the same note again.
	_, err = svm.SpendNote(note, proof)
	if err != ErrNullifierUsed {
		t.Errorf("expected ErrNullifierUsed, got %v", err)
	}
}

func TestSpendNote_NullifierSetFull(t *testing.T) {
	svm := NewShieldedVM(ShieldedConfig{
		MaxShieldedGas:   10_000_000,
		NullifierSetSize: 1,
		EnablePrivacy:    true,
	})

	note1, _ := svm.CreateShieldedNote(100, types.HexToAddress("0x01"))
	proof1 := MakeSpendProof(note1)
	_, err := svm.SpendNote(note1, proof1)
	if err != nil {
		t.Fatalf("first spend: %v", err)
	}

	note2, _ := svm.CreateShieldedNote(200, types.HexToAddress("0x02"))
	proof2 := MakeSpendProof(note2)
	_, err = svm.SpendNote(note2, proof2)
	if err != ErrNullifierSetFull {
		t.Errorf("expected ErrNullifierSetFull, got %v", err)
	}
}

func TestVerifyShieldedProof_Nil(t *testing.T) {
	svm := NewShieldedVM(DefaultShieldedConfig())
	if svm.VerifyShieldedProof(nil) {
		t.Error("nil result should fail verification")
	}
}

func TestVerifyShieldedProof_Failed(t *testing.T) {
	svm := NewShieldedVM(DefaultShieldedConfig())
	result := &ShieldedResult{Success: false}
	if svm.VerifyShieldedProof(result) {
		t.Error("failed result should fail verification")
	}
}

func TestVerifyShieldedProof_ZeroCommitment(t *testing.T) {
	svm := NewShieldedVM(DefaultShieldedConfig())
	result := &ShieldedResult{
		Success:       true,
		NullifierHash: types.HexToHash("0x01"),
	}
	if svm.VerifyShieldedProof(result) {
		t.Error("zero commitment should fail verification")
	}
}

func TestVerifyShieldedProof_ZeroNullifier(t *testing.T) {
	svm := NewShieldedVM(DefaultShieldedConfig())
	result := &ShieldedResult{
		Success:          true,
		OutputCommitment: types.HexToHash("0x01"),
	}
	if svm.VerifyShieldedProof(result) {
		t.Error("zero nullifier should fail verification")
	}
}

func TestVerifyShieldedProof_ValidExecution(t *testing.T) {
	svm := NewShieldedVM(DefaultShieldedConfig())
	tx := makeShieldedTx(types.HexToAddress("0x01"), 100_000, 0)

	result, err := svm.ExecuteShielded(tx)
	if err != nil {
		t.Fatal(err)
	}
	if !svm.VerifyShieldedProof(result) {
		t.Error("valid execution result should pass verification")
	}
}

func TestVerifyShieldedProof_ValidSpend(t *testing.T) {
	svm := NewShieldedVM(DefaultShieldedConfig())
	note, _ := svm.CreateShieldedNote(100, types.HexToAddress("0x01"))
	proof := MakeSpendProof(note)

	result, err := svm.SpendNote(note, proof)
	if err != nil {
		t.Fatal(err)
	}
	if !svm.VerifyShieldedProof(result) {
		t.Error("valid spend result should pass verification")
	}
}

func TestNullifierCount(t *testing.T) {
	svm := NewShieldedVM(DefaultShieldedConfig())
	if svm.NullifierCount() != 0 {
		t.Errorf("initial nullifier count = %d, want 0", svm.NullifierCount())
	}

	tx := makeShieldedTx(types.HexToAddress("0x01"), 100_000, 0)
	svm.ExecuteShielded(tx)

	if svm.NullifierCount() != 1 {
		t.Errorf("after 1 tx, nullifier count = %d, want 1", svm.NullifierCount())
	}
}

func TestNoteCount(t *testing.T) {
	svm := NewShieldedVM(DefaultShieldedConfig())
	if svm.NoteCount() != 0 {
		t.Errorf("initial note count = %d, want 0", svm.NoteCount())
	}

	svm.CreateShieldedNote(100, types.HexToAddress("0x01"))
	svm.CreateShieldedNote(200, types.HexToAddress("0x02"))

	if svm.NoteCount() != 2 {
		t.Errorf("note count = %d, want 2", svm.NoteCount())
	}
}

func TestMakeSpendProof(t *testing.T) {
	note := &ShieldedNote{
		Commitment: crypto.Keccak256Hash([]byte("test")),
	}
	proof := MakeSpendProof(note)
	if len(proof) != 32 {
		t.Errorf("proof length = %d, want 32", len(proof))
	}

	// Verify it matches what verifySpendProof expects.
	expected := crypto.Keccak256(note.Commitment[:])
	for i := 0; i < 32; i++ {
		if proof[i] != expected[i] {
			t.Errorf("proof[%d] = %x, want %x", i, proof[i], expected[i])
			break
		}
	}
}

func TestShieldedVM_Concurrent(t *testing.T) {
	svm := NewShieldedVM(DefaultShieldedConfig())

	var wg sync.WaitGroup
	for i := uint64(0); i < 50; i++ {
		wg.Add(1)
		go func(nonce uint64) {
			defer wg.Done()
			tx := makeShieldedTx(types.HexToAddress("0x01"), 100_000, nonce)
			_, _ = svm.ExecuteShielded(tx)
		}(i)
	}
	wg.Wait()

	if svm.NullifierCount() != 50 {
		t.Errorf("nullifier count = %d, want 50", svm.NullifierCount())
	}
}

func TestShieldedVM_ConcurrentReadWrite(t *testing.T) {
	svm := NewShieldedVM(DefaultShieldedConfig())

	var wg sync.WaitGroup

	// Writers: execute shielded txs.
	for i := uint64(0); i < 20; i++ {
		wg.Add(1)
		go func(nonce uint64) {
			defer wg.Done()
			tx := makeShieldedTx(types.HexToAddress("0x01"), 100_000, nonce)
			_, _ = svm.ExecuteShielded(tx)
		}(i)
	}

	// Readers: check nullifiers and counts.
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_ = svm.NullifierCount()
			_ = svm.NoteCount()
			_ = svm.VerifyNullifier(types.Hash{})
		}()
	}

	// Note creators.
	for i := uint64(0); i < 10; i++ {
		wg.Add(1)
		go func(val uint64) {
			defer wg.Done()
			addr := types.Address{}
			addr[0] = byte(val + 1)
			_, _ = svm.CreateShieldedNote(val+1, addr)
		}(i)
	}

	wg.Wait()
}

func TestShieldedVM_FullWorkflow(t *testing.T) {
	svm := NewShieldedVM(DefaultShieldedConfig())

	// Step 1: Create a shielded note.
	recipient := types.HexToAddress("0xCAFE")
	note, err := svm.CreateShieldedNote(1000, recipient)
	if err != nil {
		t.Fatalf("create note: %v", err)
	}
	if svm.NoteCount() != 1 {
		t.Fatalf("note count = %d, want 1", svm.NoteCount())
	}

	// Step 2: Generate a valid proof.
	proof := MakeSpendProof(note)

	// Step 3: Spend the note.
	result, err := svm.SpendNote(note, proof)
	if err != nil {
		t.Fatalf("spend note: %v", err)
	}
	if !result.Success {
		t.Error("spend should succeed")
	}

	// Step 4: Verify the result.
	if !svm.VerifyShieldedProof(result) {
		t.Error("result should pass proof verification")
	}

	// Step 5: Confirm note is spent.
	if svm.NoteCount() != 0 {
		t.Errorf("note count = %d, want 0", svm.NoteCount())
	}
	if svm.VerifyNullifier(result.NullifierHash) {
		t.Error("nullifier should be marked as spent")
	}

	// Step 6: Double-spend should fail.
	_, err = svm.SpendNote(note, proof)
	if err != ErrNullifierUsed {
		t.Errorf("double-spend: expected ErrNullifierUsed, got %v", err)
	}
}
