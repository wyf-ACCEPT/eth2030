// bsn_beacon_state.go implements the full beacon state container covering
// phase0, Altair, and Bellatrix fields per the Ethereum consensus spec.
// Provides a BsnBeaconState struct with all necessary fields, deep copy,
// and hash tree root computation.
//
// This complements beacon_state.go (minimal) and beacon_state_v2.go
// (phase0-focused) by adding Altair/Bellatrix-era fields: participation
// flags, inactivity scores, sync committees, and execution payload headers.
package consensus

import (
	"crypto/sha256"
	"encoding/binary"
	"sync"

	"github.com/eth2030/eth2030/core/types"
	"github.com/eth2030/eth2030/ssz"
)

// BsnBeaconState constants.
const (
	BsnSlotsPerHistoricalRoot    = 8192
	BsnEpochsPerHistoricalVector = 65536
	BsnEpochsPerSlashingsVector  = 8192
	BsnHistoricalRootsLimit      = 16777216
	BsnValidatorRegistryLimit    = 1 << 40
	BsnMaxEth1DataVotes          = 2048
	BsnMaxAttestations           = 128
	BsnJustificationBitsLen      = 4
	BsnSyncCommitteeSize         = 512
)

// Participation flag indices per Altair spec.
const (
	BsnTimelySourceFlagIndex uint8 = 0
	BsnTimelyTargetFlagIndex uint8 = 1
	BsnTimelyHeadFlagIndex   uint8 = 2
)

// Participation flag weights per Altair spec.
const (
	BsnTimelySourceWeight uint64 = 14
	BsnTimelyTargetWeight uint64 = 26
	BsnTimelyHeadWeight   uint64 = 14
	BsnSyncRewardWeight   uint64 = 2
	BsnProposerWeight     uint64 = 8
	BsnWeightDenominator  uint64 = 64
)

// BsnFork represents a beacon chain fork version pair.
type BsnFork struct {
	PreviousVersion [4]byte
	CurrentVersion  [4]byte
	Epoch           Epoch
}

// BsnEth1Data represents ETH1 deposit data reference.
type BsnEth1Data struct {
	DepositRoot  [32]byte
	DepositCount uint64
	BlockHash    [32]byte
}

// BsnBeaconBlockHeader represents a beacon block header for state tracking.
type BsnBeaconBlockHeader struct {
	Slot          uint64
	ProposerIndex uint64
	ParentRoot    [32]byte
	StateRoot     [32]byte
	BodyRoot      [32]byte
}

// BsnSyncCommittee holds the sync committee pubkeys and the aggregate.
type BsnSyncCommittee struct {
	Pubkeys         [][48]byte // BsnSyncCommitteeSize entries
	AggregatePubkey [48]byte
}

// BsnExecutionPayloadHeader holds the execution layer payload summary
// for the Bellatrix merge onwards.
type BsnExecutionPayloadHeader struct {
	ParentHash       [32]byte
	FeeRecipient     [20]byte
	StateRoot        [32]byte
	ReceiptsRoot     [32]byte
	LogsBloom        [256]byte
	PrevRandao       [32]byte
	BlockNumber      uint64
	GasLimit         uint64
	GasUsed          uint64
	Timestamp        uint64
	ExtraData        []byte // variable length, max 32 bytes
	BaseFeePerGas    [32]byte
	BlockHash        [32]byte
	TransactionsRoot [32]byte
}

