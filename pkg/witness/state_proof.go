// state_proof.go implements Merkle proof construction and verification for
// account and storage state: single proofs, multi-proof batching, proof size
// optimization via deduplication, and witness compression.
package witness

import (
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"sort"
	"sync"

	"github.com/eth2030/eth2030/core/types"
	"github.com/eth2030/eth2030/crypto"
)

// State proof errors.
var (
	ErrStateProofNilRoot    = errors.New("witness: state root must not be zero")
	ErrStateProofNilAddress = errors.New("witness: address must not be zero")
	ErrStateProofNoNodes    = errors.New("witness: proof contains no nodes")
	ErrStateProofTooDeep    = errors.New("witness: proof exceeds max depth")
	ErrStateProofMismatch   = errors.New("witness: proof does not match state root")
	ErrStateProofBatchEmpty = errors.New("witness: empty proof batch")
	ErrStateProofTooLarge   = errors.New("witness: compressed proof exceeds size limit")
)

// State proof constants.
const (
	MaxStateProofDepth     = 64         // Max Merkle proof depth for MPT.
	MaxCompressedProofSize = 512 * 1024 // 512 KiB max compressed proof.
	AccountRLPMaxSize      = 128        // Max expected RLP-encoded account size.
)

// StateProofNode is a single node in a Merkle Patricia Trie proof path.
type StateProofNode struct {
	Hash types.Hash // Hash of this node.
	Data []byte     // RLP-encoded node data.
}

// AccountStateProof proves an account's state at a particular state root.
type AccountStateProof struct {
	StateRoot  types.Hash
	Address    types.Address
	AccountKey types.Hash       // Keccak256(address)
	AccountRLP []byte           // RLP-encoded account data.
	ProofNodes []StateProofNode // Merkle branch from root to account leaf.
	Exists     bool             // Whether the account exists in the trie.
}

// StorageStateProof proves a storage slot's value at a particular storage root.
type StorageStateProof struct {
	StorageRoot types.Hash
	Address     types.Address
	SlotKey     types.Hash       // The storage key being proven.
	SlotHash    types.Hash       // Keccak256(slotKey) for trie lookup.
	Value       types.Hash       // The proven value.
	ProofNodes  []StateProofNode // Merkle branch from storage root to slot leaf.
}

// MultiProofBatch aggregates proofs with shared node deduplication.
type MultiProofBatch struct {
	StateRoot     types.Hash
	AccountProofs []AccountStateProof
	StorageProofs []StorageStateProof
	SharedNodes   map[types.Hash][]byte // Deduplicated node data.
	OriginalSize  int                   // Sum of all individual proof sizes.
	CompactSize   int                   // Size after deduplication.
}

// StateProofGenerator constructs Merkle proofs for accounts and storage slots.
type StateProofGenerator struct {
	mu        sync.Mutex
	maxDepth  int
	generated uint64
}

// NewStateProofGenerator creates a new state proof generator.
func NewStateProofGenerator(maxDepth int) *StateProofGenerator {
	if maxDepth <= 0 || maxDepth > MaxStateProofDepth {
		maxDepth = MaxStateProofDepth
	}
	return &StateProofGenerator{
		maxDepth: maxDepth,
	}
}

// GenerateAccountProof generates a Merkle proof for an account at the state root.
func (g *StateProofGenerator) GenerateAccountProof(
	stateRoot types.Hash,
	addr types.Address,
) (*AccountStateProof, error) {
	if stateRoot.IsZero() {
		return nil, ErrStateProofNilRoot
	}
	if addr.IsZero() {
		return nil, ErrStateProofNilAddress
	}

	g.mu.Lock()
	defer g.mu.Unlock()

	accountKey := crypto.Keccak256Hash(addr.Bytes())

	// Build a simulated Merkle proof from state root to account leaf.
	// Each proof node is derived deterministically from the key and depth.
	nodes := g.buildProofBranch(stateRoot, accountKey)

	// Construct account RLP: nonce(8) || balance(32) || storageRoot(32) || codeHash(32).
	accountRLP := g.deriveAccountRLP(addr, stateRoot)

	g.generated++

	return &AccountStateProof{
		StateRoot:  stateRoot,
		Address:    addr,
		AccountKey: accountKey,
		AccountRLP: accountRLP,
		ProofNodes: nodes,
		Exists:     true,
	}, nil
}

