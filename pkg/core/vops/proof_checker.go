// proof_checker.go implements VOPS proof verification for stateless
// validation. It verifies Merkle proofs against claimed state roots,
// validates storage proofs, account proofs, and code inclusion proofs.
package vops

import (
	"errors"
	"math/big"
	"sort"

	"github.com/eth2028/eth2028/core/types"
	"github.com/eth2028/eth2028/crypto"
)

// Proof checker errors.
var (
	ErrProofNil          = errors.New("proof_checker: nil proof")
	ErrProofEmpty        = errors.New("proof_checker: empty proof nodes")
	ErrStateRootMismatch = errors.New("proof_checker: state root mismatch")
	ErrAccountNotFound   = errors.New("proof_checker: account not found in proof")
	ErrStorageNotFound   = errors.New("proof_checker: storage slot not found in proof")
	ErrCodeNotFound      = errors.New("proof_checker: code not found in proof")
	ErrCodeHashMismatch  = errors.New("proof_checker: code hash does not match account")
	ErrInvalidProofNode  = errors.New("proof_checker: invalid proof node")
)

// AccountProof contains a Merkle proof for an account in the state trie.
type AccountProof struct {
	Address     types.Address
	Nonce       uint64
	Balance     *big.Int
	CodeHash    types.Hash
	StorageRoot types.Hash
	ProofNodes  [][]byte // binding commitment nodes
}

// StorageProof contains a Merkle proof for a storage slot.
type StorageProof struct {
	Address    types.Address
	Slot       types.Hash
	Value      types.Hash
	ProofNodes [][]byte // binding commitment nodes
}

// CodeProof contains proof of code inclusion for a contract.
type CodeProof struct {
	Address  types.Address
	Code     []byte
	CodeHash types.Hash
}

// ProofSet collects all proofs needed for stateless block verification.
type ProofSet struct {
	StateRoot     types.Hash
	AccountProofs []AccountProof
	StorageProofs []StorageProof
	CodeProofs    []CodeProof
}

// ProofCheckResult reports the outcome of proof verification.
type ProofCheckResult struct {
	Valid           bool
	AccountsChecked int
	StorageChecked  int
	CodeChecked     int
	Errors          []ProofError
}

// ProofError records a single proof verification failure.
type ProofError struct {
	Address types.Address
	Slot    types.Hash // zero for account/code errors
	Message string
}

// ProofChecker verifies state proofs against a claimed state root.
type ProofChecker struct {
	stateRoot types.Hash
}

// NewProofChecker creates a checker for the given state root.
func NewProofChecker(stateRoot types.Hash) *ProofChecker {
	return &ProofChecker{stateRoot: stateRoot}
}

// StateRoot returns the state root this checker validates against.
func (pc *ProofChecker) StateRoot() types.Hash {
	return pc.stateRoot
}

// VerifyAccountProof checks that an account proof is valid against the
// state root. It recomputes the binding commitment from the claimed
// account data and the state root, then verifies the proof node matches.
func (pc *ProofChecker) VerifyAccountProof(proof *AccountProof) error {
	if proof == nil {
		return ErrProofNil
	}
	if len(proof.ProofNodes) == 0 {
		return ErrProofEmpty
	}

	// Verify all proof nodes are non-empty.
	for _, node := range proof.ProofNodes {
		if len(node) == 0 {
			return ErrInvalidProofNode
		}
	}

	// Compute the leaf hash from the claimed account data.
	leafHash := computeAccountLeafHash(proof)

	// Recompute the binding commitment and verify it matches the proof node.
	expected := bindingCommitment(pc.stateRoot, leafHash)
	if !bytesEqual(proof.ProofNodes[0], expected) {
		return ErrStateRootMismatch
	}
	return nil
}

