package vm

import (
	"math/big"
	"runtime"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/eth2028/eth2028/core/types"
)

func TestNewParallelExecutor_Defaults(t *testing.T) {
	pe := NewParallelExecutor(0)
	if pe.Workers() != runtime.NumCPU() {
		t.Fatalf("expected %d workers, got %d", runtime.NumCPU(), pe.Workers())
	}
}

func TestNewParallelExecutor_CustomWorkers(t *testing.T) {
	pe := NewParallelExecutor(4)
	if pe.Workers() != 4 {
		t.Fatalf("expected 4 workers, got %d", pe.Workers())
	}
}

func TestParallelExecutor_EmptyTxs(t *testing.T) {
	pe := NewParallelExecutor(2)
	_, err := pe.ExecuteParallel(nil, newParallelMockStateDB(), 30_000_000)
	if err != ErrNoTransactions {
		t.Fatalf("expected ErrNoTransactions, got %v", err)
	}
}

func TestParallelExecutor_NilState(t *testing.T) {
	pe := NewParallelExecutor(2)
	tx := types.NewTransaction(&types.LegacyTx{
		Gas:      21000,
		GasPrice: big.NewInt(1),
		Value:    big.NewInt(0),
	})
	_, err := pe.ExecuteParallel([]*types.Transaction{tx}, nil, 30_000_000)
	if err != ErrNilState {
		t.Fatalf("expected ErrNilState, got %v", err)
	}
}

func TestParallelExecutor_SimpleTransfers(t *testing.T) {
	pe := NewParallelExecutor(4)
	state := newParallelMockStateDB()

	txs := make([]*types.Transaction, 10)
	for i := 0; i < 10; i++ {
		to := types.BytesToAddress([]byte{byte(i + 1)})
		txs[i] = types.NewTransaction(&types.LegacyTx{
			Nonce:    uint64(i),
			Gas:      21000,
			GasPrice: big.NewInt(1),
			Value:    big.NewInt(100),
			To:       &to,
		})
	}

	receipts, err := pe.ExecuteParallel(txs, state, 30_000_000)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(receipts) != 10 {
		t.Fatalf("expected 10 receipts, got %d", len(receipts))
	}
	for i, r := range receipts {
		if r.Status != types.ReceiptStatusSuccessful {
			t.Fatalf("tx %d: expected success", i)
		}
		if r.GasUsed != 21000 {
			t.Fatalf("tx %d: expected 21000 gas, got %d", i, r.GasUsed)
		}
	}

	// Cumulative gas should be increasing.
	for i := 1; i < len(receipts); i++ {
		if receipts[i].CumulativeGasUsed <= receipts[i-1].CumulativeGasUsed {
			t.Fatalf("cumulative gas not increasing at index %d", i)
		}
	}
}

func TestParallelExecutor_WithCalldata(t *testing.T) {
	pe := NewParallelExecutor(2)
	state := newParallelMockStateDB()

	data := make([]byte, 36)
	for i := range data {
		data[i] = byte(i + 1)
	}
	to := types.HexToAddress("0xdead")
	txs := []*types.Transaction{
		types.NewTransaction(&types.LegacyTx{
			Gas:      100_000,
			GasPrice: big.NewInt(1),
			Value:    big.NewInt(0),
			To:       &to,
			Data:     data,
		}),
	}

	receipts, err := pe.ExecuteParallel(txs, state, 30_000_000)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if receipts[0].Status != types.ReceiptStatusSuccessful {
		t.Fatal("expected success")
	}
	if receipts[0].GasUsed == 0 {
		t.Fatal("gas used should be non-zero")
	}
}

func TestParallelExecutor_Counters(t *testing.T) {
	pe := NewParallelExecutor(2)
	state := newParallelMockStateDB()

	txs := make([]*types.Transaction, 5)
	for i := range txs {
		to := types.BytesToAddress([]byte{byte(i + 1)})
		txs[i] = types.NewTransaction(&types.LegacyTx{
			Gas:      21000,
			GasPrice: big.NewInt(1),
			Value:    big.NewInt(0),
			To:       &to,
		})
	}

	_, err := pe.ExecuteParallel(txs, state, 30_000_000)
	if err != nil {
		t.Fatal(err)
	}
	if pe.TotalExecuted() != 5 {
		t.Fatalf("expected 5 executed, got %d", pe.TotalExecuted())
	}
}

