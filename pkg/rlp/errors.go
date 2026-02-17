package rlp

import "errors"

var (
	// ErrExpectedString is returned when a list is encountered where a string was expected.
	ErrExpectedString = errors.New("rlp: expected string")

	// ErrExpectedList is returned when a string is encountered where a list was expected.
	ErrExpectedList = errors.New("rlp: expected list")

	// ErrCanonSize is returned when an RLP string uses a non-canonical size encoding.
	ErrCanonSize = errors.New("rlp: non-canonical size information")

	// ErrEOL is returned when the end of the current list has been reached.
	ErrEOL = errors.New("rlp: end of list")

	// ErrCanonInt is returned when an integer uses non-canonical encoding (leading zeros).
	ErrCanonInt = errors.New("rlp: non-canonical integer encoding")

	// ErrNonCanonicalSize is returned when a size prefix is not in canonical form.
	ErrNonCanonicalSize = errors.New("rlp: non-canonical size")

	// ErrUint64Range is returned when a decoded integer exceeds uint64 range.
	ErrUint64Range = errors.New("rlp: uint64 overflow")

	// ErrValueTooLarge is returned when a value is too large to encode.
	ErrValueTooLarge = errors.New("rlp: value too large")
)
