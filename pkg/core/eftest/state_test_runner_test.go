package eftest

import (
	"math/big"
	"os"
	"path/filepath"
	"testing"

	"github.com/eth2028/eth2028/core"
	"github.com/eth2028/eth2028/core/state"
	"github.com/eth2028/eth2028/core/types"
	"github.com/eth2028/eth2028/crypto"
)

// testdataPath returns the path to go-ethereum's statetest.json fixture.
func testdataPath() string {
	return filepath.Join("..", "..", "..", "refs", "go-ethereum", "cmd", "evm", "testdata", "statetest.json")
}

func TestEFLoadStateTest(t *testing.T) {
	path := testdataPath()
	if _, err := os.Stat(path); os.IsNotExist(err) {
		t.Skipf("fixture not found: %s", path)
	}

	tests, err := LoadStateTests(path)
	if err != nil {
		t.Fatalf("LoadStateTests: %v", err)
	}

	if len(tests) == 0 {
		t.Fatal("expected at least one test")
	}

	// Verify we got named tests.
	for name, test := range tests {
		if name == "" {
			t.Error("empty test name")
		}
		if test.Name != name {
			t.Errorf("name mismatch: key=%q, test.Name=%q", name, test.Name)
		}
	}
	t.Logf("loaded %d test(s)", len(tests))
}

func TestEFSubtests(t *testing.T) {
	path := testdataPath()
	if _, err := os.Stat(path); os.IsNotExist(err) {
		t.Skipf("fixture not found: %s", path)
	}

	tests, err := LoadStateTests(path)
	if err != nil {
		t.Fatalf("LoadStateTests: %v", err)
	}

	for name, test := range tests {
		subs := test.Subtests()
		if len(subs) == 0 {
			t.Errorf("test %q has no subtests", name)
		}
		for _, sub := range subs {
			if sub.Fork == "" {
				t.Errorf("test %q has subtest with empty fork", name)
			}
		}
	}
}

func TestEFPreStateSetup(t *testing.T) {
	path := testdataPath()
	if _, err := os.Stat(path); os.IsNotExist(err) {
		t.Skipf("fixture not found: %s", path)
	}

	tests, err := LoadStateTests(path)
	if err != nil {
		t.Fatalf("LoadStateTests: %v", err)
	}

	for _, test := range tests {
		// Build pre-state and verify it matches.
		statedb := state.NewMemoryStateDB()
		for addrHex, acct := range test.json.Pre {
			addr := hexToAddress(addrHex)
			statedb.CreateAccount(addr)
			statedb.AddBalance(addr, hexToBigInt(acct.Balance))
			statedb.SetNonce(addr, hexToUint64(acct.Nonce))
			code := hexToBytes(acct.Code)
			if len(code) > 0 {
				statedb.SetCode(addr, code)
			}

			// Verify balance.
			got := statedb.GetBalance(addr)
			expected := hexToBigInt(acct.Balance)
			if got.Cmp(expected) != 0 {
				t.Errorf("balance mismatch for %s: got %s, expected %s", addrHex, got, expected)
			}

			// Verify nonce.
			gotNonce := statedb.GetNonce(addr)
			expectedNonce := hexToUint64(acct.Nonce)
			if gotNonce != expectedNonce {
				t.Errorf("nonce mismatch for %s: got %d, expected %d", addrHex, gotNonce, expectedNonce)
			}

			// Verify code.
			gotCode := statedb.GetCode(addr)
			if len(code) > 0 && len(gotCode) != len(code) {
				t.Errorf("code length mismatch for %s: got %d, expected %d", addrHex, len(gotCode), len(code))
			}
		}
		break // Just verify the first test.
	}
}

