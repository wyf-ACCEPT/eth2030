package vm

import (
	"sync"
	"testing"
)

func TestForkLevelString(t *testing.T) {
	tests := []struct {
		fork ForkLevel
		want string
	}{
		{ForkGlamsterdam, "Glamsterdam"},
		{ForkHogota, "Hogota"},
		{ForkI, "I+"},
		{ForkJ, "J+"},
		{ForkK, "K+"},
		{ForkLevel(99), "Unknown"},
	}
	for _, tt := range tests {
		if got := tt.fork.String(); got != tt.want {
			t.Errorf("ForkLevel(%d).String() = %q, want %q", tt.fork, got, tt.want)
		}
	}
}

func TestNewRepricingEngine_Glamsterdam(t *testing.T) {
	e := NewRepricingEngine(ForkGlamsterdam)

	// SLOAD should be repriced to Glamsterdam value.
	got := e.CurrentGasCost(byte(SLOAD))
	if got != ColdSloadGlamst {
		t.Errorf("SLOAD at Glamsterdam: got %d, want %d", got, ColdSloadGlamst)
	}

	// BALANCE should be repriced to Glamsterdam value.
	got = e.CurrentGasCost(byte(BALANCE))
	if got != ColdAccountAccessGlamst {
		t.Errorf("BALANCE at Glamsterdam: got %d, want %d", got, ColdAccountAccessGlamst)
	}

	// KECCAK256 should be repriced.
	got = e.CurrentGasCost(byte(KECCAK256))
	if got != GasKeccak256Glamsterdan {
		t.Errorf("KECCAK256 at Glamsterdam: got %d, want %d", got, GasKeccak256Glamsterdan)
	}

	// CREATE should be repriced.
	got = e.CurrentGasCost(byte(CREATE))
	if got != GasCreateGlamsterdam {
		t.Errorf("CREATE at Glamsterdam: got %d, want %d", got, GasCreateGlamsterdam)
	}
}

func TestNewRepricingEngine_Hogota(t *testing.T) {
	e := NewRepricingEngine(ForkHogota)

	// SLOAD repriced in Hogota: 2100 -> Glamst -> 1800.
	got := e.CurrentGasCost(byte(SLOAD))
	if got != 1800 {
		t.Errorf("SLOAD at Hogota: got %d, want 1800", got)
	}

	// BALANCE repriced in Hogota: 2600 -> Glamst -> 400.
	got = e.CurrentGasCost(byte(BALANCE))
	if got != 400 {
		t.Errorf("BALANCE at Hogota: got %d, want 400", got)
	}

	// EXTCODESIZE should still be at Glamsterdam value (not repriced in Hogota).
	got = e.CurrentGasCost(byte(EXTCODESIZE))
	if got != ColdAccountAccessGlamst {
		t.Errorf("EXTCODESIZE at Hogota: got %d, want %d", got, ColdAccountAccessGlamst)
	}
}

func TestNewRepricingEngine_ForkI(t *testing.T) {
	e := NewRepricingEngine(ForkI)

	// EXTCODESIZE repriced at I+.
	got := e.CurrentGasCost(byte(EXTCODESIZE))
	if got != 100 {
		t.Errorf("EXTCODESIZE at I+: got %d, want 100", got)
	}

	// EXTCODEHASH repriced at I+.
	got = e.CurrentGasCost(byte(EXTCODEHASH))
	if got != 200 {
		t.Errorf("EXTCODEHASH at I+: got %d, want 200", got)
	}

	// SLOAD should still be Hogota value.
	got = e.CurrentGasCost(byte(SLOAD))
	if got != 1800 {
		t.Errorf("SLOAD at I+: got %d, want 1800", got)
	}
}

func TestNewRepricingEngine_ForkJ(t *testing.T) {
	e := NewRepricingEngine(ForkJ)

	// CREATE repriced at J+.
	got := e.CurrentGasCost(byte(CREATE))
	if got != 20000 {
		t.Errorf("CREATE at J+: got %d, want 20000", got)
	}

	// CREATE2 repriced at J+.
	got = e.CurrentGasCost(byte(CREATE2))
	if got != 20000 {
		t.Errorf("CREATE2 at J+: got %d, want 20000", got)
	}
}

func TestNewRepricingEngine_ForkK(t *testing.T) {
	e := NewRepricingEngine(ForkK)

	// SLOAD repriced at K+.
	got := e.CurrentGasCost(byte(SLOAD))
	if got != 800 {
		t.Errorf("SLOAD at K+: got %d, want 800", got)
	}

	// CALL repriced at K+.
	got = e.CurrentGasCost(byte(CALL))
	if got != 1000 {
		t.Errorf("CALL at K+: got %d, want 1000", got)
	}

	// STATICCALL repriced at K+.
	got = e.CurrentGasCost(byte(STATICCALL))
	if got != 1000 {
		t.Errorf("STATICCALL at K+: got %d, want 1000", got)
	}

	// CREATE should still be J+ value.
	got = e.CurrentGasCost(byte(CREATE))
	if got != 20000 {
		t.Errorf("CREATE at K+: got %d, want 20000", got)
	}
}

