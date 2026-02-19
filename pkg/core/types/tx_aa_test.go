package types

import (
	"math/big"
	"testing"
)

func TestAATxType(t *testing.T) {
	if AATxType != 0x05 {
		t.Errorf("AATxType = 0x%02x, want 0x05", AATxType)
	}
}

func TestAATxConstants(t *testing.T) {
	if AABaseCost != 15000 {
		t.Errorf("AABaseCost = %d, want 15000", AABaseCost)
	}
	if RoleSenderDeployment != 0xA0 {
		t.Errorf("RoleSenderDeployment = 0x%02x, want 0xA0", RoleSenderDeployment)
	}
	if RoleSenderValidation != 0xA1 {
		t.Errorf("RoleSenderValidation = 0x%02x, want 0xA1", RoleSenderValidation)
	}
	if RolePaymasterValidation != 0xA2 {
		t.Errorf("RolePaymasterValidation = 0x%02x, want 0xA2", RolePaymasterValidation)
	}
	if RoleSenderExecution != 0xA3 {
		t.Errorf("RoleSenderExecution = 0x%02x, want 0xA3", RoleSenderExecution)
	}
	if RolePaymasterPostOp != 0xA4 {
		t.Errorf("RolePaymasterPostOp = 0x%02x, want 0xA4", RolePaymasterPostOp)
	}
}

func TestAAEntryPoint(t *testing.T) {
	expected := HexToAddress("0x0000000000000000000000000000000000007701")
	if AAEntryPoint != expected {
		t.Errorf("AAEntryPoint = %x, want %x", AAEntryPoint, expected)
	}
}

func makeTestAATx() *AATx {
	sender := HexToAddress("0xaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa")
	return &AATx{
		ChainID:              big.NewInt(1),
		Nonce:                42,
		Sender:               sender,
		SenderValidationData: []byte{0x01, 0x02, 0x03},
		SenderExecutionData:  []byte{0xAA, 0xBB},
		MaxPriorityFeePerGas: big.NewInt(1_000_000_000),
		MaxFeePerGas:         big.NewInt(30_000_000_000),
		SenderValidationGas:  100_000,
		SenderExecutionGas:   200_000,
	}
}

func TestAATx_TxDataInterface(t *testing.T) {
	tx := makeTestAATx()

	// Verify TxData interface methods.
	if tx.txType() != AATxType {
		t.Errorf("txType() = 0x%02x, want 0x%02x", tx.txType(), AATxType)
	}
	if tx.chainID().Cmp(big.NewInt(1)) != 0 {
		t.Errorf("chainID() = %s, want 1", tx.chainID())
	}
	if tx.nonce() != 42 {
		t.Errorf("nonce() = %d, want 42", tx.nonce())
	}
	if tx.gasPrice().Cmp(big.NewInt(30_000_000_000)) != 0 {
		t.Errorf("gasPrice() = %s, want 30000000000", tx.gasPrice())
	}
	if tx.gasTipCap().Cmp(big.NewInt(1_000_000_000)) != 0 {
		t.Errorf("gasTipCap() = %s, want 1000000000", tx.gasTipCap())
	}
	if tx.gasFeeCap().Cmp(big.NewInt(30_000_000_000)) != 0 {
		t.Errorf("gasFeeCap() = %s, want 30000000000", tx.gasFeeCap())
	}
	if tx.value().Sign() != 0 {
		t.Errorf("value() = %s, want 0", tx.value())
	}

	// to() returns &Sender.
	to := tx.to()
	if to == nil || *to != tx.Sender {
		t.Errorf("to() should return sender address")
	}

	// data() returns SenderExecutionData.
	if string(tx.data()) != string(tx.SenderExecutionData) {
		t.Error("data() should return SenderExecutionData")
	}

	// gas() returns total of all gas fields + base cost.
	expectedGas := AABaseCost + tx.SenderValidationGas + tx.PaymasterValidationGas +
		tx.SenderExecutionGas + tx.PaymasterPostOpGas
	if tx.gas() != expectedGas {
		t.Errorf("gas() = %d, want %d", tx.gas(), expectedGas)
	}
}

func TestAATx_TotalGas(t *testing.T) {
	tx := &AATx{
		SenderValidationGas:    100_000,
		PaymasterValidationGas: 50_000,
		SenderExecutionGas:     200_000,
		PaymasterPostOpGas:     30_000,
	}

	expected := AABaseCost + 100_000 + 50_000 + 200_000 + 30_000
	if tx.totalGas() != expected {
		t.Errorf("totalGas() = %d, want %d", tx.totalGas(), expected)
	}
}