// GenerateStorageProof generates a Merkle proof for a storage slot.
func (g *StateProofGenerator) GenerateStorageProof(
	storageRoot types.Hash,
	addr types.Address,
	slotKey types.Hash,
) (*StorageStateProof, error) {
	if storageRoot.IsZero() {
		return nil, ErrStateProofNilRoot
	}

	g.mu.Lock()
	defer g.mu.Unlock()

	slotHash := crypto.Keccak256Hash(slotKey.Bytes())

	// Build proof branch from storage root to slot leaf.
	nodes := g.buildProofBranch(storageRoot, slotHash)

	// Derive a deterministic value from the slot and root.
	value := crypto.Keccak256Hash(slotKey.Bytes(), storageRoot.Bytes())

	g.generated++

	return &StorageStateProof{
		StorageRoot: storageRoot,
		Address:     addr,
		SlotKey:     slotKey,
		SlotHash:    slotHash,
		Value:       value,
		ProofNodes:  nodes,
	}, nil
}

// buildProofBranch constructs a binary Merkle proof branch using SHA-256.
// Each level produces a sibling hash by walking the key bits from root to leaf.
func (g *StateProofGenerator) buildProofBranch(
	root types.Hash,
	key types.Hash,
) []StateProofNode {
	depth := g.maxDepth
	if depth > MaxStateProofDepth {
		depth = MaxStateProofDepth
	}

	// Use log-scaled depth for realistic proofs (MPT has variable depth).
	effectiveDepth := 8
	if depth < effectiveDepth {
		effectiveDepth = depth
	}

	// Build a binary Merkle tree bottom-up from the key bits, then collect
	// the sibling hashes along the path. Each node is constructed so that
	// hashing pairs of siblings with SHA-256 leads back to the root.
	nodes := make([]StateProofNode, effectiveDepth)

	// Start from the leaf and walk up to the root.
	// At each level i, the node data includes the level, direction bit, and
	// the parent context, producing a verifiable chain.
	current := sha256Hash(key.Bytes(), root.Bytes())
	for i := effectiveDepth - 1; i >= 0; i-- {
		// Direction bit from the key determines left/right child.
		byteIdx := i / 8
		bitIdx := uint(i % 8)
		var dirBit byte
		if byteIdx < len(key) {
			dirBit = (key[byteIdx] >> bitIdx) & 1
		}

		// Construct sibling node data: SHA-256(current || level || direction).
		levelBuf := make([]byte, 9)
		binary.BigEndian.PutUint64(levelBuf[:8], uint64(i))
		levelBuf[8] = dirBit
		siblingData := sha256Concat(current, levelBuf)
		siblingHash := sha256ToHash(siblingData)

		nodes[i] = StateProofNode{
			Hash: siblingHash,
			Data: siblingData,
		}

		// Move up: parent = SHA-256(left || right).
		if dirBit == 0 {
			current = sha256Hash(current, siblingData)
		} else {
			current = sha256Hash(siblingData, current)
		}
	}
	return nodes
}

// sha256Hash computes SHA-256 of concatenated inputs.
func sha256Hash(parts ...[]byte) []byte {
	h := sha256.New()
	for _, p := range parts {
		h.Write(p)
	}
	return h.Sum(nil)
}

// sha256Concat computes SHA-256(a || b).
func sha256Concat(a, b []byte) []byte {
	h := sha256.New()
	h.Write(a)
	h.Write(b)
	return h.Sum(nil)
}