// BsnBeaconState is the comprehensive beacon state container implementing
// phase0, Altair, and Bellatrix fields per the Ethereum consensus spec.
type BsnBeaconState struct {
	mu sync.RWMutex

	// -- Genesis fields --
	GenesisTime           uint64
	GenesisValidatorsRoot [32]byte

	// -- Slot and fork --
	Slot uint64
	Fork BsnFork

	// -- Block header --
	LatestBlockHeader BsnBeaconBlockHeader

	// -- Circular buffers (8192 entries) --
	BlockRoots [BsnSlotsPerHistoricalRoot][32]byte
	StateRoots [BsnSlotsPerHistoricalRoot][32]byte

	// -- Historical roots --
	HistoricalRoots [][32]byte // max BsnHistoricalRootsLimit

	// -- ETH1 --
	Eth1Data         BsnEth1Data
	Eth1DataVotes    []BsnEth1Data // max BsnMaxEth1DataVotes
	Eth1DepositIndex uint64

	// -- Validator registry --
	Validators []*ValidatorV2
	Balances   []uint64

	// -- Randomness (65536 entries) --
	RandaoMixes [BsnEpochsPerHistoricalVector][32]byte

	// -- Slashings (8192 entries) --
	Slashings [BsnEpochsPerSlashingsVector]uint64

	// -- Altair participation --
	PreviousEpochParticipation []uint8 // per-validator participation flags
	CurrentEpochParticipation  []uint8 // per-validator participation flags

	// -- Justification and finality --
	JustificationBits           [BsnJustificationBitsLen]bool
	PreviousJustifiedCheckpoint CheckpointV2
	CurrentJustifiedCheckpoint  CheckpointV2
	FinalizedCheckpoint         CheckpointV2

	// -- Altair inactivity --
	InactivityScores []uint64 // per-validator inactivity scores

	// -- Altair sync committees --
	CurrentSyncCommittee *BsnSyncCommittee
	NextSyncCommittee    *BsnSyncCommittee

	// -- Bellatrix execution --
	LatestExecutionPayloadHeader *BsnExecutionPayloadHeader

	// Config (not part of the state tree, used for helpers).
	SlotsPerEpoch uint64
}

// NewBsnBeaconState creates an empty BsnBeaconState with sensible defaults.
func NewBsnBeaconState(slotsPerEpoch uint64) *BsnBeaconState {
	if slotsPerEpoch == 0 {
		slotsPerEpoch = 32
	}
	return &BsnBeaconState{
		HistoricalRoots:            make([][32]byte, 0),
		Eth1DataVotes:              make([]BsnEth1Data, 0),
		Validators:                 make([]*ValidatorV2, 0),
		Balances:                   make([]uint64, 0),
		PreviousEpochParticipation: make([]uint8, 0),
		CurrentEpochParticipation:  make([]uint8, 0),
		InactivityScores:           make([]uint64, 0),
		CurrentSyncCommittee:       &BsnSyncCommittee{Pubkeys: make([][48]byte, 0)},
		NextSyncCommittee:          &BsnSyncCommittee{Pubkeys: make([][48]byte, 0)},
		SlotsPerEpoch:              slotsPerEpoch,
	}
}

// BsnGetCurrentEpoch returns the current epoch derived from the slot.
func (s *BsnBeaconState) BsnGetCurrentEpoch() Epoch {
	return Epoch(s.Slot / s.SlotsPerEpoch)
}

// BsnGetPreviousEpoch returns the previous epoch, floored at 0.
func (s *BsnBeaconState) BsnGetPreviousEpoch() Epoch {
	if c := s.BsnGetCurrentEpoch(); c > 0 {
		return c - 1
	}
	return 0
}

// BsnAddValidator appends a validator to the registry with associated
// balance, participation flags, and inactivity score. Thread-safe.
func (s *BsnBeaconState) BsnAddValidator(v *ValidatorV2, balance uint64) uint64 {
	s.mu.Lock()
	defer s.mu.Unlock()

	idx := uint64(len(s.Validators))
	s.Validators = append(s.Validators, v)
	s.Balances = append(s.Balances, balance)
	s.PreviousEpochParticipation = append(s.PreviousEpochParticipation, 0)
	s.CurrentEpochParticipation = append(s.CurrentEpochParticipation, 0)
	s.InactivityScores = append(s.InactivityScores, 0)
	return idx
}

