package types

import (
	"bytes"
	"testing"
)

func TestParseDelegation_Valid(t *testing.T) {
	addr := HexToAddress("0x1234567890abcdef1234567890abcdef12345678")
	code := AddressToDelegation(addr)

	parsed, ok := ParseDelegation(code)
	if !ok {
		t.Fatal("ParseDelegation should return true for valid delegation code")
	}
	if parsed != addr {
		t.Errorf("ParseDelegation returned wrong address: got %v, want %v", parsed.Hex(), addr.Hex())
	}
}

func TestParseDelegation_ZeroAddress(t *testing.T) {
	addr := Address{}
	code := AddressToDelegation(addr)

	parsed, ok := ParseDelegation(code)
	if !ok {
		t.Fatal("ParseDelegation should return true for delegation to zero address")
	}
	if parsed != addr {
		t.Errorf("ParseDelegation returned wrong address: got %v, want %v", parsed.Hex(), addr.Hex())
	}
}

func TestParseDelegation_TooShort(t *testing.T) {
	// Only prefix, no address.
	code := []byte{0xef, 0x01, 0x00}
	_, ok := ParseDelegation(code)
	if ok {
		t.Error("ParseDelegation should return false for prefix-only code")
	}
}

func TestParseDelegation_TooLong(t *testing.T) {
	code := make([]byte, 24)
	code[0] = 0xef
	code[1] = 0x01
	code[2] = 0x00
	_, ok := ParseDelegation(code)
	if ok {
		t.Error("ParseDelegation should return false for code that is too long")
	}
}

func TestParseDelegation_WrongPrefix(t *testing.T) {
	code := make([]byte, 23)
	code[0] = 0xff
	code[1] = 0x01
	code[2] = 0x00
	_, ok := ParseDelegation(code)
	if ok {
		t.Error("ParseDelegation should return false for wrong prefix")
	}
}

func TestParseDelegation_Empty(t *testing.T) {
	_, ok := ParseDelegation(nil)
	if ok {
		t.Error("ParseDelegation should return false for nil")
	}
	_, ok = ParseDelegation([]byte{})
	if ok {
		t.Error("ParseDelegation should return false for empty")
	}
}

func TestAddressToDelegation_Structure(t *testing.T) {
	addr := HexToAddress("0xaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa")
	code := AddressToDelegation(addr)

	if len(code) != 23 {
		t.Fatalf("delegation code should be 23 bytes, got %d", len(code))
	}
	if !bytes.Equal(code[:3], DelegationPrefix) {
		t.Errorf("code should start with delegation prefix, got %x", code[:3])
	}

	var extracted Address
	copy(extracted[:], code[3:])
	if extracted != addr {
		t.Errorf("extracted address mismatch: got %v, want %v", extracted.Hex(), addr.Hex())
	}
}

func TestAddressToDelegation_Roundtrip(t *testing.T) {
	addresses := []Address{
		HexToAddress("0x0000000000000000000000000000000000000000"),
		HexToAddress("0x1234567890abcdef1234567890abcdef12345678"),
		HexToAddress("0xffffffffffffffffffffffffffffffffffffffff"),
		HexToAddress("0xdeadbeefdeadbeefdeadbeefdeadbeefdeadbeef"),
	}

	for _, addr := range addresses {
		t.Run(addr.Hex(), func(t *testing.T) {
			code := AddressToDelegation(addr)
			if !HasDelegationPrefix(code) {
				t.Fatal("HasDelegationPrefix should return true")
			}
			parsed, ok := ParseDelegation(code)
			if !ok {
				t.Fatal("ParseDelegation should succeed")
			}
			if parsed != addr {
				t.Errorf("roundtrip failed: got %v, want %v", parsed.Hex(), addr.Hex())
			}
		})
	}
}

func TestHasDelegationPrefix_Valid(t *testing.T) {
	code := make([]byte, 23)
	code[0] = 0xef
	code[1] = 0x01
	code[2] = 0x00
	if !HasDelegationPrefix(code) {
		t.Error("should return true for valid delegation code")
	}
}

func TestHasDelegationPrefix_LongerCode(t *testing.T) {
	code := make([]byte, 50)
	code[0] = 0xef
	code[1] = 0x01
	code[2] = 0x00
	if !HasDelegationPrefix(code) {
		t.Error("should return true for code starting with prefix regardless of length")
	}
}

func TestHasDelegationPrefix_Empty(t *testing.T) {
	if HasDelegationPrefix(nil) {
		t.Error("should return false for nil")
	}
	if HasDelegationPrefix([]byte{}) {
		t.Error("should return false for empty")
	}
}

func TestHasDelegationPrefix_TooShort(t *testing.T) {
	if HasDelegationPrefix([]byte{0xef, 0x01}) {
		t.Error("should return false for 2-byte code")
	}
}

func TestHasDelegationPrefix_WrongPrefix(t *testing.T) {
	// Regular EVM bytecode.
	if HasDelegationPrefix([]byte{0x60, 0x80, 0x60, 0x40, 0x52}) {
		t.Error("should return false for regular contract code")
	}
}

func TestDelegationPrefix_IsExpectedValue(t *testing.T) {
	if !bytes.Equal(DelegationPrefix, []byte{0xef, 0x01, 0x00}) {
		t.Errorf("DelegationPrefix = %x, want ef0100", DelegationPrefix)
	}
}

func TestAuthMagic_IsExpectedValue(t *testing.T) {
	if AuthMagic != 0x05 {
		t.Errorf("AuthMagic = %d, want 5", AuthMagic)
	}
}

func TestGasConstants(t *testing.T) {
	if PerAuthBaseCost != 12500 {
		t.Errorf("PerAuthBaseCost = %d, want 12500", PerAuthBaseCost)
	}
	if PerEmptyAccountCost != 25000 {
		t.Errorf("PerEmptyAccountCost = %d, want 25000", PerEmptyAccountCost)
	}
}
