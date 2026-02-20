package state

import (
	"math/big"
	"sync"
	"testing"

	"github.com/eth2028/eth2028/core/types"
	"github.com/eth2028/eth2028/crypto"
)

// buildResurrectionProof creates a valid ExpiryProof for testing.
// It constructs Merkle proof nodes that form a valid hash chain.
// The returned proof's StateRoot is the Keccak256 hash of the
// topmost proof node, which the test must use as the state root
// when expiring the account via ExpireAccountWithRoot.
func buildResurrectionProof(
	addr types.Address,
	nonce uint64,
	balance *big.Int,
	codeHash []byte,
	storageKeys []types.Hash,
	storageValues []types.Hash,
	epochExpired uint64,
) ExpiryProof {
	// Leaf node: contains the address hash prefix and account data.
	addrHash := crypto.Keccak256(addr[:])
	leafData := make([]byte, 0, 128)
	leafData = append(leafData, addrHash[:4]...)
	if balance != nil {
		leafData = append(leafData, balance.Bytes()...)
	}
	leafData = append(leafData, byte(nonce))

	// Build chain: leaf -> mid -> rootNode
	// keccak(rootNode) == stateRoot
	// rootNode contains keccak(mid)
	// mid contains keccak(leaf)
	leafHash := crypto.Keccak256(leafData)

	midData := make([]byte, 0, 64)
	midData = append(midData, []byte("mid:")...)
	midData = append(midData, leafHash...)

	midHash := crypto.Keccak256(midData)

	rootData := make([]byte, 0, 64)
	rootData = append(rootData, []byte("root:")...)
	rootData = append(rootData, midHash...)

	proofNodes := [][]byte{leafData, midData, rootData}
	computedRoot := crypto.Keccak256Hash(rootData)

	return ExpiryProof{
		Address:       addr,
		Nonce:         nonce,
		Balance:       balance,
		CodeHash:      codeHash,
		StorageKeys:   storageKeys,
		StorageValues: storageValues,
		ProofNodes:    proofNodes,
		EpochExpired:  epochExpired,
		StateRoot:     computedRoot,
	}
}

func TestEpochManager_Basic(t *testing.T) {
	em := NewEpochManager(2)
	if em.CurrentEpoch() != 0 {
		t.Errorf("initial epoch = %d, want 0", em.CurrentEpoch())
	}
	if em.Threshold() != 2 {
		t.Errorf("threshold = %d, want 2", em.Threshold())
	}

	if !em.AdvanceEpoch(5) {
		t.Error("AdvanceEpoch(5) should return true")
	}
	if em.CurrentEpoch() != 5 {
		t.Errorf("epoch = %d, want 5", em.CurrentEpoch())
	}

	// Advancing to same or lower epoch is a no-op.
	if em.AdvanceEpoch(5) {
		t.Error("AdvanceEpoch(5) again should return false")
	}
	if em.AdvanceEpoch(3) {
		t.Error("AdvanceEpoch(3) should return false")
	}
}

func TestEpochManager_DefaultThreshold(t *testing.T) {
	em := NewEpochManager(0) // should default to 2
	if em.Threshold() != 2 {
		t.Errorf("threshold = %d, want 2 (default)", em.Threshold())
	}
}

func TestEpochManager_IsEligibleForExpiry(t *testing.T) {
	em := NewEpochManager(2)
	em.AdvanceEpoch(10)

	// lastAccess=7, threshold=2, current=10: 10 > 7+2 = true
	if !em.IsEligibleForExpiry(7) {
		t.Error("epoch 7 + threshold 2 < current 10, should be eligible")
	}

	// lastAccess=8, threshold=2, current=10: 10 > 8+2 = false (boundary)
	if em.IsEligibleForExpiry(8) {
		t.Error("epoch 8 + threshold 2 = current 10, should NOT be eligible")
	}

	// lastAccess=9, threshold=2, current=10: 10 > 9+2 = false
	if em.IsEligibleForExpiry(9) {
		t.Error("epoch 9 + threshold 2 > current 10, should NOT be eligible")
	}
}