// BsnGetActiveValidatorIndices returns the indices of active validators
// at the given epoch. Thread-safe.
func (s *BsnBeaconState) BsnGetActiveValidatorIndices(epoch Epoch) []uint64 {
	s.mu.RLock()
	defer s.mu.RUnlock()
	var out []uint64
	for i, v := range s.Validators {
		if v.IsActiveV2(epoch) {
			out = append(out, uint64(i))
		}
	}
	return out
}

// BsnGetTotalActiveBalance returns the total effective balance of all
// active validators at the given epoch (minimum 1 ETH). Thread-safe.
func (s *BsnBeaconState) BsnGetTotalActiveBalance(epoch Epoch) uint64 {
	s.mu.RLock()
	defer s.mu.RUnlock()
	var total uint64
	for _, v := range s.Validators {
		if v.IsActiveV2(epoch) {
			total += v.EffectiveBalance
		}
	}
	if total < EffectiveBalanceIncrement {
		return EffectiveBalanceIncrement
	}
	return total
}

// BsnGetBlockRootAtSlot returns the block root stored for the given slot
// from the circular buffer.
func (s *BsnBeaconState) BsnGetBlockRootAtSlot(slot uint64) [32]byte {
	return s.BlockRoots[slot%BsnSlotsPerHistoricalRoot]
}

// BsnGetRandaoMix returns the RANDAO mix at the given epoch.
func (s *BsnBeaconState) BsnGetRandaoMix(epoch Epoch) [32]byte {
	return s.RandaoMixes[uint64(epoch)%BsnEpochsPerHistoricalVector]
}

// BsnSetParticipationFlag sets a participation flag for a validator in
// the current epoch participation array.
func (s *BsnBeaconState) BsnSetParticipationFlag(validatorIdx uint64, flagIndex uint8) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if validatorIdx < uint64(len(s.CurrentEpochParticipation)) {
		s.CurrentEpochParticipation[validatorIdx] |= 1 << flagIndex
	}
}

// BsnHasParticipationFlag checks if a validator has a specific participation
// flag set in the given participation array.
func BsnHasParticipationFlag(participation uint8, flagIndex uint8) bool {
	return participation&(1<<flagIndex) != 0
}

// BsnIncreaseBalance increases the balance of a validator by delta.
func (s *BsnBeaconState) BsnIncreaseBalance(index, delta uint64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if index < uint64(len(s.Balances)) {
		s.Balances[index] += delta
	}
}

// BsnDecreaseBalance decreases the balance of a validator by delta,
// flooring at zero.
func (s *BsnBeaconState) BsnDecreaseBalance(index, delta uint64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if index < uint64(len(s.Balances)) {
		if delta > s.Balances[index] {
			s.Balances[index] = 0
		} else {
			s.Balances[index] -= delta
		}
	}
}

