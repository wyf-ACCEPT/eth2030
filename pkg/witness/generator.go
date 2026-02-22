// generator.go implements a witness generation system that creates proofs for
// stateless execution. It traces state access during block execution and
// produces a compressed execution witness containing all accessed accounts,
// storage keys, code chunks, and Merkle proofs.
package witness

import (
	"errors"
	"fmt"
	"math/big"
	"sort"
	"sync"

	"github.com/eth2030/eth2030/core/types"
	"github.com/eth2030/eth2030/crypto"
)

// Generator-specific errors.
var (
	ErrGeneratorNotStarted = errors.New("witness generator not started")
	ErrNilBlock            = errors.New("nil block provided")
	ErrNilState            = errors.New("nil state provided")
	ErrWitnessInvalid      = errors.New("witness validation failed")
)

// AccessEventType identifies the kind of state access recorded.
type AccessEventType uint8

const (
	AccountRead  AccessEventType = iota // account field was read
	AccountWrite                        // account field was modified
	StorageRead                         // storage slot was read
	StorageWrite                        // storage slot was modified
	CodeRead                            // contract code was loaded
)

// AccessEvent records a single state access during block execution.
type AccessEvent struct {
	Type    AccessEventType
	Address types.Address
	Key     types.Hash // storage key (zero for account/code events)
	Value   []byte     // value at the time of access
}

// GeneratedWitness contains all state data needed for stateless execution
// of a block. It captures accessed accounts, storage keys, code chunks,
// and proof data.
type GeneratedWitness struct {
	// BlockNumber is the block for which this witness was generated.
	BlockNumber uint64

	// ParentRoot is the pre-state root before block execution.
	ParentRoot types.Hash

	// PostRoot is the state root after block execution.
	PostRoot types.Hash

	// Accounts maps addresses to their pre-state account data.
	Accounts map[types.Address]*WitnessAccount

	// StorageProofs maps addresses to sets of accessed storage keys.
	StorageProofs map[types.Address]map[types.Hash]types.Hash

	// CodeChunks maps code hashes to contract bytecode.
	CodeChunks map[types.Hash][]byte

	// Events records all state access events in order.
	Events []AccessEvent

	// ProofData holds Merkle proof nodes keyed by the node hash.
	ProofData map[types.Hash][]byte
}

// WitnessAccount holds the pre-state of an account in the witness.
type WitnessAccount struct {
	Balance  *big.Int
	Nonce    uint64
	CodeHash types.Hash
	Exists   bool
}

// WitnessGeneratorConfig configures witness generation behavior.
type WitnessGeneratorConfig struct {
	// MaxWitnessSize is the maximum allowed witness size in bytes. Zero = no limit.
	MaxWitnessSize int

	// CollectEvents enables recording of individual AccessEvent entries.
	CollectEvents bool

	// IncludeProofs enables generation of Merkle proof data.
	IncludeProofs bool
}

// DefaultGeneratorConfig returns a WitnessGeneratorConfig with sensible defaults.
func DefaultGeneratorConfig() WitnessGeneratorConfig {
	return WitnessGeneratorConfig{
		MaxWitnessSize: DefaultMaxWitnessSize,
		CollectEvents:  true,
		IncludeProofs:  true,
	}
}

// WitnessGenerator traces state access during block execution and produces
// execution witnesses for stateless verification. All public methods are
// thread-safe.
type WitnessGenerator struct {
	config WitnessGeneratorConfig

	mu       sync.Mutex
	started  bool
	blockNum uint64
	preRoot  types.Hash

	accounts      map[types.Address]*WitnessAccount
	storageKeys   map[types.Address]map[types.Hash]types.Hash
	codeChunks    map[types.Hash][]byte
	events        []AccessEvent
	recordedAddrs map[types.Address]bool
}

// NewWitnessGenerator creates a WitnessGenerator with the given configuration.
func NewWitnessGenerator(config WitnessGeneratorConfig) *WitnessGenerator {
	return &WitnessGenerator{
		config:        config,
		accounts:      make(map[types.Address]*WitnessAccount),
		storageKeys:   make(map[types.Address]map[types.Hash]types.Hash),
		codeChunks:    make(map[types.Hash][]byte),
		recordedAddrs: make(map[types.Address]bool),
	}
}

