package trie

import (
	"bytes"
	"math/big"
	"testing"

	"github.com/eth2030/eth2030/core/types"
	"github.com/eth2030/eth2030/crypto"
)

// -- EncodeAccountFields / DecodeAccountFields roundtrip --

func TestEncodeDecodeAccountFields_Roundtrip(t *testing.T) {
	nonce := uint64(42)
	balance := big.NewInt(1_000_000_000)
	storageHash := types.HexToHash("0xabcdef0123456789abcdef0123456789abcdef0123456789abcdef0123456789")
	codeHash := types.EmptyCodeHash

	encoded := EncodeAccountFields(nonce, balance, storageHash, codeHash)
	if len(encoded) == 0 {
		t.Fatal("EncodeAccountFields returned empty")
	}

	gotNonce, gotBalance, gotStorage, gotCode, err := DecodeAccountFields(encoded)
	if err != nil {
		t.Fatalf("DecodeAccountFields error: %v", err)
	}
	if gotNonce != nonce {
		t.Fatalf("nonce = %d, want %d", gotNonce, nonce)
	}
	if gotBalance.Cmp(balance) != 0 {
		t.Fatalf("balance = %s, want %s", gotBalance, balance)
	}
	if gotStorage != storageHash {
		t.Fatalf("storageHash mismatch")
	}
	if gotCode != codeHash {
		t.Fatalf("codeHash mismatch")
	}
}

func TestEncodeDecodeAccountFields_ZeroValues(t *testing.T) {
	nonce := uint64(0)
	balance := big.NewInt(0)
	storageHash := types.EmptyRootHash
	codeHash := types.EmptyCodeHash

	encoded := EncodeAccountFields(nonce, balance, storageHash, codeHash)
	gotNonce, gotBalance, gotStorage, gotCode, err := DecodeAccountFields(encoded)
	if err != nil {
		t.Fatalf("DecodeAccountFields error: %v", err)
	}
	if gotNonce != 0 {
		t.Fatalf("nonce = %d, want 0", gotNonce)
	}
	if gotBalance.Sign() != 0 {
		t.Fatalf("balance = %s, want 0", gotBalance)
	}
	if gotStorage != storageHash {
		t.Fatalf("storageHash mismatch")
	}
	if gotCode != codeHash {
		t.Fatalf("codeHash mismatch")
	}
}

func TestEncodeDecodeAccountFields_LargeBalance(t *testing.T) {
	nonce := uint64(999)
	// ~1 ETH in wei
	balance, _ := new(big.Int).SetString("1000000000000000000", 10)
	storageHash := types.HexToHash("0x1234567890abcdef1234567890abcdef1234567890abcdef1234567890abcdef")
	codeHash := types.HexToHash("0xdeadbeefdeadbeefdeadbeefdeadbeefdeadbeefdeadbeefdeadbeefdeadbeef")

	encoded := EncodeAccountFields(nonce, balance, storageHash, codeHash)
	gotNonce, gotBalance, gotStorage, gotCode, err := DecodeAccountFields(encoded)
	if err != nil {
		t.Fatalf("DecodeAccountFields error: %v", err)
	}
	if gotNonce != nonce {
		t.Fatalf("nonce = %d, want %d", gotNonce, nonce)
	}
	if gotBalance.Cmp(balance) != 0 {
		t.Fatalf("balance = %s, want %s", gotBalance, balance)
	}
	if gotStorage != storageHash {
		t.Fatalf("storageHash mismatch")
	}
	if gotCode != codeHash {
		t.Fatalf("codeHash mismatch")
	}
}

func TestEncodeAccountFields_NilBalance(t *testing.T) {
	// nil balance should be treated as zero.
	encoded := EncodeAccountFields(0, nil, types.EmptyRootHash, types.EmptyCodeHash)
	_, gotBalance, _, _, err := DecodeAccountFields(encoded)
	if err != nil {
		t.Fatalf("DecodeAccountFields error: %v", err)
	}
	if gotBalance.Sign() != 0 {
		t.Fatalf("balance = %s, want 0", gotBalance)
	}
}

func TestDecodeAccountFields_InvalidData(t *testing.T) {
	// Empty data.
	_, _, _, _, err := DecodeAccountFields(nil)
	if err == nil {
		t.Fatal("expected error for nil data")
	}

	// Garbage data.
	_, _, _, _, err = DecodeAccountFields([]byte{0xff, 0xfe})
	if err == nil {
		t.Fatal("expected error for garbage data")
	}

	// Valid RLP but wrong number of elements (3 instead of 4).
	// Encode a 3-element list: we can craft this manually.
	_, _, _, _, err = DecodeAccountFields([]byte{0xc3, 0x01, 0x02, 0x03})
	if err == nil {
		t.Fatal("expected error for 3-element account encoding")
	}
}

