package proofs

import (
	"testing"

	"github.com/eth2030/eth2030/core/types"
)

func TestNewStateTransitionCircuit(t *testing.T) {
	circuit := NewStateTransitionCircuit(4)
	def := circuit.Definition()

	if def == nil {
		t.Fatal("definition should not be nil")
	}
	if def.Name != "state_transition" {
		t.Errorf("name = %q, want %q", def.Name, "state_transition")
	}
	if def.PublicInputCount != 2 {
		t.Errorf("public input count = %d, want 2", def.PublicInputCount)
	}
	if def.PrivateWitnessCount != 12 {
		t.Errorf("private witness count = %d, want 12 (4 txs * 3)", def.PrivateWitnessCount)
	}
	if def.TotalVariables() != 14 {
		t.Errorf("total variables = %d, want 14", def.TotalVariables())
	}
	if def.ConstraintCount() == 0 {
		t.Error("should have constraints")
	}
}

func TestNewStateTransitionCircuitZeroSlots(t *testing.T) {
	circuit := NewStateTransitionCircuit(0)
	def := circuit.Definition()
	// Should default to 1 tx slot.
	if def.PrivateWitnessCount != 3 {
		t.Errorf("private witness count = %d, want 3 (1 tx * 3)", def.PrivateWitnessCount)
	}
}

func TestStateTransitionGenerateWitness(t *testing.T) {
	circuit := NewStateTransitionCircuit(2)

	preRoot := types.HexToHash("0xaaaa")
	postRoot := types.HexToHash("0xbbbb")

	txData := []TransactionWitnessData{
		{GasUsed: 21000, NonceIncrement: 1, BalanceChange: -100},
		{GasUsed: 50000, NonceIncrement: 1, BalanceChange: -500},
	}

	witness, err := circuit.GenerateWitness(preRoot, postRoot, txData)
	if err != nil {
		t.Fatalf("GenerateWitness failed: %v", err)
	}

	if len(witness.PublicInputs) != 2 {
		t.Errorf("public inputs = %d, want 2", len(witness.PublicInputs))
	}
	if len(witness.PrivateValues) != 6 {
		t.Errorf("private values = %d, want 6", len(witness.PrivateValues))
	}

	// Check gas values.
	if witness.PrivateValues[0] != 21000 {
		t.Errorf("gas[0] = %d, want 21000", witness.PrivateValues[0])
	}
	if witness.PrivateValues[3] != 50000 {
		t.Errorf("gas[1] = %d, want 50000", witness.PrivateValues[3])
	}
}

func TestStateTransitionGenerateWitnessNilDef(t *testing.T) {
	circuit := &StateTransitionCircuit{}
	_, err := circuit.GenerateWitness(types.Hash{}, types.Hash{}, nil)
	if err != ErrCircuitNilDef {
		t.Errorf("expected ErrCircuitNilDef, got %v", err)
	}
}

func TestStateTransitionVerifyWitness(t *testing.T) {
	circuit := NewStateTransitionCircuit(2)

	preRoot := types.HexToHash("0xaaaa")
	postRoot := types.HexToHash("0xbbbb")

	txData := []TransactionWitnessData{
		{GasUsed: 21000, NonceIncrement: 1, BalanceChange: -100},
		{GasUsed: 50000, NonceIncrement: 1, BalanceChange: -500},
	}

	witness, err := circuit.GenerateWitness(preRoot, postRoot, txData)
	if err != nil {
		t.Fatal(err)
	}

	if err := circuit.VerifyWitness(witness); err != nil {
		t.Fatalf("VerifyWitness failed: %v", err)
	}
}

func TestStateTransitionVerifyWitnessNil(t *testing.T) {
	circuit := NewStateTransitionCircuit(1)

	if err := circuit.VerifyWitness(nil); err != ErrCircuitWitnessMissing {
		t.Errorf("expected ErrCircuitWitnessMissing, got %v", err)
	}
}

func TestStateTransitionVerifyWitnessBadInputs(t *testing.T) {
	circuit := NewStateTransitionCircuit(1)

	witness := &CircuitWitness{
		PublicInputs:  []int64{1}, // only 1, need 2
		PrivateValues: []int64{0, 0, 0},
	}
	if err := circuit.VerifyWitness(witness); err != ErrCircuitInputsMismatch {
		t.Errorf("expected ErrCircuitInputsMismatch, got %v", err)
	}
}

