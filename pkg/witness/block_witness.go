// Block execution witness construction per EIP-6800/EIP-8025.
//
// WitnessBuilder tracks all state accesses during block execution and
// assembles them into a compact BlockExecutionWitness. This witness
// captures the pre-state reads, state diffs, and accessed code needed
// to re-execute the block without the full state trie.
//
// The witness is structured as:
//   - Header data: parent hash, state root, block number
//   - PreState: account/storage values read during execution
//   - Codes: contract bytecodes accessed during execution
//   - StateDiffs: changes (old -> new) for each modified account/storage
//
// Encoding and decoding are in block_witness_codec.go.
package witness

import (
	"errors"
	"math/big"
	"sort"
	"sync"

	"github.com/eth2030/eth2030/core/types"
)

// Block witness errors.
var (
	ErrWitnessEmpty          = errors.New("witness: empty witness")
	ErrWitnessEncodeTooLarge = errors.New("witness: encoded witness exceeds max size")
	ErrWitnessDecodeShort    = errors.New("witness: truncated witness data")
	ErrWitnessDecodeBadMagic = errors.New("witness: invalid magic bytes")
	ErrWitnessPreStateFail   = errors.New("witness: pre-state verification failed")
	ErrWitnessNilHeader      = errors.New("witness: nil header data")
)

// Block witness constants.
const (
	witnessVersion byte   = 1
	witnessMagic   uint32 = 0x57544E53 // "WTNS"
	maxWitnessSize        = 16 * 1024 * 1024
)

// BlockExecutionWitness captures all data needed for stateless block validation.
type BlockExecutionWitness struct {
	ParentHash types.Hash
	StateRoot  types.Hash
	BlockNum   uint64
	PreState   map[types.Address]*PreStateAccount
	Codes      map[types.Hash][]byte
	StateDiffs []StateDiff
}

// PreStateAccount is the pre-execution state of an account.
type PreStateAccount struct {
	Nonce    uint64
	Balance  []byte // big-endian encoded balance.
	CodeHash types.Hash
	Storage  map[types.Hash]types.Hash
	Exists   bool
}

// StateDiff describes the changes to a single account during execution.
type StateDiff struct {
	Address        [20]byte
	BalanceDiff    BalanceDiff
	NonceDiff      NonceDiff
	StorageChanges []StorageChange
}

// StorageChange records a single storage slot modification.
type StorageChange struct {
	Key      [32]byte
	OldValue [32]byte
	NewValue [32]byte
}

// BalanceDiff records a balance change.
type BalanceDiff struct {
	OldBalance []byte // big-endian encoded.
	NewBalance []byte // big-endian encoded.
	Changed    bool
}

// NonceDiff records a nonce change.
type NonceDiff struct {
	OldNonce uint64
	NewNonce uint64
	Changed  bool
}

// NewBlockExecutionWitness creates an empty block execution witness.
func NewBlockExecutionWitness(parentHash, stateRoot types.Hash, blockNum uint64) *BlockExecutionWitness {
	return &BlockExecutionWitness{
		ParentHash: parentHash,
		StateRoot:  stateRoot,
		BlockNum:   blockNum,
		PreState:   make(map[types.Address]*PreStateAccount),
		Codes:      make(map[types.Hash][]byte),
	}
}

// WitnessBuilder collects state accesses during block execution and assembles
// them into a BlockExecutionWitness. All methods are safe for concurrent use.
type WitnessBuilder struct {
	mu sync.Mutex

	parentHash types.Hash
	stateRoot  types.Hash
	blockNum   uint64

	reads    map[types.Address]*preStateReads
	codes    map[types.Hash][]byte
	codeAddr map[types.Address]types.Hash
	writes   map[types.Address]*stateDiffAccum
}

// preStateReads tracks all reads for one account.
type preStateReads struct {
	nonce    uint64
	balance  []byte
	codeHash types.Hash
	storage  map[types.Hash]types.Hash
	exists   bool
	touched  bool
}