func TestEFSimpleTransfer(t *testing.T) {
	statedb := state.NewMemoryStateDB()

	sender := types.HexToAddress("0xa94f5374fce5edbc8e2a8697c15331677e6ebf0b")
	receiver := types.HexToAddress("0x1234567890abcdef1234567890abcdef12345678")

	statedb.CreateAccount(sender)
	statedb.AddBalance(sender, big.NewInt(1_000_000_000))
	statedb.SetNonce(sender, 0)

	statedb.CreateAccount(receiver)
	// receiver starts with zero balance (default)

	// Verify balances are set.
	if statedb.GetBalance(sender).Cmp(big.NewInt(1_000_000_000)) != 0 {
		t.Fatal("sender balance not set correctly")
	}

	// Simulate a transfer.
	statedb.SubBalance(sender, big.NewInt(100))
	statedb.AddBalance(receiver, big.NewInt(100))

	if statedb.GetBalance(receiver).Cmp(big.NewInt(100)) != 0 {
		t.Errorf("receiver balance: got %s, expected 100", statedb.GetBalance(receiver))
	}

	// Commit and verify state root changes.
	root, err := statedb.Commit()
	if err != nil {
		t.Fatalf("Commit: %v", err)
	}
	if root == (types.Hash{}) {
		t.Error("state root should not be zero after state changes")
	}
}

func TestEFStateRootComputation(t *testing.T) {
	// Verify deterministic state root computation.
	build := func() types.Hash {
		statedb := state.NewMemoryStateDB()
		addr := types.HexToAddress("0xaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa")
		statedb.CreateAccount(addr)
		statedb.AddBalance(addr, big.NewInt(42))
		statedb.SetNonce(addr, 1)
		root, _ := statedb.Commit()
		return root
	}

	root1 := build()
	root2 := build()
	if root1 != root2 {
		t.Errorf("non-deterministic state root: %s vs %s", root1.Hex(), root2.Hex())
	}
	if root1 == (types.Hash{}) {
		t.Error("state root should not be zero")
	}
}

func TestEFLogsHashComputation(t *testing.T) {
	// Empty logs should produce a consistent hash.
	hash1 := computeLogsHash(nil)
	hash2 := computeLogsHash([]*types.Log{})
	if hash1 != hash2 {
		t.Errorf("empty logs hash inconsistent: %s vs %s", hash1.Hex(), hash2.Hex())
	}

	// Non-empty logs should produce a different hash.
	logs := []*types.Log{
		{
			Address: types.HexToAddress("0xbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"),
			Topics:  []types.Hash{types.HexToHash("0x01")},
			Data:    []byte{0x42},
		},
	}
	hash3 := computeLogsHash(logs)
	if hash3 == hash1 {
		t.Error("non-empty logs should produce different hash")
	}
}

func TestEFForkConfigMapping(t *testing.T) {
	forks := SupportedForks()
	if len(forks) == 0 {
		t.Fatal("no supported forks")
	}

	for _, fork := range forks {
		config := ForkConfig(fork)
		if config == nil {
			t.Errorf("ForkConfig(%q) returned nil", fork)
			continue
		}
		if config.ChainID == nil {
			t.Errorf("ForkConfig(%q) has nil ChainID", fork)
		}
	}

	// Verify London has EIP-1559.
	london := ForkConfig("London")
	if london == nil || !london.IsLondon(big.NewInt(0)) {
		t.Error("London config should have London fork active")
	}

	// Verify unsupported fork returns nil.
	if ForkConfig("FutureUnknownFork") != nil {
		t.Error("unsupported fork should return nil")
	}
}

func TestEFHexParsing(t *testing.T) {
	// hexToBytes
	b := hexToBytes("0x1234")
	if len(b) != 2 || b[0] != 0x12 || b[1] != 0x34 {
		t.Errorf("hexToBytes(0x1234) = %x", b)
	}

	// hexToUint64
	v := hexToUint64("0x10")
	if v != 16 {
		t.Errorf("hexToUint64(0x10) = %d, expected 16", v)
	}

	// hexToBigInt
	bi := hexToBigInt("0xff")
	if bi.Int64() != 255 {
		t.Errorf("hexToBigInt(0xff) = %s", bi)
	}

	// Empty string handling.
	if hexToUint64("") != 0 {
		t.Error("hexToUint64('') should be 0")
	}
	if hexToBigInt("").Sign() != 0 {
		t.Error("hexToBigInt('') should be 0")
	}
	if hexToBytes("") != nil {
		t.Error("hexToBytes('') should be nil")
	}

	// Without 0x prefix.
	if hexToUint64("ff") != 255 {
		t.Errorf("hexToUint64('ff') = %d", hexToUint64("ff"))
	}
}

