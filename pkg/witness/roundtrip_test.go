package witness

import (
	"math/big"
	"testing"

	"github.com/eth2030/eth2030/core/state"
	"github.com/eth2030/eth2030/core/types"
)

// TestRoundTripValueTransfer simulates a value transfer, collects the witness,
// then replays the same operations using only the witness data and checks that
// the final state matches.
func TestRoundTripValueTransfer(t *testing.T) {
	// --- Phase 1: Execute with WitnessCollector ---
	sdb := state.NewMemoryStateDB()
	from := types.HexToAddress("0xaaaa")
	to := types.HexToAddress("0xbbbb")
	coinbase := types.HexToAddress("0xcccc")

	sdb.CreateAccount(from)
	sdb.AddBalance(from, big.NewInt(10_000))
	sdb.SetNonce(from, 0)
	sdb.CreateAccount(to)
	sdb.AddBalance(to, big.NewInt(500))
	sdb.CreateAccount(coinbase)

	w := NewBlockWitness()
	collector := NewWitnessCollector(sdb, w)

	// Simulate a simple value transfer: from sends 200 to to.
	// Check nonce.
	nonce := collector.GetNonce(from)
	if nonce != 0 {
		t.Fatalf("unexpected nonce: %d", nonce)
	}
	// Check balance.
	bal := collector.GetBalance(from)
	if bal.Cmp(big.NewInt(10_000)) != 0 {
		t.Fatalf("unexpected from balance: %s", bal)
	}
	// Deduct gas cost (simulated: 21000 * 1 gwei = 21000).
	gasCost := big.NewInt(21000)
	collector.SubBalance(from, gasCost)
	// Transfer 200.
	collector.SubBalance(from, big.NewInt(200))
	collector.AddBalance(to, big.NewInt(200))
	// Increment nonce.
	collector.SetNonce(from, nonce+1)
	// Pay coinbase.
	collector.GetBalance(coinbase)
	collector.AddBalance(coinbase, big.NewInt(21000))

	// Record final state from collector.
	finalFromBal := collector.GetBalance(from)
	finalToBal := collector.GetBalance(to)
	finalFromNonce := collector.GetNonce(from)
	finalCoinbaseBal := collector.GetBalance(coinbase)

	// --- Phase 2: Replay with WitnessStateDB ---
	verifier := NewWitnessStateDB(w)

	// Check that verifier has the same pre-state.
	if verifier.GetBalance(from).Cmp(big.NewInt(10_000)) != 0 {
		t.Fatalf("verifier pre-state from balance = %s, want 10000", verifier.GetBalance(from))
	}
	if verifier.GetBalance(to).Cmp(big.NewInt(500)) != 0 {
		t.Fatalf("verifier pre-state to balance = %s, want 500", verifier.GetBalance(to))
	}

	// Replay the same operations.
	vNonce := verifier.GetNonce(from)
	if vNonce != 0 {
		t.Fatalf("verifier nonce = %d, want 0", vNonce)
	}
	verifier.SubBalance(from, gasCost)
	verifier.SubBalance(from, big.NewInt(200))
	verifier.AddBalance(to, big.NewInt(200))
	verifier.SetNonce(from, vNonce+1)
	verifier.AddBalance(coinbase, big.NewInt(21000))

	// --- Phase 3: Compare final states ---
	if verifier.GetBalance(from).Cmp(finalFromBal) != 0 {
		t.Fatalf("from balance mismatch: collector=%s, verifier=%s",
			finalFromBal, verifier.GetBalance(from))
	}
	if verifier.GetBalance(to).Cmp(finalToBal) != 0 {
		t.Fatalf("to balance mismatch: collector=%s, verifier=%s",
			finalToBal, verifier.GetBalance(to))
	}
	if verifier.GetNonce(from) != finalFromNonce {
		t.Fatalf("from nonce mismatch: collector=%d, verifier=%d",
			finalFromNonce, verifier.GetNonce(from))
	}
	if verifier.GetBalance(coinbase).Cmp(finalCoinbaseBal) != 0 {
		t.Fatalf("coinbase balance mismatch: collector=%s, verifier=%s",
			finalCoinbaseBal, verifier.GetBalance(coinbase))
	}
}

