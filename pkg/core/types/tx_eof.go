package types

import "errors"

// EOF validation errors.
var (
	ErrEOFInitcodeTooShort       = errors.New("EOF initcode too short")
	ErrEOFInitcodeInvalidMagic   = errors.New("EOF initcode: invalid magic bytes")
	ErrEOFInitcodeInvalidVersion = errors.New("EOF initcode: unsupported version")
)

// EOF-related constants for creation transactions (EIP-7698).
const (
	// eofMagic0 and eofMagic1 are the EOF container magic bytes.
	eofMagic0 byte = 0xEF
	eofMagic1 byte = 0x00

	// EOFInitcodeWordGas is the per-32-byte-word gas cost for EOF initcode
	// (same as EIP-3860 InitCodeWordGas).
	EOFInitcodeWordGas uint64 = 2

	// EOFCreateBaseGas is the base gas cost for an EOF creation transaction.
	EOFCreateBaseGas uint64 = 32000
)

// EOFCreateResult holds the result of an EOF creation transaction execution.
type EOFCreateResult struct {
	Address Address
	Code    []byte
	GasUsed uint64
}

// IsEOFInitcode returns true if data starts with the EOF magic bytes 0xEF00.
// This is used to identify EOF creation transactions per EIP-7698.
func IsEOFInitcode(data []byte) bool {
	return len(data) >= 2 && data[0] == eofMagic0 && data[1] == eofMagic1
}

// ValidateEOFInitcode performs basic validation that data is plausible EOF
// initcode: it must start with 0xEF0001 (EOF v1 magic + version).
// Full structural validation is done by the vm.ParseEOF / vm.ValidateEOF functions.
func ValidateEOFInitcode(data []byte) error {
	if len(data) < 3 {
		return ErrEOFInitcodeTooShort
	}
	if data[0] != eofMagic0 || data[1] != eofMagic1 {
		return ErrEOFInitcodeInvalidMagic
	}
	if data[2] != 0x01 {
		return ErrEOFInitcodeInvalidVersion
	}
	return nil
}

// ComputeEOFCreateGas computes the intrinsic gas cost for an EOF creation
// transaction based on the length of the initcode.
// Gas = EOFCreateBaseGas + EOFInitcodeWordGas * ceil(initcodeLen / 32)
func ComputeEOFCreateGas(initcodeLen int) uint64 {
	if initcodeLen <= 0 {
		return EOFCreateBaseGas
	}
	words := (uint64(initcodeLen) + 31) / 32
	return EOFCreateBaseGas + EOFInitcodeWordGas*words
}