func TestExpiryEngine_ExpireAccount(t *testing.T) {
	sdb := NewMemoryStateDB()
	addr := types.BytesToAddress([]byte{0x01})
	sdb.CreateAccount(addr)
	sdb.SetNonce(addr, 5)
	sdb.AddBalance(addr, big.NewInt(1000))

	engine := NewExpiryEngine(DefaultExpiryEngineConfig(), sdb)

	if err := engine.ExpireAccount(addr, 10); err != nil {
		t.Fatalf("ExpireAccount: %v", err)
	}

	if !engine.IsExpired(addr) {
		t.Error("account should be expired after ExpireAccount")
	}

	// Double expire should fail.
	if err := engine.ExpireAccount(addr, 11); err != errAlreadyExpired {
		t.Errorf("double expire: got %v, want errAlreadyExpired", err)
	}

	stats := engine.GetStats()
	if stats.ExpiredCount != 1 {
		t.Errorf("ExpiredCount = %d, want 1", stats.ExpiredCount)
	}
}

func TestExpiryEngine_IsExpired_Unknown(t *testing.T) {
	sdb := NewMemoryStateDB()
	engine := NewExpiryEngine(DefaultExpiryEngineConfig(), sdb)

	if engine.IsExpired(types.BytesToAddress([]byte{0xFF})) {
		t.Error("unknown address should not be expired")
	}
}

func TestExpiryEngine_ResurrectWithWitness_Success(t *testing.T) {
	sdb := NewMemoryStateDB()
	addr := types.BytesToAddress([]byte{0x01})
	sdb.CreateAccount(addr)
	sdb.SetNonce(addr, 3)
	sdb.AddBalance(addr, big.NewInt(500))

	engine := NewExpiryEngine(DefaultExpiryEngineConfig(), sdb)

	// Build the proof first to get the computed root.
	proof := buildResurrectionProof(
		addr, 3, big.NewInt(500), types.EmptyCodeHash.Bytes(),
		nil, nil, 10,
	)

	// Expire with the proof's computed root so they match.
	if err := engine.ExpireAccountWithRoot(addr, 10, proof.StateRoot); err != nil {
		t.Fatalf("ExpireAccountWithRoot: %v", err)
	}

	if err := engine.ResurrectWithWitness(proof); err != nil {
		t.Fatalf("ResurrectWithWitness: %v", err)
	}

	if engine.IsExpired(addr) {
		t.Error("account should not be expired after resurrection")
	}

	stats := engine.GetStats()
	if stats.ResurrectedCount != 1 {
		t.Errorf("ResurrectedCount = %d, want 1", stats.ResurrectedCount)
	}
	if stats.TotalProofsChecked != 1 {
		t.Errorf("TotalProofsChecked = %d, want 1", stats.TotalProofsChecked)
	}
}

func TestExpiryEngine_ResurrectWithWitness_StorageRestored(t *testing.T) {
	sdb := NewMemoryStateDB()
	addr := types.BytesToAddress([]byte{0x02})
	sdb.CreateAccount(addr)
	sdb.SetNonce(addr, 1)

	key1 := types.BytesToHash([]byte{0x11})
	val1 := types.BytesToHash([]byte{0xAA})
	key2 := types.BytesToHash([]byte{0x22})
	val2 := types.BytesToHash([]byte{0xBB})
	sdb.SetState(addr, key1, val1)
	sdb.SetState(addr, key2, val2)

	engine := NewExpiryEngine(DefaultExpiryEngineConfig(), sdb)

	// Build proof with storage restoration.
	proof := buildResurrectionProof(
		addr, 1, big.NewInt(0), types.EmptyCodeHash.Bytes(),
		[]types.Hash{key1, key2}, []types.Hash{val1, val2}, 5,
	)

	if err := engine.ExpireAccountWithRoot(addr, 5, proof.StateRoot); err != nil {
		t.Fatalf("ExpireAccountWithRoot: %v", err)
	}

	// Clear the account from statedb to simulate expiry removal.
	sdb.CreateAccount(addr)

	if err := engine.ResurrectWithWitness(proof); err != nil {
		t.Fatalf("ResurrectWithWitness: %v", err)
	}

	// Check storage was restored.
	got1 := sdb.GetState(addr, key1)
	got2 := sdb.GetState(addr, key2)
	if got1 != val1 {
		t.Errorf("storage key1: got %x, want %x", got1, val1)
	}
	if got2 != val2 {
		t.Errorf("storage key2: got %x, want %x", got2, val2)
	}
}

