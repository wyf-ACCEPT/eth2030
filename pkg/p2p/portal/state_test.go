package portal

import (
	"bytes"
	"math/big"
	"sync"
	"testing"

	"github.com/eth2028/eth2028/core/types"
	"github.com/eth2028/eth2028/crypto"
)

// --- ContentType constants ---

func TestStateContentTypeConstants(t *testing.T) {
	// Verify content type selectors are distinct and in the state namespace.
	types_ := []byte{
		ContentTypeAccountTrieNode,
		ContentTypeContractStorageTrieNode,
		ContentTypeContractBytecode,
		ContentTypeAccountProof,
	}
	seen := make(map[byte]bool)
	for _, ct := range types_ {
		if seen[ct] {
			t.Fatalf("duplicate content type: 0x%02x", ct)
		}
		seen[ct] = true
		// State types use 0x20+ range, distinct from history (0x00-0x03).
		if ct < 0x20 {
			t.Fatalf("state content type 0x%02x overlaps with history range", ct)
		}
	}
}

// --- StateContentKey encode/decode ---

func TestAccountTrieNodeKeyRoundTrip(t *testing.T) {
	addrHash := types.HexToHash("0xaaaa")
	key := StateContentKey{
		ContentType: ContentTypeAccountTrieNode,
		AddressHash: addrHash,
	}
	encoded := key.Encode()
	if encoded[0] != ContentTypeAccountTrieNode {
		t.Fatalf("type = 0x%02x, want 0x%02x", encoded[0], ContentTypeAccountTrieNode)
	}
	if len(encoded) != 1+types.HashLength {
		t.Fatalf("encoded len = %d, want %d", len(encoded), 1+types.HashLength)
	}

	decoded, err := DecodeStateContentKey(encoded)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if decoded.ContentType != ContentTypeAccountTrieNode {
		t.Fatal("decoded type mismatch")
	}
	if decoded.AddressHash != addrHash {
		t.Fatal("decoded address hash mismatch")
	}
}

func TestAccountProofKeyRoundTrip(t *testing.T) {
	addrHash := types.HexToHash("0xbbbb")
	key := StateContentKey{
		ContentType: ContentTypeAccountProof,
		AddressHash: addrHash,
	}
	encoded := key.Encode()
	decoded, err := DecodeStateContentKey(encoded)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if decoded.ContentType != ContentTypeAccountProof {
		t.Fatal("decoded type mismatch")
	}
	if decoded.AddressHash != addrHash {
		t.Fatal("decoded address hash mismatch")
	}
}

func TestStorageTrieNodeKeyRoundTrip(t *testing.T) {
	addrHash := types.HexToHash("0xcccc")
	slot := types.HexToHash("0xdddd")
	key := StateContentKey{
		ContentType: ContentTypeContractStorageTrieNode,
		AddressHash: addrHash,
		Slot:        slot,
	}
	encoded := key.Encode()
	if len(encoded) != 1+2*types.HashLength {
		t.Fatalf("encoded len = %d, want %d", len(encoded), 1+2*types.HashLength)
	}

	decoded, err := DecodeStateContentKey(encoded)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if decoded.ContentType != ContentTypeContractStorageTrieNode {
		t.Fatal("decoded type mismatch")
	}
	if decoded.AddressHash != addrHash {
		t.Fatal("decoded address hash mismatch")
	}
	if decoded.Slot != slot {
		t.Fatal("decoded slot mismatch")
	}
}

func TestContractBytecodeKeyRoundTrip(t *testing.T) {
	addrHash := types.HexToHash("0xeeee")
	codeHash := types.HexToHash("0xffff")
	key := StateContentKey{
		ContentType: ContentTypeContractBytecode,
		AddressHash: addrHash,
		CodeHash:    codeHash,
	}
	encoded := key.Encode()
	if len(encoded) != 1+2*types.HashLength {
		t.Fatalf("encoded len = %d, want %d", len(encoded), 1+2*types.HashLength)
	}

	decoded, err := DecodeStateContentKey(encoded)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if decoded.ContentType != ContentTypeContractBytecode {
		t.Fatal("decoded type mismatch")
	}
	if decoded.AddressHash != addrHash {
		t.Fatal("decoded address hash mismatch")
	}
	if decoded.CodeHash != codeHash {
		t.Fatal("decoded code hash mismatch")
	}
}

