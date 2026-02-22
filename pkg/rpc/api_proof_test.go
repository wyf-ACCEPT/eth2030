package rpc

import (
	"encoding/hex"
	"math/big"
	"strings"
	"testing"

	"github.com/eth2030/eth2030/core/types"
	"github.com/eth2030/eth2030/crypto"
	"github.com/eth2030/eth2030/rlp"
	"github.com/eth2030/eth2030/trie"
)

// TestGetProof_NonEmptyAccountProof verifies that eth_getProof returns
// non-empty Merkle proof nodes for an existing account.
func TestGetProof_NonEmptyAccountProof(t *testing.T) {
	api := NewEthAPI(newMockBackend())
	resp := callRPC(t, api, "eth_getProof",
		"0x000000000000000000000000000000000000aaaa",
		[]string{},
		"latest")

	if resp.Error != nil {
		t.Fatalf("error: %v", resp.Error.Message)
	}
	result, ok := resp.Result.(*AccountProof)
	if !ok {
		t.Fatalf("result not *AccountProof: %T", resp.Result)
	}

	// The account proof should contain at least one node.
	if len(result.AccountProof) == 0 {
		t.Fatal("expected non-empty accountProof")
	}

	// Each proof node should be a valid 0x-prefixed hex string.
	for i, node := range result.AccountProof {
		if !strings.HasPrefix(node, "0x") {
			t.Fatalf("proof node %d missing 0x prefix: %s", i, node)
		}
		if len(node) <= 2 {
			t.Fatalf("proof node %d is empty", i)
		}
	}

	// Verify balance and nonce are correct.
	if result.Balance != "0xde0b6b3a7640000" { // 1e18
		t.Fatalf("want balance 0xde0b6b3a7640000, got %v", result.Balance)
	}
	if result.Nonce != "0x5" {
		t.Fatalf("want nonce 0x5, got %v", result.Nonce)
	}
}

// TestGetProof_VerifyAccountProof verifies that the returned Merkle proof
// can be verified against the state root using trie.VerifyProof.
func TestGetProof_VerifyAccountProof(t *testing.T) {
	mb := newMockBackend()
	api := NewEthAPI(mb)

	addrHex := "0x000000000000000000000000000000000000aaaa"
	resp := callRPC(t, api, "eth_getProof", addrHex, []string{}, "latest")

	if resp.Error != nil {
		t.Fatalf("error: %v", resp.Error.Message)
	}
	result := resp.Result.(*AccountProof)

	// Decode proof nodes from hex.
	proofNodes := make([][]byte, len(result.AccountProof))
	for i, hexNode := range result.AccountProof {
		node, err := hex.DecodeString(strings.TrimPrefix(hexNode, "0x"))
		if err != nil {
			t.Fatalf("decode proof node %d: %v", i, err)
		}
		proofNodes[i] = node
	}

	// Build the state trie root from the mock state.
	stateTrie := mb.statedb.BuildStateTrie()
	root := stateTrie.Hash()

	// Verify the proof against the state root.
	addr := types.HexToAddress(addrHex)
	addrHash := crypto.Keccak256(addr[:])
	val, err := trie.VerifyProof(root, addrHash, proofNodes)
	if err != nil {
		t.Fatalf("VerifyProof error: %v", err)
	}
	if val == nil {
		t.Fatal("VerifyProof returned nil value for existing account")
	}
}

// TestGetProof_NonExistentAccount verifies eth_getProof for an account
// that does not exist returns zero values and a valid absence proof.
func TestGetProof_NonExistentAccount(t *testing.T) {
	api := NewEthAPI(newMockBackend())
	resp := callRPC(t, api, "eth_getProof",
		"0x0000000000000000000000000000000000001234",
		[]string{},
		"latest")

	if resp.Error != nil {
		t.Fatalf("error: %v", resp.Error.Message)
	}
	result := resp.Result.(*AccountProof)

	// Non-existent account should have zero balance and nonce.
	if result.Balance != "0x0" {
		t.Fatalf("want balance 0x0, got %v", result.Balance)
	}
	if result.Nonce != "0x0" {
		t.Fatalf("want nonce 0x0, got %v", result.Nonce)
	}

	// Storage hash should be the empty root hash.
	emptyRoot := encodeHash(types.EmptyRootHash)
	if result.StorageHash != emptyRoot {
		t.Fatalf("want storageHash %v, got %v", emptyRoot, result.StorageHash)
	}

	// Code hash should be the empty code hash.
	emptyCode := encodeHash(types.EmptyCodeHash)
	if result.CodeHash != emptyCode {
		t.Fatalf("want codeHash %v, got %v", emptyCode, result.CodeHash)
	}

	// The proof should still have nodes (absence proof in non-empty trie).
	if len(result.AccountProof) == 0 {
		t.Fatal("expected non-empty absence proof")
	}
}