func TestExpiryEngine_ResurrectWithWitness_EmptyProof(t *testing.T) {
	sdb := NewMemoryStateDB()
	addr := types.BytesToAddress([]byte{0x01})
	sdb.CreateAccount(addr)

	engine := NewExpiryEngine(DefaultExpiryEngineConfig(), sdb)
	engine.ExpireAccount(addr, 1)

	proof := ExpiryProof{
		Address:    addr,
		ProofNodes: nil,
	}

	if err := engine.ResurrectWithWitness(proof); err != errProofEmpty {
		t.Errorf("got %v, want errProofEmpty", err)
	}

	stats := engine.GetStats()
	if stats.VerifyFailures != 1 {
		t.Errorf("VerifyFailures = %d, want 1", stats.VerifyFailures)
	}
}

func TestExpiryEngine_ResurrectWithWitness_TooManyNodes(t *testing.T) {
	cfg := DefaultExpiryEngineConfig()
	cfg.MaxProofNodes = 3
	sdb := NewMemoryStateDB()
	addr := types.BytesToAddress([]byte{0x01})
	sdb.CreateAccount(addr)

	engine := NewExpiryEngine(cfg, sdb)
	engine.ExpireAccount(addr, 1)

	nodes := make([][]byte, 4)
	for i := range nodes {
		nodes[i] = []byte{byte(i)}
	}
	proof := ExpiryProof{
		Address:    addr,
		ProofNodes: nodes,
	}

	if err := engine.ResurrectWithWitness(proof); err != errProofTooManyNodes {
		t.Errorf("got %v, want errProofTooManyNodes", err)
	}
}

func TestExpiryEngine_ResurrectWithWitness_KeyValueMismatch(t *testing.T) {
	sdb := NewMemoryStateDB()
	addr := types.BytesToAddress([]byte{0x01})
	sdb.CreateAccount(addr)

	engine := NewExpiryEngine(DefaultExpiryEngineConfig(), sdb)
	engine.ExpireAccount(addr, 1)

	proof := ExpiryProof{
		Address:       addr,
		ProofNodes:    [][]byte{{0x01}},
		StorageKeys:   []types.Hash{{0x01}},
		StorageValues: nil, // mismatch
	}

	if err := engine.ResurrectWithWitness(proof); err != errProofKeyValueLen {
		t.Errorf("got %v, want errProofKeyValueLen", err)
	}
}

func TestExpiryEngine_ResurrectWithWitness_TooManyKeys(t *testing.T) {
	cfg := DefaultExpiryEngineConfig()
	cfg.MaxStorageKeys = 2
	sdb := NewMemoryStateDB()
	addr := types.BytesToAddress([]byte{0x01})
	sdb.CreateAccount(addr)

	engine := NewExpiryEngine(cfg, sdb)
	engine.ExpireAccount(addr, 1)

	proof := ExpiryProof{
		Address:       addr,
		ProofNodes:    [][]byte{{0x01}},
		StorageKeys:   make([]types.Hash, 3),
		StorageValues: make([]types.Hash, 3),
	}

	if err := engine.ResurrectWithWitness(proof); err != errProofTooManyKeys {
		t.Errorf("got %v, want errProofTooManyKeys", err)
	}
}

func TestExpiryEngine_ResurrectWithWitness_UnknownAddress(t *testing.T) {
	sdb := NewMemoryStateDB()
	engine := NewExpiryEngine(DefaultExpiryEngineConfig(), sdb)

	proof := ExpiryProof{
		Address:    types.BytesToAddress([]byte{0xFF}),
		ProofNodes: [][]byte{{0x01}},
	}

	if err := engine.ResurrectWithWitness(proof); err != errUnknownExpired {
		t.Errorf("got %v, want errUnknownExpired", err)
	}
}

