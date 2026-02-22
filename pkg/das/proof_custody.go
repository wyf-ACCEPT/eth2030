// proof_custody.go implements the proof-of-custody scheme for blob data
// availability, part of the DAS (Data Availability Sampling) subsystem.
// This targets the "proof custody" milestone in the Ethereum 2028 roadmap
// (Data Layer, Throughput track).
//
// The scheme requires nodes to post custody bonds, prove they hold specific
// data, and face slashing if they fail custody challenges.
package das

import (
	"encoding/binary"
	"errors"
	"fmt"
	"sync"

	"github.com/eth2030/eth2030/core/types"
	"github.com/eth2030/eth2030/crypto"
)

// Proof custody errors.
var (
	ErrBondAlreadyRegistered = errors.New("das: bond already registered")
	ErrBondNotFound          = errors.New("das: bond not found")
	ErrBondExpired           = errors.New("das: bond has expired")
	ErrStakeTooLow           = errors.New("das: stake below minimum")
	ErrNilBond               = errors.New("das: bond is nil")
	ErrNilChallenge          = errors.New("das: challenge is nil")
	ErrChallengeDeadlinePast = errors.New("das: challenge deadline has passed")
	ErrDataEmpty             = errors.New("das: data is empty")
	ErrInvalidProof          = errors.New("das: invalid proof")
)

// ProofCustodyConfig configures the proof-of-custody scheme.
type ProofCustodyConfig struct {
	// MinStake is the minimum bond stake in gwei.
	MinStake uint64

	// BondDuration is the number of epochs a bond remains active.
	BondDuration uint64

	// ChallengeWindow is the number of epochs a challenge remains valid.
	ChallengeWindow uint64

	// SlashingPenalty is the base penalty in gwei for failed custody.
	SlashingPenalty uint64
}

// DefaultProofCustodyConfig returns a sensible default configuration.
func DefaultProofCustodyConfig() ProofCustodyConfig {
	return ProofCustodyConfig{
		MinStake:        32_000_000_000, // 32 ETH in gwei
		BondDuration:    256,
		ChallengeWindow: 64,
		SlashingPenalty: 1_000_000_000, // 1 ETH in gwei
	}
}

// CustodyBond represents a node's commitment to custody blob data.
type CustodyBond struct {
	// NodeID identifies the bonded node.
	NodeID types.Hash

	// Epoch is the epoch at which the bond was created.
	Epoch uint64

	// Commitment is a hash commitment derived from the node ID and epoch.
	Commitment types.Hash

	// Stake is the amount of stake in gwei backing this bond.
	Stake uint64

	// ExpiresAt is the epoch at which this bond expires.
	ExpiresAt uint64
}

// DataHeldProof proves that a node holds specific data for a bonded index.
type DataHeldProof struct {
	// BondID identifies the bond this proof is for.
	BondID types.Hash

	// DataIndex is the index of the data being proven.
	DataIndex uint64

	// DataHash is the keccak256 hash of the data.
	DataHash types.Hash

	// Proof is the proof bytes: keccak256(bondID || dataIndex || dataHash).
	Proof []byte

	// Timestamp is the time of proof generation (epoch number).
	Timestamp uint64
}

// CustodyBondChallenge is issued to a node to prove it holds custody data.
// This is separate from the existing CustodyChallenge in custody_proof.go
// which targets the v1 column-based system.
type CustodyBondChallenge struct {
	// BondID identifies the bond being challenged.
	BondID types.Hash

	// DataIndex is the data index the challenged node must prove.
	DataIndex uint64

	// Deadline is the epoch by which the node must respond.
	Deadline uint64

	// ChallengerID identifies the challenger.
	ChallengerID types.Hash
}

// SlashResult holds the outcome of a slashing attempt.
type SlashResult struct {
	// BondID identifies the bond that was slashed.
	BondID types.Hash

	// Slashed indicates whether slashing occurred.
	Slashed bool

	// PenaltyAmount is the penalty in gwei.
	PenaltyAmount uint64

	// Reason explains why slashing occurred (or why it didn't).
	Reason string
}

// ProofCustodyScheme implements proof-of-custody for DAS blob data.
// It is safe for concurrent use.
type ProofCustodyScheme struct {
	config ProofCustodyConfig
	mu     sync.RWMutex
	bonds  map[types.Hash]*CustodyBond // keyed by bond commitment
}

// NewProofCustodyScheme creates a new proof-of-custody scheme.
func NewProofCustodyScheme(config ProofCustodyConfig) *ProofCustodyScheme {
	if config.MinStake == 0 {
		config.MinStake = 32_000_000_000
	}
	if config.BondDuration == 0 {
		config.BondDuration = 256
	}
	if config.ChallengeWindow == 0 {
		config.ChallengeWindow = 64
	}
	if config.SlashingPenalty == 0 {
		config.SlashingPenalty = 1_000_000_000
	}
	return &ProofCustodyScheme{
		config: config,
		bonds:  make(map[types.Hash]*CustodyBond),
	}
}

// GenerateCustodyBond generates a new custody bond commitment for a node
// at the given epoch.
func (pcs *ProofCustodyScheme) GenerateCustodyBond(nodeID types.Hash, epoch uint64) (*CustodyBond, error) {
	commitment := computeBondCommitment(nodeID, epoch)

	return &CustodyBond{
		NodeID:     nodeID,
		Epoch:      epoch,
		Commitment: commitment,
		Stake:      pcs.config.MinStake,
		ExpiresAt:  epoch + pcs.config.BondDuration,
	}, nil
}

