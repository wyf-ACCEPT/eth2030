package consensus

import (
	"github.com/eth2030/eth2030/core/types"
)

// CommitteeIndex represents a beacon committee index.
type CommitteeIndex uint64

// ExtValidator is a comprehensive validator record containing all fields
// from the beacon chain spec, beyond the basic ValidatorBalance type.
type ExtValidator struct {
	Pubkey                [48]byte
	WithdrawalCredentials [32]byte
	EffectiveBalance      uint64
	Slashed               bool
	ActivationEligibility Epoch
	ActivationEpoch       Epoch
	ExitEpoch             Epoch
	WithdrawableEpoch     Epoch
}

// IsActive returns true if the validator is active at the given epoch.
func (v *ExtValidator) IsActive(epoch Epoch) bool {
	return v.ActivationEpoch <= epoch && epoch < v.ExitEpoch
}

// IsSlashable returns true if the validator can be slashed at the given epoch.
func (v *ExtValidator) IsSlashable(epoch Epoch) bool {
	return !v.Slashed && v.ActivationEpoch <= epoch && epoch < v.WithdrawableEpoch
}

// IsExited returns true if the validator has exited by the given epoch.
func (v *ExtValidator) IsExited(epoch Epoch) bool {
	return epoch >= v.ExitEpoch
}

// IsWithdrawable returns true if the validator's balance is withdrawable.
func (v *ExtValidator) IsWithdrawable(epoch Epoch) bool {
	return epoch >= v.WithdrawableEpoch
}

// ExtBeaconState extends BeaconState with full validator registry, sync
// committee state, and slashing tracking.
type ExtBeaconState struct {
	// Versioning.
	GenesisTime       uint64
	GenesisValidators types.Hash
	Fork              BeaconFork

	// Consensus bookkeeping.
	Slot                 Slot
	Epoch                Epoch
	FinalizedCheckpoint  Checkpoint
	JustifiedCheckpoint  Checkpoint
	PreviousJustified    Checkpoint
	JustificationBits    JustificationBits

	// Block roots.
	LatestBlockRoot types.Hash

	// Validator registry.
	Validators []*ExtValidator
	Balances   []uint64

	// Sync committee.
	CurrentSyncCommittee *SyncCommitteeState
	NextSyncCommittee    *SyncCommitteeState

	// Slashings accumulator per epoch (indexed by epoch % EPOCHS_PER_SLASHING).
	Slashings     []uint64
	SlashingEpoch uint64
}

// BeaconFork holds fork version information.
type BeaconFork struct {
	PreviousVersion [4]byte
	CurrentVersion  [4]byte
	Epoch           Epoch
}

// SyncCommitteeState holds the sync committee public keys and aggregate key.
type SyncCommitteeState struct {
	Pubkeys         [][48]byte
	AggregatePubkey [48]byte
}

// SignedBeaconBlock is a beacon block with its signature.
type SignedBeaconBlock struct {
	Block     *ExtBeaconBlock
	Signature [96]byte
}

// ExtBeaconBlock is a comprehensive beacon block.
type ExtBeaconBlock struct {
	Slot          Slot
	ProposerIndex ValidatorIndex
	ParentRoot    types.Hash
	StateRoot     types.Hash
	Body          *ExtBeaconBlockBody
}

// ExtBeaconBlockBody holds the body of a beacon block (extended version).
type ExtBeaconBlockBody struct {
	RandaoReveal      [96]byte
	Eth1Data          ExtEth1Data
	Graffiti          [32]byte
	ProposerSlashings []*ExtProposerSlashing
	AttesterSlashings []*ExtAttesterSlashing
	Attestations      []*ExtAttestation
	Deposits          []*ExtDeposit
	VoluntaryExits    []*SignedVoluntaryExit
	SyncAggregate     *ExtSyncAggregate
}

// ExtEth1Data contains execution layer deposit info (extended version).
type ExtEth1Data struct {
	DepositRoot  types.Hash
	DepositCount uint64
	BlockHash    types.Hash
}

// ExtProposerSlashing contains evidence of a double-proposal (extended version).
type ExtProposerSlashing struct {
	SignedHeader1 ExtSignedBeaconBlockHeader
	SignedHeader2 ExtSignedBeaconBlockHeader
}

