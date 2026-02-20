package vops

import (
	"math/big"
	"sync"
	"testing"

	"github.com/eth2028/eth2028/core/types"
	"github.com/eth2028/eth2028/witness"
)

// extPartialState builds a richer partial state for extended tests,
// distinct from makeTestState in executor_test.go.
func extPartialState() *PartialState {
	ps := NewPartialState()
	sender := types.BytesToAddress([]byte{0x10})
	ps.SetAccount(sender, &AccountState{
		Nonce:    0,
		Balance:  big.NewInt(5_000_000_000),
		CodeHash: types.EmptyCodeHash,
	})
	recipient := types.BytesToAddress([]byte{0x20})
	ps.SetAccount(recipient, &AccountState{
		Nonce:    0,
		Balance:  big.NewInt(1000),
		CodeHash: types.EmptyCodeHash,
	})
	return ps
}

func extHeader() *types.Header {
	return &types.Header{
		Number:   big.NewInt(10),
		Coinbase: types.BytesToAddress([]byte{0xFE}),
	}
}

func extTx(sender, to types.Address, value int64, nonce uint64, gas uint64, gasPrice int64) *types.Transaction {
	recipient := to
	tx := types.NewTransaction(&types.LegacyTx{
		Nonce:    nonce,
		GasPrice: big.NewInt(gasPrice),
		Gas:      gas,
		To:       &recipient,
		Value:    big.NewInt(value),
	})
	tx.SetSender(sender)
	return tx
}

// ---------- Partial State Execution ----------

func TestExtExecute_GasRefundMechanics(t *testing.T) {
	// Verify exact gas refund calculation: sender pays gasCost upfront,
	// then gets refunded for unused gas.
	pe := NewPartialExecutor(DefaultVOPSConfig())
	ps := extPartialState()
	sender := types.BytesToAddress([]byte{0x10})
	recipient := types.BytesToAddress([]byte{0x20})
	header := extHeader()

	gasLimit := uint64(200000)
	gasPrice := int64(2)
	tx := extTx(sender, recipient, 50, 0, gasLimit, gasPrice)

	result, err := pe.Execute(tx, ps, header)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !result.Success {
		t.Error("expected success")
	}
	if result.GasUsed != 21000 {
		t.Errorf("GasUsed = %d, want 21000", result.GasUsed)
	}

	// Sender balance = initial - value - gasUsed*gasPrice
	// = 5000000000 - 50 - 21000*2 = 4999957950
	postSender := result.PostState.GetAccount(sender)
	expected := big.NewInt(5_000_000_000 - 50 - 21000*2)
	if postSender.Balance.Cmp(expected) != 0 {
		t.Errorf("sender balance = %s, want %s", postSender.Balance, expected)
	}
}

func TestExtExecute_CoinbaseReceivesFee(t *testing.T) {
	pe := NewPartialExecutor(DefaultVOPSConfig())
	ps := extPartialState()
	sender := types.BytesToAddress([]byte{0x10})
	recipient := types.BytesToAddress([]byte{0x20})
	header := extHeader()

	gasPrice := int64(5)
	tx := extTx(sender, recipient, 0, 0, 100000, gasPrice)

	result, err := pe.Execute(tx, ps, header)
	if err != nil {
		t.Fatal(err)
	}

	coinbase := result.PostState.GetAccount(header.Coinbase)
	if coinbase == nil {
		t.Fatal("coinbase not in post state")
	}
	// Fee = gasUsed * gasPrice = 21000 * 5 = 105000
	expectedFee := big.NewInt(21000 * 5)
	if coinbase.Balance.Cmp(expectedFee) != 0 {
		t.Errorf("coinbase balance = %s, want %s", coinbase.Balance, expectedFee)
	}
}

func TestExtExecute_NonceMismatchCases(t *testing.T) {
	tests := []struct {
		name       string
		stateNonce uint64
		txNonce    uint64
		wantErr    error
	}{
		{"too_high", 0, 5, ErrNonceMismatch},
		{"too_low", 5, 3, ErrNonceMismatch},
		{"exact_match", 7, 7, nil},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			pe := NewPartialExecutor(DefaultVOPSConfig())
			ps := NewPartialState()
			sender := types.BytesToAddress([]byte{0x10})
			ps.SetAccount(sender, &AccountState{
				Nonce:    tc.stateNonce,
				Balance:  big.NewInt(1_000_000_000),
				CodeHash: types.EmptyCodeHash,
			})
			recipient := types.BytesToAddress([]byte{0x20})
			ps.SetAccount(recipient, &AccountState{
				Balance:  big.NewInt(0),
				CodeHash: types.EmptyCodeHash,
			})
			tx := extTx(sender, recipient, 0, tc.txNonce, 100000, 1)
			_, err := pe.Execute(tx, ps, extHeader())
			if err != tc.wantErr {
				t.Errorf("err = %v, want %v", err, tc.wantErr)
			}
		})
	}
}

