// Package encrypted implements a commit-reveal transaction pool that prevents
// MEV frontrunning by hiding transaction contents until after ordering.
// Transactions are first committed (with only a hash visible), then revealed
// after the commit is included, and ordered by commit time for fairness.
package encrypted

import (
	"math/big"

	"github.com/eth2030/eth2030/core/types"
)

// CommitRevealWindow is the time in seconds between commit and reveal deadline.
// Set to 12 seconds (1 Ethereum slot).
const CommitRevealWindow uint64 = 12

// CommitState represents the lifecycle of a committed transaction.
type CommitState uint8

const (
	COMMITTED CommitState = 0
	REVEALED  CommitState = 1
	EXPIRED   CommitState = 2
)

// CommitTx is the commit phase of a commit-reveal transaction.
// It contains only the hash of the transaction, not the contents.
type CommitTx struct {
	CommitHash types.Hash
	Sender     types.Address
	GasLimit   uint64
	MaxFee     *big.Int
	Timestamp  uint64
}

// RevealTx is the reveal phase, linking back to the commit via hash.
type RevealTx struct {
	CommitHash  types.Hash
	Transaction *types.Transaction
}

// CommitEntry tracks the full state of a committed transaction.
type CommitEntry struct {
	Commit        *CommitTx
	State         CommitState
	RevealDeadline uint64
}