// TestRoundTripStorageWrite simulates a contract call that reads and writes
// storage, collects the witness, and replays with the verifier.
func TestRoundTripStorageWrite(t *testing.T) {
	// --- Phase 1: Execute with WitnessCollector ---
	sdb := state.NewMemoryStateDB()
	caller := types.HexToAddress("0xaaaa")
	contract := types.HexToAddress("0xbbbb")
	slot1 := types.HexToHash("0x01")
	slot2 := types.HexToHash("0x02")
	slot3 := types.HexToHash("0x03") // new slot, not in pre-state

	sdb.CreateAccount(caller)
	sdb.AddBalance(caller, big.NewInt(100_000))
	sdb.SetNonce(caller, 0)
	sdb.CreateAccount(contract)
	sdb.SetCode(contract, []byte{0x60, 0x00})
	sdb.SetState(contract, slot1, types.HexToHash("0xaa"))
	sdb.SetState(contract, slot2, types.HexToHash("0xbb"))

	w := NewBlockWitness()
	collector := NewWitnessCollector(sdb, w)

	// Simulate: read slot1, write slot1, read slot2, write slot3.
	val1 := collector.GetState(contract, slot1)
	if val1 != types.HexToHash("0xaa") {
		t.Fatalf("slot1 = %s, want 0xaa", val1.Hex())
	}
	collector.SetState(contract, slot1, types.HexToHash("0xcc"))

	val2 := collector.GetState(contract, slot2)
	if val2 != types.HexToHash("0xbb") {
		t.Fatalf("slot2 = %s, want 0xbb", val2.Hex())
	}

	// Write to a new slot.
	collector.SetState(contract, slot3, types.HexToHash("0xdd"))

	// Read code.
	code := collector.GetCode(contract)
	codeHash := collector.GetCodeHash(contract)

	// Deduct gas and increment nonce.
	collector.SubBalance(caller, big.NewInt(21000))
	collector.SetNonce(caller, 1)

	// Record final state.
	finalSlot1 := collector.GetState(contract, slot1)
	finalSlot2 := collector.GetState(contract, slot2)
	finalSlot3 := collector.GetState(contract, slot3)

	// --- Phase 2: Replay with WitnessStateDB ---
	verifier := NewWitnessStateDB(w)

	// Verify pre-state was captured.
	if verifier.GetState(contract, slot1) != types.HexToHash("0xaa") {
		t.Fatalf("verifier pre-state slot1 = %s, want 0xaa", verifier.GetState(contract, slot1).Hex())
	}
	if verifier.GetState(contract, slot2) != types.HexToHash("0xbb") {
		t.Fatalf("verifier pre-state slot2 = %s, want 0xbb", verifier.GetState(contract, slot2).Hex())
	}

	// Verify code.
	vCode := verifier.GetCode(contract)
	if len(vCode) != len(code) {
		t.Fatalf("verifier code length = %d, want %d", len(vCode), len(code))
	}
	if verifier.GetCodeHash(contract) != codeHash {
		t.Fatalf("verifier code hash mismatch")
	}

	// Replay writes.
	verifier.SetState(contract, slot1, types.HexToHash("0xcc"))
	verifier.SetState(contract, slot3, types.HexToHash("0xdd"))
	verifier.SubBalance(caller, big.NewInt(21000))
	verifier.SetNonce(caller, 1)

	// Compare final state.
	if verifier.GetState(contract, slot1) != finalSlot1 {
		t.Fatalf("slot1 mismatch: collector=%s, verifier=%s",
			finalSlot1.Hex(), verifier.GetState(contract, slot1).Hex())
	}
	if verifier.GetState(contract, slot2) != finalSlot2 {
		t.Fatalf("slot2 mismatch: collector=%s, verifier=%s",
			finalSlot2.Hex(), verifier.GetState(contract, slot2).Hex())
	}
	if verifier.GetState(contract, slot3) != finalSlot3 {
		t.Fatalf("slot3 mismatch: collector=%s, verifier=%s",
			finalSlot3.Hex(), verifier.GetState(contract, slot3).Hex())
	}
}

// TestRoundTripSnapshotRevert simulates a transaction that gets reverted,
// then verifies the revert works correctly on both collector and verifier.
func TestRoundTripSnapshotRevert(t *testing.T) {
	sdb := state.NewMemoryStateDB()
	addr := types.HexToAddress("0xaaaa")
	sdb.CreateAccount(addr)
	sdb.AddBalance(addr, big.NewInt(5000))
	sdb.SetNonce(addr, 2)

	w := NewBlockWitness()
	collector := NewWitnessCollector(sdb, w)

	// Take snapshot.
	snap := collector.Snapshot()

	// Make some changes.
	collector.AddBalance(addr, big.NewInt(1000))
	collector.SetNonce(addr, 3)

	// Revert.
	collector.RevertToSnapshot(snap)

	// Final state should be reverted.
	finalBal := collector.GetBalance(addr)
	finalNonce := collector.GetNonce(addr)
	if finalBal.Cmp(big.NewInt(5000)) != 0 {
		t.Fatalf("collector balance after revert = %s, want 5000", finalBal)
	}
	if finalNonce != 2 {
		t.Fatalf("collector nonce after revert = %d, want 2", finalNonce)
	}

	// Now replay on verifier.
	verifier := NewWitnessStateDB(w)

	vSnap := verifier.Snapshot()
	verifier.AddBalance(addr, big.NewInt(1000))
	verifier.SetNonce(addr, 3)
	verifier.RevertToSnapshot(vSnap)

	if verifier.GetBalance(addr).Cmp(finalBal) != 0 {
		t.Fatalf("verifier balance after revert = %s, want %s",
			verifier.GetBalance(addr), finalBal)
	}
	if verifier.GetNonce(addr) != finalNonce {
		t.Fatalf("verifier nonce after revert = %d, want %d",
			verifier.GetNonce(addr), finalNonce)
	}
}