func TestExpiryEngine_ResurrectWithWitness_EpochMismatch(t *testing.T) {
	sdb := NewMemoryStateDB()
	addr := types.BytesToAddress([]byte{0x01})
	sdb.CreateAccount(addr)

	engine := NewExpiryEngine(DefaultExpiryEngineConfig(), sdb)
	engine.ExpireAccount(addr, 10)

	rec := engine.GetExpiredRecord(addr)
	proof := ExpiryProof{
		Address:      addr,
		ProofNodes:   [][]byte{{0x01}},
		EpochExpired: 999, // wrong epoch
		StateRoot:    rec.stateRoot,
	}

	if err := engine.ResurrectWithWitness(proof); err != errProofEpochMismatch {
		t.Errorf("got %v, want errProofEpochMismatch", err)
	}
}

func TestExpiryEngine_ResurrectWithWitness_RootMismatch(t *testing.T) {
	sdb := NewMemoryStateDB()
	addr := types.BytesToAddress([]byte{0x01})
	sdb.CreateAccount(addr)

	engine := NewExpiryEngine(DefaultExpiryEngineConfig(), sdb)
	engine.ExpireAccount(addr, 10)

	proof := ExpiryProof{
		Address:      addr,
		ProofNodes:   [][]byte{{0x01}},
		EpochExpired: 10,
		StateRoot:    types.BytesToHash([]byte{0xDE, 0xAD}), // wrong root
	}

	if err := engine.ResurrectWithWitness(proof); err != errProofRootMismatch {
		t.Errorf("got %v, want errProofRootMismatch", err)
	}
}

func TestExpiryEngine_ResurrectWithWitness_AccountDataMismatch(t *testing.T) {
	sdb := NewMemoryStateDB()
	addr := types.BytesToAddress([]byte{0x01})
	sdb.CreateAccount(addr)
	sdb.SetNonce(addr, 5)
	sdb.AddBalance(addr, big.NewInt(100))

	engine := NewExpiryEngine(DefaultExpiryEngineConfig(), sdb)

	// Build a valid proof chain but with wrong nonce (999 != 5).
	proof := buildResurrectionProof(
		addr, 999, big.NewInt(100), types.EmptyCodeHash.Bytes(),
		nil, nil, 10,
	)

	// Expire with the proof's root so root check passes.
	if err := engine.ExpireAccountWithRoot(addr, 10, proof.StateRoot); err != nil {
		t.Fatalf("ExpireAccountWithRoot: %v", err)
	}

	// Should fail on account data mismatch (nonce 999 != 5).
	if err := engine.ResurrectWithWitness(proof); err != errProofAccountData {
		t.Errorf("got %v, want errProofAccountData", err)
	}
}

func TestExpiryEngine_ExpireBatch(t *testing.T) {
	sdb := NewMemoryStateDB()
	addrs := make([]types.Address, 5)
	for i := range addrs {
		addrs[i] = types.BytesToAddress([]byte{byte(i + 1)})
		sdb.CreateAccount(addrs[i])
	}

	engine := NewExpiryEngine(DefaultExpiryEngineConfig(), sdb)

	count, err := engine.ExpireBatch(addrs, 10)
	if err != nil {
		t.Fatalf("ExpireBatch: %v", err)
	}
	if count != 5 {
		t.Errorf("count = %d, want 5", count)
	}

	for _, addr := range addrs {
		if !engine.IsExpired(addr) {
			t.Errorf("address %x should be expired", addr)
		}
	}

	stats := engine.GetStats()
	if stats.ExpiredCount != 5 {
		t.Errorf("ExpiredCount = %d, want 5", stats.ExpiredCount)
	}
}

