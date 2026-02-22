package vm

import (
	"fmt"
	"sync"
	"testing"

	"github.com/eth2030/eth2030/crypto"
)

func defaultNIIConfig() NIIEnhancedConfig {
	return NIIEnhancedConfig{
		MaxBatchSize:  256,
		MaxProofDepth: 64,
		CacheSize:     1024,
	}
}

// buildMerkleProof constructs a valid Merkle root and proof path for testing.
// It builds a binary tree from the leaf up using the given key, value, and siblings.
func buildMerkleProof(key, value []byte, siblings [][]byte, leafIdx uint64) (root []byte, path [][]byte) {
	leafData := append(key, value...)
	current := crypto.Keccak256(leafData)

	path = make([][]byte, len(siblings))
	for i, sibling := range siblings {
		path[i] = sibling
		bit := (leafIdx >> uint(i)) & 1
		if bit == 0 {
			combined := append(current, sibling...)
			current = crypto.Keccak256(combined)
		} else {
			combined := append(sibling, current...)
			current = crypto.Keccak256(combined)
		}
	}
	return current, path
}

// --- NewNIIEnhancedPrecompile ---

func TestNewNIIEnhancedPrecompile(t *testing.T) {
	p := NewNIIEnhancedPrecompile(defaultNIIConfig())
	if p == nil {
		t.Fatal("NewNIIEnhancedPrecompile returned nil")
	}
	if p.CacheSize() != 0 {
		t.Errorf("CacheSize() = %d, want 0", p.CacheSize())
	}
}

func TestNewNIIEnhancedPrecompileDefaults(t *testing.T) {
	// Zero config should get sensible defaults.
	p := NewNIIEnhancedPrecompile(NIIEnhancedConfig{})
	if p.config.MaxBatchSize != 256 {
		t.Errorf("default MaxBatchSize = %d, want 256", p.config.MaxBatchSize)
	}
	if p.config.MaxProofDepth != 64 {
		t.Errorf("default MaxProofDepth = %d, want 64", p.config.MaxProofDepth)
	}
	if p.config.CacheSize != 1024 {
		t.Errorf("default CacheSize = %d, want 1024", p.config.CacheSize)
	}
}

// --- VerifyInclusion ---

func TestVerifyInclusionValidProof(t *testing.T) {
	p := NewNIIEnhancedPrecompile(defaultNIIConfig())

	key := []byte("account_0x1234")
	value := []byte("balance_1000")

	// Build siblings for a 3-level tree.
	siblings := [][]byte{
		crypto.Keccak256([]byte("sibling0")),
		crypto.Keccak256([]byte("sibling1")),
		crypto.Keccak256([]byte("sibling2")),
	}

	// Derive leaf index from key (same as the precompile does internally).
	var leafIdx uint64
	for i := 0; i < 8 && i < len(key); i++ {
		leafIdx |= uint64(key[i]) << uint(8*i)
	}

	root, path := buildMerkleProof(key, value, siblings, leafIdx)

	valid, gas, err := p.VerifyInclusion(key, value, root, path)
	if err != nil {
		t.Fatalf("VerifyInclusion: %v", err)
	}
	if !valid {
		t.Error("expected valid proof")
	}
	expectedGas := uint64(niiEnhancedBaseGas + 3*niiEnhancedPerStepGas)
	if gas != expectedGas {
		t.Errorf("gas = %d, want %d", gas, expectedGas)
	}
}

func TestVerifyInclusionInvalidProof(t *testing.T) {
	p := NewNIIEnhancedPrecompile(defaultNIIConfig())

	key := []byte("account_0x1234")
	value := []byte("balance_1000")
	wrongRoot := crypto.Keccak256([]byte("wrong_root"))

	siblings := [][]byte{
		crypto.Keccak256([]byte("sibling0")),
	}

	valid, _, err := p.VerifyInclusion(key, value, wrongRoot, siblings)
	if err != nil {
		t.Fatalf("VerifyInclusion: %v", err)
	}
	if valid {
		t.Error("expected invalid proof with wrong root")
	}
}

