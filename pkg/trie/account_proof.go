package trie

import (
	"errors"
	"math/big"

	"github.com/eth2030/eth2030/core/types"
	"github.com/eth2030/eth2030/crypto"
	"github.com/eth2030/eth2030/rlp"
)

var (
	// ErrProofVerifyFailed is returned when an account proof verification fails.
	ErrProofVerifyFailed = errors.New("trie: account proof verification failed")
)

// ProofResult combines an account proof with zero or more storage proofs,
// matching the response shape of eth_getProof (EIP-1186).
type ProofResult struct {
	Account      *AccountProofData
	StorageProofs []StorageProofData
}

// AccountProofData contains Merkle proof data for a single Ethereum account.
type AccountProofData struct {
	Address     types.Address
	AccountRLP  []byte   // RLP-encoded account: [nonce, balance, storageRoot, codeHash]
	Proof       [][]byte // list of RLP-encoded trie nodes on the path
	Balance     *big.Int
	Nonce       uint64
	StorageHash types.Hash
	CodeHash    types.Hash
}

// StorageProofData contains the Merkle proof for a single storage slot.
type StorageProofData struct {
	Key   types.Hash
	Value types.Hash
	Proof [][]byte
}

// GenerateAccountProof generates a Merkle proof for the given account address
// against a state trie identified by root. The trieGetter function resolves
// a trie node by its hash, but for in-memory tries built with this package,
// we walk the trie directly. This function builds an in-memory trie approach:
// it accepts a *Trie directly, hashes the address with Keccak256, and walks
// the path collecting proof nodes.
func GenerateAccountProof(root types.Hash, address types.Address, stateTrie *Trie) (*AccountProofData, error) {
	result := &AccountProofData{
		Address: address,
		Balance: new(big.Int),
	}

	addrHash := crypto.Keccak256(address[:])

	// Compute and verify root hash matches.
	trieRoot := stateTrie.Hash()
	if trieRoot != root {
		return nil, errors.New("trie: root hash mismatch")
	}

	// Try to generate a presence proof.
	proof, err := stateTrie.Prove(addrHash)
	if err == ErrNotFound {
		// Account does not exist; generate absence proof.
		proof, err = stateTrie.ProveAbsence(addrHash)
		if err != nil {
			return nil, err
		}
		result.Proof = proof
		result.StorageHash = types.EmptyRootHash
		result.CodeHash = types.EmptyCodeHash
		return result, nil
	}
	if err != nil {
		return nil, err
	}

	result.Proof = proof

	// Retrieve the raw account RLP from the trie.
	accountRLP, err := stateTrie.Get(addrHash)
	if err != nil {
		return nil, err
	}
	result.AccountRLP = accountRLP

	// Decode the account fields.
	nonce, balance, storageHash, codeHash, err := DecodeAccountFields(accountRLP)
	if err != nil {
		return nil, err
	}

	result.Nonce = nonce
	result.Balance = balance
	result.StorageHash = storageHash
	result.CodeHash = codeHash

	return result, nil
}

// VerifyAccountProof verifies that the given account proof is valid against
// the provided state root. It checks that:
//  1. The proof nodes hash correctly from leaf to root.
//  2. The account data at the leaf matches the proof's declared fields.
//
// Returns (true, nil) if valid, (false, nil) if the account is provably
// absent, or (false, error) on verification failure.
func VerifyAccountProof(root types.Hash, proof *AccountProofData) (bool, error) {
	addrHash := crypto.Keccak256(proof.Address[:])

	val, err := VerifyProof(root, addrHash, proof.Proof)
	if err != nil {
		return false, ErrProofVerifyFailed
	}

	// If VerifyProof returns nil, the account is provably absent.
	if val == nil {
		if proof.Nonce == 0 && proof.Balance.Sign() == 0 &&
			proof.StorageHash == types.EmptyRootHash &&
			proof.CodeHash == types.EmptyCodeHash {
			return false, nil
		}
		return false, ErrProofVerifyFailed
	}

	// Verify the account RLP matches what the proof produced.
	if proof.AccountRLP != nil {
		if !bytesEqual(val, proof.AccountRLP) {
			return false, ErrProofVerifyFailed
		}
	}

	// Decode the proved value and check fields match.
	nonce, balance, storageHash, codeHash, err := DecodeAccountFields(val)
	if err != nil {
		return false, ErrProofVerifyFailed
	}

	if nonce != proof.Nonce {
		return false, ErrProofVerifyFailed
	}
	if balance.Cmp(proof.Balance) != 0 {
		return false, ErrProofVerifyFailed
	}
	if storageHash != proof.StorageHash {
		return false, ErrProofVerifyFailed
	}
	if codeHash != proof.CodeHash {
		return false, ErrProofVerifyFailed
	}

	return true, nil
}

