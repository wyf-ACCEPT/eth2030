package snap

import (
	"testing"

	"github.com/eth2028/eth2028/core/types"
)

// --- Protocol identity ---

func TestSnapProto_ProtocolNameAndVersion(t *testing.T) {
	if ProtocolName != "snap" {
		t.Fatalf("ProtocolName: want 'snap', got %q", ProtocolName)
	}
	if ProtocolVersion != 1 {
		t.Fatalf("ProtocolVersion: want 1, got %d", ProtocolVersion)
	}
}

// --- Message code values ---

func TestSnapProto_MessageCodeValues(t *testing.T) {
	tests := []struct {
		name string
		code uint64
		want uint64
	}{
		{"GetAccountRangeMsg", GetAccountRangeMsg, 0x00},
		{"AccountRangeMsg", AccountRangeMsg, 0x01},
		{"GetStorageRangesMsg", GetStorageRangesMsg, 0x02},
		{"StorageRangesMsg", StorageRangesMsg, 0x03},
		{"GetByteCodesMsg", GetByteCodesMsg, 0x04},
		{"ByteCodesMsg", ByteCodesMsg, 0x05},
		{"GetTrieNodesMsg", GetTrieNodesMsg, 0x06},
		{"TrieNodesMsg", TrieNodesMsg, 0x07},
	}
	for _, tt := range tests {
		if tt.code != tt.want {
			t.Errorf("%s: want 0x%02x, got 0x%02x", tt.name, tt.want, tt.code)
		}
	}
}

func TestSnapProto_MessageCodesSequential(t *testing.T) {
	codes := []uint64{
		GetAccountRangeMsg, AccountRangeMsg,
		GetStorageRangesMsg, StorageRangesMsg,
		GetByteCodesMsg, ByteCodesMsg,
		GetTrieNodesMsg, TrieNodesMsg,
	}
	for i, c := range codes {
		if c != uint64(i) {
			t.Errorf("code[%d]: want %d, got %d", i, i, c)
		}
	}
}

func TestSnapProto_MessageCodesPaired(t *testing.T) {
	// Each Get message should have a response code = Get + 1.
	pairs := [][2]uint64{
		{GetAccountRangeMsg, AccountRangeMsg},
		{GetStorageRangesMsg, StorageRangesMsg},
		{GetByteCodesMsg, ByteCodesMsg},
		{GetTrieNodesMsg, TrieNodesMsg},
	}
	for _, p := range pairs {
		if p[1] != p[0]+1 {
			t.Errorf("response code for 0x%02x: want 0x%02x, got 0x%02x", p[0], p[0]+1, p[1])
		}
	}
}

// --- Response limits ---

func TestSnapProto_SoftResponseLimit(t *testing.T) {
	if SoftResponseLimit != 500*1024 {
		t.Fatalf("SoftResponseLimit: want %d, got %d", 500*1024, SoftResponseLimit)
	}
}

func TestSnapProto_HardResponseLimit(t *testing.T) {
	if HardResponseLimit != 2*1024*1024 {
		t.Fatalf("HardResponseLimit: want %d, got %d", 2*1024*1024, HardResponseLimit)
	}
}

func TestSnapProto_SoftLessThanHard(t *testing.T) {
	if SoftResponseLimit >= HardResponseLimit {
		t.Fatalf("SoftResponseLimit (%d) should be < HardResponseLimit (%d)",
			SoftResponseLimit, HardResponseLimit)
	}
}

// --- Max count limits ---

func TestSnapProto_MaxAccountRangeCount(t *testing.T) {
	if MaxAccountRangeCount != 256 {
		t.Fatalf("MaxAccountRangeCount: want 256, got %d", MaxAccountRangeCount)
	}
}

func TestSnapProto_MaxStorageRangeCount(t *testing.T) {
	if MaxStorageRangeCount != 512 {
		t.Fatalf("MaxStorageRangeCount: want 512, got %d", MaxStorageRangeCount)
	}
}

func TestSnapProto_MaxByteCodeCount(t *testing.T) {
	if MaxByteCodeCount != 64 {
		t.Fatalf("MaxByteCodeCount: want 64, got %d", MaxByteCodeCount)
	}
}

func TestSnapProto_MaxTrieNodeCount(t *testing.T) {
	if MaxTrieNodeCount != 512 {
		t.Fatalf("MaxTrieNodeCount: want 512, got %d", MaxTrieNodeCount)
	}
}

// --- Packet struct field tests ---

func TestSnapProto_GetAccountRangePacketFields(t *testing.T) {
	root := types.Hash{0x01}
	origin := types.Hash{0x10}
	limit := types.Hash{0xff}

	pkt := GetAccountRangePacket{
		ID:     42,
		Root:   root,
		Origin: origin,
		Limit:  limit,
		Bytes:  SoftResponseLimit,
	}
	if pkt.ID != 42 {
		t.Fatalf("ID: want 42, got %d", pkt.ID)
	}
	if pkt.Root != root {
		t.Fatal("Root mismatch")
	}
	if pkt.Origin != origin {
		t.Fatal("Origin mismatch")
	}
	if pkt.Limit != limit {
		t.Fatal("Limit mismatch")
	}
	if pkt.Bytes != SoftResponseLimit {
		t.Fatalf("Bytes: want %d, got %d", SoftResponseLimit, pkt.Bytes)
	}
}

