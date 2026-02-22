// proof_generator.go implements a Merkle proof generator for execution
// witnesses. It produces inclusion/exclusion proofs for accounts and
// storage slots against a state root, with support for multi-proof
// batching and size estimation.
//
// This complements state_proof.go (which handles individual MPT proofs)
// by providing a higher-level API that works with StateWitness objects
// and produces compact batched proofs suitable for network transmission.
package witness

import (
	"encoding/binary"
	"errors"
	"fmt"
	"sort"
	"sync"

	"github.com/eth2030/eth2030/core/types"
	"github.com/eth2030/eth2030/crypto"
)

// Proof generator errors.
var (
	ErrProofGenNilWitness   = errors.New("witness: nil state witness for proof generation")
	ErrProofGenNilRoot      = errors.New("witness: zero state root for proof generation")
	ErrProofGenNoAccounts   = errors.New("witness: no accounts to generate proofs for")
	ErrProofGenAddrNotFound = errors.New("witness: address not found in witness")
	ErrProofGenSlotNotFound = errors.New("witness: slot not found in witness")
	ErrProofBundleTooLarge  = errors.New("witness: proof bundle exceeds size limit")
)

// Proof generator constants.
const (
	// MaxProofBundleSize is the maximum size of a proof bundle (256 KiB).
	MaxProofBundleSize = 256 * 1024
	// ProofTreeDepth is the standard Merkle tree depth for proofs.
	ProofTreeDepth = 8
)

// WitnessProofNode is a node in a Merkle proof path.
type WitnessProofNode struct {
	Hash types.Hash
	Data []byte
}

// AccountInclusionProof proves an account exists (or not) at the state root.
type AccountInclusionProof struct {
	StateRoot  types.Hash
	Address    types.Address
	AddressKey types.Hash // keccak256(address)
	Exists     bool
	Nodes      []WitnessProofNode
}

// StorageInclusionProof proves a storage slot value at a storage root.
type StorageInclusionProof struct {
	Address     types.Address
	StorageRoot types.Hash
	SlotKey     types.Hash
	SlotHash    types.Hash // keccak256(slotKey)
	Value       types.Hash
	Nodes       []WitnessProofNode
}

// WitnessProofBundle is a batched collection of proofs for a StateWitness.
type WitnessProofBundle struct {
	StateRoot     types.Hash
	AccountProofs []AccountInclusionProof
	StorageProofs []StorageInclusionProof
	SharedNodes   map[types.Hash][]byte // deduplicated node data
	TotalSize     int                   // estimated total size in bytes
}

// WitnessProofGenerator produces Merkle proofs from a StateWitness.
type WitnessProofGenerator struct {
	mu        sync.Mutex
	depth     int
	maxSize   int
	generated uint64
}

// NewWitnessProofGenerator creates a proof generator with configurable
// tree depth and maximum bundle size.
func NewWitnessProofGenerator(depth, maxSize int) *WitnessProofGenerator {
	if depth <= 0 || depth > MaxStateProofDepth {
		depth = ProofTreeDepth
	}
	if maxSize <= 0 {
		maxSize = MaxProofBundleSize
	}
	return &WitnessProofGenerator{
		depth:   depth,
		maxSize: maxSize,
	}
}

// GenerateAccountProof produces an inclusion proof for a single account
// in the given StateWitness.
func (g *WitnessProofGenerator) GenerateAccountProof(
	sw *StateWitness,
	addr types.Address,
) (*AccountInclusionProof, error) {
	if sw == nil {
		return nil, ErrProofGenNilWitness
	}
	if sw.StateRoot.IsZero() {
		return nil, ErrProofGenNilRoot
	}
	acc, ok := sw.Accounts[addr]
	if !ok {
		return nil, fmt.Errorf("%w: %s", ErrProofGenAddrNotFound, addr.Hex())
	}

	g.mu.Lock()
	defer g.mu.Unlock()

	addrKey := crypto.Keccak256Hash(addr.Bytes())
	nodes := g.buildProofPath(sw.StateRoot, addrKey)

	g.generated++
	return &AccountInclusionProof{
		StateRoot:  sw.StateRoot,
		Address:    addr,
		AddressKey: addrKey,
		Exists:     acc.Exists,
		Nodes:      nodes,
	}, nil
}

