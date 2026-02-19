package snap

import (
	"bytes"
	"errors"

	"github.com/eth2028/eth2028/core/rawdb"
	"github.com/eth2028/eth2028/core/types"
	"github.com/eth2028/eth2028/crypto"
)

var (
	// ErrRequestTooLarge is returned when a request exceeds protocol limits.
	ErrRequestTooLarge = errors.New("snap: request exceeds size limits")

	// ErrInvalidRange is returned when the requested range is malformed.
	ErrInvalidRange = errors.New("snap: invalid range (origin > limit)")

	// ErrMissingRoot is returned when the requested state root is unknown.
	ErrMissingRoot = errors.New("snap: unknown state root")
)

// StateBackend provides read access to the world state for serving
// snap protocol requests. It abstracts away the underlying trie and
// database implementation.
type StateBackend interface {
	// AccountIterator returns accounts in hash-sorted order starting at
	// the given origin hash under the given state root. The callback is
	// invoked for each account. Return false from the callback to stop.
	AccountIterator(root types.Hash, origin types.Hash, fn func(hash types.Hash, body []byte) bool) error

	// StorageIterator returns storage slots in hash-sorted order for the
	// given account under the given state root, starting at origin.
	StorageIterator(root types.Hash, account types.Hash, origin []byte, fn func(hash types.Hash, body []byte) bool) error

	// Code returns the bytecode for the given code hash.
	Code(hash types.Hash) ([]byte, error)

	// TrieNode returns the trie node at the given path under the given root.
	TrieNode(root types.Hash, path []byte) ([]byte, error)

	// AccountProof returns a Merkle proof for the given account hash
	// under the state root.
	AccountProof(root types.Hash, hash types.Hash) ([][]byte, error)

	// StorageProof returns a Merkle proof for the given slot hash in
	// the given account under the state root.
	StorageProof(root types.Hash, account types.Hash, slot types.Hash) ([][]byte, error)
}

// ServerHandler implements the Handler interface by serving snap protocol
// requests from a StateBackend.
type ServerHandler struct {
	backend StateBackend
}

// NewServerHandler creates a new snap protocol server handler.
func NewServerHandler(backend StateBackend) *ServerHandler {
	return &ServerHandler{backend: backend}
}

// HandleGetAccountRange iterates accounts in hash-sorted order within the
// [origin, limit] range and returns up to the soft byte limit of account data,
// plus boundary proofs for the range endpoints.
func (h *ServerHandler) HandleGetAccountRange(req *GetAccountRangePacket) (*AccountRangePacket, error) {
	// Validate the range.
	if bytes.Compare(req.Origin[:], req.Limit[:]) > 0 {
		return nil, ErrInvalidRange
	}

	softLimit := req.Bytes
	if softLimit == 0 || softLimit > SoftResponseLimit {
		softLimit = SoftResponseLimit
	}

	var accounts []AccountData
	var totalSize uint64

	err := h.backend.AccountIterator(req.Root, req.Origin, func(hash types.Hash, body []byte) bool {
		// Stop if past the limit hash.
		if bytes.Compare(hash[:], req.Limit[:]) > 0 {
			return false
		}
		// Stop if we hit the account count cap.
		if len(accounts) >= MaxAccountRangeCount {
			return false
		}

		accounts = append(accounts, AccountData{
			Hash: hash,
			Body: append([]byte{}, body...),
		})
		totalSize += uint64(types.HashLength + len(body))

		// Stop if we exceed the soft byte limit (but include at least one).
		if totalSize >= softLimit && len(accounts) > 0 {
			return false
		}
		// Hard cap.
		if totalSize >= HardResponseLimit {
			return false
		}
		return true
	})
	if err != nil {
		return nil, err
	}

	// Generate boundary proofs if we have accounts.
	var proof [][]byte
	if len(accounts) > 0 {
		// Proof for the first account (origin side).
		originProof, err := h.backend.AccountProof(req.Root, accounts[0].Hash)
		if err == nil {
			proof = append(proof, originProof...)
		}
		// Proof for the last account (limit side), if different from origin.
		if len(accounts) > 1 {
			lastProof, err := h.backend.AccountProof(req.Root, accounts[len(accounts)-1].Hash)
			if err == nil {
				proof = append(proof, lastProof...)
			}
		}
	}

	return &AccountRangePacket{
		ID:       req.ID,
		Accounts: accounts,
		Proof:    proof,
	}, nil
}