func TestStateTransitionVerifyWitnessBadPrivate(t *testing.T) {
	circuit := NewStateTransitionCircuit(1)

	witness := &CircuitWitness{
		PublicInputs:  []int64{1, 2},
		PrivateValues: []int64{0}, // only 1, need 3
	}
	if err := circuit.VerifyWitness(witness); err != ErrCircuitWitnessMissing {
		t.Errorf("expected ErrCircuitWitnessMissing, got %v", err)
	}
}

func TestStateTransitionProve(t *testing.T) {
	circuit := NewStateTransitionCircuit(2)

	preRoot := types.HexToHash("0xaaaa")
	postRoot := types.HexToHash("0xbbbb")
	blockHash := types.HexToHash("0xcccc")

	txData := []TransactionWitnessData{
		{GasUsed: 21000, NonceIncrement: 1, BalanceChange: -100},
		{GasUsed: 50000, NonceIncrement: 1, BalanceChange: -500},
	}

	witness, err := circuit.GenerateWitness(preRoot, postRoot, txData)
	if err != nil {
		t.Fatal(err)
	}

	proof, err := circuit.Prove(witness, blockHash)
	if err != nil {
		t.Fatalf("Prove failed: %v", err)
	}

	if proof.CircuitName != "state_transition" {
		t.Errorf("circuit name = %q, want %q", proof.CircuitName, "state_transition")
	}
	if len(proof.ProofData) == 0 {
		t.Error("proof data should not be empty")
	}
	if proof.Commitment.IsZero() {
		t.Error("commitment should not be zero")
	}
	if proof.BlockHash != blockHash {
		t.Error("block hash mismatch")
	}
	if len(proof.PublicInputs) != 2 {
		t.Errorf("public inputs = %d, want 2", len(proof.PublicInputs))
	}
}

func TestStateTransitionVerifyProof(t *testing.T) {
	circuit := NewStateTransitionCircuit(2)

	preRoot := types.HexToHash("0xaaaa")
	postRoot := types.HexToHash("0xbbbb")
	blockHash := types.HexToHash("0xcccc")

	txData := []TransactionWitnessData{
		{GasUsed: 21000, NonceIncrement: 1, BalanceChange: -100},
		{GasUsed: 50000, NonceIncrement: 1, BalanceChange: -500},
	}

	witness, _ := circuit.GenerateWitness(preRoot, postRoot, txData)
	proof, _ := circuit.Prove(witness, blockHash)

	valid, err := circuit.VerifyProof(proof)
	if err != nil {
		t.Fatalf("VerifyProof failed: %v", err)
	}
	if !valid {
		t.Error("proof should be valid")
	}
}

func TestStateTransitionVerifyProofNil(t *testing.T) {
	circuit := NewStateTransitionCircuit(1)
	_, err := circuit.VerifyProof(nil)
	if err != ErrCircuitProofInvalid {
		t.Errorf("expected ErrCircuitProofInvalid, got %v", err)
	}
}

func TestStateTransitionVerifyProofWrongCircuit(t *testing.T) {
	circuit := NewStateTransitionCircuit(1)
	proof := &SNARKProof{
		CircuitName: "wrong_circuit",
		ProofData:   []byte{0x01},
		Commitment:  types.HexToHash("0xabcd"),
	}
	_, err := circuit.VerifyProof(proof)
	if err != ErrCircuitProofInvalid {
		t.Errorf("expected ErrCircuitProofInvalid, got %v", err)
	}
}

func TestStateTransitionVerifyProofTampered(t *testing.T) {
	circuit := NewStateTransitionCircuit(1)

	preRoot := types.HexToHash("0xaaaa")
	postRoot := types.HexToHash("0xbbbb")

	txData := []TransactionWitnessData{
		{GasUsed: 21000, NonceIncrement: 1, BalanceChange: -100},
	}

	witness, _ := circuit.GenerateWitness(preRoot, postRoot, txData)
	proof, _ := circuit.Prove(witness, types.Hash{})

	// Tamper with commitment.
	proof.Commitment = types.HexToHash("0xdeadbeef")

	_, err := circuit.VerifyProof(proof)
	if err != ErrCircuitProofInvalid {
		t.Errorf("expected ErrCircuitProofInvalid, got %v", err)
	}
}