func TestVerifyInclusionEmptyKey(t *testing.T) {
	p := NewNIIEnhancedPrecompile(defaultNIIConfig())
	_, _, err := p.VerifyInclusion(nil, []byte("value"), []byte("root"), nil)
	if err != ErrNIIEmptyKey {
		t.Errorf("empty key: got %v, want %v", err, ErrNIIEmptyKey)
	}
}

func TestVerifyInclusionEmptyRoot(t *testing.T) {
	p := NewNIIEnhancedPrecompile(defaultNIIConfig())
	_, _, err := p.VerifyInclusion([]byte("key"), []byte("value"), nil, nil)
	if err != ErrNIIEmptyRoot {
		t.Errorf("empty root: got %v, want %v", err, ErrNIIEmptyRoot)
	}
}

func TestVerifyInclusionDepthExceeded(t *testing.T) {
	cfg := defaultNIIConfig()
	cfg.MaxProofDepth = 2
	p := NewNIIEnhancedPrecompile(cfg)

	siblings := [][]byte{
		crypto.Keccak256([]byte("s0")),
		crypto.Keccak256([]byte("s1")),
		crypto.Keccak256([]byte("s2")),
	}

	_, _, err := p.VerifyInclusion([]byte("key"), []byte("value"), []byte("root"), siblings)
	if err != ErrNIIProofDepthExceeded {
		t.Errorf("depth exceeded: got %v, want %v", err, ErrNIIProofDepthExceeded)
	}
}

func TestVerifyInclusionEmptySibling(t *testing.T) {
	p := NewNIIEnhancedPrecompile(defaultNIIConfig())

	siblings := [][]byte{
		nil, // empty sibling
	}
	_, _, err := p.VerifyInclusion([]byte("key"), []byte("value"), []byte("root"), siblings)
	if err != ErrNIIInvalidProof {
		t.Errorf("empty sibling: got %v, want %v", err, ErrNIIInvalidProof)
	}
}

func TestVerifyInclusionNoPath(t *testing.T) {
	p := NewNIIEnhancedPrecompile(defaultNIIConfig())

	key := []byte("key")
	value := []byte("value")
	// With no proof path, the computed root is just keccak256(key || value).
	expectedRoot := crypto.Keccak256(append(key, value...))

	valid, gas, err := p.VerifyInclusion(key, value, expectedRoot, nil)
	if err != nil {
		t.Fatalf("VerifyInclusion: %v", err)
	}
	if !valid {
		t.Error("no-path proof should be valid when root matches leaf hash")
	}
	if gas != niiEnhancedBaseGas {
		t.Errorf("gas = %d, want %d", gas, niiEnhancedBaseGas)
	}
}

// --- BatchVerify ---

func TestBatchVerifyMultipleValid(t *testing.T) {
	p := NewNIIEnhancedPrecompile(defaultNIIConfig())

	// Build 3 items that all verify against the same root.
	// Each item has its own valid proof; we use independent single-level trees
	// to keep it simple. For batch verification with a shared root,
	// we build a deeper tree.
	key := []byte("shared_key_for_batch_test")
	value := []byte("shared_value")
	siblings := [][]byte{crypto.Keccak256([]byte("batch_sibling"))}

	var leafIdx uint64
	for i := 0; i < 8 && i < len(key); i++ {
		leafIdx |= uint64(key[i]) << uint(8*i)
	}
	root, path := buildMerkleProof(key, value, siblings, leafIdx)

	items := []NIIInclusionItem{
		{Key: key, Value: value, ProofPath: path, LeafIndex: 0},
		{Key: key, Value: value, ProofPath: path, LeafIndex: 0},
		{Key: key, Value: value, ProofPath: path, LeafIndex: 0},
	}

	result, err := p.BatchVerify(items, root)
	if err != nil {
		t.Fatalf("BatchVerify: %v", err)
	}
	if result.VerifiedCount != 3 {
		t.Errorf("VerifiedCount = %d, want 3", result.VerifiedCount)
	}
	for i, v := range result.Proofs {
		if !v {
			t.Errorf("Proofs[%d] = false, want true", i)
		}
	}
	if result.TotalGas == 0 {
		t.Error("TotalGas should be > 0")
	}
}

