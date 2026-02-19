package core

import (
	"math/big"
	"testing"

	"github.com/eth2028/eth2028/core/state"
	"github.com/eth2028/eth2028/core/types"
)

func TestCalcWithdrawalFee(t *testing.T) {
	tests := []struct {
		name           string
		excessRequests uint64
		wantMin        *big.Int // fee should be >= this
	}{
		{
			name:           "zero excess, minimum fee",
			excessRequests: 0,
			wantMin:        big.NewInt(1),
		},
		{
			name:           "small excess",
			excessRequests: 5,
			wantMin:        big.NewInt(1), // still >= min
		},
		{
			name:           "moderate excess",
			excessRequests: 17, // one full fraction
			wantMin:        big.NewInt(2), // e^1 ~= 2.71
		},
		{
			name:           "high excess",
			excessRequests: 34, // two full fractions
			wantMin:        big.NewInt(7), // e^2 ~= 7.38
		},
		{
			name:           "very high excess",
			excessRequests: 100,
			wantMin:        big.NewInt(100), // e^(100/17) is large
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fee := CalcWithdrawalFee(tt.excessRequests)
			if fee.Cmp(tt.wantMin) < 0 {
				t.Errorf("CalcWithdrawalFee(%d) = %s, want >= %s",
					tt.excessRequests, fee.String(), tt.wantMin.String())
			}
		})
	}
}

func TestCalcWithdrawalFeeMonotonic(t *testing.T) {
	// Fee should increase monotonically with excess.
	prev := CalcWithdrawalFee(0)
	for excess := uint64(1); excess <= 100; excess++ {
		fee := CalcWithdrawalFee(excess)
		if fee.Cmp(prev) < 0 {
			t.Errorf("fee decreased at excess=%d: %s < %s", excess, fee, prev)
		}
		prev = fee
	}
}

func TestUpdateExcessWithdrawalRequests(t *testing.T) {
	tests := []struct {
		name           string
		previousExcess uint64
		count          uint64
		want           uint64
	}{
		{"below target", 0, 1, 0},
		{"at target", 0, 2, 0},
		{"above target", 0, 5, 3},
		{"accumulating", 10, 5, 13},
		{"decaying", 10, 0, 8},
		{"decay to zero", 1, 0, 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := UpdateExcessWithdrawalRequests(tt.previousExcess, tt.count)
			if got != tt.want {
				t.Errorf("UpdateExcessWithdrawalRequests(%d, %d) = %d, want %d",
					tt.previousExcess, tt.count, got, tt.want)
			}
		})
	}
}

func TestProcessWithdrawalRequests(t *testing.T) {
	statedb := state.NewMemoryStateDB()
	addr := WithdrawalRequestContract

	// Set up a queue with 3 requests.
	headSlot := types.Hash{}
	headSlot[31] = WithdrawalRequestQueueHeadStorageSlot
	tailSlot := types.Hash{}
	tailSlot[31] = WithdrawalRequestQueueTailStorageSlot

	statedb.SetState(addr, headSlot, uint64ToHash(0))
	statedb.SetState(addr, tailSlot, uint64ToHash(3))

	// Write 3 requests into queue storage.
	for i := uint64(0); i < 3; i++ {
		base := WithdrawalRequestQueueStorageOffset + i*3

		// Source address: 0x00...0(i+1) (right-aligned in 32 bytes)
		addrVal := types.Hash{}
		addrVal[31] = byte(i + 1)
		statedb.SetState(addr, uint64ToHash(base), addrVal)

		// Pubkey slot 1: first 32 bytes
		pubkey1 := types.Hash{}
		pubkey1[0] = byte(0xAA + i)
		statedb.SetState(addr, uint64ToHash(base+1), pubkey1)

		// Pubkey slot 2: last 16 bytes of pubkey + 8 bytes LE amount
		pubkey2 := types.Hash{}
		pubkey2[0] = byte(0xBB + i)
		// Amount = (i+1) * 1000000 in little-endian at offset 16
		amount := (i + 1) * 1000000
		pubkey2[16] = byte(amount)
		pubkey2[17] = byte(amount >> 8)
		pubkey2[18] = byte(amount >> 16)
		pubkey2[19] = byte(amount >> 24)
		statedb.SetState(addr, uint64ToHash(base+2), pubkey2)
	}

	// Set count and excess for update logic.
	excessSlot := types.Hash{}
	excessSlot[31] = ExcessWithdrawalRequestsStorageSlot
	countSlot := types.Hash{}
	countSlot[31] = WithdrawalRequestCountStorageSlot
	statedb.SetState(addr, countSlot, uint64ToHash(3))
	statedb.SetState(addr, excessSlot, uint64ToHash(0))

	header := &types.Header{Number: big.NewInt(100)}
	requests := ProcessWithdrawalRequests(statedb, header)

	if len(requests) != 3 {
		t.Fatalf("expected 3 requests, got %d", len(requests))
	}

	// Check first request source address.
	if requests[0].SourceAddress[19] != 1 {
		t.Errorf("request[0] source address byte mismatch: got %d, want 1", requests[0].SourceAddress[19])
	}

	// Check amounts.
	if requests[0].Amount != 1000000 {
		t.Errorf("request[0] amount = %d, want 1000000", requests[0].Amount)
	}
	if requests[1].Amount != 2000000 {
		t.Errorf("request[1] amount = %d, want 2000000", requests[1].Amount)
	}

	// Queue should be empty (head == tail), both reset to 0.
	newHead := statedb.GetState(addr, headSlot)
	newTail := statedb.GetState(addr, tailSlot)
	if newHead != (types.Hash{}) || newTail != (types.Hash{}) {
		t.Error("queue pointers not reset after full drain")
	}

	// Count should be reset to 0.
	newCount := statedb.GetState(addr, countSlot)
	if newCount != (types.Hash{}) {
		t.Error("count not reset after processing")
	}

	// Excess should be 1 (0 + 3 - 2 = 1).
	newExcess := statedb.GetState(addr, excessSlot)
	if hashToUint64V2(newExcess) != 1 {
		t.Errorf("excess = %d, want 1", hashToUint64V2(newExcess))
	}
}

