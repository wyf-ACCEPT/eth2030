// Package zkvm provides a framework for zkVM guest execution and proof
// verification, supporting EIP-8079 native rollup proof-carrying transactions.
//
// poseidon.go implements the Poseidon hash function, a ZK-friendly hash
// used in circuit-based proofs (R1CS, PLONK, STARKs). Operates over the
// BN254 scalar field for compatibility with Ethereum's BN254 precompiles.
package zkvm

import (
	"math/big"
)

// BN254 scalar field order (curve order, not base field).
// r = 21888242871839275222246405745257275088548364400416034343698204186575808495617
var bn254ScalarField, _ = new(big.Int).SetString(
	"21888242871839275222246405745257275088548364400416034343698204186575808495617", 10,
)

// PoseidonParams holds parameters for the Poseidon hash function.
// Default: t=3 (rate=2, capacity=1), full rounds=8, partial rounds=57.
type PoseidonParams struct {
	// T is the state width (rate + capacity).
	T int

	// FullRounds is the number of full S-box rounds (applied to all elements).
	FullRounds int

	// PartialRounds is the number of partial S-box rounds (applied to element 0).
	PartialRounds int

	// RoundConstants are the additive round constants.
	// Length = T * (FullRounds + PartialRounds).
	RoundConstants []*big.Int

	// MDS is the Maximum Distance Separable matrix (T x T).
	MDS [][]*big.Int

	// Field is the prime field modulus.
	Field *big.Int
}

// DefaultPoseidonParams returns Poseidon parameters for BN254 scalar field
// with t=3, full rounds=8, partial rounds=57.
func DefaultPoseidonParams() *PoseidonParams {
	t := 3
	fullRounds := 8
	partialRounds := 57
	totalRounds := fullRounds + partialRounds
	field := new(big.Int).Set(bn254ScalarField)

	// Generate deterministic round constants via a simple PRNG over the field.
	// In production, these would be derived per the Poseidon paper specification
	// using a Grain LFSR. Here we use a reproducible method.
	rcs := generateRoundConstants(t, totalRounds, field)

	// Generate a Cauchy MDS matrix.
	mds := generateMDS(t, field)

	return &PoseidonParams{
		T:              t,
		FullRounds:     fullRounds,
		PartialRounds:  partialRounds,
		RoundConstants: rcs,
		MDS:            mds,
		Field:          field,
	}
}

// SBox computes x^5 mod field (the Poseidon S-box for BN254).
func SBox(x, field *big.Int) *big.Int {
	x2 := new(big.Int).Mul(x, x)
	x2.Mod(x2, field)
	x4 := new(big.Int).Mul(x2, x2)
	x4.Mod(x4, field)
	x5 := new(big.Int).Mul(x4, x)
	x5.Mod(x5, field)
	return x5
}

// MDSMul multiplies a state vector by the MDS matrix.
func MDSMul(state []*big.Int, mds [][]*big.Int, field *big.Int) []*big.Int {
	t := len(state)
	result := make([]*big.Int, t)
	for i := 0; i < t; i++ {
		sum := new(big.Int)
		for j := 0; j < t; j++ {
			prod := new(big.Int).Mul(mds[i][j], state[j])
			sum.Add(sum, prod)
		}
		sum.Mod(sum, field)
		result[i] = sum
	}
	return result
}

// poseidonPermutation applies the Poseidon permutation to the state.
func poseidonPermutation(state []*big.Int, params *PoseidonParams) []*big.Int {
	t := params.T
	field := params.Field
	halfFull := params.FullRounds / 2
	rcIdx := 0

	// First half of full rounds.
	for r := 0; r < halfFull; r++ {
		// Add round constants.
		for i := 0; i < t; i++ {
			state[i] = new(big.Int).Add(state[i], params.RoundConstants[rcIdx])
			state[i].Mod(state[i], field)
			rcIdx++
		}
		// Full S-box: apply to all elements.
		for i := 0; i < t; i++ {
			state[i] = SBox(state[i], field)
		}
		// MDS mixing.
		state = MDSMul(state, params.MDS, field)
	}

	// Partial rounds.
	for r := 0; r < params.PartialRounds; r++ {
		// Add round constants.
		for i := 0; i < t; i++ {
			state[i] = new(big.Int).Add(state[i], params.RoundConstants[rcIdx])
			state[i].Mod(state[i], field)
			rcIdx++
		}
		// Partial S-box: apply only to element 0.
		state[0] = SBox(state[0], field)
		// MDS mixing.
		state = MDSMul(state, params.MDS, field)
	}

	// Second half of full rounds.
	for r := 0; r < halfFull; r++ {
		// Add round constants.
		for i := 0; i < t; i++ {
			state[i] = new(big.Int).Add(state[i], params.RoundConstants[rcIdx])
			state[i].Mod(state[i], field)
			rcIdx++
		}
		// Full S-box.
		for i := 0; i < t; i++ {
			state[i] = SBox(state[i], field)
		}
		// MDS mixing.
		state = MDSMul(state, params.MDS, field)
	}

	return state
}

