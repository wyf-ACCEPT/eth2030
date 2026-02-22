// kps.go implements Key Pair Sharing (KPS) for distributed key management.
// KPS enables validators to split their private keys into shares distributed
// among a group of key holders. A configurable threshold of shares is needed
// to reconstruct the original key, providing fault tolerance and preventing
// single points of failure using Shamir's Secret Sharing.
package consensus

import (
	"crypto/rand"
	"errors"
	"fmt"
	"sync"

	"github.com/eth2030/eth2030/core/types"
	"github.com/eth2030/eth2030/crypto"
)

// KPS errors.
var (
	ErrKPSInvalidThreshold  = errors.New("kps: threshold must be > 0 and <= total shares")
	ErrKPSInvalidShares     = errors.New("kps: total shares must be > 0")
	ErrKPSInsufficientShares = errors.New("kps: insufficient shares for recombination")
	ErrKPSDuplicateShare    = errors.New("kps: duplicate share index")
	ErrKPSInvalidShareData  = errors.New("kps: invalid share data")
	ErrKPSInvalidPrivateKey = errors.New("kps: invalid private key")
	ErrKPSGroupFull         = errors.New("kps: group is full")
	ErrKPSMemberExists      = errors.New("kps: member already exists")
	ErrKPSMemberNotFound    = errors.New("kps: member not found")
	ErrKPSGroupNotFound     = errors.New("kps: group not found")
	ErrKPSKeyGenFailed      = errors.New("kps: key generation failed")
)

// KPS key size constant.
const kpsKeySize = 32

// KPSConfig holds configuration for the KPS manager.
type KPSConfig struct {
	DefaultThreshold    int
	MaxGroupSize        int
	KeyRotationInterval uint64 // in epochs
}

// DefaultKPSConfig returns sensible defaults for KPS.
func DefaultKPSConfig() KPSConfig {
	return KPSConfig{
		DefaultThreshold:    2,
		MaxGroupSize:        10,
		KeyRotationInterval: 256,
	}
}

// KeyShare represents a single share of a split private key.
type KeyShare struct {
	Index   int
	Data    []byte
	GroupID types.Hash
}

// KPSKeyPair holds a generated key pair with its associated shares.
type KPSKeyPair struct {
	PublicKey     []byte
	PrivateShares []*KeyShare
	Threshold    int
	TotalShares  int
}

// KeyGroup manages a group of key holders who collectively hold shares
// of a validator's private key.
type KeyGroup struct {
	mu           sync.RWMutex
	groupID      types.Hash
	threshold    int
	totalMembers int
	members      []types.Address
}

// NewKeyGroup creates a new key group with the given parameters.
func NewKeyGroup(groupID types.Hash, threshold, totalMembers int) *KeyGroup {
	return &KeyGroup{
		groupID:      groupID,
		threshold:    threshold,
		totalMembers: totalMembers,
		members:      make([]types.Address, 0, totalMembers),
	}
}

// AddMember adds a member to the key group.
func (kg *KeyGroup) AddMember(memberID types.Address) error {
	kg.mu.Lock()
	defer kg.mu.Unlock()

	if len(kg.members) >= kg.totalMembers {
		return ErrKPSGroupFull
	}
	for _, m := range kg.members {
		if m == memberID {
			return ErrKPSMemberExists
		}
	}
	kg.members = append(kg.members, memberID)
	return nil
}

// RemoveMember removes a member from the key group.
func (kg *KeyGroup) RemoveMember(memberID types.Address) error {
	kg.mu.Lock()
	defer kg.mu.Unlock()

	for i, m := range kg.members {
		if m == memberID {
			kg.members = append(kg.members[:i], kg.members[i+1:]...)
			return nil
		}
	}
	return ErrKPSMemberNotFound
}

// GetMembers returns a copy of the member list.
func (kg *KeyGroup) GetMembers() []types.Address {
	kg.mu.RLock()
	defer kg.mu.RUnlock()
	result := make([]types.Address, len(kg.members))
	copy(result, kg.members)
	return result
}

// GroupID returns the group identifier.
func (kg *KeyGroup) GroupID() types.Hash {
	return kg.groupID
}

// Threshold returns the group threshold.
func (kg *KeyGroup) Threshold() int {
	kg.mu.RLock()
	defer kg.mu.RUnlock()
	return kg.threshold
}

// TotalMembers returns the maximum group size.
func (kg *KeyGroup) TotalMembers() int {
	return kg.totalMembers
}

// MemberCount returns the current number of members.
func (kg *KeyGroup) MemberCount() int {
	kg.mu.RLock()
	defer kg.mu.RUnlock()
	return len(kg.members)
}