func TestProcessWithdrawalRequestsMaxCap(t *testing.T) {
	statedb := state.NewMemoryStateDB()
	addr := WithdrawalRequestContract

	headSlot := types.Hash{}
	headSlot[31] = WithdrawalRequestQueueHeadStorageSlot
	tailSlot := types.Hash{}
	tailSlot[31] = WithdrawalRequestQueueTailStorageSlot

	// Put 20 requests in queue (more than max 16).
	statedb.SetState(addr, headSlot, uint64ToHash(0))
	statedb.SetState(addr, tailSlot, uint64ToHash(20))

	for i := uint64(0); i < 20; i++ {
		base := WithdrawalRequestQueueStorageOffset + i*3
		addrVal := types.Hash{}
		addrVal[31] = byte(i + 1)
		statedb.SetState(addr, uint64ToHash(base), addrVal)

		pubkey1 := types.Hash{}
		pubkey1[0] = byte(i + 1)
		statedb.SetState(addr, uint64ToHash(base+1), pubkey1)

		pubkey2 := types.Hash{}
		pubkey2[0] = byte(i + 1)
		pubkey2[16] = 1 // amount = 1
		statedb.SetState(addr, uint64ToHash(base+2), pubkey2)
	}

	countSlot := types.Hash{}
	countSlot[31] = WithdrawalRequestCountStorageSlot
	excessSlot := types.Hash{}
	excessSlot[31] = ExcessWithdrawalRequestsStorageSlot
	statedb.SetState(addr, countSlot, uint64ToHash(20))
	statedb.SetState(addr, excessSlot, uint64ToHash(0))

	header := &types.Header{Number: big.NewInt(100)}
	requests := ProcessWithdrawalRequests(statedb, header)

	// Should cap at MaxWithdrawalRequestsPerBlock.
	if len(requests) != MaxWithdrawalRequestsPerBlock {
		t.Errorf("got %d requests, want %d", len(requests), MaxWithdrawalRequestsPerBlock)
	}

	// Queue head should advance to 16, tail should remain 20.
	newHead := statedb.GetState(addr, headSlot)
	if hashToUint64V2(newHead) != 16 {
		t.Errorf("queue head = %d, want 16", hashToUint64V2(newHead))
	}
}

func TestValidateWithdrawalRequest(t *testing.T) {
	// Valid request.
	validReq := &types.WithdrawalRequest{
		SourceAddress:   types.HexToAddress("0x1234567890abcdef1234567890abcdef12345678"),
		ValidatorPubkey: [48]byte{1, 2, 3},
		Amount:          32000000000, // 32 ETH in gwei
	}
	if err := ValidateWithdrawalRequest(validReq); err != nil {
		t.Errorf("valid request rejected: %v", err)
	}

	// Invalid: empty pubkey.
	invalidReq := &types.WithdrawalRequest{
		SourceAddress:   types.HexToAddress("0x1234567890abcdef1234567890abcdef12345678"),
		ValidatorPubkey: [48]byte{},
		Amount:          32000000000,
	}
	if err := ValidateWithdrawalRequest(invalidReq); err != ErrWithdrawalRequestEmptyPubkey {
		t.Errorf("expected ErrWithdrawalRequestEmptyPubkey, got %v", err)
	}
}

func TestAddWithdrawalRequest(t *testing.T) {
	queue := &WithdrawalRequestQueue{
		Fee: big.NewInt(1),
	}

	// Add valid request.
	req := types.WithdrawalRequest{
		SourceAddress:   types.HexToAddress("0x1111111111111111111111111111111111111111"),
		ValidatorPubkey: [48]byte{0xAA},
		Amount:          1000000,
	}
	if err := AddWithdrawalRequest(queue, req); err != nil {
		t.Fatalf("AddWithdrawalRequest failed: %v", err)
	}
	if len(queue.Requests) != 1 {
		t.Errorf("queue length = %d, want 1", len(queue.Requests))
	}

	// Try to add invalid request (empty pubkey).
	badReq := types.WithdrawalRequest{
		SourceAddress: types.HexToAddress("0x2222222222222222222222222222222222222222"),
		Amount:        1000000,
	}
	if err := AddWithdrawalRequest(queue, badReq); err == nil {
		t.Error("expected error for empty pubkey, got nil")
	}
	if len(queue.Requests) != 1 {
		t.Errorf("queue length = %d, want 1 (bad request should not be added)", len(queue.Requests))
	}
}

func TestHashUint64V2Roundtrip(t *testing.T) {
	values := []uint64{0, 1, 255, 256, 65535, 1<<32 - 1, 1<<64 - 1}
	for _, v := range values {
		h := uint64ToHash(v)
		got := hashToUint64V2(h)
		if got != v {
			t.Errorf("roundtrip failed for %d: got %d", v, got)
		}
	}
}