// -- GenerateAccountProof and VerifyAccountProof --

func TestGenerateAndVerifyAccountProof_ExistingAccount(t *testing.T) {
	stateTrie := New()

	addr := types.HexToAddress("0x1234567890abcdef1234567890abcdef12345678")
	nonce := uint64(42)
	balance := big.NewInt(1_000_000_000)
	storageHash := types.EmptyRootHash
	codeHash := types.EmptyCodeHash

	accountRLP := EncodeAccountFields(nonce, balance, storageHash, codeHash)
	addrHash := crypto.Keccak256(addr[:])
	stateTrie.Put(addrHash, accountRLP)

	root := stateTrie.Hash()

	proof, err := GenerateAccountProof(root, addr, stateTrie)
	if err != nil {
		t.Fatalf("GenerateAccountProof error: %v", err)
	}

	if proof.Address != addr {
		t.Fatalf("address mismatch")
	}
	if proof.Nonce != nonce {
		t.Fatalf("nonce = %d, want %d", proof.Nonce, nonce)
	}
	if proof.Balance.Cmp(balance) != 0 {
		t.Fatalf("balance = %s, want %s", proof.Balance, balance)
	}
	if proof.StorageHash != storageHash {
		t.Fatalf("storage hash mismatch")
	}
	if proof.CodeHash != codeHash {
		t.Fatalf("code hash mismatch")
	}
	if !bytes.Equal(proof.AccountRLP, accountRLP) {
		t.Fatalf("AccountRLP mismatch")
	}
	if len(proof.Proof) == 0 {
		t.Fatalf("expected non-empty proof")
	}

	// Verify the proof.
	valid, err := VerifyAccountProof(root, proof)
	if err != nil {
		t.Fatalf("VerifyAccountProof error: %v", err)
	}
	if !valid {
		t.Fatalf("expected valid proof")
	}
}

func TestGenerateAndVerifyAccountProof_NonExistent(t *testing.T) {
	stateTrie := New()

	// Insert one account so the trie is not empty.
	addr1 := types.HexToAddress("0x1111111111111111111111111111111111111111")
	accountRLP := EncodeAccountFields(1, big.NewInt(100), types.EmptyRootHash, types.EmptyCodeHash)
	stateTrie.Put(crypto.Keccak256(addr1[:]), accountRLP)

	root := stateTrie.Hash()

	// Generate proof for a non-existent account.
	addr2 := types.HexToAddress("0x2222222222222222222222222222222222222222")
	proof, err := GenerateAccountProof(root, addr2, stateTrie)
	if err != nil {
		t.Fatalf("GenerateAccountProof error: %v", err)
	}

	if proof.Nonce != 0 {
		t.Fatalf("nonce = %d, want 0", proof.Nonce)
	}
	if proof.Balance.Sign() != 0 {
		t.Fatalf("balance = %s, want 0", proof.Balance)
	}
	if proof.StorageHash != types.EmptyRootHash {
		t.Fatalf("expected empty root hash")
	}
	if proof.CodeHash != types.EmptyCodeHash {
		t.Fatalf("expected empty code hash")
	}

	// Verify absence proof.
	valid, err := VerifyAccountProof(root, proof)
	if err != nil {
		t.Fatalf("VerifyAccountProof error: %v", err)
	}
	if valid {
		t.Fatalf("expected valid=false for absent account (absence is valid but account doesn't exist)")
	}
}

func TestGenerateAccountProof_EmptyTrie(t *testing.T) {
	stateTrie := New()
	root := stateTrie.Hash()

	addr := types.HexToAddress("0xdeadbeefdeadbeefdeadbeefdeadbeefdeadbeef")
	proof, err := GenerateAccountProof(root, addr, stateTrie)
	if err != nil {
		t.Fatalf("GenerateAccountProof error: %v", err)
	}

	if proof.Nonce != 0 {
		t.Fatalf("nonce = %d, want 0", proof.Nonce)
	}

	// Verify absence against empty root.
	valid, err := VerifyAccountProof(root, proof)
	if err != nil {
		t.Fatalf("VerifyAccountProof error: %v", err)
	}
	if valid {
		t.Fatalf("expected valid=false for absent account in empty trie")
	}
}

