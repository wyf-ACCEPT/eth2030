package vm

import (
	"math/big"
	"testing"

	"github.com/eth2030/eth2030/core/types"
	"github.com/eth2030/eth2030/crypto"
)

// createTestStateDB returns a mockStateDB with enhanced functionality for
// CREATE/CREATE2 testing, including nonce, balance, and code tracking.
type createTestStateDB struct {
	mockStateDB
	nonces   map[types.Address]uint64
	balances map[types.Address]*big.Int
	codes    map[types.Address][]byte
	codeh    map[types.Address]types.Hash
	accounts map[types.Address]bool
	snapID   int
}

func newCreateTestStateDB() *createTestStateDB {
	return &createTestStateDB{
		mockStateDB: *newMockStateDB(),
		nonces:      make(map[types.Address]uint64),
		balances:    make(map[types.Address]*big.Int),
		codes:       make(map[types.Address][]byte),
		codeh:       make(map[types.Address]types.Hash),
		accounts:    make(map[types.Address]bool),
	}
}

func (s *createTestStateDB) GetNonce(addr types.Address) uint64 {
	return s.nonces[addr]
}
func (s *createTestStateDB) SetNonce(addr types.Address, n uint64) {
	s.nonces[addr] = n
}
func (s *createTestStateDB) GetBalance(addr types.Address) *big.Int {
	if b, ok := s.balances[addr]; ok {
		return new(big.Int).Set(b)
	}
	return new(big.Int)
}
func (s *createTestStateDB) AddBalance(addr types.Address, amount *big.Int) {
	if _, ok := s.balances[addr]; !ok {
		s.balances[addr] = new(big.Int)
	}
	s.balances[addr].Add(s.balances[addr], amount)
}
func (s *createTestStateDB) SubBalance(addr types.Address, amount *big.Int) {
	if _, ok := s.balances[addr]; !ok {
		s.balances[addr] = new(big.Int)
	}
	s.balances[addr].Sub(s.balances[addr], amount)
}
func (s *createTestStateDB) GetCode(addr types.Address) []byte {
	return s.codes[addr]
}
func (s *createTestStateDB) SetCode(addr types.Address, code []byte) {
	s.codes[addr] = code
	if len(code) > 0 {
		s.codeh[addr] = crypto.Keccak256Hash(code)
	} else {
		s.codeh[addr] = types.EmptyCodeHash
	}
}
func (s *createTestStateDB) GetCodeHash(addr types.Address) types.Hash {
	if h, ok := s.codeh[addr]; ok {
		return h
	}
	return types.Hash{}
}
func (s *createTestStateDB) GetCodeSize(addr types.Address) int {
	return len(s.codes[addr])
}
func (s *createTestStateDB) CreateAccount(addr types.Address) {
	s.accounts[addr] = true
}
func (s *createTestStateDB) Exist(addr types.Address) bool {
	return s.accounts[addr]
}
func (s *createTestStateDB) Empty(addr types.Address) bool {
	return !s.accounts[addr]
}
func (s *createTestStateDB) Snapshot() int {
	s.snapID++
	return s.snapID
}
func (s *createTestStateDB) RevertToSnapshot(int) {}

func TestCreateExecutorValidateInitCode(t *testing.T) {
	rules := ForkRules{IsCancun: true}
	ce := NewCreateExecutor(rules)

	// Valid init code under the limit.
	small := make([]byte, 100)
	if err := ce.ValidateInitCode(small); err != nil {
		t.Fatalf("expected no error for small init code, got %v", err)
	}

	// Init code exactly at the limit.
	exact := make([]byte, MaxInitCodeSize)
	if err := ce.ValidateInitCode(exact); err != nil {
		t.Fatalf("expected no error for init code at limit, got %v", err)
	}

	// Init code exceeding the limit.
	oversize := make([]byte, MaxInitCodeSize+1)
	if err := ce.ValidateInitCode(oversize); err == nil {
		t.Fatal("expected error for oversized init code")
	}
}

func TestCreateExecutorValidateInitCodeGlamsterdam(t *testing.T) {
	rules := ForkRules{IsGlamsterdan: true, IsEIP7954: true}
	ce := NewCreateExecutor(rules)

	// Valid under Glamsterdam limits.
	ok := make([]byte, MaxInitCodeSizeGlamsterdam)
	if err := ce.ValidateInitCode(ok); err != nil {
		t.Fatalf("expected no error at Glamsterdam limit, got %v", err)
	}

	over := make([]byte, MaxInitCodeSizeGlamsterdam+1)
	if err := ce.ValidateInitCode(over); err == nil {
		t.Fatal("expected error for init code exceeding Glamsterdam limit")
	}
}