func TestBatchVerifyEmpty(t *testing.T) {
	p := NewNIIEnhancedPrecompile(defaultNIIConfig())
	result, err := p.BatchVerify(nil, []byte("root"))
	if err != nil {
		t.Fatalf("BatchVerify empty: %v", err)
	}
	if result.VerifiedCount != 0 {
		t.Errorf("VerifiedCount = %d, want 0", result.VerifiedCount)
	}
	if result.TotalGas != 0 {
		t.Errorf("TotalGas = %d, want 0", result.TotalGas)
	}
}

func TestBatchVerifyTooLarge(t *testing.T) {
	cfg := defaultNIIConfig()
	cfg.MaxBatchSize = 2
	p := NewNIIEnhancedPrecompile(cfg)

	items := make([]NIIInclusionItem, 3)
	for i := range items {
		items[i] = NIIInclusionItem{Key: []byte("k"), Value: []byte("v")}
	}

	_, err := p.BatchVerify(items, []byte("root"))
	if err != ErrNIIBatchTooLarge {
		t.Errorf("batch too large: got %v, want %v", err, ErrNIIBatchTooLarge)
	}
}

func TestBatchVerifyEmptyRoot(t *testing.T) {
	p := NewNIIEnhancedPrecompile(defaultNIIConfig())
	items := []NIIInclusionItem{{Key: []byte("k"), Value: []byte("v")}}
	_, err := p.BatchVerify(items, nil)
	if err != ErrNIIEmptyRoot {
		t.Errorf("empty root: got %v, want %v", err, ErrNIIEmptyRoot)
	}
}

func TestBatchVerifyMixedResults(t *testing.T) {
	p := NewNIIEnhancedPrecompile(defaultNIIConfig())

	key := []byte("valid_key_12345")
	value := []byte("valid_value")
	siblings := [][]byte{crypto.Keccak256([]byte("sib"))}

	var leafIdx uint64
	for i := 0; i < 8 && i < len(key); i++ {
		leafIdx |= uint64(key[i]) << uint(8*i)
	}
	root, path := buildMerkleProof(key, value, siblings, leafIdx)

	items := []NIIInclusionItem{
		{Key: key, Value: value, ProofPath: path, LeafIndex: 0},                          // valid
		{Key: []byte("bad_key_1234567"), Value: []byte("wrong"), ProofPath: path, LeafIndex: 0}, // invalid
	}

	result, err := p.BatchVerify(items, root)
	if err != nil {
		t.Fatalf("BatchVerify: %v", err)
	}
	if result.VerifiedCount != 1 {
		t.Errorf("VerifiedCount = %d, want 1", result.VerifiedCount)
	}
	if !result.Proofs[0] {
		t.Error("Proofs[0] should be true")
	}
	if result.Proofs[1] {
		t.Error("Proofs[1] should be false")
	}
}

// --- EstimateGas ---

func TestEstimateGasSingle(t *testing.T) {
	p := NewNIIEnhancedPrecompile(defaultNIIConfig())
	gas := p.EstimateGas(10, 1)
	expected := uint64(niiEnhancedBaseGas + 10*niiEnhancedPerStepGas)
	if gas != expected {
		t.Errorf("EstimateGas(10,1) = %d, want %d", gas, expected)
	}
}

func TestEstimateGasBatch(t *testing.T) {
	p := NewNIIEnhancedPrecompile(defaultNIIConfig())
	gas := p.EstimateGas(5, 10)
	perItem := uint64(niiEnhancedBaseGas + 5*niiEnhancedPerStepGas)
	expected := perItem * 10 * niiEnhancedBatchBonus / 100
	if gas != expected {
		t.Errorf("EstimateGas(5,10) = %d, want %d", gas, expected)
	}
}

func TestEstimateGasZeroBatch(t *testing.T) {
	p := NewNIIEnhancedPrecompile(defaultNIIConfig())
	gas := p.EstimateGas(3, 0)
	// batchSize <= 0 defaults to 1.
	expected := uint64(niiEnhancedBaseGas + 3*niiEnhancedPerStepGas)
	if gas != expected {
		t.Errorf("EstimateGas(3,0) = %d, want %d", gas, expected)
	}
}

// --- Cache ---

func TestCacheProofAndHit(t *testing.T) {
	p := NewNIIEnhancedPrecompile(defaultNIIConfig())

	key := []byte("proof_key_1")
	p.CacheProof(key, true)

	found, result := p.CacheHit(key)
	if !found {
		t.Fatal("expected cache hit")
	}
	if !result {
		t.Error("cached result should be true")
	}
}