// --- ConflictDetector tests ---

func TestConflictDetector_NoConflict(t *testing.T) {
	cd := NewConflictDetector()

	addr1 := types.HexToAddress("0x01")
	addr2 := types.HexToAddress("0x02")
	key1 := types.HexToHash("0x10")
	key2 := types.HexToHash("0x20")

	// tx0 writes to addr1/key1, tx1 writes to addr2/key2: no conflict.
	r0 := &ParallelExecResult{
		TxIndex:  0,
		WriteSet: []StorageAccess{{Address: addr1, Key: key1, Type: AccessWrite}},
	}
	r1 := &ParallelExecResult{
		TxIndex:  1,
		WriteSet: []StorageAccess{{Address: addr2, Key: key2, Type: AccessWrite}},
	}

	conflicts := cd.DetectConflicts([]*ParallelExecResult{r0, r1})
	if len(conflicts) != 0 {
		t.Fatalf("expected no conflicts, got %d", len(conflicts))
	}
}

func TestConflictDetector_WAWConflict(t *testing.T) {
	cd := NewConflictDetector()

	addr := types.HexToAddress("0x01")
	key := types.HexToHash("0x10")

	// Both txs write the same key: WAW conflict on tx1.
	r0 := &ParallelExecResult{
		TxIndex:  0,
		WriteSet: []StorageAccess{{Address: addr, Key: key, Type: AccessWrite}},
	}
	r1 := &ParallelExecResult{
		TxIndex:  1,
		WriteSet: []StorageAccess{{Address: addr, Key: key, Type: AccessWrite}},
	}

	conflicts := cd.DetectConflicts([]*ParallelExecResult{r0, r1})
	if len(conflicts) != 1 || conflicts[0] != 1 {
		t.Fatalf("expected conflict on tx1, got %v", conflicts)
	}
}

func TestConflictDetector_RAWConflict(t *testing.T) {
	cd := NewConflictDetector()

	addr := types.HexToAddress("0x01")
	key := types.HexToHash("0x10")

	// tx0 writes key, tx1 reads same key: RAW conflict on tx1.
	r0 := &ParallelExecResult{
		TxIndex:  0,
		WriteSet: []StorageAccess{{Address: addr, Key: key, Type: AccessWrite}},
	}
	r1 := &ParallelExecResult{
		TxIndex:  1,
		ReadSet:  []StorageAccess{{Address: addr, Key: key, Type: AccessRead}},
	}

	conflicts := cd.DetectConflicts([]*ParallelExecResult{r0, r1})
	if len(conflicts) != 1 || conflicts[0] != 1 {
		t.Fatalf("expected conflict on tx1, got %v", conflicts)
	}
}

func TestConflictDetector_NoConflict_ReadOnly(t *testing.T) {
	cd := NewConflictDetector()

	addr := types.HexToAddress("0x01")
	key := types.HexToHash("0x10")

	// Both txs only read the same key: no conflict.
	r0 := &ParallelExecResult{
		TxIndex: 0,
		ReadSet: []StorageAccess{{Address: addr, Key: key, Type: AccessRead}},
	}
	r1 := &ParallelExecResult{
		TxIndex: 1,
		ReadSet: []StorageAccess{{Address: addr, Key: key, Type: AccessRead}},
	}

	conflicts := cd.DetectConflicts([]*ParallelExecResult{r0, r1})
	if len(conflicts) != 0 {
		t.Fatalf("expected no conflicts for read-only, got %d", len(conflicts))
	}
}

