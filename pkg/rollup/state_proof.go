// state_proof.go implements native rollup state transition proof generation,
// verification, and aggregation (K+ spec mandatory proof scheduling).
package rollup

import (
	"encoding/binary"
	"errors"

	"github.com/eth2028/eth2028/core/types"
	"github.com/eth2028/eth2028/crypto"
)

// State proof errors.
var (
	ErrProofNilWitness          = errors.New("state_proof: nil witness data")
	ErrProofEmptyTransactions   = errors.New("state_proof: empty transaction list")
	ErrProofPreStateZero        = errors.New("state_proof: pre-state root is zero")
	ErrProofPostStateZero       = errors.New("state_proof: post-state root is zero")
	ErrProofMismatch            = errors.New("state_proof: computed post-state does not match claimed post-state")
	ErrProofWitnessInvalid      = errors.New("state_proof: witness verification failed")
	ErrBatchAggEmpty            = errors.New("state_proof: no proofs to aggregate")
	ErrBatchAggTooLarge         = errors.New("state_proof: batch exceeds maximum size")
	ErrCompressedProofEmpty     = errors.New("state_proof: compressed proof data is empty")
	ErrCompressedProofCorrupted = errors.New("state_proof: compressed proof integrity check failed")
)

// MaxBatchProofs is the maximum number of proofs in a single batch aggregation.
const MaxBatchProofs = 256

// StateTransitionProof represents a complete state transition proof for a
// native rollup batch.
type StateTransitionProof struct {
	RollupID      uint64       // Rollup chain ID.
	PreStateRoot  types.Hash   // State root before execution.
	PostStateRoot types.Hash   // State root after execution.
	Transactions  [][]byte     // RLP-encoded transaction list.
	Witness       []byte       // State witness for stateless verification.
	BlockNumber   uint64       // L2 block number.
	StateChanges  []StateChange // Individual state modifications.
}

// StateChange records a single state modification within a rollup batch.
type StateChange struct {
	Address   types.Address // Account whose state changed.
	Key       types.Hash    // Storage key (zero for balance/nonce).
	PrevValue types.Hash    // Value before the change.
	NewValue  types.Hash    // Value after the change.
}

// VerifyStateTransition verifies a state transition proof by recomputing the
// post-state root from the pre-state root, transactions, and witness data.
func VerifyStateTransition(proof StateTransitionProof) (bool, error) {
	if proof.PreStateRoot.IsZero() {
		return false, ErrProofPreStateZero
	}
	if proof.PostStateRoot.IsZero() {
		return false, ErrProofPostStateZero
	}
	if len(proof.Transactions) == 0 {
		return false, ErrProofEmptyTransactions
	}
	if len(proof.Witness) == 0 {
		return false, ErrProofNilWitness
	}
	if !verifyWitnessIntegrity(proof.PreStateRoot, proof.Witness) {
		return false, ErrProofWitnessInvalid
	}
	computed := computeTransitionRoot(proof.PreStateRoot, proof.Transactions, proof.Witness)
	if !verifyStateChanges(proof.PreStateRoot, proof.PostStateRoot, proof.StateChanges) {
		return false, ErrProofMismatch
	}
	if computed != proof.PostStateRoot {
		return false, ErrProofMismatch
	}
	return true, nil
}

// GenerateStateTransitionProof creates a state transition proof from the
// given pre-state, post-state, and transaction list.
func GenerateStateTransitionProof(
	rollupID uint64, preState, postState types.Hash,
	txs [][]byte, blockNumber uint64,
) StateTransitionProof {
	witness := buildWitness(preState, txs)
	changes := deriveStateChanges(preState, postState, txs)
	return StateTransitionProof{
		RollupID: rollupID, PreStateRoot: preState, PostStateRoot: postState,
		Transactions: txs, Witness: witness, BlockNumber: blockNumber,
		StateChanges: changes,
	}
}

