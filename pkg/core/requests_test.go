package core

import (
	"math/big"
	"testing"

	"github.com/eth2030/eth2030/core/state"
	"github.com/eth2030/eth2030/core/types"
)

// praguePragueConfig returns a config where Prague is active at time 0.
func pragueConfig() *ChainConfig {
	return TestConfig
}

// prePragueConfig returns a config where Prague is NOT active.
func prePragueConfig() *ChainConfig {
	return &ChainConfig{
		ChainID:                 big.NewInt(1337),
		HomesteadBlock:          big.NewInt(0),
		EIP150Block:             big.NewInt(0),
		EIP155Block:             big.NewInt(0),
		EIP158Block:             big.NewInt(0),
		ByzantiumBlock:          big.NewInt(0),
		ConstantinopleBlock:     big.NewInt(0),
		PetersburgBlock:         big.NewInt(0),
		IstanbulBlock:           big.NewInt(0),
		BerlinBlock:             big.NewInt(0),
		LondonBlock:             big.NewInt(0),
		TerminalTotalDifficulty: big.NewInt(0),
		ShanghaiTime:            newUint64(0),
		CancunTime:              newUint64(0),
		PragueTime:              nil, // Prague not active
	}
}

// setupSystemContract creates a system contract account in statedb with
// pending requests stored in well-known storage slots.
func setupSystemContract(statedb *state.MemoryStateDB, addr types.Address, requestData []types.Hash) {
	statedb.CreateAccount(addr)
	statedb.SetCode(addr, []byte{0x00}) // minimal code so Exist returns true

	// Store request count at slot 0.
	count := uint64(len(requestData))
	var countVal types.Hash
	countVal[31] = byte(count & 0xFF)
	countVal[30] = byte((count >> 8) & 0xFF)
	statedb.SetState(addr, requestCountSlot, countVal)

	// Store each request at consecutive slots starting from slot 1.
	for i, data := range requestData {
		slot := incrementSlot(requestDataSlotBase, uint64(i))
		statedb.SetState(addr, slot, data)
	}
}

func TestProcessRequests_PrePrague_ReturnsNil(t *testing.T) {
	config := prePragueConfig()
	statedb := state.NewMemoryStateDB()
	header := &types.Header{
		Number:   big.NewInt(1),
		GasLimit: 10_000_000,
		Time:     1000,
	}

	requests, err := ProcessRequests(config, statedb, header)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if requests != nil {
		t.Fatalf("expected nil requests pre-Prague, got %d", len(requests))
	}
}

func TestProcessRequests_NilConfig_ReturnsNil(t *testing.T) {
	statedb := state.NewMemoryStateDB()
	header := &types.Header{
		Number:   big.NewInt(1),
		GasLimit: 10_000_000,
		Time:     1000,
	}

	requests, err := ProcessRequests(nil, statedb, header)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if requests != nil {
		t.Fatalf("expected nil requests with nil config, got %d", len(requests))
	}
}

func TestProcessRequests_PostPrague_NoContracts(t *testing.T) {
	config := pragueConfig()
	statedb := state.NewMemoryStateDB()
	header := &types.Header{
		Number:   big.NewInt(1),
		GasLimit: 10_000_000,
		Time:     1000,
	}

	// No system contracts deployed - should return empty requests.
	requests, err := ProcessRequests(config, statedb, header)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(requests) != 0 {
		t.Fatalf("expected 0 requests when no contracts exist, got %d", len(requests))
	}
}

func TestProcessRequests_DepositRequests(t *testing.T) {
	config := pragueConfig()
	statedb := state.NewMemoryStateDB()
	header := &types.Header{
		Number:   big.NewInt(1),
		GasLimit: 10_000_000,
		Time:     1000,
	}

	// Set up deposit contract with 2 requests.
	var req1, req2 types.Hash
	req1[0] = 0xAA
	req1[1] = 0xBB
	req2[0] = 0xCC
	req2[1] = 0xDD
	setupSystemContract(statedb, types.DepositContractAddress, []types.Hash{req1, req2})

	requests, err := ProcessRequests(config, statedb, header)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Filter deposit requests.
	deposits := requests.FilterByType(types.DepositRequestType)
	if len(deposits) != 2 {
		t.Fatalf("expected 2 deposit requests, got %d", len(deposits))
	}
	if deposits[0].Data[0] != 0xAA || deposits[0].Data[1] != 0xBB {
		t.Fatalf("deposit request 0 data mismatch: %x", deposits[0].Data)
	}
	if deposits[1].Data[0] != 0xCC || deposits[1].Data[1] != 0xDD {
		t.Fatalf("deposit request 1 data mismatch: %x", deposits[1].Data)
	}
}