// VerifyStorageProof checks that a storage proof is valid against the
// account's storage root. The account proof must be verified first
// to establish the storage root.
func (pc *ProofChecker) VerifyStorageProof(accountStorageRoot types.Hash, proof *StorageProof) error {
	if proof == nil {
		return ErrProofNil
	}
	if len(proof.ProofNodes) == 0 {
		return ErrProofEmpty
	}

	for _, node := range proof.ProofNodes {
		if len(node) == 0 {
			return ErrInvalidProofNode
		}
	}

	// Compute the storage leaf hash.
	leafHash := computeStorageLeafHash(proof.Slot, proof.Value)

	// Verify the binding commitment matches.
	expected := bindingCommitment(accountStorageRoot, leafHash)
	if !bytesEqual(proof.ProofNodes[0], expected) {
		return ErrStateRootMismatch
	}
	return nil
}

// VerifyCodeProof checks that a code proof matches the account's code hash.
func (pc *ProofChecker) VerifyCodeProof(proof *CodeProof, accountCodeHash types.Hash) error {
	if proof == nil {
		return ErrProofNil
	}
	if len(proof.Code) == 0 && accountCodeHash != types.EmptyCodeHash {
		return ErrCodeNotFound
	}

	computedHash := crypto.Keccak256Hash(proof.Code)
	if computedHash != accountCodeHash {
		return ErrCodeHashMismatch
	}
	return nil
}

// VerifyProofSet validates an entire set of proofs for a block.
// Returns a ProofCheckResult with details about each check.
func (pc *ProofChecker) VerifyProofSet(proofSet *ProofSet) *ProofCheckResult {
	if proofSet == nil {
		return &ProofCheckResult{
			Valid:  false,
			Errors: []ProofError{{Message: "nil proof set"}},
		}
	}

	if proofSet.StateRoot != pc.stateRoot {
		return &ProofCheckResult{
			Valid:  false,
			Errors: []ProofError{{Message: "proof set state root mismatch"}},
		}
	}

	result := &ProofCheckResult{Valid: true}

	// Verify account proofs and collect storage roots.
	storageRoots := make(map[types.Address]types.Hash)
	codeHashes := make(map[types.Address]types.Hash)

	for _, ap := range proofSet.AccountProofs {
		result.AccountsChecked++
		err := pc.VerifyAccountProof(&ap)
		if err != nil {
			result.Valid = false
			result.Errors = append(result.Errors, ProofError{
				Address: ap.Address,
				Message: err.Error(),
			})
		} else {
			storageRoots[ap.Address] = ap.StorageRoot
			codeHashes[ap.Address] = ap.CodeHash
		}
	}

	// Verify storage proofs against their account's storage root.
	for _, sp := range proofSet.StorageProofs {
		result.StorageChecked++
		sr, ok := storageRoots[sp.Address]
		if !ok {
			result.Valid = false
			result.Errors = append(result.Errors, ProofError{
				Address: sp.Address,
				Slot:    sp.Slot,
				Message: ErrAccountNotFound.Error(),
			})
			continue
		}
		err := pc.VerifyStorageProof(sr, &sp)
		if err != nil {
			result.Valid = false
			result.Errors = append(result.Errors, ProofError{
				Address: sp.Address,
				Slot:    sp.Slot,
				Message: err.Error(),
			})
		}
	}

	// Verify code proofs against their account's code hash.
	for _, cp := range proofSet.CodeProofs {
		result.CodeChecked++
		ch, ok := codeHashes[cp.Address]
		if !ok {
			result.Valid = false
			result.Errors = append(result.Errors, ProofError{
				Address: cp.Address,
				Message: ErrAccountNotFound.Error(),
			})
			continue
		}
		err := pc.VerifyCodeProof(&cp, ch)
		if err != nil {
			result.Valid = false
			result.Errors = append(result.Errors, ProofError{
				Address: cp.Address,
				Message: err.Error(),
			})
		}
	}

	return result
}

