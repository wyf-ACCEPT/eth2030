package engine

import (
	"encoding/json"
	"errors"
	"fmt"
)

// Extended engine error variables for all Engine API error conditions.
var (
	// ErrRequestTooLarge is returned when a request exceeds the maximum allowed size.
	ErrRequestTooLarge = errors.New("request too large")

	// ErrServerBusy is returned when the server is too busy to process the request.
	ErrServerBusy = errors.New("server busy")

	// ErrRequestTimeout is returned when request processing times out.
	ErrRequestTimeout = errors.New("request timeout")

	// ErrPayloadNotBuilding is returned when getPayload is called but no
	// payload is being built for the given ID.
	ErrPayloadNotBuilding = errors.New("payload not building")

	// ErrInvalidTerminalBlock is returned when the terminal block does not
	// satisfy the terminal total difficulty condition.
	ErrInvalidTerminalBlock = errors.New("invalid terminal block")

	// ErrPayloadTimestamp is returned when the payload timestamp does not
	// advance beyond the parent block's timestamp.
	ErrPayloadTimestamp = errors.New("invalid payload timestamp")

	// ErrMissingWithdrawals is returned when withdrawals are expected but
	// not provided in V2+ payloads.
	ErrMissingWithdrawals = errors.New("missing withdrawals")

	// ErrMissingExecutionRequests is returned when execution requests are
	// expected but not provided in V4+ payloads.
	ErrMissingExecutionRequests = errors.New("missing execution requests")

	// ErrMissingBlockAccessList is returned when the block access list is
	// expected but not provided in V5+ payloads.
	ErrMissingBlockAccessList = errors.New("missing block access list")
)

// Engine API extended error codes for additional conditions.
const (
	// ServerBusyCode indicates the server is overloaded.
	ServerBusyCode = -32005

	// RequestTimeoutCode indicates the request timed out.
	RequestTimeoutCode = -32006
)

// EngineError is a structured error that carries an Engine API error code
// for proper JSON-RPC encoding.
type EngineError struct {
	Code    int
	Message string
	Cause   error
}

// Error implements the error interface.
func (e *EngineError) Error() string {
	if e.Cause != nil {
		return fmt.Sprintf("%s: %v", e.Message, e.Cause)
	}
	return e.Message
}

// Unwrap returns the underlying cause for errors.Is/errors.As.
func (e *EngineError) Unwrap() error {
	return e.Cause
}

// MarshalJSON encodes the error as a JSON-RPC error object.
func (e *EngineError) MarshalJSON() ([]byte, error) {
	return json.Marshal(struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
	}{
		Code:    e.Code,
		Message: e.Error(),
	})
}

// NewEngineError creates a new EngineError with the given code and message.
func NewEngineError(code int, message string) *EngineError {
	return &EngineError{Code: code, Message: message}
}

// WrapEngineError wraps an existing error with an Engine API error code.
func WrapEngineError(code int, message string, cause error) *EngineError {
	return &EngineError{Code: code, Message: message, Cause: cause}
}

// ErrorCodeFromError maps known engine errors to their corresponding
// JSON-RPC error codes.
func ErrorCodeFromError(err error) int {
	if err == nil {
		return 0
	}

	// Check for EngineError type first.
	var engineErr *EngineError
	if errors.As(err, &engineErr) {
		return engineErr.Code
	}

	switch {
	case errors.Is(err, ErrUnknownPayload), errors.Is(err, ErrPayloadNotBuilding):
		return UnknownPayloadCode
	case errors.Is(err, ErrInvalidForkchoiceState):
		return InvalidForkchoiceStateCode
	case errors.Is(err, ErrInvalidPayloadAttributes):
		return InvalidPayloadAttributeCode
	case errors.Is(err, ErrTooLargeRequest), errors.Is(err, ErrRequestTooLarge):
		return TooLargeRequestCode
	case errors.Is(err, ErrUnsupportedFork):
		return UnsupportedForkCode
	case errors.Is(err, ErrInvalidParams):
		return InvalidParamsCode
	case errors.Is(err, ErrInvalidBlockHash):
		return InvalidParamsCode
	case errors.Is(err, ErrInvalidBlobHashes):
		return InvalidParamsCode
	case errors.Is(err, ErrMissingBeaconRoot):
		return InvalidParamsCode
	case errors.Is(err, ErrServerBusy):
		return ServerBusyCode
	case errors.Is(err, ErrRequestTimeout):
		return RequestTimeoutCode
	default:
		return InternalErrorCode
	}
}

// IsClientError returns true if the error code indicates a client-side error
// (invalid request, params, etc).
func IsClientError(code int) bool {
	return code >= -32699 && code <= -32600
}

// IsServerError returns true if the error code indicates a server-side error.
func IsServerError(code int) bool {
	return code >= -32099 && code <= -32000
}

// IsEngineError returns true if the error code is an Engine API specific
// error (-38001 through -38005).
func IsEngineError(code int) bool {
	return code >= -38005 && code <= -38001
}

// ErrorResponse constructs a full JSON-RPC error response as raw JSON bytes.
func ErrorResponse(id json.RawMessage, code int, message string) []byte {
	resp := struct {
		JSONRPC string          `json:"jsonrpc"`
		Error   interface{}     `json:"error"`
		ID      json.RawMessage `json:"id"`
	}{
		JSONRPC: "2.0",
		Error: struct {
			Code    int    `json:"code"`
			Message string `json:"message"`
		}{Code: code, Message: message},
		ID: id,
	}
	out, _ := json.Marshal(resp)
	return out
}

// ErrorName returns a human-readable name for an Engine API error code.
func ErrorName(code int) string {
	switch code {
	case ParseErrorCode:
		return "ParseError"
	case InvalidRequestCode:
		return "InvalidRequest"
	case MethodNotFoundCode:
		return "MethodNotFound"
	case InvalidParamsCode:
		return "InvalidParams"
	case InternalErrorCode:
		return "InternalError"
	case UnknownPayloadCode:
		return "UnknownPayload"
	case InvalidForkchoiceStateCode:
		return "InvalidForkchoiceState"
	case InvalidPayloadAttributeCode:
		return "InvalidPayloadAttributes"
	case TooLargeRequestCode:
		return "TooLargeRequest"
	case UnsupportedForkCode:
		return "UnsupportedFork"
	case ServerBusyCode:
		return "ServerBusy"
	case RequestTimeoutCode:
		return "RequestTimeout"
	default:
		return fmt.Sprintf("Unknown(%d)", code)
	}
}

// ValidatePayloadVersion checks that required fields are present for the
// given payload version. Returns an appropriate EngineError if validation fails.
func ValidatePayloadVersion(version int, hasWithdrawals, hasExecutionRequests, hasBlockAccessList bool) *EngineError {
	if version >= 2 && !hasWithdrawals {
		return WrapEngineError(InvalidParamsCode, "withdrawals required for V2+ payload", ErrMissingWithdrawals)
	}
	if version >= 4 && !hasExecutionRequests {
		return WrapEngineError(InvalidParamsCode, "execution requests required for V4+ payload", ErrMissingExecutionRequests)
	}
	if version >= 5 && !hasBlockAccessList {
		return WrapEngineError(InvalidParamsCode, "block access list required for V5+ payload", ErrMissingBlockAccessList)
	}
	return nil
}