func TestVerifyAccountProof_InvalidProof(t *testing.T) {
	stateTrie := New()

	addr := types.HexToAddress("0xaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa")
	accountRLP := EncodeAccountFields(10, big.NewInt(500), types.EmptyRootHash, types.EmptyCodeHash)
	stateTrie.Put(crypto.Keccak256(addr[:]), accountRLP)

	root := stateTrie.Hash()

	proof, err := GenerateAccountProof(root, addr, stateTrie)
	if err != nil {
		t.Fatalf("GenerateAccountProof error: %v", err)
	}

	// Tamper with the nonce.
	tampered := &AccountProofData{
		Address:     proof.Address,
		AccountRLP:  proof.AccountRLP,
		Proof:       proof.Proof,
		Balance:     proof.Balance,
		Nonce:       999, // wrong nonce
		StorageHash: proof.StorageHash,
		CodeHash:    proof.CodeHash,
	}

	_, err = VerifyAccountProof(root, tampered)
	if err == nil {
		t.Fatal("expected error for tampered nonce")
	}
}

func TestVerifyAccountProof_WrongRoot(t *testing.T) {
	stateTrie := New()

	addr := types.HexToAddress("0xbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb")
	accountRLP := EncodeAccountFields(1, big.NewInt(100), types.EmptyRootHash, types.EmptyCodeHash)
	stateTrie.Put(crypto.Keccak256(addr[:]), accountRLP)

	root := stateTrie.Hash()

	proof, err := GenerateAccountProof(root, addr, stateTrie)
	if err != nil {
		t.Fatalf("GenerateAccountProof error: %v", err)
	}

	// Verify against wrong root.
	wrongRoot := types.HexToHash("0xdeadbeef")
	_, err = VerifyAccountProof(wrongRoot, proof)
	if err == nil {
		t.Fatal("expected error for wrong root")
	}
}

func TestVerifyAccountProof_TamperedProofNodes(t *testing.T) {
	stateTrie := New()

	addr := types.HexToAddress("0xcccccccccccccccccccccccccccccccccccccccc")
	accountRLP := EncodeAccountFields(5, big.NewInt(200), types.EmptyRootHash, types.EmptyCodeHash)
	stateTrie.Put(crypto.Keccak256(addr[:]), accountRLP)

	root := stateTrie.Hash()

	proof, err := GenerateAccountProof(root, addr, stateTrie)
	if err != nil {
		t.Fatalf("GenerateAccountProof error: %v", err)
	}

	// Tamper with the first proof node.
	tamperedProof := make([][]byte, len(proof.Proof))
	for i := range proof.Proof {
		tamperedProof[i] = make([]byte, len(proof.Proof[i]))
		copy(tamperedProof[i], proof.Proof[i])
	}
	if len(tamperedProof) > 0 && len(tamperedProof[0]) > 0 {
		tamperedProof[0][0] ^= 0xff
	}

	tampered := &AccountProofData{
		Address:     proof.Address,
		AccountRLP:  proof.AccountRLP,
		Proof:       tamperedProof,
		Balance:     proof.Balance,
		Nonce:       proof.Nonce,
		StorageHash: proof.StorageHash,
		CodeHash:    proof.CodeHash,
	}

	_, err = VerifyAccountProof(root, tampered)
	if err == nil {
		t.Fatal("expected error for tampered proof nodes")
	}
}

// -- GenerateStorageProof --

func TestGenerateStorageProof_ExistingSlot(t *testing.T) {
	storageTrie := New()

	slot := types.HexToHash("0x0000000000000000000000000000000000000000000000000000000000000001")
	slotHash := crypto.Keccak256(slot[:])
	storageTrie.Put(slotHash, big.NewInt(42).Bytes())

	storageRoot := storageTrie.Hash()

	sp, err := GenerateStorageProof(storageRoot, slot, storageTrie)
	if err != nil {
		t.Fatalf("GenerateStorageProof error: %v", err)
	}

	if sp.Key != slot {
		t.Fatalf("key mismatch")
	}
	// The value should be big-endian encoding of 42.
	expected := types.BytesToHash(big.NewInt(42).Bytes())
	if sp.Value != expected {
		t.Fatalf("value = %s, want %s", sp.Value.Hex(), expected.Hex())
	}
	if len(sp.Proof) == 0 {
		t.Fatalf("expected non-empty proof")
	}
}

func TestGenerateStorageProof_NonExistentSlot(t *testing.T) {
	storageTrie := New()

	// Insert one slot.
	slot1 := types.HexToHash("0x01")
	storageTrie.Put(crypto.Keccak256(slot1[:]), big.NewInt(100).Bytes())

	storageRoot := storageTrie.Hash()

	// Query a non-existent slot.
	slot2 := types.HexToHash("0x02")
	sp, err := GenerateStorageProof(storageRoot, slot2, storageTrie)
	if err != nil {
		t.Fatalf("GenerateStorageProof error: %v", err)
	}

	if sp.Key != slot2 {
		t.Fatalf("key mismatch")
	}
	if sp.Value != (types.Hash{}) {
		t.Fatalf("expected zero value for non-existent slot")
	}
}