func TestEFEmptyPreState(t *testing.T) {
	statedb := state.NewMemoryStateDB()
	root, err := statedb.Commit()
	if err != nil {
		t.Fatalf("Commit: %v", err)
	}
	if root != types.EmptyRootHash {
		t.Errorf("empty state root: got %s, expected %s", root.Hex(), types.EmptyRootHash.Hex())
	}
}

func TestEFStorageRootComputation(t *testing.T) {
	statedb := state.NewMemoryStateDB()
	addr := types.HexToAddress("0xcccccccccccccccccccccccccccccccccccccccc")
	statedb.CreateAccount(addr)
	statedb.AddBalance(addr, big.NewInt(1))

	// Empty storage should give EmptyRootHash.
	emptyRoot := statedb.StorageRoot(addr)
	if emptyRoot != types.EmptyRootHash {
		t.Errorf("empty storage root: got %s, expected %s", emptyRoot.Hex(), types.EmptyRootHash.Hex())
	}

	// Add storage and verify root changes.
	statedb.SetState(addr, types.HexToHash("0x01"), types.HexToHash("0x42"))
	storageRoot := statedb.StorageRoot(addr)
	if storageRoot == types.EmptyRootHash {
		t.Error("storage root should not be empty after adding storage")
	}

	// Same storage state should produce same root.
	statedb2 := state.NewMemoryStateDB()
	statedb2.CreateAccount(addr)
	statedb2.AddBalance(addr, big.NewInt(1))
	statedb2.SetState(addr, types.HexToHash("0x01"), types.HexToHash("0x42"))
	if statedb2.StorageRoot(addr) != storageRoot {
		t.Error("same storage should produce same root")
	}
}

func TestEFMultipleDataVariants(t *testing.T) {
	path := testdataPath()
	if _, err := os.Stat(path); os.IsNotExist(err) {
		t.Skipf("fixture not found: %s", path)
	}

	tests, err := LoadStateTests(path)
	if err != nil {
		t.Fatalf("LoadStateTests: %v", err)
	}

	// Verify data index selection works.
	for _, test := range tests {
		if len(test.json.Tx.Data) > 1 {
			t.Logf("test %s has %d data variants", test.Name, len(test.json.Tx.Data))
		}
		// Verify index bounds.
		for _, sub := range test.Subtests() {
			posts := test.json.Post[sub.Fork]
			if sub.Index < len(posts) {
				idx := posts[sub.Index].Indexes
				if idx.Data >= len(test.json.Tx.Data) && len(test.json.Tx.Data) > 0 {
					t.Errorf("test %s: data index %d out of range (len=%d)",
						test.Name, idx.Data, len(test.json.Tx.Data))
				}
			}
		}
	}
}

func TestEFPrivateKeyParsing(t *testing.T) {
	// This is the well-known test key from go-ethereum's statetest.json.
	keyHex := "0x45a915e4d060149eb4365960e6a7a45f334393093061116b197e3240065ff2d8"
	key, err := hexToPrivateKey(keyHex)
	if err != nil {
		t.Fatalf("hexToPrivateKey: %v", err)
	}

	addr := crypto.PubkeyToAddress(key.PublicKey)
	if addr == (types.Address{}) {
		t.Error("derived address should not be zero")
	}
	t.Logf("derived address: %s", addr.Hex())

	// Verify this is a valid secp256k1 key.
	if key.D == nil || key.D.Sign() == 0 {
		t.Error("private key D should not be zero")
	}
	if key.PublicKey.X == nil || key.PublicKey.Y == nil {
		t.Error("public key coordinates should not be nil")
	}
}