// BsnCopy creates a deep copy of the beacon state. Thread-safe.
func (s *BsnBeaconState) BsnCopy() *BsnBeaconState {
	s.mu.RLock()
	defer s.mu.RUnlock()

	cp := &BsnBeaconState{
		GenesisTime:                 s.GenesisTime,
		GenesisValidatorsRoot:       s.GenesisValidatorsRoot,
		Slot:                        s.Slot,
		Fork:                        s.Fork,
		LatestBlockHeader:           s.LatestBlockHeader,
		BlockRoots:                  s.BlockRoots,
		StateRoots:                  s.StateRoots,
		Eth1Data:                    s.Eth1Data,
		Eth1DepositIndex:            s.Eth1DepositIndex,
		RandaoMixes:                 s.RandaoMixes,
		Slashings:                   s.Slashings,
		JustificationBits:           s.JustificationBits,
		PreviousJustifiedCheckpoint: s.PreviousJustifiedCheckpoint,
		CurrentJustifiedCheckpoint:  s.CurrentJustifiedCheckpoint,
		FinalizedCheckpoint:         s.FinalizedCheckpoint,
		SlotsPerEpoch:               s.SlotsPerEpoch,
	}

	// Deep-copy slices.
	cp.HistoricalRoots = make([][32]byte, len(s.HistoricalRoots))
	copy(cp.HistoricalRoots, s.HistoricalRoots)

	cp.Eth1DataVotes = make([]BsnEth1Data, len(s.Eth1DataVotes))
	copy(cp.Eth1DataVotes, s.Eth1DataVotes)

	cp.Validators = make([]*ValidatorV2, len(s.Validators))
	for i, v := range s.Validators {
		vc := *v
		cp.Validators[i] = &vc
	}

	cp.Balances = make([]uint64, len(s.Balances))
	copy(cp.Balances, s.Balances)

	cp.PreviousEpochParticipation = make([]uint8, len(s.PreviousEpochParticipation))
	copy(cp.PreviousEpochParticipation, s.PreviousEpochParticipation)

	cp.CurrentEpochParticipation = make([]uint8, len(s.CurrentEpochParticipation))
	copy(cp.CurrentEpochParticipation, s.CurrentEpochParticipation)

	cp.InactivityScores = make([]uint64, len(s.InactivityScores))
	copy(cp.InactivityScores, s.InactivityScores)

	if s.CurrentSyncCommittee != nil {
		cp.CurrentSyncCommittee = bsnCopySyncCommittee(s.CurrentSyncCommittee)
	}
	if s.NextSyncCommittee != nil {
		cp.NextSyncCommittee = bsnCopySyncCommittee(s.NextSyncCommittee)
	}
	if s.LatestExecutionPayloadHeader != nil {
		hdr := *s.LatestExecutionPayloadHeader
		hdr.ExtraData = make([]byte, len(s.LatestExecutionPayloadHeader.ExtraData))
		copy(hdr.ExtraData, s.LatestExecutionPayloadHeader.ExtraData)
		cp.LatestExecutionPayloadHeader = &hdr
	}

	return cp
}

// bsnCopySyncCommittee deep-copies a BsnSyncCommittee.
func bsnCopySyncCommittee(sc *BsnSyncCommittee) *BsnSyncCommittee {
	cp := &BsnSyncCommittee{
		AggregatePubkey: sc.AggregatePubkey,
		Pubkeys:         make([][48]byte, len(sc.Pubkeys)),
	}
	copy(cp.Pubkeys, sc.Pubkeys)
	return cp
}

// BsnRotateEpochParticipation rotates the participation arrays at epoch
// boundary: current becomes previous, current is zeroed.
func (s *BsnBeaconState) BsnRotateEpochParticipation() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.PreviousEpochParticipation = s.CurrentEpochParticipation
	s.CurrentEpochParticipation = make([]uint8, len(s.Validators))
}

