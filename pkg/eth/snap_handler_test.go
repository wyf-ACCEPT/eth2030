package eth

import (
	"errors"
	"testing"
	"time"

	"github.com/eth2030/eth2030/core/types"
)

// mockStateProvider implements StateProvider for testing.
type mockStateProvider struct {
	accounts []SnapAccountEntry
	slots    []SnapStorageEntry
	codes    []SnapByteCodeEntry
	nodes    []SnapTrieNodeEntry
}

func (m *mockStateProvider) GetAccountRange(root types.Hash, start, limit types.Hash, maxResults int) ([]SnapAccountEntry, bool) {
	count := len(m.accounts)
	if count > maxResults {
		count = maxResults
		return m.accounts[:count], true
	}
	return m.accounts, false
}

func (m *mockStateProvider) GetStorageRanges(root types.Hash, accounts []types.Address, start, limit types.Hash, maxResults int) ([]SnapStorageEntry, bool) {
	count := len(m.slots)
	if count > maxResults {
		count = maxResults
		return m.slots[:count], true
	}
	return m.slots, false
}

func (m *mockStateProvider) GetByteCodes(hashes []types.Hash) []SnapByteCodeEntry {
	var result []SnapByteCodeEntry
	for _, h := range hashes {
		for _, c := range m.codes {
			if c.Hash == h {
				result = append(result, c)
				break
			}
		}
	}
	return result
}

func (m *mockStateProvider) GetTrieNodes(root types.Hash, paths [][]byte) []SnapTrieNodeEntry {
	count := len(paths)
	if count > len(m.nodes) {
		count = len(m.nodes)
	}
	return m.nodes[:count]
}

func newMockStateProvider() *mockStateProvider {
	return &mockStateProvider{
		accounts: []SnapAccountEntry{
			{
				Hash:     types.HexToHash("0x01"),
				Address:  types.HexToAddress("0xaaaa"),
				Nonce:    5,
				Balance:  []byte{0x01},
				Root:     types.HexToHash("0x56e81f171bcc55a6ff8345e692c0f86e5b48e01b996cadc001622fb5e363b421"),
				CodeHash: types.HexToHash("0xc5d2460186f7233c927e7db2dcc703c0e500b653ca82273b7bfad8045d85a470"),
			},
			{
				Hash:    types.HexToHash("0x02"),
				Address: types.HexToAddress("0xbbbb"),
				Nonce:   10,
				Balance: []byte{0x02},
			},
		},
		slots: []SnapStorageEntry{
			{Hash: types.HexToHash("0x10"), Key: types.HexToHash("0x00"), Value: types.HexToHash("0xff")},
		},
		codes: []SnapByteCodeEntry{
			{Hash: types.HexToHash("0xc0de"), Code: []byte{0x60, 0x00, 0x60, 0x00}},
		},
		nodes: []SnapTrieNodeEntry{
			{Path: []byte{0x01}, Data: []byte{0xab, 0xcd}},
			{Path: []byte{0x02}, Data: []byte{0xef, 0x01}},
		},
	}
}

// TestSnapHandler_HandleGetAccountRange tests account range retrieval.
func TestSnapHandler_HandleGetAccountRange(t *testing.T) {
	sp := newMockStateProvider()
	h := NewSnapProtocolHandler(sp, nil)

	result, err := h.HandleGetAccountRange("peer-1", types.Hash{}, types.Hash{}, types.Hash{}, 100)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result.Accounts) != 2 {
		t.Fatalf("want 2 accounts, got %d", len(result.Accounts))
	}
	if result.More {
		t.Fatal("expected no more accounts")
	}
}

// TestSnapHandler_HandleGetAccountRange_Limited tests account range with limit.
func TestSnapHandler_HandleGetAccountRange_Limited(t *testing.T) {
	sp := newMockStateProvider()
	h := NewSnapProtocolHandler(sp, nil)

	result, err := h.HandleGetAccountRange("peer-1", types.Hash{}, types.Hash{}, types.Hash{}, 1)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result.Accounts) != 1 {
		t.Fatalf("want 1 account, got %d", len(result.Accounts))
	}
	if !result.More {
		t.Fatal("expected more accounts")
	}
}

