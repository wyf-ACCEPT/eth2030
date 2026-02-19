package rpc

import (
	"encoding/json"
	"math/big"
	"testing"

	"github.com/eth2028/eth2028/core/state"
	"github.com/eth2028/eth2028/core/types"
	"github.com/eth2028/eth2028/core/vm"
	"github.com/eth2028/eth2028/trie"
)

// mockBeaconBackend implements the parts of Backend needed for BeaconAPI tests.
type mockBeaconBackend struct {
	headers map[int64]*types.Header
	current *types.Header
}

func newMockBeaconBackend() *mockBeaconBackend {
	h0 := &types.Header{
		Number:     big.NewInt(0),
		ParentHash: types.Hash{},
		Root:       types.HexToHash("0xaaaa"),
		TxHash:     types.HexToHash("0xbbbb"),
		Time:       1606824023,
		GasLimit:   30000000,
		GasUsed:    21000,
	}
	h1 := &types.Header{
		Number:     big.NewInt(1),
		ParentHash: h0.Hash(),
		Root:       types.HexToHash("0xcccc"),
		TxHash:     types.HexToHash("0xdddd"),
		Time:       1606824035,
		GasLimit:   30000000,
		GasUsed:    42000,
	}
	return &mockBeaconBackend{
		headers: map[int64]*types.Header{0: h0, 1: h1},
		current: h1,
	}
}

func (m *mockBeaconBackend) HeaderByNumber(number BlockNumber) *types.Header {
	if number == LatestBlockNumber {
		return m.current
	}
	return m.headers[int64(number)]
}

func (m *mockBeaconBackend) HeaderByHash(hash types.Hash) *types.Header {
	for _, h := range m.headers {
		if h.Hash() == hash {
			return h
		}
	}
	return nil
}

func (m *mockBeaconBackend) CurrentHeader() *types.Header { return m.current }
func (m *mockBeaconBackend) ChainID() *big.Int            { return big.NewInt(1) }

// Unused Backend methods for beacon tests.
func (m *mockBeaconBackend) BlockByNumber(BlockNumber) *types.Block                { return nil }
func (m *mockBeaconBackend) BlockByHash(types.Hash) *types.Block                   { return nil }
func (m *mockBeaconBackend) StateAt(types.Hash) (state.StateDB, error)             { return nil, nil }
func (m *mockBeaconBackend) SendTransaction(*types.Transaction) error              { return nil }
func (m *mockBeaconBackend) GetTransaction(types.Hash) (*types.Transaction, uint64, uint64) {
	return nil, 0, 0
}
func (m *mockBeaconBackend) SuggestGasPrice() *big.Int                { return big.NewInt(0) }
func (m *mockBeaconBackend) GetReceipts(types.Hash) []*types.Receipt  { return nil }
func (m *mockBeaconBackend) GetLogs(types.Hash) []*types.Log          { return nil }
func (m *mockBeaconBackend) GetBlockReceipts(uint64) []*types.Receipt { return nil }
func (m *mockBeaconBackend) GetProof(types.Address, []types.Hash, BlockNumber) (*trie.AccountProof, error) {
	return nil, nil
}
func (m *mockBeaconBackend) EVMCall(types.Address, *types.Address, []byte, uint64, *big.Int, BlockNumber) ([]byte, uint64, error) {
	return nil, 0, nil
}
func (m *mockBeaconBackend) TraceTransaction(types.Hash) (*vm.StructLogTracer, error) {
	return nil, nil
}
func (m *mockBeaconBackend) HistoryOldestBlock() uint64 { return 0 }

func makeBeaconAPI(t *testing.T) *BeaconAPI {
	t.Helper()
	backend := newMockBeaconBackend()
	state := NewConsensusState()
	state.FinalizedEpoch = 2
	state.FinalizedRoot = types.HexToHash("0x1111")
	state.JustifiedEpoch = 3
	state.JustifiedRoot = types.HexToHash("0x2222")
	state.Peers = []*BeaconPeer{
		{PeerID: "peer1", State: "connected", Direction: "inbound", Address: "10.0.0.1:9000"},
	}
	state.Validators = []*ValidatorEntry{
		{
			Index:   "0",
			Balance: "32000000000",
			Status:  "active_ongoing",
			Validator: &ValidatorData{
				Pubkey:                "0xaabb",
				WithdrawalCredentials: "0x00",
				EffectiveBalance:      "32000000000",
				Slashed:               false,
				ActivationEpoch:       "0",
				ExitEpoch:             "18446744073709551615",
			},
		},
	}
	return NewBeaconAPI(state, backend)
}