func TestDecodeStateContentKeyErrors(t *testing.T) {
	// Empty.
	if _, err := DecodeStateContentKey(nil); err != ErrInvalidStateKey {
		t.Fatalf("nil: got %v, want ErrInvalidStateKey", err)
	}
	if _, err := DecodeStateContentKey([]byte{}); err != ErrInvalidStateKey {
		t.Fatalf("empty: got %v, want ErrInvalidStateKey", err)
	}

	// Unknown type.
	bad := make([]byte, 1+types.HashLength)
	bad[0] = 0xFF
	if _, err := DecodeStateContentKey(bad); err != ErrInvalidStateKey {
		t.Fatalf("unknown type: got %v, want ErrInvalidStateKey", err)
	}

	// Account type too short.
	short := []byte{ContentTypeAccountProof, 0x01}
	if _, err := DecodeStateContentKey(short); err != ErrInvalidStateKey {
		t.Fatalf("short account key: got %v, want ErrInvalidStateKey", err)
	}

	// Storage type too short (missing slot).
	shortStorage := make([]byte, 1+types.HashLength)
	shortStorage[0] = ContentTypeContractStorageTrieNode
	if _, err := DecodeStateContentKey(shortStorage); err != ErrInvalidStateKey {
		t.Fatalf("short storage key: got %v, want ErrInvalidStateKey", err)
	}
}

func TestStateContentKeyEncodeUnknownType(t *testing.T) {
	key := StateContentKey{ContentType: 0xFF}
	if key.Encode() != nil {
		t.Fatal("unknown content type should encode to nil")
	}
}

// --- Content key uniqueness ---

func TestDifferentStateContentTypesProduceDifferentIDs(t *testing.T) {
	addrHash := types.HexToHash("0x1111")
	slot := types.HexToHash("0x2222")

	accountKey := StateContentKey{
		ContentType: ContentTypeAccountProof,
		AddressHash: addrHash,
	}
	storageKey := StateContentKey{
		ContentType: ContentTypeContractStorageTrieNode,
		AddressHash: addrHash,
		Slot:        slot,
	}
	codeKey := StateContentKey{
		ContentType: ContentTypeContractBytecode,
		AddressHash: addrHash,
		CodeHash:    slot,
	}

	id1 := ComputeContentID(accountKey.Encode())
	id2 := ComputeContentID(storageKey.Encode())
	id3 := ComputeContentID(codeKey.Encode())

	if id1 == id2 {
		t.Fatal("account and storage content IDs should differ")
	}
	if id1 == id3 {
		t.Fatal("account and code content IDs should differ")
	}
	if id2 == id3 {
		t.Fatal("storage and code content IDs should differ")
	}
}

// --- Key helper functions ---

func TestMakeAccountProofKey(t *testing.T) {
	addr := types.HexToAddress("0xdead")
	key := MakeAccountProofKey(addr)
	if key.ContentType != ContentTypeAccountProof {
		t.Fatal("wrong content type")
	}
	expectedHash := crypto.Keccak256Hash(addr[:])
	if key.AddressHash != expectedHash {
		t.Fatal("address hash mismatch")
	}
}

func TestMakeStorageProofKey(t *testing.T) {
	addr := types.HexToAddress("0xbeef")
	slot := types.HexToHash("0x01")
	key := MakeStorageProofKey(addr, slot)
	if key.ContentType != ContentTypeContractStorageTrieNode {
		t.Fatal("wrong content type")
	}
	expectedAddr := crypto.Keccak256Hash(addr[:])
	expectedSlot := crypto.Keccak256Hash(slot[:])
	if key.AddressHash != expectedAddr {
		t.Fatal("address hash mismatch")
	}
	if key.Slot != expectedSlot {
		t.Fatal("slot hash mismatch")
	}
}

func TestMakeContractCodeKey(t *testing.T) {
	addr := types.HexToAddress("0xcafe")
	codeHash := types.HexToHash("0xc0de")
	key := MakeContractCodeKey(addr, codeHash)
	if key.ContentType != ContentTypeContractBytecode {
		t.Fatal("wrong content type")
	}
	if key.CodeHash != codeHash {
		t.Fatal("code hash mismatch")
	}
}