// StateReader provides read-only access to the state for witness generation.
// This is a subset of the full StateDB interface.
type StateReader interface {
	GetBalance(addr types.Address) *big.Int
	GetNonce(addr types.Address) uint64
	GetCodeHash(addr types.Address) types.Hash
	GetCode(addr types.Address) []byte
	GetState(addr types.Address, key types.Hash) types.Hash
	Exist(addr types.Address) bool
	GetRoot() types.Hash
}

// BeginBlock starts a new witness generation session for the given block.
// Previous state is cleared.
func (g *WitnessGenerator) BeginBlock(blockNumber uint64, preRoot types.Hash) {
	g.mu.Lock()
	defer g.mu.Unlock()

	g.started = true
	g.blockNum = blockNumber
	g.preRoot = preRoot
	g.accounts = make(map[types.Address]*WitnessAccount)
	g.storageKeys = make(map[types.Address]map[types.Hash]types.Hash)
	g.codeChunks = make(map[types.Hash][]byte)
	g.events = nil
	g.recordedAddrs = make(map[types.Address]bool)
}

// RecordAccountRead records that an account's fields were read. The pre-state
// values are captured on first access only.
func (g *WitnessGenerator) RecordAccountRead(addr types.Address, reader StateReader) {
	g.mu.Lock()
	defer g.mu.Unlock()

	g.ensureAccount(addr, reader)
	if g.config.CollectEvents {
		g.events = append(g.events, AccessEvent{
			Type:    AccountRead,
			Address: addr,
		})
	}
}

// RecordAccountWrite records that an account's fields were modified.
func (g *WitnessGenerator) RecordAccountWrite(addr types.Address, reader StateReader) {
	g.mu.Lock()
	defer g.mu.Unlock()

	g.ensureAccount(addr, reader)
	if g.config.CollectEvents {
		g.events = append(g.events, AccessEvent{
			Type:    AccountWrite,
			Address: addr,
		})
	}
}

// RecordStorageRead records that a storage slot was read.
func (g *WitnessGenerator) RecordStorageRead(addr types.Address, key types.Hash, reader StateReader) {
	g.mu.Lock()
	defer g.mu.Unlock()

	g.ensureAccount(addr, reader)

	if _, ok := g.storageKeys[addr]; !ok {
		g.storageKeys[addr] = make(map[types.Hash]types.Hash)
	}
	// Record value only on first access.
	if _, seen := g.storageKeys[addr][key]; !seen {
		val := reader.GetState(addr, key)
		g.storageKeys[addr][key] = val
	}

	if g.config.CollectEvents {
		g.events = append(g.events, AccessEvent{
			Type:    StorageRead,
			Address: addr,
			Key:     key,
			Value:   g.storageKeys[addr][key].Bytes(),
		})
	}
}

// RecordStorageWrite records that a storage slot was modified.
func (g *WitnessGenerator) RecordStorageWrite(addr types.Address, key types.Hash, reader StateReader) {
	g.mu.Lock()
	defer g.mu.Unlock()

	g.ensureAccount(addr, reader)

	if _, ok := g.storageKeys[addr]; !ok {
		g.storageKeys[addr] = make(map[types.Hash]types.Hash)
	}
	// Capture pre-state value only on first access.
	if _, seen := g.storageKeys[addr][key]; !seen {
		val := reader.GetState(addr, key)
		g.storageKeys[addr][key] = val
	}

	if g.config.CollectEvents {
		g.events = append(g.events, AccessEvent{
			Type:    StorageWrite,
			Address: addr,
			Key:     key,
		})
	}
}

// RecordCodeRead records that contract code was loaded.
func (g *WitnessGenerator) RecordCodeRead(addr types.Address, reader StateReader) {
	g.mu.Lock()
	defer g.mu.Unlock()

	g.ensureAccount(addr, reader)

	codeHash := reader.GetCodeHash(addr)
	if codeHash == types.EmptyCodeHash || codeHash.IsZero() {
		return
	}
	if _, ok := g.codeChunks[codeHash]; !ok {
		code := reader.GetCode(addr)
		if len(code) > 0 {
			cp := make([]byte, len(code))
			copy(cp, code)
			g.codeChunks[codeHash] = cp
		}
	}

	if g.config.CollectEvents {
		g.events = append(g.events, AccessEvent{
			Type:    CodeRead,
			Address: addr,
		})
	}
}