func TestGetOpGasCost_CrossFork(t *testing.T) {
	e := NewRepricingEngine(ForkK) // engine at K+

	// GetOpGasCost should return the cost for any fork, regardless of engine state.
	tests := []struct {
		name   string
		opcode byte
		fork   ForkLevel
		want   uint64
	}{
		{"SLOAD base", byte(SLOAD), ForkLevel(-1), ColdSloadCost},
		{"SLOAD Glamsterdam", byte(SLOAD), ForkGlamsterdam, ColdSloadGlamst},
		{"SLOAD Hogota", byte(SLOAD), ForkHogota, 1800},
		{"SLOAD K+", byte(SLOAD), ForkK, 800},
		{"BALANCE Hogota", byte(BALANCE), ForkHogota, 400},
		{"EXTCODESIZE I+", byte(EXTCODESIZE), ForkI, 100},
		{"CREATE J+", byte(CREATE), ForkJ, 20000},
	}
	for _, tt := range tests {
		got := e.GetOpGasCost(tt.opcode, tt.fork)
		if got != tt.want {
			t.Errorf("%s: got %d, want %d", tt.name, got, tt.want)
		}
	}
}

func TestGetRepricingRules(t *testing.T) {
	e := NewRepricingEngine(ForkGlamsterdam)

	rules := e.GetRepricingRules(ForkHogota)
	if len(rules) != 2 {
		t.Fatalf("Hogota rules: got %d rules, want 2", len(rules))
	}

	// Verify the rules are copies (mutating them should not affect the engine).
	rules[0].NewGas = 999999
	original := e.GetRepricingRules(ForkHogota)
	if original[0].NewGas == 999999 {
		t.Error("GetRepricingRules returned mutable references, want copies")
	}

	// Check Glamsterdam has rules.
	glamRules := e.GetRepricingRules(ForkGlamsterdam)
	if len(glamRules) == 0 {
		t.Error("Glamsterdam should have repricing rules")
	}

	// Verify specific Hogota rules (use the fresh copy, not the mutated one).
	found := false
	for _, r := range original {
		if r.Opcode == byte(SLOAD) && r.NewGas == 1800 {
			found = true
		}
	}
	if !found {
		t.Error("expected SLOAD->1800 rule in Hogota rules")
	}
}

func TestGetRepricingRules_NoRulesForFork(t *testing.T) {
	e := NewRepricingEngine(ForkGlamsterdam)

	// ForkLevel -1 has no rules.
	rules := e.GetRepricingRules(ForkLevel(-1))
	if len(rules) != 0 {
		t.Errorf("got %d rules for invalid fork, want 0", len(rules))
	}
}

func TestApplyRepricing(t *testing.T) {
	e := NewRepricingEngine(ForkGlamsterdam)

	// Initially at Glamsterdam.
	if e.CurrentFork() != ForkGlamsterdam {
		t.Errorf("initial fork: got %v, want Glamsterdam", e.CurrentFork())
	}

	// Apply Hogota repricing.
	e.ApplyRepricing(ForkHogota)
	if e.CurrentFork() != ForkHogota {
		t.Errorf("after ApplyRepricing(Hogota): got %v, want Hogota", e.CurrentFork())
	}

	got := e.CurrentGasCost(byte(SLOAD))
	if got != 1800 {
		t.Errorf("SLOAD after Hogota apply: got %d, want 1800", got)
	}

	// Apply K+ repricing.
	e.ApplyRepricing(ForkK)
	got = e.CurrentGasCost(byte(SLOAD))
	if got != 800 {
		t.Errorf("SLOAD after K+ apply: got %d, want 800", got)
	}

	// Re-apply Glamsterdam (downgrade should reset).
	e.ApplyRepricing(ForkGlamsterdam)
	got = e.CurrentGasCost(byte(SLOAD))
	if got != ColdSloadGlamst {
		t.Errorf("SLOAD after re-apply Glamsterdam: got %d, want %d", got, ColdSloadGlamst)
	}
}

func TestEstimateImpact_BasicCase(t *testing.T) {
	e := NewRepricingEngine(ForkGlamsterdam)

	// Simulate bytecode with SLOAD (0x54) and BALANCE (0x31).
	txData := []byte{byte(SLOAD), byte(BALANCE), byte(SLOAD)}

	impact := e.EstimateImpact(txData, ForkHogota)
	if impact == nil {
		t.Fatal("EstimateImpact returned nil")
	}

	// Old gas (Glamsterdam): SLOAD=2800 + BALANCE=3500 + SLOAD=2800 = 9100
	expectedOld := uint64(ColdSloadGlamst + ColdAccountAccessGlamst + ColdSloadGlamst)
	if impact.OldGas != expectedOld {
		t.Errorf("OldGas: got %d, want %d", impact.OldGas, expectedOld)
	}

	// New gas (Hogota): SLOAD=1800 + BALANCE=400 + SLOAD=1800 = 4000
	expectedNew := uint64(1800 + 400 + 1800)
	if impact.NewGas != expectedNew {
		t.Errorf("NewGas: got %d, want %d", impact.NewGas, expectedNew)
	}

	// Savings should be positive (cost decreased).
	if impact.Savings <= 0 {
		t.Errorf("Savings should be positive, got %d", impact.Savings)
	}

	// PercentChange should be negative (costs went down).
	if impact.PercentChange >= 0 {
		t.Errorf("PercentChange should be negative, got %f", impact.PercentChange)
	}
}

