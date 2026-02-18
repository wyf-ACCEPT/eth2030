package p2p

import (
	"sync"
	"time"
)

// PeerScore tracks the reputation of a connected peer. Scores influence
// peer selection and disconnection decisions. Higher is better.
type PeerScore struct {
	mu          sync.RWMutex
	value       float64   // Current score (-100 to +100).
	lastUpdated time.Time // When the score was last changed.

	// Counters for scoring decisions.
	goodResponses int // Timely, valid responses.
	badResponses  int // Invalid, late, or useless responses.
	timeouts      int // Request timeouts.
	usefulBlocks  int // Blocks the peer announced that we needed.
}

// Score boundaries.
const (
	MaxScore     = 100.0
	MinScore     = -100.0
	DefaultScore = 0.0

	// ScoreDisconnect is the threshold below which a peer should be dropped.
	ScoreDisconnect = -50.0

	// Reward/penalty amounts.
	scoreGoodResponse  = 1.0
	scoreBadResponse   = -5.0
	scoreTimeout       = -10.0
	scoreUsefulBlock   = 2.0
	scoreUselessBlock  = -0.5
	scoreHandshakeOK   = 5.0
	scoreHandshakeFail = -20.0
)

// NewPeerScore creates a score tracker with the default initial score.
func NewPeerScore() *PeerScore {
	return &PeerScore{
		value:       DefaultScore,
		lastUpdated: time.Now(),
	}
}

// Value returns the current score value.
func (ps *PeerScore) Value() float64 {
	ps.mu.RLock()
	defer ps.mu.RUnlock()
	return ps.value
}

// ShouldDisconnect returns true if the peer's score has dropped below the
// disconnect threshold.
func (ps *PeerScore) ShouldDisconnect() bool {
	ps.mu.RLock()
	defer ps.mu.RUnlock()
	return ps.value <= ScoreDisconnect
}

// GoodResponse records a valid, timely response from the peer.
func (ps *PeerScore) GoodResponse() {
	ps.mu.Lock()
	defer ps.mu.Unlock()
	ps.goodResponses++
	ps.adjust(scoreGoodResponse)
}

// BadResponse records an invalid or useless response.
func (ps *PeerScore) BadResponse() {
	ps.mu.Lock()
	defer ps.mu.Unlock()
	ps.badResponses++
	ps.adjust(scoreBadResponse)
}

// Timeout records a request timeout.
func (ps *PeerScore) Timeout() {
	ps.mu.Lock()
	defer ps.mu.Unlock()
	ps.timeouts++
	ps.adjust(scoreTimeout)
}

// UsefulBlock records that a block announcement from the peer was needed.
func (ps *PeerScore) UsefulBlock() {
	ps.mu.Lock()
	defer ps.mu.Unlock()
	ps.usefulBlocks++
	ps.adjust(scoreUsefulBlock)
}

// UselessBlock records that a block announcement was already known.
func (ps *PeerScore) UselessBlock() {
	ps.mu.Lock()
	defer ps.mu.Unlock()
	ps.adjust(scoreUselessBlock)
}

// HandshakeOK records a successful protocol handshake.
func (ps *PeerScore) HandshakeOK() {
	ps.mu.Lock()
	defer ps.mu.Unlock()
	ps.adjust(scoreHandshakeOK)
}

// HandshakeFail records a failed protocol handshake.
func (ps *PeerScore) HandshakeFail() {
	ps.mu.Lock()
	defer ps.mu.Unlock()
	ps.adjust(scoreHandshakeFail)
}

// Stats returns a snapshot of the scoring counters.
func (ps *PeerScore) Stats() ScoreStats {
	ps.mu.RLock()
	defer ps.mu.RUnlock()
	return ScoreStats{
		Value:         ps.value,
		GoodResponses: ps.goodResponses,
		BadResponses:  ps.badResponses,
		Timeouts:      ps.timeouts,
		UsefulBlocks:  ps.usefulBlocks,
		LastUpdated:   ps.lastUpdated,
	}
}

// ScoreStats is a read-only snapshot of a peer's scoring state.
type ScoreStats struct {
	Value         float64
	GoodResponses int
	BadResponses  int
	Timeouts      int
	UsefulBlocks  int
	LastUpdated   time.Time
}

// adjust adds delta to the score, clamping to [MinScore, MaxScore].
// Caller must hold ps.mu.
func (ps *PeerScore) adjust(delta float64) {
	ps.value += delta
	if ps.value > MaxScore {
		ps.value = MaxScore
	}
	if ps.value < MinScore {
		ps.value = MinScore
	}
	ps.lastUpdated = time.Now()
}