func TestSerializeDeserializeProof(t *testing.T) {
	circuit := NewStateTransitionCircuit(2)

	preRoot := types.HexToHash("0xaaaa")
	postRoot := types.HexToHash("0xbbbb")
	blockHash := types.HexToHash("0xcccc")

	txData := []TransactionWitnessData{
		{GasUsed: 21000, NonceIncrement: 1, BalanceChange: -100},
		{GasUsed: 50000, NonceIncrement: 1, BalanceChange: -500},
	}

	witness, _ := circuit.GenerateWitness(preRoot, postRoot, txData)
	proof, _ := circuit.Prove(witness, blockHash)

	// Serialize.
	data, err := SerializeProof(proof)
	if err != nil {
		t.Fatalf("SerializeProof failed: %v", err)
	}
	if len(data) == 0 {
		t.Fatal("serialized data should not be empty")
	}

	// Deserialize.
	restored, err := DeserializeProof(data)
	if err != nil {
		t.Fatalf("DeserializeProof failed: %v", err)
	}

	// Verify roundtrip.
	if restored.CircuitName != proof.CircuitName {
		t.Errorf("circuit name = %q, want %q", restored.CircuitName, proof.CircuitName)
	}
	if len(restored.PublicInputs) != len(proof.PublicInputs) {
		t.Fatalf("public inputs len = %d, want %d", len(restored.PublicInputs), len(proof.PublicInputs))
	}
	for i := range proof.PublicInputs {
		if restored.PublicInputs[i] != proof.PublicInputs[i] {
			t.Errorf("public input[%d] = %d, want %d", i, restored.PublicInputs[i], proof.PublicInputs[i])
		}
	}
	if restored.Commitment != proof.Commitment {
		t.Error("commitment mismatch")
	}
	if restored.BlockHash != proof.BlockHash {
		t.Error("block hash mismatch")
	}
	if len(restored.ProofData) != len(proof.ProofData) {
		t.Errorf("proof data len = %d, want %d", len(restored.ProofData), len(proof.ProofData))
	}
}

func TestSerializeProofNil(t *testing.T) {
	_, err := SerializeProof(nil)
	if err != ErrCircuitSerialize {
		t.Errorf("expected ErrCircuitSerialize, got %v", err)
	}
}

func TestDeserializeProofTooShort(t *testing.T) {
	_, err := DeserializeProof([]byte{0x00})
	if err != ErrCircuitDeserialize {
		t.Errorf("expected ErrCircuitDeserialize, got %v", err)
	}
}

func TestDeserializeProofTruncated(t *testing.T) {
	// Valid start but truncated.
	data := []byte{0x00, 0x04, 't', 'e', 's', 't', 0x00}
	_, err := DeserializeProof(data)
	if err != ErrCircuitDeserialize {
		t.Errorf("expected ErrCircuitDeserialize, got %v", err)
	}
}

func TestComputeVerificationKeyFingerprint(t *testing.T) {
	key1 := []byte{0x01, 0x02, 0x03}
	key2 := []byte{0x04, 0x05, 0x06}

	fp1 := ComputeVerificationKeyFingerprint(key1)
	fp2 := ComputeVerificationKeyFingerprint(key2)

	if fp1.IsZero() {
		t.Error("fingerprint should not be zero")
	}
	if fp1 == fp2 {
		t.Error("different keys should produce different fingerprints")
	}

	// Deterministic.
	fp3 := ComputeVerificationKeyFingerprint(key1)
	if fp1 != fp3 {
		t.Error("fingerprint should be deterministic")
	}
}

func TestConstraintTypes(t *testing.T) {
	if ConstraintR1CS != 0 {
		t.Errorf("ConstraintR1CS = %d, want 0", ConstraintR1CS)
	}
	if ConstraintLinear != 1 {
		t.Errorf("ConstraintLinear = %d, want 1", ConstraintLinear)
	}
	if ConstraintBoolean != 2 {
		t.Errorf("ConstraintBoolean = %d, want 2", ConstraintBoolean)
	}
}

