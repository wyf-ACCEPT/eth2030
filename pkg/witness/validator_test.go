package witness

import (
	"sync"
	"testing"

	"github.com/eth2028/eth2028/core/types"
	"github.com/eth2028/eth2028/crypto"
)

func TestNewWitnessValidator(t *testing.T) {
	// Default config should set MaxWitnessSize.
	v := NewWitnessValidator(WitnessValidatorConfig{})
	if v.config.MaxWitnessSize != DefaultMaxWitnessSize {
		t.Fatalf("expected default max witness size %d, got %d",
			DefaultMaxWitnessSize, v.config.MaxWitnessSize)
	}

	// Custom config is preserved.
	v = NewWitnessValidator(WitnessValidatorConfig{
		MaxWitnessSize: 500,
		StrictMode:     true,
		AllowMissing:   true,
	})
	if v.config.MaxWitnessSize != 500 {
		t.Fatalf("expected max witness size 500, got %d", v.config.MaxWitnessSize)
	}
	if !v.config.StrictMode {
		t.Fatal("expected strict mode to be true")
	}
	if !v.config.AllowMissing {
		t.Fatal("expected allow missing to be true")
	}
}

func TestValidateWitness_EmptyWitness(t *testing.T) {
	v := NewWitnessValidator(WitnessValidatorConfig{})
	result := v.ValidateWitness(types.Hash{}, nil, nil, nil)
	if result.Valid {
		t.Fatal("expected empty witness to be invalid")
	}
	if result.Error != ErrEmptyWitness.Error() {
		t.Fatalf("expected error %q, got %q", ErrEmptyWitness.Error(), result.Error)
	}
	stats := v.Stats()
	if stats.Failed != 1 {
		t.Fatalf("expected 1 failure, got %d", stats.Failed)
	}
}

func TestValidateWitness_TooLarge(t *testing.T) {
	v := NewWitnessValidator(WitnessValidatorConfig{MaxWitnessSize: 100})

	// Create a proof that exceeds the limit.
	bigProof := make([]byte, 101)
	result := v.ValidateWitness(types.Hash{}, []types.Hash{{1}}, nil, [][]byte{bigProof})
	if result.Valid {
		t.Fatal("expected oversized witness to be invalid")
	}
	if result.Error != ErrWitnessTooLarge.Error() {
		t.Fatalf("expected error %q, got %q", ErrWitnessTooLarge.Error(), result.Error)
	}
}

func TestValidateWitness_ValidWithMatchingRoot(t *testing.T) {
	v := NewWitnessValidator(WitnessValidatorConfig{})

	// Create a proof node and use its hash as the state root.
	proofNode := []byte("test proof node data for validation")
	stateRoot := crypto.Keccak256Hash(proofNode)

	// The account key is embedded in the proof node for lookup.
	accountKey := types.BytesToHash(proofNode[:types.HashLength])
	result := v.ValidateWitness(stateRoot, []types.Hash{accountKey}, nil, [][]byte{proofNode})
	if !result.Valid {
		t.Fatalf("expected valid witness, got error: %s", result.Error)
	}

	stats := v.Stats()
	if stats.Validated != 1 {
		t.Fatalf("expected 1 validated, got %d", stats.Validated)
	}
}

func TestValidateWitness_MissingKeys(t *testing.T) {
	v := NewWitnessValidator(WitnessValidatorConfig{})

	// Proof has a node, but account key doesn't match any proof key.
	proofNode := []byte("short node")
	stateRoot := crypto.Keccak256Hash(proofNode)
	missingKey := types.HexToHash("0xdeadbeef")

	result := v.ValidateWitness(stateRoot, []types.Hash{missingKey}, nil, [][]byte{proofNode})
	if result.Valid {
		t.Fatal("expected invalid result due to missing keys")
	}
	if len(result.MissingKeys) != 1 {
		t.Fatalf("expected 1 missing key, got %d", len(result.MissingKeys))
	}
	if result.MissingKeys[0] != missingKey {
		t.Fatalf("missing key mismatch: expected %s, got %s", missingKey, result.MissingKeys[0])
	}
}

func TestValidateWitness_AllowMissing(t *testing.T) {
	v := NewWitnessValidator(WitnessValidatorConfig{AllowMissing: true})

	proofNode := []byte("short node")
	stateRoot := crypto.Keccak256Hash(proofNode)
	missingKey := types.HexToHash("0xdeadbeef")

	result := v.ValidateWitness(stateRoot, []types.Hash{missingKey}, nil, [][]byte{proofNode})
	if !result.Valid {
		t.Fatalf("expected valid with AllowMissing, got error: %s", result.Error)
	}
	// MissingKeys should still be empty when AllowMissing is true
	// because the missing-keys check is skipped.
	if len(result.MissingKeys) != 0 {
		t.Fatalf("expected 0 missing keys with AllowMissing, got %d", len(result.MissingKeys))
	}
}

