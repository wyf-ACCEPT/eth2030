package types

import (
	"math/big"
	"testing"
)

func TestBytesToHash(t *testing.T) {
	b := []byte{0x01, 0x02, 0x03}
	h := BytesToHash(b)
	if h[HashLength-1] != 0x03 || h[HashLength-2] != 0x02 || h[HashLength-3] != 0x01 {
		t.Fatalf("BytesToHash failed: got %x", h)
	}
	// Leading bytes should be zero.
	for i := 0; i < HashLength-3; i++ {
		if h[i] != 0 {
			t.Fatalf("BytesToHash did not left-pad: byte %d is %x", i, h[i])
		}
	}
}

func TestBytesToHash_LongerThan32(t *testing.T) {
	b := make([]byte, 40)
	for i := range b {
		b[i] = byte(i)
	}
	h := BytesToHash(b)
	// Should take the rightmost 32 bytes.
	for i := 0; i < HashLength; i++ {
		if h[i] != byte(i+8) {
			t.Fatalf("BytesToHash longer input: byte %d got %x, want %x", i, h[i], byte(i+8))
		}
	}
}

func TestHexToHash(t *testing.T) {
	h := HexToHash("0xdead")
	if h[HashLength-1] != 0xad || h[HashLength-2] != 0xde {
		t.Fatalf("HexToHash failed: got %x", h)
	}
}

func TestHashIsZero(t *testing.T) {
	var h Hash
	if !h.IsZero() {
		t.Fatal("zero hash should be zero")
	}
	h[0] = 1
	if h.IsZero() {
		t.Fatal("non-zero hash should not be zero")
	}
}

func TestHashHex(t *testing.T) {
	h := HexToHash("0xff")
	hex := h.Hex()
	if hex[0:2] != "0x" {
		t.Fatal("Hex should start with 0x")
	}
}

func TestBytesToAddress(t *testing.T) {
	b := []byte{0xab, 0xcd}
	a := BytesToAddress(b)
	if a[AddressLength-1] != 0xcd || a[AddressLength-2] != 0xab {
		t.Fatalf("BytesToAddress failed: got %x", a)
	}
}

func TestHexToAddress(t *testing.T) {
	a := HexToAddress("0xdeadbeef")
	if a[AddressLength-1] != 0xef || a[AddressLength-2] != 0xbe {
		t.Fatalf("HexToAddress failed: got %x", a)
	}
}

func TestAddressIsZero(t *testing.T) {
	var a Address
	if !a.IsZero() {
		t.Fatal("zero address should be zero")
	}
	a[0] = 1
	if a.IsZero() {
		t.Fatal("non-zero address should not be zero")
	}
}

func TestEmptyRootHash(t *testing.T) {
	// Keccak256 of RLP of empty trie.
	expected := HexToHash("56e81f171bcc55a6ff8345e692c0f86e5b48e01b996cadc001622fb5e363b421")
	if EmptyRootHash != expected {
		t.Fatalf("EmptyRootHash mismatch: got %s, want %s", EmptyRootHash, expected)
	}
}

func TestEmptyCodeHash(t *testing.T) {
	// Keccak256 of empty string.
	expected := HexToHash("c5d2460186f7233c927e7db2dcc703c0e500b653ca82273b7bfad8045d85a470")
	if EmptyCodeHash != expected {
		t.Fatalf("EmptyCodeHash mismatch: got %s, want %s", EmptyCodeHash, expected)
	}
}

func TestEmptyUncleHash(t *testing.T) {
	expected := HexToHash("1dcc4de8dec75d7aab85b567b6ccd41ad312451b948a7413f0a142fd40d49347")
	if EmptyUncleHash != expected {
		t.Fatalf("EmptyUncleHash mismatch: got %s, want %s", EmptyUncleHash, expected)
	}
}

func TestNewAccount(t *testing.T) {
	acc := NewAccount()
	if acc.Nonce != 0 {
		t.Fatal("new account nonce should be 0")
	}
	if acc.Balance.Cmp(big.NewInt(0)) != 0 {
		t.Fatal("new account balance should be 0")
	}
	if acc.Root != EmptyRootHash {
		t.Fatal("new account root should be EmptyRootHash")
	}
	if BytesToHash(acc.CodeHash) != EmptyCodeHash {
		t.Fatal("new account code hash should be EmptyCodeHash")
	}
}

func TestHashString(t *testing.T) {
	h := HexToHash("0x1234")
	s := h.String()
	if s != h.Hex() {
		t.Fatalf("String() should match Hex(): got %s vs %s", s, h.Hex())
	}
}

func TestAddressString(t *testing.T) {
	a := HexToAddress("0xabcd")
	s := a.String()
	if s != a.Hex() {
		t.Fatalf("String() should match Hex(): got %s vs %s", s, a.Hex())
	}
}