// HandleGetStorageRanges iterates storage slots for the requested accounts
// within the [origin, limit] range.
func (h *ServerHandler) HandleGetStorageRanges(req *GetStorageRangesPacket) (*StorageRangesPacket, error) {
	softLimit := req.Bytes
	if softLimit == 0 || softLimit > SoftResponseLimit {
		softLimit = SoftResponseLimit
	}

	var allSlots [][]StorageData
	var totalSize uint64
	var proof [][]byte

	// Determine origin/limit for the storage hash range.
	origin := req.Origin
	limit := req.Limit
	if len(limit) == 0 {
		// Default to full range.
		limit = bytes.Repeat([]byte{0xff}, types.HashLength)
	}

	for i, account := range req.Accounts {
		if totalSize >= HardResponseLimit {
			break
		}

		var slots []StorageData

		iterOrigin := origin
		// Only the first account uses the specified origin; subsequent accounts
		// start from the beginning of their storage trie.
		if i > 0 {
			iterOrigin = nil
		}

		err := h.backend.StorageIterator(req.Root, account, iterOrigin, func(hash types.Hash, body []byte) bool {
			// Stop if we are past the limit hash.
			if len(limit) >= types.HashLength && bytes.Compare(hash[:], limit[:types.HashLength]) > 0 {
				return false
			}
			if len(slots) >= MaxStorageRangeCount {
				return false
			}

			slots = append(slots, StorageData{
				Hash: hash,
				Body: append([]byte{}, body...),
			})
			totalSize += uint64(types.HashLength + len(body))

			if totalSize >= softLimit {
				return false
			}
			if totalSize >= HardResponseLimit {
				return false
			}
			return true
		})
		if err != nil {
			return nil, err
		}

		allSlots = append(allSlots, slots)

		// If we exhausted the soft limit mid-account, generate a proof
		// for the last served storage slot so the client can resume.
		if totalSize >= softLimit && len(slots) > 0 {
			lastSlot := slots[len(slots)-1].Hash
			slotProof, err := h.backend.StorageProof(req.Root, account, lastSlot)
			if err == nil {
				proof = append(proof, slotProof...)
			}
			break
		}
	}

	return &StorageRangesPacket{
		ID:    req.ID,
		Slots: allSlots,
		Proof: proof,
	}, nil
}

// HandleGetByteCodes retrieves contract bytecodes by code hash, up to
// the soft response size limit.
func (h *ServerHandler) HandleGetByteCodes(req *GetByteCodesPacket) (*ByteCodesPacket, error) {
	softLimit := req.Bytes
	if softLimit == 0 || softLimit > SoftResponseLimit {
		softLimit = SoftResponseLimit
	}

	var codes [][]byte
	var totalSize uint64

	for _, hash := range req.Hashes {
		if len(codes) >= MaxByteCodeCount {
			break
		}
		if totalSize >= HardResponseLimit {
			break
		}

		code, err := h.backend.Code(hash)
		if err != nil {
			// Skip missing bytecodes rather than failing the entire request.
			if errors.Is(err, rawdb.ErrNotFound) {
				continue
			}
			return nil, err
		}

		// Verify the code hash matches.
		computed := crypto.Keccak256Hash(code)
		if computed != hash {
			continue
		}

		codes = append(codes, code)
		totalSize += uint64(len(code))

		if totalSize >= softLimit {
			break
		}
	}

	return &ByteCodesPacket{
		ID:    req.ID,
		Codes: codes,
	}, nil
}

// HandleGetTrieNodes retrieves trie nodes by path under the given state root,
// up to the soft response size limit.
func (h *ServerHandler) HandleGetTrieNodes(req *GetTrieNodesPacket) (*TrieNodesPacket, error) {
	softLimit := req.Bytes
	if softLimit == 0 || softLimit > SoftResponseLimit {
		softLimit = SoftResponseLimit
	}

	var nodes [][]byte
	var totalSize uint64

	for _, pathSet := range req.Paths {
		if len(nodes) >= MaxTrieNodeCount {
			break
		}
		if totalSize >= HardResponseLimit {
			break
		}

		// The path set encodes either an account trie path (single element)
		// or a storage trie path (account hash + storage path).
		var path []byte
		for _, p := range pathSet {
			path = append(path, p...)
		}

		data, err := h.backend.TrieNode(req.Root, path)
		if err != nil {
			// Skip missing nodes.
			nodes = append(nodes, nil)
			continue
		}

		nodes = append(nodes, data)
		totalSize += uint64(len(data))

		if totalSize >= softLimit {
			break
		}
	}

	return &TrieNodesPacket{
		ID:    req.ID,
		Nodes: nodes,
	}, nil
}
