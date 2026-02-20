package das

import (
	"errors"
	"testing"
)

func makeValidCellMsg(cellIdx, colIdx, rowIdx uint16) *CellMessageEntry {
	return &CellMessageEntry{
		CellIndex:   cellIdx,
		ColumnIndex: colIdx,
		RowIndex:    rowIdx,
		Data:        make([]byte, 64),
		Proof:       make([]byte, 48),
	}
}

func TestEncodeDecode_CellMessage(t *testing.T) {
	codec := NewCellMessageCodec()

	msg := &CellMessageEntry{
		CellIndex:   10,
		ColumnIndex: 5,
		RowIndex:    2,
		Data:        []byte{0xAA, 0xBB, 0xCC, 0xDD},
		Proof:       []byte{0x01, 0x02, 0x03},
	}

	encoded, err := codec.EncodeCellMessage(msg)
	if err != nil {
		t.Fatalf("EncodeCellMessage: %v", err)
	}

	decoded, err := codec.DecodeCellMessage(encoded)
	if err != nil {
		t.Fatalf("DecodeCellMessage: %v", err)
	}

	if decoded.CellIndex != msg.CellIndex {
		t.Errorf("CellIndex = %d, want %d", decoded.CellIndex, msg.CellIndex)
	}
	if decoded.ColumnIndex != msg.ColumnIndex {
		t.Errorf("ColumnIndex = %d, want %d", decoded.ColumnIndex, msg.ColumnIndex)
	}
	if decoded.RowIndex != msg.RowIndex {
		t.Errorf("RowIndex = %d, want %d", decoded.RowIndex, msg.RowIndex)
	}
	if len(decoded.Data) != len(msg.Data) {
		t.Errorf("Data length = %d, want %d", len(decoded.Data), len(msg.Data))
	}
	for i := range decoded.Data {
		if decoded.Data[i] != msg.Data[i] {
			t.Errorf("Data[%d] = 0x%02x, want 0x%02x", i, decoded.Data[i], msg.Data[i])
		}
	}
	if len(decoded.Proof) != len(msg.Proof) {
		t.Errorf("Proof length = %d, want %d", len(decoded.Proof), len(msg.Proof))
	}
}

func TestEncodeDecode_BatchCellMessages(t *testing.T) {
	codec := NewCellMessageCodec()

	cells := []*CellMessageEntry{
		{CellIndex: 0, ColumnIndex: 0, RowIndex: 0, Data: []byte{1, 2, 3}, Proof: []byte{10, 20}},
		{CellIndex: 5, ColumnIndex: 3, RowIndex: 1, Data: []byte{4, 5, 6, 7}, Proof: []byte{30}},
		{CellIndex: 10, ColumnIndex: 7, RowIndex: 2, Data: []byte{8}, Proof: []byte{40, 50, 60}},
	}

	batch, err := codec.BatchCellMessages(cells)
	if err != nil {
		t.Fatalf("BatchCellMessages: %v", err)
	}

	decoded, err := codec.DecodeBatchCellMessages(batch)
	if err != nil {
		t.Fatalf("DecodeBatchCellMessages: %v", err)
	}

	if len(decoded) != len(cells) {
		t.Fatalf("decoded %d cells, want %d", len(decoded), len(cells))
	}

	for i := range decoded {
		if decoded[i].CellIndex != cells[i].CellIndex {
			t.Errorf("cell[%d] CellIndex = %d, want %d", i, decoded[i].CellIndex, cells[i].CellIndex)
		}
		if decoded[i].ColumnIndex != cells[i].ColumnIndex {
			t.Errorf("cell[%d] ColumnIndex = %d, want %d", i, decoded[i].ColumnIndex, cells[i].ColumnIndex)
		}
		if decoded[i].RowIndex != cells[i].RowIndex {
			t.Errorf("cell[%d] RowIndex = %d, want %d", i, decoded[i].RowIndex, cells[i].RowIndex)
		}
	}
}

