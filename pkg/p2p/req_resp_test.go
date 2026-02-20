package p2p

import (
	"errors"
	"testing"
	"time"
)

func TestReqResp_BeaconBlocksByRangeEncodeDecode(t *testing.T) {
	req := &BeaconBlocksByRangeRequest{
		StartSlot: 100,
		Count:     64,
		Step:      1,
	}
	data := req.Encode()
	if len(data) != 24 {
		t.Fatalf("expected 24 bytes, got %d", len(data))
	}
	decoded, err := DecodeBeaconBlocksByRange(data)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if decoded.StartSlot != 100 || decoded.Count != 64 || decoded.Step != 1 {
		t.Fatalf("decoded mismatch: %+v", decoded)
	}
}

func TestReqResp_BeaconBlocksByRangeDecodeTooShort(t *testing.T) {
	_, err := DecodeBeaconBlocksByRange([]byte{0x01, 0x02})
	if err == nil {
		t.Fatal("expected error for short data")
	}
}

func TestReqResp_BeaconBlocksByRootEncodeDecode(t *testing.T) {
	root1 := [32]byte{1, 2, 3}
	root2 := [32]byte{4, 5, 6}
	req := &BeaconBlocksByRootRequest{Roots: [][32]byte{root1, root2}}

	data := req.Encode()
	decoded, err := DecodeBeaconBlocksByRoot(data)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(decoded.Roots) != 2 {
		t.Fatalf("expected 2 roots, got %d", len(decoded.Roots))
	}
	if decoded.Roots[0] != root1 {
		t.Fatalf("root 0 mismatch: %v", decoded.Roots[0])
	}
	if decoded.Roots[1] != root2 {
		t.Fatalf("root 1 mismatch: %v", decoded.Roots[1])
	}
}

func TestReqResp_BeaconBlocksByRootDecodeTooShort(t *testing.T) {
	_, err := DecodeBeaconBlocksByRoot([]byte{0x01})
	if err == nil {
		t.Fatal("expected error for short data")
	}
}

func TestReqResp_BlobSidecarsByRangeEncodeDecode(t *testing.T) {
	req := &BlobSidecarsByRangeRequest{StartSlot: 200, Count: 32}
	data := req.Encode()
	if len(data) != 16 {
		t.Fatalf("expected 16 bytes, got %d", len(data))
	}
	decoded, err := DecodeBlobSidecarsByRange(data)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if decoded.StartSlot != 200 || decoded.Count != 32 {
		t.Fatalf("decoded mismatch: %+v", decoded)
	}
}

func TestReqResp_DataColumnsByRangeEncodeDecode(t *testing.T) {
	req := &DataColumnsByRangeRequest{
		StartSlot:   300,
		Count:       10,
		ColumnIndex: 5,
	}
	data := req.Encode()
	if len(data) != 24 {
		t.Fatalf("expected 24 bytes, got %d", len(data))
	}
	decoded, err := DecodeDataColumnsByRange(data)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if decoded.StartSlot != 300 || decoded.Count != 10 || decoded.ColumnIndex != 5 {
		t.Fatalf("decoded mismatch: %+v", decoded)
	}
}

func TestReqResp_SSZChunkEncodeDecode(t *testing.T) {
	chunk := SSZChunk{
		Code:    RespSuccess,
		Payload: []byte{0xaa, 0xbb, 0xcc},
	}
	data := EncodeSSZChunk(chunk)
	decoded, consumed, err := DecodeSSZChunk(data)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if consumed != len(data) {
		t.Fatalf("expected consumed %d, got %d", len(data), consumed)
	}
	if decoded.Code != RespSuccess {
		t.Fatalf("expected success code, got %d", decoded.Code)
	}
	if len(decoded.Payload) != 3 {
		t.Fatalf("expected 3 byte payload, got %d", len(decoded.Payload))
	}
}

func TestReqResp_SSZChunkWithError(t *testing.T) {
	chunk := SSZChunk{
		Code:  RespInvalidRequest,
		Error: "bad request",
	}
	data := EncodeSSZChunk(chunk)
	decoded, _, err := DecodeSSZChunk(data)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if decoded.Code != RespInvalidRequest {
		t.Fatalf("expected invalid request code")
	}
	if decoded.Error != "bad request" {
		t.Fatalf("expected error 'bad request', got %q", decoded.Error)
	}
}

func TestReqResp_SSZStreamDecode(t *testing.T) {
	chunk1 := SSZChunk{Code: RespSuccess, Payload: []byte{0x01}}
	chunk2 := SSZChunk{Code: RespSuccess, Payload: []byte{0x02, 0x03}}

	var combined []byte
	combined = append(combined, EncodeSSZChunk(chunk1)...)
	combined = append(combined, EncodeSSZChunk(chunk2)...)

	chunks, err := DecodeSSZStream(combined)
	if err != nil {
		t.Fatalf("decode stream: %v", err)
	}
	if len(chunks) != 2 {
		t.Fatalf("expected 2 chunks, got %d", len(chunks))
	}
	if len(chunks[0].Payload) != 1 || chunks[0].Payload[0] != 0x01 {
		t.Fatalf("chunk 0 payload mismatch")
	}
	if len(chunks[1].Payload) != 2 {
		t.Fatalf("chunk 1 payload mismatch")
	}
}