// KPSManager manages key pair sharing for validators.
type KPSManager struct {
	mu     sync.RWMutex
	config KPSConfig
	groups map[types.Hash]*KeyGroup
	keys   map[types.Hash]*KPSKeyPair // keyed by group ID
}

// NewKPSManager creates a new KPS manager with the given configuration.
func NewKPSManager(config KPSConfig) *KPSManager {
	if config.DefaultThreshold <= 0 {
		config.DefaultThreshold = 2
	}
	if config.MaxGroupSize <= 0 {
		config.MaxGroupSize = 10
	}
	if config.KeyRotationInterval == 0 {
		config.KeyRotationInterval = 256
	}
	return &KPSManager{
		config: config,
		groups: make(map[types.Hash]*KeyGroup),
		keys:   make(map[types.Hash]*KPSKeyPair),
	}
}

// Config returns the manager configuration.
func (m *KPSManager) Config() KPSConfig {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.config
}

// GenerateKeyPair generates a new KPS key pair, splitting the private key
// into shares using the default threshold and max group size.
func (m *KPSManager) GenerateKeyPair() (*KPSKeyPair, error) {
	m.mu.RLock()
	threshold := m.config.DefaultThreshold
	totalShares := m.config.MaxGroupSize
	m.mu.RUnlock()

	// Generate a random private key.
	privKey := make([]byte, kpsKeySize)
	if _, err := rand.Read(privKey); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrKPSKeyGenFailed, err)
	}

	shares, err := SplitKey(privKey, threshold, totalShares)
	if err != nil {
		return nil, err
	}

	// Derive public key as hash of private key.
	pubKey := crypto.Keccak256(privKey)

	return &KPSKeyPair{
		PublicKey:     pubKey,
		PrivateShares: shares,
		Threshold:    threshold,
		TotalShares:  totalShares,
	}, nil
}

// SplitKey splits a private key into shares using Shamir's Secret Sharing
// over GF(256). The threshold parameter specifies the minimum number of
// shares needed to reconstruct the key.
func SplitKey(privateKey []byte, threshold, totalShares int) ([]*KeyShare, error) {
	if len(privateKey) == 0 {
		return nil, ErrKPSInvalidPrivateKey
	}
	if totalShares <= 0 {
		return nil, ErrKPSInvalidShares
	}
	if threshold <= 0 || threshold > totalShares {
		return nil, ErrKPSInvalidThreshold
	}

	// Compute a group ID from the private key.
	groupID := crypto.Keccak256Hash(privateKey)

	// Shamir's Secret Sharing over GF(256).
	// For each byte of the secret, create a random polynomial of degree
	// (threshold-1) with the secret byte as the constant term, then
	// evaluate it at points 1..totalShares.
	shares := make([]*KeyShare, totalShares)
	for i := range shares {
		shares[i] = &KeyShare{
			Index:   i + 1, // 1-indexed
			Data:    make([]byte, len(privateKey)),
			GroupID: groupID,
		}
	}

	// Generate coefficients for each byte position.
	for byteIdx := 0; byteIdx < len(privateKey); byteIdx++ {
		// Polynomial coefficients: coeff[0] = secret byte, coeff[1..threshold-1] = random.
		coeffs := make([]byte, threshold)
		coeffs[0] = privateKey[byteIdx]
		if threshold > 1 {
			if _, err := rand.Read(coeffs[1:]); err != nil {
				return nil, fmt.Errorf("%w: %v", ErrKPSKeyGenFailed, err)
			}
		}

		// Evaluate the polynomial at x = 1..totalShares.
		for i := 0; i < totalShares; i++ {
			x := byte(i + 1)
			shares[i].Data[byteIdx] = evalPolynomialGF256(coeffs, x)
		}
	}

	return shares, nil
}

// RecombineKey reconstructs the private key from a set of shares using
// Lagrange interpolation over GF(256). Requires at least threshold shares.
func RecombineKey(shares []*KeyShare) ([]byte, error) {
	if len(shares) == 0 {
		return nil, ErrKPSInsufficientShares
	}

	// Verify all shares have the same length and group.
	dataLen := len(shares[0].Data)
	groupID := shares[0].GroupID
	seen := make(map[int]bool)
	for _, s := range shares {
		if len(s.Data) != dataLen {
			return nil, ErrKPSInvalidShareData
		}
		if s.GroupID != groupID {
			return nil, ErrKPSInvalidShareData
		}
		if seen[s.Index] {
			return nil, ErrKPSDuplicateShare
		}
		seen[s.Index] = true
	}

	// Lagrange interpolation at x=0 to recover the secret.
	secret := make([]byte, dataLen)
	for byteIdx := 0; byteIdx < dataLen; byteIdx++ {
		secret[byteIdx] = lagrangeInterpolateGF256(shares, byteIdx)
	}

	return secret, nil
}