func TestValidateCellMessageEntry_Valid(t *testing.T) {
	msg := makeValidCellMsg(10, 5, 2)
	if err := ValidateCellMessageEntry(msg); err != nil {
		t.Fatalf("valid message rejected: %v", err)
	}
}

func TestValidateCellMessageEntry_Errors(t *testing.T) {
	tests := []struct {
		name    string
		msg     *CellMessageEntry
		wantErr error
	}{
		{
			name:    "nil message",
			msg:     nil,
			wantErr: ErrCellMsgNil,
		},
		{
			name:    "cell index out of range",
			msg:     &CellMessageEntry{CellIndex: uint16(CellsPerExtBlob), ColumnIndex: 0, RowIndex: 0, Data: []byte{1}},
			wantErr: ErrCellMsgCellIndex,
		},
		{
			name:    "column index out of range",
			msg:     &CellMessageEntry{CellIndex: 0, ColumnIndex: uint16(MaxColumnIndex), RowIndex: 0, Data: []byte{1}},
			wantErr: ErrCellMsgColumnIndex,
		},
		{
			name:    "row index out of range",
			msg:     &CellMessageEntry{CellIndex: 0, ColumnIndex: 0, RowIndex: uint16(MaxRowIndex), Data: []byte{1}},
			wantErr: ErrCellMsgRowIndex,
		},
		{
			name:    "empty data",
			msg:     &CellMessageEntry{CellIndex: 0, ColumnIndex: 0, RowIndex: 0, Data: nil},
			wantErr: ErrCellMsgDataEmpty,
		},
		{
			name:    "data too large",
			msg:     &CellMessageEntry{CellIndex: 0, ColumnIndex: 0, RowIndex: 0, Data: make([]byte, MaxCellDataSize+1)},
			wantErr: ErrCellMsgDataTooLarge,
		},
		{
			name:    "proof too large",
			msg:     &CellMessageEntry{CellIndex: 0, ColumnIndex: 0, RowIndex: 0, Data: []byte{1}, Proof: make([]byte, MaxCellProofSize+1)},
			wantErr: ErrCellMsgProofTooLarge,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateCellMessageEntry(tt.msg)
			if err == nil {
				t.Fatal("expected error, got nil")
			}
			if !errors.Is(err, tt.wantErr) {
				t.Errorf("got error %v, want %v", err, tt.wantErr)
			}
		})
	}
}

func TestCellMessageCodec_DecodeErrors(t *testing.T) {
	codec := NewCellMessageCodec()

	// Data too short.
	_, err := codec.DecodeCellMessage([]byte{0x01})
	if err == nil {
		t.Error("expected error for short data")
	}

	// Wrong version.
	bad := make([]byte, CellMessageHeaderSize)
	bad[0] = 0xFF // wrong version
	_, err = codec.DecodeCellMessage(bad)
	if !errors.Is(err, ErrCellMsgVersion) {
		t.Errorf("expected ErrCellMsgVersion, got %v", err)
	}
}

func TestBatchCellMessages_Errors(t *testing.T) {
	codec := NewCellMessageCodec()

	// Empty batch.
	_, err := codec.BatchCellMessages(nil)
	if err != ErrBatchEmpty {
		t.Errorf("expected ErrBatchEmpty, got %v", err)
	}

	// Batch decode with short data.
	_, err = codec.DecodeBatchCellMessages([]byte{0x01})
	if !errors.Is(err, ErrBatchDecode) {
		t.Errorf("expected ErrBatchDecode, got %v", err)
	}

	// Batch decode with wrong version.
	bad := make([]byte, BatchHeaderSize)
	bad[0] = 0xFF
	_, err = codec.DecodeBatchCellMessages(bad)
	if !errors.Is(err, ErrCellMsgVersion) {
		t.Errorf("expected ErrCellMsgVersion, got %v", err)
	}
}