// TestRoundTripMultipleAccounts tests witness generation and verification
// with multiple accounts and storage interactions.
func TestRoundTripMultipleAccounts(t *testing.T) {
	sdb := state.NewMemoryStateDB()
	addrs := []types.Address{
		types.HexToAddress("0x0001"),
		types.HexToAddress("0x0002"),
		types.HexToAddress("0x0003"),
	}
	for i, addr := range addrs {
		sdb.CreateAccount(addr)
		sdb.AddBalance(addr, big.NewInt(int64((i+1)*1000)))
		sdb.SetNonce(addr, uint64(i))
	}
	// Give the third one some storage.
	sdb.SetState(addrs[2], types.HexToHash("0x10"), types.HexToHash("0xab"))

	w := NewBlockWitness()
	collector := NewWitnessCollector(sdb, w)

	// Read all accounts.
	for _, addr := range addrs {
		collector.GetBalance(addr)
		collector.GetNonce(addr)
	}
	// Read storage on third.
	collector.GetState(addrs[2], types.HexToHash("0x10"))

	// Transfer: addr[0] sends 100 to addr[1].
	collector.SubBalance(addrs[0], big.NewInt(100))
	collector.AddBalance(addrs[1], big.NewInt(100))
	// Write storage on third.
	collector.SetState(addrs[2], types.HexToHash("0x10"), types.HexToHash("0xcd"))

	finalBalances := make([]*big.Int, len(addrs))
	for i, addr := range addrs {
		finalBalances[i] = collector.GetBalance(addr)
	}
	finalSlot := collector.GetState(addrs[2], types.HexToHash("0x10"))

	// Replay on verifier.
	verifier := NewWitnessStateDB(w)
	verifier.SubBalance(addrs[0], big.NewInt(100))
	verifier.AddBalance(addrs[1], big.NewInt(100))
	verifier.SetState(addrs[2], types.HexToHash("0x10"), types.HexToHash("0xcd"))

	for i, addr := range addrs {
		vBal := verifier.GetBalance(addr)
		if vBal.Cmp(finalBalances[i]) != 0 {
			t.Fatalf("addr[%d] balance mismatch: collector=%s, verifier=%s",
				i, finalBalances[i], vBal)
		}
	}
	if verifier.GetState(addrs[2], types.HexToHash("0x10")) != finalSlot {
		t.Fatalf("storage mismatch: collector=%s, verifier=%s",
			finalSlot.Hex(), verifier.GetState(addrs[2], types.HexToHash("0x10")).Hex())
	}
}

// TestRoundTripAccountCreation tests that creating a new account during
// execution is captured in the witness and works in the verifier.
func TestRoundTripAccountCreation(t *testing.T) {
	sdb := state.NewMemoryStateDB()
	creator := types.HexToAddress("0xaaaa")
	sdb.CreateAccount(creator)
	sdb.AddBalance(creator, big.NewInt(50_000))

	w := NewBlockWitness()
	collector := NewWitnessCollector(sdb, w)

	newAddr := types.HexToAddress("0xbbbb")

	// Check new address does not exist.
	exists := collector.Exist(newAddr)
	if exists {
		t.Fatal("new address should not exist yet")
	}

	// Create the account.
	collector.CreateAccount(newAddr)
	collector.AddBalance(newAddr, big.NewInt(100))
	collector.SetNonce(newAddr, 1)

	// Deduct from creator.
	collector.SubBalance(creator, big.NewInt(100))

	finalCreatorBal := collector.GetBalance(creator)
	finalNewBal := collector.GetBalance(newAddr)
	finalNewNonce := collector.GetNonce(newAddr)

	// Replay on verifier.
	verifier := NewWitnessStateDB(w)

	// The witness should have recorded that newAddr did not exist.
	if verifier.Exist(newAddr) {
		t.Fatal("verifier: new address should not exist before creation")
	}

	verifier.CreateAccount(newAddr)
	verifier.AddBalance(newAddr, big.NewInt(100))
	verifier.SetNonce(newAddr, 1)
	verifier.SubBalance(creator, big.NewInt(100))

	if verifier.GetBalance(creator).Cmp(finalCreatorBal) != 0 {
		t.Fatalf("creator balance mismatch: collector=%s, verifier=%s",
			finalCreatorBal, verifier.GetBalance(creator))
	}
	if verifier.GetBalance(newAddr).Cmp(finalNewBal) != 0 {
		t.Fatalf("new account balance mismatch: collector=%s, verifier=%s",
			finalNewBal, verifier.GetBalance(newAddr))
	}
	if verifier.GetNonce(newAddr) != finalNewNonce {
		t.Fatalf("new account nonce mismatch: collector=%d, verifier=%d",
			finalNewNonce, verifier.GetNonce(newAddr))
	}
}