func TestConflictDetector_Reset(t *testing.T) {
	cd := NewConflictDetector()
	addr := types.HexToAddress("0x01")
	key := types.HexToHash("0x10")

	cd.RecordWrite(addr, key, 0)
	cd.Reset()

	// After reset, no data should remain.
	conflicts := cd.DetectConflicts([]*ParallelExecResult{
		{TxIndex: 0, WriteSet: []StorageAccess{{Address: addr, Key: key, Type: AccessWrite}}},
	})
	if len(conflicts) != 0 {
		t.Fatalf("expected no conflicts after reset, got %d", len(conflicts))
	}
}

// --- WorkStealQueue tests ---

func TestWorkStealQueue_PushPop(t *testing.T) {
	q := newWorkStealQueue()
	q.Push(workItem{txIndex: 0})
	q.Push(workItem{txIndex: 1})
	q.Push(workItem{txIndex: 2})

	if q.Len() != 3 {
		t.Fatalf("expected len 3, got %d", q.Len())
	}

	// Pop returns from the back (LIFO).
	item, ok := q.Pop()
	if !ok || item.txIndex != 2 {
		t.Fatalf("expected txIndex 2, got %d", item.txIndex)
	}

	// Steal returns from the front (FIFO).
	item, ok = q.Steal()
	if !ok || item.txIndex != 0 {
		t.Fatalf("expected txIndex 0 from steal, got %d", item.txIndex)
	}

	if q.Len() != 1 {
		t.Fatalf("expected len 1, got %d", q.Len())
	}
}

func TestWorkStealQueue_Empty(t *testing.T) {
	q := newWorkStealQueue()
	_, ok := q.Pop()
	if ok {
		t.Fatal("expected Pop to fail on empty queue")
	}
	_, ok = q.Steal()
	if ok {
		t.Fatal("expected Steal to fail on empty queue")
	}
}

func TestWorkStealQueue_Concurrent(t *testing.T) {
	q := newWorkStealQueue()
	const n = 100
	for i := 0; i < n; i++ {
		q.Push(workItem{txIndex: i})
	}

	var wg sync.WaitGroup
	var consumed atomic.Int64

	for g := 0; g < 4; g++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				if _, ok := q.Steal(); ok {
					consumed.Add(1)
				} else if _, ok := q.Pop(); ok {
					consumed.Add(1)
				} else {
					return
				}
			}
		}()
	}
	wg.Wait()

	if consumed.Load() != n {
		t.Fatalf("expected %d consumed, got %d", n, consumed.Load())
	}
}

func TestSortInts(t *testing.T) {
	a := []int{5, 2, 8, 1, 3}
	sortInts(a)
	for i := 1; i < len(a); i++ {
		if a[i] < a[i-1] {
			t.Fatalf("not sorted at index %d: %v", i, a)
		}
	}
}

func TestSortInts_Empty(t *testing.T) {
	sortInts(nil)
	sortInts([]int{})
	sortInts([]int{42})
}

// --- ParallelExecResult tests ---

func TestParallelExecResult_Fields(t *testing.T) {
	r := &ParallelExecResult{
		TxIndex:  3,
		GasUsed:  21000,
		Conflict: false,
		ReadSet:  []StorageAccess{{Address: types.HexToAddress("0x01"), Key: types.HexToHash("0x10"), Type: AccessRead}},
		WriteSet: []StorageAccess{{Address: types.HexToAddress("0x01"), Key: types.HexToHash("0x20"), Type: AccessWrite}},
	}
	if r.TxIndex != 3 {
		t.Fatal("wrong tx index")
	}
	if len(r.ReadSet) != 1 || len(r.WriteSet) != 1 {
		t.Fatal("wrong access set lengths")
	}
}

func TestParallelExecutor_ConflictingTxs(t *testing.T) {
	pe := NewParallelExecutor(2)
	state := newParallelMockStateDB()

	// Two transactions targeting the same contract with the same calldata
	// will produce the same storage write, causing a WAW conflict.
	to := types.HexToAddress("0xdead")
	data := []byte{0xaa, 0xbb, 0xcc, 0xdd}

	txs := []*types.Transaction{
		types.NewTransaction(&types.LegacyTx{
			Gas: 100_000, GasPrice: big.NewInt(1), To: &to, Data: data,
		}),
		types.NewTransaction(&types.LegacyTx{
			Gas: 100_000, GasPrice: big.NewInt(1), To: &to, Data: data,
		}),
	}

	receipts, err := pe.ExecuteParallel(txs, state, 30_000_000)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(receipts) != 2 {
		t.Fatalf("expected 2 receipts, got %d", len(receipts))
	}
	// Both should succeed (re-execution handles conflicts).
	for i, r := range receipts {
		if r.Status != types.ReceiptStatusSuccessful {
			t.Fatalf("tx %d: expected success", i)
		}
	}
}