func TestCacheMiss(t *testing.T) {
	p := NewNIIEnhancedPrecompile(defaultNIIConfig())

	found, _ := p.CacheHit([]byte("nonexistent"))
	if found {
		t.Error("expected cache miss")
	}
}

func TestCacheOverwrite(t *testing.T) {
	p := NewNIIEnhancedPrecompile(defaultNIIConfig())

	key := []byte("key")
	p.CacheProof(key, true)
	p.CacheProof(key, false) // overwrite

	found, result := p.CacheHit(key)
	if !found {
		t.Fatal("expected cache hit")
	}
	if result {
		t.Error("cache should have been overwritten to false")
	}
}

func TestCacheEviction(t *testing.T) {
	cfg := defaultNIIConfig()
	cfg.CacheSize = 3
	p := NewNIIEnhancedPrecompile(cfg)

	p.CacheProof([]byte("key1"), true)
	p.CacheProof([]byte("key2"), true)
	p.CacheProof([]byte("key3"), true)
	// Cache is full; adding key4 should evict key1.
	p.CacheProof([]byte("key4"), true)

	if found, _ := p.CacheHit([]byte("key1")); found {
		t.Error("key1 should have been evicted")
	}
	if found, _ := p.CacheHit([]byte("key4")); !found {
		t.Error("key4 should be in cache")
	}
	if p.CacheSize() != 3 {
		t.Errorf("CacheSize() = %d, want 3", p.CacheSize())
	}
}

func TestClearCache(t *testing.T) {
	p := NewNIIEnhancedPrecompile(defaultNIIConfig())
	p.CacheProof([]byte("k1"), true)
	p.CacheProof([]byte("k2"), false)

	p.ClearCache()

	if p.CacheSize() != 0 {
		t.Errorf("CacheSize() after clear = %d, want 0", p.CacheSize())
	}
	if found, _ := p.CacheHit([]byte("k1")); found {
		t.Error("cache should be empty after clear")
	}
}

func TestCacheEmptyKey(t *testing.T) {
	p := NewNIIEnhancedPrecompile(defaultNIIConfig())
	p.CacheProof(nil, true)
	if p.CacheSize() != 0 {
		t.Error("nil key should not be cached")
	}
	found, _ := p.CacheHit(nil)
	if found {
		t.Error("nil key should not hit cache")
	}
}

// --- Concurrency ---

func TestConcurrentCacheAccess(t *testing.T) {
	p := NewNIIEnhancedPrecompile(defaultNIIConfig())

	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			key := []byte(fmt.Sprintf("key_%d", i))
			p.CacheProof(key, i%2 == 0)
			p.CacheHit(key)
		}(i)
	}
	wg.Wait()

	if p.CacheSize() > 50 {
		t.Errorf("CacheSize() = %d, should be <= 50", p.CacheSize())
	}
}

// --- Batch gas discount ---

func TestBatchGasDiscount(t *testing.T) {
	p := NewNIIEnhancedPrecompile(defaultNIIConfig())

	key := []byte("discount_key_1234")
	value := []byte("discount_value")
	siblings := [][]byte{crypto.Keccak256([]byte("dsib"))}

	var leafIdx uint64
	for i := 0; i < 8 && i < len(key); i++ {
		leafIdx |= uint64(key[i]) << uint(8*i)
	}
	root, path := buildMerkleProof(key, value, siblings, leafIdx)

	// Single verification gas.
	_, singleGas, _ := p.VerifyInclusion(key, value, root, path)

	// Batch of 5 with same proof.
	items := make([]NIIInclusionItem, 5)
	for i := range items {
		items[i] = NIIInclusionItem{Key: key, Value: value, ProofPath: path}
	}
	result, _ := p.BatchVerify(items, root)

	// Batch gas should be 80% of 5 * singleGas.
	expectedBatchGas := singleGas * 5 * niiEnhancedBatchBonus / 100
	if result.TotalGas != expectedBatchGas {
		t.Errorf("batch gas = %d, want %d (80%% of %d*5)", result.TotalGas, expectedBatchGas, singleGas)
	}
}