func beaconReq(method string, params ...interface{}) *Request {
	rawParams := make([]json.RawMessage, len(params))
	for i, p := range params {
		b, _ := json.Marshal(p)
		rawParams[i] = b
	}
	return &Request{
		JSONRPC: "2.0",
		Method:  method,
		Params:  rawParams,
		ID:      json.RawMessage(`1`),
	}
}

func TestBeaconGetGenesis(t *testing.T) {
	api := makeBeaconAPI(t)
	resp := api.getGenesis(beaconReq("beacon_getGenesis"))

	if resp.Error != nil {
		t.Fatalf("unexpected error: %v", resp.Error.Message)
	}

	data, _ := json.Marshal(resp.Result)
	var genesis GenesisResponse
	json.Unmarshal(data, &genesis)

	if genesis.GenesisTime != "1606824023" {
		t.Errorf("genesis time = %q, want 1606824023", genesis.GenesisTime)
	}
	if genesis.GenesisForkVersion != "0x00000000" {
		t.Errorf("fork version = %q, want 0x00000000", genesis.GenesisForkVersion)
	}
}

func TestBeaconGetBlock(t *testing.T) {
	api := makeBeaconAPI(t)

	// Slot 0 should succeed.
	resp := api.getBlock(beaconReq("beacon_getBlock", "0x0"))
	if resp.Error != nil {
		t.Fatalf("unexpected error: %v", resp.Error.Message)
	}

	data, _ := json.Marshal(resp.Result)
	var block BlockResponse
	json.Unmarshal(data, &block)

	if block.Slot != "0" {
		t.Errorf("slot = %q, want 0", block.Slot)
	}

	// Non-existent slot should return 404.
	resp = api.getBlock(beaconReq("beacon_getBlock", "0x999"))
	if resp.Error == nil {
		t.Fatal("expected error for non-existent slot")
	}
	if resp.Error.Code != BeaconErrNotFound {
		t.Errorf("error code = %d, want %d", resp.Error.Code, BeaconErrNotFound)
	}
}

func TestBeaconGetBlockHeader(t *testing.T) {
	api := makeBeaconAPI(t)
	resp := api.getBlockHeader(beaconReq("beacon_getBlockHeader", "0x1"))
	if resp.Error != nil {
		t.Fatalf("unexpected error: %v", resp.Error.Message)
	}

	data, _ := json.Marshal(resp.Result)
	var header HeaderResponse
	json.Unmarshal(data, &header)

	if !header.Canonical {
		t.Error("expected canonical = true")
	}
	if header.Header == nil || header.Header.Message == nil {
		t.Fatal("header message is nil")
	}
	if header.Header.Message.Slot != "1" {
		t.Errorf("slot = %q, want 1", header.Header.Message.Slot)
	}
}

func TestBeaconGetStateRoot(t *testing.T) {
	api := makeBeaconAPI(t)

	// Test "head" state ID.
	resp := api.getStateRoot(beaconReq("beacon_getStateRoot", "head"))
	if resp.Error != nil {
		t.Fatalf("unexpected error: %v", resp.Error.Message)
	}

	data, _ := json.Marshal(resp.Result)
	var sr StateRootResponse
	json.Unmarshal(data, &sr)
	if sr.Root == "" {
		t.Error("state root is empty")
	}
}

func TestBeaconGetStateFinalityCheckpoints(t *testing.T) {
	api := makeBeaconAPI(t)
	resp := api.getStateFinalityCheckpoints(beaconReq("beacon_getStateFinalityCheckpoints", "head"))
	if resp.Error != nil {
		t.Fatalf("unexpected error: %v", resp.Error.Message)
	}

	data, _ := json.Marshal(resp.Result)
	var cp FinalityCheckpointsResponse
	json.Unmarshal(data, &cp)

	if cp.Finalized == nil {
		t.Fatal("finalized checkpoint is nil")
	}
	if cp.Finalized.Epoch != "2" {
		t.Errorf("finalized epoch = %q, want 2", cp.Finalized.Epoch)
	}
	if cp.CurrentJustified == nil {
		t.Fatal("current justified checkpoint is nil")
	}
	if cp.CurrentJustified.Epoch != "3" {
		t.Errorf("justified epoch = %q, want 3", cp.CurrentJustified.Epoch)
	}
}

