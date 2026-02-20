package das

import (
	"encoding/binary"
	"errors"
	"fmt"
	"sync"
)

// Cell message codec constants.
const (
	// CellMessageVersion is the codec version byte.
	CellMessageVersion = 0x01

	// CellMessageHeaderSize is the fixed header size in bytes:
	// version(1) + cellIndex(2) + columnIndex(2) + rowIndex(2) + dataLen(4) + proofLen(2) = 13
	CellMessageHeaderSize = 13

	// MaxCellDataSize is the maximum cell data size (same as BytesPerCell).
	MaxCellDataSize = BytesPerCell // 2048

	// MaxCellProofSize is the maximum KZG proof size (48 bytes compressed G1).
	MaxCellProofSize = 48

	// BatchHeaderSize is the size of the batch header: version(1) + count(4) = 5
	BatchHeaderSize = 5

	// MaxBatchSize is the maximum number of cell messages in a batch.
	MaxBatchSize = 1024

	// MaxColumnIndex is the maximum column index (same as NumberOfColumns).
	MaxColumnIndex = NumberOfColumns

	// MaxRowIndex is the maximum row index (blobs per block).
	MaxRowIndex = MaxBlobCommitmentsPerBlock
)

// Cell message errors.
var (
	ErrCellMsgNil           = errors.New("das: cell message is nil")
	ErrCellMsgDataEmpty     = errors.New("das: cell message data is empty")
	ErrCellMsgDataTooLarge  = errors.New("das: cell message data exceeds maximum size")
	ErrCellMsgProofTooLarge = errors.New("das: cell message proof exceeds maximum size")
	ErrCellMsgCellIndex     = errors.New("das: cell index out of range")
	ErrCellMsgColumnIndex   = errors.New("das: column index out of range")
	ErrCellMsgRowIndex      = errors.New("das: row index out of range")
	ErrCellMsgDecode        = errors.New("das: failed to decode cell message")
	ErrCellMsgVersion       = errors.New("das: unsupported cell message version")
	ErrBatchTooLarge        = errors.New("das: batch exceeds maximum size")
	ErrBatchEmpty           = errors.New("das: batch is empty")
	ErrBatchDecode          = errors.New("das: failed to decode batch")
)

// CellMessageEntry represents a cell-level DAS message with its position
// in the extended data matrix and the KZG commitment proof.
type CellMessageEntry struct {
	// CellIndex is the cell's position within the extended blob (0..CellsPerExtBlob-1).
	CellIndex uint16

	// ColumnIndex identifies the column in the data matrix (0..NumberOfColumns-1).
	ColumnIndex uint16

	// RowIndex identifies the row (blob index) in the data matrix.
	RowIndex uint16

	// Data is the raw cell payload (up to BytesPerCell bytes).
	Data []byte

	// Proof is the KZG commitment proof for this cell (48 bytes).
	Proof []byte
}

// CellMessageCodec handles encoding and decoding of cell-level messages.
type CellMessageCodec struct{}

// NewCellMessageCodec creates a new codec instance.
func NewCellMessageCodec() *CellMessageCodec {
	return &CellMessageCodec{}
}

// EncodeCellMessage serializes a CellMessageEntry into a byte slice.
// Format: version(1) | cellIndex(2) | columnIndex(2) | rowIndex(2) | dataLen(4) | proofLen(2) | data | proof
func (c *CellMessageCodec) EncodeCellMessage(msg *CellMessageEntry) ([]byte, error) {
	if err := ValidateCellMessageEntry(msg); err != nil {
		return nil, err
	}

	dataLen := len(msg.Data)
	proofLen := len(msg.Proof)
	total := CellMessageHeaderSize + dataLen + proofLen
	buf := make([]byte, total)

	buf[0] = CellMessageVersion
	binary.BigEndian.PutUint16(buf[1:3], msg.CellIndex)
	binary.BigEndian.PutUint16(buf[3:5], msg.ColumnIndex)
	binary.BigEndian.PutUint16(buf[5:7], msg.RowIndex)
	binary.BigEndian.PutUint32(buf[7:11], uint32(dataLen))
	binary.BigEndian.PutUint16(buf[11:13], uint16(proofLen))

	copy(buf[CellMessageHeaderSize:CellMessageHeaderSize+dataLen], msg.Data)
	copy(buf[CellMessageHeaderSize+dataLen:], msg.Proof)

	return buf, nil
}