func TestAATx_Copy(t *testing.T) {
	paymaster := HexToAddress("0xbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb")
	deployer := HexToAddress("0xcccccccccccccccccccccccccccccccccccccccc")

	tx := makeTestAATx()
	tx.Paymaster = &paymaster
	tx.PaymasterData = []byte{0xDD}
	tx.Deployer = &deployer
	tx.DeployerData = []byte{0xEE}
	tx.PaymasterValidationGas = 50_000
	tx.PaymasterPostOpGas = 20_000
	tx.AccessList = AccessList{
		{Address: HexToAddress("0x1111111111111111111111111111111111111111"), StorageKeys: []Hash{{1}}},
	}

	cpy := tx.copy().(*AATx)

	// Verify deep copy.
	if cpy.ChainID == tx.ChainID {
		t.Error("ChainID should be a new instance")
	}
	if cpy.ChainID.Cmp(tx.ChainID) != 0 {
		t.Error("ChainID values differ")
	}
	if cpy.Nonce != tx.Nonce {
		t.Error("Nonce differs")
	}
	if cpy.Sender != tx.Sender {
		t.Error("Sender differs")
	}
	if cpy.Paymaster == tx.Paymaster {
		t.Error("Paymaster should be a new pointer")
	}
	if *cpy.Paymaster != *tx.Paymaster {
		t.Error("Paymaster value differs")
	}
	if cpy.Deployer == tx.Deployer {
		t.Error("Deployer should be a new pointer")
	}
	if *cpy.Deployer != *tx.Deployer {
		t.Error("Deployer value differs")
	}

	// Mutating copy shouldn't affect original.
	cpy.Nonce = 999
	if tx.Nonce == 999 {
		t.Error("copy mutation affected original")
	}
	cpy.SenderValidationData[0] = 0xFF
	if tx.SenderValidationData[0] == 0xFF {
		t.Error("copy data mutation affected original")
	}
}

func TestAATx_RLPRoundtrip(t *testing.T) {
	paymaster := HexToAddress("0xbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb")

	tx := makeTestAATx()
	tx.Paymaster = &paymaster
	tx.PaymasterData = []byte{0xDD, 0xEE}
	tx.PaymasterValidationGas = 50_000
	tx.PaymasterPostOpGas = 20_000
	tx.AccessList = AccessList{
		{Address: HexToAddress("0x1111111111111111111111111111111111111111"), StorageKeys: []Hash{{1}}},
	}

	// Encode.
	encoded, err := EncodeAATx(tx)
	if err != nil {
		t.Fatalf("EncodeAATx: %v", err)
	}

	// First byte should be the type.
	if encoded[0] != AATxType {
		t.Errorf("encoded[0] = 0x%02x, want 0x%02x", encoded[0], AATxType)
	}

	// Decode (without type byte).
	decoded, err := DecodeAATx(encoded[1:])
	if err != nil {
		t.Fatalf("DecodeAATx: %v", err)
	}

	// Verify fields.
	if decoded.ChainID.Cmp(tx.ChainID) != 0 {
		t.Errorf("ChainID: got %s, want %s", decoded.ChainID, tx.ChainID)
	}
	if decoded.Nonce != tx.Nonce {
		t.Errorf("Nonce: got %d, want %d", decoded.Nonce, tx.Nonce)
	}
	if decoded.Sender != tx.Sender {
		t.Errorf("Sender: got %x, want %x", decoded.Sender, tx.Sender)
	}
	if string(decoded.SenderValidationData) != string(tx.SenderValidationData) {
		t.Error("SenderValidationData mismatch")
	}
	if string(decoded.SenderExecutionData) != string(tx.SenderExecutionData) {
		t.Error("SenderExecutionData mismatch")
	}
	if decoded.Paymaster == nil || *decoded.Paymaster != *tx.Paymaster {
		t.Error("Paymaster mismatch")
	}
	if string(decoded.PaymasterData) != string(tx.PaymasterData) {
		t.Error("PaymasterData mismatch")
	}
	if decoded.MaxPriorityFeePerGas.Cmp(tx.MaxPriorityFeePerGas) != 0 {
		t.Error("MaxPriorityFeePerGas mismatch")
	}
	if decoded.MaxFeePerGas.Cmp(tx.MaxFeePerGas) != 0 {
		t.Error("MaxFeePerGas mismatch")
	}
	if decoded.SenderValidationGas != tx.SenderValidationGas {
		t.Errorf("SenderValidationGas: got %d, want %d", decoded.SenderValidationGas, tx.SenderValidationGas)
	}
	if decoded.PaymasterValidationGas != tx.PaymasterValidationGas {
		t.Errorf("PaymasterValidationGas: got %d, want %d", decoded.PaymasterValidationGas, tx.PaymasterValidationGas)
	}
	if decoded.SenderExecutionGas != tx.SenderExecutionGas {
		t.Errorf("SenderExecutionGas: got %d, want %d", decoded.SenderExecutionGas, tx.SenderExecutionGas)
	}
	if decoded.PaymasterPostOpGas != tx.PaymasterPostOpGas {
		t.Errorf("PaymasterPostOpGas: got %d, want %d", decoded.PaymasterPostOpGas, tx.PaymasterPostOpGas)
	}
	if len(decoded.AccessList) != 1 {
		t.Errorf("AccessList length: got %d, want 1", len(decoded.AccessList))
	}
}