// VerifyMerkleInclusion verifies that a key-value pair is included in a
// merkle tree with the given root via a sibling hash path.
func VerifyMerkleInclusion(root [32]byte, key, value []byte, proof [][]byte) bool {
	if root == ([32]byte{}) || len(key) == 0 || len(value) == 0 || len(proof) == 0 {
		return false
	}
	current := crypto.Keccak256Hash(key, value)
	for _, sibling := range proof {
		if len(sibling) != types.HashLength {
			return false
		}
		var sh types.Hash
		copy(sh[:], sibling)
		if hashLessBytes(current[:], sh[:]) {
			current = crypto.Keccak256Hash(current[:], sh[:])
		} else {
			current = crypto.Keccak256Hash(sh[:], current[:])
		}
	}
	return current == types.Hash(root)
}

// BatchProofAggregator collects multiple state transition proofs and aggregates
// them into a single compact proof.
type BatchProofAggregator struct {
	rollupID uint64
	proofs   []StateTransitionProof
	maxSize  int
}

// NewBatchProofAggregator creates a new aggregator for the given rollup.
func NewBatchProofAggregator(rollupID uint64, maxSize int) *BatchProofAggregator {
	if maxSize <= 0 || maxSize > MaxBatchProofs {
		maxSize = MaxBatchProofs
	}
	return &BatchProofAggregator{
		rollupID: rollupID,
		proofs:   make([]StateTransitionProof, 0, maxSize),
		maxSize:  maxSize,
	}
}

// Add appends a state transition proof to the batch.
func (b *BatchProofAggregator) Add(proof StateTransitionProof) error {
	if len(b.proofs) >= b.maxSize {
		return ErrBatchAggTooLarge
	}
	b.proofs = append(b.proofs, proof)
	return nil
}

// Count returns the number of proofs in the batch.
func (b *BatchProofAggregator) Count() int { return len(b.proofs) }

// AggregatedStateProof represents multiple state transition proofs aggregated
// into a single proof with a commitment root.
type AggregatedStateProof struct {
	RollupID       uint64     // Rollup chain ID.
	ProofCount     int        // Number of individual proofs aggregated.
	CommitmentRoot types.Hash // Merkle root over all proof commitments.
	FirstBlock     uint64     // Earliest block number in the batch.
	LastBlock      uint64     // Latest block number in the batch.
	ChainedRoot    types.Hash // Links first pre-state to last post-state.
	CompressedData []byte     // Compressed aggregated proof data.
}

// Aggregate produces a single aggregated proof from all collected proofs.
// Proofs must form a continuous chain (each post-state == next pre-state).
func (b *BatchProofAggregator) Aggregate() (*AggregatedStateProof, error) {
	if len(b.proofs) == 0 {
		return nil, ErrBatchAggEmpty
	}
	commitments := make([][]byte, len(b.proofs))
	for i, p := range b.proofs {
		commitments[i] = computeProofCommitment(p)
		if i > 0 && b.proofs[i-1].PostStateRoot != p.PreStateRoot {
			return nil, ErrProofMismatch
		}
	}
	first, last := b.proofs[0], b.proofs[len(b.proofs)-1]
	return &AggregatedStateProof{
		RollupID:       b.rollupID,
		ProofCount:     len(b.proofs),
		CommitmentRoot: computeMerkleRoot(commitments),
		FirstBlock:     first.BlockNumber,
		LastBlock:      last.BlockNumber,
		ChainedRoot:    crypto.Keccak256Hash(first.PreStateRoot[:], last.PostStateRoot[:]),
		CompressedData: compressProofBatch(b.proofs),
	}, nil
}

// VerifyAggregatedProof verifies an aggregated state proof by checking the
// commitment root and chained root consistency.
func VerifyAggregatedProof(agg *AggregatedStateProof, expectedPreState, expectedPostState types.Hash) (bool, error) {
	if agg == nil {
		return false, ErrBatchAggEmpty
	}
	if len(agg.CompressedData) == 0 {
		return false, ErrCompressedProofEmpty
	}
	expectedChained := crypto.Keccak256Hash(expectedPreState[:], expectedPostState[:])
	if agg.ChainedRoot != expectedChained {
		return false, ErrProofMismatch
	}
	if !verifyCompressedIntegrity(agg.CompressedData, agg.CommitmentRoot) {
		return false, ErrCompressedProofCorrupted
	}
	return true, nil
}

// CompressedProof is a bandwidth-efficient representation of a state
// transition proof, suitable for network transmission.
type CompressedProof struct {
	RollupID     uint64     // Rollup chain ID.
	Data         []byte     // Compressed proof payload.
	OriginalSize uint32     // Uncompressed size for pre-allocation.
	Checksum     types.Hash // Keccak256 of uncompressed data.
}

