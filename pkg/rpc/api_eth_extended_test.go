package rpc

import (
	"testing"

	"github.com/eth2028/eth2028/core/types"
	"github.com/eth2028/eth2028/crypto"
)

func TestEthExtendedAPI_New(t *testing.T) {
	mb := newMockBackend()
	api := NewEthExtendedAPI(mb)
	if api == nil {
		t.Fatal("expected non-nil API")
	}
}

func TestEthExtendedAPI_GetUncleByBlockHashAndIndex(t *testing.T) {
	api := NewEthExtendedAPI(newMockBackend())
	h := types.HexToHash("0xabcd")

	uncle := api.GetUncleByBlockHashAndIndex(h, 0)
	if uncle != nil {
		t.Fatal("post-merge: uncle should be nil")
	}
}

func TestEthExtendedAPI_GetUncleByBlockNumberAndIndex(t *testing.T) {
	api := NewEthExtendedAPI(newMockBackend())

	uncle := api.GetUncleByBlockNumberAndIndex(42, 0)
	if uncle != nil {
		t.Fatal("post-merge: uncle should be nil")
	}
}

func TestEthExtendedAPI_GetUncleCountByBlockHash(t *testing.T) {
	api := NewEthExtendedAPI(newMockBackend())
	h := types.HexToHash("0xabcd")

	count := api.GetUncleCountByBlockHash(h)
	if count != 0 {
		t.Fatalf("post-merge: uncle count should be 0, got %d", count)
	}
}

func TestEthExtendedAPI_GetUncleCountByBlockNumber(t *testing.T) {
	api := NewEthExtendedAPI(newMockBackend())

	count := api.GetUncleCountByBlockNumber(42)
	if count != 0 {
		t.Fatalf("post-merge: uncle count should be 0, got %d", count)
	}
}

func TestEthExtendedAPI_GetWork(t *testing.T) {
	api := NewEthExtendedAPI(newMockBackend())

	work := api.GetWork()
	zeroHash := "0x0000000000000000000000000000000000000000000000000000000000000000"

	for i, w := range work {
		if w != zeroHash {
			t.Fatalf("work[%d]: want zero hash, got %s", i, w)
		}
	}
}

func TestEthExtendedAPI_Accounts_Empty(t *testing.T) {
	api := NewEthExtendedAPI(newMockBackend())

	accounts := api.Accounts()
	if len(accounts) != 0 {
		t.Fatalf("want 0 accounts, got %d", len(accounts))
	}
}

func TestEthExtendedAPI_AddAccountAndAccounts(t *testing.T) {
	api := NewEthExtendedAPI(newMockBackend())

	key, err := crypto.GenerateKey()
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}

	addr := api.AddAccount(key)
	if addr.IsZero() {
		t.Fatal("address should not be zero")
	}

	accounts := api.Accounts()
	if len(accounts) != 1 {
		t.Fatalf("want 1 account, got %d", len(accounts))
	}
	if accounts[0] != addr {
		t.Fatalf("want address %v, got %v", addr, accounts[0])
	}
}

func TestEthExtendedAPI_AddMultipleAccounts(t *testing.T) {
	api := NewEthExtendedAPI(newMockBackend())

	key1, _ := crypto.GenerateKey()
	key2, _ := crypto.GenerateKey()

	addr1 := api.AddAccount(key1)
	addr2 := api.AddAccount(key2)

	if addr1 == addr2 {
		t.Fatal("two different keys should produce different addresses")
	}

	accounts := api.Accounts()
	if len(accounts) != 2 {
		t.Fatalf("want 2 accounts, got %d", len(accounts))
	}
}

func TestEthExtendedAPI_Sign(t *testing.T) {
	api := NewEthExtendedAPI(newMockBackend())

	key, err := crypto.GenerateKey()
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	addr := api.AddAccount(key)

	data := []byte("hello ethereum")
	sig, err := api.Sign(addr, data)
	if err != nil {
		t.Fatalf("sign error: %v", err)
	}

	// Signature should be 65 bytes [R || S || V].
	if len(sig) != 65 {
		t.Fatalf("want 65 byte signature, got %d", len(sig))
	}

	// Verify the signature by recovering the address.
	hash := crypto.Keccak256(data)
	recovered, err := crypto.SigToPub(hash, sig)
	if err != nil {
		t.Fatalf("recover pubkey: %v", err)
	}
	recoveredAddr := crypto.PubkeyToAddress(*recovered)
	if recoveredAddr != addr {
		t.Fatalf("recovered address %v != signer address %v", recoveredAddr, addr)
	}
}

