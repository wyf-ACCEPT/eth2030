package engine

import (
	"encoding/json"
	"fmt"

	"github.com/eth2028/eth2028/core/types"
)

// InclusionListV1 is the Engine API representation of an inclusion list.
// Sent from the CL to the EL via engine_newInclusionListV1.
type InclusionListV1 struct {
	Slot           uint64        `json:"slot"`
	ValidatorIndex uint64        `json:"validatorIndex"`
	CommitteeRoot  types.Hash    `json:"inclusionListCommitteeRoot"`
	Transactions   [][]byte      `json:"transactions"`
}

// InclusionListStatusV1 is the response to engine_newInclusionListV1.
type InclusionListStatusV1 struct {
	Status string  `json:"status"`
	Error  *string `json:"error,omitempty"`
}

// GetInclusionListResponseV1 is the response to engine_getInclusionListV1.
type GetInclusionListResponseV1 struct {
	Transactions [][]byte `json:"transactions"`
}

// Inclusion list status values.
const (
	ILStatusAccepted = "ACCEPTED"
	ILStatusInvalid  = "INVALID"
)

// ToCore converts the Engine API inclusion list to the core types representation.
func (il *InclusionListV1) ToCore() *types.InclusionList {
	return &types.InclusionList{
		Slot:           il.Slot,
		ValidatorIndex: il.ValidatorIndex,
		CommitteeRoot:  il.CommitteeRoot,
		Transactions:   il.Transactions,
	}
}

// InclusionListFromCore converts a core types InclusionList to Engine API format.
func InclusionListFromCore(il *types.InclusionList) *InclusionListV1 {
	return &InclusionListV1{
		Slot:           il.Slot,
		ValidatorIndex: il.ValidatorIndex,
		CommitteeRoot:  il.CommitteeRoot,
		Transactions:   il.Transactions,
	}
}

// NewInclusionListV1 receives and validates a new inclusion list from the CL.
func (api *EngineAPI) NewInclusionListV1(il InclusionListV1) (InclusionListStatusV1, error) {
	coreIL := il.ToCore()

	// Validate that the backend supports inclusion list handling.
	ilBackend, ok := api.backend.(InclusionListBackend)
	if !ok {
		return InclusionListStatusV1{
			Status: ILStatusInvalid,
			Error:  strPtr("inclusion lists not supported"),
		}, nil
	}

	err := ilBackend.ProcessInclusionList(coreIL)
	if err != nil {
		return InclusionListStatusV1{
			Status: ILStatusInvalid,
			Error:  strPtr(err.Error()),
		}, nil
	}

	return InclusionListStatusV1{Status: ILStatusAccepted}, nil
}

// GetInclusionListV1 returns an inclusion list generated from the EL's mempool.
// Called by CL validators who are inclusion list committee members.
func (api *EngineAPI) GetInclusionListV1() (*GetInclusionListResponseV1, error) {
	ilBackend, ok := api.backend.(InclusionListBackend)
	if !ok {
		return &GetInclusionListResponseV1{Transactions: [][]byte{}}, nil
	}

	il := ilBackend.GetInclusionList()
	return &GetInclusionListResponseV1{
		Transactions: il.Transactions,
	}, nil
}

// InclusionListBackend extends the Backend interface with inclusion list support.
type InclusionListBackend interface {
	// ProcessInclusionList validates and stores a new inclusion list from the CL.
	ProcessInclusionList(il *types.InclusionList) error

	// GetInclusionList generates an inclusion list from the mempool.
	GetInclusionList() *types.InclusionList
}

// handleNewInclusionListV1 processes an engine_newInclusionListV1 request.
func (api *EngineAPI) handleNewInclusionListV1(params []json.RawMessage) (any, *jsonrpcError) {
	if len(params) != 1 {
		return nil, &jsonrpcError{
			Code:    InvalidParamsCode,
			Message: fmt.Sprintf("expected 1 param, got %d", len(params)),
		}
	}

	var il InclusionListV1
	if err := json.Unmarshal(params[0], &il); err != nil {
		return nil, &jsonrpcError{
			Code:    InvalidParamsCode,
			Message: fmt.Sprintf("invalid inclusion list: %v", err),
		}
	}

	result, err := api.NewInclusionListV1(il)
	if err != nil {
		return nil, engineErrorToRPC(err)
	}
	return result, nil
}

// handleGetInclusionListV1 processes an engine_getInclusionListV1 request.
func (api *EngineAPI) handleGetInclusionListV1(params []json.RawMessage) (any, *jsonrpcError) {
	result, err := api.GetInclusionListV1()
	if err != nil {
		return nil, engineErrorToRPC(err)
	}
	return result, nil
}

func strPtr(s string) *string { return &s }
