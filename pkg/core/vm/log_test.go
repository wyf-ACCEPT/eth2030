package vm

import (
	"math/big"
	"testing"

	"github.com/eth2028/eth2028/core/types"
)

// logCaptureMockState extends the basic mockStateDB with log capture support.
type logCaptureMockState struct {
	mockStateDB
	logs []*types.Log
}

func newLogCaptureMock() *logCaptureMockState {
	return &logCaptureMockState{
		mockStateDB: *newMockStateDB(),
	}
}

func (m *logCaptureMockState) AddLog(log *types.Log) {
	m.logs = append(m.logs, log)
}

func (m *logCaptureMockState) Exist(types.Address) bool { return true }
func (m *logCaptureMockState) Empty(types.Address) bool { return false }

// setupLogTest creates an EVM with a log-capturing mock StateDB.
func setupLogTest() (*EVM, *Contract, *Memory, *Stack, *logCaptureMockState) {
	mock := newLogCaptureMock()
	addr := types.Address{0xCA, 0xFE}
	evm := NewEVM(BlockContext{}, TxContext{}, Config{})
	evm.StateDB = mock
	contract := NewContract(types.Address{0x01}, addr, big.NewInt(0), 10_000_000)
	mem := NewMemory()
	mem.Resize(256)
	st := NewStack()
	return evm, contract, mem, st, mock
}

// --- Direct opcode tests (LOG0-LOG4) ---

func TestMakeLogLOG0(t *testing.T) {
	evm, contract, mem, st, mock := setupLogTest()
	pc := uint64(0)

	// Write data to memory.
	mem.Set(0, 4, []byte{0xDE, 0xAD, 0xBE, 0xEF})

	// LOG0: pop offset=0, size=4; no topics.
	logFn := makeLog(0)
	st.Push(big.NewInt(4)) // size
	st.Push(big.NewInt(0)) // offset
	_, err := logFn(&pc, evm, contract, mem, st)
	if err != nil {
		t.Fatalf("LOG0 error: %v", err)
	}

	if len(mock.logs) != 1 {
		t.Fatalf("expected 1 log, got %d", len(mock.logs))
	}
	log := mock.logs[0]
	if log.Address != contract.Address {
		t.Errorf("log address = %x, want %x", log.Address, contract.Address)
	}
	if len(log.Topics) != 0 {
		t.Errorf("LOG0 topics = %d, want 0", len(log.Topics))
	}
	if len(log.Data) != 4 || log.Data[0] != 0xDE || log.Data[3] != 0xEF {
		t.Errorf("LOG0 data = %x, want deadbeef", log.Data)
	}
}

func TestMakeLogLOG1(t *testing.T) {
	evm, contract, mem, st, mock := setupLogTest()
	pc := uint64(0)

	mem.Set(0, 3, []byte{0x01, 0x02, 0x03})

	topic1 := types.HexToHash("0xaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa")

	logFn := makeLog(1)
	st.Push(new(big.Int).SetBytes(topic1[:])) // topic1
	st.Push(big.NewInt(3))                     // size
	st.Push(big.NewInt(0))                     // offset
	_, err := logFn(&pc, evm, contract, mem, st)
	if err != nil {
		t.Fatalf("LOG1 error: %v", err)
	}

	if len(mock.logs) != 1 {
		t.Fatalf("expected 1 log, got %d", len(mock.logs))
	}
	log := mock.logs[0]
	if len(log.Topics) != 1 {
		t.Fatalf("LOG1 topics = %d, want 1", len(log.Topics))
	}
	if log.Topics[0] != topic1 {
		t.Errorf("LOG1 topic[0] = %x, want %x", log.Topics[0], topic1)
	}
	if len(log.Data) != 3 || log.Data[0] != 0x01 || log.Data[2] != 0x03 {
		t.Errorf("LOG1 data = %x, want 010203", log.Data)
	}
}

