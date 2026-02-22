// randao.go implements RANDAO mix computation per the Ethereum beacon chain
// spec. RANDAO provides the source of randomness for validator shuffling,
// committee selection, and proposer election.
//
// Key functions:
//   - ProcessRandaoReveal: verifies BLS signature and XORs into mix
//   - GetRandaoMix: retrieves the RANDAO mix for a given epoch
//   - ComputeShuffledIndexRandao: swap-or-not shuffle using RANDAO seed
//   - ComputeRandaoSeed: derives seed from RANDAO mix + epoch + domain
package consensus

import (
	"crypto/sha256"
	"encoding/binary"
	"errors"

	"github.com/eth2030/eth2030/crypto"
)

// RANDAO constants.
const (
	// RandaoDomainType is the domain type for RANDAO reveal signatures.
	RandaoDomainType uint32 = 0x02000000

	// RandaoShuffleRounds is the number of swap-or-not shuffle rounds.
	RandaoShuffleRounds = 90

	// RandaoMixesLength is the number of epochs of RANDAO mixes retained.
	RandaoMixesLength = 65536
)

// RANDAO errors.
var (
	ErrRandaoNilState      = errors.New("randao: nil beacon state")
	ErrRandaoInvalidReveal = errors.New("randao: invalid RANDAO reveal signature")
	ErrRandaoNoValidators  = errors.New("randao: no active validators")
	ErrRandaoInvalidIndex  = errors.New("randao: index out of range")
	ErrRandaoZeroCount     = errors.New("randao: zero index count for shuffle")
)

// RandaoManager manages RANDAO mix state and processes reveals.
type RandaoManager struct {
	// Mixes stores the RANDAO mix for each epoch, indexed by epoch % length.
	Mixes [RandaoMixesLength][32]byte

	// SlotsPerEpoch is needed for epoch calculation.
	SlotsPerEpoch uint64
}

// NewRandaoManager creates a new RANDAO manager with default config.
func NewRandaoManager(slotsPerEpoch uint64) *RandaoManager {
	if slotsPerEpoch == 0 {
		slotsPerEpoch = 32
	}
	return &RandaoManager{
		SlotsPerEpoch: slotsPerEpoch,
	}
}

// ProcessRandaoReveal verifies the RANDAO reveal BLS signature from the
// block proposer and XORs it into the current epoch's RANDAO mix.
//
// Per the spec:
//  1. The reveal is a BLS signature over the epoch using DOMAIN_RANDAO.
//  2. Verify: BLS_verify(proposer_pubkey, signing_root(epoch), reveal)
//  3. Update: mix[epoch] ^= sha256(reveal)
//
// Parameters:
//   - proposerPubkey: the BLS public key of the block proposer
//   - reveal: the 96-byte BLS signature (RANDAO reveal)
//   - epoch: the current epoch being processed
//   - forkVersion: current fork version for domain computation
//   - genesisRoot: genesis validators root
//   - verifySig: if true, performs BLS signature verification
func (rm *RandaoManager) ProcessRandaoReveal(
	proposerPubkey [48]byte,
	reveal [96]byte,
	epoch Epoch,
	forkVersion [4]byte,
	genesisRoot [32]byte,
	verifySig bool,
) error {
	if verifySig {
		// Compute signing root: sha256(epoch_root || domain).
		var epochRoot [32]byte
		binary.LittleEndian.PutUint64(epochRoot[:8], uint64(epoch))

		domain := DomainSeparation(DomainRandao, forkVersion, genesisRoot)
		signingRoot := ComputeSigningRoot(epochRoot, domain)

		if !crypto.BLSVerify(proposerPubkey, signingRoot[:], reveal) {
			return ErrRandaoInvalidReveal
		}
	}

	// XOR the hash of the reveal into the current mix.
	revealHash := sha256.Sum256(reveal[:])
	mixIdx := uint64(epoch) % RandaoMixesLength
	for i := 0; i < 32; i++ {
		rm.Mixes[mixIdx][i] ^= revealHash[i]
	}

	return nil
}

// GetRandaoMix returns the RANDAO mix for a given epoch.
func (rm *RandaoManager) GetRandaoMix(epoch Epoch) [32]byte {
	return rm.Mixes[uint64(epoch)%RandaoMixesLength]
}