// ExtSignedBeaconBlockHeader is a signed beacon block header (extended version).
type ExtSignedBeaconBlockHeader struct {
	Header    ExtBeaconBlockHeader
	Signature [96]byte
}

// ExtBeaconBlockHeader is a lightweight block header (extended version).
type ExtBeaconBlockHeader struct {
	Slot          Slot
	ProposerIndex ValidatorIndex
	ParentRoot    types.Hash
	StateRoot     types.Hash
	BodyRoot      types.Hash
}

// ExtAttesterSlashing contains evidence of a double-vote or surround-vote (extended version).
type ExtAttesterSlashing struct {
	Attestation1 *ExtIndexedAttestation
	Attestation2 *ExtIndexedAttestation
}

// ExtIndexedAttestation is an attestation with validator indices (extended version).
type ExtIndexedAttestation struct {
	AttestingIndices []ValidatorIndex
	Data             AttestationData
	Signature        [96]byte
}

// ExtAttestation is a full attestation with committee index per EIP-7549.
type ExtAttestation struct {
	AggregationBits []byte
	Data            AttestationData
	CommitteeBits   []byte
	Signature       [96]byte
}

// ExtDeposit represents a deposit from the execution layer (extended version).
type ExtDeposit struct {
	Proof [33]types.Hash // Merkle proof
	Data  DepositData
}

// DepositData is the data in a deposit.
type DepositData struct {
	Pubkey                [48]byte
	WithdrawalCredentials [32]byte
	Amount                uint64
	Signature             [96]byte
}

// SignedVoluntaryExit is a signed voluntary exit message.
type SignedVoluntaryExit struct {
	Exit      ExtVoluntaryExit
	Signature [96]byte
}

// ExtVoluntaryExit represents a validator's voluntary exit request (extended version).
type ExtVoluntaryExit struct {
	Epoch          Epoch
	ValidatorIndex ValidatorIndex
}

// ExtSyncAggregate contains the sync committee's participation bitfield (extended version).
type ExtSyncAggregate struct {
	SyncCommitteeBits      []byte
	SyncCommitteeSignature [96]byte
}

// ExtSyncCommitteeMessage is a message from a sync committee member (extended version).
type ExtSyncCommitteeMessage struct {
	Slot            Slot
	BeaconBlockRoot types.Hash
	ValidatorIndex  ValidatorIndex
	Signature       [96]byte
}

// ExtSyncCommitteeContribution is an aggregated contribution from sync
// committee subnet members (extended version).
type ExtSyncCommitteeContribution struct {
	Slot              Slot
	BeaconBlockRoot   types.Hash
	SubcommitteeIndex uint64
	AggregationBits   []byte
	Signature         [96]byte
}

// ActiveValidatorCount returns the number of active validators at epoch.
func (s *ExtBeaconState) ActiveValidatorCount(epoch Epoch) int {
	count := 0
	for _, v := range s.Validators {
		if v.IsActive(epoch) {
			count++
		}
	}
	return count
}

// TotalActiveBalance returns the sum of effective balances of active validators.
func (s *ExtBeaconState) TotalActiveBalance(epoch Epoch) uint64 {
	var total uint64
	for _, v := range s.Validators {
		if v.IsActive(epoch) {
			total += v.EffectiveBalance
		}
	}
	return total
}

// SlashableValidators returns the indices of validators that can be slashed.
func (s *ExtBeaconState) SlashableValidators(epoch Epoch) []ValidatorIndex {
	var indices []ValidatorIndex
	for i, v := range s.Validators {
		if v.IsSlashable(epoch) {
			indices = append(indices, ValidatorIndex(i))
		}
	}
	return indices
}

// WithdrawableValidators returns the indices of validators whose balance
// is withdrawable at the given epoch.
func (s *ExtBeaconState) WithdrawableValidators(epoch Epoch) []ValidatorIndex {
	var indices []ValidatorIndex
	for i, v := range s.Validators {
		if v.IsWithdrawable(epoch) {
			indices = append(indices, ValidatorIndex(i))
		}
	}
	return indices
}