// --- StateNetwork lifecycle ---

func newTestStateNetwork() (*StateNetwork, *MemoryStore) {
	var self [32]byte
	rt := NewRoutingTable(self)
	store := NewMemoryStore(1 << 20) // 1 MiB
	config := DefaultStateNetworkConfig()
	sn := NewStateNetwork(config, rt, store)
	return sn, store
}

func TestStateNetworkStartStop(t *testing.T) {
	sn, _ := newTestStateNetwork()
	if sn.isStarted() {
		t.Fatal("should not be started initially")
	}
	sn.Start()
	if !sn.isStarted() {
		t.Fatal("should be started after Start()")
	}
	sn.Stop()
	if sn.isStarted() {
		t.Fatal("should not be started after Stop()")
	}
}

func TestStateNetworkNotStarted(t *testing.T) {
	sn, _ := newTestStateNetwork()
	addr := types.HexToAddress("0x01")
	slot := types.HexToHash("0x02")
	codeHash := types.HexToHash("0x03")

	if _, err := sn.GetAccountProof(addr); err != ErrStateNotStarted {
		t.Fatalf("GetAccountProof: got %v, want ErrStateNotStarted", err)
	}
	if _, err := sn.GetStorageProof(addr, slot); err != ErrStateNotStarted {
		t.Fatalf("GetStorageProof: got %v, want ErrStateNotStarted", err)
	}
	if _, err := sn.GetContractCode(codeHash); err != ErrStateNotStarted {
		t.Fatalf("GetContractCode: got %v, want ErrStateNotStarted", err)
	}
}

// --- StoreContent / FindContent ---

func TestStateNetworkStoreAndFind(t *testing.T) {
	sn, _ := newTestStateNetwork()

	addrHash := types.HexToHash("0x1234")
	key := StateContentKey{
		ContentType: ContentTypeAccountTrieNode,
		AddressHash: addrHash,
	}
	data := []byte("trie node data")

	if err := sn.StoreContent(key, data); err != nil {
		t.Fatalf("StoreContent: %v", err)
	}

	got, err := sn.FindContent(key)
	if err != nil {
		t.Fatalf("FindContent: %v", err)
	}
	if !bytes.Equal(got, data) {
		t.Fatalf("data mismatch: got %q, want %q", got, data)
	}
}

func TestStateNetworkStoreEmptyPayload(t *testing.T) {
	sn, _ := newTestStateNetwork()
	key := StateContentKey{
		ContentType: ContentTypeAccountTrieNode,
		AddressHash: types.HexToHash("0xaa"),
	}
	if err := sn.StoreContent(key, nil); err != ErrEmptyStatePayload {
		t.Fatalf("nil data: got %v, want ErrEmptyStatePayload", err)
	}
	if err := sn.StoreContent(key, []byte{}); err != ErrEmptyStatePayload {
		t.Fatalf("empty data: got %v, want ErrEmptyStatePayload", err)
	}
}

func TestStateNetworkStoreContentTooLarge(t *testing.T) {
	var self [32]byte
	rt := NewRoutingTable(self)
	store := NewMemoryStore(1 << 20)
	config := StateNetworkConfig{
		MaxContentSize: 100,
		ContentRadius:  MaxRadius(),
	}
	sn := NewStateNetwork(config, rt, store)

	key := StateContentKey{
		ContentType: ContentTypeAccountTrieNode,
		AddressHash: types.HexToHash("0xbb"),
	}
	bigData := make([]byte, 200)
	if err := sn.StoreContent(key, bigData); err != ErrStateContentTooLarge {
		t.Fatalf("got %v, want ErrStateContentTooLarge", err)
	}
}

func TestStateNetworkStoreInvalidKey(t *testing.T) {
	sn, _ := newTestStateNetwork()
	badKey := StateContentKey{ContentType: 0xFF}
	if err := sn.StoreContent(badKey, []byte("data")); err != ErrInvalidStateKey {
		t.Fatalf("got %v, want ErrInvalidStateKey", err)
	}
}