func TestMakeLogLOG2(t *testing.T) {
	evm, contract, mem, st, mock := setupLogTest()
	pc := uint64(0)

	mem.Set(10, 2, []byte{0xAB, 0xCD})

	topic1 := types.HexToHash("0x1111111111111111111111111111111111111111111111111111111111111111")
	topic2 := types.HexToHash("0x2222222222222222222222222222222222222222222222222222222222222222")

	logFn := makeLog(2)
	st.Push(new(big.Int).SetBytes(topic2[:])) // topic2 (pushed first, popped second)
	st.Push(new(big.Int).SetBytes(topic1[:])) // topic1 (pushed second, popped first)
	st.Push(big.NewInt(2))                     // size
	st.Push(big.NewInt(10))                    // offset
	_, err := logFn(&pc, evm, contract, mem, st)
	if err != nil {
		t.Fatalf("LOG2 error: %v", err)
	}

	if len(mock.logs) != 1 {
		t.Fatalf("expected 1 log, got %d", len(mock.logs))
	}
	log := mock.logs[0]
	if len(log.Topics) != 2 {
		t.Fatalf("LOG2 topics = %d, want 2", len(log.Topics))
	}
	if log.Topics[0] != topic1 {
		t.Errorf("LOG2 topic[0] = %x, want %x", log.Topics[0], topic1)
	}
	if log.Topics[1] != topic2 {
		t.Errorf("LOG2 topic[1] = %x, want %x", log.Topics[1], topic2)
	}
	if len(log.Data) != 2 || log.Data[0] != 0xAB || log.Data[1] != 0xCD {
		t.Errorf("LOG2 data = %x, want abcd", log.Data)
	}
}

func TestMakeLogLOG3(t *testing.T) {
	evm, contract, mem, st, mock := setupLogTest()
	pc := uint64(0)

	mem.Set(0, 1, []byte{0xFF})

	topic1 := types.HexToHash("0x0000000000000000000000000000000000000000000000000000000000000001")
	topic2 := types.HexToHash("0x0000000000000000000000000000000000000000000000000000000000000002")
	topic3 := types.HexToHash("0x0000000000000000000000000000000000000000000000000000000000000003")

	logFn := makeLog(3)
	// Push in reverse order since stack is LIFO.
	st.Push(new(big.Int).SetBytes(topic3[:]))
	st.Push(new(big.Int).SetBytes(topic2[:]))
	st.Push(new(big.Int).SetBytes(topic1[:]))
	st.Push(big.NewInt(1)) // size
	st.Push(big.NewInt(0)) // offset
	_, err := logFn(&pc, evm, contract, mem, st)
	if err != nil {
		t.Fatalf("LOG3 error: %v", err)
	}

	if len(mock.logs) != 1 {
		t.Fatalf("expected 1 log, got %d", len(mock.logs))
	}
	log := mock.logs[0]
	if len(log.Topics) != 3 {
		t.Fatalf("LOG3 topics = %d, want 3", len(log.Topics))
	}
	if log.Topics[0] != topic1 || log.Topics[1] != topic2 || log.Topics[2] != topic3 {
		t.Errorf("LOG3 topics = %x, %x, %x", log.Topics[0], log.Topics[1], log.Topics[2])
	}
}

func TestMakeLogLOG4(t *testing.T) {
	evm, contract, mem, st, mock := setupLogTest()
	pc := uint64(0)

	mem.Set(0, 5, []byte{0x10, 0x20, 0x30, 0x40, 0x50})

	topic1 := types.HexToHash("0x0000000000000000000000000000000000000000000000000000000000000011")
	topic2 := types.HexToHash("0x0000000000000000000000000000000000000000000000000000000000000022")
	topic3 := types.HexToHash("0x0000000000000000000000000000000000000000000000000000000000000033")
	topic4 := types.HexToHash("0x0000000000000000000000000000000000000000000000000000000000000044")

	logFn := makeLog(4)
	// Push in reverse order.
	st.Push(new(big.Int).SetBytes(topic4[:]))
	st.Push(new(big.Int).SetBytes(topic3[:]))
	st.Push(new(big.Int).SetBytes(topic2[:]))
	st.Push(new(big.Int).SetBytes(topic1[:]))
	st.Push(big.NewInt(5)) // size
	st.Push(big.NewInt(0)) // offset
	_, err := logFn(&pc, evm, contract, mem, st)
	if err != nil {
		t.Fatalf("LOG4 error: %v", err)
	}

	if len(mock.logs) != 1 {
		t.Fatalf("expected 1 log, got %d", len(mock.logs))
	}
	log := mock.logs[0]
	if len(log.Topics) != 4 {
		t.Fatalf("LOG4 topics = %d, want 4", len(log.Topics))
	}
	if log.Topics[0] != topic1 || log.Topics[1] != topic2 || log.Topics[2] != topic3 || log.Topics[3] != topic4 {
		t.Errorf("LOG4 topics mismatch")
	}
	if len(log.Data) != 5 || log.Data[0] != 0x10 || log.Data[4] != 0x50 {
		t.Errorf("LOG4 data = %x, want 1020304050", log.Data)
	}
}

// --- Zero-size data log ---