func TestCellMessageRouter_Route(t *testing.T) {
	router := NewCellMessageRouter()

	var received []*CellMessageEntry
	handler := func(msg *CellMessageEntry) error {
		received = append(received, msg)
		return nil
	}

	// Register handler for column 5.
	router.RegisterColumnHandler(5, handler)

	msg := makeValidCellMsg(10, 5, 2)
	n, err := router.RouteMessage(msg)
	if err != nil {
		t.Fatalf("RouteMessage: %v", err)
	}
	if n != 1 {
		t.Errorf("handled = %d, want 1", n)
	}
	if len(received) != 1 {
		t.Fatalf("received %d messages, want 1", len(received))
	}
	if received[0].CellIndex != 10 {
		t.Errorf("received CellIndex = %d, want 10", received[0].CellIndex)
	}
}

func TestCellMessageRouter_GlobalHandler(t *testing.T) {
	router := NewCellMessageRouter()

	globalCount := 0
	router.RegisterGlobalHandler(func(msg *CellMessageEntry) error {
		globalCount++
		return nil
	})

	msg1 := makeValidCellMsg(0, 0, 0)
	msg2 := makeValidCellMsg(5, 3, 1)

	router.RouteMessage(msg1)
	router.RouteMessage(msg2)

	if globalCount != 2 {
		t.Errorf("global handler invoked %d times, want 2", globalCount)
	}
}

func TestCellMessageRouter_HandlerError(t *testing.T) {
	router := NewCellMessageRouter()

	testErr := errors.New("handler failed")
	router.RegisterColumnHandler(0, func(msg *CellMessageEntry) error {
		return testErr
	})

	msg := makeValidCellMsg(0, 0, 0)
	_, err := router.RouteMessage(msg)
	if !errors.Is(err, testErr) {
		t.Errorf("expected handler error, got %v", err)
	}
}

func TestCellMessageRouter_InvalidMessage(t *testing.T) {
	router := NewCellMessageRouter()

	// Routing a nil message should fail validation.
	_, err := router.RouteMessage(nil)
	if err == nil {
		t.Error("expected error for nil message")
	}
}

func TestCellMessageRouter_HandlerCount(t *testing.T) {
	router := NewCellMessageRouter()

	if router.HandlerCount() != 0 {
		t.Errorf("initial HandlerCount = %d, want 0", router.HandlerCount())
	}

	noop := func(msg *CellMessageEntry) error { return nil }

	router.RegisterColumnHandler(0, noop)
	router.RegisterColumnHandler(0, noop)
	router.RegisterColumnHandler(5, noop)
	router.RegisterGlobalHandler(noop)

	if router.HandlerCount() != 4 {
		t.Errorf("HandlerCount = %d, want 4", router.HandlerCount())
	}
	if router.ColumnHandlerCount(0) != 2 {
		t.Errorf("ColumnHandlerCount(0) = %d, want 2", router.ColumnHandlerCount(0))
	}
	if router.ColumnHandlerCount(5) != 1 {
		t.Errorf("ColumnHandlerCount(5) = %d, want 1", router.ColumnHandlerCount(5))
	}
	if router.ColumnHandlerCount(99) != 0 {
		t.Errorf("ColumnHandlerCount(99) = %d, want 0", router.ColumnHandlerCount(99))
	}
}

func TestCellMessageCodec_FullCellSize(t *testing.T) {
	codec := NewCellMessageCodec()

	msg := &CellMessageEntry{
		CellIndex:   127,
		ColumnIndex: 127,
		RowIndex:    8,
		Data:        make([]byte, BytesPerCell),
		Proof:       make([]byte, 48),
	}

	encoded, err := codec.EncodeCellMessage(msg)
	if err != nil {
		t.Fatalf("EncodeCellMessage: %v", err)
	}

	expectedLen := CellMessageHeaderSize + BytesPerCell + 48
	if len(encoded) != expectedLen {
		t.Errorf("encoded length = %d, want %d", len(encoded), expectedLen)
	}

	decoded, err := codec.DecodeCellMessage(encoded)
	if err != nil {
		t.Fatalf("DecodeCellMessage: %v", err)
	}
	if len(decoded.Data) != BytesPerCell {
		t.Errorf("decoded data length = %d, want %d", len(decoded.Data), BytesPerCell)
	}
}
