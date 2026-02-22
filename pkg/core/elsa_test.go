package core

import (
	"math/big"
	"sync"
	"testing"
	"time"

	"github.com/eth2030/eth2030/core/state"
	"github.com/eth2030/eth2030/core/types"
)

func setupELSA(t *testing.T) (*ELSA, types.Address) {
	t.Helper()
	elsa := NewELSA(DefaultELSAConfig())
	addr := types.HexToAddress("0x1234567890abcdef1234567890abcdef12345678")

	db := state.NewMemoryStateDB()
	db.CreateAccount(addr)
	db.AddBalance(addr, big.NewInt(1000))
	db.SetNonce(addr, 42)
	db.SetCode(addr, []byte{0x60, 0x00, 0x60, 0x00, 0xFD}) // PUSH0 PUSH0 REVERT
	db.SetState(addr, types.HexToHash("0x01"), types.HexToHash("0xAA"))
	db.SetState(addr, types.HexToHash("0x02"), types.HexToHash("0xBB"))
	db.Commit()

	elsa.SetState(db)
	return elsa, addr
}

func TestNewELSA(t *testing.T) {
	elsa := NewELSA(ELSAConfig{})
	if elsa == nil {
		t.Fatal("NewELSA returned nil")
	}
	if elsa.config.MaxBatchSize != DefaultELSAConfig().MaxBatchSize {
		t.Errorf("expected default MaxBatchSize %d, got %d",
			DefaultELSAConfig().MaxBatchSize, elsa.config.MaxBatchSize)
	}
}

func TestNewELSACustomConfig(t *testing.T) {
	cfg := ELSAConfig{MaxBatchSize: 50, MaxSubscriptions: 10, CacheSize: 100}
	elsa := NewELSA(cfg)
	if elsa.config.MaxBatchSize != 50 {
		t.Errorf("expected MaxBatchSize 50, got %d", elsa.config.MaxBatchSize)
	}
	if elsa.config.MaxSubscriptions != 10 {
		t.Errorf("expected MaxSubscriptions 10, got %d", elsa.config.MaxSubscriptions)
	}
}

func TestGetAccount(t *testing.T) {
	elsa, addr := setupELSA(t)

	acct, err := elsa.GetAccount(addr)
	if err != nil {
		t.Fatalf("GetAccount: %v", err)
	}
	if acct.Address != addr {
		t.Errorf("address mismatch")
	}
	if acct.Balance.Cmp(big.NewInt(1000)) != 0 {
		t.Errorf("balance: want 1000, got %s", acct.Balance)
	}
	if acct.Nonce != 42 {
		t.Errorf("nonce: want 42, got %d", acct.Nonce)
	}
	if acct.CodeHash.IsZero() {
		t.Error("code hash should not be zero for contract account")
	}
}

func TestGetAccountNotFound(t *testing.T) {
	elsa := NewELSA(DefaultELSAConfig())
	missing := types.HexToAddress("0xdead")
	_, err := elsa.GetAccount(missing)
	if err != ErrELSAAccountNotFound {
		t.Errorf("expected ErrELSAAccountNotFound, got %v", err)
	}
}

func TestGetStorage(t *testing.T) {
	elsa, addr := setupELSA(t)

	val, err := elsa.GetStorage(addr, types.HexToHash("0x01"))
	if err != nil {
		t.Fatalf("GetStorage: %v", err)
	}
	expected := types.HexToHash("0xAA")
	if val != expected {
		t.Errorf("storage slot 0x01: want %s, got %s", expected, val)
	}
}

func TestGetStorageEmpty(t *testing.T) {
	elsa, addr := setupELSA(t)

	val, err := elsa.GetStorage(addr, types.HexToHash("0xFF"))
	if err != nil {
		t.Fatalf("GetStorage: %v", err)
	}
	if val != (types.Hash{}) {
		t.Errorf("expected zero hash for empty slot, got %s", val)
	}
}

func TestGetCode(t *testing.T) {
	elsa, addr := setupELSA(t)

	code, err := elsa.GetCode(addr)
	if err != nil {
		t.Fatalf("GetCode: %v", err)
	}
	if len(code) != 5 {
		t.Errorf("code length: want 5, got %d", len(code))
	}
}

func TestGetCodeNoAccount(t *testing.T) {
	elsa := NewELSA(DefaultELSAConfig())
	code, err := elsa.GetCode(types.HexToAddress("0xdead"))
	if err != nil {
		t.Fatalf("GetCode: %v", err)
	}
	if code != nil {
		t.Errorf("expected nil code for nonexistent account, got %v", code)
	}
}

