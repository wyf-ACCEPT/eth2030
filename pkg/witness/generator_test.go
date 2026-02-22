package witness

import (
	"math/big"
	"testing"

	"github.com/eth2030/eth2030/core/types"
)

// mockStateReader implements StateReader for testing.
type mockStateReader struct {
	balances  map[types.Address]*big.Int
	nonces    map[types.Address]uint64
	codeHash  map[types.Address]types.Hash
	code      map[types.Address][]byte
	storage   map[types.Address]map[types.Hash]types.Hash
	exists    map[types.Address]bool
	stateRoot types.Hash
}

func newMockStateReader() *mockStateReader {
	return &mockStateReader{
		balances:  make(map[types.Address]*big.Int),
		nonces:    make(map[types.Address]uint64),
		codeHash:  make(map[types.Address]types.Hash),
		code:      make(map[types.Address][]byte),
		storage:   make(map[types.Address]map[types.Hash]types.Hash),
		exists:    make(map[types.Address]bool),
		stateRoot: types.HexToHash("0xaabb"),
	}
}

func (m *mockStateReader) createAccount(addr types.Address, balance *big.Int, nonce uint64) {
	m.balances[addr] = new(big.Int).Set(balance)
	m.nonces[addr] = nonce
	m.codeHash[addr] = types.EmptyCodeHash
	m.exists[addr] = true
}

func (m *mockStateReader) setCode(addr types.Address, c []byte, h types.Hash) {
	m.code[addr] = c
	m.codeHash[addr] = h
}

func (m *mockStateReader) setStorage(addr types.Address, key, value types.Hash) {
	if _, ok := m.storage[addr]; !ok {
		m.storage[addr] = make(map[types.Hash]types.Hash)
	}
	m.storage[addr][key] = value
}

func (m *mockStateReader) GetBalance(addr types.Address) *big.Int {
	if b, ok := m.balances[addr]; ok {
		return new(big.Int).Set(b)
	}
	return new(big.Int)
}

func (m *mockStateReader) GetNonce(addr types.Address) uint64 {
	return m.nonces[addr]
}

func (m *mockStateReader) GetCodeHash(addr types.Address) types.Hash {
	if h, ok := m.codeHash[addr]; ok {
		return h
	}
	return types.EmptyCodeHash
}

func (m *mockStateReader) GetCode(addr types.Address) []byte {
	return m.code[addr]
}

func (m *mockStateReader) GetState(addr types.Address, key types.Hash) types.Hash {
	if slots, ok := m.storage[addr]; ok {
		return slots[key]
	}
	return types.Hash{}
}

func (m *mockStateReader) Exist(addr types.Address) bool {
	return m.exists[addr]
}

func (m *mockStateReader) GetRoot() types.Hash {
	return m.stateRoot
}

// --- BeginBlock / GenerateWitness tests ---

func TestWitnessGenerator_NotStarted(t *testing.T) {
	gen := NewWitnessGenerator(DefaultGeneratorConfig())
	_, err := gen.GenerateWitness(types.Hash{})
	if err != ErrGeneratorNotStarted {
		t.Fatalf("expected ErrGeneratorNotStarted, got %v", err)
	}
}

func TestWitnessGenerator_EmptyBlock(t *testing.T) {
	gen := NewWitnessGenerator(DefaultGeneratorConfig())
	preRoot := types.HexToHash("0x1234")
	gen.BeginBlock(1, preRoot)

	postRoot := types.HexToHash("0x5678")
	w, err := gen.GenerateWitness(postRoot)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if w.BlockNumber != 1 {
		t.Fatalf("expected block 1, got %d", w.BlockNumber)
	}
	if w.ParentRoot != preRoot {
		t.Fatal("parent root mismatch")
	}
	if w.PostRoot != postRoot {
		t.Fatal("post root mismatch")
	}
	if len(w.Accounts) != 0 {
		t.Fatalf("expected 0 accounts, got %d", len(w.Accounts))
	}
}