func TestMakeLogZeroData(t *testing.T) {
	evm, contract, mem, st, mock := setupLogTest()
	pc := uint64(0)

	topic := types.HexToHash("0xdeaddeaddeaddeaddeaddeaddeaddeaddeaddeaddeaddeaddeaddeaddeaddead")

	logFn := makeLog(1)
	st.Push(new(big.Int).SetBytes(topic[:]))
	st.Push(big.NewInt(0)) // size = 0
	st.Push(big.NewInt(0)) // offset = 0
	_, err := logFn(&pc, evm, contract, mem, st)
	if err != nil {
		t.Fatalf("LOG1 zero data error: %v", err)
	}

	if len(mock.logs) != 1 {
		t.Fatalf("expected 1 log, got %d", len(mock.logs))
	}
	log := mock.logs[0]
	if len(log.Data) != 0 {
		t.Errorf("LOG1 zero data: data len = %d, want 0", len(log.Data))
	}
	if len(log.Topics) != 1 || log.Topics[0] != topic {
		t.Errorf("LOG1 zero data: topic mismatch")
	}
}

// --- Static call (read-only) rejection ---

func TestMakeLogWriteProtection(t *testing.T) {
	for n := 0; n <= 4; n++ {
		evm, contract, mem, st, _ := setupLogTest()
		evm.readOnly = true
		pc := uint64(0)

		logFn := makeLog(n)
		// Push the required stack items (topics + offset + size).
		for i := 0; i < n; i++ {
			st.Push(big.NewInt(0)) // topic
		}
		st.Push(big.NewInt(0)) // size
		st.Push(big.NewInt(0)) // offset

		_, err := logFn(&pc, evm, contract, mem, st)
		if err != ErrWriteProtection {
			t.Errorf("LOG%d in readOnly: got err=%v, want ErrWriteProtection", n, err)
		}
	}
}

// --- No StateDB (LOG should not panic) ---

func TestMakeLogNoStateDB(t *testing.T) {
	evm := NewEVM(BlockContext{}, TxContext{}, Config{})
	// evm.StateDB is nil
	contract := NewContract(types.Address{}, types.Address{0x01}, big.NewInt(0), 1_000_000)
	mem := NewMemory()
	mem.Resize(64)
	st := NewStack()
	pc := uint64(0)

	mem.Set(0, 4, []byte{0x01, 0x02, 0x03, 0x04})

	logFn := makeLog(0)
	st.Push(big.NewInt(4)) // size
	st.Push(big.NewInt(0)) // offset
	_, err := logFn(&pc, evm, contract, mem, st)
	if err != nil {
		t.Fatalf("LOG0 without StateDB should not error, got: %v", err)
	}
	// No panic is the success criteria.
}

// --- Gas calculation tests ---

func TestLogGasCalculation(t *testing.T) {
	tests := []struct {
		name      string
		numTopics uint64
		dataSize  uint64
		wantGas   uint64
	}{
		{"LOG0 empty", 0, 0, 375},                         // 375 base
		{"LOG0 32 bytes", 0, 32, 375 + 8*32},              // 375 + 256 = 631
		{"LOG1 empty", 1, 0, 375 + 375},                   // 375 + 375 = 750
		{"LOG1 10 bytes", 1, 10, 375 + 375 + 8*10},        // 375 + 375 + 80 = 830
		{"LOG2 empty", 2, 0, 375 + 2*375},                 // 375 + 750 = 1125
		{"LOG2 64 bytes", 2, 64, 375 + 2*375 + 8*64},      // 375 + 750 + 512 = 1637
		{"LOG3 1 byte", 3, 1, 375 + 3*375 + 8*1},          // 375 + 1125 + 8 = 1508
		{"LOG4 100 bytes", 4, 100, 375 + 4*375 + 8*100},   // 375 + 1500 + 800 = 2675
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := LogGas(tt.numTopics, tt.dataSize)
			if got != tt.wantGas {
				t.Errorf("LogGas(%d, %d) = %d, want %d", tt.numTopics, tt.dataSize, got, tt.wantGas)
			}
		})
	}
}