func TestExpiryEngine_ExpireBatch_SkipsAlreadyExpired(t *testing.T) {
	sdb := NewMemoryStateDB()
	addr := types.BytesToAddress([]byte{0x01})
	sdb.CreateAccount(addr)

	engine := NewExpiryEngine(DefaultExpiryEngineConfig(), sdb)
	engine.ExpireAccount(addr, 1)

	addr2 := types.BytesToAddress([]byte{0x02})
	sdb.CreateAccount(addr2)

	count, err := engine.ExpireBatch([]types.Address{addr, addr2}, 2)
	if err != errAlreadyExpired {
		t.Errorf("expected errAlreadyExpired, got %v", err)
	}
	if count != 1 {
		t.Errorf("count = %d, want 1", count)
	}
	if !engine.IsExpired(addr2) {
		t.Error("addr2 should be expired")
	}
}

func TestExpiryEngine_ExpiredAddresses(t *testing.T) {
	sdb := NewMemoryStateDB()
	engine := NewExpiryEngine(DefaultExpiryEngineConfig(), sdb)

	addrs := engine.ExpiredAddresses()
	if len(addrs) != 0 {
		t.Errorf("empty engine should have 0 expired, got %d", len(addrs))
	}

	for i := 0; i < 3; i++ {
		addr := types.BytesToAddress([]byte{byte(i + 1)})
		sdb.CreateAccount(addr)
		engine.ExpireAccount(addr, uint64(i))
	}

	addrs = engine.ExpiredAddresses()
	if len(addrs) != 3 {
		t.Errorf("expected 3 expired addresses, got %d", len(addrs))
	}
}

func TestExpiryEngine_GetExpiredRecord_DeepCopy(t *testing.T) {
	sdb := NewMemoryStateDB()
	addr := types.BytesToAddress([]byte{0x01})
	sdb.CreateAccount(addr)
	sdb.AddBalance(addr, big.NewInt(100))

	engine := NewExpiryEngine(DefaultExpiryEngineConfig(), sdb)
	engine.ExpireAccount(addr, 5)

	rec := engine.GetExpiredRecord(addr)
	if rec == nil {
		t.Fatal("record is nil")
	}

	// Mutate the copy.
	rec.balance.SetInt64(9999)
	rec.nonce = 9999

	// Original should be unchanged.
	orig := engine.GetExpiredRecord(addr)
	if orig.balance.Int64() != 100 {
		t.Errorf("balance was mutated: got %d, want 100", orig.balance.Int64())
	}
	if orig.nonce != 0 {
		t.Errorf("nonce was mutated: got %d, want 0", orig.nonce)
	}
}

func TestExpiryEngine_GetExpiredRecord_Nil(t *testing.T) {
	sdb := NewMemoryStateDB()
	engine := NewExpiryEngine(DefaultExpiryEngineConfig(), sdb)

	if engine.GetExpiredRecord(types.BytesToAddress([]byte{0xFF})) != nil {
		t.Error("should return nil for non-expired address")
	}
}

func TestExpiryEngine_Concurrent(t *testing.T) {
	sdb := NewMemoryStateDB()
	n := 50
	for i := 0; i < n; i++ {
		addr := types.BytesToAddress([]byte{byte(i)})
		sdb.CreateAccount(addr)
		sdb.SetNonce(addr, uint64(i))
	}

	engine := NewExpiryEngine(DefaultExpiryEngineConfig(), sdb)

	var wg sync.WaitGroup
	wg.Add(n)
	for i := 0; i < n; i++ {
		go func(idx int) {
			defer wg.Done()
			addr := types.BytesToAddress([]byte{byte(idx)})
			engine.ExpireAccount(addr, uint64(idx))
		}(i)
	}
	wg.Wait()

	stats := engine.GetStats()
	if stats.ExpiredCount != n {
		t.Errorf("ExpiredCount = %d, want %d", stats.ExpiredCount, n)
	}

	// Concurrent reads.
	wg.Add(n)
	for i := 0; i < n; i++ {
		go func(idx int) {
			defer wg.Done()
			addr := types.BytesToAddress([]byte{byte(idx)})
			engine.IsExpired(addr)
			engine.GetExpiredRecord(addr)
			engine.GetStats()
		}(i)
	}
	wg.Wait()
}

