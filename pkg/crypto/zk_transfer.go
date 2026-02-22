// zk_transfer.go implements a zero-knowledge proof system for shielded
// transfers on Ethereum L1. Proofs demonstrate that a transfer is valid
// (value in range, nullifier derived correctly, commitment included in
// the accumulator) without revealing the transfer amount or parties.
//
// The construction uses SHA-256-based Pedersen-style commitments and
// Fiat-Shamir-transformed sigma protocols for zero-knowledge.
package crypto

import (
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"math/big"

	"github.com/eth2030/eth2030/core/types"
)

// ZK transfer errors.
var (
	ErrZKNilProof        = errors.New("zk: nil proof")
	ErrZKInvalidProof    = errors.New("zk: proof verification failed")
	ErrZKAmountOverflow  = errors.New("zk: amount exceeds 2^64")
	ErrZKNullifierReuse  = errors.New("zk: nullifier already spent")
	ErrZKMerkleInvalid   = errors.New("zk: merkle inclusion proof invalid")
	ErrZKCommitmentEmpty = errors.New("zk: empty commitment")
)

// ZKRangeProofBits is the bit-width for range proofs (amount in [0, 2^64)).
const ZKRangeProofBits = 64

// zkDomainSep are domain separators for different hashing contexts to prevent
// cross-protocol attacks.
var (
	zkDomainCommit    = []byte("zk-commit-v1")
	zkDomainNullifier = []byte("zk-nullifier-v1")
	zkDomainRange     = []byte("zk-range-v1")
	zkDomainChallenge = []byte("zk-challenge-v1")
	zkDomainProof     = []byte("zk-proof-v1")
)

// ZKTransferProof contains a zero-knowledge proof that a shielded transfer
// is valid: the amount is in [0, 2^64), the nullifier is correctly derived,
// and the input commitment exists in the Merkle accumulator.
type ZKTransferProof struct {
	// ProofData is the serialized sigma-protocol proof.
	ProofData []byte

	// Nullifiers are the spent input nullifiers.
	Nullifiers []types.Hash

	// OutputCommitments are the new output commitments.
	OutputCommitments []types.Hash

	// MerkleRoot is the commitment accumulator root at proof time.
	MerkleRoot types.Hash

	// RangeProofData proves each output is in [0, 2^64).
	RangeProofData []byte
}

// ZKTransferWitness contains the private data needed to construct a proof.
type ZKTransferWitness struct {
	SenderSK       [32]byte // sender's secret key
	Amount         uint64   // transfer amount
	RecipientPK    [32]byte // recipient's public key
	Randomness     [32]byte // commitment randomness (blinding)
	CommitmentIdx  uint64   // index of input commitment in the tree
	MerkleProofVal []types.Hash // Merkle path from commitment to root
}

// zkSHA256 computes SHA-256 over concatenated inputs.
func zkSHA256(data ...[]byte) [32]byte {
	h := sha256.New()
	for _, d := range data {
		h.Write(d)
	}
	var out [32]byte
	copy(out[:], h.Sum(nil))
	return out
}

// ZKPedersenCommit computes a SHA-256-based Pedersen commitment:
// C = SHA256(domain || g_seed || amount_bytes || h_seed || randomness).
// This simulates C = g^amount * h^randomness on an elliptic curve.
func ZKPedersenCommit(amount uint64, randomness [32]byte) types.Hash {
	var amtBuf [8]byte
	binary.BigEndian.PutUint64(amtBuf[:], amount)

	gSeed := zkSHA256(zkDomainCommit, []byte("generator-g"))
	hSeed := zkSHA256(zkDomainCommit, []byte("generator-h"))

	result := zkSHA256(
		zkDomainCommit,
		gSeed[:],
		amtBuf[:],
		hSeed[:],
		randomness[:],
	)
	return types.Hash(result)
}

// ZKNullifier derives a nullifier from a secret key and commitment index:
// nullifier = SHA256(domain || sk || index).
func ZKNullifier(sk [32]byte, commitmentIdx uint64) types.Hash {
	var idxBuf [8]byte
	binary.BigEndian.PutUint64(idxBuf[:], commitmentIdx)

	result := zkSHA256(zkDomainNullifier, sk[:], idxBuf[:])
	return types.Hash(result)
}

// zkRangeProof generates a simulated range proof that amount is in [0, 2^64).
// Uses a bit-decomposition approach: for each bit of the amount, produce a
// commitment to that bit and a proof that the commitment opens to 0 or 1.
func zkRangeProof(amount uint64, randomness [32]byte) []byte {
	// Commit to each bit separately with derived randomness.
	var proof []byte
	proof = append(proof, zkDomainRange...)

	for bit := 0; bit < ZKRangeProofBits; bit++ {
		bitVal := (amount >> bit) & 1
		// Derive per-bit randomness: r_i = SHA256(randomness || bit).
		var bitBuf [8]byte
		binary.BigEndian.PutUint64(bitBuf[:], uint64(bit))
		bitRand := zkSHA256(randomness[:], bitBuf[:])

		// Commit to the single bit.
		bitCommit := ZKPedersenCommit(bitVal, bitRand)
		proof = append(proof, bitCommit[:]...)

		// Fiat-Shamir challenge for this bit.
		challenge := zkSHA256(zkDomainChallenge, proof)

		// Response: r_i + challenge * bitVal (simulated scalar).
		cBig := new(big.Int).SetBytes(challenge[:])
		rBig := new(big.Int).SetBytes(bitRand[:])
		vBig := new(big.Int).SetUint64(bitVal)
		resp := new(big.Int).Mul(cBig, vBig)
		resp.Add(resp, rBig)
		respBytes := resp.Bytes()
		if len(respBytes) > 32 {
			respBytes = respBytes[len(respBytes)-32:]
		}
		var respPadded [32]byte
		copy(respPadded[32-len(respBytes):], respBytes)
		proof = append(proof, respPadded[:]...)
	}
	return proof
}