func TestBeaconGetStateValidators(t *testing.T) {
	api := makeBeaconAPI(t)
	resp := api.getStateValidators(beaconReq("beacon_getStateValidators", "head"))
	if resp.Error != nil {
		t.Fatalf("unexpected error: %v", resp.Error.Message)
	}

	data, _ := json.Marshal(resp.Result)
	var vl ValidatorListResponse
	json.Unmarshal(data, &vl)

	if len(vl.Validators) != 1 {
		t.Fatalf("validators count = %d, want 1", len(vl.Validators))
	}
	if vl.Validators[0].Status != "active_ongoing" {
		t.Errorf("validator status = %q, want active_ongoing", vl.Validators[0].Status)
	}
}

func TestBeaconGetNodeVersion(t *testing.T) {
	api := makeBeaconAPI(t)
	resp := api.getNodeVersion(beaconReq("beacon_getNodeVersion"))
	if resp.Error != nil {
		t.Fatalf("unexpected error: %v", resp.Error.Message)
	}

	data, _ := json.Marshal(resp.Result)
	var ver VersionResponse
	json.Unmarshal(data, &ver)

	if ver.Version != "eth2028/v0.1.0-beacon" {
		t.Errorf("version = %q, want eth2028/v0.1.0-beacon", ver.Version)
	}
}

func TestBeaconGetNodeSyncing(t *testing.T) {
	api := makeBeaconAPI(t)
	resp := api.getNodeSyncing(beaconReq("beacon_getNodeSyncing"))
	if resp.Error != nil {
		t.Fatalf("unexpected error: %v", resp.Error.Message)
	}

	data, _ := json.Marshal(resp.Result)
	var sync SyncingResponse
	json.Unmarshal(data, &sync)

	if sync.HeadSlot != "1" {
		t.Errorf("head slot = %q, want 1", sync.HeadSlot)
	}
	if sync.IsSyncing {
		t.Error("expected is_syncing = false")
	}
}

func TestBeaconGetNodePeers(t *testing.T) {
	api := makeBeaconAPI(t)
	resp := api.getNodePeers(beaconReq("beacon_getNodePeers"))
	if resp.Error != nil {
		t.Fatalf("unexpected error: %v", resp.Error.Message)
	}

	data, _ := json.Marshal(resp.Result)
	var peers PeerListResponse
	json.Unmarshal(data, &peers)

	if len(peers.Peers) != 1 {
		t.Fatalf("peer count = %d, want 1", len(peers.Peers))
	}
	if peers.Peers[0].PeerID != "peer1" {
		t.Errorf("peer ID = %q, want peer1", peers.Peers[0].PeerID)
	}
}

func TestBeaconGetNodeHealth(t *testing.T) {
	api := makeBeaconAPI(t)
	resp := api.getNodeHealth(beaconReq("beacon_getNodeHealth"))
	if resp.Error != nil {
		t.Fatalf("unexpected error: %v", resp.Error.Message)
	}

	data, _ := json.Marshal(resp.Result)
	var health map[string]string
	json.Unmarshal(data, &health)

	if health["status"] != "healthy" {
		t.Errorf("status = %q, want healthy", health["status"])
	}

	// Test syncing state.
	api.state.IsSyncing = true
	resp = api.getNodeHealth(beaconReq("beacon_getNodeHealth"))
	data, _ = json.Marshal(resp.Result)
	json.Unmarshal(data, &health)
	if health["status"] != "syncing" {
		t.Errorf("status = %q, want syncing", health["status"])
	}
}

func TestRegisterBeaconRoutes(t *testing.T) {
	api := makeBeaconAPI(t)
	routes := RegisterBeaconRoutes(api)

	expectedMethods := []string{
		"beacon_getGenesis",
		"beacon_getBlock",
		"beacon_getBlockHeader",
		"beacon_getStateRoot",
		"beacon_getStateFinalityCheckpoints",
		"beacon_getStateValidators",
		"beacon_getNodeVersion",
		"beacon_getNodeSyncing",
		"beacon_getNodePeers",
		"beacon_getNodeHealth",
	}

	for _, method := range expectedMethods {
		if _, ok := routes[method]; !ok {
			t.Errorf("missing route: %s", method)
		}
	}
}

func TestBeaconGetBlockMissingParams(t *testing.T) {
	api := makeBeaconAPI(t)
	resp := api.getBlock(&Request{
		JSONRPC: "2.0",
		Method:  "beacon_getBlock",
		Params:  nil,
		ID:      json.RawMessage(`1`),
	})
	if resp.Error == nil {
		t.Fatal("expected error for missing params")
	}
	if resp.Error.Code != BeaconErrBadRequest {
		t.Errorf("error code = %d, want %d", resp.Error.Code, BeaconErrBadRequest)
	}
}
