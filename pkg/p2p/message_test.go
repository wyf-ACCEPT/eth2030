package p2p

import (
	"testing"
)

func TestForkID(t *testing.T) {
	// Mainnet genesis fork ID (Frontier, no next fork initially).
	fid := ForkID{
		Hash: [4]byte{0xfc, 0x64, 0xec, 0x04},
		Next: 0,
	}

	if fid.Hash != [4]byte{0xfc, 0x64, 0xec, 0x04} {
		t.Errorf("ForkID.Hash = %x, want fc64ec04", fid.Hash)
	}
	if fid.Next != 0 {
		t.Errorf("ForkID.Next = %d, want 0", fid.Next)
	}
}

func TestForkIDWithNext(t *testing.T) {
	// ForkID with a next fork block scheduled.
	fid := ForkID{
		Hash: [4]byte{0xa0, 0x0b, 0xc3, 0x24},
		Next: 17034870,
	}

	if fid.Next != 17034870 {
		t.Errorf("ForkID.Next = %d, want 17034870", fid.Next)
	}
}

func TestEncodeDecodeMessage(t *testing.T) {
	type testPayload struct {
		Value uint64
		Name  string
	}

	original := testPayload{Value: 42, Name: "hello"}
	msg, err := EncodeMessage(StatusMsg, original)
	if err != nil {
		t.Fatalf("EncodeMessage error: %v", err)
	}

	if msg.Code != StatusMsg {
		t.Errorf("msg.Code = 0x%02x, want 0x%02x", msg.Code, StatusMsg)
	}
	if msg.Size == 0 {
		t.Error("msg.Size = 0, want > 0")
	}
	if len(msg.Payload) == 0 {
		t.Error("msg.Payload is empty")
	}
	if msg.Size != uint32(len(msg.Payload)) {
		t.Errorf("msg.Size = %d, Payload length = %d, should match", msg.Size, len(msg.Payload))
	}

	var decoded testPayload
	if err := DecodeMessage(msg, &decoded); err != nil {
		t.Fatalf("DecodeMessage error: %v", err)
	}
	if decoded.Value != 42 {
		t.Errorf("decoded.Value = %d, want 42", decoded.Value)
	}
	if decoded.Name != "hello" {
		t.Errorf("decoded.Name = %q, want %q", decoded.Name, "hello")
	}
}

func TestEncodeDecodeStatusMsg(t *testing.T) {
	// Encode and decode a ForkID (simpler than full StatusData since big.Int
	// encoding requires special handling at the protocol level).
	original := ForkID{
		Hash: [4]byte{0xab, 0xcd, 0xef, 0x01},
		Next: 1920000,
	}

	msg, err := EncodeMessage(StatusMsg, original)
	if err != nil {
		t.Fatalf("EncodeMessage error: %v", err)
	}

	var decoded ForkID
	if err := DecodeMessage(msg, &decoded); err != nil {
		t.Fatalf("DecodeMessage error: %v", err)
	}

	if decoded.Hash != original.Hash {
		t.Errorf("decoded Hash = %x, want %x", decoded.Hash, original.Hash)
	}
	if decoded.Next != original.Next {
		t.Errorf("decoded Next = %d, want %d", decoded.Next, original.Next)
	}
}

func TestEncodeDecodeUint64Payload(t *testing.T) {
	var val uint64 = 12345
	msg, err := EncodeMessage(TransactionsMsg, val)
	if err != nil {
		t.Fatalf("EncodeMessage error: %v", err)
	}

	var decoded uint64
	if err := DecodeMessage(msg, &decoded); err != nil {
		t.Fatalf("DecodeMessage error: %v", err)
	}
	if decoded != val {
		t.Errorf("decoded = %d, want %d", decoded, val)
	}
}

func TestValidateMessageCode(t *testing.T) {
	validCodes := []uint64{
		StatusMsg, NewBlockHashesMsg, TransactionsMsg,
		GetBlockHeadersMsg, BlockHeadersMsg,
		GetBlockBodiesMsg, BlockBodiesMsg,
		NewBlockMsg, GetReceiptsMsg, ReceiptsMsg,
	}
	for _, code := range validCodes {
		if err := ValidateMessageCode(code); err != nil {
			t.Errorf("ValidateMessageCode(0x%02x) = %v, want nil", code, err)
		}
	}

	// Invalid codes.
	invalidCodes := []uint64{0x08, 0x0b, 0x0c, 0xff, 0x100}
	for _, code := range invalidCodes {
		if err := ValidateMessageCode(code); err == nil {
			t.Errorf("ValidateMessageCode(0x%02x) = nil, want error", code)
		}
	}
}

func TestMessageName(t *testing.T) {
	tests := []struct {
		code uint64
		name string
	}{
		{StatusMsg, "Status"},
		{NewBlockHashesMsg, "NewBlockHashes"},
		{TransactionsMsg, "Transactions"},
		{GetBlockHeadersMsg, "GetBlockHeaders"},
		{BlockHeadersMsg, "BlockHeaders"},
		{GetBlockBodiesMsg, "GetBlockBodies"},
		{BlockBodiesMsg, "BlockBodies"},
		{NewBlockMsg, "NewBlock"},
		{GetReceiptsMsg, "GetReceipts"},
		{ReceiptsMsg, "Receipts"},
	}
	for _, tt := range tests {
		if got := MessageName(tt.code); got != tt.name {
			t.Errorf("MessageName(0x%02x) = %q, want %q", tt.code, got, tt.name)
		}
	}

	// Unknown code.
	unknown := MessageName(0xff)
	if unknown == "" {
		t.Error("MessageName(0xff) returned empty string")
	}
}

func TestMaxMessageSize(t *testing.T) {
	if MaxMessageSize != 16*1024*1024 {
		t.Errorf("MaxMessageSize = %d, want %d", MaxMessageSize, 16*1024*1024)
	}
}

func TestMessageErrors(t *testing.T) {
	if ErrMessageTooLarge == nil {
		t.Error("ErrMessageTooLarge is nil")
	}
	if ErrInvalidMsgCode == nil {
		t.Error("ErrInvalidMsgCode is nil")
	}
	if ErrDecode == nil {
		t.Error("ErrDecode is nil")
	}
}

func TestDecodeMessageError(t *testing.T) {
	// Construct a message with invalid RLP payload.
	msg := Message{
		Code:    StatusMsg,
		Size:    3,
		Payload: []byte{0xff, 0xff, 0xff},
	}

	var decoded ForkID
	err := DecodeMessage(msg, &decoded)
	if err == nil {
		t.Error("DecodeMessage with invalid payload should return error")
	}
}