func TestProcessRequests_WithdrawalRequests(t *testing.T) {
	config := pragueConfig()
	statedb := state.NewMemoryStateDB()
	header := &types.Header{
		Number:   big.NewInt(1),
		GasLimit: 10_000_000,
		Time:     1000,
	}

	// Set up withdrawal request contract with 1 request.
	var req types.Hash
	req[0] = 0x11
	req[1] = 0x22
	req[2] = 0x33
	setupSystemContract(statedb, types.WithdrawalRequestAddress, []types.Hash{req})

	requests, err := ProcessRequests(config, statedb, header)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	withdrawals := requests.FilterByType(types.WithdrawalRequestType)
	if len(withdrawals) != 1 {
		t.Fatalf("expected 1 withdrawal request, got %d", len(withdrawals))
	}
	if withdrawals[0].Data[0] != 0x11 {
		t.Fatalf("withdrawal request data mismatch: %x", withdrawals[0].Data)
	}
}

func TestProcessRequests_ConsolidationRequests(t *testing.T) {
	config := pragueConfig()
	statedb := state.NewMemoryStateDB()
	header := &types.Header{
		Number:   big.NewInt(1),
		GasLimit: 10_000_000,
		Time:     1000,
	}

	// Set up consolidation contract with 1 request.
	var req types.Hash
	req[0] = 0xFF
	setupSystemContract(statedb, types.ConsolidationRequestAddress, []types.Hash{req})

	requests, err := ProcessRequests(config, statedb, header)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	consolidations := requests.FilterByType(types.ConsolidationRequestType)
	if len(consolidations) != 1 {
		t.Fatalf("expected 1 consolidation request, got %d", len(consolidations))
	}
	if consolidations[0].Data[0] != 0xFF {
		t.Fatalf("consolidation request data mismatch: %x", consolidations[0].Data)
	}
}

func TestProcessRequests_AllThreeTypes(t *testing.T) {
	config := pragueConfig()
	statedb := state.NewMemoryStateDB()
	header := &types.Header{
		Number:   big.NewInt(1),
		GasLimit: 10_000_000,
		Time:     1000,
	}

	// Set up all three system contracts.
	var dep types.Hash
	dep[0] = 0x01
	setupSystemContract(statedb, types.DepositContractAddress, []types.Hash{dep})

	var wd types.Hash
	wd[0] = 0x02
	setupSystemContract(statedb, types.WithdrawalRequestAddress, []types.Hash{wd})

	var con types.Hash
	con[0] = 0x03
	setupSystemContract(statedb, types.ConsolidationRequestAddress, []types.Hash{con})

	requests, err := ProcessRequests(config, statedb, header)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(requests) != 3 {
		t.Fatalf("expected 3 total requests, got %d", len(requests))
	}

	// Check that requests are ordered by type: deposits, withdrawals, consolidations.
	if requests[0].Type != types.DepositRequestType {
		t.Fatalf("expected first request to be deposit, got type %d", requests[0].Type)
	}
	if requests[1].Type != types.WithdrawalRequestType {
		t.Fatalf("expected second request to be withdrawal, got type %d", requests[1].Type)
	}
	if requests[2].Type != types.ConsolidationRequestType {
		t.Fatalf("expected third request to be consolidation, got type %d", requests[2].Type)
	}
}

func TestProcessRequests_ClearsCountAfterRead(t *testing.T) {
	config := pragueConfig()
	statedb := state.NewMemoryStateDB()
	header := &types.Header{
		Number:   big.NewInt(1),
		GasLimit: 10_000_000,
		Time:     1000,
	}

	var req types.Hash
	req[0] = 0xAA
	setupSystemContract(statedb, types.DepositContractAddress, []types.Hash{req})

	// First call should return 1 request.
	requests, err := ProcessRequests(config, statedb, header)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(requests.FilterByType(types.DepositRequestType)) != 1 {
		t.Fatal("expected 1 deposit request on first call")
	}

	// Second call should return 0 requests (count was cleared).
	requests2, err := ProcessRequests(config, statedb, header)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(requests2.FilterByType(types.DepositRequestType)) != 0 {
		t.Fatal("expected 0 deposit requests on second call after count cleared")
	}
}