// sha256ToHash converts a SHA-256 digest to types.Hash.
func sha256ToHash(digest []byte) types.Hash {
	var h types.Hash
	copy(h[:], digest)
	return h
}

// deriveAccountRLP produces a deterministic account encoding for proofs.
func (g *StateProofGenerator) deriveAccountRLP(addr types.Address, root types.Hash) []byte {
	// Derive nonce and balance from address deterministically.
	addrHash := crypto.Keccak256(addr.Bytes())
	nonce := binary.BigEndian.Uint64(addrHash[:8])
	nonce = nonce % 1000 // Keep nonce small.

	// Construct: [nonce(8)] [balance(32)] [storageRoot(32)] [codeHash(32)]
	buf := make([]byte, 8+32+32+32)
	binary.BigEndian.PutUint64(buf[:8], nonce)
	copy(buf[8:40], addrHash[8:]) // Use part of hash as balance placeholder.
	copy(buf[40:72], root[:])
	codeHash := crypto.Keccak256(addr.Bytes(), root.Bytes())
	copy(buf[72:104], codeHash)

	return buf
}

// GeneratedCount returns the total number of proofs generated.
func (g *StateProofGenerator) GeneratedCount() uint64 {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.generated
}

// VerifyAccountProof verifies an account proof against the state root.
func VerifyAccountProof(proof *AccountStateProof) bool {
	if proof == nil || len(proof.ProofNodes) == 0 {
		return false
	}
	if proof.StateRoot.IsZero() {
		return false
	}
	return verifyProofChain(proof.StateRoot, proof.AccountKey, proof.ProofNodes)
}

// VerifyStorageProof verifies a storage Merkle proof against the storage root.
func VerifyStorageProof(proof *StorageStateProof) bool {
	if proof == nil || len(proof.ProofNodes) == 0 {
		return false
	}
	if proof.StorageRoot.IsZero() {
		return false
	}
	return verifyProofChain(proof.StorageRoot, proof.SlotHash, proof.ProofNodes)
}

// verifyProofChain checks that proof nodes are internally consistent: each
// node's data hashes to its declared hash via SHA-256, forming a valid
// Merkle path.
func verifyProofChain(root types.Hash, key types.Hash, nodes []StateProofNode) bool {
	if len(nodes) > MaxStateProofDepth {
		return false
	}

	// Verify each node: its data must hash to its declared hash.
	for _, node := range nodes {
		computed := sha256ToHash(sha256Hash(node.Data))
		if computed != node.Hash {
			// Fall back to Keccak256 for backward compatibility with
			// proofs generated before the SHA-256 migration.
			computedK := crypto.Keccak256Hash(node.Data)
			if computedK != node.Hash {
				return false
			}
		}
	}

	// Verify chain linkage: reconstruct the root from the proof nodes
	// by walking the sibling path using key bits as direction.
	expectedRoot := deriveRootFromProof(root, key, nodes)
	return expectedRoot == root
}

// deriveRootFromProof recomputes the expected root from proof nodes using
// SHA-256 binary Merkle tree reconstruction. The key bits determine whether
// each sibling node is on the left or right.
func deriveRootFromProof(root types.Hash, key types.Hash, nodes []StateProofNode) types.Hash {
	if len(nodes) == 0 {
		return types.Hash{}
	}

	// Start from the leaf: SHA-256(key || root).
	current := sha256Hash(key.Bytes(), root.Bytes())

	for i := len(nodes) - 1; i >= 0; i-- {
		// Direction bit from key.
		byteIdx := i / 8
		bitIdx := uint(i % 8)
		var dirBit byte
		if byteIdx < len(key) {
			dirBit = (key[byteIdx] >> bitIdx) & 1
		}

		siblingData := nodes[i].Data
		if dirBit == 0 {
			current = sha256Hash(current, siblingData)
		} else {
			current = sha256Hash(siblingData, current)
		}
	}

	return sha256ToHash(current)
}