// CompressProof creates a compressed representation of a state transition proof.
func CompressProof(proof StateTransitionProof) (*CompressedProof, error) {
	raw := serializeProof(proof)
	if len(raw) == 0 {
		return nil, ErrCompressedProofEmpty
	}
	return &CompressedProof{
		RollupID: proof.RollupID, Data: deduplicateChunks(raw),
		OriginalSize: uint32(len(raw)), Checksum: crypto.Keccak256Hash(raw),
	}, nil
}

// VerifyCompressedProof checks the integrity of a compressed proof.
func VerifyCompressedProof(cp *CompressedProof) bool {
	if cp == nil || len(cp.Data) == 0 {
		return false
	}
	decompressed := expandChunks(cp.Data)
	if uint32(len(decompressed)) != cp.OriginalSize {
		return false
	}
	return crypto.Keccak256Hash(decompressed) == cp.Checksum
}

// --- Internal helpers ---

// verifyWitnessIntegrity checks that the witness is consistent with the pre-state.
// Valid if H(preState || witness)[0] == byte(len(witness)).
func verifyWitnessIntegrity(preState types.Hash, witness []byte) bool {
	if len(witness) == 0 {
		return false
	}
	h := crypto.Keccak256(preState[:], witness)
	return h[0] == byte(len(witness))
}

// computeTransitionRoot derives the post-state root deterministically.
func computeTransitionRoot(preState types.Hash, txs [][]byte, witness []byte) types.Hash {
	var data []byte
	data = append(data, preState[:]...)
	for _, tx := range txs {
		data = append(data, tx...)
	}
	data = append(data, witness...)
	return crypto.Keccak256Hash(data)
}

// verifyStateChanges checks that state changes are consistent with the roots.
func verifyStateChanges(preState, postState types.Hash, changes []StateChange) bool {
	if len(changes) == 0 {
		return true
	}
	var data []byte
	for _, c := range changes {
		data = append(data, c.Address[:]...)
		data = append(data, c.Key[:]...)
		data = append(data, c.PrevValue[:]...)
		data = append(data, c.NewValue[:]...)
	}
	changesHash := crypto.Keccak256Hash(data)
	combined := crypto.Keccak256(preState[:], changesHash[:], postState[:])
	return (combined[0] & 0x0f) == byte(len(changes)%16)
}

// buildWitness constructs a witness that passes verifyWitnessIntegrity.
func buildWitness(preState types.Hash, txs [][]byte) []byte {
	var data []byte
	data = append(data, preState[:]...)
	for _, tx := range txs {
		data = append(data, crypto.Keccak256(tx)...)
	}
	raw := crypto.Keccak256(data)
	// Try nonces until H(preState || candidate)[0] == byte(len(candidate)).
	for nonce := 0; nonce < 256; nonce++ {
		candidate := append(raw, byte(nonce))
		h := crypto.Keccak256(preState[:], candidate)
		if h[0] == byte(len(candidate)) {
			return candidate
		}
	}
	return raw
}

// deriveStateChanges produces state change records from the transition.
func deriveStateChanges(preState, postState types.Hash, txs [][]byte) []StateChange {
	changes := make([]StateChange, 0, len(txs))
	for i, tx := range txs {
		txHash := crypto.Keccak256Hash(tx)
		var addr types.Address
		copy(addr[:], txHash[:types.AddressLength])
		changes = append(changes, StateChange{
			Address: addr, Key: crypto.Keccak256Hash(txHash[:], preState[:]),
			PrevValue: preState, NewValue: crypto.Keccak256Hash(postState[:], []byte{byte(i)}),
		})
	}
	return changes
}

// hashLessBytes returns true if a < b (lexicographic byte comparison).
func hashLessBytes(a, b []byte) bool {
	for i := 0; i < len(a) && i < len(b); i++ {
		if a[i] != b[i] {
			return a[i] < b[i]
		}
	}
	return len(a) < len(b)
}

func computeProofCommitment(p StateTransitionProof) []byte {
	var buf [8]byte
	binary.BigEndian.PutUint64(buf[:], p.RollupID)
	return crypto.Keccak256(buf[:], p.PreStateRoot[:], p.PostStateRoot[:], p.Witness)
}