func TestProcessRequests_ZeroCountContract(t *testing.T) {
	config := pragueConfig()
	statedb := state.NewMemoryStateDB()
	header := &types.Header{
		Number:   big.NewInt(1),
		GasLimit: 10_000_000,
		Time:     1000,
	}

	// Create deposit contract with count=0 (no requests).
	statedb.CreateAccount(types.DepositContractAddress)
	statedb.SetCode(types.DepositContractAddress, []byte{0x00})
	statedb.SetState(types.DepositContractAddress, requestCountSlot, types.Hash{})

	requests, err := ProcessRequests(config, statedb, header)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(requests) != 0 {
		t.Fatalf("expected 0 requests with zero count, got %d", len(requests))
	}
}

func TestProcessWithRequests(t *testing.T) {
	config := pragueConfig()
	statedb := state.NewMemoryStateDB()

	header := &types.Header{
		Number:   big.NewInt(1),
		GasLimit: 10_000_000,
		Time:     1000,
		BaseFee:  big.NewInt(1_000_000_000),
		Coinbase: types.HexToAddress("0xfee"),
	}

	// Set up a withdrawal request.
	var req types.Hash
	req[0] = 0x42
	setupSystemContract(statedb, types.WithdrawalRequestAddress, []types.Hash{req})

	// Create an empty block.
	block := types.NewBlock(header, &types.Body{})

	proc := NewStateProcessor(config)
	result, err := proc.ProcessWithRequests(block, statedb)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result.Receipts) != 0 {
		t.Fatalf("expected 0 receipts for empty block, got %d", len(result.Receipts))
	}
	if len(result.Requests) != 1 {
		t.Fatalf("expected 1 request, got %d", len(result.Requests))
	}
	if result.Requests[0].Type != types.WithdrawalRequestType {
		t.Fatalf("expected withdrawal request, got type %d", result.Requests[0].Type)
	}
}

func TestValidateRequests_PostPrague_ValidHash(t *testing.T) {
	config := pragueConfig()
	v := NewBlockValidator(config)

	requests := types.Requests{
		types.NewRequest(types.DepositRequestType, []byte{0xAA, 0xBB}),
		types.NewRequest(types.WithdrawalRequestType, []byte{0xCC}),
	}

	hash := types.ComputeRequestsHash(requests)
	header := &types.Header{
		Number:       big.NewInt(1),
		Time:         1000,
		RequestsHash: &hash,
	}

	if err := v.ValidateRequests(header, requests); err != nil {
		t.Fatalf("valid requests hash rejected: %v", err)
	}
}

func TestValidateRequests_PostPrague_InvalidHash(t *testing.T) {
	config := pragueConfig()
	v := NewBlockValidator(config)

	requests := types.Requests{
		types.NewRequest(types.DepositRequestType, []byte{0xAA, 0xBB}),
	}

	// Use a wrong hash.
	wrongHash := types.HexToHash("0xdeadbeef")
	header := &types.Header{
		Number:       big.NewInt(1),
		Time:         1000,
		RequestsHash: &wrongHash,
	}

	if err := v.ValidateRequests(header, requests); err == nil {
		t.Fatal("expected error for invalid requests hash")
	}
}

func TestValidateRequests_PostPrague_MissingHash(t *testing.T) {
	config := pragueConfig()
	v := NewBlockValidator(config)

	header := &types.Header{
		Number:       big.NewInt(1),
		Time:         1000,
		RequestsHash: nil, // missing
	}

	if err := v.ValidateRequests(header, nil); err == nil {
		t.Fatal("expected error for missing requests_hash in post-Prague block")
	}
}