// TestMakeGasLogDynamic tests the dynamic gas function returned by makeGasLog.
func TestMakeGasLogDynamic(t *testing.T) {
	tests := []struct {
		name      string
		numTopics uint64
		dataSize  uint64
	}{
		{"LOG0 0 data", 0, 0},
		{"LOG0 10 data", 0, 10},
		{"LOG1 20 data", 1, 20},
		{"LOG2 32 data", 2, 32},
		{"LOG3 64 data", 3, 64},
		{"LOG4 100 data", 4, 100},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			evm := NewEVM(BlockContext{}, TxContext{}, Config{})
			contract := NewContract(types.Address{}, types.Address{}, big.NewInt(0), 10_000_000)
			mem := NewMemory()
			// Pre-expand memory so there is no memory expansion cost.
			memNeeded := tt.dataSize
			if memNeeded > 0 {
				words := (memNeeded + 31) / 32
				mem.Resize(words * 32)
			}
			st := NewStack()
			// Stack layout for LOGn: [offset, size, topic1, ..., topicN]
			// Back(0) = offset, Back(1) = size
			// The dynamic gas reads Back(1) for data size.
			for i := uint64(0); i < tt.numTopics; i++ {
				st.Push(big.NewInt(0)) // topics
			}
			st.Push(new(big.Int).SetUint64(tt.dataSize)) // size
			st.Push(big.NewInt(0))                         // offset

			gasFn := makeGasLog(tt.numTopics)
			got, _ := gasFn(evm, contract, st, mem, tt.dataSize)

			// Expected: numTopics * GasLogTopic + dataSize * GasLogData + 0 (no mem expansion)
			want := tt.numTopics*GasLogTopic + tt.dataSize*GasLogData
			if got != want {
				t.Errorf("makeGasLog(%d)(..., dataSize=%d) = %d, want %d", tt.numTopics, tt.dataSize, got, want)
			}
		})
	}
}

// --- Jump table wiring tests ---

func TestJumpTableLogOpcodes(t *testing.T) {
	tbl := NewFrontierJumpTable()

	for i := 0; i <= 4; i++ {
		op := LOG0 + OpCode(i)
		entry := tbl[op]
		if entry == nil {
			t.Fatalf("LOG%d (0x%02x) not in jump table", i, byte(op))
		}
		if entry.execute == nil {
			t.Errorf("LOG%d: execute is nil", i)
		}
		if entry.constantGas != GasLog {
			t.Errorf("LOG%d: constantGas = %d, want %d", i, entry.constantGas, GasLog)
		}
		if entry.minStack != 2+i {
			t.Errorf("LOG%d: minStack = %d, want %d", i, entry.minStack, 2+i)
		}
		if !entry.writes {
			t.Errorf("LOG%d: writes should be true", i)
		}
		if entry.memorySize == nil {
			t.Errorf("LOG%d: memorySize should not be nil", i)
		}
		if entry.dynamicGas == nil {
			t.Errorf("LOG%d: dynamicGas should not be nil", i)
		}
	}
}

// TestLogOpcodesInAllForks ensures LOG0-LOG4 are present in every fork's jump table.
func TestLogOpcodesInAllForks(t *testing.T) {
	forks := []struct {
		name string
		fn   func() JumpTable
	}{
		{"Frontier", NewFrontierJumpTable},
		{"Homestead", NewHomesteadJumpTable},
		{"Byzantium", NewByzantiumJumpTable},
		{"Constantinople", NewConstantinopleJumpTable},
		{"Istanbul", NewIstanbulJumpTable},
		{"Berlin", NewBerlinJumpTable},
		{"London", NewLondonJumpTable},
		{"Merge", NewMergeJumpTable},
		{"Shanghai", NewShanghaiJumpTable},
		{"Cancun", NewCancunJumpTable},
		{"Prague", NewPragueJumpTable},
		{"Glamsterdan", NewGlamsterdanJumpTable},
	}

	for _, fork := range forks {
		t.Run(fork.name, func(t *testing.T) {
			tbl := fork.fn()
			for i := 0; i <= 4; i++ {
				op := LOG0 + OpCode(i)
				if tbl[op] == nil || tbl[op].execute == nil {
					t.Errorf("LOG%d missing in %s jump table", i, fork.name)
				}
			}
		})
	}
}

// --- Full interpreter integration tests ---

// TestRunLOG0Bytecode tests LOG0 through the full interpreter loop.
func TestRunLOG0Bytecode(t *testing.T) {
	mock := newLogCaptureMock()
	evm := NewEVM(BlockContext{}, TxContext{}, Config{})
	evm.StateDB = mock

	initialGas := uint64(100_000)
	contract := NewContract(types.Address{0x01}, types.Address{0xCA, 0xFE}, big.NewInt(0), initialGas)

	// Bytecode: MSTORE 0xDEADBEEF at offset 0, then LOG0(offset=0, size=4)
	contract.Code = []byte{
		byte(PUSH4), 0xDE, 0xAD, 0xBE, 0xEF, // push 0xDEADBEEF
		byte(PUSH1), 0x00, // offset 0
		byte(MSTORE), // store at offset 0 (32-byte padded)
		byte(PUSH1), 0x04, // size = 4
		byte(PUSH1), 0x1c, // offset = 28 (last 4 bytes of 32-byte word)
		byte(LOG0),
		byte(STOP),
	}

	ret, err := evm.Run(contract, nil)
	if err != nil {
		t.Fatalf("Run error: %v", err)
	}
	if ret != nil {
		t.Errorf("expected nil return, got %x", ret)
	}

	if len(mock.logs) != 1 {
		t.Fatalf("expected 1 log, got %d", len(mock.logs))
	}
	log := mock.logs[0]
	if log.Address != contract.Address {
		t.Errorf("log address = %x, want %x", log.Address, contract.Address)
	}
	if len(log.Topics) != 0 {
		t.Errorf("LOG0 topics = %d, want 0", len(log.Topics))
	}
	if len(log.Data) != 4 || log.Data[0] != 0xDE || log.Data[1] != 0xAD || log.Data[2] != 0xBE || log.Data[3] != 0xEF {
		t.Errorf("LOG0 data = %x, want deadbeef", log.Data)
	}
}