// PoseidonHash hashes one or more field elements using the Poseidon hash.
// Uses a sponge construction with rate = T-1 and capacity = 1.
// Returns a single field element.
func PoseidonHash(params *PoseidonParams, inputs ...*big.Int) *big.Int {
	if params == nil {
		params = DefaultPoseidonParams()
	}
	t := params.T
	rate := t - 1 // capacity = 1

	// Initialize state to zeros.
	state := make([]*big.Int, t)
	for i := range state {
		state[i] = new(big.Int)
	}

	// Absorb inputs into rate portion of state.
	for i := 0; i < len(inputs); i += rate {
		for j := 0; j < rate && i+j < len(inputs); j++ {
			val := new(big.Int).Set(inputs[i+j])
			val.Mod(val, params.Field)
			state[j+1].Add(state[j+1], val)
			state[j+1].Mod(state[j+1], params.Field)
		}
		state = poseidonPermutation(state, params)
	}

	// If no inputs, still permute once.
	if len(inputs) == 0 {
		state = poseidonPermutation(state, params)
	}

	return new(big.Int).Set(state[0])
}

// PoseidonSponge implements a sponge construction for variable-length input.
type PoseidonSponge struct {
	params *PoseidonParams
	state  []*big.Int
	buf    []*big.Int
	rate   int
}

// NewPoseidonSponge creates a new Poseidon sponge with the given parameters.
func NewPoseidonSponge(params *PoseidonParams) *PoseidonSponge {
	if params == nil {
		params = DefaultPoseidonParams()
	}
	state := make([]*big.Int, params.T)
	for i := range state {
		state[i] = new(big.Int)
	}
	return &PoseidonSponge{
		params: params,
		state:  state,
		rate:   params.T - 1,
	}
}

// Absorb adds field elements to the sponge.
func (s *PoseidonSponge) Absorb(inputs ...*big.Int) {
	for _, inp := range inputs {
		val := new(big.Int).Set(inp)
		val.Mod(val, s.params.Field)
		s.buf = append(s.buf, val)

		if len(s.buf) == s.rate {
			s.absorbBlock()
		}
	}
}

func (s *PoseidonSponge) absorbBlock() {
	for j := 0; j < len(s.buf); j++ {
		s.state[j+1].Add(s.state[j+1], s.buf[j])
		s.state[j+1].Mod(s.state[j+1], s.params.Field)
	}
	s.state = poseidonPermutation(s.state, s.params)
	s.buf = s.buf[:0]
}

// Squeeze extracts field elements from the sponge.
func (s *PoseidonSponge) Squeeze(count int) []*big.Int {
	// Flush remaining buffer.
	if len(s.buf) > 0 {
		s.absorbBlock()
	}

	results := make([]*big.Int, 0, count)
	for len(results) < count {
		// Extract from rate portion.
		for j := 1; j <= s.rate && len(results) < count; j++ {
			results = append(results, new(big.Int).Set(s.state[j]))
		}
		if len(results) < count {
			s.state = poseidonPermutation(s.state, s.params)
		}
	}
	return results
}

// --- Parameter generation helpers ---

// generateRoundConstants produces deterministic round constants.
// Uses SHA-like iterative hashing over the field.
func generateRoundConstants(t, totalRounds int, field *big.Int) []*big.Int {
	numConstants := t * totalRounds
	constants := make([]*big.Int, numConstants)

	// Seed: hash of "Poseidon" + parameters.
	seed := new(big.Int).SetBytes([]byte("PoseidonBN254"))
	for i := 0; i < numConstants; i++ {
		// c_i = (seed + i)^5 mod field (deterministic derivation).
		val := new(big.Int).Add(seed, big.NewInt(int64(i)))
		val.Exp(val, big.NewInt(5), field)
		constants[i] = val
	}
	return constants
}

// generateMDS produces a Cauchy MDS matrix over the field.
// M[i][j] = 1 / (x_i + y_j) where x and y are distinct field elements.
func generateMDS(t int, field *big.Int) [][]*big.Int {
	// Use x_i = i, y_j = t + j as distinct elements.
	mds := make([][]*big.Int, t)
	for i := 0; i < t; i++ {
		mds[i] = make([]*big.Int, t)
		for j := 0; j < t; j++ {
			sum := new(big.Int).Add(big.NewInt(int64(i)), big.NewInt(int64(t+j)))
			sum.Mod(sum, field)
			// Compute modular inverse: 1/(x_i + y_j).
			inv := new(big.Int).ModInverse(sum, field)
			if inv == nil {
				// Fallback: should not happen with distinct elements in a prime field.
				inv = big.NewInt(1)
			}
			mds[i][j] = inv
		}
	}
	return mds
}