func TestSnapProto_AccountRangePacketFields(t *testing.T) {
	accounts := []AccountData{
		{Hash: types.Hash{0xaa}, Body: []byte("account1")},
		{Hash: types.Hash{0xbb}, Body: []byte("account2")},
	}
	proof := [][]byte{{0x01, 0x02}, {0x03, 0x04}}

	pkt := AccountRangePacket{
		ID:       7,
		Accounts: accounts,
		Proof:    proof,
	}
	if pkt.ID != 7 {
		t.Fatalf("ID: want 7, got %d", pkt.ID)
	}
	if len(pkt.Accounts) != 2 {
		t.Fatalf("Accounts: want 2, got %d", len(pkt.Accounts))
	}
	if len(pkt.Proof) != 2 {
		t.Fatalf("Proof: want 2, got %d", len(pkt.Proof))
	}
}

func TestSnapProto_AccountDataFields(t *testing.T) {
	ad := AccountData{
		Hash: types.Hash{0xde, 0xad},
		Body: []byte("slim account body"),
	}
	if ad.Hash[0] != 0xde || ad.Hash[1] != 0xad {
		t.Fatal("Hash mismatch")
	}
	if string(ad.Body) != "slim account body" {
		t.Fatalf("Body: want 'slim account body', got %q", string(ad.Body))
	}
}

func TestSnapProto_GetStorageRangesPacketFields(t *testing.T) {
	accts := []types.Hash{{0x01}, {0x02}}
	pkt := GetStorageRangesPacket{
		ID:       99,
		Root:     types.Hash{0xff},
		Accounts: accts,
		Origin:   []byte{0x00},
		Limit:    []byte{0xff},
		Bytes:    HardResponseLimit,
	}
	if pkt.ID != 99 {
		t.Fatalf("ID: want 99, got %d", pkt.ID)
	}
	if len(pkt.Accounts) != 2 {
		t.Fatalf("Accounts: want 2, got %d", len(pkt.Accounts))
	}
	if pkt.Bytes != HardResponseLimit {
		t.Fatalf("Bytes: want %d, got %d", HardResponseLimit, pkt.Bytes)
	}
}

func TestSnapProto_StorageRangesPacketFields(t *testing.T) {
	slots := [][]StorageData{
		{
			{Hash: types.Hash{0x01}, Body: []byte("val1")},
			{Hash: types.Hash{0x02}, Body: []byte("val2")},
		},
	}
	proof := [][]byte{{0xaa}}

	pkt := StorageRangesPacket{
		ID:    5,
		Slots: slots,
		Proof: proof,
	}
	if pkt.ID != 5 {
		t.Fatalf("ID: want 5, got %d", pkt.ID)
	}
	if len(pkt.Slots) != 1 {
		t.Fatalf("Slots: want 1 account, got %d", len(pkt.Slots))
	}
	if len(pkt.Slots[0]) != 2 {
		t.Fatalf("Slots[0]: want 2 entries, got %d", len(pkt.Slots[0]))
	}
}

func TestSnapProto_StorageDataFields(t *testing.T) {
	sd := StorageData{
		Hash: types.Hash{0xab},
		Body: []byte("storage value"),
	}
	if sd.Hash[0] != 0xab {
		t.Fatal("Hash mismatch")
	}
	if string(sd.Body) != "storage value" {
		t.Fatalf("Body: want 'storage value', got %q", string(sd.Body))
	}
}

func TestSnapProto_GetByteCodesPacketFields(t *testing.T) {
	hashes := []types.Hash{{0x01}, {0x02}, {0x03}}
	pkt := GetByteCodesPacket{
		ID:     11,
		Hashes: hashes,
		Bytes:  SoftResponseLimit,
	}
	if pkt.ID != 11 {
		t.Fatalf("ID: want 11, got %d", pkt.ID)
	}
	if len(pkt.Hashes) != 3 {
		t.Fatalf("Hashes: want 3, got %d", len(pkt.Hashes))
	}
}

func TestSnapProto_ByteCodesPacketFields(t *testing.T) {
	codes := [][]byte{
		{0x60, 0x80, 0x60, 0x40},
		{0x60, 0x00},
	}
	pkt := ByteCodesPacket{
		ID:    11,
		Codes: codes,
	}
	if pkt.ID != 11 {
		t.Fatalf("ID: want 11, got %d", pkt.ID)
	}
	if len(pkt.Codes) != 2 {
		t.Fatalf("Codes: want 2, got %d", len(pkt.Codes))
	}
}