func TestValidateRequests_PrePrague_NoHash(t *testing.T) {
	config := prePragueConfig()
	v := NewBlockValidator(config)

	header := &types.Header{
		Number:       big.NewInt(1),
		Time:         1000,
		RequestsHash: nil,
	}

	// Pre-Prague with no hash should be valid.
	if err := v.ValidateRequests(header, nil); err != nil {
		t.Fatalf("pre-Prague block without requests_hash should be valid: %v", err)
	}
}

func TestValidateRequests_PrePrague_HasHash(t *testing.T) {
	config := prePragueConfig()
	v := NewBlockValidator(config)

	hash := types.Hash{0x01}
	header := &types.Header{
		Number:       big.NewInt(1),
		Time:         1000,
		RequestsHash: &hash,
	}

	// Pre-Prague with requests_hash should fail.
	if err := v.ValidateRequests(header, nil); err == nil {
		t.Fatal("expected error for pre-Prague block with requests_hash")
	}
}

func TestValidateRequests_PostPrague_EmptyRequests(t *testing.T) {
	config := pragueConfig()
	v := NewBlockValidator(config)

	// Empty requests list should produce a valid hash.
	var requests types.Requests
	hash := types.ComputeRequestsHash(requests)
	header := &types.Header{
		Number:       big.NewInt(1),
		Time:         1000,
		RequestsHash: &hash,
	}

	if err := v.ValidateRequests(header, requests); err != nil {
		t.Fatalf("valid empty requests hash rejected: %v", err)
	}
}

// Test helper functions.

func TestCountToUint64(t *testing.T) {
	// Zero value.
	var zero types.Hash
	if countToUint64(zero) != 0 {
		t.Fatal("expected 0 for zero hash")
	}

	// Value 1 in big-endian.
	var one types.Hash
	one[31] = 1
	if countToUint64(one) != 1 {
		t.Fatalf("expected 1, got %d", countToUint64(one))
	}

	// Value 256 in big-endian.
	var v256 types.Hash
	v256[30] = 1
	if countToUint64(v256) != 256 {
		t.Fatalf("expected 256, got %d", countToUint64(v256))
	}

	// Value 0xFFFF in big-endian.
	var vFFFF types.Hash
	vFFFF[30] = 0xFF
	vFFFF[31] = 0xFF
	if countToUint64(vFFFF) != 0xFFFF {
		t.Fatalf("expected 0xFFFF, got %d", countToUint64(vFFFF))
	}
}

func TestIncrementSlot(t *testing.T) {
	base := types.BytesToHash([]byte{0x01})

	// Increment by 0.
	result := incrementSlot(base, 0)
	if result != base {
		t.Fatalf("increment by 0 should return base, got %s", result.Hex())
	}

	// Increment by 1: slot 1 + 1 = slot 2.
	result = incrementSlot(base, 1)
	expected := types.BytesToHash([]byte{0x02})
	if result != expected {
		t.Fatalf("increment by 1: want %s, got %s", expected.Hex(), result.Hex())
	}

	// Increment by 255.
	result = incrementSlot(base, 255)
	var exp256 types.Hash
	exp256[31] = 0x00 // 1 + 255 = 256 = 0x100
	exp256[30] = 0x01
	if result != exp256 {
		t.Fatalf("increment by 255: want %s, got %s", exp256.Hex(), result.Hex())
	}
}

func TestTrimTrailingZeros(t *testing.T) {
	// All zeros.
	result := trimTrailingZeros(make([]byte, 32))
	if result != nil {
		t.Fatalf("expected nil for all zeros, got %x", result)
	}

	// Data with trailing zeros.
	data := []byte{0xAA, 0xBB, 0x00, 0x00}
	result = trimTrailingZeros(data)
	if len(result) != 2 || result[0] != 0xAA || result[1] != 0xBB {
		t.Fatalf("expected [AA BB], got %x", result)
	}

	// Data with no trailing zeros.
	data2 := []byte{0x01, 0x02, 0x03}
	result = trimTrailingZeros(data2)
	if len(result) != 3 {
		t.Fatalf("expected 3 bytes, got %d", len(result))
	}

	// Single non-zero byte.
	data3 := []byte{0xFF, 0x00, 0x00, 0x00}
	result = trimTrailingZeros(data3)
	if len(result) != 1 || result[0] != 0xFF {
		t.Fatalf("expected [FF], got %x", result)
	}
}
