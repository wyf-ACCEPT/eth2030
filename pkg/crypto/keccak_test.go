package crypto

import (
	"encoding/hex"
	"testing"

	"github.com/eth2028/eth2028/core/types"
)

func TestKeccak256EmptyString(t *testing.T) {
	hash := Keccak256([]byte{})
	got := hex.EncodeToString(hash)
	want := "c5d2460186f7233c927e7db2dcc703c0e500b653ca82273b7bfad8045d85a470"
	if got != want {
		t.Errorf("Keccak256(empty) = %s, want %s", got, want)
	}
}

func TestKeccak256Hello(t *testing.T) {
	hash := Keccak256([]byte("hello"))
	got := hex.EncodeToString(hash)
	want := "1c8aff950685c2ed4bc3174f3472287b56d9517b9c948127319a09a7a36deac8"
	if got != want {
		t.Errorf("Keccak256(hello) = %s, want %s", got, want)
	}
}

func TestKeccak256MultipleInputs(t *testing.T) {
	// Keccak256("hello", "world") should equal Keccak256("helloworld")
	combined := Keccak256([]byte("helloworld"))
	separate := Keccak256([]byte("hello"), []byte("world"))
	if hex.EncodeToString(combined) != hex.EncodeToString(separate) {
		t.Errorf("Keccak256 multi-input mismatch: %x != %x", combined, separate)
	}
}

func TestKeccak256HashReturnsCorrectType(t *testing.T) {
	h := Keccak256Hash([]byte{})
	want := types.HexToHash("c5d2460186f7233c927e7db2dcc703c0e500b653ca82273b7bfad8045d85a470")
	if h != want {
		t.Errorf("Keccak256Hash(empty) = %s, want %s", h, want)
	}
}

func TestKeccak256HashLength(t *testing.T) {
	h := Keccak256Hash([]byte("test"))
	if len(h) != 32 {
		t.Errorf("Keccak256Hash length = %d, want 32", len(h))
	}
}

func TestKeccak256Deterministic(t *testing.T) {
	data := []byte("deterministic test")
	h1 := Keccak256(data)
	h2 := Keccak256(data)
	if hex.EncodeToString(h1) != hex.EncodeToString(h2) {
		t.Error("Keccak256 is not deterministic")
	}
}