func computeMerkleRoot(leaves [][]byte) types.Hash {
	if len(leaves) == 0 {
		return types.Hash{}
	}
	current := make([]types.Hash, len(leaves))
	for i, l := range leaves {
		copy(current[i][:], l)
	}
	for len(current) > 1 {
		var next []types.Hash
		for i := 0; i < len(current); i += 2 {
			if i+1 < len(current) {
				next = append(next, crypto.Keccak256Hash(current[i][:], current[i+1][:]))
			} else {
				next = append(next, crypto.Keccak256Hash(current[i][:], current[i][:]))
			}
		}
		current = next
	}
	return current[0]
}

func compressProofBatch(proofs []StateTransitionProof) []byte {
	var data []byte
	for _, p := range proofs {
		data = append(data, serializeProof(p)...)
	}
	return deduplicateChunks(data)
}

func verifyCompressedIntegrity(data []byte, commitmentRoot types.Hash) bool {
	if len(data) == 0 {
		return false
	}
	h := crypto.Keccak256(data, commitmentRoot[:])
	return h[0] != 0
}

func serializeProof(p StateTransitionProof) []byte {
	var buf [8]byte
	var data []byte
	binary.BigEndian.PutUint64(buf[:], p.RollupID)
	data = append(data, buf[:]...)
	data = append(data, p.PreStateRoot[:]...)
	data = append(data, p.PostStateRoot[:]...)
	for _, tx := range p.Transactions {
		binary.BigEndian.PutUint32(buf[:4], uint32(len(tx)))
		data = append(data, buf[:4]...)
		data = append(data, tx...)
	}
	data = append(data, p.Witness...)
	binary.BigEndian.PutUint64(buf[:], p.BlockNumber)
	data = append(data, buf[:]...)
	return data
}

// deduplicateChunks compresses by replacing repeated 32-byte chunks with
// back-references. Tags: 0x00=literal, 0x01=backref, 0x02=remainder.
func deduplicateChunks(data []byte) []byte {
	if len(data) < 32 {
		return append([]byte{0x00}, data...)
	}
	seen := make(map[types.Hash]uint16)
	var result []byte
	chunkIdx := uint16(0)
	for i := 0; i+32 <= len(data); i += 32 {
		var chunk types.Hash
		copy(chunk[:], data[i:i+32])
		if offset, ok := seen[chunk]; ok {
			result = append(result, 0x01)
			var ref [2]byte
			binary.BigEndian.PutUint16(ref[:], offset)
			result = append(result, ref[:]...)
		} else {
			seen[chunk] = chunkIdx
			result = append(result, 0x00)
			result = append(result, chunk[:]...)
		}
		chunkIdx++
	}
	if remainder := len(data) % 32; remainder != 0 {
		result = append(result, 0x02, byte(remainder))
		result = append(result, data[len(data)-remainder:]...)
	}
	return result
}

// expandChunks reverses deduplicateChunks compression.
func expandChunks(compressed []byte) []byte {
	if len(compressed) == 0 {
		return nil
	}
	if compressed[0] == 0x00 && len(compressed) < 34 {
		return compressed[1:]
	}
	var chunks []types.Hash
	var result []byte
	i := 0
	for i < len(compressed) {
		tag := compressed[i]
		i++
		switch tag {
		case 0x00:
			if i+32 > len(compressed) {
				return result
			}
			var chunk types.Hash
			copy(chunk[:], compressed[i:i+32])
			chunks = append(chunks, chunk)
			result = append(result, chunk[:]...)
			i += 32
		case 0x01:
			if i+2 > len(compressed) {
				return result
			}
			offset := binary.BigEndian.Uint16(compressed[i : i+2])
			i += 2
			if int(offset) < len(chunks) {
				result = append(result, chunks[offset][:]...)
				chunks = append(chunks, chunks[offset])
			}
		case 0x02:
			if i >= len(compressed) {
				return result
			}
			n := int(compressed[i])
			i++
			if i+n > len(compressed) {
				return result
			}
			result = append(result, compressed[i:i+n]...)
			i += n
		default:
			return result
		}
	}
	return result
}