func TestValidateWitness_StrictMode_ExtraKeys(t *testing.T) {
	v := NewWitnessValidator(WitnessValidatorConfig{StrictMode: true})

	// Build a proof node that contains a 32-byte key prefix.
	var proofData [64]byte
	copy(proofData[:32], types.HexToHash("0xaaaa").Bytes())
	copy(proofData[32:], []byte("extra data padding here!"))

	_ = types.BytesToHash(proofData[:32]) // proof contains this key
	stateRoot := crypto.Keccak256Hash(proofData[:])

	// Only pass a different key as expected -- the proof key is "extra".
	otherKey := types.HexToHash("0xbbbb")
	result := v.ValidateWitness(stateRoot, []types.Hash{otherKey}, nil, [][]byte{proofData[:]})
	if result.Valid {
		t.Fatal("expected invalid in strict mode due to extra keys")
	}
	if len(result.ExtraKeys) == 0 {
		t.Fatal("expected extra keys to be reported")
	}
}

func TestValidateWitness_RootMismatch(t *testing.T) {
	v := NewWitnessValidator(WitnessValidatorConfig{})

	proofNode := []byte("some proof data that is long enough to have a hash prefix here")
	wrongRoot := types.HexToHash("0x1111111111111111111111111111111111111111111111111111111111111111")

	accountKey := types.BytesToHash(proofNode[:types.HashLength])
	result := v.ValidateWitness(wrongRoot, []types.Hash{accountKey}, nil, [][]byte{proofNode})
	if result.Valid {
		t.Fatal("expected invalid due to root mismatch")
	}
	if result.Error != "proof root does not match state root" {
		t.Fatalf("unexpected error: %s", result.Error)
	}
}

func TestValidateAccountProof_Valid(t *testing.T) {
	v := NewWitnessValidator(WitnessValidatorConfig{})

	addr := types.HexToAddress("0x1234567890abcdef1234567890abcdef12345678")

	// Build a proof where the first node hashes to the root.
	firstNode := []byte("first proof node for account proof validation test data")
	root := crypto.Keccak256Hash(firstNode)

	ok := v.ValidateAccountProof(addr, [][]byte{firstNode}, root)
	if !ok {
		t.Fatal("expected account proof to be valid")
	}
}

func TestValidateAccountProof_EmptyProof(t *testing.T) {
	v := NewWitnessValidator(WitnessValidatorConfig{})
	addr := types.HexToAddress("0xdeadbeef")
	root := types.HexToHash("0x1111")

	if v.ValidateAccountProof(addr, nil, root) {
		t.Fatal("expected empty proof to be invalid")
	}
}

func TestValidateAccountProof_ZeroRoot(t *testing.T) {
	v := NewWitnessValidator(WitnessValidatorConfig{})
	addr := types.HexToAddress("0xdeadbeef")

	if v.ValidateAccountProof(addr, [][]byte{[]byte("data")}, types.Hash{}) {
		t.Fatal("expected zero root to be invalid")
	}
}

func TestValidateAccountProof_EmptyNode(t *testing.T) {
	v := NewWitnessValidator(WitnessValidatorConfig{})
	addr := types.HexToAddress("0xdeadbeef")

	firstNode := []byte("valid node")
	root := crypto.Keccak256Hash(firstNode)

	// Second node is empty, should fail.
	if v.ValidateAccountProof(addr, [][]byte{firstNode, {}}, root) {
		t.Fatal("expected proof with empty node to be invalid")
	}
}

func TestValidateStorageProof_Valid(t *testing.T) {
	v := NewWitnessValidator(WitnessValidatorConfig{})

	addr := types.HexToAddress("0xabcdef")
	key := types.HexToHash("0x01")

	firstNode := []byte("storage proof node data for testing")
	storageRoot := crypto.Keccak256Hash(firstNode)

	ok := v.ValidateStorageProof(addr, key, [][]byte{firstNode}, storageRoot)
	if !ok {
		t.Fatal("expected storage proof to be valid")
	}
}

func TestValidateStorageProof_EmptyProof(t *testing.T) {
	v := NewWitnessValidator(WitnessValidatorConfig{})
	addr := types.HexToAddress("0xabcdef")
	key := types.HexToHash("0x01")
	root := types.HexToHash("0x1111")

	if v.ValidateStorageProof(addr, key, nil, root) {
		t.Fatal("expected empty proof to be invalid")
	}
}

func TestValidateStorageProof_RootMismatch(t *testing.T) {
	v := NewWitnessValidator(WitnessValidatorConfig{})
	addr := types.HexToAddress("0xabcdef")
	key := types.HexToHash("0x01")

	firstNode := []byte("storage proof node")
	wrongRoot := types.HexToHash("0x2222222222222222222222222222222222222222222222222222222222222222")

	if v.ValidateStorageProof(addr, key, [][]byte{firstNode}, wrongRoot) {
		t.Fatal("expected storage proof with wrong root to be invalid")
	}
}

func TestValidateStorageProof_TooDeep(t *testing.T) {
	v := NewWitnessValidator(WitnessValidatorConfig{})
	addr := types.HexToAddress("0xabcdef")
	key := types.HexToHash("0x01")

	// Create proof with too many nodes.
	proof := make([][]byte, maxProofDepth+1)
	for i := range proof {
		proof[i] = []byte("node")
	}
	root := crypto.Keccak256Hash(proof[0])
	if v.ValidateStorageProof(addr, key, proof, root) {
		t.Fatal("expected proof exceeding max depth to be invalid")
	}
}