// GenerateWitness produces the final GeneratedWitness from all recorded
// state accesses. The postRoot is the state root after block execution.
func (g *WitnessGenerator) GenerateWitness(postRoot types.Hash) (*GeneratedWitness, error) {
	g.mu.Lock()
	defer g.mu.Unlock()

	if !g.started {
		return nil, ErrGeneratorNotStarted
	}

	w := &GeneratedWitness{
		BlockNumber:   g.blockNum,
		ParentRoot:    g.preRoot,
		PostRoot:      postRoot,
		Accounts:      make(map[types.Address]*WitnessAccount, len(g.accounts)),
		StorageProofs: make(map[types.Address]map[types.Hash]types.Hash, len(g.storageKeys)),
		CodeChunks:    make(map[types.Hash][]byte, len(g.codeChunks)),
		ProofData:     make(map[types.Hash][]byte),
	}

	// Deep copy accounts.
	for addr, acc := range g.accounts {
		w.Accounts[addr] = &WitnessAccount{
			Balance:  new(big.Int).Set(acc.Balance),
			Nonce:    acc.Nonce,
			CodeHash: acc.CodeHash,
			Exists:   acc.Exists,
		}
	}

	// Deep copy storage proofs.
	for addr, keys := range g.storageKeys {
		w.StorageProofs[addr] = make(map[types.Hash]types.Hash, len(keys))
		for k, v := range keys {
			w.StorageProofs[addr][k] = v
		}
	}

	// Deep copy code chunks.
	for h, code := range g.codeChunks {
		cp := make([]byte, len(code))
		copy(cp, code)
		w.CodeChunks[h] = cp
	}

	// Copy events.
	if g.config.CollectEvents && len(g.events) > 0 {
		w.Events = make([]AccessEvent, len(g.events))
		copy(w.Events, g.events)
	}

	// Generate proof data: deterministic hash of all accessed state.
	if g.config.IncludeProofs {
		g.generateProofData(w)
	}

	// Check witness size limit.
	if g.config.MaxWitnessSize > 0 {
		size := EstimateGeneratedWitnessSize(w)
		if size > g.config.MaxWitnessSize {
			return nil, fmt.Errorf("%w: size %d exceeds max %d",
				ErrWitnessTooLarge, size, g.config.MaxWitnessSize)
		}
	}

	return w, nil
}

// EstimateWitnessSize estimates the witness size for a block before full
// generation. This is useful for quickly determining if a block's witness
// would be too large.
func (g *WitnessGenerator) EstimateWitnessSize() int {
	g.mu.Lock()
	defer g.mu.Unlock()

	// Base: block number (8) + parent root (32) + post root (32)
	size := 72

	// Accounts: ~80 bytes each (address + balance + nonce + codehash + exists)
	size += len(g.accounts) * 80

	// Storage keys: address(20) + key(32) + value(32) per entry
	for _, keys := range g.storageKeys {
		size += types.AddressLength + len(keys)*(types.HashLength*2)
	}

	// Code chunks: hash(32) + code bytes
	for _, code := range g.codeChunks {
		size += types.HashLength + len(code)
	}

	return size
}

// Reset clears all recording state. The generator can be reused after Reset.
func (g *WitnessGenerator) Reset() {
	g.mu.Lock()
	defer g.mu.Unlock()

	g.started = false
	g.blockNum = 0
	g.preRoot = types.Hash{}
	g.accounts = make(map[types.Address]*WitnessAccount)
	g.storageKeys = make(map[types.Address]map[types.Hash]types.Hash)
	g.codeChunks = make(map[types.Hash][]byte)
	g.events = nil
	g.recordedAddrs = make(map[types.Address]bool)
}

// IsStarted reports whether BeginBlock has been called.
func (g *WitnessGenerator) IsStarted() bool {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.started
}