// TestSnapHandler_HandleGetStorageRanges tests storage range retrieval.
func TestSnapHandler_HandleGetStorageRanges(t *testing.T) {
	sp := newMockStateProvider()
	h := NewSnapProtocolHandler(sp, nil)

	accounts := []types.Address{types.HexToAddress("0xaaaa")}
	result, err := h.HandleGetStorageRanges("peer-1", types.Hash{}, accounts, types.Hash{}, types.Hash{}, 100)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result.Slots) != 1 {
		t.Fatalf("want 1 slot, got %d", len(result.Slots))
	}
}

// TestSnapHandler_HandleGetByteCodes tests bytecode retrieval.
func TestSnapHandler_HandleGetByteCodes(t *testing.T) {
	sp := newMockStateProvider()
	h := NewSnapProtocolHandler(sp, nil)

	hashes := []types.Hash{types.HexToHash("0xc0de"), types.HexToHash("0xmissing")}
	result, err := h.HandleGetByteCodes("peer-1", hashes)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result.Codes) != 1 {
		t.Fatalf("want 1 code, got %d", len(result.Codes))
	}
	if len(result.Missing) != 1 {
		t.Fatalf("want 1 missing, got %d", len(result.Missing))
	}
}

// TestSnapHandler_HandleGetTrieNodes tests trie node retrieval.
func TestSnapHandler_HandleGetTrieNodes(t *testing.T) {
	sp := newMockStateProvider()
	h := NewSnapProtocolHandler(sp, nil)

	paths := [][]byte{{0x01}, {0x02}, {0x03}}
	result, err := h.HandleGetTrieNodes("peer-1", types.Hash{}, paths)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result.Nodes) != 2 {
		t.Fatalf("want 2 nodes, got %d", len(result.Nodes))
	}
	if result.Missing != 1 {
		t.Fatalf("want 1 missing, got %d", result.Missing)
	}
}

// TestSnapHandler_Throttling tests per-peer request throttling.
func TestSnapHandler_Throttling(t *testing.T) {
	sp := newMockStateProvider()
	throttler := NewRequestThrottler(2, time.Second)
	h := NewSnapProtocolHandler(sp, throttler)

	// First two requests should succeed.
	_, err := h.HandleGetAccountRange("peer-1", types.Hash{}, types.Hash{}, types.Hash{}, 10)
	if err != nil {
		t.Fatalf("first request should succeed: %v", err)
	}
	_, err = h.HandleGetAccountRange("peer-1", types.Hash{}, types.Hash{}, types.Hash{}, 10)
	if err != nil {
		t.Fatalf("second request should succeed: %v", err)
	}

	// Third should be throttled.
	_, err = h.HandleGetAccountRange("peer-1", types.Hash{}, types.Hash{}, types.Hash{}, 10)
	if !errors.Is(err, ErrSnapRequestThrottled) {
		t.Fatalf("want ErrSnapRequestThrottled, got %v", err)
	}

	// Different peer should still work.
	_, err = h.HandleGetAccountRange("peer-2", types.Hash{}, types.Hash{}, types.Hash{}, 10)
	if err != nil {
		t.Fatalf("different peer should succeed: %v", err)
	}
}

// TestSnapHandler_Stopped tests requests after stopping.
func TestSnapHandler_Stopped(t *testing.T) {
	sp := newMockStateProvider()
	h := NewSnapProtocolHandler(sp, nil)
	h.Stop()

	_, err := h.HandleGetAccountRange("peer-1", types.Hash{}, types.Hash{}, types.Hash{}, 10)
	if !errors.Is(err, ErrSnapHandlerStopped) {
		t.Fatalf("want ErrSnapHandlerStopped, got %v", err)
	}

	_, err = h.HandleGetStorageRanges("peer-1", types.Hash{}, nil, types.Hash{}, types.Hash{}, 10)
	if !errors.Is(err, ErrSnapHandlerStopped) {
		t.Fatalf("want ErrSnapHandlerStopped for storage, got %v", err)
	}

	_, err = h.HandleGetByteCodes("peer-1", nil)
	if !errors.Is(err, ErrSnapHandlerStopped) {
		t.Fatalf("want ErrSnapHandlerStopped for bytecodes, got %v", err)
	}

	_, err = h.HandleGetTrieNodes("peer-1", types.Hash{}, nil)
	if !errors.Is(err, ErrSnapHandlerStopped) {
		t.Fatalf("want ErrSnapHandlerStopped for trie nodes, got %v", err)
	}
}