func TestCreateExecutorValidateDeployedCode(t *testing.T) {
	rules := ForkRules{IsCancun: true}
	ce := NewCreateExecutor(rules)

	// Valid deployed code.
	ok := make([]byte, MaxCodeSize)
	if err := ce.ValidateDeployedCode(ok); err != nil {
		t.Fatalf("expected no error at max size, got %v", err)
	}

	// Oversized deployed code.
	over := make([]byte, MaxCodeSize+1)
	if err := ce.ValidateDeployedCode(over); err == nil {
		t.Fatal("expected error for oversized deployed code")
	}
}

func TestCreateExecutorComputeAddressCREATE(t *testing.T) {
	rules := ForkRules{IsCancun: true}
	ce := NewCreateExecutor(rules)

	caller := types.HexToAddress("0xaaaa")
	params := &CreateParams{
		Kind:     CreateKindCreate,
		Caller:   caller,
		InitCode: []byte{0x60, 0x00, 0xf3},
	}

	addr0 := ce.ComputeAddress(params, 0)
	addr1 := ce.ComputeAddress(params, 1)

	if addr0.IsZero() || addr1.IsZero() {
		t.Fatal("expected non-zero addresses")
	}
	if addr0 == addr1 {
		t.Fatal("expected different addresses for different nonces")
	}

	// Determinism check.
	addr0Again := ce.ComputeAddress(params, 0)
	if addr0 != addr0Again {
		t.Fatal("CREATE address computation not deterministic")
	}
}

func TestCreateExecutorComputeAddressCREATE2(t *testing.T) {
	rules := ForkRules{IsCancun: true}
	ce := NewCreateExecutor(rules)

	caller := types.HexToAddress("0xbbbb")
	initCode := []byte{0xde, 0xad, 0xbe, 0xef}
	salt1 := big.NewInt(1)
	salt2 := big.NewInt(2)

	params1 := &CreateParams{
		Kind:     CreateKindCreate2,
		Caller:   caller,
		InitCode: initCode,
		Salt:     salt1,
	}
	params2 := &CreateParams{
		Kind:     CreateKindCreate2,
		Caller:   caller,
		InitCode: initCode,
		Salt:     salt2,
	}

	addr1 := ce.ComputeAddress(params1, 0)
	addr2 := ce.ComputeAddress(params2, 0)

	if addr1.IsZero() || addr2.IsZero() {
		t.Fatal("expected non-zero addresses")
	}
	if addr1 == addr2 {
		t.Fatal("expected different addresses for different salts")
	}
}

func TestCreateExecutorCalcCreateGas(t *testing.T) {
	rules := ForkRules{IsCancun: true}
	ce := NewCreateExecutor(rules)

	// 64 bytes init code = 2 words.
	params := &CreateParams{
		Kind:     CreateKindCreate,
		InitCode: make([]byte, 64),
	}

	gas := ce.CalcCreateGas(params)
	// GasCreate(32000) + 2 words * InitCodeWordGas(2) = 32004
	expected := uint64(GasCreate) + 2*InitCodeWordGas
	if gas != expected {
		t.Fatalf("CREATE gas: got %d, want %d", gas, expected)
	}
}

func TestCreateExecutorCalcCreate2Gas(t *testing.T) {
	rules := ForkRules{IsCancun: true}
	ce := NewCreateExecutor(rules)

	// 64 bytes init code = 2 words.
	params := &CreateParams{
		Kind:     CreateKindCreate2,
		InitCode: make([]byte, 64),
	}

	gas := ce.CalcCreateGas(params)
	// GasCreate(32000) + 2 * (InitCodeWordGas(2) + GasKeccak256Word(6)) = 32000 + 16 = 32016
	expected := uint64(GasCreate) + 2*(InitCodeWordGas+GasKeccak256Word)
	if gas != expected {
		t.Fatalf("CREATE2 gas: got %d, want %d", gas, expected)
	}
}

func TestCreateExecutorCalcCodeDepositGas(t *testing.T) {
	rules := ForkRules{IsCancun: true}
	ce := NewCreateExecutor(rules)

	code := make([]byte, 100)
	gas := ce.CalcCodeDepositGas(code)
	expected := uint64(100) * CreateDataGas
	if gas != expected {
		t.Fatalf("code deposit gas: got %d, want %d", gas, expected)
	}
}