func TestReqResp_RetryBackoff(t *testing.T) {
	rc := DefaultRetryConfig()

	b0 := rc.backoffDuration(0)
	b1 := rc.backoffDuration(1)
	b2 := rc.backoffDuration(2)

	if b1 <= b0 {
		t.Fatalf("expected backoff to increase: b0=%v, b1=%v", b0, b1)
	}
	if b2 <= b1 {
		t.Fatalf("expected backoff to increase: b1=%v, b2=%v", b1, b2)
	}

	// Test max backoff cap.
	rc.MaxBackoff = 200 * time.Millisecond
	bHuge := rc.backoffDuration(100)
	if bHuge > rc.MaxBackoff {
		t.Fatalf("backoff exceeded max: %v > %v", bHuge, rc.MaxBackoff)
	}
}

func TestReqResp_ManagerRequestBlocksByRange(t *testing.T) {
	proto := NewReqRespProtocol(DefaultProtocolConfig())
	proto.SetStreamSendFunc(func(peer string, method MethodID, payload []byte) (*StreamedResponse, error) {
		return &StreamedResponse{
			Chunks: []ResponseChunk{
				{Code: RespSuccess, Payload: []byte{0x01}},
				{Code: RespSuccess, Payload: []byte{0x02}},
			},
		}, nil
	})

	retry := DefaultRetryConfig()
	retry.MaxRetries = 1
	mgr := NewReqRespManager(proto, retry)

	req := &BeaconBlocksByRangeRequest{StartSlot: 100, Count: 2, Step: 1}
	resp, err := mgr.RequestBlocksByRange("peer1", req)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	if len(resp.Chunks) != 2 {
		t.Fatalf("expected 2 chunks, got %d", len(resp.Chunks))
	}
}

func TestReqResp_ManagerRetryOnFailure(t *testing.T) {
	proto := NewReqRespProtocol(DefaultProtocolConfig())

	attempts := 0
	proto.SetStreamSendFunc(func(peer string, method MethodID, payload []byte) (*StreamedResponse, error) {
		attempts++
		if attempts < 3 {
			return nil, ErrProtocolTimeout
		}
		return &StreamedResponse{Chunks: []ResponseChunk{{Code: RespSuccess}}}, nil
	})

	retry := RetryConfig{
		MaxRetries:        3,
		InitialBackoff:    time.Millisecond,
		MaxBackoff:        10 * time.Millisecond,
		BackoffMultiplier: 1.5,
		RequestTimeout:    time.Second,
	}
	mgr := NewReqRespManager(proto, retry)

	req := &BeaconBlocksByRangeRequest{StartSlot: 0, Count: 1, Step: 1}
	resp, err := mgr.RequestBlocksByRange("peer1", req)
	if err != nil {
		t.Fatalf("expected success after retries, got: %v", err)
	}
	if len(resp.Chunks) != 1 {
		t.Fatalf("expected 1 chunk, got %d", len(resp.Chunks))
	}
	if attempts != 3 {
		t.Fatalf("expected 3 attempts, got %d", attempts)
	}
}

func TestReqResp_ManagerMaxRetriesExceeded(t *testing.T) {
	proto := NewReqRespProtocol(DefaultProtocolConfig())
	proto.SetStreamSendFunc(func(peer string, method MethodID, payload []byte) (*StreamedResponse, error) {
		return nil, ErrProtocolTimeout
	})

	retry := RetryConfig{
		MaxRetries:        2,
		InitialBackoff:    time.Millisecond,
		MaxBackoff:        5 * time.Millisecond,
		BackoffMultiplier: 1.0,
		RequestTimeout:    time.Second,
	}
	mgr := NewReqRespManager(proto, retry)

	req := &BlobSidecarsByRangeRequest{StartSlot: 0, Count: 1}
	_, err := mgr.RequestBlobSidecarsByRange("peer1", req)
	if !errors.Is(err, ErrReqRespMaxRetries) {
		t.Fatalf("expected ErrReqRespMaxRetries, got: %v", err)
	}
}

func TestReqResp_ManagerClosed(t *testing.T) {
	proto := NewReqRespProtocol(DefaultProtocolConfig())
	mgr := NewReqRespManager(proto, DefaultRetryConfig())
	mgr.Close()

	req := &BeaconBlocksByRangeRequest{StartSlot: 0, Count: 1, Step: 1}
	_, err := mgr.RequestBlocksByRange("peer1", req)
	if !errors.Is(err, ErrReqRespClosed) {
		t.Fatalf("expected ErrReqRespClosed, got: %v", err)
	}
}

func TestReqResp_SSZChunkDecodeTooShort(t *testing.T) {
	_, _, err := DecodeSSZChunk([]byte{0x00})
	if err == nil {
		t.Fatal("expected error for short chunk data")
	}
}

func TestReqResp_DataColumnsByRangeDecodeTooShort(t *testing.T) {
	_, err := DecodeDataColumnsByRange([]byte{0x01, 0x02})
	if err == nil {
		t.Fatal("expected error for short data")
	}
}
