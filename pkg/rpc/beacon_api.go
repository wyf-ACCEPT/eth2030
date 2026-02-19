// Package rpc - Beacon API compatibility layer.
// Implements a subset of the Ethereum Beacon API as JSON-RPC methods,
// allowing consensus-layer clients to interact via the standard RPC server.
package rpc

import (
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/eth2028/eth2028/core/types"
)

// Beacon API error codes per the Beacon API spec.
const (
	BeaconErrNotFound       = 404
	BeaconErrBadRequest     = 400
	BeaconErrInternal       = 500
	BeaconErrNotImplemented = 501
)

// BeaconError represents a Beacon API error response.
type BeaconError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

func (e *BeaconError) Error() string {
	return fmt.Sprintf("beacon error %d: %s", e.Code, e.Message)
}

// --- Response types ---

// GenesisResponse is the response for beacon_getGenesis.
type GenesisResponse struct {
	GenesisTime           string `json:"genesis_time"`
	GenesisValidatorsRoot string `json:"genesis_validators_root"`
	GenesisForkVersion    string `json:"genesis_fork_version"`
}

// BlockResponse is the response for beacon_getBlock.
type BlockResponse struct {
	Slot          string `json:"slot"`
	ProposerIndex string `json:"proposer_index"`
	ParentRoot    string `json:"parent_root"`
	StateRoot     string `json:"state_root"`
	BodyRoot      string `json:"body_root"`
}

// HeaderResponse is the response for beacon_getBlockHeader.
type HeaderResponse struct {
	Root      string              `json:"root"`
	Canonical bool               `json:"canonical"`
	Header    *SignedHeaderData   `json:"header"`
}

// SignedHeaderData wraps a beacon block header with its signature.
type SignedHeaderData struct {
	Message   *BeaconHeaderMessage `json:"message"`
	Signature string               `json:"signature"`
}

// BeaconHeaderMessage contains the beacon block header fields.
type BeaconHeaderMessage struct {
	Slot          string `json:"slot"`
	ProposerIndex string `json:"proposer_index"`
	ParentRoot    string `json:"parent_root"`
	StateRoot     string `json:"state_root"`
	BodyRoot      string `json:"body_root"`
}

// StateRootResponse is the response for beacon_getStateRoot.
type StateRootResponse struct {
	Root string `json:"root"`
}

// FinalityCheckpointsResponse is the response for beacon_getStateFinalityCheckpoints.
type FinalityCheckpointsResponse struct {
	PreviousJustified *Checkpoint `json:"previous_justified"`
	CurrentJustified  *Checkpoint `json:"current_justified"`
	Finalized         *Checkpoint `json:"finalized"`
}

// Checkpoint represents an epoch checkpoint.
type Checkpoint struct {
	Epoch string `json:"epoch"`
	Root  string `json:"root"`
}

// ValidatorListResponse is the response for beacon_getStateValidators.
type ValidatorListResponse struct {
	Validators []*ValidatorEntry `json:"validators"`
}

// ValidatorEntry represents a single validator in the list.
type ValidatorEntry struct {
	Index     string           `json:"index"`
	Balance   string           `json:"balance"`
	Status    string           `json:"status"`
	Validator *ValidatorData   `json:"validator"`
}

// ValidatorData contains the validator's registration fields.
type ValidatorData struct {
	Pubkey                string `json:"pubkey"`
	WithdrawalCredentials string `json:"withdrawal_credentials"`
	EffectiveBalance      string `json:"effective_balance"`
	Slashed               bool   `json:"slashed"`
	ActivationEpoch       string `json:"activation_epoch"`
	ExitEpoch             string `json:"exit_epoch"`
}

// VersionResponse is the response for beacon_getNodeVersion.
type VersionResponse struct {
	Version string `json:"version"`
}

// SyncingResponse is the response for beacon_getNodeSyncing.
type SyncingResponse struct {
	HeadSlot     string `json:"head_slot"`
	SyncDistance string `json:"sync_distance"`
	IsSyncing    bool   `json:"is_syncing"`
	IsOptimistic bool   `json:"is_optimistic"`
}

// PeerListResponse is the response for beacon_getNodePeers.
type PeerListResponse struct {
	Peers []*BeaconPeer `json:"peers"`
}

// BeaconPeer describes a connected beacon peer.
type BeaconPeer struct {
	PeerID    string `json:"peer_id"`
	State     string `json:"state"`
	Direction string `json:"direction"`
	Address   string `json:"address"`
}

// --- Consensus state ---