// DecodeCellMessage deserializes a byte slice into a CellMessageEntry.
func (c *CellMessageCodec) DecodeCellMessage(data []byte) (*CellMessageEntry, error) {
	if len(data) < CellMessageHeaderSize {
		return nil, fmt.Errorf("%w: data too short (%d bytes)", ErrCellMsgDecode, len(data))
	}

	version := data[0]
	if version != CellMessageVersion {
		return nil, fmt.Errorf("%w: got %d, want %d", ErrCellMsgVersion, version, CellMessageVersion)
	}

	cellIndex := binary.BigEndian.Uint16(data[1:3])
	columnIndex := binary.BigEndian.Uint16(data[3:5])
	rowIndex := binary.BigEndian.Uint16(data[5:7])
	dataLen := binary.BigEndian.Uint32(data[7:11])
	proofLen := binary.BigEndian.Uint16(data[11:13])

	expectedTotal := CellMessageHeaderSize + int(dataLen) + int(proofLen)
	if len(data) < expectedTotal {
		return nil, fmt.Errorf("%w: expected %d bytes, got %d", ErrCellMsgDecode, expectedTotal, len(data))
	}

	cellData := make([]byte, dataLen)
	copy(cellData, data[CellMessageHeaderSize:CellMessageHeaderSize+int(dataLen)])

	proof := make([]byte, proofLen)
	copy(proof, data[CellMessageHeaderSize+int(dataLen):CellMessageHeaderSize+int(dataLen)+int(proofLen)])

	msg := &CellMessageEntry{
		CellIndex:   cellIndex,
		ColumnIndex: columnIndex,
		RowIndex:    rowIndex,
		Data:        cellData,
		Proof:       proof,
	}

	return msg, nil
}

// BatchCellMessages encodes multiple cell messages into a single batch.
// Format: version(1) | count(4) | [encoded_message_len(4) | encoded_message]*
func (c *CellMessageCodec) BatchCellMessages(cells []*CellMessageEntry) ([]byte, error) {
	if len(cells) == 0 {
		return nil, ErrBatchEmpty
	}
	if len(cells) > MaxBatchSize {
		return nil, fmt.Errorf("%w: %d cells", ErrBatchTooLarge, len(cells))
	}

	// Encode all messages first to compute total size.
	encodedMsgs := make([][]byte, len(cells))
	totalSize := BatchHeaderSize
	for i, cell := range cells {
		enc, err := c.EncodeCellMessage(cell)
		if err != nil {
			return nil, fmt.Errorf("encoding cell %d: %w", i, err)
		}
		encodedMsgs[i] = enc
		totalSize += 4 + len(enc) // 4 bytes for length prefix
	}

	buf := make([]byte, totalSize)
	buf[0] = CellMessageVersion
	binary.BigEndian.PutUint32(buf[1:5], uint32(len(cells)))

	offset := BatchHeaderSize
	for _, enc := range encodedMsgs {
		binary.BigEndian.PutUint32(buf[offset:offset+4], uint32(len(enc)))
		offset += 4
		copy(buf[offset:], enc)
		offset += len(enc)
	}

	return buf, nil
}

// DecodeBatchCellMessages decodes a batch of cell messages.
func (c *CellMessageCodec) DecodeBatchCellMessages(data []byte) ([]*CellMessageEntry, error) {
	if len(data) < BatchHeaderSize {
		return nil, fmt.Errorf("%w: data too short (%d bytes)", ErrBatchDecode, len(data))
	}

	version := data[0]
	if version != CellMessageVersion {
		return nil, fmt.Errorf("%w: got version %d, want %d", ErrCellMsgVersion, version, CellMessageVersion)
	}

	count := binary.BigEndian.Uint32(data[1:5])
	if count == 0 {
		return nil, ErrBatchEmpty
	}
	if count > MaxBatchSize {
		return nil, fmt.Errorf("%w: %d cells", ErrBatchTooLarge, count)
	}

	cells := make([]*CellMessageEntry, 0, count)
	offset := BatchHeaderSize

	for i := uint32(0); i < count; i++ {
		if offset+4 > len(data) {
			return nil, fmt.Errorf("%w: truncated at message %d length prefix", ErrBatchDecode, i)
		}
		msgLen := binary.BigEndian.Uint32(data[offset : offset+4])
		offset += 4

		if offset+int(msgLen) > len(data) {
			return nil, fmt.Errorf("%w: truncated at message %d body", ErrBatchDecode, i)
		}

		msg, err := c.DecodeCellMessage(data[offset : offset+int(msgLen)])
		if err != nil {
			return nil, fmt.Errorf("decoding message %d: %w", i, err)
		}
		cells = append(cells, msg)
		offset += int(msgLen)
	}

	return cells, nil
}