func TestGetProof(t *testing.T) {
	elsa, addr := setupELSA(t)

	proof, err := elsa.GetProof(addr, []types.Hash{types.HexToHash("0x01")})
	if err != nil {
		t.Fatalf("GetProof: %v", err)
	}
	if proof.Address != addr {
		t.Error("proof address mismatch")
	}
	if len(proof.AccountProof) == 0 {
		t.Error("account proof should not be empty")
	}
	if len(proof.StorageProofs) != 1 {
		t.Fatalf("expected 1 storage proof, got %d", len(proof.StorageProofs))
	}
	if proof.StorageProofs[0].Key != types.HexToHash("0x01") {
		t.Error("storage proof key mismatch")
	}
}

func TestVerifyProof(t *testing.T) {
	elsa, addr := setupELSA(t)

	root := elsa.State().GetRoot()
	proof, err := elsa.GetProof(addr, []types.Hash{types.HexToHash("0x01")})
	if err != nil {
		t.Fatalf("GetProof: %v", err)
	}

	if !VerifyProof(root, proof) {
		t.Error("valid proof should verify successfully")
	}
}

func TestVerifyProofNilProof(t *testing.T) {
	if VerifyProof(types.Hash{}, nil) {
		t.Error("nil proof should not verify")
	}
}

func TestVerifyProofWrongRoot(t *testing.T) {
	elsa, addr := setupELSA(t)

	proof, err := elsa.GetProof(addr, nil)
	if err != nil {
		t.Fatalf("GetProof: %v", err)
	}

	fakeRoot := types.HexToHash("0xdeadbeef")
	if VerifyProof(fakeRoot, proof) {
		t.Error("proof should not verify against wrong root")
	}
}

func TestBatchGetAccounts(t *testing.T) {
	elsa, addr := setupELSA(t)

	missing := types.HexToAddress("0xdead")
	results, err := elsa.BatchGetAccounts([]types.Address{addr, missing})
	if err != nil {
		t.Fatalf("BatchGetAccounts: %v", err)
	}
	if len(results) != 2 {
		t.Fatalf("expected 2 results, got %d", len(results))
	}
	if results[0] == nil {
		t.Error("expected non-nil result for existing account")
	}
	if results[1] != nil {
		t.Error("expected nil result for missing account")
	}
}

func TestBatchGetAccountsTooLarge(t *testing.T) {
	cfg := ELSAConfig{MaxBatchSize: 2, MaxSubscriptions: 10, CacheSize: 10}
	elsa := NewELSA(cfg)

	addrs := make([]types.Address, 5)
	_, err := elsa.BatchGetAccounts(addrs)
	if err != ErrELSABatchTooLarge {
		t.Errorf("expected ErrELSABatchTooLarge, got %v", err)
	}
}

func TestSubscribeStateChanges(t *testing.T) {
	elsa, addr := setupELSA(t)

	sub, err := elsa.SubscribeStateChanges(addr)
	if err != nil {
		t.Fatalf("SubscribeStateChanges: %v", err)
	}
	if sub == nil {
		t.Fatal("subscription should not be nil")
	}
	if elsa.SubscriptionCount() != 1 {
		t.Errorf("expected 1 subscription, got %d", elsa.SubscriptionCount())
	}

	// Send a change.
	change := &StateChange{
		Address:     addr,
		Slot:        types.HexToHash("0x01"),
		OldValue:    types.HexToHash("0xAA"),
		NewValue:    types.HexToHash("0xCC"),
		BlockNumber: 100,
	}
	elsa.NotifyStateChange(change)

	select {
	case received := <-sub.Changes:
		if received.NewValue != change.NewValue {
			t.Errorf("received change value mismatch")
		}
		if received.BlockNumber != 100 {
			t.Errorf("block number: want 100, got %d", received.BlockNumber)
		}
	case <-time.After(100 * time.Millisecond):
		t.Fatal("timed out waiting for state change")
	}
}

func TestSubscriptionMaxReached(t *testing.T) {
	cfg := ELSAConfig{MaxBatchSize: 10, MaxSubscriptions: 2, CacheSize: 10}
	elsa := NewELSA(cfg)
	addr := types.HexToAddress("0x01")

	_, err := elsa.SubscribeStateChanges(addr)
	if err != nil {
		t.Fatalf("sub 1: %v", err)
	}
	_, err = elsa.SubscribeStateChanges(addr)
	if err != nil {
		t.Fatalf("sub 2: %v", err)
	}
	_, err = elsa.SubscribeStateChanges(addr)
	if err != ErrELSAMaxSubscription {
		t.Errorf("expected ErrELSAMaxSubscription, got %v", err)
	}
}

