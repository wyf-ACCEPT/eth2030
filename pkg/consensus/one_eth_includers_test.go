package consensus

import (
	"math/big"
	"testing"

	"github.com/eth2030/eth2030/core/types"
	"github.com/eth2030/eth2030/crypto"
)

func TestIncluderStatusString(t *testing.T) {
	tests := []struct {
		status IncluderStatus
		want   string
	}{
		{IncluderActive, "active"},
		{IncluderSlashed, "slashed"},
		{IncluderExited, "exited"},
		{IncluderStatus(99), "unknown"},
	}
	for _, tt := range tests {
		if got := tt.status.String(); got != tt.want {
			t.Errorf("IncluderStatus(%d).String() = %q, want %q", tt.status, got, tt.want)
		}
	}
}

func TestRegisterIncluder(t *testing.T) {
	pool := NewIncluderPool()
	addr := types.HexToAddress("0xaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa")

	err := pool.RegisterIncluder(addr, OneETH)
	if err != nil {
		t.Fatalf("RegisterIncluder: %v", err)
	}

	if pool.ActiveCount() != 1 {
		t.Errorf("ActiveCount: got %d, want 1", pool.ActiveCount())
	}
	if pool.TotalCount() != 1 {
		t.Errorf("TotalCount: got %d, want 1", pool.TotalCount())
	}
}

func TestRegisterIncluderZeroAddress(t *testing.T) {
	pool := NewIncluderPool()
	err := pool.RegisterIncluder(types.Address{}, OneETH)
	if err != ErrIncluderZeroAddress {
		t.Errorf("expected ErrIncluderZeroAddress, got %v", err)
	}
}

func TestRegisterIncluderWrongStake(t *testing.T) {
	pool := NewIncluderPool()
	addr := types.HexToAddress("0xaaaa")

	// Too little.
	tooLittle := new(big.Int).Sub(OneETH, big.NewInt(1))
	err := pool.RegisterIncluder(addr, tooLittle)
	if err != ErrIncluderWrongStake {
		t.Errorf("expected ErrIncluderWrongStake for too little, got %v", err)
	}

	// Too much.
	tooMuch := new(big.Int).Add(OneETH, big.NewInt(1))
	err = pool.RegisterIncluder(addr, tooMuch)
	if err != ErrIncluderWrongStake {
		t.Errorf("expected ErrIncluderWrongStake for too much, got %v", err)
	}

	// Nil.
	err = pool.RegisterIncluder(addr, nil)
	if err != ErrIncluderWrongStake {
		t.Errorf("expected ErrIncluderWrongStake for nil, got %v", err)
	}
}

func TestRegisterIncluderDuplicate(t *testing.T) {
	pool := NewIncluderPool()
	addr := types.HexToAddress("0xbbbb")

	pool.RegisterIncluder(addr, OneETH)
	err := pool.RegisterIncluder(addr, OneETH)
	if err != ErrIncluderAlreadyRegistered {
		t.Errorf("expected ErrIncluderAlreadyRegistered, got %v", err)
	}
}

func TestUnregisterIncluder(t *testing.T) {
	pool := NewIncluderPool()
	addr := types.HexToAddress("0xcccc")

	pool.RegisterIncluder(addr, OneETH)
	err := pool.UnregisterIncluder(addr)
	if err != nil {
		t.Fatalf("UnregisterIncluder: %v", err)
	}

	if pool.ActiveCount() != 0 {
		t.Errorf("ActiveCount after unregister: got %d, want 0", pool.ActiveCount())
	}
}

func TestUnregisterIncluderNotRegistered(t *testing.T) {
	pool := NewIncluderPool()
	err := pool.UnregisterIncluder(types.HexToAddress("0xdead"))
	if err != ErrIncluderNotRegistered {
		t.Errorf("expected ErrIncluderNotRegistered, got %v", err)
	}
}