// TestRunLOG1Bytecode tests LOG1 through the full interpreter loop.
func TestRunLOG1Bytecode(t *testing.T) {
	mock := newLogCaptureMock()
	evm := NewEVM(BlockContext{}, TxContext{}, Config{})
	evm.StateDB = mock

	contract := NewContract(types.Address{0x01}, types.Address{0xBB}, big.NewInt(0), 100_000)

	// Store 2 bytes [0xAB, 0xCD] at memory offset 0, then LOG1 with topic.
	contract.Code = []byte{
		byte(PUSH2), 0xAB, 0xCD, // push 0xABCD
		byte(PUSH1), 0x00, // offset 0
		byte(MSTORE), // store at offset 0 (padded to 32 bytes)
		byte(PUSH32), // topic1
		0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
		0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
		0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
		0x00, 0x00, 0x00, 0x00, 0xDE, 0xAD, 0xBE, 0xEF,
		byte(PUSH1), 0x02, // size = 2
		byte(PUSH1), 0x1e, // offset = 30 (last 2 bytes of the 32-byte word)
		byte(LOG1),
		byte(STOP),
	}

	_, err := evm.Run(contract, nil)
	if err != nil {
		t.Fatalf("Run error: %v", err)
	}

	if len(mock.logs) != 1 {
		t.Fatalf("expected 1 log, got %d", len(mock.logs))
	}
	log := mock.logs[0]
	if len(log.Topics) != 1 {
		t.Fatalf("LOG1 topics = %d, want 1", len(log.Topics))
	}
	expectedTopic := types.HexToHash("0x00000000000000000000000000000000000000000000000000000000deadbeef")
	if log.Topics[0] != expectedTopic {
		t.Errorf("LOG1 topic = %x, want %x", log.Topics[0], expectedTopic)
	}
	if len(log.Data) != 2 || log.Data[0] != 0xAB || log.Data[1] != 0xCD {
		t.Errorf("LOG1 data = %x, want abcd", log.Data)
	}
}

// TestRunLOG2Bytecode tests LOG2 through the full interpreter loop.
func TestRunLOG2Bytecode(t *testing.T) {
	mock := newLogCaptureMock()
	evm := NewEVM(BlockContext{}, TxContext{}, Config{})
	evm.StateDB = mock

	contract := NewContract(types.Address{0x01}, types.Address{0xCC}, big.NewInt(0), 100_000)

	// LOG2 with 0 data bytes, 2 topics.
	contract.Code = []byte{
		byte(PUSH1), 0x02, // topic2
		byte(PUSH1), 0x01, // topic1
		byte(PUSH1), 0x00, // size = 0
		byte(PUSH1), 0x00, // offset = 0
		byte(LOG2),
		byte(STOP),
	}

	_, err := evm.Run(contract, nil)
	if err != nil {
		t.Fatalf("Run error: %v", err)
	}

	if len(mock.logs) != 1 {
		t.Fatalf("expected 1 log, got %d", len(mock.logs))
	}
	log := mock.logs[0]
	if len(log.Topics) != 2 {
		t.Fatalf("LOG2 topics = %d, want 2", len(log.Topics))
	}
	expectedTopic1 := types.HexToHash("0x0000000000000000000000000000000000000000000000000000000000000001")
	expectedTopic2 := types.HexToHash("0x0000000000000000000000000000000000000000000000000000000000000002")
	if log.Topics[0] != expectedTopic1 {
		t.Errorf("LOG2 topic[0] = %x, want %x", log.Topics[0], expectedTopic1)
	}
	if log.Topics[1] != expectedTopic2 {
		t.Errorf("LOG2 topic[1] = %x, want %x", log.Topics[1], expectedTopic2)
	}
	if len(log.Data) != 0 {
		t.Errorf("LOG2 data len = %d, want 0", len(log.Data))
	}
}