func TestCircuitVariableFields(t *testing.T) {
	circuit := NewStateTransitionCircuit(1)
	def := circuit.Definition()

	// First two variables should be public.
	if !def.Variables[0].IsPublic {
		t.Error("preStateRoot should be public")
	}
	if !def.Variables[1].IsPublic {
		t.Error("postStateRoot should be public")
	}

	// Rest should be private.
	for i := 2; i < len(def.Variables); i++ {
		if def.Variables[i].IsPublic {
			t.Errorf("variable %d should be private", i)
		}
	}

	// Check names.
	if def.Variables[0].Name != "preStateRoot" {
		t.Errorf("var[0] name = %q, want %q", def.Variables[0].Name, "preStateRoot")
	}
	if def.Variables[1].Name != "postStateRoot" {
		t.Errorf("var[1] name = %q, want %q", def.Variables[1].Name, "postStateRoot")
	}
}

func TestProveRoundtrip(t *testing.T) {
	circuit := NewStateTransitionCircuit(3)

	preRoot := types.HexToHash("0x1111")
	postRoot := types.HexToHash("0x2222")
	blockHash := types.HexToHash("0x3333")

	txData := []TransactionWitnessData{
		{GasUsed: 21000, NonceIncrement: 1, BalanceChange: -100},
		{GasUsed: 42000, NonceIncrement: 2, BalanceChange: -200},
		{GasUsed: 63000, NonceIncrement: 1, BalanceChange: 500},
	}

	// Generate witness -> Prove -> Serialize -> Deserialize -> Verify.
	witness, err := circuit.GenerateWitness(preRoot, postRoot, txData)
	if err != nil {
		t.Fatal(err)
	}

	proof, err := circuit.Prove(witness, blockHash)
	if err != nil {
		t.Fatal(err)
	}

	data, err := SerializeProof(proof)
	if err != nil {
		t.Fatal(err)
	}

	restored, err := DeserializeProof(data)
	if err != nil {
		t.Fatal(err)
	}

	valid, err := circuit.VerifyProof(restored)
	if err != nil {
		t.Fatal(err)
	}
	if !valid {
		t.Error("roundtrip proof should be valid")
	}
}

func TestHashToInt64(t *testing.T) {
	// Use hashes with different data in the first 8 bytes (big-endian).
	h1 := types.HexToHash("0xaaaa000000000000000000000000000000000000000000000000000000000000")
	h2 := types.HexToHash("0xbbbb000000000000000000000000000000000000000000000000000000000000")

	v1 := hashToInt64(h1)
	v2 := hashToInt64(h2)

	if v1 == v2 {
		t.Error("different hashes should produce different int64 values")
	}

	// Deterministic.
	v3 := hashToInt64(h1)
	if v1 != v3 {
		t.Error("hashToInt64 should be deterministic")
	}
}

func TestSetVerificationKey(t *testing.T) {
	circuit := NewStateTransitionCircuit(1)

	vk := &VerificationKey{
		CircuitName: "state_transition",
		KeyData:     []byte{0x01, 0x02, 0x03},
		Fingerprint: ComputeVerificationKeyFingerprint([]byte{0x01, 0x02, 0x03}),
	}

	circuit.SetVerificationKey(vk)
	// Just verify no panic; VK not used in stub verification.
}

func TestGenerateWitnessFewerTxs(t *testing.T) {
	// Circuit with 5 tx slots, but only 2 txs provided.
	circuit := NewStateTransitionCircuit(5)

	preRoot := types.HexToHash("0xaaaa")
	postRoot := types.HexToHash("0xbbbb")

	txData := []TransactionWitnessData{
		{GasUsed: 21000, NonceIncrement: 1, BalanceChange: -100},
		{GasUsed: 50000, NonceIncrement: 1, BalanceChange: -500},
	}

	witness, err := circuit.GenerateWitness(preRoot, postRoot, txData)
	if err != nil {
		t.Fatal(err)
	}

	// Should have 15 private values (5 * 3), with last 9 zeroed.
	if len(witness.PrivateValues) != 15 {
		t.Errorf("private values = %d, want 15", len(witness.PrivateValues))
	}
	for i := 6; i < 15; i++ {
		if witness.PrivateValues[i] != 0 {
			t.Errorf("private[%d] = %d, want 0", i, witness.PrivateValues[i])
		}
	}
}