func TestSelectIncluder(t *testing.T) {
	pool := NewIncluderPool()

	addrs := []types.Address{
		types.HexToAddress("0x1111111111111111111111111111111111111111"),
		types.HexToAddress("0x2222222222222222222222222222222222222222"),
		types.HexToAddress("0x3333333333333333333333333333333333333333"),
	}
	for _, addr := range addrs {
		pool.RegisterIncluder(addr, OneETH)
	}

	seed := types.HexToHash("0xabcdef")
	selected, err := pool.SelectIncluder(100, seed)
	if err != nil {
		t.Fatalf("SelectIncluder: %v", err)
	}
	if selected.IsZero() {
		t.Fatal("selected address should not be zero")
	}

	// Determinism: same slot + seed -> same selection.
	selected2, _ := pool.SelectIncluder(100, seed)
	if selected != selected2 {
		t.Error("SelectIncluder should be deterministic")
	}

	// Different slot -> may produce different selection (not guaranteed
	// with only 3 includers, but we check it runs without error).
	_, err = pool.SelectIncluder(101, seed)
	if err != nil {
		t.Fatalf("SelectIncluder(101): %v", err)
	}
}

func TestSelectIncluderEmptyPool(t *testing.T) {
	pool := NewIncluderPool()
	_, err := pool.SelectIncluder(1, types.Hash{})
	if err != ErrIncluderPoolEmpty {
		t.Errorf("expected ErrIncluderPoolEmpty, got %v", err)
	}
}

func TestSelectIncluderAfterSlash(t *testing.T) {
	pool := NewIncluderPool()
	addr1 := types.HexToAddress("0x1111111111111111111111111111111111111111")
	addr2 := types.HexToAddress("0x2222222222222222222222222222222222222222")

	pool.RegisterIncluder(addr1, OneETH)
	pool.RegisterIncluder(addr2, OneETH)

	// Slash addr1.
	pool.SlashIncluder(addr1, "missed duty")

	// Only addr2 should be selectable.
	for i := 0; i < 10; i++ {
		var seed types.Hash
		seed[0] = byte(i)
		selected, _ := pool.SelectIncluder(Slot(i), seed)
		if selected != addr2 {
			t.Errorf("slot %d: selected %s, expected %s", i, selected.Hex(), addr2.Hex())
		}
	}
}

func TestIncluderDutyHash(t *testing.T) {
	duty := &IncluderDuty{
		Slot:       100,
		Includer:   types.HexToAddress("0xaaaa"),
		TxListHash: types.HexToHash("0xbbbb"),
		Deadline:   1700000000,
	}

	h := duty.Hash()
	if h.IsZero() {
		t.Fatal("duty hash should not be zero")
	}

	// Determinism.
	h2 := duty.Hash()
	if h != h2 {
		t.Fatal("duty hash should be deterministic")
	}
}

func TestVerifyIncluderSignature(t *testing.T) {
	// Generate a key pair.
	key, err := crypto.GenerateKey()
	if err != nil {
		t.Fatalf("GenerateKey: %v", err)
	}
	addr := crypto.PubkeyToAddress(key.PublicKey)

	duty := &IncluderDuty{
		Slot:       42,
		Includer:   addr,
		TxListHash: types.HexToHash("0xdeadbeef"),
		Deadline:   1700000000,
	}

	dutyHash := duty.Hash()
	sig, err := crypto.Sign(dutyHash[:], key)
	if err != nil {
		t.Fatalf("Sign: %v", err)
	}

	if !VerifyIncluderSignature(duty, sig) {
		t.Fatal("valid signature should verify")
	}
}

func TestVerifyIncluderSignatureWrongSigner(t *testing.T) {
	key, _ := crypto.GenerateKey()
	// Use a different address in the duty.
	duty := &IncluderDuty{
		Slot:       1,
		Includer:   types.HexToAddress("0xdead"),
		TxListHash: types.Hash{0x01},
		Deadline:   1234,
	}

	dutyHash := duty.Hash()
	sig, _ := crypto.Sign(dutyHash[:], key)

	if VerifyIncluderSignature(duty, sig) {
		t.Fatal("wrong signer should not verify")
	}
}

