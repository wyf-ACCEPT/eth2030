// Package vm implements the Ethereum Virtual Machine.
//
// precompile_adapters.go provides exported adapter types for eth2030's custom
// precompiles, enabling the geth adapter package to reference them. Each
// adapter wraps the corresponding unexported precompile struct.
package vm

// BN256AddGlamsterdanAdapter exposes the Glamsterdam-repriced bn256Add.
type BN256AddGlamsterdanAdapter struct{ bn256AddGlamsterdan }

// BN256PairingGlamsterdanAdapter exposes the Glamsterdam-repriced bn256Pairing.
type BN256PairingGlamsterdanAdapter struct{ bn256PairingGlamsterdan }

// Blake2FGlamsterdanAdapter exposes the Glamsterdam-repriced blake2F.
type Blake2FGlamsterdanAdapter struct{ blake2FGlamsterdan }

// KZGPointEvalGlamsterdanAdapter exposes the Glamsterdam-repriced kzgPointEval.
type KZGPointEvalGlamsterdanAdapter struct{ kzgPointEvaluationGlamsterdan }

// NTTPrecompileAdapter exposes the NTT precompile (0x15).
type NTTPrecompileAdapter struct{ nttPrecompile }

// NiiModExpAdapter exposes the NII modexp precompile (0x0201).
type NiiModExpAdapter struct{ NiiModExpPrecompile }

// NiiFieldMulAdapter exposes the NII field multiplication precompile (0x0202).
type NiiFieldMulAdapter struct{ NiiFieldMulPrecompile }

// NiiFieldInvAdapter exposes the NII field inverse precompile (0x0203).
type NiiFieldInvAdapter struct{ NiiFieldInvPrecompile }

// NiiBatchVerifyAdapter exposes the NII batch verify precompile (0x0204).
type NiiBatchVerifyAdapter struct{ NiiBatchVerifyPrecompile }

// FieldMulExtAdapter exposes the extended field multiplication precompile (0x0205).
type FieldMulExtAdapter struct{ FieldMulExtPrecompile }

// FieldInvExtAdapter exposes the extended field inverse precompile (0x0206).
type FieldInvExtAdapter struct{ FieldInvExtPrecompile }

// FieldExpAdapter exposes the field exponentiation precompile (0x0207).
type FieldExpAdapter struct{ FieldExpPrecompile }

// BatchFieldVerifyAdapter exposes the batch field verify precompile (0x0208).
type BatchFieldVerifyAdapter struct{ BatchFieldVerifyPrecompile }
