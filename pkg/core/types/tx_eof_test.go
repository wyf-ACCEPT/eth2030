package types

import (
	"testing"
)

func TestIsEOFInitcode(t *testing.T) {
	tests := []struct {
		name string
		data []byte
		want bool
	}{
		{"valid EOF magic", []byte{0xEF, 0x00, 0x01}, true},
		{"just magic", []byte{0xEF, 0x00}, true},
		{"too short", []byte{0xEF}, false},
		{"empty", nil, false},
		{"wrong first byte", []byte{0xFE, 0x00}, false},
		{"wrong second byte", []byte{0xEF, 0x01}, false},
		{"legacy tx data", []byte{0x60, 0x00, 0x60, 0x00}, false},
		{"long EOF initcode", append([]byte{0xEF, 0x00, 0x01}, make([]byte, 100)...), true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := IsEOFInitcode(tt.data); got != tt.want {
				t.Errorf("IsEOFInitcode() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestValidateEOFInitcode(t *testing.T) {
	tests := []struct {
		name    string
		data    []byte
		wantErr error
	}{
		{"valid v1", []byte{0xEF, 0x00, 0x01, 0x00}, nil},
		{"minimal valid", []byte{0xEF, 0x00, 0x01}, nil},
		{"too short 0", nil, ErrEOFInitcodeTooShort},
		{"too short 1", []byte{0xEF}, ErrEOFInitcodeTooShort},
		{"too short 2", []byte{0xEF, 0x00}, ErrEOFInitcodeTooShort},
		{"bad magic0", []byte{0xFE, 0x00, 0x01}, ErrEOFInitcodeInvalidMagic},
		{"bad magic1", []byte{0xEF, 0x01, 0x01}, ErrEOFInitcodeInvalidMagic},
		{"version 0", []byte{0xEF, 0x00, 0x00}, ErrEOFInitcodeInvalidVersion},
		{"version 2", []byte{0xEF, 0x00, 0x02}, ErrEOFInitcodeInvalidVersion},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateEOFInitcode(tt.data)
			if err != tt.wantErr {
				t.Errorf("ValidateEOFInitcode() error = %v, want %v", err, tt.wantErr)
			}
		})
	}
}

func TestComputeEOFCreateGas(t *testing.T) {
	tests := []struct {
		name        string
		initcodeLen int
		wantGas     uint64
	}{
		{"zero length", 0, EOFCreateBaseGas},
		{"negative", -1, EOFCreateBaseGas},
		{"1 byte (1 word)", 1, EOFCreateBaseGas + EOFInitcodeWordGas*1},
		{"31 bytes (1 word)", 31, EOFCreateBaseGas + EOFInitcodeWordGas*1},
		{"32 bytes (1 word)", 32, EOFCreateBaseGas + EOFInitcodeWordGas*1},
		{"33 bytes (2 words)", 33, EOFCreateBaseGas + EOFInitcodeWordGas*2},
		{"64 bytes (2 words)", 64, EOFCreateBaseGas + EOFInitcodeWordGas*2},
		{"100 bytes (4 words)", 100, EOFCreateBaseGas + EOFInitcodeWordGas*4},
		{"1024 bytes (32 words)", 1024, EOFCreateBaseGas + EOFInitcodeWordGas*32},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ComputeEOFCreateGas(tt.initcodeLen)
			if got != tt.wantGas {
				t.Errorf("ComputeEOFCreateGas(%d) = %d, want %d", tt.initcodeLen, got, tt.wantGas)
			}
		})
	}
}

func TestEOFCreateGasConstants(t *testing.T) {
	if EOFCreateBaseGas != 32000 {
		t.Errorf("EOFCreateBaseGas = %d, want 32000", EOFCreateBaseGas)
	}
	if EOFInitcodeWordGas != 2 {
		t.Errorf("EOFInitcodeWordGas = %d, want 2", EOFInitcodeWordGas)
	}
}

func TestEOFCreateResult(t *testing.T) {
	// Verify EOFCreateResult can hold expected values.
	addr := Address{0x01}
	code := []byte{0x60, 0x00}
	result := EOFCreateResult{
		Address: addr,
		Code:    code,
		GasUsed: 12345,
	}
	if result.Address != addr {
		t.Errorf("Address = %v, want %v", result.Address, addr)
	}
	if len(result.Code) != 2 {
		t.Errorf("Code len = %d, want 2", len(result.Code))
	}
	if result.GasUsed != 12345 {
		t.Errorf("GasUsed = %d, want 12345", result.GasUsed)
	}
}