func TestAATx_RLPRoundtrip_NoOptionalFields(t *testing.T) {
	tx := makeTestAATx()

	encoded, err := EncodeAATx(tx)
	if err != nil {
		t.Fatalf("EncodeAATx: %v", err)
	}

	decoded, err := DecodeAATx(encoded[1:])
	if err != nil {
		t.Fatalf("DecodeAATx: %v", err)
	}

	if decoded.Paymaster != nil {
		t.Error("Paymaster should be nil")
	}
	if decoded.Deployer != nil {
		t.Error("Deployer should be nil")
	}
}

func TestComputeAASigHash(t *testing.T) {
	tx := makeTestAATx()

	h1 := ComputeAASigHash(tx)
	h2 := ComputeAASigHash(tx)

	// Same tx should produce same hash.
	if h1 != h2 {
		t.Error("same tx should produce same sig hash")
	}

	// Different tx should produce different hash.
	tx2 := makeTestAATx()
	tx2.Nonce = 99
	h3 := ComputeAASigHash(tx2)
	if h1 == h3 {
		t.Error("different nonce should produce different sig hash")
	}
}

func TestValidateAATx(t *testing.T) {
	tests := []struct {
		name   string
		modify func(*AATx)
		errStr string
	}{
		{
			name:   "valid basic tx",
			modify: func(tx *AATx) {},
		},
		{
			name:   "negative chain ID",
			modify: func(tx *AATx) { tx.ChainID = big.NewInt(-1) },
			errStr: "negative chain ID",
		},
		{
			name:   "nil max fee",
			modify: func(tx *AATx) { tx.MaxFeePerGas = nil },
			errStr: "invalid max fee per gas",
		},
		{
			name:   "negative max fee",
			modify: func(tx *AATx) { tx.MaxFeePerGas = big.NewInt(-1) },
			errStr: "invalid max fee per gas",
		},
		{
			name:   "nil priority fee",
			modify: func(tx *AATx) { tx.MaxPriorityFeePerGas = nil },
			errStr: "invalid max priority fee",
		},
		{
			name:   "priority > max fee",
			modify: func(tx *AATx) { tx.MaxPriorityFeePerGas = big.NewInt(100); tx.MaxFeePerGas = big.NewInt(50) },
			errStr: "max priority fee exceeds max fee",
		},
		{
			name:   "zero sender validation gas",
			modify: func(tx *AATx) { tx.SenderValidationGas = 0 },
			errStr: "sender validation gas must be > 0",
		},
		{
			name:   "zero sender execution gas",
			modify: func(tx *AATx) { tx.SenderExecutionGas = 0 },
			errStr: "sender execution gas must be > 0",
		},
		{
			name: "paymaster with zero validation gas",
			modify: func(tx *AATx) {
				pm := HexToAddress("0xbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb")
				tx.Paymaster = &pm
				tx.PaymasterValidationGas = 0
			},
			errStr: "paymaster validation gas must be > 0",
		},
		{
			name: "deployer set but no data",
			modify: func(tx *AATx) {
				d := HexToAddress("0xcccccccccccccccccccccccccccccccccccccccc")
				tx.Deployer = &d
				tx.DeployerData = nil
			},
			errStr: "deployer set but deployer data is empty",
		},
		{
			name:   "zero sender",
			modify: func(tx *AATx) { tx.Sender = Address{} },
			errStr: "sender is zero address",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tx := makeTestAATx()
			tt.modify(tx)
			err := ValidateAATx(tx)
			if tt.errStr == "" {
				if err != nil {
					t.Errorf("unexpected error: %v", err)
				}
			} else {
				if err == nil {
					t.Errorf("expected error containing %q, got nil", tt.errStr)
				} else if !containsStr(err.Error(), tt.errStr) {
					t.Errorf("expected error containing %q, got %q", tt.errStr, err.Error())
				}
			}
		})
	}
}

func containsStr(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > 0 && containsSubstring(s, substr))
}

func containsSubstring(s, sub string) bool {
	for i := 0; i <= len(s)-len(sub); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
