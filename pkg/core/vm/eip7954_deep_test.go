package vm

import (
	"testing"
)

func TestCodeSizeLimitsForFork_Pre(t *testing.T) {
	rules := ForkRules{IsEIP7954: false}
	limits := CodeSizeLimitsForFork(rules)

	if limits.MaxCodeSize != MaxCodeSize {
		t.Errorf("MaxCodeSize = %d, want %d", limits.MaxCodeSize, MaxCodeSize)
	}
	if limits.MaxInitCodeSize != MaxInitCodeSize {
		t.Errorf("MaxInitCodeSize = %d, want %d", limits.MaxInitCodeSize, MaxInitCodeSize)
	}
	if limits.ForkName != "Pre-Glamsterdam" {
		t.Errorf("ForkName = %q, want Pre-Glamsterdam", limits.ForkName)
	}
}

func TestCodeSizeLimitsForFork_Post(t *testing.T) {
	rules := ForkRules{IsEIP7954: true}
	limits := CodeSizeLimitsForFork(rules)

	if limits.MaxCodeSize != MaxCodeSizeGlamsterdam {
		t.Errorf("MaxCodeSize = %d, want %d", limits.MaxCodeSize, MaxCodeSizeGlamsterdam)
	}
	if limits.MaxInitCodeSize != MaxInitCodeSizeGlamsterdam {
		t.Errorf("MaxInitCodeSize = %d, want %d", limits.MaxInitCodeSize, MaxInitCodeSizeGlamsterdam)
	}
	if limits.ForkName != "Glamsterdam" {
		t.Errorf("ForkName = %q, want Glamsterdam", limits.ForkName)
	}
}

func TestCodeSizeLimits_ValidateInitCode(t *testing.T) {
	limits := CodeSizeLimits{
		MaxCodeSize:     24576,
		MaxInitCodeSize: 49152,
		ForkName:        "Test",
	}

	// Empty init code.
	if err := limits.ValidateInitCode(nil); err != ErrInitCodeEmpty {
		t.Errorf("nil init code: got %v, want ErrInitCodeEmpty", err)
	}
	if err := limits.ValidateInitCode([]byte{}); err != ErrInitCodeEmpty {
		t.Errorf("empty init code: got %v, want ErrInitCodeEmpty", err)
	}

	// Valid init code.
	if err := limits.ValidateInitCode(make([]byte, 100)); err != nil {
		t.Errorf("valid init code: unexpected error %v", err)
	}

	// Exactly at limit.
	if err := limits.ValidateInitCode(make([]byte, 49152)); err != nil {
		t.Errorf("init code at limit: unexpected error %v", err)
	}

	// Over limit.
	if err := limits.ValidateInitCode(make([]byte, 49153)); err == nil {
		t.Error("init code over limit: expected error")
	}
}

func TestCodeSizeLimits_ValidateDeployedCode(t *testing.T) {
	limits := CodeSizeLimits{
		MaxCodeSize:     24576,
		MaxInitCodeSize: 49152,
		ForkName:        "Test",
	}

	// Empty deployed code is valid (nothing to deploy).
	if err := limits.ValidateDeployedCode(nil); err != nil {
		t.Errorf("nil deployed code: unexpected error %v", err)
	}

	// Valid.
	if err := limits.ValidateDeployedCode(make([]byte, 100)); err != nil {
		t.Errorf("valid deployed code: unexpected error %v", err)
	}

	// Exactly at limit.
	if err := limits.ValidateDeployedCode(make([]byte, 24576)); err != nil {
		t.Errorf("deployed code at limit: unexpected error %v", err)
	}

	// Over limit.
	if err := limits.ValidateDeployedCode(make([]byte, 24577)); err == nil {
		t.Error("deployed code over limit: expected error")
	}
}

func TestInitCodeGas(t *testing.T) {
	tests := []struct {
		name    string
		size    int
		wantGas uint64
	}{
		{"empty", 0, 0},
		{"1 byte", 1, InitCodeWordGas},          // ceil(1/32) = 1 word
		{"32 bytes", 32, InitCodeWordGas},       // 1 word
		{"33 bytes", 33, 2 * InitCodeWordGas},   // 2 words
		{"64 bytes", 64, 2 * InitCodeWordGas},   // 2 words
		{"100 bytes", 100, 4 * InitCodeWordGas}, // ceil(100/32) = 4 words
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			code := make([]byte, tt.size)
			got := InitCodeGas(code)
			if got != tt.wantGas {
				t.Errorf("InitCodeGas(%d bytes) = %d, want %d", tt.size, got, tt.wantGas)
			}
		})
	}
}