// TestRunLOG4Bytecode tests LOG4 through the full interpreter loop.
func TestRunLOG4Bytecode(t *testing.T) {
	mock := newLogCaptureMock()
	evm := NewEVM(BlockContext{}, TxContext{}, Config{})
	evm.StateDB = mock

	contract := NewContract(types.Address{0x01}, types.Address{0xDD}, big.NewInt(0), 100_000)

	// LOG4 with 1 byte of data and 4 topics.
	contract.Code = []byte{
		byte(PUSH1), 0xFF, // value to store
		byte(PUSH1), 0x00, // offset 0
		byte(MSTORE8), // store single byte at offset 0
		byte(PUSH1), 0x04, // topic4
		byte(PUSH1), 0x03, // topic3
		byte(PUSH1), 0x02, // topic2
		byte(PUSH1), 0x01, // topic1
		byte(PUSH1), 0x01, // size = 1
		byte(PUSH1), 0x00, // offset = 0
		byte(LOG4),
		byte(STOP),
	}

	_, err := evm.Run(contract, nil)
	if err != nil {
		t.Fatalf("Run error: %v", err)
	}

	if len(mock.logs) != 1 {
		t.Fatalf("expected 1 log, got %d", len(mock.logs))
	}
	log := mock.logs[0]
	if len(log.Topics) != 4 {
		t.Fatalf("LOG4 topics = %d, want 4", len(log.Topics))
	}
	for i := 0; i < 4; i++ {
		expected := types.HexToHash("0x000000000000000000000000000000000000000000000000000000000000000" + string(rune('1'+i)))
		if log.Topics[i] != expected {
			t.Errorf("LOG4 topic[%d] = %x, want %x", i, log.Topics[i], expected)
		}
	}
	if len(log.Data) != 1 || log.Data[0] != 0xFF {
		t.Errorf("LOG4 data = %x, want ff", log.Data)
	}
}

// TestRunLOGStaticCallRejection verifies that LOG opcodes fail in STATICCALL context.
func TestRunLOGStaticCallRejection(t *testing.T) {
	for i := 0; i <= 4; i++ {
		t.Run("LOG"+string(rune('0'+i)), func(t *testing.T) {
			mock := newLogCaptureMock()
			evm := NewEVM(BlockContext{}, TxContext{}, Config{})
			evm.StateDB = mock

			// Build bytecode that does LOGn.
			logOp := byte(LOG0) + byte(i)
			var code []byte
			// Push topics in reverse (they'll be popped in order).
			for j := 0; j < i; j++ {
				code = append(code, byte(PUSH1), byte(j+1)) // topic
			}
			code = append(code, byte(PUSH1), 0x00) // size = 0
			code = append(code, byte(PUSH1), 0x00) // offset = 0
			code = append(code, logOp)
			code = append(code, byte(STOP))

			// Deploy the code to an address.
			contractAddr := types.Address{0xAA}
			mock.mockStateDB.storage = make(map[types.Address]map[types.Hash]types.Hash)
			// We need SetCode on the mock. Let's use the EVM's StaticCall directly.
			// StaticCall sets readOnly=true, so LOG should fail.

			// For simplicity, execute in readOnly mode.
			evm.readOnly = true
			contract := NewContract(types.Address{0x01}, contractAddr, big.NewInt(0), 100_000)
			contract.Code = code

			_, err := evm.Run(contract, nil)
			if err != ErrWriteProtection {
				t.Errorf("LOG%d in STATICCALL: got err=%v, want ErrWriteProtection", i, err)
			}

			// No logs should have been added.
			if len(mock.logs) != 0 {
				t.Errorf("LOG%d in STATICCALL: %d logs captured, want 0", i, len(mock.logs))
			}
		})
	}
}