// stateDiffAccum accumulates state changes for one account.
type stateDiffAccum struct {
	balanceOld []byte
	balanceNew []byte
	balanceSet bool
	nonceOld   uint64
	nonceNew   uint64
	nonceSet   bool
	storage    map[types.Hash][2]types.Hash // key -> [old, new]
}

// NewWitnessBuilder creates a new witness builder for a block.
func NewWitnessBuilder(parentHash, stateRoot types.Hash, blockNum uint64) *WitnessBuilder {
	return &WitnessBuilder{
		parentHash: parentHash,
		stateRoot:  stateRoot,
		blockNum:   blockNum,
		reads:      make(map[types.Address]*preStateReads),
		codes:      make(map[types.Hash][]byte),
		codeAddr:   make(map[types.Address]types.Hash),
		writes:     make(map[types.Address]*stateDiffAccum),
	}
}

// RecordRead records a storage slot read during execution.
func (wb *WitnessBuilder) RecordRead(address [20]byte, key [32]byte, value [32]byte) {
	wb.mu.Lock()
	defer wb.mu.Unlock()

	addr := types.Address(address)
	r := wb.getOrCreateReads(addr)
	k := types.Hash(key)
	if _, exists := r.storage[k]; !exists {
		r.storage[k] = types.Hash(value)
	}
}

// RecordWrite records a storage slot write during execution.
func (wb *WitnessBuilder) RecordWrite(address [20]byte, key [32]byte, oldValue, newValue [32]byte) {
	wb.mu.Lock()
	defer wb.mu.Unlock()

	addr := types.Address(address)
	r := wb.getOrCreateReads(addr)
	k := types.Hash(key)
	if _, exists := r.storage[k]; !exists {
		r.storage[k] = types.Hash(oldValue)
	}

	w := wb.getOrCreateWrites(addr)
	if _, exists := w.storage[k]; !exists {
		w.storage[k] = [2]types.Hash{types.Hash(oldValue), types.Hash(newValue)}
	} else {
		entry := w.storage[k]
		entry[1] = types.Hash(newValue)
		w.storage[k] = entry
	}
}

// RecordCodeAccess records a contract code access during execution.
func (wb *WitnessBuilder) RecordCodeAccess(address [20]byte, codeHash [32]byte, code []byte) {
	wb.mu.Lock()
	defer wb.mu.Unlock()

	addr := types.Address(address)
	ch := types.Hash(codeHash)

	if _, exists := wb.codes[ch]; !exists {
		cp := make([]byte, len(code))
		copy(cp, code)
		wb.codes[ch] = cp
	}
	wb.codeAddr[addr] = ch
	r := wb.getOrCreateReads(addr)
	r.codeHash = ch
}

// RecordAccountAccess records an account field access during execution.
func (wb *WitnessBuilder) RecordAccountAccess(address [20]byte, nonce uint64, balance []byte) {
	wb.mu.Lock()
	defer wb.mu.Unlock()

	addr := types.Address(address)
	r := wb.getOrCreateReads(addr)
	if !r.touched {
		r.nonce = nonce
		r.balance = make([]byte, len(balance))
		copy(r.balance, balance)
		r.exists = true
		r.touched = true
	}
}

// RecordBalanceChange records a balance change for diff tracking.
func (wb *WitnessBuilder) RecordBalanceChange(address [20]byte, oldBalance, newBalance *big.Int) {
	wb.mu.Lock()
	defer wb.mu.Unlock()

	addr := types.Address(address)
	w := wb.getOrCreateWrites(addr)
	if !w.balanceSet {
		w.balanceOld = oldBalance.Bytes()
		w.balanceSet = true
	}
	w.balanceNew = newBalance.Bytes()
}

// RecordNonceChange records a nonce change for diff tracking.
func (wb *WitnessBuilder) RecordNonceChange(address [20]byte, oldNonce, newNonce uint64) {
	wb.mu.Lock()
	defer wb.mu.Unlock()

	addr := types.Address(address)
	w := wb.getOrCreateWrites(addr)
	if !w.nonceSet {
		w.nonceOld = oldNonce
		w.nonceSet = true
	}
	w.nonceNew = newNonce
}

