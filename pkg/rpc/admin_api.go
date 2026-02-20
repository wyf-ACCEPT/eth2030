// admin_api.go provides JSON-RPC dispatch for admin namespace methods.
// It wraps AdminBackend (defined in api_admin.go) with a request/response
// layer matching the pattern used by EthAPI and DebugAPI.
package rpc

import (
	"encoding/json"
	"fmt"
)

// AdminDispatchAPI provides JSON-RPC dispatch for admin_ namespace methods.
// It delegates to AdminAPI (from api_admin.go) for the actual logic,
// but handles JSON-RPC request/response parsing and formatting.
type AdminDispatchAPI struct {
	inner *AdminAPI
}

// NewAdminDispatchAPI creates a new admin dispatch API wrapping the given
// admin backend.
func NewAdminDispatchAPI(backend AdminBackend) *AdminDispatchAPI {
	return &AdminDispatchAPI{
		inner: NewAdminAPI(backend),
	}
}

// HandleAdminRequest dispatches an admin_ namespace JSON-RPC request.
func (a *AdminDispatchAPI) HandleAdminRequest(req *Request) *Response {
	switch req.Method {
	case "admin_addPeer":
		return a.adminAddPeer(req)
	case "admin_removePeer":
		return a.adminRemovePeer(req)
	case "admin_peers":
		return a.adminPeers(req)
	case "admin_nodeInfo":
		return a.adminNodeInfo(req)
	case "admin_datadir":
		return a.adminDatadir(req)
	case "admin_startRPC":
		return a.adminStartRPC(req)
	case "admin_stopRPC":
		return a.adminStopRPC(req)
	case "admin_chainId":
		return a.adminChainId(req)
	default:
		return errorResponse(req.ID, ErrCodeMethodNotFound,
			fmt.Sprintf("method %q not found in admin namespace", req.Method))
	}
}

// adminAddPeer handles admin_addPeer(enode). Returns true on success.
func (a *AdminDispatchAPI) adminAddPeer(req *Request) *Response {
	if len(req.Params) < 1 {
		return errorResponse(req.ID, ErrCodeInvalidParams, "missing enode URL parameter")
	}

	var enodeURL string
	if err := json.Unmarshal(req.Params[0], &enodeURL); err != nil {
		return errorResponse(req.ID, ErrCodeInvalidParams, "invalid enode URL: "+err.Error())
	}

	ok, err := a.inner.AdminAddPeer(enodeURL)
	if err != nil {
		return errorResponse(req.ID, ErrCodeInternal, err.Error())
	}
	return successResponse(req.ID, ok)
}

// adminRemovePeer handles admin_removePeer(enode). Returns true on success.
func (a *AdminDispatchAPI) adminRemovePeer(req *Request) *Response {
	if len(req.Params) < 1 {
		return errorResponse(req.ID, ErrCodeInvalidParams, "missing enode URL parameter")
	}

	var enodeURL string
	if err := json.Unmarshal(req.Params[0], &enodeURL); err != nil {
		return errorResponse(req.ID, ErrCodeInvalidParams, "invalid enode URL: "+err.Error())
	}

	ok, err := a.inner.AdminRemovePeer(enodeURL)
	if err != nil {
		return errorResponse(req.ID, ErrCodeInternal, err.Error())
	}
	return successResponse(req.ID, ok)
}

// adminPeers handles admin_peers(). Returns list of connected peers.
func (a *AdminDispatchAPI) adminPeers(req *Request) *Response {
	peers, err := a.inner.AdminPeers()
	if err != nil {
		return errorResponse(req.ID, ErrCodeInternal, err.Error())
	}
	return successResponse(req.ID, peers)
}

// adminNodeInfo handles admin_nodeInfo(). Returns node information.
func (a *AdminDispatchAPI) adminNodeInfo(req *Request) *Response {
	info, err := a.inner.AdminNodeInfo()
	if err != nil {
		return errorResponse(req.ID, ErrCodeInternal, err.Error())
	}
	return successResponse(req.ID, info)
}

// adminDatadir handles admin_datadir(). Returns data directory path.
func (a *AdminDispatchAPI) adminDatadir(req *Request) *Response {
	dir, err := a.inner.AdminDataDir()
	if err != nil {
		return errorResponse(req.ID, ErrCodeInternal, err.Error())
	}
	return successResponse(req.ID, dir)
}

// adminStartRPC handles admin_startRPC(host, port).
func (a *AdminDispatchAPI) adminStartRPC(req *Request) *Response {
	if len(req.Params) < 2 {
		return errorResponse(req.ID, ErrCodeInvalidParams,
			"expected params: [host, port]")
	}

	var host string
	if err := json.Unmarshal(req.Params[0], &host); err != nil {
		return errorResponse(req.ID, ErrCodeInvalidParams, "invalid host: "+err.Error())
	}

	var port int
	if err := json.Unmarshal(req.Params[1], &port); err != nil {
		return errorResponse(req.ID, ErrCodeInvalidParams, "invalid port: "+err.Error())
	}

	ok, err := a.inner.AdminStartRPC(host, port)
	if err != nil {
		return errorResponse(req.ID, ErrCodeInternal, err.Error())
	}
	return successResponse(req.ID, ok)
}

// adminStopRPC handles admin_stopRPC().
func (a *AdminDispatchAPI) adminStopRPC(req *Request) *Response {
	ok, err := a.inner.AdminStopRPC()
	if err != nil {
		return errorResponse(req.ID, ErrCodeInternal, err.Error())
	}
	return successResponse(req.ID, ok)
}

// adminChainId handles admin_chainId().
func (a *AdminDispatchAPI) adminChainId(req *Request) *Response {
	id, err := a.inner.AdminChainID()
	if err != nil {
		return errorResponse(req.ID, ErrCodeInternal, err.Error())
	}
	return successResponse(req.ID, id)
}