// TestResponseSizer tests response size tracking.
func TestResponseSizer_Basic(t *testing.T) {
	sizer := NewResponseSizer(100)

	if sizer.CurrentSize() != 0 {
		t.Fatalf("want initial size 0, got %d", sizer.CurrentSize())
	}
	if sizer.Remaining() != 100 {
		t.Fatalf("want remaining 100, got %d", sizer.Remaining())
	}

	if !sizer.Add(50) {
		t.Fatal("50 bytes should fit")
	}
	if sizer.CurrentSize() != 50 {
		t.Fatalf("want size 50, got %d", sizer.CurrentSize())
	}
	if sizer.Remaining() != 50 {
		t.Fatalf("want remaining 50, got %d", sizer.Remaining())
	}

	if !sizer.Add(50) {
		t.Fatal("another 50 bytes should fit")
	}
	if sizer.Add(1) {
		t.Fatal("1 more byte should not fit")
	}

	sizer.Reset()
	if sizer.CurrentSize() != 0 {
		t.Fatalf("want 0 after reset, got %d", sizer.CurrentSize())
	}
}

// TestRequestThrottler tests per-peer throttling.
func TestRequestThrottler_Basic(t *testing.T) {
	rt := NewRequestThrottler(3, time.Second)

	if !rt.Allow("peer-1") {
		t.Fatal("first request should be allowed")
	}
	if !rt.Allow("peer-1") {
		t.Fatal("second request should be allowed")
	}
	if !rt.Allow("peer-1") {
		t.Fatal("third request should be allowed")
	}
	if rt.Allow("peer-1") {
		t.Fatal("fourth request should be denied")
	}

	if rt.PeerRequestCount("peer-1") != 4 {
		t.Fatalf("want 4 requests counted, got %d", rt.PeerRequestCount("peer-1"))
	}
}

// TestRequestThrottler_RemovePeer tests peer removal.
func TestRequestThrottler_RemovePeer(t *testing.T) {
	rt := NewRequestThrottler(3, time.Second)

	rt.Allow("peer-1")
	rt.Allow("peer-1")
	rt.RemovePeer("peer-1")

	if rt.PeerRequestCount("peer-1") != 0 {
		t.Fatalf("want 0 after removal, got %d", rt.PeerRequestCount("peer-1"))
	}

	// Should be able to make requests again.
	if !rt.Allow("peer-1") {
		t.Fatal("request should be allowed after removal")
	}
}

// TestSnapHandler_NilStateProvider tests handler with nil state provider.
func TestSnapHandler_NilStateProvider(t *testing.T) {
	h := NewSnapProtocolHandler(nil, nil)

	result, err := h.HandleGetAccountRange("peer-1", types.Hash{}, types.Hash{}, types.Hash{}, 10)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result.Accounts) != 0 {
		t.Fatalf("want 0 accounts with nil provider, got %d", len(result.Accounts))
	}

	codes, err := h.HandleGetByteCodes("peer-1", []types.Hash{types.HexToHash("0x01")})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(codes.Missing) != 1 {
		t.Fatalf("want 1 missing with nil provider, got %d", len(codes.Missing))
	}
}

// TestSnapHandler_HandleGetByteCodes_Truncation tests bytecode request truncation.
func TestSnapHandler_HandleGetByteCodes_Truncation(t *testing.T) {
	sp := newMockStateProvider()
	h := NewSnapProtocolHandler(sp, nil)

	// Create more hashes than the limit.
	hashes := make([]types.Hash, MaxByteCodesResponse+10)
	for i := range hashes {
		hashes[i] = types.HexToHash("0x01")
	}

	result, err := h.HandleGetByteCodes("peer-1", hashes)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Should not crash, just truncate the request.
	_ = result
}