func TestEthExtendedAPI_Sign_UnknownAccount(t *testing.T) {
	api := NewEthExtendedAPI(newMockBackend())

	unknownAddr := types.HexToAddress("0xdeadbeef")
	_, err := api.Sign(unknownAddr, []byte("data"))
	if err == nil {
		t.Fatal("expected error for unknown account")
	}
}

func TestEthExtendedAPI_Sign_EmptyData(t *testing.T) {
	api := NewEthExtendedAPI(newMockBackend())

	key, _ := crypto.GenerateKey()
	addr := api.AddAccount(key)

	sig, err := api.Sign(addr, []byte{})
	if err != nil {
		t.Fatalf("sign error: %v", err)
	}
	if len(sig) != 65 {
		t.Fatalf("want 65 byte signature, got %d", len(sig))
	}
}

func TestEthExtendedAPI_GetStorageAt(t *testing.T) {
	mb := newMockBackend()

	// Set a storage value in the mock state.
	addr := types.HexToAddress("0xaaaa")
	key := types.HexToHash("0x01")
	val := types.HexToHash("0x42")
	mb.statedb.SetState(addr, key, val)

	api := NewEthExtendedAPI(mb)

	result := api.GetStorageAt(addr, key)
	if result != val {
		t.Fatalf("want storage value %v, got %v", val, result)
	}
}

func TestEthExtendedAPI_GetStorageAt_Empty(t *testing.T) {
	mb := newMockBackend()
	api := NewEthExtendedAPI(mb)

	addr := types.HexToAddress("0xbbbb")
	key := types.HexToHash("0x99")

	result := api.GetStorageAt(addr, key)
	if !result.IsZero() {
		t.Fatalf("want zero hash for empty storage, got %v", result)
	}
}

func TestEthExtendedAPI_GetStorageAt_NoHeader(t *testing.T) {
	mb := newMockBackend()
	// Remove the current header.
	delete(mb.headers, 42)

	api := NewEthExtendedAPI(mb)
	result := api.GetStorageAt(types.Address{}, types.Hash{})
	if !result.IsZero() {
		t.Fatalf("want zero hash when no header, got %v", result)
	}
}

func TestEthExtendedAPI_GetCompilers(t *testing.T) {
	api := NewEthExtendedAPI(newMockBackend())

	compilers := api.GetCompilers()
	if compilers == nil {
		t.Fatal("compilers should not be nil")
	}
	if len(compilers) != 0 {
		t.Fatalf("want 0 compilers, got %d", len(compilers))
	}
}

func TestEthExtendedAPI_CreateAccessList(t *testing.T) {
	mb := newMockBackend()
	// Ensure the mock EVM call succeeds.
	mb.callResult = []byte{0x01}
	mb.callGasUsed = 21000

	api := NewEthExtendedAPI(mb)

	to := types.HexToAddress("0xcccc")
	acl := api.CreateAccessList(to, []byte{0x12, 0x34}, 100000)

	if len(acl) != 1 {
		t.Fatalf("want 1 access list entry, got %d", len(acl))
	}
	if acl[0].Address != to {
		t.Fatalf("want address %v, got %v", to, acl[0].Address)
	}
	if len(acl[0].StorageKeys) != 0 {
		t.Fatalf("want 0 storage keys, got %d", len(acl[0].StorageKeys))
	}
}

func TestEthExtendedAPI_CreateAccessList_DefaultGas(t *testing.T) {
	mb := newMockBackend()
	mb.callResult = []byte{0x01}

	api := NewEthExtendedAPI(mb)

	to := types.HexToAddress("0xdddd")
	// gasLimit=0 should default to 50M internally.
	acl := api.CreateAccessList(to, nil, 0)
	if len(acl) != 1 {
		t.Fatalf("want 1 access list entry, got %d", len(acl))
	}
}

func TestEthExtendedAPI_CreateAccessList_CallFails(t *testing.T) {
	mb := newMockBackend()
	mb.callErr = errCallFailed

	api := NewEthExtendedAPI(mb)

	to := types.HexToAddress("0xeeee")
	acl := api.CreateAccessList(to, []byte{0xff}, 100000)

	// When the call fails, should return empty access list.
	if len(acl) != 0 {
		t.Fatalf("want 0 access list entries on error, got %d", len(acl))
	}
}

func TestEthExtendedAPI_CreateAccessList_EmptyData(t *testing.T) {
	mb := newMockBackend()
	mb.callResult = nil
	mb.callGasUsed = 21000

	api := NewEthExtendedAPI(mb)

	to := types.HexToAddress("0xaaaa")
	acl := api.CreateAccessList(to, nil, 21000)
	if len(acl) != 1 {
		t.Fatalf("want 1 access list entry, got %d", len(acl))
	}
}