func TestCreateExecutorCheckCollision(t *testing.T) {
	rules := ForkRules{IsCancun: true}
	ce := NewCreateExecutor(rules)

	stateDB := newCreateTestStateDB()
	addr := types.HexToAddress("0x1234")

	// No collision on empty address.
	if err := ce.CheckCollision(stateDB, addr); err != nil {
		t.Fatalf("expected no collision on empty address, got %v", err)
	}

	// Collision on non-zero nonce.
	stateDB.nonces[addr] = 1
	if err := ce.CheckCollision(stateDB, addr); err == nil {
		t.Fatal("expected collision for non-zero nonce")
	}
}

func TestCreateExecutorCheckCollisionCode(t *testing.T) {
	rules := ForkRules{IsCancun: true}
	ce := NewCreateExecutor(rules)

	stateDB := newCreateTestStateDB()
	addr := types.HexToAddress("0x5678")

	// Set code (non-empty code hash).
	stateDB.SetCode(addr, []byte{0x60, 0x00, 0xf3})

	if err := ce.CheckCollision(stateDB, addr); err == nil {
		t.Fatal("expected collision for address with code")
	}
}

func TestCreateExecutorCheckCollisionEIP7610(t *testing.T) {
	rules := ForkRules{IsPrague: true}
	ce := NewCreateExecutor(rules)

	stateDB := newCreateTestStateDB()
	addr := types.HexToAddress("0x9abc")

	// Set non-empty storage.
	stateDB.SetState(addr, types.BytesToHash([]byte{0}), types.BytesToHash([]byte{1}))

	if err := ce.CheckCollision(stateDB, addr); err == nil {
		t.Fatal("expected collision for address with non-empty storage (EIP-7610)")
	}
}

func TestCheckNonceOverflow(t *testing.T) {
	if err := CheckNonceOverflow(0); err != nil {
		t.Fatal("expected no error for nonce 0")
	}
	if err := CheckNonceOverflow(1000); err != nil {
		t.Fatal("expected no error for nonce 1000")
	}
	if err := CheckNonceOverflow(MaxNonce); err == nil {
		t.Fatal("expected error at MaxNonce")
	}
}

func TestCreateAddressFromNonce(t *testing.T) {
	caller := types.HexToAddress("0xaaaa")
	a0 := CreateAddressFromNonce(caller, 0)
	a1 := CreateAddressFromNonce(caller, 1)

	if a0 == a1 {
		t.Fatal("expected different addresses for different nonces")
	}

	// Must match createAddress.
	expected := createAddress(caller, 0)
	if a0 != expected {
		t.Fatal("CreateAddressFromNonce does not match createAddress")
	}
}

func TestCreate2AddressFromSaltAndCode(t *testing.T) {
	caller := types.HexToAddress("0xbbbb")
	salt := big.NewInt(42)
	code := []byte{0x01, 0x02, 0x03}

	addr := Create2AddressFromSaltAndCode(caller, salt, code)
	if addr.IsZero() {
		t.Fatal("expected non-zero address")
	}

	// Determinism.
	addr2 := Create2AddressFromSaltAndCode(caller, salt, code)
	if addr != addr2 {
		t.Fatal("CREATE2 address computation is not deterministic")
	}

	// Different salt yields different address.
	addr3 := Create2AddressFromSaltAndCode(caller, big.NewInt(99), code)
	if addr == addr3 {
		t.Fatal("expected different address for different salt")
	}
}

func TestCreateKindString(t *testing.T) {
	if CreateKindCreate.String() != "CREATE" {
		t.Fatalf("expected CREATE, got %s", CreateKindCreate.String())
	}
	if CreateKindCreate2.String() != "CREATE2" {
		t.Fatalf("expected CREATE2, got %s", CreateKindCreate2.String())
	}
}

func TestCreateExecutorNilStateDB(t *testing.T) {
	rules := ForkRules{IsCancun: true}
	ce := NewCreateExecutor(rules)

	// CheckCollision with nil stateDB should return nil.
	if err := ce.CheckCollision(nil, types.Address{}); err != nil {
		t.Fatalf("expected no error with nil stateDB, got %v", err)
	}
}

func TestCreateExecutorEmptyInitCode(t *testing.T) {
	rules := ForkRules{IsCancun: true}
	ce := NewCreateExecutor(rules)

	params := &CreateParams{
		Kind:     CreateKindCreate,
		InitCode: nil,
	}

	gas := ce.CalcCreateGas(params)
	if gas != uint64(GasCreate) {
		t.Fatalf("expected base gas only for empty init code, got %d", gas)
	}
}

func TestMaxNonceValue(t *testing.T) {
	// MaxNonce should be 2^64 - 2 per EIP-2681.
	expected := ^uint64(0) - 1
	if MaxNonce != expected {
		t.Fatalf("MaxNonce = %d, want %d", MaxNonce, expected)
	}
}