func TestExpiryEngine_ResurrectRestoresNonceAndBalance(t *testing.T) {
	sdb := NewMemoryStateDB()
	addr := types.BytesToAddress([]byte{0x01})
	sdb.CreateAccount(addr)
	sdb.SetNonce(addr, 42)
	sdb.AddBalance(addr, big.NewInt(1_000_000))

	engine := NewExpiryEngine(DefaultExpiryEngineConfig(), sdb)

	proof := buildResurrectionProof(
		addr, 42, big.NewInt(1_000_000), types.EmptyCodeHash.Bytes(),
		nil, nil, 10,
	)

	if err := engine.ExpireAccountWithRoot(addr, 10, proof.StateRoot); err != nil {
		t.Fatalf("ExpireAccountWithRoot: %v", err)
	}

	// Simulate account being removed from state.
	sdb.CreateAccount(addr)

	if err := engine.ResurrectWithWitness(proof); err != nil {
		t.Fatalf("ResurrectWithWitness: %v", err)
	}

	if sdb.GetNonce(addr) != 42 {
		t.Errorf("nonce = %d, want 42", sdb.GetNonce(addr))
	}
	if sdb.GetBalance(addr).Int64() != 1_000_000 {
		t.Errorf("balance = %d, want 1000000", sdb.GetBalance(addr).Int64())
	}
}

func TestExpiryEngine_DoubleResurrect(t *testing.T) {
	sdb := NewMemoryStateDB()
	addr := types.BytesToAddress([]byte{0x01})
	sdb.CreateAccount(addr)

	engine := NewExpiryEngine(DefaultExpiryEngineConfig(), sdb)

	proof := buildResurrectionProof(
		addr, 0, big.NewInt(0), types.EmptyCodeHash.Bytes(),
		nil, nil, 10,
	)

	if err := engine.ExpireAccountWithRoot(addr, 10, proof.StateRoot); err != nil {
		t.Fatalf("ExpireAccountWithRoot: %v", err)
	}

	// First resurrection succeeds.
	if err := engine.ResurrectWithWitness(proof); err != nil {
		t.Fatalf("first resurrect: %v", err)
	}

	// Second resurrection fails (no longer expired).
	if err := engine.ResurrectWithWitness(proof); err != errUnknownExpired {
		t.Errorf("second resurrect: got %v, want errUnknownExpired", err)
	}
}

func TestExpiryEngine_EpochManagerIntegration(t *testing.T) {
	sdb := NewMemoryStateDB()
	engine := NewExpiryEngine(DefaultExpiryEngineConfig(), sdb)

	em := engine.EpochManager()
	if em.CurrentEpoch() != 0 {
		t.Errorf("initial epoch = %d, want 0", em.CurrentEpoch())
	}

	em.AdvanceEpoch(10)
	if em.CurrentEpoch() != 10 {
		t.Errorf("epoch = %d, want 10", em.CurrentEpoch())
	}

	if !em.IsEligibleForExpiry(7) {
		t.Error("should be eligible for expiry")
	}
}

func TestExpiryEngine_ProofAddressMismatch(t *testing.T) {
	sdb := NewMemoryStateDB()
	addr := types.BytesToAddress([]byte{0x01})
	sdb.CreateAccount(addr)

	engine := NewExpiryEngine(DefaultExpiryEngineConfig(), sdb)
	engine.ExpireAccount(addr, 10)

	proof := ExpiryProof{
		Address:    types.BytesToAddress([]byte{0x02}),
		ProofNodes: [][]byte{{0x01}},
	}

	if err := engine.ResurrectWithWitness(proof); err != errUnknownExpired {
		t.Errorf("got %v, want errUnknownExpired", err)
	}
}