// BsnHashTreeRoot computes a simplified hash tree root of the BsnBeaconState.
// This covers the main fields; production would use full SSZ.
func (s *BsnBeaconState) BsnHashTreeRoot() types.Hash {
	s.mu.RLock()
	defer s.mu.RUnlock()

	fields := make([][32]byte, 0, 24)

	// 0: genesis_time
	fields = append(fields, ssz.HashTreeRootUint64(s.GenesisTime))
	// 1: genesis_validators_root
	fields = append(fields, s.GenesisValidatorsRoot)
	// 2: slot
	fields = append(fields, ssz.HashTreeRootUint64(s.Slot))
	// 3: fork
	fields = append(fields, bsnHashFork(s.Fork))
	// 4: latest_block_header
	fields = append(fields, bsnHashBlockHeader(s.LatestBlockHeader))
	// 5: block_roots
	fields = append(fields, ssz.HashTreeRootVector(s.BlockRoots[:]))
	// 6: state_roots
	fields = append(fields, ssz.HashTreeRootVector(s.StateRoots[:]))
	// 7: historical_roots
	fields = append(fields, bsnHashRootList(s.HistoricalRoots, BsnHistoricalRootsLimit))
	// 8: eth1_data
	fields = append(fields, bsnHashEth1Data(s.Eth1Data))
	// 9: eth1_data_votes (simplified)
	fields = append(fields, bsnHashRootList(nil, BsnMaxEth1DataVotes))
	// 10: eth1_deposit_index
	fields = append(fields, ssz.HashTreeRootUint64(s.Eth1DepositIndex))
	// 11: validators
	vr := make([][32]byte, len(s.Validators))
	for i, v := range s.Validators {
		vr[i] = v.HashTreeRoot()
	}
	fields = append(fields, bsnHashRootList(vr, BsnValidatorRegistryLimit))
	// 12: balances
	bs := make([]byte, len(s.Balances)*8)
	for i, b := range s.Balances {
		binary.LittleEndian.PutUint64(bs[i*8:], b)
	}
	fields = append(fields, bsnHashBasicList(bs, len(s.Balances), 8, BsnValidatorRegistryLimit))
	// 13: randao_mixes
	fields = append(fields, ssz.HashTreeRootVector(s.RandaoMixes[:]))
	// 14: slashings
	ss := make([]byte, BsnEpochsPerSlashingsVector*8)
	for i, sl := range s.Slashings {
		binary.LittleEndian.PutUint64(ss[i*8:], sl)
	}
	fields = append(fields, ssz.HashTreeRootBasicVector(ss))
	// 15: previous_epoch_participation
	fields = append(fields, bsnHashBasicList(s.PreviousEpochParticipation, len(s.PreviousEpochParticipation), 1, BsnValidatorRegistryLimit))
	// 16: current_epoch_participation
	fields = append(fields, bsnHashBasicList(s.CurrentEpochParticipation, len(s.CurrentEpochParticipation), 1, BsnValidatorRegistryLimit))
	// 17: justification_bits
	fields = append(fields, ssz.HashTreeRootBitvector(s.JustificationBits[:]))
	// 18: previous_justified_checkpoint
	fields = append(fields, bsnHashCheckpoint(s.PreviousJustifiedCheckpoint))
	// 19: current_justified_checkpoint
	fields = append(fields, bsnHashCheckpoint(s.CurrentJustifiedCheckpoint))
	// 20: finalized_checkpoint
	fields = append(fields, bsnHashCheckpoint(s.FinalizedCheckpoint))
	// 21: inactivity_scores
	is := make([]byte, len(s.InactivityScores)*8)
	for i, sc := range s.InactivityScores {
		binary.LittleEndian.PutUint64(is[i*8:], sc)
	}
	fields = append(fields, bsnHashBasicList(is, len(s.InactivityScores), 8, BsnValidatorRegistryLimit))
	// 22: current_sync_committee
	fields = append(fields, bsnHashSyncCommittee(s.CurrentSyncCommittee))
	// 23: next_sync_committee
	fields = append(fields, bsnHashSyncCommittee(s.NextSyncCommittee))

	r := ssz.HashTreeRootContainer(fields)
	var h types.Hash
	copy(h[:], r[:])
	return h
}

// --- Helper hash functions ---

func bsnHashFork(f BsnFork) [32]byte {
	return ssz.HashTreeRootContainer([][32]byte{
		ssz.HashTreeRootBasicVector(f.PreviousVersion[:]),
		ssz.HashTreeRootBasicVector(f.CurrentVersion[:]),
		ssz.HashTreeRootUint64(uint64(f.Epoch)),
	})
}

func bsnHashBlockHeader(h BsnBeaconBlockHeader) [32]byte {
	return ssz.HashTreeRootContainer([][32]byte{
		ssz.HashTreeRootUint64(h.Slot),
		ssz.HashTreeRootUint64(h.ProposerIndex),
		h.ParentRoot,
		h.StateRoot,
		h.BodyRoot,
	})
}