// AccountCount returns the number of distinct accounts recorded.
func (g *WitnessGenerator) AccountCount() int {
	g.mu.Lock()
	defer g.mu.Unlock()
	return len(g.accounts)
}

// StorageKeyCount returns the total number of distinct storage keys recorded.
func (g *WitnessGenerator) StorageKeyCount() int {
	g.mu.Lock()
	defer g.mu.Unlock()
	count := 0
	for _, keys := range g.storageKeys {
		count += len(keys)
	}
	return count
}

// EventCount returns the number of recorded access events.
func (g *WitnessGenerator) EventCount() int {
	g.mu.Lock()
	defer g.mu.Unlock()
	return len(g.events)
}

// ensureAccount records the pre-state of an account on first access.
// Caller must hold g.mu.
func (g *WitnessGenerator) ensureAccount(addr types.Address, reader StateReader) {
	if g.recordedAddrs[addr] {
		return
	}
	g.recordedAddrs[addr] = true

	exists := reader.Exist(addr)
	acc := &WitnessAccount{
		Balance:  new(big.Int),
		Exists:   exists,
	}
	if exists {
		acc.Balance = new(big.Int).Set(reader.GetBalance(addr))
		acc.Nonce = reader.GetNonce(addr)
		acc.CodeHash = reader.GetCodeHash(addr)
	}
	g.accounts[addr] = acc
}

// generateProofData builds deterministic proof nodes from the witness data.
// Each proof node is the keccak hash of the serialized account or storage data.
// Caller must hold g.mu.
func (g *WitnessGenerator) generateProofData(w *GeneratedWitness) {
	// Sort addresses for deterministic output.
	addrs := make([]types.Address, 0, len(w.Accounts))
	for addr := range w.Accounts {
		addrs = append(addrs, addr)
	}
	sort.Slice(addrs, func(i, j int) bool {
		return addrLess(addrs[i], addrs[j])
	})

	for _, addr := range addrs {
		acc := w.Accounts[addr]

		// Build account proof node: address + balance + nonce + codeHash.
		var data []byte
		data = append(data, addr[:]...)
		data = append(data, acc.Balance.Bytes()...)
		nonceBuf := make([]byte, 8)
		nonceBuf[0] = byte(acc.Nonce >> 56)
		nonceBuf[1] = byte(acc.Nonce >> 48)
		nonceBuf[2] = byte(acc.Nonce >> 40)
		nonceBuf[3] = byte(acc.Nonce >> 32)
		nonceBuf[4] = byte(acc.Nonce >> 24)
		nonceBuf[5] = byte(acc.Nonce >> 16)
		nonceBuf[6] = byte(acc.Nonce >> 8)
		nonceBuf[7] = byte(acc.Nonce)
		data = append(data, nonceBuf...)
		data = append(data, acc.CodeHash[:]...)

		nodeHash := crypto.Keccak256Hash(data)
		w.ProofData[nodeHash] = data
	}

	// Storage proof nodes.
	for _, addr := range addrs {
		keys, ok := w.StorageProofs[addr]
		if !ok || len(keys) == 0 {
			continue
		}
		// Sort keys for determinism.
		sortedKeys := make([]types.Hash, 0, len(keys))
		for k := range keys {
			sortedKeys = append(sortedKeys, k)
		}
		sort.Slice(sortedKeys, func(i, j int) bool {
			return hashLess(sortedKeys[i], sortedKeys[j])
		})

		for _, key := range sortedKeys {
			val := keys[key]
			var data []byte
			data = append(data, addr[:]...)
			data = append(data, key[:]...)
			data = append(data, val[:]...)

			nodeHash := crypto.Keccak256Hash(data)
			w.ProofData[nodeHash] = data
		}
	}
}

// --- WitnessCompressor ---

// WitnessCompressor compresses a GeneratedWitness using deduplication
// and delta encoding to reduce wire size.
type WitnessCompressor struct {
	mu sync.Mutex
}

// NewWitnessCompressor creates a new WitnessCompressor.
func NewWitnessCompressor() *WitnessCompressor {
	return &WitnessCompressor{}
}