func TestStateNetworkFindContentNotFound(t *testing.T) {
	sn, _ := newTestStateNetwork()
	key := StateContentKey{
		ContentType: ContentTypeAccountProof,
		AddressHash: types.HexToHash("0xnonexistent"),
	}
	_, err := sn.FindContent(key)
	if err != ErrStateContentNotFound {
		t.Fatalf("got %v, want ErrStateContentNotFound", err)
	}
}

func TestStateNetworkFindContentInvalidKey(t *testing.T) {
	sn, _ := newTestStateNetwork()
	badKey := StateContentKey{ContentType: 0xFF}
	_, err := sn.FindContent(badKey)
	if err != ErrInvalidStateKey {
		t.Fatalf("got %v, want ErrInvalidStateKey", err)
	}
}

// --- Account proof encode/decode ---

func TestAccountProofRoundTrip(t *testing.T) {
	addr := types.HexToAddress("0xdeadbeef")
	proof := &AccountProofResult{
		Address:     addr,
		Balance:     big.NewInt(1000000),
		Nonce:       42,
		CodeHash:    types.EmptyCodeHash,
		StorageHash: types.EmptyRootHash,
		Proof: [][]byte{
			{0x01, 0x02, 0x03},
			{0x04, 0x05},
		},
	}

	encoded := EncodeAccountProof(proof)
	decoded, err := decodeAccountProof(addr, encoded)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}

	if decoded.Address != addr {
		t.Fatal("address mismatch")
	}
	if decoded.Nonce != 42 {
		t.Fatalf("nonce = %d, want 42", decoded.Nonce)
	}
	if decoded.Balance.Cmp(big.NewInt(1000000)) != 0 {
		t.Fatalf("balance = %v, want 1000000", decoded.Balance)
	}
	if decoded.CodeHash != types.EmptyCodeHash {
		t.Fatal("code hash mismatch")
	}
	if decoded.StorageHash != types.EmptyRootHash {
		t.Fatal("storage hash mismatch")
	}
	if len(decoded.Proof) != 2 {
		t.Fatalf("proof len = %d, want 2", len(decoded.Proof))
	}
	if !bytes.Equal(decoded.Proof[0], []byte{0x01, 0x02, 0x03}) {
		t.Fatal("proof[0] mismatch")
	}
	if !bytes.Equal(decoded.Proof[1], []byte{0x04, 0x05}) {
		t.Fatal("proof[1] mismatch")
	}
}

func TestAccountProofDecodeErrors(t *testing.T) {
	addr := types.HexToAddress("0x01")
	// Too short.
	if _, err := decodeAccountProof(addr, []byte{0x01}); err == nil {
		t.Fatal("expected error for short data")
	}
}

func TestAccountProofZeroBalance(t *testing.T) {
	proof := &AccountProofResult{
		Balance:     big.NewInt(0),
		Nonce:       0,
		CodeHash:    types.EmptyCodeHash,
		StorageHash: types.EmptyRootHash,
		Proof:       nil,
	}
	encoded := EncodeAccountProof(proof)
	decoded, err := decodeAccountProof(types.Address{}, encoded)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if decoded.Balance.Sign() != 0 {
		t.Fatalf("balance = %v, want 0", decoded.Balance)
	}
}

// --- Storage proof encode/decode ---

func TestStorageProofRoundTrip(t *testing.T) {
	slot := types.HexToHash("0x01")
	value := types.HexToHash("0x0000000000000000000000000000000000000000000000000000000000000064")
	proof := &StorageProofResult{
		Key:   slot,
		Value: value,
		Proof: [][]byte{
			{0xaa, 0xbb},
			{0xcc, 0xdd, 0xee},
		},
	}

	encoded := EncodeStorageProof(proof)
	decoded, err := decodeStorageProof(slot, encoded)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if decoded.Key != slot {
		t.Fatal("key mismatch")
	}
	if decoded.Value != value {
		t.Fatal("value mismatch")
	}
	if len(decoded.Proof) != 2 {
		t.Fatalf("proof len = %d, want 2", len(decoded.Proof))
	}
}

func TestStorageProofDecodeErrors(t *testing.T) {
	slot := types.HexToHash("0x01")
	if _, err := decodeStorageProof(slot, []byte{0x01}); err == nil {
		t.Fatal("expected error for short data")
	}
}

// --- GetAccountProof / GetStorageProof / GetContractCode integration ---