// TestGetProof_WithStorageKeys verifies that storage proofs are returned
// for the requested storage keys.
func TestGetProof_WithStorageKeys(t *testing.T) {
	mb := newMockBackend()
	addr := types.HexToAddress("0xaaaa")
	slot := types.HexToHash("0x0000000000000000000000000000000000000000000000000000000000000001")
	val := types.HexToHash("0x000000000000000000000000000000000000000000000000000000000000002a")
	mb.statedb.SetState(addr, slot, val)

	api := NewEthAPI(mb)
	resp := callRPC(t, api, "eth_getProof",
		"0x000000000000000000000000000000000000aaaa",
		[]string{"0x0000000000000000000000000000000000000000000000000000000000000001"},
		"latest")

	if resp.Error != nil {
		t.Fatalf("error: %v", resp.Error.Message)
	}
	result := resp.Result.(*AccountProof)

	if len(result.StorageProof) != 1 {
		t.Fatalf("want 1 storage proof, got %d", len(result.StorageProof))
	}

	sp := result.StorageProof[0]
	// Value should be 0x2a (42).
	if sp.Value != "0x2a" {
		t.Fatalf("want storage value 0x2a, got %v", sp.Value)
	}

	// Storage proof should have at least one node.
	if len(sp.Proof) == 0 {
		t.Fatal("expected non-empty storage proof")
	}

	// Each proof node should be 0x-prefixed hex.
	for i, node := range sp.Proof {
		if !strings.HasPrefix(node, "0x") {
			t.Fatalf("storage proof node %d missing 0x prefix", i)
		}
	}
}

// TestGetProof_StorageAbsenceProof verifies that a storage slot that doesn't
// exist returns a zero value and a valid absence proof.
func TestGetProof_StorageAbsenceProof(t *testing.T) {
	mb := newMockBackend()
	// Set one storage slot so the storage trie is non-empty.
	addr := types.HexToAddress("0xaaaa")
	slot1 := types.HexToHash("0x01")
	mb.statedb.SetState(addr, slot1, types.HexToHash("0x42"))

	api := NewEthAPI(mb)
	// Request proof for a slot that doesn't exist.
	resp := callRPC(t, api, "eth_getProof",
		"0x000000000000000000000000000000000000aaaa",
		[]string{"0x0000000000000000000000000000000000000000000000000000000000000099"},
		"latest")

	if resp.Error != nil {
		t.Fatalf("error: %v", resp.Error.Message)
	}
	result := resp.Result.(*AccountProof)

	if len(result.StorageProof) != 1 {
		t.Fatalf("want 1 storage proof, got %d", len(result.StorageProof))
	}

	sp := result.StorageProof[0]
	if sp.Value != "0x0" {
		t.Fatalf("want storage value 0x0, got %v", sp.Value)
	}

	// Absence proof should still have nodes.
	if len(sp.Proof) == 0 {
		t.Fatal("expected non-empty absence proof for storage slot")
	}
}

