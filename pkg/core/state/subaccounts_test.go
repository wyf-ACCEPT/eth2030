package state

import (
	"sync"
	"testing"

	"github.com/eth2028/eth2028/core/types"
)

var testParent = types.HexToAddress("0xaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa")

func TestDeriveSubaccountAddress(t *testing.T) {
	sm := NewSubaccountManager()

	// Determinism: same inputs always yield the same address.
	a1 := sm.DeriveSubaccountAddress(testParent, 0)
	a2 := sm.DeriveSubaccountAddress(testParent, 0)
	if a1 != a2 {
		t.Fatalf("DeriveSubaccountAddress not deterministic: %s vs %s", a1, a2)
	}

	// Different indices yield different addresses.
	a3 := sm.DeriveSubaccountAddress(testParent, 1)
	if a1 == a3 {
		t.Fatal("expected different addresses for different indices")
	}

	// Different parents yield different addresses.
	other := types.HexToAddress("0xbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb")
	a4 := sm.DeriveSubaccountAddress(other, 0)
	if a1 == a4 {
		t.Fatal("expected different addresses for different parents")
	}

	// Address must not be zero.
	if a1.IsZero() {
		t.Fatal("derived address should not be zero")
	}
}

func TestCreateSubaccountBasic(t *testing.T) {
	sm := NewSubaccountManager()

	addr, err := sm.CreateSubaccount(testParent, 0)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if addr.IsZero() {
		t.Fatal("subaccount address should not be zero")
	}

	// Verify the address matches DeriveSubaccountAddress.
	expected := sm.DeriveSubaccountAddress(testParent, 0)
	if addr != expected {
		t.Fatalf("created address %s != derived %s", addr, expected)
	}
}

func TestCreateSubaccountDuplicate(t *testing.T) {
	sm := NewSubaccountManager()

	_, err := sm.CreateSubaccount(testParent, 0)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	_, err = sm.CreateSubaccount(testParent, 0)
	if err != ErrSubaccountExists {
		t.Fatalf("expected ErrSubaccountExists, got %v", err)
	}
}

func TestCreateSubaccountIndexOutOfRange(t *testing.T) {
	sm := NewSubaccountManager()

	_, err := sm.CreateSubaccount(testParent, MaxSubaccountsPerParent)
	if err != ErrIndexOutOfRange {
		t.Fatalf("expected ErrIndexOutOfRange, got %v", err)
	}

	_, err = sm.CreateSubaccount(testParent, MaxSubaccountsPerParent+100)
	if err != ErrIndexOutOfRange {
		t.Fatalf("expected ErrIndexOutOfRange for large index, got %v", err)
	}
}

func TestGetParent(t *testing.T) {
	sm := NewSubaccountManager()

	addr, _ := sm.CreateSubaccount(testParent, 5)

	parent, ok := sm.GetParent(addr)
	if !ok {
		t.Fatal("expected to find parent")
	}
	if parent != testParent {
		t.Fatalf("expected parent %s, got %s", testParent, parent)
	}

	// Unknown address returns false.
	unknown := types.HexToAddress("0xdead")
	_, ok = sm.GetParent(unknown)
	if ok {
		t.Fatal("expected false for unknown address")
	}
}

func TestListSubaccounts(t *testing.T) {
	sm := NewSubaccountManager()

	// Empty list for unknown parent.
	list := sm.ListSubaccounts(testParent)
	if list != nil {
		t.Fatalf("expected nil for parent with no subaccounts, got %v", list)
	}

	// Create several subaccounts.
	var addrs []types.Address
	for i := uint32(0); i < 5; i++ {
		addr, err := sm.CreateSubaccount(testParent, i)
		if err != nil {
			t.Fatalf("unexpected error creating index %d: %v", i, err)
		}
		addrs = append(addrs, addr)
	}

	list = sm.ListSubaccounts(testParent)
	if len(list) != 5 {
		t.Fatalf("expected 5 subaccounts, got %d", len(list))
	}
	for i, a := range list {
		if a != addrs[i] {
			t.Fatalf("list[%d] = %s, want %s", i, a, addrs[i])
		}
	}

	// Ensure returned slice is a copy (mutations don't affect manager).
	list[0] = types.Address{}
	original := sm.ListSubaccounts(testParent)
	if original[0] != addrs[0] {
		t.Fatal("ListSubaccounts must return a defensive copy")
	}
}

func TestIsSubaccount(t *testing.T) {
	sm := NewSubaccountManager()

	addr, _ := sm.CreateSubaccount(testParent, 0)

	if !sm.IsSubaccount(addr) {
		t.Fatal("expected addr to be a subaccount")
	}
	if sm.IsSubaccount(testParent) {
		t.Fatal("parent should not be considered a subaccount")
	}
	if sm.IsSubaccount(types.HexToAddress("0xdead")) {
		t.Fatal("random address should not be a subaccount")
	}
}

func TestTransferBetweenSubaccounts(t *testing.T) {
	sm := NewSubaccountManager()

	a1, _ := sm.CreateSubaccount(testParent, 0)
	a2, _ := sm.CreateSubaccount(testParent, 1)

	// Give a1 some balance.
	sm.mu.Lock()
	sm.states[a1].Balance = 1000
	sm.mu.Unlock()

	// Valid transfer.
	if err := sm.TransferBetweenSubaccounts(a1, a2, 300); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	s1, _ := sm.GetSubaccountState(a1)
	s2, _ := sm.GetSubaccountState(a2)
	if s1.Balance != 700 {
		t.Fatalf("expected from balance 700, got %d", s1.Balance)
	}
	if s2.Balance != 300 {
		t.Fatalf("expected to balance 300, got %d", s2.Balance)
	}
}