// ValidateCellMessageEntry validates that a cell message has correct indices
// and data sizes.
func ValidateCellMessageEntry(msg *CellMessageEntry) error {
	if msg == nil {
		return ErrCellMsgNil
	}
	if int(msg.CellIndex) >= CellsPerExtBlob {
		return fmt.Errorf("%w: %d >= %d", ErrCellMsgCellIndex, msg.CellIndex, CellsPerExtBlob)
	}
	if int(msg.ColumnIndex) >= MaxColumnIndex {
		return fmt.Errorf("%w: %d >= %d", ErrCellMsgColumnIndex, msg.ColumnIndex, MaxColumnIndex)
	}
	if int(msg.RowIndex) >= MaxRowIndex {
		return fmt.Errorf("%w: %d >= %d", ErrCellMsgRowIndex, msg.RowIndex, MaxRowIndex)
	}
	if len(msg.Data) == 0 {
		return ErrCellMsgDataEmpty
	}
	if len(msg.Data) > MaxCellDataSize {
		return fmt.Errorf("%w: %d > %d", ErrCellMsgDataTooLarge, len(msg.Data), MaxCellDataSize)
	}
	if len(msg.Proof) > MaxCellProofSize {
		return fmt.Errorf("%w: %d > %d", ErrCellMsgProofTooLarge, len(msg.Proof), MaxCellProofSize)
	}
	return nil
}

// CellMessageHandler is a callback invoked when a cell message is routed.
type CellMessageHandler func(msg *CellMessageEntry) error

// CellMessageRouter routes cell-level messages to appropriate DAS validators
// based on column index assignments.
type CellMessageRouter struct {
	mu       sync.RWMutex
	handlers map[uint16][]CellMessageHandler // handlers per column index
	global   []CellMessageHandler            // handlers for all messages
}

// NewCellMessageRouter creates a new cell message router.
func NewCellMessageRouter() *CellMessageRouter {
	return &CellMessageRouter{
		handlers: make(map[uint16][]CellMessageHandler),
	}
}

// RegisterColumnHandler registers a handler for messages targeting a specific
// column index. Multiple handlers can be registered per column.
func (r *CellMessageRouter) RegisterColumnHandler(columnIndex uint16, handler CellMessageHandler) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.handlers[columnIndex] = append(r.handlers[columnIndex], handler)
}

// RegisterGlobalHandler registers a handler that receives all routed messages.
func (r *CellMessageRouter) RegisterGlobalHandler(handler CellMessageHandler) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.global = append(r.global, handler)
}

// RouteMessage validates and routes a cell message to registered handlers.
// Returns the number of handlers that processed the message and any error
// from the first failing handler.
func (r *CellMessageRouter) RouteMessage(msg *CellMessageEntry) (int, error) {
	if err := ValidateCellMessageEntry(msg); err != nil {
		return 0, err
	}

	r.mu.RLock()
	defer r.mu.RUnlock()

	handled := 0

	// Dispatch to column-specific handlers.
	if handlers, ok := r.handlers[msg.ColumnIndex]; ok {
		for _, h := range handlers {
			if err := h(msg); err != nil {
				return handled, err
			}
			handled++
		}
	}

	// Dispatch to global handlers.
	for _, h := range r.global {
		if err := h(msg); err != nil {
			return handled, err
		}
		handled++
	}

	return handled, nil
}

// HandlerCount returns the total number of registered handlers.
func (r *CellMessageRouter) HandlerCount() int {
	r.mu.RLock()
	defer r.mu.RUnlock()

	count := len(r.global)
	for _, handlers := range r.handlers {
		count += len(handlers)
	}
	return count
}

// ColumnHandlerCount returns the number of handlers for a specific column.
func (r *CellMessageRouter) ColumnHandlerCount(columnIndex uint16) int {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return len(r.handlers[columnIndex])
}