// TestRunLOGGasAccounting tests that LOG opcodes consume the correct gas
// through the full interpreter loop.
func TestRunLOGGasAccounting(t *testing.T) {
	tests := []struct {
		name     string
		logOp    byte
		nTopics  int
		dataSize int
	}{
		{"LOG0 0 data", byte(LOG0), 0, 0},
		{"LOG0 4 data", byte(LOG0), 0, 4},
		{"LOG1 0 data", byte(LOG1), 1, 0},
		{"LOG2 10 data", byte(LOG2), 2, 10},
		{"LOG4 32 data", byte(LOG4), 4, 32},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mock := newLogCaptureMock()
			evm := NewEVM(BlockContext{}, TxContext{}, Config{})
			evm.StateDB = mock

			initialGas := uint64(1_000_000)
			contract := NewContract(types.Address{0x01}, types.Address{0xEE}, big.NewInt(0), initialGas)

			// Build bytecode:
			// PUSH1 <data_byte>, PUSH1 0, MSTORE8   (store data bytes)
			// ... repeat for each byte
			// PUSH1 <topic_value> ... (for each topic)
			// PUSH1 <size>, PUSH1 <offset>, LOGn, STOP
			var code []byte

			// Store data bytes at memory offset 0.
			for j := 0; j < tt.dataSize; j++ {
				code = append(code, byte(PUSH1), byte(j+1))    // value
				code = append(code, byte(PUSH1), byte(j))      // offset
				code = append(code, byte(MSTORE8))
			}

			// Push topics.
			for j := 0; j < tt.nTopics; j++ {
				code = append(code, byte(PUSH1), byte(j+0x10)) // topic values
			}
			// Push size and offset.
			code = append(code, byte(PUSH1), byte(tt.dataSize)) // size
			code = append(code, byte(PUSH1), 0x00)               // offset
			code = append(code, tt.logOp)
			code = append(code, byte(STOP))

			contract.Code = code

			_, err := evm.Run(contract, nil)
			if err != nil {
				t.Fatalf("Run error: %v", err)
			}

			gasUsed := initialGas - contract.Gas

			// Calculate expected gas:
			// MSTORE8 setup: per byte: PUSH1(3) + PUSH1(3) + MSTORE8(3) = 9 per byte
			// + memory expansion for first MSTORE8 if dataSize > 0
			// Topic PUSHes: nTopics * PUSH1(3)
			// Size PUSH1(3) + Offset PUSH1(3)
			// LOGn: constantGas(375) + dynamicGas(nTopics*375 + dataSize*8)
			// STOP: 0

			// For memory expansion: first MSTORE8 at offset 0 expands mem from 0 to 32 bytes = 3 gas.
			var memExpansionGas uint64
			if tt.dataSize > 0 {
				// Memory needed: at least 1 byte, rounded to 32.
				neededWords := (uint64(tt.dataSize) + 31) / 32
				neededSize := neededWords * 32
				memExpansionGas, _ = MemoryCost(0, neededSize)
			}

			setupGas := uint64(tt.dataSize) * (GasPush + GasPush + GasMstore8) // MSTORE8 ops
			// Subsequent MSTORE8s after the first have 0 additional mem expansion since
			// memory is already expanded. But the MSTORE8 memorySize function returns
			// offset + 1, which would be within the already-allocated range after the first.
			// Actually, the first MSTORE8 at offset 0 allocates 32 bytes; subsequent ones
			// at offset 1,2... up to 31 are all within that range. So mem expansion
			// is only charged once.

			topicPushGas := uint64(tt.nTopics) * GasPush
			sizePushGas := GasPush      // PUSH1 for size
			offsetPushGas := GasPush    // PUSH1 for offset
			logConstant := GasLog       // 375
			logDynamic := uint64(tt.nTopics)*GasLogTopic + uint64(tt.dataSize)*GasLogData

			// LOG memory expansion: if dataSize > 0, the LOG instruction reads from
			// already-allocated memory, so its memorySize should not trigger additional
			// expansion. But we need to account for it in the interpreter's memorySize check.
			// The memory was already expanded by MSTORE8, so LOG's mem expansion is 0.

			expectedGas := setupGas + memExpansionGas + topicPushGas + sizePushGas + offsetPushGas + logConstant + logDynamic

			if gasUsed != expectedGas {
				t.Errorf("gas used = %d, want %d (setup=%d, memExp=%d, topicPush=%d, sizeOffset=%d, logConst=%d, logDyn=%d)",
					gasUsed, expectedGas, setupGas, memExpansionGas, topicPushGas, sizePushGas+offsetPushGas, logConstant, logDynamic)
			}
		})
	}
}

// TestRunMultipleLogs tests that multiple LOG operations accumulate logs correctly.
func TestRunMultipleLogs(t *testing.T) {
	mock := newLogCaptureMock()
	evm := NewEVM(BlockContext{}, TxContext{}, Config{})
	evm.StateDB = mock

	contract := NewContract(types.Address{0x01}, types.Address{0xFF}, big.NewInt(0), 100_000)

	// Three LOG0 calls with different data.
	contract.Code = []byte{
		// Store 0xAA at offset 0.
		byte(PUSH1), 0xAA,
		byte(PUSH1), 0x00,
		byte(MSTORE8),
		// LOG0(offset=0, size=1)
		byte(PUSH1), 0x01,
		byte(PUSH1), 0x00,
		byte(LOG0),
		// Store 0xBB at offset 1.
		byte(PUSH1), 0xBB,
		byte(PUSH1), 0x01,
		byte(MSTORE8),
		// LOG0(offset=1, size=1)
		byte(PUSH1), 0x01,
		byte(PUSH1), 0x01,
		byte(LOG0),
		// Store 0xCC at offset 2.
		byte(PUSH1), 0xCC,
		byte(PUSH1), 0x02,
		byte(MSTORE8),
		// LOG0(offset=2, size=1)
		byte(PUSH1), 0x01,
		byte(PUSH1), 0x02,
		byte(LOG0),
		byte(STOP),
	}

	_, err := evm.Run(contract, nil)
	if err != nil {
		t.Fatalf("Run error: %v", err)
	}

	if len(mock.logs) != 3 {
		t.Fatalf("expected 3 logs, got %d", len(mock.logs))
	}
	expectedData := []byte{0xAA, 0xBB, 0xCC}
	for i, log := range mock.logs {
		if len(log.Data) != 1 || log.Data[0] != expectedData[i] {
			t.Errorf("log[%d] data = %x, want %x", i, log.Data, expectedData[i:i+1])
		}
	}
}