// RegisterBond registers a custody bond. Returns an error if the bond is
// already registered or the stake is below the minimum.
func (pcs *ProofCustodyScheme) RegisterBond(bond *CustodyBond) error {
	if bond == nil {
		return ErrNilBond
	}
	if bond.Stake < pcs.config.MinStake {
		return fmt.Errorf("%w: %d < %d", ErrStakeTooLow, bond.Stake, pcs.config.MinStake)
	}

	pcs.mu.Lock()
	defer pcs.mu.Unlock()

	if _, exists := pcs.bonds[bond.Commitment]; exists {
		return ErrBondAlreadyRegistered
	}
	pcs.bonds[bond.Commitment] = bond
	return nil
}

// GetActiveBonds returns all bonds that are active (not expired) at the
// given epoch.
func (pcs *ProofCustodyScheme) GetActiveBonds(epoch uint64) []*CustodyBond {
	pcs.mu.RLock()
	defer pcs.mu.RUnlock()

	var active []*CustodyBond
	for _, bond := range pcs.bonds {
		if bond.ExpiresAt > epoch && bond.Epoch <= epoch {
			active = append(active, bond)
		}
	}
	return active
}

// ProveDataHeld generates a proof that the node holds the specified data
// for the given bond.
func (pcs *ProofCustodyScheme) ProveDataHeld(bond *CustodyBond, dataIndex uint64, data []byte) (*DataHeldProof, error) {
	if bond == nil {
		return nil, ErrNilBond
	}
	if len(data) == 0 {
		return nil, ErrDataEmpty
	}

	dataHash := crypto.Keccak256Hash(data)
	proof := computeDataHeldProof(bond.Commitment, dataIndex, dataHash)

	return &DataHeldProof{
		BondID:    bond.Commitment,
		DataIndex: dataIndex,
		DataHash:  dataHash,
		Proof:     proof,
		Timestamp: bond.Epoch,
	}, nil
}

// VerifyDataHeld verifies a proof of data possession. The proof is valid if
// the proof bytes match keccak256(bondID || dataIndex || dataHash).
func (pcs *ProofCustodyScheme) VerifyDataHeld(proof *DataHeldProof) bool {
	if proof == nil {
		return false
	}
	if len(proof.Proof) != 32 {
		return false
	}
	if proof.DataHash.IsZero() {
		return false
	}

	expected := computeDataHeldProof(proof.BondID, proof.DataIndex, proof.DataHash)
	if len(expected) != len(proof.Proof) {
		return false
	}
	for i := range expected {
		if expected[i] != proof.Proof[i] {
			return false
		}
	}
	return true
}

// SlashNonCustodian slashes a bond for failing to prove custody in response
// to a challenge. The bond must be registered and the challenge must be valid.
func (pcs *ProofCustodyScheme) SlashNonCustodian(bond *CustodyBond, challenge *CustodyBondChallenge) (*SlashResult, error) {
	if bond == nil {
		return nil, ErrNilBond
	}
	if challenge == nil {
		return nil, ErrNilChallenge
	}

	pcs.mu.RLock()
	registered, exists := pcs.bonds[bond.Commitment]
	pcs.mu.RUnlock()

	if !exists {
		return &SlashResult{
			BondID:  bond.Commitment,
			Slashed: false,
			Reason:  "bond not registered",
		}, ErrBondNotFound
	}

	// Check the challenge is for this bond.
	if challenge.BondID != bond.Commitment {
		return &SlashResult{
			BondID:  bond.Commitment,
			Slashed: false,
			Reason:  "challenge bond ID mismatch",
		}, nil
	}

	// Check expiration.
	if registered.ExpiresAt <= registered.Epoch {
		return &SlashResult{
			BondID:  bond.Commitment,
			Slashed: false,
			Reason:  "bond expired",
		}, ErrBondExpired
	}

	// Apply slashing penalty.
	penalty := pcs.config.SlashingPenalty
	if penalty > registered.Stake {
		penalty = registered.Stake
	}

	// Remove the bond after slashing.
	pcs.mu.Lock()
	delete(pcs.bonds, bond.Commitment)
	pcs.mu.Unlock()

	return &SlashResult{
		BondID:        bond.Commitment,
		Slashed:       true,
		PenaltyAmount: penalty,
		Reason:        "failed custody challenge",
	}, nil
}

// BondCount returns the number of registered bonds. Thread-safe.
func (pcs *ProofCustodyScheme) BondCount() int {
	pcs.mu.RLock()
	defer pcs.mu.RUnlock()
	return len(pcs.bonds)
}

// --- Internal helpers ---

// computeBondCommitment derives a deterministic commitment from node ID and epoch.
func computeBondCommitment(nodeID types.Hash, epoch uint64) types.Hash {
	var epochBuf [8]byte
	binary.BigEndian.PutUint64(epochBuf[:], epoch)
	return crypto.Keccak256Hash(nodeID[:], epochBuf[:])
}

// computeDataHeldProof computes the proof hash: keccak256(bondID || dataIndex || dataHash).
func computeDataHeldProof(bondID types.Hash, dataIndex uint64, dataHash types.Hash) []byte {
	var indexBuf [8]byte
	binary.BigEndian.PutUint64(indexBuf[:], dataIndex)

	h := crypto.Keccak256(bondID[:], indexBuf[:], dataHash[:])
	return h
}