// parallelMockStateDB is a minimal StateDB implementation for parallel executor tests.
type parallelMockStateDB struct {
	mu       sync.Mutex
	storage  map[storageKey]types.Hash
	balances map[types.Address]*big.Int
}

func newParallelMockStateDB() *parallelMockStateDB {
	return &parallelMockStateDB{
		storage:  make(map[storageKey]types.Hash),
		balances: make(map[types.Address]*big.Int),
	}
}

func (m *parallelMockStateDB) CreateAccount(addr types.Address)             {}
func (m *parallelMockStateDB) GetBalance(addr types.Address) *big.Int       { return big.NewInt(1000000) }
func (m *parallelMockStateDB) AddBalance(addr types.Address, amount *big.Int) {}
func (m *parallelMockStateDB) SubBalance(addr types.Address, amount *big.Int) {}
func (m *parallelMockStateDB) GetNonce(addr types.Address) uint64           { return 0 }
func (m *parallelMockStateDB) SetNonce(addr types.Address, nonce uint64)    {}
func (m *parallelMockStateDB) GetCode(addr types.Address) []byte            { return nil }
func (m *parallelMockStateDB) SetCode(addr types.Address, code []byte)      {}
func (m *parallelMockStateDB) GetCodeHash(addr types.Address) types.Hash    { return types.Hash{} }
func (m *parallelMockStateDB) GetCodeSize(addr types.Address) int           { return 0 }
func (m *parallelMockStateDB) GetState(addr types.Address, key types.Hash) types.Hash {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.storage[storageKey{addr: addr, key: key}]
}
func (m *parallelMockStateDB) SetState(addr types.Address, key types.Hash, value types.Hash) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.storage[storageKey{addr: addr, key: key}] = value
}
func (m *parallelMockStateDB) GetCommittedState(addr types.Address, key types.Hash) types.Hash {
	return types.Hash{}
}
func (m *parallelMockStateDB) GetTransientState(addr types.Address, key types.Hash) types.Hash {
	return types.Hash{}
}
func (m *parallelMockStateDB) SetTransientState(addr types.Address, key types.Hash, value types.Hash) {}
func (m *parallelMockStateDB) ClearTransientStorage()                                                   {}
func (m *parallelMockStateDB) SelfDestruct(addr types.Address)                                          {}
func (m *parallelMockStateDB) HasSelfDestructed(addr types.Address) bool                                { return false }
func (m *parallelMockStateDB) Exist(addr types.Address) bool                                            { return true }
func (m *parallelMockStateDB) Empty(addr types.Address) bool                                            { return false }
func (m *parallelMockStateDB) Snapshot() int                                                            { return 0 }
func (m *parallelMockStateDB) RevertToSnapshot(id int)                                                  {}
func (m *parallelMockStateDB) AddLog(log *types.Log)                                                    {}
func (m *parallelMockStateDB) AddRefund(gas uint64)                                                     {}
func (m *parallelMockStateDB) SubRefund(gas uint64)                                                     {}
func (m *parallelMockStateDB) GetRefund() uint64                                                        { return 0 }
func (m *parallelMockStateDB) AddAddressToAccessList(addr types.Address)                                {}
func (m *parallelMockStateDB) AddSlotToAccessList(addr types.Address, slot types.Hash)                  {}
func (m *parallelMockStateDB) AddressInAccessList(addr types.Address) bool                              { return false }
func (m *parallelMockStateDB) SlotInAccessList(addr types.Address, slot types.Hash) (bool, bool)        { return false, false }

var _ StateDB = (*parallelMockStateDB)(nil)
