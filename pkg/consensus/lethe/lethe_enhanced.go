// Package lethe provides LETHE privacy channels. This file adds the
// EnhancedLETHE layer that provides stronger privacy guarantees through
// mix-network-style session mixing and traffic noise injection, building
// on top of the basic LETHE channel encryption.
package lethe

import (
	"crypto/rand"
	"encoding/binary"
	"errors"
	"sync"
	"time"
)

// Enhanced LETHE errors.
var (
	ErrLETHESessionNotFound  = errors.New("lethe: session not found")
	ErrLETHESessionFull      = errors.New("lethe: session mix buffer is full")
	ErrLETHEInsufficientMix  = errors.New("lethe: insufficient items for mixing")
	ErrLETHESessionClosed    = errors.New("lethe: session is closed")
	ErrLETHENoParticipants   = errors.New("lethe: session requires participants")
	ErrLETHEMaxSessions      = errors.New("lethe: maximum sessions reached")
	ErrLETHEInvalidNoiseData = errors.New("lethe: invalid noise-wrapped data")
)

// SessionStatus represents the lifecycle state of a privacy session.
type SessionStatus int

const (
	SessionActive SessionStatus = iota
	SessionFlushed
	SessionClosed
)

// EnhancedLETHEConfig configures the enhanced privacy layer.
type EnhancedLETHEConfig struct {
	NoiseLevel     int           // number of noise bytes to inject (0 disables)
	SessionTimeout time.Duration // how long sessions remain active
	MaxSessions    int           // maximum concurrent sessions
	MinMixSize     int           // minimum items required before flushing
	MaxMixBuffer   int           // maximum items in a session's mix buffer
}

// DefaultEnhancedLETHEConfig returns sensible defaults.
func DefaultEnhancedLETHEConfig() EnhancedLETHEConfig {
	return EnhancedLETHEConfig{
		NoiseLevel:     32,
		SessionTimeout: 30 * time.Second,
		MaxSessions:    256,
		MinMixSize:     3,
		MaxMixBuffer:   1024,
	}
}

// PrivacySession holds a mixing session where participants submit data
// that is collected and output in a shuffled order to break linkability.
type PrivacySession struct {
	ID           string
	Participants []string
	CreatedAt    time.Time
	Status       SessionStatus
	mixBuffer    [][]byte // collected data items awaiting flush
}

// EnhancedLETHE provides mix-network-style privacy sessions and traffic
// noise injection for validator operations. It is safe for concurrent use.
type EnhancedLETHE struct {
	mu       sync.RWMutex
	config   EnhancedLETHEConfig
	sessions map[string]*PrivacySession
}

// NewEnhancedLETHE creates a new enhanced LETHE privacy layer.
func NewEnhancedLETHE(config EnhancedLETHEConfig) *EnhancedLETHE {
	return &EnhancedLETHE{
		config:   config,
		sessions: make(map[string]*PrivacySession),
	}
}

// CreateSession creates a new mixing session with the given participants.
// Returns the session or an error if participants are empty or max sessions
// is reached.
func (el *EnhancedLETHE) CreateSession(participants []string) (*PrivacySession, error) {
	if len(participants) == 0 {
		return nil, ErrLETHENoParticipants
	}

	el.mu.Lock()
	defer el.mu.Unlock()

	if el.config.MaxSessions > 0 && len(el.sessions) >= el.config.MaxSessions {
		return nil, ErrLETHEMaxSessions
	}

	id := generateSessionID()
	session := &PrivacySession{
		ID:           id,
		Participants: make([]string, len(participants)),
		CreatedAt:    time.Now(),
		Status:       SessionActive,
		mixBuffer:    make([][]byte, 0),
	}
	copy(session.Participants, participants)

	el.sessions[id] = session

	// Return a safe copy for the caller.
	out := &PrivacySession{
		ID:           session.ID,
		Participants: make([]string, len(session.Participants)),
		CreatedAt:    session.CreatedAt,
		Status:       session.Status,
	}
	copy(out.Participants, session.Participants)
	return out, nil
}

// SubmitToMix submits data into a session's mix buffer. The data will be
// held until FlushMix is called. Returns an error if the session is not
// found, closed, or the buffer is full.
func (el *EnhancedLETHE) SubmitToMix(sessionID string, data []byte) error {
	el.mu.Lock()
	defer el.mu.Unlock()

	session, ok := el.sessions[sessionID]
	if !ok {
		return ErrLETHESessionNotFound
	}
	if session.Status != SessionActive {
		return ErrLETHESessionClosed
	}

	maxBuf := el.config.MaxMixBuffer
	if maxBuf > 0 && len(session.mixBuffer) >= maxBuf {
		return ErrLETHESessionFull
	}

	// Store a copy of the data.
	item := make([]byte, len(data))
	copy(item, data)
	session.mixBuffer = append(session.mixBuffer, item)
	return nil
}