func TestExpiryProof_BalanceNil(t *testing.T) {
	sdb := NewMemoryStateDB()
	addr := types.BytesToAddress([]byte{0x01})
	sdb.CreateAccount(addr) // zero balance

	engine := NewExpiryEngine(DefaultExpiryEngineConfig(), sdb)

	proof := buildResurrectionProof(
		addr, 0, nil, types.EmptyCodeHash.Bytes(),
		nil, nil, 10,
	)

	if err := engine.ExpireAccountWithRoot(addr, 10, proof.StateRoot); err != nil {
		t.Fatalf("ExpireAccountWithRoot: %v", err)
	}

	if err := engine.ResurrectWithWitness(proof); err != nil {
		t.Fatalf("ResurrectWithWitness with nil balance: %v", err)
	}
}

func TestExpiryEngine_BalanceMismatch(t *testing.T) {
	sdb := NewMemoryStateDB()
	addr := types.BytesToAddress([]byte{0x01})
	sdb.CreateAccount(addr)
	sdb.AddBalance(addr, big.NewInt(500))

	engine := NewExpiryEngine(DefaultExpiryEngineConfig(), sdb)

	// Build proof with wrong balance (999 != 500).
	proof := buildResurrectionProof(
		addr, 0, big.NewInt(999), types.EmptyCodeHash.Bytes(),
		nil, nil, 10,
	)

	if err := engine.ExpireAccountWithRoot(addr, 10, proof.StateRoot); err != nil {
		t.Fatalf("ExpireAccountWithRoot: %v", err)
	}

	if err := engine.ResurrectWithWitness(proof); err != errProofAccountData {
		t.Errorf("got %v, want errProofAccountData", err)
	}
}

func TestContainsBytes(t *testing.T) {
	tests := []struct {
		haystack []byte
		needle   []byte
		want     bool
	}{
		{[]byte{1, 2, 3, 4}, []byte{2, 3}, true},
		{[]byte{1, 2, 3, 4}, []byte{1, 2}, true},
		{[]byte{1, 2, 3, 4}, []byte{3, 4}, true},
		{[]byte{1, 2, 3, 4}, []byte{5, 6}, false},
		{[]byte{1, 2, 3, 4}, []byte{}, true},
		{[]byte{}, []byte{1}, false},
		{nil, nil, true},
	}

	for _, tt := range tests {
		got := containsBytes(tt.haystack, tt.needle)
		if got != tt.want {
			t.Errorf("containsBytes(%v, %v) = %v, want %v",
				tt.haystack, tt.needle, got, tt.want)
		}
	}
}

func TestExpiryEngineConfig_Defaults(t *testing.T) {
	cfg := DefaultExpiryEngineConfig()
	if cfg.ExpiryThreshold != 2 {
		t.Errorf("ExpiryThreshold = %d, want 2", cfg.ExpiryThreshold)
	}
	if cfg.MaxProofNodes != 64 {
		t.Errorf("MaxProofNodes = %d, want 64", cfg.MaxProofNodes)
	}
	if cfg.MaxStorageKeys != 256 {
		t.Errorf("MaxStorageKeys = %d, want 256", cfg.MaxStorageKeys)
	}
}

func TestExpiryEngine_InvalidMerkleProof(t *testing.T) {
	sdb := NewMemoryStateDB()
	addr := types.BytesToAddress([]byte{0x01})
	sdb.CreateAccount(addr)

	engine := NewExpiryEngine(DefaultExpiryEngineConfig(), sdb)

	// Build a valid proof to get the root.
	validProof := buildResurrectionProof(
		addr, 0, big.NewInt(0), types.EmptyCodeHash.Bytes(),
		nil, nil, 10,
	)

	if err := engine.ExpireAccountWithRoot(addr, 10, validProof.StateRoot); err != nil {
		t.Fatalf("ExpireAccountWithRoot: %v", err)
	}

	// Tamper with the proof nodes to break the hash chain.
	tamperedProof := validProof
	tamperedProof.ProofNodes = [][]byte{
		{0x01, 0x02, 0x03},                              // random leaf
		{0x04, 0x05, 0x06},                              // random mid
		append([]byte("root:"), make([]byte, 32)...),     // root with wrong child
	}

	if err := engine.ResurrectWithWitness(tamperedProof); err != errProofVerifyFailed {
		t.Errorf("got %v, want errProofVerifyFailed", err)
	}
}