// ConsensusState holds the consensus-layer state the Beacon API reads from.
// In a full implementation this would be backed by the beacon chain store;
// here we provide a lightweight in-memory representation that can be updated
// by the consensus engine.
type ConsensusState struct {
	mu sync.RWMutex

	GenesisTime        uint64
	GenesisValRoot     types.Hash
	GenesisForkVersion [4]byte

	HeadSlot       uint64
	FinalizedEpoch uint64
	FinalizedRoot  types.Hash
	JustifiedEpoch uint64
	JustifiedRoot  types.Hash

	// Validators is a simplified list; a production client would use a
	// validator registry backed by the beacon state tree.
	Validators []*ValidatorEntry

	// Syncing state
	IsSyncing    bool
	SyncDistance  uint64

	// Peers known to the consensus layer.
	Peers []*BeaconPeer
}

// NewConsensusState creates a ConsensusState with default genesis values.
func NewConsensusState() *ConsensusState {
	return &ConsensusState{
		GenesisTime:        uint64(time.Date(2020, 12, 1, 12, 0, 23, 0, time.UTC).Unix()),
		GenesisForkVersion: [4]byte{0x00, 0x00, 0x00, 0x00},
	}
}

// --- BeaconAPI ---

// BeaconAPI implements the Beacon API JSON-RPC methods.
type BeaconAPI struct {
	state   *ConsensusState
	backend Backend
}

// NewBeaconAPI creates a new BeaconAPI.
func NewBeaconAPI(state *ConsensusState, backend Backend) *BeaconAPI {
	return &BeaconAPI{
		state:   state,
		backend: backend,
	}
}

// RegisterBeaconRoutes registers all beacon_ methods into the given method map.
// The returned dispatch function handles requests for beacon_ prefixed methods.
func RegisterBeaconRoutes(api *BeaconAPI) map[string]func(*Request) *Response {
	return map[string]func(*Request) *Response{
		"beacon_getGenesis":                   api.getGenesis,
		"beacon_getBlock":                     api.getBlock,
		"beacon_getBlockHeader":               api.getBlockHeader,
		"beacon_getStateRoot":                 api.getStateRoot,
		"beacon_getStateFinalityCheckpoints":  api.getStateFinalityCheckpoints,
		"beacon_getStateValidators":           api.getStateValidators,
		"beacon_getNodeVersion":               api.getNodeVersion,
		"beacon_getNodeSyncing":               api.getNodeSyncing,
		"beacon_getNodePeers":                 api.getNodePeers,
		"beacon_getNodeHealth":                api.getNodeHealth,
	}
}

func (api *BeaconAPI) getGenesis(req *Request) *Response {
	api.state.mu.RLock()
	defer api.state.mu.RUnlock()

	resp := &GenesisResponse{
		GenesisTime:           fmt.Sprintf("%d", api.state.GenesisTime),
		GenesisValidatorsRoot: encodeHash(api.state.GenesisValRoot),
		GenesisForkVersion:    fmt.Sprintf("0x%x", api.state.GenesisForkVersion),
	}
	return successResponse(req.ID, resp)
}

func (api *BeaconAPI) getBlock(req *Request) *Response {
	if len(req.Params) < 1 {
		return beaconErrorResponse(req.ID, BeaconErrBadRequest, "missing slot parameter")
	}

	var slotStr string
	if err := json.Unmarshal(req.Params[0], &slotStr); err != nil {
		return beaconErrorResponse(req.ID, BeaconErrBadRequest, "invalid slot parameter")
	}
	slot := parseHexUint64(slotStr)

	// Map slot to EL block number (simplified 1:1 mapping).
	header := api.backend.HeaderByNumber(BlockNumber(slot))
	if header == nil {
		return beaconErrorResponse(req.ID, BeaconErrNotFound, fmt.Sprintf("block at slot %d not found", slot))
	}

	resp := &BlockResponse{
		Slot:          fmt.Sprintf("%d", slot),
		ProposerIndex: "0",
		ParentRoot:    encodeHash(header.ParentHash),
		StateRoot:     encodeHash(header.Root),
		BodyRoot:      encodeHash(header.TxHash),
	}
	return successResponse(req.ID, resp)
}

func (api *BeaconAPI) getBlockHeader(req *Request) *Response {
	if len(req.Params) < 1 {
		return beaconErrorResponse(req.ID, BeaconErrBadRequest, "missing slot parameter")
	}

	var slotStr string
	if err := json.Unmarshal(req.Params[0], &slotStr); err != nil {
		return beaconErrorResponse(req.ID, BeaconErrBadRequest, "invalid slot parameter")
	}
	slot := parseHexUint64(slotStr)

	header := api.backend.HeaderByNumber(BlockNumber(slot))
	if header == nil {
		return beaconErrorResponse(req.ID, BeaconErrNotFound, fmt.Sprintf("header at slot %d not found", slot))
	}

	resp := &HeaderResponse{
		Root:      encodeHash(header.Hash()),
		Canonical: true,
		Header: &SignedHeaderData{
			Message: &BeaconHeaderMessage{
				Slot:          fmt.Sprintf("%d", slot),
				ProposerIndex: "0",
				ParentRoot:    encodeHash(header.ParentHash),
				StateRoot:     encodeHash(header.Root),
				BodyRoot:      encodeHash(header.TxHash),
			},
			Signature: "0x" + fmt.Sprintf("%0192x", 0),
		},
	}
	return successResponse(req.ID, resp)
}