func TestExtExecute_InsufficientBalanceEdge(t *testing.T) {
	pe := NewPartialExecutor(DefaultVOPSConfig())
	ps := NewPartialState()
	sender := types.BytesToAddress([]byte{0x10})
	// Balance = gasCost + value - 1 (one short).
	// gasCost = 100000 * 1 = 100000, value = 100.
	// Need 100100, have 100099.
	ps.SetAccount(sender, &AccountState{
		Nonce:    0,
		Balance:  big.NewInt(100099),
		CodeHash: types.EmptyCodeHash,
	})
	recipient := types.BytesToAddress([]byte{0x20})
	ps.SetAccount(recipient, &AccountState{
		Balance:  big.NewInt(0),
		CodeHash: types.EmptyCodeHash,
	})

	tx := extTx(sender, recipient, 100, 0, 100000, 1)
	_, err := pe.Execute(tx, ps, extHeader())
	if err != ErrInsufficientBal {
		t.Errorf("err = %v, want ErrInsufficientBal", err)
	}
}

func TestExtExecute_ExactBalance(t *testing.T) {
	pe := NewPartialExecutor(DefaultVOPSConfig())
	ps := NewPartialState()
	sender := types.BytesToAddress([]byte{0x10})
	// gasCost = 100000*1=100000, value=100. Total=100100.
	ps.SetAccount(sender, &AccountState{
		Nonce:    0,
		Balance:  big.NewInt(100100),
		CodeHash: types.EmptyCodeHash,
	})
	recipient := types.BytesToAddress([]byte{0x20})
	ps.SetAccount(recipient, &AccountState{
		Balance:  big.NewInt(0),
		CodeHash: types.EmptyCodeHash,
	})

	tx := extTx(sender, recipient, 100, 0, 100000, 1)
	result, err := pe.Execute(tx, ps, extHeader())
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	// Sender gets refund for unused gas.
	// Sender balance = 100100 - 100100 + refund((100000-21000)*1) = 79000
	postSender := result.PostState.GetAccount(sender)
	expected := big.NewInt(79000)
	if postSender.Balance.Cmp(expected) != 0 {
		t.Errorf("sender balance = %s, want %s", postSender.Balance, expected)
	}
}

// ---------- Witness-Backed State ----------

func TestExtBuildPartialStateFromWitness_BalanceAndNonce(t *testing.T) {
	var stem [31]byte
	stem[0] = 0xAA

	// Encode balance = 1000 (big-endian, SetBytes is big-endian).
	balVal := [32]byte{}
	balVal[30] = 0x03
	balVal[31] = 0xE8 // 1000 = 0x03E8

	// Encode nonce = 42 (little-endian per bytesToUint64).
	nonceVal := [32]byte{}
	nonceVal[0] = 42

	w := &witness.ExecutionWitness{
		ParentRoot: types.HexToHash("0x01"),
		State: []witness.StemStateDiff{
			{
				Stem: stem,
				Suffixes: []witness.SuffixStateDiff{
					{Suffix: 1, CurrentValue: &balVal},
					{Suffix: 2, CurrentValue: &nonceVal},
				},
			},
		},
	}

	ps := BuildPartialStateFromWitness(w)
	if len(ps.Accounts) != 1 {
		t.Fatalf("expected 1 account, got %d", len(ps.Accounts))
	}

	// Find the account by the derived address.
	for _, acct := range ps.Accounts {
		if acct.Balance.Int64() != 1000 {
			t.Errorf("balance = %d, want 1000", acct.Balance.Int64())
		}
		if acct.Nonce != 42 {
			t.Errorf("nonce = %d, want 42", acct.Nonce)
		}
	}
}