func TestSnapProto_GetTrieNodesPacketFields(t *testing.T) {
	paths := []TrieNodePathSet{
		{[]byte{0x01}, []byte{0x02}},
		{[]byte{0x03}},
	}
	pkt := GetTrieNodesPacket{
		ID:    22,
		Root:  types.Hash{0xcc},
		Paths: paths,
		Bytes: HardResponseLimit,
	}
	if pkt.ID != 22 {
		t.Fatalf("ID: want 22, got %d", pkt.ID)
	}
	if len(pkt.Paths) != 2 {
		t.Fatalf("Paths: want 2, got %d", len(pkt.Paths))
	}
	if len(pkt.Paths[0]) != 2 {
		t.Fatalf("Paths[0]: want 2 components, got %d", len(pkt.Paths[0]))
	}
}

func TestSnapProto_TrieNodesPacketFields(t *testing.T) {
	nodes := [][]byte{
		{0xf8, 0x51},
		{0xe2, 0x17},
	}
	pkt := TrieNodesPacket{
		ID:    22,
		Nodes: nodes,
	}
	if pkt.ID != 22 {
		t.Fatalf("ID: want 22, got %d", pkt.ID)
	}
	if len(pkt.Nodes) != 2 {
		t.Fatalf("Nodes: want 2, got %d", len(pkt.Nodes))
	}
}

func TestSnapProto_TrieNodePathSetType(t *testing.T) {
	// TrieNodePathSet is [][]byte. Verify multi-component paths work.
	ps := TrieNodePathSet{
		[]byte{},         // empty = account trie
		[]byte{0x01},     // first storage key component
		[]byte{0x02, 03}, // second component
	}
	if len(ps) != 3 {
		t.Fatalf("path set len: want 3, got %d", len(ps))
	}
	if len(ps[0]) != 0 {
		t.Fatal("first element should be empty for account trie")
	}
}

// --- Empty / nil packet edge cases ---

func TestSnapProto_AccountRangePacketEmpty(t *testing.T) {
	pkt := AccountRangePacket{
		ID:       1,
		Accounts: nil,
		Proof:    nil,
	}
	if pkt.Accounts != nil {
		t.Fatal("nil accounts should remain nil")
	}
	if pkt.Proof != nil {
		t.Fatal("nil proof should remain nil")
	}
}

func TestSnapProto_StorageRangesPacketEmpty(t *testing.T) {
	pkt := StorageRangesPacket{
		ID:    2,
		Slots: nil,
		Proof: nil,
	}
	if pkt.Slots != nil {
		t.Fatal("nil slots should remain nil")
	}
}

func TestSnapProto_ByteCodesPacketEmpty(t *testing.T) {
	pkt := ByteCodesPacket{
		ID:    3,
		Codes: nil,
	}
	if pkt.Codes != nil {
		t.Fatal("nil codes should remain nil")
	}
}

func TestSnapProto_TrieNodesPacketEmpty(t *testing.T) {
	pkt := TrieNodesPacket{
		ID:    4,
		Nodes: nil,
	}
	if pkt.Nodes != nil {
		t.Fatal("nil nodes should remain nil")
	}
}

// --- Request ID echo convention ---

func TestSnapProto_RequestIDEchoed(t *testing.T) {
	// Verify that response packets can echo the same request ID.
	reqID := uint64(0xDEADBEEF)

	acctReq := GetAccountRangePacket{ID: reqID}
	acctResp := AccountRangePacket{ID: acctReq.ID}
	if acctResp.ID != reqID {
		t.Fatalf("account range ID: want %d, got %d", reqID, acctResp.ID)
	}

	codeReq := GetByteCodesPacket{ID: reqID}
	codeResp := ByteCodesPacket{ID: codeReq.ID}
	if codeResp.ID != reqID {
		t.Fatalf("bytecodes ID: want %d, got %d", reqID, codeResp.ID)
	}

	trieReq := GetTrieNodesPacket{ID: reqID}
	trieResp := TrieNodesPacket{ID: trieReq.ID}
	if trieResp.ID != reqID {
		t.Fatalf("trie nodes ID: want %d, got %d", reqID, trieResp.ID)
	}

	storReq := GetStorageRangesPacket{ID: reqID}
	storResp := StorageRangesPacket{ID: storReq.ID}
	if storResp.ID != reqID {
		t.Fatalf("storage ranges ID: want %d, got %d", reqID, storResp.ID)
	}
}

// --- Zero-value packet safety ---

func TestSnapProto_ZeroValuePackets(t *testing.T) {
	// Zero-value structs should not panic.
	var acctReq GetAccountRangePacket
	if acctReq.ID != 0 {
		t.Fatal("zero-value ID should be 0")
	}
	if acctReq.Bytes != 0 {
		t.Fatal("zero-value Bytes should be 0")
	}

	var storReq GetStorageRangesPacket
	if storReq.Accounts != nil {
		t.Fatal("zero-value Accounts should be nil")
	}

	var codeReq GetByteCodesPacket
	if codeReq.Hashes != nil {
		t.Fatal("zero-value Hashes should be nil")
	}

	var trieReq GetTrieNodesPacket
	if trieReq.Paths != nil {
		t.Fatal("zero-value Paths should be nil")
	}
}