func (api *BeaconAPI) getStateRoot(req *Request) *Response {
	if len(req.Params) < 1 {
		return beaconErrorResponse(req.ID, BeaconErrBadRequest, "missing state_id parameter")
	}

	var stateID string
	if err := json.Unmarshal(req.Params[0], &stateID); err != nil {
		return beaconErrorResponse(req.ID, BeaconErrBadRequest, "invalid state_id parameter")
	}

	// Resolve state_id: "head", "finalized", "justified", or a slot number.
	var header *types.Header
	switch stateID {
	case "head":
		header = api.backend.CurrentHeader()
	case "finalized":
		api.state.mu.RLock()
		epoch := api.state.FinalizedEpoch
		api.state.mu.RUnlock()
		// Simplified: epoch * 32 slots per epoch.
		header = api.backend.HeaderByNumber(BlockNumber(epoch * 32))
	case "justified":
		api.state.mu.RLock()
		epoch := api.state.JustifiedEpoch
		api.state.mu.RUnlock()
		header = api.backend.HeaderByNumber(BlockNumber(epoch * 32))
	default:
		slot := parseHexUint64(stateID)
		header = api.backend.HeaderByNumber(BlockNumber(slot))
	}

	if header == nil {
		return beaconErrorResponse(req.ID, BeaconErrNotFound, "state not found")
	}

	return successResponse(req.ID, &StateRootResponse{
		Root: encodeHash(header.Root),
	})
}

func (api *BeaconAPI) getStateFinalityCheckpoints(req *Request) *Response {
	api.state.mu.RLock()
	defer api.state.mu.RUnlock()

	prevEpoch := uint64(0)
	if api.state.JustifiedEpoch > 0 {
		prevEpoch = api.state.JustifiedEpoch - 1
	}

	resp := &FinalityCheckpointsResponse{
		PreviousJustified: &Checkpoint{
			Epoch: fmt.Sprintf("%d", prevEpoch),
			Root:  encodeHash(api.state.JustifiedRoot),
		},
		CurrentJustified: &Checkpoint{
			Epoch: fmt.Sprintf("%d", api.state.JustifiedEpoch),
			Root:  encodeHash(api.state.JustifiedRoot),
		},
		Finalized: &Checkpoint{
			Epoch: fmt.Sprintf("%d", api.state.FinalizedEpoch),
			Root:  encodeHash(api.state.FinalizedRoot),
		},
	}
	return successResponse(req.ID, resp)
}

func (api *BeaconAPI) getStateValidators(req *Request) *Response {
	api.state.mu.RLock()
	defer api.state.mu.RUnlock()

	resp := &ValidatorListResponse{
		Validators: api.state.Validators,
	}
	if resp.Validators == nil {
		resp.Validators = []*ValidatorEntry{}
	}
	return successResponse(req.ID, resp)
}

func (api *BeaconAPI) getNodeVersion(req *Request) *Response {
	return successResponse(req.ID, &VersionResponse{
		Version: "eth2028/v0.1.0-beacon",
	})
}

func (api *BeaconAPI) getNodeSyncing(req *Request) *Response {
	api.state.mu.RLock()
	defer api.state.mu.RUnlock()

	header := api.backend.CurrentHeader()
	headSlot := uint64(0)
	if header != nil {
		headSlot = header.Number.Uint64()
	}

	return successResponse(req.ID, &SyncingResponse{
		HeadSlot:     fmt.Sprintf("%d", headSlot),
		SyncDistance: fmt.Sprintf("%d", api.state.SyncDistance),
		IsSyncing:    api.state.IsSyncing,
		IsOptimistic: false,
	})
}

func (api *BeaconAPI) getNodePeers(req *Request) *Response {
	api.state.mu.RLock()
	defer api.state.mu.RUnlock()

	peers := api.state.Peers
	if peers == nil {
		peers = []*BeaconPeer{}
	}
	return successResponse(req.ID, &PeerListResponse{Peers: peers})
}

func (api *BeaconAPI) getNodeHealth(req *Request) *Response {
	api.state.mu.RLock()
	syncing := api.state.IsSyncing
	api.state.mu.RUnlock()

	status := "healthy"
	if syncing {
		status = "syncing"
	}
	return successResponse(req.ID, map[string]string{"status": status})
}

func beaconErrorResponse(id json.RawMessage, code int, msg string) *Response {
	return &Response{
		JSONRPC: "2.0",
		Error:   &RPCError{Code: code, Message: msg},
		ID:      id,
	}
}