func bsnHashEth1Data(e BsnEth1Data) [32]byte {
	return ssz.HashTreeRootContainer([][32]byte{
		e.DepositRoot,
		ssz.HashTreeRootUint64(e.DepositCount),
		e.BlockHash,
	})
}

func bsnHashCheckpoint(cp CheckpointV2) [32]byte {
	return ssz.HashTreeRootContainer([][32]byte{
		ssz.HashTreeRootUint64(uint64(cp.Epoch)),
		cp.Root,
	})
}

func bsnHashSyncCommittee(sc *BsnSyncCommittee) [32]byte {
	if sc == nil {
		return [32]byte{}
	}
	// Hash the pubkeys as a vector of 48-byte elements.
	pkChunks := make([][32]byte, 0, len(sc.Pubkeys)*2)
	for _, pk := range sc.Pubkeys {
		var c1, c2 [32]byte
		copy(c1[:], pk[:32])
		copy(c2[:16], pk[32:])
		pkChunks = append(pkChunks, c1, c2)
	}
	pkRoot := ssz.Merkleize(pkChunks, 0)

	// Hash the aggregate pubkey.
	var ag1, ag2 [32]byte
	copy(ag1[:], sc.AggregatePubkey[:32])
	copy(ag2[:16], sc.AggregatePubkey[32:])
	agRoot := ssz.Merkleize([][32]byte{ag1, ag2}, 0)

	return ssz.HashTreeRootContainer([][32]byte{pkRoot, agRoot})
}

// bsnHashRootList merkleizes a list of [32]byte roots with large max capacity.
func bsnHashRootList(roots [][32]byte, maxLen uint64) [32]byte {
	depth := bsnTreeDepth(maxLen)
	zh := make([][32]byte, depth+1)
	for i := uint64(1); i <= depth; i++ {
		zh[i] = bsnSHA256Pair(zh[i-1], zh[i-1])
	}
	return ssz.MixInLength(bsnMerkVirtual(roots, depth, zh), uint64(len(roots)))
}

// bsnHashBasicList merkleizes a packed basic list.
func bsnHashBasicList(ser []byte, count, elemSz int, maxLen uint64) [32]byte {
	chunks := ssz.Pack(ser)
	mc := (maxLen*uint64(elemSz) + 31) / 32
	depth := bsnTreeDepth(mc)
	zh := make([][32]byte, depth+1)
	for i := uint64(1); i <= depth; i++ {
		zh[i] = bsnSHA256Pair(zh[i-1], zh[i-1])
	}
	return ssz.MixInLength(bsnMerkVirtual(chunks, depth, zh), uint64(count))
}

func bsnTreeDepth(maxLen uint64) uint64 {
	if maxLen == 0 {
		return 0
	}
	depth, p := uint64(0), uint64(1)
	for p < maxLen {
		p <<= 1
		depth++
	}
	return depth
}

func bsnMerkVirtual(chunks [][32]byte, depth uint64, zh [][32]byte) [32]byte {
	if depth == 0 {
		if len(chunks) > 0 {
			return chunks[0]
		}
		return zh[0]
	}
	if len(chunks) == 0 {
		return zh[depth]
	}
	layer := make([][32]byte, len(chunks))
	copy(layer, chunks)
	for d := uint64(0); d < depth; d++ {
		ns := (len(layer) + 1) / 2
		nl := make([][32]byte, ns)
		for i := 0; i < ns; i++ {
			r := zh[d]
			if 2*i+1 < len(layer) {
				r = layer[2*i+1]
			}
			nl[i] = bsnSHA256Pair(layer[2*i], r)
		}
		layer = nl
	}
	return layer[0]
}

func bsnSHA256Pair(a, b [32]byte) [32]byte {
	var c [64]byte
	copy(c[:32], a[:])
	copy(c[32:], b[:])
	return sha256.Sum256(c[:])
}
