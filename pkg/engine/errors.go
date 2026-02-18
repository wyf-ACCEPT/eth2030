package engine

import "errors"

// Standard Engine API error codes per the execution-apis spec.
var (
	// ErrInvalidParams is returned when the request parameters are invalid.
	ErrInvalidParams = errors.New("invalid params")

	// ErrUnknownPayload is returned when the requested payload is not found.
	ErrUnknownPayload = errors.New("unknown payload")

	// ErrInvalidForkchoiceState is returned when the forkchoice state is invalid.
	ErrInvalidForkchoiceState = errors.New("invalid forkchoice state")

	// ErrInvalidPayloadAttributes is returned when payload attributes are invalid.
	ErrInvalidPayloadAttributes = errors.New("invalid payload attributes")

	// ErrTooLargeRequest is returned when the request size exceeds limits.
	ErrTooLargeRequest = errors.New("too large request")

	// ErrUnsupportedFork is returned when the requested fork is not supported.
	ErrUnsupportedFork = errors.New("unsupported fork")

	// ErrInvalidBlockHash is returned when the block hash in the payload
	// does not match the computed block hash.
	ErrInvalidBlockHash = errors.New("invalid block hash")

	// ErrInvalidBlobHashes is returned when the blob versioned hashes
	// in the payload do not match the expected hashes from the CL.
	ErrInvalidBlobHashes = errors.New("invalid blob versioned hashes")

	// ErrMissingBeaconRoot is returned when the parent beacon block root
	// is missing (zero) in a V3+ newPayload call.
	ErrMissingBeaconRoot = errors.New("missing parent beacon block root")
)

// Standard JSON-RPC 2.0 error codes.
const (
	ParseErrorCode     = -32700
	InvalidRequestCode = -32600
	MethodNotFoundCode = -32601
	InvalidParamsCode  = -32602
	InternalErrorCode  = -32603
)

// Engine API specific error codes (per execution-apis spec).
const (
	UnknownPayloadCode          = -38001
	InvalidForkchoiceStateCode  = -38002
	InvalidPayloadAttributeCode = -38003
	TooLargeRequestCode         = -38004
	UnsupportedForkCode         = -38005
)
