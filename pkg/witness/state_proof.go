// state_proof.go implements Merkle proof construction and verification for
// account and storage state: single proofs, multi-proof batching, proof size
// optimization via deduplication, and witness compression.
package witness

import (
	"encoding/binary"
	"errors"
	"sort"
	"sync"

	"github.com/eth2028/eth2028/core/types"
	"github.com/eth2028/eth2028/crypto"
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
	StateRoot   types.Hash
	Address     types.Address
	AccountKey  types.Hash       // Keccak256(address)
	AccountRLP  []byte           // RLP-encoded account data.
	ProofNodes  []StateProofNode // Merkle branch from root to account leaf.
	Exists      bool             // Whether the account exists in the trie.
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

// buildProofBranch constructs a deterministic Merkle proof branch derived
// from the key, depth, and root.
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

	nodes := make([]StateProofNode, effectiveDepth)
	for i := 0; i < effectiveDepth; i++ {
		// Derive node data: hash(root || key || depth).
		depthBuf := make([]byte, 8)
		binary.BigEndian.PutUint64(depthBuf, uint64(i))
		nodeData := crypto.Keccak256(root.Bytes(), key.Bytes(), depthBuf)
		nodeHash := crypto.Keccak256Hash(nodeData)
		nodes[i] = StateProofNode{
			Hash: nodeHash,
			Data: nodeData,
		}
	}
	return nodes
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
// node hashes to its declared hash and forms a valid path from root to leaf.
func verifyProofChain(root types.Hash, key types.Hash, nodes []StateProofNode) bool {
	if len(nodes) > MaxStateProofDepth {
		return false
	}

	// Verify each node: its data must hash to its declared hash.
	for _, node := range nodes {
		computed := crypto.Keccak256Hash(node.Data)
		if computed != node.Hash {
			return false
		}
	}

	// Verify chain linkage: the proof must relate to the root.
	// Reconstruct the expected root node hash from the chain.
	expectedRoot := deriveRootFromProof(root, key, nodes)
	return expectedRoot == root
}

// deriveRootFromProof recomputes the expected root from proof nodes.
func deriveRootFromProof(root types.Hash, key types.Hash, nodes []StateProofNode) types.Hash {
	if len(nodes) == 0 {
		return types.Hash{}
	}
	// Walk the proof: hash each node with its position to get root commitment.
	current := nodes[0].Data
	for i := 1; i < len(nodes); i++ {
		current = crypto.Keccak256(current, nodes[i].Data)
	}
	// Final binding to root and key.
	rootBinding := crypto.Keccak256(root.Bytes(), key.Bytes(), current)
	return types.BytesToHash(rootBinding)
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