func TestExtBuildPartialStateFromWitness_CodeHash(t *testing.T) {
	var stem [31]byte
	stem[0] = 0xBB

	codeHash := [32]byte{}
	codeHash[0] = 0xDE
	codeHash[1] = 0xAD

	w := &witness.ExecutionWitness{
		ParentRoot: types.HexToHash("0x01"),
		State: []witness.StemStateDiff{
			{
				Stem: stem,
				Suffixes: []witness.SuffixStateDiff{
					{Suffix: 3, CurrentValue: &codeHash},
				},
			},
		},
	}

	ps := BuildPartialStateFromWitness(w)
	for _, acct := range ps.Accounts {
		if acct.CodeHash[0] != 0xDE || acct.CodeHash[1] != 0xAD {
			t.Errorf("code hash mismatch: %x", acct.CodeHash)
		}
	}
}

func TestExtBuildPartialStateFromWitness_StorageSlots(t *testing.T) {
	var stem [31]byte
	stem[0] = 0xCC

	// Suffixes >= 4 are treated as storage.
	storageVal := [32]byte{0xFF}

	w := &witness.ExecutionWitness{
		ParentRoot: types.HexToHash("0x01"),
		State: []witness.StemStateDiff{
			{
				Stem: stem,
				Suffixes: []witness.SuffixStateDiff{
					{Suffix: 64, CurrentValue: &storageVal},
				},
			},
		},
	}

	ps := BuildPartialStateFromWitness(w)
	// Should have an account and a storage entry.
	if len(ps.Accounts) != 1 {
		t.Fatalf("expected 1 account, got %d", len(ps.Accounts))
	}
	if len(ps.Storage) != 1 {
		t.Fatalf("expected 1 storage entry, got %d", len(ps.Storage))
	}
}

func TestExtBuildPartialStateFromWitness_NilCurrentValue(t *testing.T) {
	var stem [31]byte
	stem[0] = 0xDD

	w := &witness.ExecutionWitness{
		ParentRoot: types.HexToHash("0x01"),
		State: []witness.StemStateDiff{
			{
				Stem: stem,
				Suffixes: []witness.SuffixStateDiff{
					{Suffix: 1, CurrentValue: nil}, // nil should be skipped
				},
			},
		},
	}

	ps := BuildPartialStateFromWitness(w)
	if len(ps.Accounts) != 0 {
		t.Errorf("expected 0 accounts for nil current values, got %d", len(ps.Accounts))
	}
}

// ---------- Stateless Block Validation ----------

func TestExtStatelessValidation_RoundTrip(t *testing.T) {
	preRoot := types.HexToHash("0x1000")
	postRoot := types.HexToHash("0x2000")
	keys := [][]byte{{0x01}, {0x02}, {0x03}}

	proof := BuildValidityProof(preRoot, postRoot, keys)
	if !ValidateTransition(preRoot, postRoot, proof) {
		t.Error("round-trip proof should validate")
	}
}

func TestExtStatelessValidation_TamperedKey(t *testing.T) {
	preRoot := types.HexToHash("0x1000")
	postRoot := types.HexToHash("0x2000")
	originalKeys := [][]byte{{0x01}, {0x02}}

	proof := BuildValidityProof(preRoot, postRoot, originalKeys)

	// Create a different set of keys and build proof from those.
	tamperedKeys := [][]byte{{0xFF}, {0x02}}
	tamperedProof := BuildValidityProof(preRoot, postRoot, tamperedKeys)

	// Try to validate with original keys but tampered proof data.
	if ValidateTransition(preRoot, postRoot, &ValidityProof{
		PreStateRoot:  preRoot,
		PostStateRoot: postRoot,
		AccessedKeys:  originalKeys,
		ProofData:     tamperedProof.ProofData,
	}) {
		t.Error("tampered proof data should not validate against original keys")
	}

	// Also verify that the original proof still validates.
	if !ValidateTransition(preRoot, postRoot, proof) {
		t.Error("original proof should still validate")
	}
}

func TestExtStatelessValidation_AdditionalKey(t *testing.T) {
	preRoot := types.HexToHash("0x1000")
	postRoot := types.HexToHash("0x2000")
	keys := [][]byte{{0x01}}

	proof := BuildValidityProof(preRoot, postRoot, keys)

	// Add an extra key -- proof data was committed over 1 key, not 2.
	proof.AccessedKeys = append(proof.AccessedKeys, []byte{0x02})
	// The proof data doesn't change, so it should fail.
	if ValidateTransition(preRoot, postRoot, proof) {
		t.Error("proof with extra key should fail when commitment doesn't match")
	}
}

// ---------- VOPSValidator Extended ----------