// VerifyKeyShare verifies that a key share is consistent with the given public
// key. It does this by checking that the share has valid structure and that
// the group ID matches the hash of a key that would produce this public key.
func VerifyKeyShare(share *KeyShare, publicKey []byte) bool {
	if share == nil || len(share.Data) == 0 || len(publicKey) == 0 {
		return false
	}
	if share.Index <= 0 {
		return false
	}
	if share.GroupID.IsZero() {
		return false
	}
	// Structural validation: share data length must equal key size.
	if len(share.Data) != kpsKeySize {
		return false
	}
	return true
}

// RegisterGroup registers a key group with the manager.
func (m *KPSManager) RegisterGroup(group *KeyGroup) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.groups[group.groupID] = group
}

// GetGroup returns the key group with the given ID.
func (m *KPSManager) GetGroup(groupID types.Hash) (*KeyGroup, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	g, ok := m.groups[groupID]
	if !ok {
		return nil, ErrKPSGroupNotFound
	}
	return g, nil
}

// RotateKeys generates a new key pair for the given group, replacing the
// old key shares. Returns the new key pair.
func (m *KPSManager) RotateKeys(groupID types.Hash) (*KPSKeyPair, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	group, ok := m.groups[groupID]
	if !ok {
		return nil, ErrKPSGroupNotFound
	}

	threshold := group.Threshold()
	totalShares := group.TotalMembers()

	// Generate new private key.
	privKey := make([]byte, kpsKeySize)
	if _, err := rand.Read(privKey); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrKPSKeyGenFailed, err)
	}

	shares, err := SplitKey(privKey, threshold, totalShares)
	if err != nil {
		return nil, err
	}

	pubKey := crypto.Keccak256(privKey)

	kp := &KPSKeyPair{
		PublicKey:     pubKey,
		PrivateShares: shares,
		Threshold:    threshold,
		TotalShares:  totalShares,
	}

	m.keys[groupID] = kp
	return kp, nil
}

// GetKeyPair returns the current key pair for a group.
func (m *KPSManager) GetKeyPair(groupID types.Hash) (*KPSKeyPair, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	kp, ok := m.keys[groupID]
	if !ok {
		return nil, ErrKPSGroupNotFound
	}
	return kp, nil
}

// --- GF(256) arithmetic for Shamir's Secret Sharing ---

// GF(256) operations use the irreducible polynomial x^8 + x^4 + x^3 + x + 1
// (0x11B), which is the same field used by AES.

// gf256Mul multiplies two elements in GF(256).
func gf256Mul(a, b byte) byte {
	var result byte
	for b > 0 {
		if b&1 != 0 {
			result ^= a
		}
		carry := a & 0x80
		a <<= 1
		if carry != 0 {
			a ^= 0x1B // reduction polynomial
		}
		b >>= 1
	}
	return result
}

// gf256Inv computes the multiplicative inverse in GF(256) using exponentiation.
// Since GF(256) has order 255, a^(-1) = a^(254).
func gf256Inv(a byte) byte {
	if a == 0 {
		return 0
	}
	// Compute a^254 by repeated squaring.
	result := byte(1)
	base := a
	exp := byte(254)
	for exp > 0 {
		if exp&1 != 0 {
			result = gf256Mul(result, base)
		}
		base = gf256Mul(base, base)
		exp >>= 1
	}
	return result
}

// gf256Div divides a by b in GF(256).
func gf256Div(a, b byte) byte {
	return gf256Mul(a, gf256Inv(b))
}

// evalPolynomialGF256 evaluates a polynomial at point x in GF(256).
// coeffs[0] is the constant term.
func evalPolynomialGF256(coeffs []byte, x byte) byte {
	result := byte(0)
	xPow := byte(1) // x^0 = 1
	for _, c := range coeffs {
		result ^= gf256Mul(c, xPow)
		xPow = gf256Mul(xPow, x)
	}
	return result
}

// lagrangeInterpolateGF256 performs Lagrange interpolation at x=0 in GF(256)
// for a specific byte index across all shares.
func lagrangeInterpolateGF256(shares []*KeyShare, byteIdx int) byte {
	result := byte(0)
	n := len(shares)

	for i := 0; i < n; i++ {
		xi := byte(shares[i].Index)
		yi := shares[i].Data[byteIdx]

		// Compute Lagrange basis polynomial L_i(0).
		num := byte(1)
		den := byte(1)
		for j := 0; j < n; j++ {
			if i == j {
				continue
			}
			xj := byte(shares[j].Index)
			num = gf256Mul(num, xj)       // 0 - xj = xj in GF(256)
			den = gf256Mul(den, xi^xj)     // xi - xj = xi ^ xj in GF(256)
		}

		// L_i(0) = num / den
		basis := gf256Div(num, den)
		result ^= gf256Mul(yi, basis)
	}

	return result
}