func TestCodeDepositGas(t *testing.T) {
	// Pre-Glamsterdam: 200 per byte.
	preFork := ForkRules{IsGlamsterdan: false}
	if got := CodeDepositGas(100, preFork); got != 100*CreateDataGas {
		t.Errorf("pre-fork deposit gas = %d, want %d", got, 100*CreateDataGas)
	}

	// Post-Glamsterdam: 662 per byte.
	postFork := ForkRules{IsGlamsterdan: true}
	if got := CodeDepositGas(100, postFork); got != 100*GasCodeDepositGlamsterdam {
		t.Errorf("post-fork deposit gas = %d, want %d", got, 100*GasCodeDepositGlamsterdam)
	}

	// Zero length.
	if got := CodeDepositGas(0, preFork); got != 0 {
		t.Errorf("zero-length deposit gas = %d, want 0", got)
	}

	// Negative length.
	if got := CodeDepositGas(-1, preFork); got != 0 {
		t.Errorf("negative-length deposit gas = %d, want 0", got)
	}
}

func TestCreateGasTotal(t *testing.T) {
	// Pre-Glamsterdam: base 32000, init 2 words = 4, deposit 100*200 = 20000.
	preFork := ForkRules{}
	total := CreateGasTotal(64, 100, preFork)
	expected := GasCreate + 2*InitCodeWordGas + 100*CreateDataGas
	if total != expected {
		t.Errorf("pre-fork CreateGasTotal = %d, want %d", total, expected)
	}

	// Glamsterdam: base 83144, init 2 words = 4, deposit 100*662 = 66200.
	postFork := ForkRules{IsGlamsterdan: true}
	total = CreateGasTotal(64, 100, postFork)
	expected = GasCreateGlamsterdam + 2*InitCodeWordGas + 100*GasCodeDepositGlamsterdam
	if total != expected {
		t.Errorf("post-fork CreateGasTotal = %d, want %d", total, expected)
	}
}

func TestAnalyzeCodeSize_Valid(t *testing.T) {
	rules := ForkRules{IsEIP7954: true}
	initCode := make([]byte, 1024)
	deployedCode := make([]byte, 500)

	report := AnalyzeCodeSize(initCode, deployedCode, rules)
	if !report.Valid {
		t.Fatalf("expected valid report, got error: %s", report.Error)
	}
	if report.InitCodeSize != 1024 {
		t.Errorf("InitCodeSize = %d, want 1024", report.InitCodeSize)
	}
	if report.DeployedCodeSize != 500 {
		t.Errorf("DeployedCodeSize = %d, want 500", report.DeployedCodeSize)
	}
	if report.InitCodeGas == 0 {
		t.Error("InitCodeGas should be nonzero")
	}
	if report.DepositGas == 0 {
		t.Error("DepositGas should be nonzero")
	}
	if report.TotalCreateGas == 0 {
		t.Error("TotalCreateGas should be nonzero")
	}
}

func TestAnalyzeCodeSize_InitTooLarge(t *testing.T) {
	rules := ForkRules{IsEIP7954: false}
	initCode := make([]byte, MaxInitCodeSize+1)
	deployedCode := make([]byte, 100)

	report := AnalyzeCodeSize(initCode, deployedCode, rules)
	if report.Valid {
		t.Error("expected invalid report for oversized init code")
	}
}

func TestAnalyzeCodeSize_DeployedTooLarge(t *testing.T) {
	rules := ForkRules{IsEIP7954: false}
	initCode := make([]byte, 100)
	deployedCode := make([]byte, MaxCodeSize+1)

	report := AnalyzeCodeSize(initCode, deployedCode, rules)
	if report.Valid {
		t.Error("expected invalid report for oversized deployed code")
	}
}

func TestAnalyzeCodeSize_EmptyInit(t *testing.T) {
	rules := ForkRules{}
	report := AnalyzeCodeSize(nil, make([]byte, 100), rules)
	if report.Valid {
		t.Error("expected invalid report for empty init code")
	}
}

func TestIsEIP7954Active(t *testing.T) {
	if IsEIP7954Active(ForkRules{}) {
		t.Error("should not be active for empty rules")
	}
	if !IsEIP7954Active(ForkRules{IsEIP7954: true}) {
		t.Error("should be active when flag is set")
	}
}

func TestCodeSizeUtilization(t *testing.T) {
	rules := ForkRules{IsEIP7954: false}

	// Exactly at limit.
	u := CodeSizeUtilization(MaxCodeSize, rules)
	if u != 1.0 {
		t.Errorf("at-limit utilization = %f, want 1.0", u)
	}

	// Half.
	u = CodeSizeUtilization(MaxCodeSize/2, rules)
	if u < 0.49 || u > 0.51 {
		t.Errorf("half utilization = %f, want ~0.5", u)
	}

	// Over limit.
	u = CodeSizeUtilization(MaxCodeSize+1000, rules)
	if u <= 1.0 {
		t.Errorf("over-limit utilization = %f, want > 1.0", u)
	}
}

func TestInitCodeSizeUtilization(t *testing.T) {
	rules := ForkRules{IsEIP7954: true}

	u := InitCodeSizeUtilization(MaxInitCodeSizeGlamsterdam, rules)
	if u != 1.0 {
		t.Errorf("at-limit utilization = %f, want 1.0", u)
	}

	u = InitCodeSizeUtilization(0, rules)
	if u != 0 {
		t.Errorf("zero utilization = %f, want 0", u)
	}
}