func TestExtVOPSValidator_TransitionWithLargeWitness(t *testing.T) {
	v := NewVOPSValidator()
	preRoot := types.HexToHash("0xAA")
	witnessData := make([]byte, 10000)
	for i := range witnessData {
		witnessData[i] = byte(i % 256)
	}
	if err := v.AddWitness(preRoot, witnessData); err != nil {
		t.Fatal(err)
	}

	block := []byte{0x01, 0x02, 0x03}
	expectedPost := computePostRoot(preRoot, witnessData, block)

	ok, err := v.ValidateTransition(preRoot, expectedPost, block)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !ok {
		t.Error("large witness transition should validate")
	}
	if v.WitnessSize() != 10000 {
		t.Errorf("witness size = %d, want 10000", v.WitnessSize())
	}
}

func TestExtVOPSValidator_MultipleWitnesses(t *testing.T) {
	v := NewVOPSValidator()
	for i := 0; i < 10; i++ {
		root := types.BytesToHash([]byte{byte(i)})
		data := []byte{byte(i), byte(i + 1)}
		if err := v.AddWitness(root, data); err != nil {
			t.Fatal(err)
		}
	}
	if v.WitnessSize() != 20 {
		t.Errorf("total witness size = %d, want 20", v.WitnessSize())
	}
}

func TestExtVOPSValidator_ResetClearsAll(t *testing.T) {
	v := NewVOPSValidator()
	root := types.HexToHash("0x01")
	_ = v.AddWitness(root, []byte{0xAA})
	v.AddAccessListEntry(types.BytesToAddress([]byte{0x01}))
	v.AddStorageProof(types.HexToHash("0x10"), [][]byte{{0x01}})

	v.Reset()

	if v.WitnessSize() != 0 {
		t.Error("witnesses should be cleared after reset")
	}
	if len(v.AccessedAddresses()) != 0 {
		t.Error("access list should be cleared after reset")
	}
}

// ---------- Missing Witness Detection ----------

func TestExtVOPSValidator_MissingWitnessError(t *testing.T) {
	v := NewVOPSValidator()
	_, err := v.ValidateTransition(types.HexToHash("0x01"), types.HexToHash("0x02"), []byte{0x01})
	if err != ErrWitnessNotFound {
		t.Errorf("err = %v, want ErrWitnessNotFound", err)
	}
}

func TestExtVOPSValidator_EmptyBlockError(t *testing.T) {
	v := NewVOPSValidator()
	root := types.HexToHash("0x01")
	_ = v.AddWitness(root, []byte{0xAA})

	_, err := v.ValidateTransition(root, types.HexToHash("0x02"), nil)
	if err != ErrEmptyBlock {
		t.Errorf("err = %v, want ErrEmptyBlock", err)
	}

	_, err = v.ValidateTransition(root, types.HexToHash("0x02"), []byte{})
	if err != ErrEmptyBlock {
		t.Errorf("err = %v, want ErrEmptyBlock for empty slice", err)
	}
}

// ---------- Contract Creation Extended ----------

func TestExtExecute_ContractCreationWithData(t *testing.T) {
	pe := NewPartialExecutor(DefaultVOPSConfig())
	ps := NewPartialState()
	sender := types.BytesToAddress([]byte{0x10})
	ps.SetAccount(sender, &AccountState{
		Nonce:    3,
		Balance:  big.NewInt(50_000_000),
		CodeHash: types.EmptyCodeHash,
	})

	initCode := []byte{0x60, 0x00, 0x60, 0x00, 0x60, 0x00, 0x60, 0x00, 0xf1}
	tx := types.NewTransaction(&types.LegacyTx{
		Nonce:    3,
		GasPrice: big.NewInt(1),
		Gas:      500000,
		To:       nil,
		Value:    big.NewInt(1000),
		Data:     initCode,
	})
	tx.SetSender(sender)

	result, err := pe.Execute(tx, ps, extHeader())
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !result.Success {
		t.Error("contract creation should succeed")
	}
	// Gas used = 21000 + len(initCode)*16 = 21000 + 144 = 21144
	expectedGas := uint64(21000 + len(initCode)*16)
	if result.GasUsed != expectedGas {
		t.Errorf("GasUsed = %d, want %d", result.GasUsed, expectedGas)
	}
	// Sender nonce should increment.
	postSender := result.PostState.GetAccount(sender)
	if postSender.Nonce != 4 {
		t.Errorf("sender nonce = %d, want 4", postSender.Nonce)
	}

	// A new contract address should be in post state.
	contractAddr := createAddress(sender, 3)
	contractAcct := result.PostState.GetAccount(contractAddr)
	if contractAcct == nil {
		t.Fatal("contract account not in post state")
	}
	if contractAcct.Balance.Int64() != 1000 {
		t.Errorf("contract balance = %d, want 1000", contractAcct.Balance.Int64())
	}
}