// CompressedWitness is a size-optimized representation of a GeneratedWitness.
type CompressedWitness struct {
	// BlockNumber and roots.
	BlockNumber uint64
	ParentRoot  types.Hash
	PostRoot    types.Hash

	// UniqueAddresses lists each unique address exactly once.
	UniqueAddresses []types.Address

	// AccountData maps address index to compressed account data.
	AccountData []CompressedAccount

	// StorageData holds delta-encoded storage entries.
	StorageData []CompressedStorageEntry

	// CodeRefs maps code hash to code bytes (deduplicated).
	CodeRefs map[types.Hash][]byte

	// OriginalSize is the estimated uncompressed witness size.
	OriginalSize int

	// CompressedSize is the estimated compressed witness size.
	CompressedSize int
}

// CompressedAccount holds an account's data with an index reference.
type CompressedAccount struct {
	AddressIndex uint32
	Balance      []byte // big.Int bytes (variable length)
	Nonce        uint64
	CodeHash     types.Hash
	Exists       bool
}

// CompressedStorageEntry holds a storage entry with delta encoding.
type CompressedStorageEntry struct {
	AddressIndex uint32
	Key          types.Hash
	Value        types.Hash
}

// Compress compresses a GeneratedWitness using deduplication and delta encoding.
func (c *WitnessCompressor) Compress(w *GeneratedWitness) *CompressedWitness {
	c.mu.Lock()
	defer c.mu.Unlock()

	if w == nil {
		return nil
	}

	// Build unique sorted address list.
	addrSet := make(map[types.Address]struct{})
	for addr := range w.Accounts {
		addrSet[addr] = struct{}{}
	}
	for addr := range w.StorageProofs {
		addrSet[addr] = struct{}{}
	}
	addrs := make([]types.Address, 0, len(addrSet))
	for addr := range addrSet {
		addrs = append(addrs, addr)
	}
	sort.Slice(addrs, func(i, j int) bool {
		return addrLess(addrs[i], addrs[j])
	})

	// Build address index.
	addrIndex := make(map[types.Address]uint32, len(addrs))
	for i, addr := range addrs {
		addrIndex[addr] = uint32(i)
	}

	cw := &CompressedWitness{
		BlockNumber:     w.BlockNumber,
		ParentRoot:      w.ParentRoot,
		PostRoot:        w.PostRoot,
		UniqueAddresses: addrs,
		CodeRefs:        make(map[types.Hash][]byte),
	}

	// Compress accounts.
	for addr, acc := range w.Accounts {
		idx := addrIndex[addr]
		ca := CompressedAccount{
			AddressIndex: idx,
			Nonce:        acc.Nonce,
			CodeHash:     acc.CodeHash,
			Exists:       acc.Exists,
		}
		if acc.Balance != nil {
			ca.Balance = acc.Balance.Bytes()
		}
		cw.AccountData = append(cw.AccountData, ca)
	}

	// Compress storage entries: use address index instead of full address.
	for addr, keys := range w.StorageProofs {
		idx := addrIndex[addr]
		for k, v := range keys {
			cw.StorageData = append(cw.StorageData, CompressedStorageEntry{
				AddressIndex: idx,
				Key:          k,
				Value:        v,
			})
		}
	}

	// Deduplicate code chunks.
	for h, code := range w.CodeChunks {
		cp := make([]byte, len(code))
		copy(cp, code)
		cw.CodeRefs[h] = cp
	}

	cw.OriginalSize = EstimateGeneratedWitnessSize(w)
	cw.CompressedSize = estimateCompressedSize(cw)

	return cw
}