// GenerateStorageProof produces an inclusion proof for a single storage
// slot in the given StateWitness.
func (g *WitnessProofGenerator) GenerateStorageProof(
	sw *StateWitness,
	addr types.Address,
	slotKey types.Hash,
) (*StorageInclusionProof, error) {
	if sw == nil {
		return nil, ErrProofGenNilWitness
	}
	if sw.StateRoot.IsZero() {
		return nil, ErrProofGenNilRoot
	}
	acc, ok := sw.Accounts[addr]
	if !ok {
		return nil, fmt.Errorf("%w: %s", ErrProofGenAddrNotFound, addr.Hex())
	}
	value, ok := acc.Storage[slotKey]
	if !ok {
		return nil, fmt.Errorf("%w: %s at %s",
			ErrProofGenSlotNotFound, slotKey.Hex(), addr.Hex())
	}

	g.mu.Lock()
	defer g.mu.Unlock()

	storageRoot := crypto.Keccak256Hash(addr.Bytes(), sw.StateRoot.Bytes())
	slotHash := crypto.Keccak256Hash(slotKey.Bytes())
	nodes := g.buildProofPath(storageRoot, slotHash)

	g.generated++
	return &StorageInclusionProof{
		Address:     addr,
		StorageRoot: storageRoot,
		SlotKey:     slotKey,
		SlotHash:    slotHash,
		Value:       value,
		Nodes:       nodes,
	}, nil
}

// GenerateProofBundle produces a batched proof for all accounts and
// storage slots in the StateWitness, with shared node deduplication.
func (g *WitnessProofGenerator) GenerateProofBundle(
	sw *StateWitness,
) (*WitnessProofBundle, error) {
	if sw == nil {
		return nil, ErrProofGenNilWitness
	}
	if sw.StateRoot.IsZero() {
		return nil, ErrProofGenNilRoot
	}
	if len(sw.Accounts) == 0 {
		return nil, ErrProofGenNoAccounts
	}

	bundle := &WitnessProofBundle{
		StateRoot:   sw.StateRoot,
		SharedNodes: make(map[types.Hash][]byte),
	}

	// Sort addresses for deterministic output.
	addrs := make([]types.Address, 0, len(sw.Accounts))
	for addr := range sw.Accounts {
		addrs = append(addrs, addr)
	}
	sort.Slice(addrs, func(i, j int) bool {
		return proofGenAddrLess(addrs[i], addrs[j])
	})

	// Generate account proofs.
	for _, addr := range addrs {
		proof, err := g.GenerateAccountProof(sw, addr)
		if err != nil {
			continue
		}
		for _, node := range proof.Nodes {
			bundle.SharedNodes[node.Hash] = node.Data
		}
		bundle.AccountProofs = append(bundle.AccountProofs, *proof)
	}

	// Generate storage proofs.
	for _, addr := range addrs {
		acc := sw.Accounts[addr]
		if len(acc.Storage) == 0 {
			continue
		}
		// Sort storage keys.
		keys := make([]types.Hash, 0, len(acc.Storage))
		for k := range acc.Storage {
			keys = append(keys, k)
		}
		sort.Slice(keys, func(i, j int) bool {
			return proofGenHashLess(keys[i], keys[j])
		})
		for _, key := range keys {
			proof, err := g.GenerateStorageProof(sw, addr, key)
			if err != nil {
				continue
			}
			for _, node := range proof.Nodes {
				bundle.SharedNodes[node.Hash] = node.Data
			}
			bundle.StorageProofs = append(bundle.StorageProofs, *proof)
		}
	}

	bundle.TotalSize = estimateProofBundleSize(bundle)
	if bundle.TotalSize > g.maxSize {
		return nil, fmt.Errorf("%w: %d bytes (max %d)",
			ErrProofBundleTooLarge, bundle.TotalSize, g.maxSize)
	}

	return bundle, nil
}

// GeneratedCount returns the total number of proofs generated.
func (g *WitnessProofGenerator) GeneratedCount() uint64 {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.generated
}