// SetRandaoMix directly sets the RANDAO mix for an epoch (useful for
// initialization and testing).
func (rm *RandaoManager) SetRandaoMix(epoch Epoch, mix [32]byte) {
	rm.Mixes[uint64(epoch)%RandaoMixesLength] = mix
}

// CopyMixToNextEpoch copies the current epoch's mix to the next epoch slot.
// This is called during epoch transitions to initialize the next epoch's mix
// before any reveals are processed.
func (rm *RandaoManager) CopyMixToNextEpoch(epoch Epoch) {
	currentIdx := uint64(epoch) % RandaoMixesLength
	nextIdx := (uint64(epoch) + 1) % RandaoMixesLength
	rm.Mixes[nextIdx] = rm.Mixes[currentIdx]
}

// ComputeRandaoSeed derives a seed from the RANDAO mix, epoch, and domain
// type. This seed is used for committee assignment and proposer selection.
//
// seed = sha256(domain_type || epoch || randao_mix)
func (rm *RandaoManager) ComputeRandaoSeed(
	epoch Epoch,
	domainType uint32,
) [32]byte {
	mix := rm.GetRandaoMix(epoch)

	var buf [44]byte
	binary.LittleEndian.PutUint32(buf[:4], domainType)
	binary.LittleEndian.PutUint64(buf[4:12], uint64(epoch))
	copy(buf[12:44], mix[:])
	return sha256.Sum256(buf[:])
}

// ComputeShuffledIndexRandao implements the swap-or-not shuffle per the
// beacon chain spec. Given an index, total count, and seed, it returns
// the deterministically shuffled position.
//
// The algorithm performs RandaoShuffleRounds rounds of:
//  1. Compute pivot = hash(seed || round) mod count
//  2. Compute flip = (pivot + count - index) mod count
//  3. Compute bit from hash(seed || round || position/256)
//  4. If bit set, swap index with flip
func ComputeShuffledIndexRandao(
	index, indexCount uint64,
	seed [32]byte,
) (uint64, error) {
	if indexCount == 0 {
		return 0, ErrRandaoZeroCount
	}
	if index >= indexCount {
		return 0, ErrRandaoInvalidIndex
	}
	if indexCount == 1 {
		return 0, nil
	}

	cur := index
	for round := uint64(0); round < RandaoShuffleRounds; round++ {
		// Compute pivot: hash(seed || round_byte).
		var pivotInput [33]byte
		copy(pivotInput[:32], seed[:])
		pivotInput[32] = byte(round)
		pivotHash := sha256.Sum256(pivotInput[:])
		pivot := binary.LittleEndian.Uint64(pivotHash[:8]) % indexCount

		// Compute flip index.
		flip := (pivot + indexCount - cur) % indexCount

		// Position is max(cur, flip).
		pos := flip
		if cur > flip {
			pos = cur
		}

		// Compute source: hash(seed || round_byte || position/256).
		var srcInput [37]byte
		copy(srcInput[:32], seed[:])
		srcInput[32] = byte(round)
		binary.LittleEndian.PutUint32(srcInput[33:], uint32(pos/256))
		source := sha256.Sum256(srcInput[:])

		// Check the bit at position%256.
		byteIdx := (pos % 256) / 8
		bitIdx := pos % 8
		if (source[byteIdx]>>bitIdx)&1 != 0 {
			cur = flip
		}
	}
	return cur, nil
}

// ShuffleValidatorsRandao returns a fully shuffled copy of the given
// validator indices using the swap-or-not shuffle with the provided seed.
func ShuffleValidatorsRandao(
	indices []ValidatorIndex,
	seed [32]byte,
) ([]ValidatorIndex, error) {
	if len(indices) == 0 {
		return nil, ErrRandaoNoValidators
	}

	count := uint64(len(indices))
	result := make([]ValidatorIndex, count)
	for i := uint64(0); i < count; i++ {
		shuffled, err := ComputeShuffledIndexRandao(i, count, seed)
		if err != nil {
			return nil, err
		}
		result[i] = indices[shuffled]
	}
	return result, nil
}

// ComputeRandaoRevealHash computes the hash of a RANDAO reveal, which is
// XORed into the epoch mix. This is a convenience function for testing.
func ComputeRandaoRevealHash(reveal [96]byte) [32]byte {
	return sha256.Sum256(reveal[:])
}