func TestVerifyIncluderSignatureNil(t *testing.T) {
	if VerifyIncluderSignature(nil, []byte{0x01}) {
		t.Fatal("nil duty should not verify")
	}
}

func TestVerifyIncluderSignatureBadSigLen(t *testing.T) {
	duty := &IncluderDuty{Slot: 1, Includer: types.HexToAddress("0x01")}
	if VerifyIncluderSignature(duty, []byte{0x01, 0x02}) {
		t.Fatal("short signature should not verify")
	}
}

func TestIncluderReward(t *testing.T) {
	// Slot 0 should get base reward.
	reward := IncluderReward(0)
	if reward != BaseIncluderReward {
		t.Errorf("slot 0 reward: got %d, want %d", reward, BaseIncluderReward)
	}

	// Slot 1 should get base - decay.
	reward1 := IncluderReward(1)
	if reward1 != BaseIncluderReward-IncluderRewardDecay {
		t.Errorf("slot 1 reward: got %d, want %d", reward1, BaseIncluderReward-IncluderRewardDecay)
	}

	// Ensure reward never goes to zero (minimum 1).
	highSlot := IncluderReward(Slot(32 * 1000)) // large slot, but mod 32 = 0
	if highSlot < 1 {
		t.Errorf("reward should be at least 1, got %d", highSlot)
	}
}

func TestSlashIncluder(t *testing.T) {
	pool := NewIncluderPool()
	addr := types.HexToAddress("0xaaaa")
	pool.RegisterIncluder(addr, OneETH)

	err := pool.SlashIncluder(addr, "missed inclusion duty")
	if err != nil {
		t.Fatalf("SlashIncluder: %v", err)
	}

	record := pool.GetIncluder(addr)
	if record == nil {
		t.Fatal("slashed includer should still be in pool")
	}
	if record.Status != IncluderSlashed {
		t.Errorf("status: got %s, want slashed", record.Status)
	}
	if record.SlashReason != "missed inclusion duty" {
		t.Errorf("slash reason: got %q", record.SlashReason)
	}

	// Stake should be reduced by SlashPenaltyPercent (10%).
	expectedStake := new(big.Int).Mul(OneETH, big.NewInt(90))
	expectedStake.Div(expectedStake, big.NewInt(100))
	if record.Stake.Cmp(expectedStake) != 0 {
		t.Errorf("stake after slash: got %s, want %s", record.Stake.String(), expectedStake.String())
	}

	// Active count should be 0.
	if pool.ActiveCount() != 0 {
		t.Errorf("ActiveCount after slash: got %d, want 0", pool.ActiveCount())
	}
	// Total count should still include the slashed record.
	if pool.TotalCount() != 1 {
		t.Errorf("TotalCount after slash: got %d, want 1", pool.TotalCount())
	}
}

func TestSlashIncluderNotRegistered(t *testing.T) {
	pool := NewIncluderPool()
	err := pool.SlashIncluder(types.HexToAddress("0xdead"), "reason")
	if err != ErrIncluderNotRegistered {
		t.Errorf("expected ErrIncluderNotRegistered, got %v", err)
	}
}

func TestSlashIncluderAlreadySlashed(t *testing.T) {
	pool := NewIncluderPool()
	addr := types.HexToAddress("0xaaaa")
	pool.RegisterIncluder(addr, OneETH)
	pool.SlashIncluder(addr, "first")

	err := pool.SlashIncluder(addr, "second")
	if err != ErrIncluderAlreadySlashed {
		t.Errorf("expected ErrIncluderAlreadySlashed, got %v", err)
	}
}

