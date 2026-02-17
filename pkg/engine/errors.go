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
)

// JSON-RPC error codes for Engine API.
const (
	InvalidParamsCode           = -32602
	UnknownPayloadCode          = -38001
	InvalidForkchoiceStateCode  = -38002
	InvalidPayloadAttributeCode = -38003
	TooLargeRequestCode         = -38004
	UnsupportedForkCode         = -38005
)