func TestComputeWitnessHash_Empty(t *testing.T) {
	v := NewWitnessValidator(WitnessValidatorConfig{})
	h := v.ComputeWitnessHash(nil, nil)
	if !h.IsZero() {
		t.Fatal("expected zero hash for empty input")
	}
}

func TestComputeWitnessHash_Deterministic(t *testing.T) {
	v := NewWitnessValidator(WitnessValidatorConfig{})

	keys := []types.Hash{
		types.HexToHash("0x03"),
		types.HexToHash("0x01"),
		types.HexToHash("0x02"),
	}
	values := [][]byte{
		[]byte("value3"),
		[]byte("value1"),
		[]byte("value2"),
	}

	h1 := v.ComputeWitnessHash(keys, values)
	h2 := v.ComputeWitnessHash(keys, values)
	if h1 != h2 {
		t.Fatal("expected deterministic hash, got different results")
	}
	if h1.IsZero() {
		t.Fatal("expected non-zero hash")
	}
}

func TestComputeWitnessHash_OrderIndependent(t *testing.T) {
	v := NewWitnessValidator(WitnessValidatorConfig{})

	// Two orderings of the same key-value pairs.
	keys1 := []types.Hash{
		types.HexToHash("0x01"),
		types.HexToHash("0x02"),
	}
	values1 := [][]byte{[]byte("a"), []byte("b")}

	keys2 := []types.Hash{
		types.HexToHash("0x02"),
		types.HexToHash("0x01"),
	}
	values2 := [][]byte{[]byte("b"), []byte("a")}

	h1 := v.ComputeWitnessHash(keys1, values1)
	h2 := v.ComputeWitnessHash(keys2, values2)
	if h1 != h2 {
		t.Fatal("expected same hash regardless of input order")
	}
}

func TestStats_InitiallyZero(t *testing.T) {
	v := NewWitnessValidator(WitnessValidatorConfig{})
	stats := v.Stats()
	if stats.Validated != 0 || stats.Failed != 0 || stats.MissingCount != 0 {
		t.Fatal("expected all stats to be zero initially")
	}
}

func TestStats_TrackValidationsAndFailures(t *testing.T) {
	v := NewWitnessValidator(WitnessValidatorConfig{})

	// Trigger a failure (empty witness).
	v.ValidateWitness(types.Hash{}, nil, nil, nil)

	// Trigger a success: node must be >= 32 bytes so BytesToHash works.
	node := []byte("proof node with enough data for a full hash key!!")
	root := crypto.Keccak256Hash(node)
	key := types.BytesToHash(node[:types.HashLength])
	v.ValidateWitness(root, []types.Hash{key}, nil, [][]byte{node})

	stats := v.Stats()
	if stats.Failed != 1 {
		t.Fatalf("expected 1 failure, got %d", stats.Failed)
	}
	if stats.Validated != 1 {
		t.Fatalf("expected 1 validated, got %d", stats.Validated)
	}
}

func TestWitnessValidator_ConcurrentSafety(t *testing.T) {
	v := NewWitnessValidator(WitnessValidatorConfig{})
	node := []byte("concurrent proof node data here!")
	root := crypto.Keccak256Hash(node)
	key := types.BytesToHash(node[:types.HashLength])

	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			v.ValidateWitness(root, []types.Hash{key}, nil, [][]byte{node})
			v.Stats()
		}()
	}
	wg.Wait()

	stats := v.Stats()
	if stats.Validated != 50 {
		t.Fatalf("expected 50 validated, got %d", stats.Validated)
	}
}

func TestValidateWitness_StorageKeys(t *testing.T) {
	v := NewWitnessValidator(WitnessValidatorConfig{})

	// Each proof node contributes its first 32 bytes as a proof key.
	key1 := types.HexToHash("0xaa")
	key2 := types.HexToHash("0xbb")
	node1 := make([]byte, 48)
	node2 := make([]byte, 48)
	copy(node1[:32], key1.Bytes())
	copy(node2[:32], key2.Bytes())

	stateRoot := crypto.Keccak256Hash(append(node1, node2...))

	result := v.ValidateWitness(stateRoot, []types.Hash{key1}, []types.Hash{key2}, [][]byte{node1, node2})
	if !result.Valid {
		t.Fatalf("expected valid witness with storage keys, got error: %s", result.Error)
	}
}

func TestValidateWitness_ZeroStateRoot(t *testing.T) {
	v := NewWitnessValidator(WitnessValidatorConfig{})

	// With zero state root, root verification is skipped.
	var proofData [32]byte
	key := types.BytesToHash(proofData[:])

	result := v.ValidateWitness(types.Hash{}, []types.Hash{key}, nil, [][]byte{proofData[:]})
	if !result.Valid {
		t.Fatalf("expected valid with zero state root, got error: %s", result.Error)
	}
}