func TestTransferInsufficientBalance(t *testing.T) {
	sm := NewSubaccountManager()

	a1, _ := sm.CreateSubaccount(testParent, 0)
	a2, _ := sm.CreateSubaccount(testParent, 1)

	sm.mu.Lock()
	sm.states[a1].Balance = 100
	sm.mu.Unlock()

	err := sm.TransferBetweenSubaccounts(a1, a2, 200)
	if err != ErrInsufficientBalance {
		t.Fatalf("expected ErrInsufficientBalance, got %v", err)
	}
}

func TestTransferSameAccount(t *testing.T) {
	sm := NewSubaccountManager()

	a1, _ := sm.CreateSubaccount(testParent, 0)
	err := sm.TransferBetweenSubaccounts(a1, a1, 10)
	if err != ErrSameAccount {
		t.Fatalf("expected ErrSameAccount, got %v", err)
	}
}

func TestTransferDifferentParent(t *testing.T) {
	sm := NewSubaccountManager()
	otherParent := types.HexToAddress("0xcccccccccccccccccccccccccccccccccccccccc")

	a1, _ := sm.CreateSubaccount(testParent, 0)
	a2, _ := sm.CreateSubaccount(otherParent, 0)

	sm.mu.Lock()
	sm.states[a1].Balance = 100
	sm.mu.Unlock()

	err := sm.TransferBetweenSubaccounts(a1, a2, 50)
	if err != ErrDifferentParent {
		t.Fatalf("expected ErrDifferentParent, got %v", err)
	}
}

func TestTransferNonexistent(t *testing.T) {
	sm := NewSubaccountManager()

	a1, _ := sm.CreateSubaccount(testParent, 0)
	fake := types.HexToAddress("0xdead")

	err := sm.TransferBetweenSubaccounts(a1, fake, 10)
	if err == nil {
		t.Fatal("expected error for nonexistent target")
	}

	err = sm.TransferBetweenSubaccounts(fake, a1, 10)
	if err == nil {
		t.Fatal("expected error for nonexistent source")
	}
}

func TestGetSubaccountState(t *testing.T) {
	sm := NewSubaccountManager()

	addr, _ := sm.CreateSubaccount(testParent, 42)

	st, err := sm.GetSubaccountState(addr)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if st.Address != addr {
		t.Fatalf("state address mismatch")
	}
	if st.ParentAddress != testParent {
		t.Fatalf("state parent mismatch")
	}
	if st.Index != 42 {
		t.Fatalf("expected index 42, got %d", st.Index)
	}
	if st.Balance != 0 || st.Nonce != 0 {
		t.Fatal("new subaccount should have zero balance and nonce")
	}

	// Returned state is a copy - mutating it should not affect the manager.
	st.Balance = 9999
	st2, _ := sm.GetSubaccountState(addr)
	if st2.Balance != 0 {
		t.Fatal("GetSubaccountState must return a defensive copy")
	}
}

func TestGetSubaccountStateNotFound(t *testing.T) {
	sm := NewSubaccountManager()

	_, err := sm.GetSubaccountState(types.HexToAddress("0xdead"))
	if err != ErrSubaccountNotFound {
		t.Fatalf("expected ErrSubaccountNotFound, got %v", err)
	}
}

func TestMultipleParents(t *testing.T) {
	sm := NewSubaccountManager()

	p1 := types.HexToAddress("0x1111111111111111111111111111111111111111")
	p2 := types.HexToAddress("0x2222222222222222222222222222222222222222")

	a1, _ := sm.CreateSubaccount(p1, 0)
	a2, _ := sm.CreateSubaccount(p2, 0)

	// Both exist independently.
	if !sm.IsSubaccount(a1) || !sm.IsSubaccount(a2) {
		t.Fatal("both subaccounts should exist")
	}

	parent1, _ := sm.GetParent(a1)
	parent2, _ := sm.GetParent(a2)
	if parent1 != p1 || parent2 != p2 {
		t.Fatal("parents should map correctly")
	}

	if len(sm.ListSubaccounts(p1)) != 1 || len(sm.ListSubaccounts(p2)) != 1 {
		t.Fatal("each parent should have exactly 1 subaccount")
	}
}

func TestConcurrentAccess(t *testing.T) {
	sm := NewSubaccountManager()
	const goroutines = 50

	var wg sync.WaitGroup
	wg.Add(goroutines)

	for i := 0; i < goroutines; i++ {
		go func(idx int) {
			defer wg.Done()
			_, _ = sm.CreateSubaccount(testParent, uint32(idx))
			sm.IsSubaccount(sm.DeriveSubaccountAddress(testParent, uint32(idx)))
			sm.ListSubaccounts(testParent)
			sm.GetParent(sm.DeriveSubaccountAddress(testParent, uint32(idx)))
		}(i)
	}

	wg.Wait()

	// All 50 should have been created (indices 0-49 are all unique).
	list := sm.ListSubaccounts(testParent)
	if len(list) != goroutines {
		t.Fatalf("expected %d subaccounts after concurrent creation, got %d", goroutines, len(list))
	}
}

func TestZeroAmountTransfer(t *testing.T) {
	sm := NewSubaccountManager()

	a1, _ := sm.CreateSubaccount(testParent, 0)
	a2, _ := sm.CreateSubaccount(testParent, 1)

	// Zero-amount transfer should succeed.
	if err := sm.TransferBetweenSubaccounts(a1, a2, 0); err != nil {
		t.Fatalf("zero transfer should succeed: %v", err)
	}
}

func TestMaxSubaccountsConst(t *testing.T) {
	if MaxSubaccountsPerParent != 1_000_000 {
		t.Fatalf("MaxSubaccountsPerParent should be 1000000, got %d", MaxSubaccountsPerParent)
	}
}