func TestGetIncluder(t *testing.T) {
	pool := NewIncluderPool()
	addr := types.HexToAddress("0xbbbb")
	pool.RegisterIncluder(addr, OneETH)

	record := pool.GetIncluder(addr)
	if record == nil {
		t.Fatal("record should not be nil")
	}
	if record.Address != addr {
		t.Errorf("address: got %s, want %s", record.Address.Hex(), addr.Hex())
	}
	if record.Status != IncluderActive {
		t.Errorf("status: got %s, want active", record.Status)
	}
	if record.Stake.Cmp(OneETH) != 0 {
		t.Errorf("stake: got %s, want %s", record.Stake.String(), OneETH.String())
	}
}

func TestGetIncluderNotFound(t *testing.T) {
	pool := NewIncluderPool()
	record := pool.GetIncluder(types.HexToAddress("0xdead"))
	if record != nil {
		t.Fatal("expected nil for unregistered includer")
	}
}

func TestGetIncluderReturnsCopy(t *testing.T) {
	pool := NewIncluderPool()
	addr := types.HexToAddress("0xbbbb")
	pool.RegisterIncluder(addr, OneETH)

	record := pool.GetIncluder(addr)
	// Mutate the copy.
	record.Stake.SetInt64(0)

	// Original should be unchanged.
	record2 := pool.GetIncluder(addr)
	if record2.Stake.Cmp(OneETH) != 0 {
		t.Error("GetIncluder should return a defensive copy")
	}
}

func TestMultipleIncluders(t *testing.T) {
	pool := NewIncluderPool()

	for i := 0; i < 10; i++ {
		var addr types.Address
		addr[0] = byte(i + 1)
		pool.RegisterIncluder(addr, OneETH)
	}

	if pool.ActiveCount() != 10 {
		t.Errorf("ActiveCount: got %d, want 10", pool.ActiveCount())
	}

	// Slash 3 of them.
	for i := 0; i < 3; i++ {
		var addr types.Address
		addr[0] = byte(i + 1)
		pool.SlashIncluder(addr, "test")
	}

	if pool.ActiveCount() != 7 {
		t.Errorf("ActiveCount after slashing 3: got %d, want 7", pool.ActiveCount())
	}
	if pool.TotalCount() != 10 {
		t.Errorf("TotalCount: got %d, want 10", pool.TotalCount())
	}
}

func TestSelectIncluderDeterminismWithManyIncluders(t *testing.T) {
	pool := NewIncluderPool()
	for i := 0; i < 100; i++ {
		var addr types.Address
		addr[0] = byte(i / 256)
		addr[1] = byte(i % 256)
		addr[19] = byte(i + 1) // ensure non-zero
		pool.RegisterIncluder(addr, OneETH)
	}

	seed := types.HexToHash("0xfeedface")
	selected1, _ := pool.SelectIncluder(500, seed)
	selected2, _ := pool.SelectIncluder(500, seed)

	if selected1 != selected2 {
		t.Error("selection must be deterministic for same slot and seed")
	}
}

func TestValidateIncluderRegistration(t *testing.T) {
	pool := NewIncluderPool()
	addr := types.Address{0x01}

	// Valid registration.
	if err := ValidateIncluderRegistration(addr, OneETH, pool); err != nil {
		t.Errorf("valid registration: %v", err)
	}

	// Zero address.
	if err := ValidateIncluderRegistration(types.Address{}, OneETH, pool); err == nil {
		t.Error("expected error for zero address")
	}

	// Wrong stake.
	if err := ValidateIncluderRegistration(addr, big.NewInt(0), pool); err == nil {
		t.Error("expected error for wrong stake")
	}
}

func TestValidateIncluderDuty(t *testing.T) {
	duty := &IncluderDuty{Slot: 1, Includer: types.Address{0x01}}
	if err := ValidateIncluderDuty(duty); err != nil {
		t.Errorf("valid duty: %v", err)
	}

	if err := ValidateIncluderDuty(nil); err == nil {
		t.Error("expected error for nil duty")
	}

	bad := &IncluderDuty{Slot: 0, Includer: types.Address{0x01}}
	if err := ValidateIncluderDuty(bad); err == nil {
		t.Error("expected error for zero slot")
	}
}