// ---------- Collect Accessed State ----------

// extFullState implements FullStateReader for testing.
type extFullState struct {
	balances    map[types.Address]*big.Int
	nonces      map[types.Address]uint64
	codeHashes  map[types.Address]types.Hash
	codes       map[types.Address][]byte
	storage     map[types.Address]map[types.Hash]types.Hash
	storageRoot map[types.Address]types.Hash
}

func newExtFullState() *extFullState {
	return &extFullState{
		balances:    make(map[types.Address]*big.Int),
		nonces:      make(map[types.Address]uint64),
		codeHashes:  make(map[types.Address]types.Hash),
		codes:       make(map[types.Address][]byte),
		storage:     make(map[types.Address]map[types.Hash]types.Hash),
		storageRoot: make(map[types.Address]types.Hash),
	}
}

func (f *extFullState) GetBalance(addr types.Address) *big.Int {
	if b, ok := f.balances[addr]; ok {
		return new(big.Int).Set(b)
	}
	return new(big.Int)
}

func (f *extFullState) GetNonce(addr types.Address) uint64 {
	return f.nonces[addr]
}

func (f *extFullState) GetCodeHash(addr types.Address) types.Hash {
	if h, ok := f.codeHashes[addr]; ok {
		return h
	}
	return types.EmptyCodeHash
}

func (f *extFullState) GetCode(addr types.Address) []byte {
	return f.codes[addr]
}

func (f *extFullState) GetState(addr types.Address, key types.Hash) types.Hash {
	if slots, ok := f.storage[addr]; ok {
		return slots[key]
	}
	return types.Hash{}
}

func (f *extFullState) StorageRoot(addr types.Address) types.Hash {
	return f.storageRoot[addr]
}

func TestExtCollectAccessedState_SenderAndRecipient(t *testing.T) {
	pe := NewPartialExecutor(DefaultVOPSConfig())
	fs := newExtFullState()

	sender := types.BytesToAddress([]byte{0x10})
	recipient := types.BytesToAddress([]byte{0x20})
	fs.balances[sender] = big.NewInt(1_000_000)
	fs.nonces[sender] = 5
	fs.balances[recipient] = big.NewInt(500)

	tx := extTx(sender, recipient, 100, 5, 100000, 1)

	ps, err := pe.CollectAccessedState(tx, fs)
	if err != nil {
		t.Fatal(err)
	}
	if ps.GetAccount(sender) == nil {
		t.Error("sender should be in partial state")
	}
	if ps.GetAccount(recipient) == nil {
		t.Error("recipient should be in partial state")
	}
	if ps.GetAccount(sender).Nonce != 5 {
		t.Errorf("sender nonce = %d, want 5", ps.GetAccount(sender).Nonce)
	}
	if ps.GetAccount(sender).Balance.Int64() != 1_000_000 {
		t.Errorf("sender balance = %s, want 1000000", ps.GetAccount(sender).Balance)
	}
}

func TestExtCollectAccessedState_WithAccessList(t *testing.T) {
	pe := NewPartialExecutor(DefaultVOPSConfig())
	fs := newExtFullState()

	sender := types.BytesToAddress([]byte{0x10})
	alAddr := types.BytesToAddress([]byte{0x30})
	fs.balances[sender] = big.NewInt(1_000_000)
	fs.nonces[sender] = 0
	fs.balances[alAddr] = big.NewInt(999)
	fs.storage[alAddr] = map[types.Hash]types.Hash{
		types.HexToHash("0x01"): types.HexToHash("0xAA"),
		types.HexToHash("0x02"): types.HexToHash("0xBB"),
	}

	recipient := types.BytesToAddress([]byte{0x20})
	fs.balances[recipient] = big.NewInt(0)

	tx := types.NewTransaction(&types.AccessListTx{
		Nonce:    0,
		GasPrice: big.NewInt(1),
		Gas:      100000,
		To:       &recipient,
		Value:    big.NewInt(0),
		AccessList: types.AccessList{
			{
				Address:     alAddr,
				StorageKeys: []types.Hash{types.HexToHash("0x01"), types.HexToHash("0x02")},
			},
		},
	})
	tx.SetSender(sender)

	ps, err := pe.CollectAccessedState(tx, fs)
	if err != nil {
		t.Fatal(err)
	}
	if ps.GetAccount(alAddr) == nil {
		t.Error("access list address should be in partial state")
	}
	if ps.GetStorage(alAddr, types.HexToHash("0x01")) != types.HexToHash("0xAA") {
		t.Error("access list storage slot 0x01 not recorded correctly")
	}
	if ps.GetStorage(alAddr, types.HexToHash("0x02")) != types.HexToHash("0xBB") {
		t.Error("access list storage slot 0x02 not recorded correctly")
	}
}

