package p2p

import (
	"errors"
	"fmt"

	"github.com/eth2028/eth2028/rlp"
)

var (
	// ErrMessageTooLarge is returned when a message exceeds the protocol size limit.
	ErrMessageTooLarge = errors.New("p2p: message too large")

	// ErrInvalidMsgCode is returned when a message has an unrecognised code.
	ErrInvalidMsgCode = errors.New("p2p: invalid message code")

	// ErrDecode is returned when RLP decoding fails.
	ErrDecode = errors.New("p2p: decode error")
)

// MaxMessageSize is the maximum allowed size of a protocol message payload (16 MiB).
const MaxMessageSize = 16 * 1024 * 1024

// Message represents a devp2p protocol message.
type Message struct {
	Code    uint64 // Protocol message code.
	Size    uint32 // Size of the RLP-encoded payload.
	Payload []byte // RLP-encoded payload bytes.
}

// ForkID is the EIP-2124 fork identifier for chain compatibility checks.
// It consists of a CRC32 checksum of the genesis hash and fork block numbers,
// plus the block number of the next expected fork.
type ForkID struct {
	Hash [4]byte // CRC32 checksum of the genesis hash and passed fork block numbers.
	Next uint64  // Block number of the next expected fork, or 0 if no fork is planned.
}

// EncodeMessage encodes a value into a Message with the given message code.
// The value is RLP-encoded to produce the payload.
func EncodeMessage(code uint64, val interface{}) (Message, error) {
	payload, err := rlp.EncodeToBytes(val)
	if err != nil {
		return Message{}, fmt.Errorf("p2p: failed to encode message 0x%02x: %w", code, err)
	}
	if len(payload) > MaxMessageSize {
		return Message{}, ErrMessageTooLarge
	}
	return Message{
		Code:    code,
		Size:    uint32(len(payload)),
		Payload: payload,
	}, nil
}

// DecodeMessage decodes a Message's payload into the provided value.
// The value must be a pointer to the expected type.
func DecodeMessage(msg Message, val interface{}) error {
	if err := rlp.DecodeBytes(msg.Payload, val); err != nil {
		return fmt.Errorf("%w: code 0x%02x: %v", ErrDecode, msg.Code, err)
	}
	return nil
}

// ValidateMessageCode returns an error if the message code is not a known
// eth/68 protocol message.
func ValidateMessageCode(code uint64) error {
	switch code {
	case StatusMsg, NewBlockHashesMsg, TransactionsMsg,
		GetBlockHeadersMsg, BlockHeadersMsg,
		GetBlockBodiesMsg, BlockBodiesMsg,
		NewBlockMsg, GetReceiptsMsg, ReceiptsMsg:
		return nil
	default:
		return fmt.Errorf("%w: 0x%02x", ErrInvalidMsgCode, code)
	}
}

// MessageName returns a human-readable name for the given message code.
func MessageName(code uint64) string {
	switch code {
	case StatusMsg:
		return "Status"
	case NewBlockHashesMsg:
		return "NewBlockHashes"
	case TransactionsMsg:
		return "Transactions"
	case GetBlockHeadersMsg:
		return "GetBlockHeaders"
	case BlockHeadersMsg:
		return "BlockHeaders"
	case GetBlockBodiesMsg:
		return "GetBlockBodies"
	case BlockBodiesMsg:
		return "BlockBodies"
	case NewBlockMsg:
		return "NewBlock"
	case GetReceiptsMsg:
		return "GetReceipts"
	case ReceiptsMsg:
		return "Receipts"
	default:
		return fmt.Sprintf("Unknown(0x%02x)", code)
	}
}