func TestUnsubscribe(t *testing.T) {
	elsa, addr := setupELSA(t)

	sub, err := elsa.SubscribeStateChanges(addr)
	if err != nil {
		t.Fatalf("Subscribe: %v", err)
	}
	if elsa.SubscriptionCount() != 1 {
		t.Fatalf("expected 1, got %d", elsa.SubscriptionCount())
	}

	elsa.Unsubscribe(sub)
	if elsa.SubscriptionCount() != 0 {
		t.Errorf("expected 0 subscriptions after unsubscribe, got %d", elsa.SubscriptionCount())
	}
	if !sub.IsClosed() {
		t.Error("subscription should be closed after unsubscribe")
	}
}

func TestNotifyNoSubscribers(t *testing.T) {
	elsa := NewELSA(DefaultELSAConfig())
	// Should not panic.
	elsa.NotifyStateChange(&StateChange{
		Address: types.HexToAddress("0xdead"),
	})
}

func TestNotifyClosedSubscription(t *testing.T) {
	elsa, addr := setupELSA(t)

	sub, _ := elsa.SubscribeStateChanges(addr)
	sub.Close()

	// Should not panic; change is silently dropped.
	elsa.NotifyStateChange(&StateChange{
		Address: addr,
		Slot:    types.HexToHash("0x01"),
	})
}

func TestSetState(t *testing.T) {
	elsa := NewELSA(DefaultELSAConfig())
	addr := types.HexToAddress("0xab")

	db := state.NewMemoryStateDB()
	db.CreateAccount(addr)
	db.AddBalance(addr, big.NewInt(500))

	elsa.SetState(db)
	acct, err := elsa.GetAccount(addr)
	if err != nil {
		t.Fatalf("GetAccount after SetState: %v", err)
	}
	if acct.Balance.Cmp(big.NewInt(500)) != 0 {
		t.Errorf("balance: want 500, got %s", acct.Balance)
	}
}

func TestConcurrentAccess(t *testing.T) {
	elsa, addr := setupELSA(t)

	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, _ = elsa.GetAccount(addr)
			_, _ = elsa.GetStorage(addr, types.HexToHash("0x01"))
			_, _ = elsa.GetCode(addr)
		}()
	}
	wg.Wait()
}

func TestConcurrentSubscribeNotify(t *testing.T) {
	elsa, addr := setupELSA(t)

	var wg sync.WaitGroup
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			sub, err := elsa.SubscribeStateChanges(addr)
			if err != nil {
				return
			}
			// Drain a few changes.
			go func() {
				for range sub.Changes {
				}
			}()
			time.Sleep(5 * time.Millisecond)
			elsa.Unsubscribe(sub)
		}(i)
	}

	// Concurrently send notifications.
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			elsa.NotifyStateChange(&StateChange{
				Address:     addr,
				Slot:        types.HexToHash("0x01"),
				BlockNumber: 1,
			})
		}()
	}

	wg.Wait()
}

func TestGetProofMultipleSlots(t *testing.T) {
	elsa, addr := setupELSA(t)

	slots := []types.Hash{
		types.HexToHash("0x01"),
		types.HexToHash("0x02"),
		types.HexToHash("0xFF"), // does not exist
	}
	proof, err := elsa.GetProof(addr, slots)
	if err != nil {
		t.Fatalf("GetProof: %v", err)
	}
	if len(proof.StorageProofs) != 3 {
		t.Fatalf("expected 3 storage proofs, got %d", len(proof.StorageProofs))
	}
}

func TestELSAAccountFields(t *testing.T) {
	elsa, addr := setupELSA(t)

	acct, err := elsa.GetAccount(addr)
	if err != nil {
		t.Fatal(err)
	}

	// Verify StorageRoot is non-empty since account has storage.
	if acct.StorageRoot == types.EmptyRootHash {
		t.Error("StorageRoot should not be empty for account with storage")
	}
}

func TestNilStateErrors(t *testing.T) {
	elsa := &ELSA{
		config: DefaultELSAConfig(),
		subs:   make(map[types.Address][]*StateChangeSubscription),
	}
	addr := types.HexToAddress("0x01")

	if _, err := elsa.GetAccount(addr); err != ErrELSANilState {
		t.Errorf("expected ErrELSANilState, got %v", err)
	}
	if _, err := elsa.GetStorage(addr, types.Hash{}); err != ErrELSANilState {
		t.Errorf("expected ErrELSANilState, got %v", err)
	}
	if _, err := elsa.GetCode(addr); err != ErrELSANilState {
		t.Errorf("expected ErrELSANilState, got %v", err)
	}
	if _, err := elsa.GetProof(addr, nil); err != ErrELSANilState {
		t.Errorf("expected ErrELSANilState, got %v", err)
	}
}

func TestSubscriptionCloseIdempotent(t *testing.T) {
	sub := &StateChangeSubscription{
		Changes: make(chan *StateChange, 1),
	}
	sub.Close()
	sub.Close() // should not panic
	if !sub.IsClosed() {
		t.Error("should remain closed")
	}
}