func TestExtCollectAccessedState_NilSender(t *testing.T) {
	pe := NewPartialExecutor(DefaultVOPSConfig())
	fs := newExtFullState()

	// Transaction with no sender set.
	tx := types.NewTransaction(&types.LegacyTx{
		Nonce:    0,
		GasPrice: big.NewInt(1),
		Gas:      21000,
		Value:    big.NewInt(0),
	})

	_, err := pe.CollectAccessedState(tx, fs)
	if err != ErrMissingSender {
		t.Errorf("err = %v, want ErrMissingSender", err)
	}
}

func TestExtCollectAccessedState_CodeCaptured(t *testing.T) {
	pe := NewPartialExecutor(DefaultVOPSConfig())
	fs := newExtFullState()

	sender := types.BytesToAddress([]byte{0x10})
	contract := types.BytesToAddress([]byte{0x20})
	fs.balances[sender] = big.NewInt(1_000_000)
	fs.balances[contract] = big.NewInt(0)
	fs.codes[contract] = []byte{0x60, 0x00, 0xFD} // PUSH0 REVERT
	fs.codeHashes[contract] = types.HexToHash("0xC0DE")

	tx := extTx(sender, contract, 0, 0, 100000, 1)

	ps, err := pe.CollectAccessedState(tx, fs)
	if err != nil {
		t.Fatal(err)
	}
	if code, ok := ps.Code[contract]; !ok || len(code) != 3 {
		t.Errorf("contract code not captured: ok=%v, len=%d", ok, len(code))
	}
}

// ---------- Thread Safety ----------

func TestExtVOPSValidator_ConcurrentAccess(t *testing.T) {
	v := NewVOPSValidator()
	var wg sync.WaitGroup

	// Concurrent AddWitness.
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			root := types.BytesToHash([]byte{byte(idx)})
			_ = v.AddWitness(root, []byte{byte(idx), byte(idx + 1)})
		}(i)
	}
	// Concurrent AddAccessListEntry.
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			addr := types.BytesToAddress([]byte{byte(idx)})
			v.AddAccessListEntry(addr)
		}(i)
	}
	// Concurrent reads.
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_ = v.WitnessSize()
			_ = v.AccessedAddresses()
		}()
	}
	wg.Wait()

	if v.WitnessSize() != 40 { // 20 witnesses * 2 bytes each
		t.Errorf("witness size = %d, want 40", v.WitnessSize())
	}
}

// ---------- Clone Independence ----------

func TestExtClonePartialState_DeepCopy(t *testing.T) {
	ps := NewPartialState()
	addr := types.BytesToAddress([]byte{0x10})
	ps.SetAccount(addr, &AccountState{
		Nonce:    5,
		Balance:  big.NewInt(1000),
		CodeHash: types.EmptyCodeHash,
	})
	key := types.HexToHash("0x01")
	ps.SetStorage(addr, key, types.HexToHash("0xAA"))
	ps.Code[addr] = []byte{0x60, 0x00}

	clone := clonePartialState(ps)

	// Mutate clone.
	clone.GetAccount(addr).Nonce = 99
	clone.GetAccount(addr).Balance = big.NewInt(0)
	clone.SetStorage(addr, key, types.HexToHash("0xFF"))
	clone.Code[addr][0] = 0xFF

	// Verify original is unchanged.
	if ps.GetAccount(addr).Nonce != 5 {
		t.Errorf("original nonce changed to %d", ps.GetAccount(addr).Nonce)
	}
	if ps.GetAccount(addr).Balance.Int64() != 1000 {
		t.Errorf("original balance changed to %s", ps.GetAccount(addr).Balance)
	}
	if ps.GetStorage(addr, key) != types.HexToHash("0xAA") {
		t.Error("original storage changed")
	}
	if ps.Code[addr][0] != 0x60 {
		t.Error("original code changed")
	}
}