// -- ProofResult generation --

func TestGenerateProofResult(t *testing.T) {
	stateTrie := New()
	storageTrie := New()

	// Set up storage.
	slot := types.HexToHash("0x01")
	storageTrie.Put(crypto.Keccak256(slot[:]), big.NewInt(42).Bytes())
	storageRoot := storageTrie.Hash()

	// Set up account.
	addr := types.HexToAddress("0xabcdefabcdefabcdefabcdefabcdefabcdefabcd")
	accountRLP := EncodeAccountFields(10, big.NewInt(5000), storageRoot, types.EmptyCodeHash)
	stateTrie.Put(crypto.Keccak256(addr[:]), accountRLP)
	root := stateTrie.Hash()

	result, err := GenerateProofResult(root, addr, stateTrie, storageTrie, []types.Hash{slot})
	if err != nil {
		t.Fatalf("GenerateProofResult error: %v", err)
	}

	if result.Account.Nonce != 10 {
		t.Fatalf("nonce = %d, want 10", result.Account.Nonce)
	}
	if len(result.StorageProofs) != 1 {
		t.Fatalf("expected 1 storage proof, got %d", len(result.StorageProofs))
	}
	if result.StorageProofs[0].Key != slot {
		t.Fatalf("storage key mismatch")
	}
}

func TestGenerateProofResult_NilStorageTrie(t *testing.T) {
	stateTrie := New()

	addr := types.HexToAddress("0xeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeee")
	accountRLP := EncodeAccountFields(0, big.NewInt(0), types.EmptyRootHash, types.EmptyCodeHash)
	stateTrie.Put(crypto.Keccak256(addr[:]), accountRLP)
	root := stateTrie.Hash()

	slot := types.HexToHash("0x01")
	result, err := GenerateProofResult(root, addr, stateTrie, nil, []types.Hash{slot})
	if err != nil {
		t.Fatalf("GenerateProofResult error: %v", err)
	}

	if len(result.StorageProofs) != 1 {
		t.Fatalf("expected 1 storage proof, got %d", len(result.StorageProofs))
	}
	if result.StorageProofs[0].Value != (types.Hash{}) {
		t.Fatalf("expected zero value for nil storage trie")
	}
}

// -- Multiple accounts in the same trie --

func TestGenerateAccountProof_MultipleAccounts(t *testing.T) {
	stateTrie := New()

	addrs := []types.Address{
		types.HexToAddress("0xaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"),
		types.HexToAddress("0xbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"),
		types.HexToAddress("0xcccccccccccccccccccccccccccccccccccccccc"),
	}
	nonces := []uint64{1, 2, 3}
	balances := []*big.Int{big.NewInt(100), big.NewInt(200), big.NewInt(300)}

	for i, addr := range addrs {
		accountRLP := EncodeAccountFields(nonces[i], balances[i], types.EmptyRootHash, types.EmptyCodeHash)
		stateTrie.Put(crypto.Keccak256(addr[:]), accountRLP)
	}
	root := stateTrie.Hash()

	for i, addr := range addrs {
		proof, err := GenerateAccountProof(root, addr, stateTrie)
		if err != nil {
			t.Fatalf("GenerateAccountProof[%d] error: %v", i, err)
		}
		if proof.Nonce != nonces[i] {
			t.Fatalf("account[%d] nonce = %d, want %d", i, proof.Nonce, nonces[i])
		}
		if proof.Balance.Cmp(balances[i]) != 0 {
			t.Fatalf("account[%d] balance = %s, want %s", i, proof.Balance, balances[i])
		}

		valid, err := VerifyAccountProof(root, proof)
		if err != nil {
			t.Fatalf("VerifyAccountProof[%d] error: %v", i, err)
		}
		if !valid {
			t.Fatalf("expected valid proof for account[%d]", i)
		}
	}
}

func TestGenerateAccountProof_RootMismatch(t *testing.T) {
	stateTrie := New()

	addr := types.HexToAddress("0xdddddddddddddddddddddddddddddddddddddd")
	accountRLP := EncodeAccountFields(1, big.NewInt(100), types.EmptyRootHash, types.EmptyCodeHash)
	stateTrie.Put(crypto.Keccak256(addr[:]), accountRLP)
	stateTrie.Hash()

	// Use a wrong root.
	wrongRoot := types.HexToHash("0xdeadbeef")
	_, err := GenerateAccountProof(wrongRoot, addr, stateTrie)
	if err == nil {
		t.Fatal("expected error for root mismatch")
	}
}