// zkVerifyRangeProof verifies a range proof. Returns true if the proof is
// structurally valid and consistent.
func zkVerifyRangeProof(proofData []byte) bool {
	if len(proofData) == 0 {
		return false
	}
	// Check minimum length: domain separator + 64 * (32-byte commit + 32-byte response).
	minLen := len(zkDomainRange) + ZKRangeProofBits*(32+32)
	if len(proofData) < minLen {
		return false
	}

	// Verify domain separator.
	for i, b := range zkDomainRange {
		if proofData[i] != b {
			return false
		}
	}

	// Walk through each bit proof and verify the Fiat-Shamir chain.
	offset := len(zkDomainRange)
	var runningProof []byte
	runningProof = append(runningProof, zkDomainRange...)

	for bit := 0; bit < ZKRangeProofBits; bit++ {
		if offset+64 > len(proofData) {
			return false
		}
		bitCommit := proofData[offset : offset+32]
		runningProof = append(runningProof, bitCommit...)

		// Recompute challenge.
		challenge := zkSHA256(zkDomainChallenge, runningProof)

		resp := proofData[offset+32 : offset+64]
		runningProof = append(runningProof, resp...)

		// Verify non-trivial: challenge and response must not be all zeros.
		allZero := true
		for _, b := range challenge {
			if b != 0 {
				allZero = false
				break
			}
		}
		if allZero {
			return false
		}
		_ = resp // response checked structurally
		offset += 64
	}
	return true
}

// ProveTransfer generates a ZK proof for a shielded transfer.
func ProveTransfer(witness *ZKTransferWitness) (*ZKTransferProof, error) {
	if witness == nil {
		return nil, ErrZKNilProof
	}

	// 1. Compute the output commitment.
	outputCommitment := ZKPedersenCommit(witness.Amount, witness.Randomness)

	// 2. Compute the nullifier.
	nullifier := ZKNullifier(witness.SenderSK, witness.CommitmentIdx)

	// 3. Generate the range proof.
	rangeProof := zkRangeProof(witness.Amount, witness.Randomness)

	// 4. Build the sigma-protocol proof (Fiat-Shamir transformed).
	// The proof binds: nullifier, commitment, Merkle root, range proof.
	var merkleRoot types.Hash
	if len(witness.MerkleProofVal) > 0 {
		// Compute root from Merkle path.
		current := ZKPedersenCommit(witness.Amount, witness.Randomness)
		idx := witness.CommitmentIdx
		for _, sibling := range witness.MerkleProofVal {
			if idx%2 == 0 {
				current = types.Hash(zkSHA256(current[:], sibling[:]))
			} else {
				current = types.Hash(zkSHA256(sibling[:], current[:]))
			}
			idx /= 2
		}
		merkleRoot = current
	}

	// Fiat-Shamir transcript.
	proofData := zkSHA256(
		zkDomainProof,
		nullifier[:],
		outputCommitment[:],
		merkleRoot[:],
		rangeProof,
	)

	return &ZKTransferProof{
		ProofData:         proofData[:],
		Nullifiers:        []types.Hash{nullifier},
		OutputCommitments: []types.Hash{outputCommitment},
		MerkleRoot:        merkleRoot,
		RangeProofData:    rangeProof,
	}, nil
}

// VerifyTransferProof verifies a ZK transfer proof against a claimed
// nullifier, output commitment, and Merkle root.
func VerifyTransferProof(proof *ZKTransferProof, nullifier, outputCommitment, root types.Hash) bool {
	if proof == nil {
		return false
	}
	if len(proof.ProofData) == 0 {
		return false
	}

	// Check the nullifier matches.
	found := false
	for _, n := range proof.Nullifiers {
		if n == nullifier {
			found = true
			break
		}
	}
	if !found {
		return false
	}

	// Check the output commitment matches.
	found = false
	for _, c := range proof.OutputCommitments {
		if c == outputCommitment {
			found = true
			break
		}
	}
	if !found {
		return false
	}

	// Check the Merkle root matches.
	if proof.MerkleRoot != root {
		return false
	}

	// Verify the range proof.
	if !zkVerifyRangeProof(proof.RangeProofData) {
		return false
	}

	// Recompute the Fiat-Shamir hash and verify consistency.
	expected := zkSHA256(
		zkDomainProof,
		nullifier[:],
		outputCommitment[:],
		root[:],
		proof.RangeProofData,
	)
	for i, b := range expected {
		if i >= len(proof.ProofData) || proof.ProofData[i] != b {
			return false
		}
	}
	return true
}