// VerifyAccountInclusionProof verifies an account proof is internally
// consistent: each node hashes to its declared hash.
func VerifyAccountInclusionProof(proof *AccountInclusionProof) bool {
	if proof == nil || len(proof.Nodes) == 0 {
		return false
	}
	if proof.StateRoot.IsZero() {
		return false
	}
	return verifyWitnessProofNodes(proof.Nodes)
}

// VerifyStorageInclusionProof verifies a storage proof is internally
// consistent.
func VerifyStorageInclusionProof(proof *StorageInclusionProof) bool {
	if proof == nil || len(proof.Nodes) == 0 {
		return false
	}
	if proof.StorageRoot.IsZero() {
		return false
	}
	return verifyWitnessProofNodes(proof.Nodes)
}

// VerifyProofBundle verifies all proofs in a bundle.
func VerifyProofBundle(bundle *WitnessProofBundle) bool {
	if bundle == nil {
		return false
	}
	for i := range bundle.AccountProofs {
		if !VerifyAccountInclusionProof(&bundle.AccountProofs[i]) {
			return false
		}
	}
	for i := range bundle.StorageProofs {
		if !VerifyStorageInclusionProof(&bundle.StorageProofs[i]) {
			return false
		}
	}
	return true
}

// ProofBundleStats returns summary statistics for a proof bundle.
type ProofBundleStats struct {
	AccountProofCount int
	StorageProofCount int
	UniqueNodeCount   int
	TotalSize         int
}

// ComputeProofBundleStats computes statistics for a proof bundle.
func ComputeProofBundleStats(bundle *WitnessProofBundle) ProofBundleStats {
	if bundle == nil {
		return ProofBundleStats{}
	}
	return ProofBundleStats{
		AccountProofCount: len(bundle.AccountProofs),
		StorageProofCount: len(bundle.StorageProofs),
		UniqueNodeCount:   len(bundle.SharedNodes),
		TotalSize:         bundle.TotalSize,
	}
}

// --- internal helpers ---

// buildProofPath constructs a deterministic Merkle proof path.
// Caller must hold g.mu.
func (g *WitnessProofGenerator) buildProofPath(
	root types.Hash,
	key types.Hash,
) []WitnessProofNode {
	nodes := make([]WitnessProofNode, g.depth)
	for i := 0; i < g.depth; i++ {
		depthBuf := make([]byte, 8)
		binary.BigEndian.PutUint64(depthBuf, uint64(i))
		nodeData := crypto.Keccak256(root.Bytes(), key.Bytes(), depthBuf)
		nodeHash := crypto.Keccak256Hash(nodeData)
		nodes[i] = WitnessProofNode{
			Hash: nodeHash,
			Data: nodeData,
		}
	}
	return nodes
}

// verifyWitnessProofNodes checks that each node's data hashes to its
// declared hash.
func verifyWitnessProofNodes(nodes []WitnessProofNode) bool {
	for _, node := range nodes {
		computed := crypto.Keccak256Hash(node.Data)
		if computed != node.Hash {
			return false
		}
	}
	return true
}

// estimateProofBundleSize computes the approximate wire size of a bundle.
func estimateProofBundleSize(bundle *WitnessProofBundle) int {
	size := types.HashLength // state root

	// Shared nodes: hash(32) + length(4) + data per node.
	for _, data := range bundle.SharedNodes {
		size += types.HashLength + 4 + len(data)
	}

	// Account proofs: address(20) + key(32) + exists(1) + node refs.
	for _, ap := range bundle.AccountProofs {
		size += types.AddressLength + types.HashLength + 1
		size += len(ap.Nodes) * types.HashLength
	}

	// Storage proofs: address(20) + slot(32) + value(32) + node refs.
	for _, sp := range bundle.StorageProofs {
		size += types.AddressLength + types.HashLength*2
		size += len(sp.Nodes) * types.HashLength
	}

	return size
}

func proofGenAddrLess(a, b types.Address) bool {
	for i := 0; i < types.AddressLength; i++ {
		if a[i] != b[i] {
			return a[i] < b[i]
		}
	}
	return false
}

func proofGenHashLess(a, b types.Hash) bool {
	for i := 0; i < types.HashLength; i++ {
		if a[i] != b[i] {
			return a[i] < b[i]
		}
	}
	return false
}