// Build finalizes the collected data into a compact BlockExecutionWitness.
func (wb *WitnessBuilder) Build() *BlockExecutionWitness {
	wb.mu.Lock()
	defer wb.mu.Unlock()

	bew := &BlockExecutionWitness{
		ParentHash: wb.parentHash,
		StateRoot:  wb.stateRoot,
		BlockNum:   wb.blockNum,
		PreState:   make(map[types.Address]*PreStateAccount, len(wb.reads)),
		Codes:      make(map[types.Hash][]byte, len(wb.codes)),
	}

	// Copy pre-state reads.
	for addr, r := range wb.reads {
		psa := &PreStateAccount{
			Nonce:    r.nonce,
			Balance:  make([]byte, len(r.balance)),
			CodeHash: r.codeHash,
			Storage:  make(map[types.Hash]types.Hash, len(r.storage)),
			Exists:   r.exists,
		}
		copy(psa.Balance, r.balance)
		for k, v := range r.storage {
			psa.Storage[k] = v
		}
		bew.PreState[addr] = psa
	}

	// Copy codes.
	for h, code := range wb.codes {
		cp := make([]byte, len(code))
		copy(cp, code)
		bew.Codes[h] = cp
	}

	// Build state diffs sorted by address.
	sortedAddrs := make([]types.Address, 0, len(wb.writes))
	for addr := range wb.writes {
		sortedAddrs = append(sortedAddrs, addr)
	}
	sort.Slice(sortedAddrs, func(i, j int) bool {
		for b := 0; b < types.AddressLength; b++ {
			if sortedAddrs[i][b] != sortedAddrs[j][b] {
				return sortedAddrs[i][b] < sortedAddrs[j][b]
			}
		}
		return false
	})

	for _, addr := range sortedAddrs {
		w := wb.writes[addr]
		sd := StateDiff{
			Address: [20]byte(addr),
		}
		if w.balanceSet {
			sd.BalanceDiff = BalanceDiff{
				OldBalance: w.balanceOld,
				NewBalance: w.balanceNew,
				Changed:    true,
			}
		}
		if w.nonceSet {
			sd.NonceDiff = NonceDiff{
				OldNonce: w.nonceOld,
				NewNonce: w.nonceNew,
				Changed:  true,
			}
		}
		// Sort storage changes by key.
		sortedKeys := make([]types.Hash, 0, len(w.storage))
		for k := range w.storage {
			sortedKeys = append(sortedKeys, k)
		}
		sort.Slice(sortedKeys, func(i, j int) bool {
			for b := 0; b < types.HashLength; b++ {
				if sortedKeys[i][b] != sortedKeys[j][b] {
					return sortedKeys[i][b] < sortedKeys[j][b]
				}
			}
			return false
		})
		for _, k := range sortedKeys {
			vals := w.storage[k]
			sd.StorageChanges = append(sd.StorageChanges, StorageChange{
				Key:      [32]byte(k),
				OldValue: [32]byte(vals[0]),
				NewValue: [32]byte(vals[1]),
			})
		}
		bew.StateDiffs = append(bew.StateDiffs, sd)
	}

	return bew
}

// --- internal helpers ---

func (wb *WitnessBuilder) getOrCreateReads(addr types.Address) *preStateReads {
	r, ok := wb.reads[addr]
	if !ok {
		r = &preStateReads{
			storage: make(map[types.Hash]types.Hash),
		}
		wb.reads[addr] = r
	}
	return r
}

func (wb *WitnessBuilder) getOrCreateWrites(addr types.Address) *stateDiffAccum {
	w, ok := wb.writes[addr]
	if !ok {
		w = &stateDiffAccum{
			storage: make(map[types.Hash][2]types.Hash),
		}
		wb.writes[addr] = w
	}
	return w
}