func TestEstimateImpact_EmptyData(t *testing.T) {
	e := NewRepricingEngine(ForkGlamsterdam)
	impact := e.EstimateImpact(nil, ForkHogota)
	if impact.OldGas != 0 || impact.NewGas != 0 {
		t.Errorf("empty data: got old=%d new=%d, want both 0", impact.OldGas, impact.NewGas)
	}
	if impact.PercentChange != 0 {
		t.Errorf("empty data: percent change should be 0, got %f", impact.PercentChange)
	}
}

func TestEstimateImpact_SameFork(t *testing.T) {
	e := NewRepricingEngine(ForkHogota)
	txData := []byte{byte(SLOAD), byte(BALANCE)}
	impact := e.EstimateImpact(txData, ForkHogota)
	if impact.Savings != 0 {
		t.Errorf("same fork: savings should be 0, got %d", impact.Savings)
	}
	if impact.OldGas != impact.NewGas {
		t.Errorf("same fork: OldGas (%d) should equal NewGas (%d)", impact.OldGas, impact.NewGas)
	}
}

func TestRepricingEngine_ConcurrentAccess(t *testing.T) {
	e := NewRepricingEngine(ForkGlamsterdam)

	var wg sync.WaitGroup
	errCh := make(chan string, 100)

	// Concurrent readers.
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			cost := e.CurrentGasCost(byte(SLOAD))
			if cost == 0 {
				errCh <- "SLOAD cost was 0"
			}
			_ = e.GetRepricingRules(ForkHogota)
			_ = e.GetOpGasCost(byte(BALANCE), ForkHogota)
			_ = e.CurrentFork()
		}()
	}

	// Concurrent writers.
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			fork := ForkLevel(idx % 5)
			e.ApplyRepricing(fork)
		}(i)
	}

	// Concurrent impact estimations.
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			data := []byte{byte(SLOAD), byte(BALANCE), byte(CREATE)}
			_ = e.EstimateImpact(data, ForkK)
		}()
	}

	wg.Wait()
	close(errCh)
	for msg := range errCh {
		t.Error(msg)
	}
}

func TestRepricingRules_ConsistentOldNewGas(t *testing.T) {
	e := NewRepricingEngine(ForkGlamsterdam)
	for fork := ForkGlamsterdam; fork <= ForkK; fork++ {
		rules := e.GetRepricingRules(fork)
		for _, r := range rules {
			if r.OldGas == r.NewGas {
				t.Errorf("fork %v opcode 0x%02x: OldGas == NewGas (%d), no actual repricing",
					fork, r.Opcode, r.OldGas)
			}
			if r.Reason == "" {
				t.Errorf("fork %v opcode 0x%02x: empty Reason", fork, r.Opcode)
			}
		}
	}
}

func TestRepricingEngine_AllForksHaveRules(t *testing.T) {
	e := NewRepricingEngine(ForkGlamsterdam)
	for fork := ForkGlamsterdam; fork <= ForkK; fork++ {
		rules := e.GetRepricingRules(fork)
		if len(rules) == 0 {
			t.Errorf("fork %v has no repricing rules", fork)
		}
	}
}

func TestRepricingEngine_CumulativeApplication(t *testing.T) {
	// Verify that applying forks cumulatively produces the same result
	// as creating an engine at that fork directly.
	for fork := ForkGlamsterdam; fork <= ForkK; fork++ {
		direct := NewRepricingEngine(fork)
		incremental := NewRepricingEngine(ForkGlamsterdam)
		incremental.ApplyRepricing(fork)

		for op := 0; op < 256; op++ {
			d := direct.CurrentGasCost(byte(op))
			i := incremental.CurrentGasCost(byte(op))
			if d != i {
				t.Errorf("fork %v opcode 0x%02x: direct=%d incremental=%d",
					fork, op, d, i)
			}
		}
	}
}

func TestBaseGasCosts_NonZero(t *testing.T) {
	costs := baseGasCosts()

	// Key opcodes that should have non-zero base costs.
	checks := []struct {
		op   OpCode
		want uint64
	}{
		{SLOAD, ColdSloadCost},
		{BALANCE, ColdAccountAccessCost},
		{EXTCODESIZE, ColdAccountAccessCost},
		{CREATE, GasCreate},
		{CREATE2, GasCreate},
		{CALL, GasCallCold},
		{SELFDESTRUCT, GasSelfdestruct},
		{KECCAK256, GasKeccak256},
	}
	for _, c := range checks {
		if costs[byte(c.op)] != c.want {
			t.Errorf("base cost for %s: got %d, want %d", c.op, costs[byte(c.op)], c.want)
		}
	}
}