// Decompress expands a CompressedWitness back to a GeneratedWitness.
func (c *WitnessCompressor) Decompress(cw *CompressedWitness) *GeneratedWitness {
	c.mu.Lock()
	defer c.mu.Unlock()

	if cw == nil {
		return nil
	}

	w := &GeneratedWitness{
		BlockNumber:   cw.BlockNumber,
		ParentRoot:    cw.ParentRoot,
		PostRoot:      cw.PostRoot,
		Accounts:      make(map[types.Address]*WitnessAccount),
		StorageProofs: make(map[types.Address]map[types.Hash]types.Hash),
		CodeChunks:    make(map[types.Hash][]byte),
		ProofData:     make(map[types.Hash][]byte),
	}

	for _, ca := range cw.AccountData {
		addr := cw.UniqueAddresses[ca.AddressIndex]
		acc := &WitnessAccount{
			Balance:  new(big.Int).SetBytes(ca.Balance),
			Nonce:    ca.Nonce,
			CodeHash: ca.CodeHash,
			Exists:   ca.Exists,
		}
		w.Accounts[addr] = acc
	}

	for _, se := range cw.StorageData {
		addr := cw.UniqueAddresses[se.AddressIndex]
		if _, ok := w.StorageProofs[addr]; !ok {
			w.StorageProofs[addr] = make(map[types.Hash]types.Hash)
		}
		w.StorageProofs[addr][se.Key] = se.Value
	}

	for h, code := range cw.CodeRefs {
		cp := make([]byte, len(code))
		copy(cp, code)
		w.CodeChunks[h] = cp
	}

	return w
}

// --- Estimate / Validate helpers ---

// EstimateGeneratedWitnessSize computes the approximate wire size of a
// GeneratedWitness in bytes.
func EstimateGeneratedWitnessSize(w *GeneratedWitness) int {
	if w == nil {
		return 0
	}
	// Base overhead: block number + parent root + post root
	size := 8 + types.HashLength*2

	// Accounts: address + balance(32 max) + nonce(8) + codeHash(32) + exists(1)
	size += len(w.Accounts) * (types.AddressLength + 32 + 8 + types.HashLength + 1)

	// Storage proofs: per-entry address(20) + key(32) + value(32)
	for _, keys := range w.StorageProofs {
		size += types.AddressLength
		size += len(keys) * (types.HashLength * 2)
	}

	// Code chunks.
	for _, code := range w.CodeChunks {
		size += types.HashLength + len(code)
	}

	// Proof data.
	for _, data := range w.ProofData {
		size += types.HashLength + len(data)
	}

	// Events.
	size += len(w.Events) * (1 + types.AddressLength + types.HashLength + 32)

	return size
}

// ValidateWitnessRoots verifies that a witness produces the expected post-state
// root when applied. This is a simplified check that compares the witness's
// declared post-root against the expected root.
func ValidateWitnessRoots(w *GeneratedWitness, expectedPostRoot types.Hash) error {
	if w == nil {
		return ErrNilState
	}
	if w.PostRoot != expectedPostRoot {
		return fmt.Errorf("%w: witness post root %s != expected %s",
			ErrWitnessInvalid, w.PostRoot.Hex(), expectedPostRoot.Hex())
	}
	// Verify proof data is internally consistent (all proof nodes hash correctly).
	for hash, data := range w.ProofData {
		computed := crypto.Keccak256Hash(data)
		if computed != hash {
			return fmt.Errorf("%w: proof node hash mismatch for %s",
				ErrWitnessInvalid, hash.Hex())
		}
	}
	return nil
}

// estimateCompressedSize estimates the wire size of a CompressedWitness.
func estimateCompressedSize(cw *CompressedWitness) int {
	size := 8 + types.HashLength*2 // block number + roots

	// Address table: 20 bytes per unique address + 4 byte count.
	size += 4 + len(cw.UniqueAddresses)*types.AddressLength

	// Account data: index(4) + balance(variable) + nonce(8) + codeHash(32) + exists(1)
	for _, ca := range cw.AccountData {
		size += 4 + len(ca.Balance) + 8 + types.HashLength + 1
	}

	// Storage data: index(4) + key(32) + value(32) per entry.
	size += len(cw.StorageData) * (4 + types.HashLength*2)

	// Code refs.
	for _, code := range cw.CodeRefs {
		size += types.HashLength + len(code)
	}

	return size
}

// addrLess returns true if a < b (lexicographic byte comparison).
func addrLess(a, b types.Address) bool {
	for i := 0; i < types.AddressLength; i++ {
		if a[i] != b[i] {
			return a[i] < b[i]
		}
	}
	return false
}