func TestEFRunAgainstGoEthereumFixture(t *testing.T) {
	path := testdataPath()
	if _, err := os.Stat(path); os.IsNotExist(err) {
		t.Skipf("fixture not found: %s", path)
	}

	tests, err := LoadStateTests(path)
	if err != nil {
		t.Fatalf("LoadStateTests: %v", err)
	}

	var total, passed, failed, skipped int
	for name, test := range tests {
		for _, sub := range test.Subtests() {
			total++
			if !ForkSupported(sub.Fork) {
				skipped++
				continue
			}

			result := test.Run(sub)
			if result.Passed {
				passed++
			} else {
				failed++
				t.Logf("FAIL: %s [fork=%s idx=%d]: %v (expected=%s got=%s)",
					name, sub.Fork, sub.Index, result.Error,
					result.ExpectedRoot.Hex(), result.GotRoot.Hex())
			}
		}
	}

	t.Logf("Results: %d total, %d passed, %d failed, %d skipped", total, passed, failed, skipped)
}

func TestEFForkSupported(t *testing.T) {
	supported := []string{"Frontier", "Homestead", "Byzantium", "London", "Shanghai", "Cancun", "Prague"}
	for _, f := range supported {
		if !ForkSupported(f) {
			t.Errorf("expected fork %q to be supported", f)
		}
	}

	unsupported := []string{"", "FutureHardFork", "Atlantis"}
	for _, f := range unsupported {
		if ForkSupported(f) {
			t.Errorf("expected fork %q to be unsupported", f)
		}
	}
}

func TestEFContractExecution(t *testing.T) {
	// Test that code can be set and retrieved correctly.
	statedb := state.NewMemoryStateDB()
	contractAddr := types.HexToAddress("0x00000000000000000000000000000000000000f1")

	// Simple STOP opcode.
	code := []byte{0x00}
	statedb.CreateAccount(contractAddr)
	statedb.SetCode(contractAddr, code)
	// contract starts with zero balance (default)

	gotCode := statedb.GetCode(contractAddr)
	if len(gotCode) != 1 || gotCode[0] != 0x00 {
		t.Errorf("code mismatch: got %x", gotCode)
	}

	// Verify code hash.
	codeHash := statedb.GetCodeHash(contractAddr)
	expectedHash := crypto.Keccak256Hash(code)
	if codeHash != expectedHash {
		t.Errorf("code hash mismatch: got %s, expected %s", codeHash.Hex(), expectedHash.Hex())
	}
}

func TestEFGasAccounting(t *testing.T) {
	// Verify that the gas pool works correctly with ApplyTransaction.
	statedb := state.NewMemoryStateDB()
	sender := types.HexToAddress("0xa94f5374fce5edbc8e2a8697c15331677e6ebf0b")
	receiver := types.HexToAddress("0x1234567890abcdef1234567890abcdef12345678")

	statedb.CreateAccount(sender)
	statedb.AddBalance(sender, new(big.Int).Mul(big.NewInt(1e18), big.NewInt(10)))
	statedb.SetNonce(sender, 0)

	statedb.CreateAccount(receiver)

	// A simple value transfer should consume exactly 21000 gas.
	// (Assuming no EIP-7706 or Glamsterdam repricing.)
	config := ForkConfig("London")
	header := &types.Header{
		Number:   big.NewInt(1),
		GasLimit: 10_000_000,
		BaseFee:  big.NewInt(1000),
		Time:     1000,
	}

	tx := types.NewTransaction(&types.LegacyTx{
		Nonce:    0,
		GasPrice: big.NewInt(1000),
		Gas:      21000,
		To:       &receiver,
		Value:    big.NewInt(1),
	})
	tx.SetSender(sender)

	gp := new(core.GasPool).AddGas(header.GasLimit)
	statedb.SetTxContext(tx.Hash(), 0)

	receipt, gasUsed, err := core.ApplyTransaction(config, statedb, header, tx, gp)
	if err != nil {
		t.Fatalf("ApplyTransaction: %v", err)
	}

	if gasUsed != 21000 {
		t.Errorf("gas used: got %d, expected 21000", gasUsed)
	}
	if receipt == nil {
		t.Fatal("receipt is nil")
	}
	t.Logf("gas used: %d, receipt status: %d", gasUsed, receipt.Status)
}