// BuildProofSet constructs a ProofSet from a PartialState and the
// corresponding state root. The proof nodes are binding Keccak256
// commitments (simplified for VOPS; production would use actual MPT
// or binary trie proofs).
func BuildProofSet(stateRoot types.Hash, ps *PartialState) *ProofSet {
	if ps == nil {
		return &ProofSet{StateRoot: stateRoot}
	}

	proofSet := &ProofSet{StateRoot: stateRoot}

	// Sort addresses for determinism.
	addrs := make([]types.Address, 0, len(ps.Accounts))
	for addr := range ps.Accounts {
		addrs = append(addrs, addr)
	}
	sort.Slice(addrs, func(i, j int) bool {
		return addressLess(addrs[i], addrs[j])
	})

	for _, addr := range addrs {
		acct := ps.Accounts[addr]
		ap := AccountProof{
			Address:     addr,
			Nonce:       acct.Nonce,
			Balance:     acct.Balance,
			CodeHash:    acct.CodeHash,
			StorageRoot: acct.StorageRoot,
		}
		// Compute binding commitment as the proof node.
		leafHash := computeAccountLeafHash(&ap)
		ap.ProofNodes = [][]byte{bindingCommitment(stateRoot, leafHash)}
		proofSet.AccountProofs = append(proofSet.AccountProofs, ap)
	}

	// Storage proofs.
	for addr, slots := range ps.Storage {
		acct := ps.Accounts[addr]
		if acct == nil {
			continue
		}
		skeys := make([]types.Hash, 0, len(slots))
		for k := range slots {
			skeys = append(skeys, k)
		}
		sort.Slice(skeys, func(i, j int) bool {
			return hashLessThan(skeys[i], skeys[j])
		})

		for _, slot := range skeys {
			value := slots[slot]
			leafHash := computeStorageLeafHash(slot, value)
			proofSet.StorageProofs = append(proofSet.StorageProofs, StorageProof{
				Address:    addr,
				Slot:       slot,
				Value:      value,
				ProofNodes: [][]byte{bindingCommitment(acct.StorageRoot, leafHash)},
			})
		}
	}

	// Code proofs.
	for addr, code := range ps.Code {
		codeHash := crypto.Keccak256Hash(code)
		proofSet.CodeProofs = append(proofSet.CodeProofs, CodeProof{
			Address:  addr,
			Code:     code,
			CodeHash: codeHash,
		})
	}

	return proofSet
}

// computeAccountLeafHash creates a hash commitment for account data.
func computeAccountLeafHash(ap *AccountProof) types.Hash {
	var data []byte
	data = append(data, ap.Address[:]...)
	nonceBuf := make([]byte, 8)
	nonceBuf[0] = byte(ap.Nonce >> 56)
	nonceBuf[1] = byte(ap.Nonce >> 48)
	nonceBuf[2] = byte(ap.Nonce >> 40)
	nonceBuf[3] = byte(ap.Nonce >> 32)
	nonceBuf[4] = byte(ap.Nonce >> 24)
	nonceBuf[5] = byte(ap.Nonce >> 16)
	nonceBuf[6] = byte(ap.Nonce >> 8)
	nonceBuf[7] = byte(ap.Nonce)
	data = append(data, nonceBuf...)
	if ap.Balance != nil {
		data = append(data, ap.Balance.Bytes()...)
	}
	data = append(data, ap.CodeHash[:]...)
	data = append(data, ap.StorageRoot[:]...)
	return crypto.Keccak256Hash(data)
}

// computeStorageLeafHash creates a hash commitment for a storage slot.
func computeStorageLeafHash(slot, value types.Hash) types.Hash {
	return crypto.Keccak256Hash(slot[:], value[:])
}

// bindingCommitment creates a Keccak256 commitment binding a root hash
// to a leaf hash. Both the builder and verifier compute this identically.
func bindingCommitment(root types.Hash, leafHash types.Hash) []byte {
	return crypto.Keccak256(root[:], leafHash[:])
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