// EncodeAccountFields RLP-encodes an Ethereum account from its individual
// fields into the standard 4-element list: [nonce, balance, storageRoot, codeHash].
func EncodeAccountFields(nonce uint64, balance *big.Int, storageHash, codeHash types.Hash) []byte {
	if balance == nil {
		balance = new(big.Int)
	}
	data, _ := rlp.EncodeToBytes(struct {
		Nonce    uint64
		Balance  *big.Int
		Root     types.Hash
		CodeHash []byte
	}{
		Nonce:    nonce,
		Balance:  balance,
		Root:     storageHash,
		CodeHash: codeHash[:],
	})
	return data
}

// DecodeAccountFields decodes an RLP-encoded Ethereum account into its
// individual fields: nonce, balance, storageRoot, and codeHash.
func DecodeAccountFields(data []byte) (nonce uint64, balance *big.Int, storageHash, codeHash types.Hash, err error) {
	items, decErr := decodeRLPList(data)
	if decErr != nil {
		err = decErr
		return
	}
	if len(items) != 4 {
		err = errors.New("trie: invalid account encoding: expected 4 fields")
		return
	}

	// Nonce.
	nonce = decodeBytesAsUint64(items[0])

	// Balance.
	balance = new(big.Int)
	if len(items[1]) > 0 {
		balance.SetBytes(items[1])
	}

	// Storage root.
	if len(items[2]) == 32 {
		copy(storageHash[:], items[2])
	}

	// Code hash.
	if len(items[3]) == 32 {
		copy(codeHash[:], items[3])
	}

	return
}

// GenerateStorageProof generates a Merkle proof for a single storage slot
// in the given storage trie.
func GenerateStorageProof(storageRoot types.Hash, key types.Hash, storageTrie *Trie) (*StorageProofData, error) {
	result := &StorageProofData{
		Key: key,
	}

	slotHash := crypto.Keccak256(key[:])

	proof, err := storageTrie.Prove(slotHash)
	if err == ErrNotFound {
		proof, err = storageTrie.ProveAbsence(slotHash)
		if err != nil {
			return nil, err
		}
		result.Proof = proof
		return result, nil
	}
	if err != nil {
		return nil, err
	}

	result.Proof = proof

	// Retrieve the storage value.
	val, err := storageTrie.Get(slotHash)
	if err == nil && len(val) > 0 {
		result.Value = types.BytesToHash(val)
	}

	return result, nil
}

// GenerateProofResult generates a complete ProofResult for an account and
// a set of storage keys.
func GenerateProofResult(root types.Hash, address types.Address, stateTrie *Trie, storageTrie *Trie, storageKeys []types.Hash) (*ProofResult, error) {
	accountProof, err := GenerateAccountProof(root, address, stateTrie)
	if err != nil {
		return nil, err
	}

	result := &ProofResult{
		Account: accountProof,
	}

	for _, key := range storageKeys {
		if storageTrie == nil {
			result.StorageProofs = append(result.StorageProofs, StorageProofData{
				Key: key,
			})
			continue
		}
		sp, err := GenerateStorageProof(accountProof.StorageHash, key, storageTrie)
		if err != nil {
			return nil, err
		}
		result.StorageProofs = append(result.StorageProofs, *sp)
	}

	return result, nil
}

// bytesEqual compares two byte slices for equality.
func bytesEqual(a, b []byte) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