func TestGetAccountProofIntegration(t *testing.T) {
	sn, _ := newTestStateNetwork()
	sn.Start()
	defer sn.Stop()

	addr := types.HexToAddress("0xdeadbeefdeadbeef")
	proof := &AccountProofResult{
		Address:     addr,
		Balance:     big.NewInt(5000),
		Nonce:       10,
		CodeHash:    types.EmptyCodeHash,
		StorageHash: types.EmptyRootHash,
		Proof:       [][]byte{{0x01}},
	}

	key := MakeAccountProofKey(addr)
	encoded := EncodeAccountProof(proof)
	if err := sn.StoreContent(key, encoded); err != nil {
		t.Fatalf("store: %v", err)
	}

	got, err := sn.GetAccountProof(addr)
	if err != nil {
		t.Fatalf("GetAccountProof: %v", err)
	}
	if got.Nonce != 10 {
		t.Fatalf("nonce = %d, want 10", got.Nonce)
	}
	if got.Balance.Cmp(big.NewInt(5000)) != 0 {
		t.Fatalf("balance = %v, want 5000", got.Balance)
	}
}

func TestGetStorageProofIntegration(t *testing.T) {
	sn, _ := newTestStateNetwork()
	sn.Start()
	defer sn.Stop()

	addr := types.HexToAddress("0xcafe")
	slot := types.HexToHash("0x0a")
	value := types.HexToHash("0x64")
	proof := &StorageProofResult{
		Key:   slot,
		Value: value,
		Proof: [][]byte{{0xab}},
	}

	key := MakeStorageProofKey(addr, slot)
	encoded := EncodeStorageProof(proof)
	if err := sn.StoreContent(key, encoded); err != nil {
		t.Fatalf("store: %v", err)
	}

	got, err := sn.GetStorageProof(addr, slot)
	if err != nil {
		t.Fatalf("GetStorageProof: %v", err)
	}
	if got.Value != value {
		t.Fatalf("value mismatch: got %v, want %v", got.Value, value)
	}
}

func TestGetContractCodeIntegration(t *testing.T) {
	sn, _ := newTestStateNetwork()
	sn.Start()
	defer sn.Stop()

	code := []byte{0x60, 0x80, 0x60, 0x40, 0x52} // PUSH1 80 PUSH1 40 MSTORE
	codeHash := crypto.Keccak256Hash(code)

	key := StateContentKey{
		ContentType: ContentTypeContractBytecode,
		AddressHash: types.Hash{}, // matches GetContractCode wildcard
		CodeHash:    codeHash,
	}
	if err := sn.StoreContent(key, code); err != nil {
		t.Fatalf("store: %v", err)
	}

	got, err := sn.GetContractCode(codeHash)
	if err != nil {
		t.Fatalf("GetContractCode: %v", err)
	}
	if !bytes.Equal(got, code) {
		t.Fatal("code mismatch")
	}
}

// --- Concurrency safety ---

func TestStateNetworkConcurrentStoreFind(t *testing.T) {
	sn, _ := newTestStateNetwork()

	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			addrHash := types.BytesToHash([]byte{byte(idx)})
			key := StateContentKey{
				ContentType: ContentTypeAccountTrieNode,
				AddressHash: addrHash,
			}
			data := []byte{byte(idx), byte(idx + 1)}

			_ = sn.StoreContent(key, data)
			_, _ = sn.FindContent(key)
		}(i)
	}
	wg.Wait()
}

func TestStateNetworkConcurrentStartStop(t *testing.T) {
	sn, _ := newTestStateNetwork()

	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(2)
		go func() {
			defer wg.Done()
			sn.Start()
		}()
		go func() {
			defer wg.Done()
			sn.Stop()
		}()
	}
	wg.Wait()
}

// --- DefaultStateNetworkConfig ---

func TestDefaultStateNetworkConfig(t *testing.T) {
	cfg := DefaultStateNetworkConfig()
	if cfg.MaxContentSize == 0 {
		t.Fatal("MaxContentSize should not be zero")
	}
	if cfg.ContentRadius.Raw.Sign() <= 0 {
		t.Fatal("ContentRadius should be positive")
	}
	if cfg.EvictionPolicy != EvictFarthest {
		t.Fatal("default eviction policy should be EvictFarthest")
	}
}