// TestRunLOGOutOfGas tests that LOG fails with ErrOutOfGas when gas is insufficient.
func TestRunLOGOutOfGas(t *testing.T) {
	mock := newLogCaptureMock()
	evm := NewEVM(BlockContext{}, TxContext{}, Config{})
	evm.StateDB = mock

	// LOG0 with 0 data costs: 375 (constant) + 0 (dynamic) = 375 gas total.
	// Give just enough for the PUSH instructions but not the LOG.
	// PUSH1(3) + PUSH1(3) = 6 gas for setup.
	// LOG0 constant gas = 375, so 6 + 374 = 380 is not enough.
	contract := NewContract(types.Address{0x01}, types.Address{0xEE}, big.NewInt(0), 380)

	contract.Code = []byte{
		byte(PUSH1), 0x00, // size = 0
		byte(PUSH1), 0x00, // offset = 0
		byte(LOG0),
		byte(STOP),
	}

	_, err := evm.Run(contract, nil)
	if err != ErrOutOfGas {
		t.Errorf("expected ErrOutOfGas, got %v", err)
	}

	// No logs should be recorded.
	if len(mock.logs) != 0 {
		t.Errorf("expected 0 logs on OOG, got %d", len(mock.logs))
	}
}

// TestRunLOGOutOfGasDynamic tests that LOG fails when dynamic gas exceeds budget.
func TestRunLOGOutOfGasDynamic(t *testing.T) {
	mock := newLogCaptureMock()
	evm := NewEVM(BlockContext{}, TxContext{}, Config{})
	evm.StateDB = mock

	// LOG4 with 100 bytes data:
	// constant: 375
	// dynamic: 4*375 + 100*8 = 1500 + 800 = 2300
	// + memory expansion for 100 bytes (4 words = 128 bytes: 4*3 + 16/512 = 12)
	// Total: 375 + 2300 + 12 = 2687
	// We also need gas for the PUSH instructions and MSTORE8 calls.
	// This is complex, so let's use a simpler case:
	// LOG1 with 0 data: constant 375 + dynamic 375 = 750 total.
	// Give 6 (PUSH+PUSH) + 3 (PUSH for topic) + 375 (constant) + 374 (not enough for dynamic) = 758.

	contract := NewContract(types.Address{0x01}, types.Address{0xEE}, big.NewInt(0), 758)

	contract.Code = []byte{
		byte(PUSH1), 0xAA, // topic1
		byte(PUSH1), 0x00, // size = 0
		byte(PUSH1), 0x00, // offset = 0
		byte(LOG1),
		byte(STOP),
	}

	_, err := evm.Run(contract, nil)
	if err != ErrOutOfGas {
		t.Errorf("expected ErrOutOfGas for dynamic gas, got %v", err)
	}
}

// TestMemoryLogFunction tests the memoryLog function used for memory size calculation.
func TestMemoryLogFunction(t *testing.T) {
	st := NewStack()

	// Stack layout: [..., offset, size, topics...]
	// memoryLog reads Back(0) for offset and Back(1) for size.
	st.Push(big.NewInt(100)) // size (will be Back(1))
	st.Push(big.NewInt(32))  // offset (will be Back(0))

	got, overflow := memoryLog(st)
	if overflow {
		t.Fatalf("memoryLog overflowed unexpectedly")
	}
	want := uint64(32 + 100) // offset + size
	if got != want {
		t.Errorf("memoryLog = %d, want %d", got, want)
	}
}

// TestMemoryLogFunctionZero tests memoryLog with zero size.
func TestMemoryLogFunctionZero(t *testing.T) {
	st := NewStack()

	st.Push(big.NewInt(0))  // size
	st.Push(big.NewInt(64)) // offset

	got, overflow := memoryLog(st)
	if overflow {
		t.Fatalf("memoryLog overflowed unexpectedly")
	}
	want := uint64(64) // offset + 0 = 64
	if got != want {
		t.Errorf("memoryLog(0 size) = %d, want %d", got, want)
	}
}
