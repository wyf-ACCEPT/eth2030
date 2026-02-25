// Package light implements a light client for the Ethereum beacon chain.
// It tracks sync committees and finalized headers, allowing verification
// of state proofs without downloading the full blockchain state.
package light

import (
	"math/big"

	"github.com/eth2030/eth2030/core/types"
)

// LightBlock contains a block header and associated proofs sufficient for
// light client verification.
type LightBlock struct {
	Header     *types.Header
	StateProof []byte
	TxProofs   [][]byte
}

// SyncCommittee represents a beacon chain sync committee that signs
// light client updates. Each committee serves for ~27 hours (256 epochs).
type SyncCommittee struct {
	Pubkeys         [][]byte
	AggregatePubkey []byte
	Period          uint64
	// SecretKeys holds BLS secret keys for test committee members.
	// Not populated in production; used by SignSyncCommittee for testing.
	SecretKeys []*big.Int
}

// LightClientUpdate carries the data needed to advance a light client's
// view of the chain. It contains attested and finalized headers plus
// the sync committee signature.
type LightClientUpdate struct {
	AttestedHeader    *types.Header
	FinalizedHeader   *types.Header
	SyncCommitteeBits []byte
	Signature         []byte
	NextSyncCommittee *SyncCommittee
}

// LightClientState holds the current state of the light client.
type LightClientState struct {
	CurrentSlot      uint64
	FinalizedHeader  *types.Header
	CurrentCommittee *SyncCommittee
}

// SignerCount returns the number of set bits in the sync committee
// participation bitfield, indicating how many validators signed.
func (u *LightClientUpdate) SignerCount() int {
	count := 0
	for _, b := range u.SyncCommitteeBits {
		for i := 0; i < 8; i++ {
			if b&(1<<uint(i)) != 0 {
				count++
			}
		}
	}
	return count
}

// SupermajoritySigned returns true if >= 2/3 of the sync committee
// signed this update.
func (u *LightClientUpdate) SupermajoritySigned(committeeSize int) bool {
	if committeeSize == 0 {
		return false
	}
	return u.SignerCount()*3 >= committeeSize*2
}