func TestWitnessGenerator_RecordAccountRead(t *testing.T) {
	gen := NewWitnessGenerator(DefaultGeneratorConfig())
	reader := newMockStateReader()

	addr := types.HexToAddress("0xaaaa")
	reader.createAccount(addr, big.NewInt(1e18), 5)

	gen.BeginBlock(1, types.Hash{})
	gen.RecordAccountRead(addr, reader)

	w, err := gen.GenerateWitness(types.Hash{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(w.Accounts) != 1 {
		t.Fatalf("expected 1 account, got %d", len(w.Accounts))
	}
	acc := w.Accounts[addr]
	if acc == nil {
		t.Fatal("expected account for addr")
	}
	if acc.Balance.Cmp(big.NewInt(1e18)) != 0 {
		t.Fatalf("expected balance 1e18, got %s", acc.Balance.String())
	}
	if acc.Nonce != 5 {
		t.Fatalf("expected nonce 5, got %d", acc.Nonce)
	}
	if !acc.Exists {
		t.Fatal("expected account to exist")
	}
}

func TestWitnessGenerator_RecordAccountWrite(t *testing.T) {
	gen := NewWitnessGenerator(DefaultGeneratorConfig())
	reader := newMockStateReader()

	addr := types.HexToAddress("0xbbbb")
	reader.createAccount(addr, big.NewInt(500), 0)

	gen.BeginBlock(1, types.Hash{})
	gen.RecordAccountWrite(addr, reader)

	w, err := gen.GenerateWitness(types.Hash{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(w.Accounts) != 1 {
		t.Fatalf("expected 1 account, got %d", len(w.Accounts))
	}

	// Events should contain 1 AccountWrite event.
	if len(w.Events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(w.Events))
	}
	if w.Events[0].Type != AccountWrite {
		t.Fatalf("expected AccountWrite event, got %d", w.Events[0].Type)
	}
}

func TestWitnessGenerator_RecordStorageRead(t *testing.T) {
	gen := NewWitnessGenerator(DefaultGeneratorConfig())
	reader := newMockStateReader()

	addr := types.HexToAddress("0xcccc")
	reader.createAccount(addr, big.NewInt(0), 0)
	key := types.HexToHash("0x01")
	val := types.HexToHash("0xff")
	reader.setStorage(addr, key, val)

	gen.BeginBlock(1, types.Hash{})
	gen.RecordStorageRead(addr, key, reader)

	w, err := gen.GenerateWitness(types.Hash{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(w.StorageProofs) != 1 {
		t.Fatalf("expected 1 storage proof set, got %d", len(w.StorageProofs))
	}
	slots := w.StorageProofs[addr]
	if len(slots) != 1 {
		t.Fatalf("expected 1 slot, got %d", len(slots))
	}
	if slots[key] != val {
		t.Fatalf("expected value %s, got %s", val.Hex(), slots[key].Hex())
	}
}

func TestWitnessGenerator_RecordStorageWrite(t *testing.T) {
	gen := NewWitnessGenerator(DefaultGeneratorConfig())
	reader := newMockStateReader()

	addr := types.HexToAddress("0xdddd")
	reader.createAccount(addr, big.NewInt(0), 0)
	key := types.HexToHash("0x42")
	preVal := types.HexToHash("0xaa")
	reader.setStorage(addr, key, preVal)

	gen.BeginBlock(1, types.Hash{})
	gen.RecordStorageWrite(addr, key, reader)

	w, err := gen.GenerateWitness(types.Hash{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Pre-state value should be captured.
	if w.StorageProofs[addr][key] != preVal {
		t.Fatalf("expected pre-state value %s, got %s", preVal.Hex(), w.StorageProofs[addr][key].Hex())
	}
}

func TestWitnessGenerator_RecordCodeRead(t *testing.T) {
	gen := NewWitnessGenerator(DefaultGeneratorConfig())
	reader := newMockStateReader()

	addr := types.HexToAddress("0xeeee")
	reader.createAccount(addr, big.NewInt(0), 0)
	codeHash := types.HexToHash("0xc0de")
	code := []byte{0x60, 0x00, 0x60, 0x00, 0xFD} // PUSH0 PUSH0 REVERT
	reader.setCode(addr, code, codeHash)

	gen.BeginBlock(1, types.Hash{})
	gen.RecordCodeRead(addr, reader)

	w, err := gen.GenerateWitness(types.Hash{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(w.CodeChunks) != 1 {
		t.Fatalf("expected 1 code chunk, got %d", len(w.CodeChunks))
	}
	if _, ok := w.CodeChunks[codeHash]; !ok {
		t.Fatal("expected code chunk for codeHash")
	}
}

func TestWitnessGenerator_CodeReadSkipsEOA(t *testing.T) {
	gen := NewWitnessGenerator(DefaultGeneratorConfig())
	reader := newMockStateReader()

	addr := types.HexToAddress("0xf000")
	reader.createAccount(addr, big.NewInt(1e18), 0)
	// EOA: code hash = empty code hash, no code set.

	gen.BeginBlock(1, types.Hash{})
	gen.RecordCodeRead(addr, reader)

	w, err := gen.GenerateWitness(types.Hash{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(w.CodeChunks) != 0 {
		t.Fatalf("expected 0 code chunks for EOA, got %d", len(w.CodeChunks))
	}
}

func TestWitnessGenerator_DuplicateAccountAccess(t *testing.T) {
	gen := NewWitnessGenerator(DefaultGeneratorConfig())
	reader := newMockStateReader()

	addr := types.HexToAddress("0xaaaa")
	reader.createAccount(addr, big.NewInt(100), 1)

	gen.BeginBlock(1, types.Hash{})

	// Access the same account multiple times.
	gen.RecordAccountRead(addr, reader)
	gen.RecordAccountRead(addr, reader)
	gen.RecordAccountWrite(addr, reader)

	w, err := gen.GenerateWitness(types.Hash{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Only 1 account entry despite multiple accesses.
	if len(w.Accounts) != 1 {
		t.Fatalf("expected 1 account, got %d", len(w.Accounts))
	}
	// But 3 events should be recorded.
	if len(w.Events) != 3 {
		t.Fatalf("expected 3 events, got %d", len(w.Events))
	}
}

func TestWitnessGenerator_DuplicateStorageAccess(t *testing.T) {
	gen := NewWitnessGenerator(DefaultGeneratorConfig())
	reader := newMockStateReader()

	addr := types.HexToAddress("0xbbbb")
	reader.createAccount(addr, big.NewInt(0), 0)
	key := types.HexToHash("0x01")
	reader.setStorage(addr, key, types.HexToHash("0xff"))

	gen.BeginBlock(1, types.Hash{})
	gen.RecordStorageRead(addr, key, reader)
	gen.RecordStorageRead(addr, key, reader) // duplicate

	if gen.StorageKeyCount() != 1 {
		t.Fatalf("expected 1 unique storage key, got %d", gen.StorageKeyCount())
	}
}

func TestWitnessGenerator_NonExistentAccount(t *testing.T) {
	gen := NewWitnessGenerator(DefaultGeneratorConfig())
	reader := newMockStateReader()

	addr := types.HexToAddress("0xdead")
	// Account does not exist in reader.

	gen.BeginBlock(1, types.Hash{})
	gen.RecordAccountRead(addr, reader)

	w, err := gen.GenerateWitness(types.Hash{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	acc := w.Accounts[addr]
	if acc == nil {
		t.Fatal("expected account entry even for non-existent")
	}
	if acc.Exists {
		t.Fatal("expected account to not exist")
	}
	if acc.Balance.Sign() != 0 {
		t.Fatal("expected zero balance for non-existent account")
	}
}

func TestWitnessGenerator_MultipleAccounts(t *testing.T) {
	gen := NewWitnessGenerator(DefaultGeneratorConfig())
	reader := newMockStateReader()

	addr1 := types.HexToAddress("0x0001")
	addr2 := types.HexToAddress("0x0002")
	addr3 := types.HexToAddress("0x0003")
	reader.createAccount(addr1, big.NewInt(100), 0)
	reader.createAccount(addr2, big.NewInt(200), 1)
	reader.createAccount(addr3, big.NewInt(300), 2)

	gen.BeginBlock(1, types.Hash{})
	gen.RecordAccountRead(addr1, reader)
	gen.RecordAccountRead(addr2, reader)
	gen.RecordAccountRead(addr3, reader)

	if gen.AccountCount() != 3 {
		t.Fatalf("expected 3 accounts, got %d", gen.AccountCount())
	}
}

// --- EstimateWitnessSize tests ---

func TestWitnessGenerator_EstimateSize(t *testing.T) {
	gen := NewWitnessGenerator(DefaultGeneratorConfig())
	reader := newMockStateReader()

	addr := types.HexToAddress("0xaaaa")
	reader.createAccount(addr, big.NewInt(1e18), 0)

	gen.BeginBlock(1, types.Hash{})
	gen.RecordAccountRead(addr, reader)

	size := gen.EstimateWitnessSize()
	if size <= 0 {
		t.Fatal("expected positive estimated size")
	}
}

func TestEstimateGeneratedWitnessSize_Nil(t *testing.T) {
	size := EstimateGeneratedWitnessSize(nil)
	if size != 0 {
		t.Fatalf("expected 0, got %d", size)
	}
}

// --- ValidateWitnessRoots tests ---

func TestValidateWitnessRoots_Matching(t *testing.T) {
	postRoot := types.HexToHash("0xabcd")
	w := &GeneratedWitness{
		PostRoot:  postRoot,
		ProofData: make(map[types.Hash][]byte),
	}
	err := ValidateWitnessRoots(w, postRoot)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestValidateWitnessRoots_Mismatch(t *testing.T) {
	w := &GeneratedWitness{
		PostRoot:  types.HexToHash("0xaaaa"),
		ProofData: make(map[types.Hash][]byte),
	}
	err := ValidateWitnessRoots(w, types.HexToHash("0xbbbb"))
	if err == nil {
		t.Fatal("expected error for root mismatch")
	}
}

func TestValidateWitnessRoots_Nil(t *testing.T) {
	err := ValidateWitnessRoots(nil, types.Hash{})
	if err == nil {
		t.Fatal("expected error for nil witness")
	}
}

// --- WitnessCompressor tests ---

func TestWitnessCompressor_CompressDecompress(t *testing.T) {
	gen := NewWitnessGenerator(DefaultGeneratorConfig())
	reader := newMockStateReader()

	addr1 := types.HexToAddress("0xaaaa")
	addr2 := types.HexToAddress("0xbbbb")
	reader.createAccount(addr1, big.NewInt(1000), 5)
	reader.createAccount(addr2, big.NewInt(2000), 10)
	key := types.HexToHash("0x42")
	val := types.HexToHash("0xff")
	reader.setStorage(addr1, key, val)

	gen.BeginBlock(42, types.HexToHash("0xdead"))
	gen.RecordAccountRead(addr1, reader)
	gen.RecordAccountRead(addr2, reader)
	gen.RecordStorageRead(addr1, key, reader)

	postRoot := types.HexToHash("0xbeef")
	w, err := gen.GenerateWitness(postRoot)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	comp := NewWitnessCompressor()
	cw := comp.Compress(w)

	if cw == nil {
		t.Fatal("expected non-nil compressed witness")
	}
	if cw.BlockNumber != 42 {
		t.Fatalf("expected block 42, got %d", cw.BlockNumber)
	}
	if len(cw.UniqueAddresses) != 2 {
		t.Fatalf("expected 2 unique addresses, got %d", len(cw.UniqueAddresses))
	}
	if cw.CompressedSize > cw.OriginalSize {
		// Compressed should be smaller or equal due to address dedup.
		t.Logf("compressed %d, original %d (compression not always guaranteed for tiny data)",
			cw.CompressedSize, cw.OriginalSize)
	}

	// Decompress and verify round-trip.
	recovered := comp.Decompress(cw)
	if recovered == nil {
		t.Fatal("expected non-nil decompressed witness")
	}
	if recovered.BlockNumber != w.BlockNumber {
		t.Fatalf("block number mismatch: %d vs %d", recovered.BlockNumber, w.BlockNumber)
	}
	if len(recovered.Accounts) != len(w.Accounts) {
		t.Fatalf("account count mismatch: %d vs %d", len(recovered.Accounts), len(w.Accounts))
	}
	for addr, origAcc := range w.Accounts {
		recAcc := recovered.Accounts[addr]
		if recAcc == nil {
			t.Fatalf("missing account %s in decompressed witness", addr.Hex())
		}
		if recAcc.Balance.Cmp(origAcc.Balance) != 0 {
			t.Fatalf("balance mismatch for %s: %s vs %s",
				addr.Hex(), recAcc.Balance.String(), origAcc.Balance.String())
		}
		if recAcc.Nonce != origAcc.Nonce {
			t.Fatalf("nonce mismatch for %s", addr.Hex())
		}
	}
	// Verify storage round-trip.
	for addr, origKeys := range w.StorageProofs {
		recKeys := recovered.StorageProofs[addr]
		if recKeys == nil {
			t.Fatalf("missing storage proofs for %s", addr.Hex())
		}
		for k, v := range origKeys {
			if recKeys[k] != v {
				t.Fatalf("storage value mismatch for %s/%s", addr.Hex(), k.Hex())
			}
		}
	}
}

func TestWitnessCompressor_NilWitness(t *testing.T) {
	comp := NewWitnessCompressor()
	cw := comp.Compress(nil)
	if cw != nil {
		t.Fatal("expected nil for nil input")
	}
	w := comp.Decompress(nil)
	if w != nil {
		t.Fatal("expected nil for nil input")
	}
}

// --- Reset tests ---

func TestWitnessGenerator_Reset(t *testing.T) {
	gen := NewWitnessGenerator(DefaultGeneratorConfig())
	reader := newMockStateReader()

	addr := types.HexToAddress("0xaaaa")
	reader.createAccount(addr, big.NewInt(100), 0)

	gen.BeginBlock(1, types.Hash{})
	gen.RecordAccountRead(addr, reader)

	if gen.AccountCount() != 1 {
		t.Fatal("expected 1 account before reset")
	}

	gen.Reset()

	if gen.IsStarted() {
		t.Fatal("expected not started after reset")
	}
	if gen.AccountCount() != 0 {
		t.Fatal("expected 0 accounts after reset")
	}
}

// --- ProofData tests ---

func TestWitnessGenerator_ProofDataGenerated(t *testing.T) {
	gen := NewWitnessGenerator(DefaultGeneratorConfig())
	reader := newMockStateReader()

	addr := types.HexToAddress("0xaaaa")
	reader.createAccount(addr, big.NewInt(1e18), 42)
	key := types.HexToHash("0x01")
	val := types.HexToHash("0xbeef")
	reader.setStorage(addr, key, val)

	gen.BeginBlock(1, types.Hash{})
	gen.RecordAccountRead(addr, reader)
	gen.RecordStorageRead(addr, key, reader)

	w, err := gen.GenerateWitness(types.Hash{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Proof data should contain at least account + storage proof nodes.
	if len(w.ProofData) < 2 {
		t.Fatalf("expected at least 2 proof nodes, got %d", len(w.ProofData))
	}

	// Validate proof nodes are self-consistent.
	err = ValidateWitnessRoots(w, w.PostRoot)
	if err != nil {
		t.Fatalf("proof validation failed: %v", err)
	}
}

// --- Thread safety test ---

func TestWitnessGenerator_ThreadSafety(t *testing.T) {
	gen := NewWitnessGenerator(DefaultGeneratorConfig())
	reader := newMockStateReader()

	for i := 0; i < 20; i++ {
		addr := types.BytesToAddress([]byte{byte(i + 1)})
		reader.createAccount(addr, big.NewInt(int64(i*100)), uint64(i))
		key := types.BytesToHash([]byte{byte(i)})
		reader.setStorage(addr, key, types.BytesToHash([]byte{byte(i + 50)}))
	}

	gen.BeginBlock(1, types.Hash{})

	done := make(chan struct{}, 20)
	for i := 0; i < 20; i++ {
		go func(idx int) {
			defer func() { done <- struct{}{} }()
			addr := types.BytesToAddress([]byte{byte(idx + 1)})
			key := types.BytesToHash([]byte{byte(idx)})
			gen.RecordAccountRead(addr, reader)
			gen.RecordStorageRead(addr, key, reader)
		}(i)
	}
	for i := 0; i < 20; i++ {
		<-done
	}

	if gen.AccountCount() != 20 {
		t.Fatalf("expected 20 accounts, got %d", gen.AccountCount())
	}
	if gen.StorageKeyCount() != 20 {
		t.Fatalf("expected 20 storage keys, got %d", gen.StorageKeyCount())
	}

	w, err := gen.GenerateWitness(types.Hash{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(w.Accounts) != 20 {
		t.Fatalf("expected 20 accounts in witness, got %d", len(w.Accounts))
	}
}

// --- MaxWitnessSize tests ---

func TestWitnessGenerator_MaxSizeLimit(t *testing.T) {
	config := DefaultGeneratorConfig()
	config.MaxWitnessSize = 100 // very small limit
	gen := NewWitnessGenerator(config)
	reader := newMockStateReader()

	// Create many accounts to exceed the limit.
	gen.BeginBlock(1, types.Hash{})
	for i := 0; i < 50; i++ {
		addr := types.BytesToAddress([]byte{byte(i + 1)})
		reader.createAccount(addr, big.NewInt(int64(i*1e15)), uint64(i))
		gen.RecordAccountRead(addr, reader)
	}

	_, err := gen.GenerateWitness(types.Hash{})
	if err == nil {
		t.Fatal("expected error for witness too large")
	}
}

// --- EventCount test ---

func TestWitnessGenerator_EventCount(t *testing.T) {
	gen := NewWitnessGenerator(DefaultGeneratorConfig())
	reader := newMockStateReader()

	addr := types.HexToAddress("0xaaaa")
	reader.createAccount(addr, big.NewInt(100), 0)
	key := types.HexToHash("0x01")
	reader.setStorage(addr, key, types.HexToHash("0xff"))

	gen.BeginBlock(1, types.Hash{})
	gen.RecordAccountRead(addr, reader)
	gen.RecordStorageRead(addr, key, reader)
	gen.RecordStorageWrite(addr, key, reader)
	gen.RecordCodeRead(addr, reader) // EOA, no code — event skipped

	if gen.EventCount() != 3 {
		t.Fatalf("expected 3 events, got %d", gen.EventCount())
	}
}

// --- Events disabled test ---

func TestWitnessGenerator_EventsDisabled(t *testing.T) {
	config := DefaultGeneratorConfig()
	config.CollectEvents = false
	gen := NewWitnessGenerator(config)
	reader := newMockStateReader()

	addr := types.HexToAddress("0xaaaa")
	reader.createAccount(addr, big.NewInt(100), 0)

	gen.BeginBlock(1, types.Hash{})
	gen.RecordAccountRead(addr, reader)
	gen.RecordAccountWrite(addr, reader)

	w, err := gen.GenerateWitness(types.Hash{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(w.Events) != 0 {
		t.Fatalf("expected 0 events with CollectEvents=false, got %d", len(w.Events))
	}
}