// TestGetProof_MultipleStorageKeys verifies that multiple storage keys
// can be requested at once.
func TestGetProof_MultipleStorageKeys(t *testing.T) {
	mb := newMockBackend()
	addr := types.HexToAddress("0xaaaa")
	slot1 := types.HexToHash("0x01")
	slot2 := types.HexToHash("0x02")
	mb.statedb.SetState(addr, slot1, types.HexToHash("0x0a"))
	mb.statedb.SetState(addr, slot2, types.HexToHash("0x0b"))

	api := NewEthAPI(mb)
	resp := callRPC(t, api, "eth_getProof",
		"0x000000000000000000000000000000000000aaaa",
		[]string{
			"0x0000000000000000000000000000000000000000000000000000000000000001",
			"0x0000000000000000000000000000000000000000000000000000000000000002",
			"0x0000000000000000000000000000000000000000000000000000000000000003",
		},
		"latest")

	if resp.Error != nil {
		t.Fatalf("error: %v", resp.Error.Message)
	}
	result := resp.Result.(*AccountProof)

	if len(result.StorageProof) != 3 {
		t.Fatalf("want 3 storage proofs, got %d", len(result.StorageProof))
	}

	// Slot 1: value 0x0a (10).
	if result.StorageProof[0].Value != "0xa" {
		t.Fatalf("want slot 1 value 0xa, got %v", result.StorageProof[0].Value)
	}
	// Slot 2: value 0x0b (11).
	if result.StorageProof[1].Value != "0xb" {
		t.Fatalf("want slot 2 value 0xb, got %v", result.StorageProof[1].Value)
	}
	// Slot 3: absent, value 0x0.
	if result.StorageProof[2].Value != "0x0" {
		t.Fatalf("want slot 3 value 0x0, got %v", result.StorageProof[2].Value)
	}
}

// TestGetProof_VerifyStorageProof verifies that returned storage proofs can
// be cryptographically verified against the storage root.
func TestGetProof_VerifyStorageProof(t *testing.T) {
	mb := newMockBackend()
	addr := types.HexToAddress("0xaaaa")
	slot := types.HexToHash("0x01")
	mb.statedb.SetState(addr, slot, types.HexToHash("0xff"))

	api := NewEthAPI(mb)
	resp := callRPC(t, api, "eth_getProof",
		"0x000000000000000000000000000000000000aaaa",
		[]string{"0x0000000000000000000000000000000000000000000000000000000000000001"},
		"latest")

	if resp.Error != nil {
		t.Fatalf("error: %v", resp.Error.Message)
	}
	result := resp.Result.(*AccountProof)

	// Decode the storage root hash.
	storageRoot := types.HexToHash(result.StorageHash)

	// Decode storage proof nodes.
	sp := result.StorageProof[0]
	proofNodes := make([][]byte, len(sp.Proof))
	for i, hexNode := range sp.Proof {
		node, err := hex.DecodeString(strings.TrimPrefix(hexNode, "0x"))
		if err != nil {
			t.Fatalf("decode storage proof node %d: %v", i, err)
		}
		proofNodes[i] = node
	}

	// Verify the storage proof against the storage root.
	slotHash := crypto.Keccak256(slot[:])
	val, err := trie.VerifyProof(storageRoot, slotHash, proofNodes)
	if err != nil {
		t.Fatalf("VerifyProof storage error: %v", err)
	}
	if val == nil {
		t.Fatal("VerifyProof returned nil for existing storage slot")
	}

	// The raw proof value is RLP-encoded. Decode it first.
	var decoded []byte
	if err := rlp.DecodeBytes(val, &decoded); err != nil {
		t.Fatalf("RLP decode storage value: %v", err)
	}
	valInt := new(big.Int).SetBytes(decoded)
	if valInt.Cmp(big.NewInt(0xff)) != 0 {
		t.Fatalf("want storage value 255, got %s", valInt)
	}
}

// TestGetProof_EmptyStorageKeys verifies that requesting no storage keys
// returns an empty StorageProof array.
func TestGetProof_EmptyStorageKeys(t *testing.T) {
	api := NewEthAPI(newMockBackend())
	resp := callRPC(t, api, "eth_getProof",
		"0x000000000000000000000000000000000000aaaa",
		[]string{},
		"latest")

	if resp.Error != nil {
		t.Fatalf("error: %v", resp.Error.Message)
	}
	result := resp.Result.(*AccountProof)

	if len(result.StorageProof) != 0 {
		t.Fatalf("want 0 storage proofs, got %d", len(result.StorageProof))
	}
}

// TestGetProof_InvalidParams verifies error handling for missing params.
func TestGetProof_InvalidParams(t *testing.T) {
	api := NewEthAPI(newMockBackend())

	// Missing all params.
	resp := callRPC(t, api, "eth_getProof")
	if resp.Error == nil {
		t.Fatal("expected error for missing params")
	}
	if resp.Error.Code != ErrCodeInvalidParams {
		t.Fatalf("want error code %d, got %d", ErrCodeInvalidParams, resp.Error.Code)
	}
}