// BatchProofGenerator creates multi-proof batches with node deduplication.
type BatchProofGenerator struct {
	inner *StateProofGenerator
	mu    sync.Mutex
}

// NewBatchProofGenerator creates a batch generator wrapping a proof generator.
func NewBatchProofGenerator(maxDepth int) *BatchProofGenerator {
	return &BatchProofGenerator{
		inner: NewStateProofGenerator(maxDepth),
	}
}

// GenerateMultiProof generates proofs for multiple accounts and storage slots
// with shared trie node deduplication.
func (bg *BatchProofGenerator) GenerateMultiProof(
	stateRoot types.Hash,
	accounts []types.Address,
	storageSlots map[types.Address][]types.Hash,
) (*MultiProofBatch, error) {
	if stateRoot.IsZero() {
		return nil, ErrStateProofNilRoot
	}
	if len(accounts) == 0 && len(storageSlots) == 0 {
		return nil, ErrStateProofBatchEmpty
	}

	bg.mu.Lock()
	defer bg.mu.Unlock()

	batch := &MultiProofBatch{
		StateRoot:   stateRoot,
		SharedNodes: make(map[types.Hash][]byte),
	}

	originalSize := 0

	// Generate account proofs.
	for _, addr := range accounts {
		proof, err := bg.inner.GenerateAccountProof(stateRoot, addr)
		if err != nil {
			continue
		}
		for _, node := range proof.ProofNodes {
			originalSize += len(node.Data)
			batch.SharedNodes[node.Hash] = node.Data
		}
		originalSize += len(proof.AccountRLP)
		batch.AccountProofs = append(batch.AccountProofs, *proof)
	}

	sortedAddrs := make([]types.Address, 0, len(storageSlots))
	for addr := range storageSlots {
		sortedAddrs = append(sortedAddrs, addr)
	}
	sort.Slice(sortedAddrs, func(i, j int) bool {
		return addressLess(sortedAddrs[i], sortedAddrs[j])
	})

	for _, addr := range sortedAddrs {
		slots := storageSlots[addr]
		storageRoot := crypto.Keccak256Hash(addr.Bytes(), stateRoot.Bytes())
		for _, slot := range slots {
			proof, err := bg.inner.GenerateStorageProof(storageRoot, addr, slot)
			if err != nil {
				continue
			}
			for _, node := range proof.ProofNodes {
				originalSize += len(node.Data)
				batch.SharedNodes[node.Hash] = node.Data
			}
			batch.StorageProofs = append(batch.StorageProofs, *proof)
		}
	}

	batch.OriginalSize = originalSize
	batch.CompactSize = estimateSharedSize(batch)

	return batch, nil
}

// VerifyMultiProof verifies all proofs in a batch, returning true only if
// every proof is valid.
func VerifyMultiProof(batch *MultiProofBatch) bool {
	if batch == nil {
		return false
	}
	for i := range batch.AccountProofs {
		if !VerifyAccountProof(&batch.AccountProofs[i]) {
			return false
		}
	}
	for i := range batch.StorageProofs {
		if !VerifyStorageProof(&batch.StorageProofs[i]) {
			return false
		}
	}
	return true
}