// FlushMix returns all collected data from the session's mix buffer in a
// shuffled order, breaking the correlation between submission order and
// output order. The buffer is cleared after flushing. Returns an error if
// the session is not found, closed, or if fewer than MinMixSize items
// have been submitted.
func (el *EnhancedLETHE) FlushMix(sessionID string) ([][]byte, error) {
	el.mu.Lock()
	defer el.mu.Unlock()

	session, ok := el.sessions[sessionID]
	if !ok {
		return nil, ErrLETHESessionNotFound
	}
	if session.Status != SessionActive {
		return nil, ErrLETHESessionClosed
	}

	if len(session.mixBuffer) < el.config.MinMixSize {
		return nil, ErrLETHEInsufficientMix
	}

	// Copy buffer items.
	result := make([][]byte, len(session.mixBuffer))
	for i, item := range session.mixBuffer {
		c := make([]byte, len(item))
		copy(c, item)
		result[i] = c
	}

	// Fisher-Yates shuffle for unlinkability.
	shuffleSlice(result)

	// Clear the buffer.
	session.mixBuffer = session.mixBuffer[:0]
	session.Status = SessionFlushed

	return result, nil
}

// AddNoise wraps data with random noise bytes for traffic analysis resistance.
// The format is: [4-byte big-endian data length][data][noise bytes].
// Returns the noise-wrapped data.
func (el *EnhancedLETHE) AddNoise(data []byte) []byte {
	noiseLen := el.config.NoiseLevel
	if noiseLen <= 0 {
		// No noise: still wrap with length prefix for StripNoise compat.
		out := make([]byte, 4+len(data))
		binary.BigEndian.PutUint32(out[:4], uint32(len(data)))
		copy(out[4:], data)
		return out
	}

	out := make([]byte, 4+len(data)+noiseLen)
	binary.BigEndian.PutUint32(out[:4], uint32(len(data)))
	copy(out[4:], data)

	// Fill noise portion with random bytes.
	noise := out[4+len(data):]
	rand.Read(noise)

	return out
}

// StripNoise removes the noise from a noise-wrapped payload, returning
// the original data. Returns an error if the payload is too short or
// the embedded length is inconsistent.
func (el *EnhancedLETHE) StripNoise(data []byte) ([]byte, error) {
	if len(data) < 4 {
		return nil, ErrLETHEInvalidNoiseData
	}

	dataLen := binary.BigEndian.Uint32(data[:4])
	if int(dataLen) > len(data)-4 {
		return nil, ErrLETHEInvalidNoiseData
	}

	result := make([]byte, dataLen)
	copy(result, data[4:4+dataLen])
	return result, nil
}

// ActiveSessions returns the number of sessions that have not been closed.
func (el *EnhancedLETHE) ActiveSessions() int {
	el.mu.RLock()
	defer el.mu.RUnlock()

	count := 0
	for _, s := range el.sessions {
		if s.Status != SessionClosed {
			count++
		}
	}
	return count
}

// CloseSession closes a session and removes it from the manager.
// Returns an error if the session is not found.
func (el *EnhancedLETHE) CloseSession(sessionID string) error {
	el.mu.Lock()
	defer el.mu.Unlock()

	session, ok := el.sessions[sessionID]
	if !ok {
		return ErrLETHESessionNotFound
	}

	session.Status = SessionClosed
	session.mixBuffer = nil
	delete(el.sessions, sessionID)
	return nil
}

// GetSession retrieves a session by ID (returns a copy with metadata only).
// Returns an error if the session is not found.
func (el *EnhancedLETHE) GetSession(sessionID string) (*PrivacySession, error) {
	el.mu.RLock()
	defer el.mu.RUnlock()

	session, ok := el.sessions[sessionID]
	if !ok {
		return nil, ErrLETHESessionNotFound
	}

	out := &PrivacySession{
		ID:           session.ID,
		Participants: make([]string, len(session.Participants)),
		CreatedAt:    session.CreatedAt,
		Status:       session.Status,
	}
	copy(out.Participants, session.Participants)
	return out, nil
}

// MixBufferSize returns the number of items in a session's mix buffer.
// Returns -1 if the session is not found.
func (el *EnhancedLETHE) MixBufferSize(sessionID string) int {
	el.mu.RLock()
	defer el.mu.RUnlock()

	session, ok := el.sessions[sessionID]
	if !ok {
		return -1
	}
	return len(session.mixBuffer)
}

// generateSessionID creates a random hex-encoded session identifier.
func generateSessionID() string {
	var buf [16]byte
	rand.Read(buf[:])
	const hex = "0123456789abcdef"
	out := make([]byte, 32)
	for i, b := range buf {
		out[i*2] = hex[b>>4]
		out[i*2+1] = hex[b&0x0f]
	}
	return string(out)
}

// shuffleSlice performs a Fisher-Yates shuffle on a slice of byte slices.
func shuffleSlice(items [][]byte) {
	n := len(items)
	for i := n - 1; i > 0; i-- {
		var buf [8]byte
		rand.Read(buf[:])
		j := int(binary.LittleEndian.Uint64(buf[:]) % uint64(i+1))
		items[i], items[j] = items[j], items[i]
	}
}