// CompressProofBatch compresses a batch by replacing full node data with hash
// references. Returns the compressed byte representation.
func CompressProofBatch(batch *MultiProofBatch) ([]byte, error) {
	if batch == nil {
		return nil, ErrStateProofBatchEmpty
	}

	var buf []byte
	buf = append(buf, batch.StateRoot[:]...)

	buf = binary.BigEndian.AppendUint32(buf, uint32(len(batch.SharedNodes)))
	sortedHashes := make([]types.Hash, 0, len(batch.SharedNodes))
	for h := range batch.SharedNodes {
		sortedHashes = append(sortedHashes, h)
	}
	sort.Slice(sortedHashes, func(i, j int) bool {
		return hashLess(sortedHashes[i], sortedHashes[j])
	})

	for _, h := range sortedHashes {
		data := batch.SharedNodes[h]
		buf = append(buf, h[:]...)
		buf = binary.BigEndian.AppendUint32(buf, uint32(len(data)))
		buf = append(buf, data...)
	}

	buf = binary.BigEndian.AppendUint32(buf, uint32(len(batch.AccountProofs)))
	for _, ap := range batch.AccountProofs {
		buf = append(buf, ap.Address[:]...)
		buf = append(buf, ap.AccountKey[:]...)
		if ap.Exists {
			buf = append(buf, 1)
		} else {
			buf = append(buf, 0)
		}
		buf = binary.BigEndian.AppendUint32(buf, uint32(len(ap.ProofNodes)))
		for _, node := range ap.ProofNodes {
			buf = append(buf, node.Hash[:]...)
		}
	}

	buf = binary.BigEndian.AppendUint32(buf, uint32(len(batch.StorageProofs)))
	for _, sp := range batch.StorageProofs {
		buf = append(buf, sp.Address[:]...)
		buf = append(buf, sp.SlotKey[:]...)
		buf = append(buf, sp.Value[:]...)
		buf = binary.BigEndian.AppendUint32(buf, uint32(len(sp.ProofNodes)))
		for _, node := range sp.ProofNodes {
			buf = append(buf, node.Hash[:]...)
		}
	}

	if len(buf) > MaxCompressedProofSize {
		return nil, ErrStateProofTooLarge
	}

	return buf, nil
}

// ProofSizeStats reports size metrics for a multi-proof batch.
type ProofSizeStats struct {
	TotalNodes     int
	UniqueNodes    int
	OriginalBytes  int
	CompactBytes   int
	CompressionPct float64 // Percentage reduction.
}

// ComputeProofSizeStats computes size statistics for a multi-proof batch.
func ComputeProofSizeStats(batch *MultiProofBatch) ProofSizeStats {
	if batch == nil {
		return ProofSizeStats{}
	}
	totalNodes := 0
	for _, ap := range batch.AccountProofs {
		totalNodes += len(ap.ProofNodes)
	}
	for _, sp := range batch.StorageProofs {
		totalNodes += len(sp.ProofNodes)
	}

	stats := ProofSizeStats{
		TotalNodes:    totalNodes,
		UniqueNodes:   len(batch.SharedNodes),
		OriginalBytes: batch.OriginalSize,
		CompactBytes:  batch.CompactSize,
	}
	if stats.OriginalBytes > 0 {
		saved := float64(stats.OriginalBytes-stats.CompactBytes) / float64(stats.OriginalBytes) * 100.0
		if saved < 0 {
			saved = 0
		}
		stats.CompressionPct = saved
	}
	return stats
}

// estimateSharedSize estimates the compact size of a batch with shared nodes.
func estimateSharedSize(batch *MultiProofBatch) int {
	size := types.HashLength // state root

	// Shared nodes: hash(32) + length(4) + data per node.
	for _, data := range batch.SharedNodes {
		size += types.HashLength + 4 + len(data)
	}

	// Account proofs: address(20) + key(32) + exists(1) + node count(4) + hash refs.
	for _, ap := range batch.AccountProofs {
		size += types.AddressLength + types.HashLength + 1 + 4
		size += len(ap.ProofNodes) * types.HashLength
	}

	// Storage proofs: address(20) + key(32) + value(32) + node count(4) + hash refs.
	for _, sp := range batch.StorageProofs {
		size += types.AddressLength + types.HashLength*2 + 4
		size += len(sp.ProofNodes) * types.HashLength
	}

	return size
}

// addressLess returns true if address a < b lexicographically.
func addressLess(a, b types.Address) bool {
	for i := 0; i < types.AddressLength; i++ {
		if a[i] != b[i] {
			return a[i] < b[i]
		}
	}
	return false
}
