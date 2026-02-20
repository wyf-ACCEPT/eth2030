package vm

import "testing"

func TestGasConstants(t *testing.T) {
	tests := []struct {
		name  string
		value uint64
		want  uint64
	}{
		{"GasBase", GasBase, 2},
		{"GasVerylow", GasVerylow, 3},
		{"GasLow", GasLow, 5},
		{"GasMid", GasMid, 8},
		{"GasHigh", GasHigh, 10},
		{"GasExt", GasExt, 20},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.value != tt.want {
				t.Errorf("%s = %d, want %d", tt.name, tt.value, tt.want)
			}
		})
	}
}

func TestGasLegacyAliases(t *testing.T) {
	if GasQuickStep != GasBase {
		t.Errorf("GasQuickStep = %d, want GasBase (%d)", GasQuickStep, GasBase)
	}
	if GasFastestStep != GasVerylow {
		t.Errorf("GasFastestStep = %d, want GasVerylow (%d)", GasFastestStep, GasVerylow)
	}
	if GasFastStep != GasLow {
		t.Errorf("GasFastStep = %d, want GasLow (%d)", GasFastStep, GasLow)
	}
	if GasMidStep != GasMid {
		t.Errorf("GasMidStep = %d, want GasMid (%d)", GasMidStep, GasMid)
	}
	if GasSlowStep != GasHigh {
		t.Errorf("GasSlowStep = %d, want GasHigh (%d)", GasSlowStep, GasHigh)
	}
	if GasExtStep != GasExt {
		t.Errorf("GasExtStep = %d, want GasExt (%d)", GasExtStep, GasExt)
	}
}

func TestGasStateAccessCosts(t *testing.T) {
	if GasBalanceCold != 2600 {
		t.Errorf("GasBalanceCold = %d, want 2600", GasBalanceCold)
	}
	if GasBalanceWarm != 100 {
		t.Errorf("GasBalanceWarm = %d, want 100", GasBalanceWarm)
	}
	if GasSloadCold != 2100 {
		t.Errorf("GasSloadCold = %d, want 2100", GasSloadCold)
	}
	if GasSloadWarm != 100 {
		t.Errorf("GasSloadWarm = %d, want 100", GasSloadWarm)
	}
	if GasSstoreSet != 20000 {
		t.Errorf("GasSstoreSet = %d, want 20000", GasSstoreSet)
	}
	if GasSstoreReset != 2900 {
		t.Errorf("GasSstoreReset = %d, want 2900", GasSstoreReset)
	}
}

func TestGasCallCosts(t *testing.T) {
	if GasCallCold != 2600 {
		t.Errorf("GasCallCold = %d, want 2600", GasCallCold)
	}
	if GasCallWarm != 100 {
		t.Errorf("GasCallWarm = %d, want 100", GasCallWarm)
	}
}

func TestGasLogCosts(t *testing.T) {
	if GasLog != 375 {
		t.Errorf("GasLog = %d, want 375", GasLog)
	}
	if GasLogTopic != 375 {
		t.Errorf("GasLogTopic = %d, want 375", GasLogTopic)
	}
	if GasLogData != 8 {
		t.Errorf("GasLogData = %d, want 8", GasLogData)
	}
}

func TestGasKeccak256Costs(t *testing.T) {
	if GasKeccak256 != 30 {
		t.Errorf("GasKeccak256 = %d, want 30", GasKeccak256)
	}
	if GasKeccak256Word != 6 {
		t.Errorf("GasKeccak256Word = %d, want 6", GasKeccak256Word)
	}
}

func TestGasMemoryCosts(t *testing.T) {
	if GasMemory != 3 {
		t.Errorf("GasMemory = %d, want 3", GasMemory)
	}
	if GasCopy != 3 {
		t.Errorf("GasCopy = %d, want 3", GasCopy)
	}
}

func TestGasReturnCosts(t *testing.T) {
	if GasReturn != 0 {
		t.Errorf("GasReturn = %d, want 0", GasReturn)
	}
	if GasStop != 0 {
		t.Errorf("GasStop = %d, want 0", GasStop)
	}
	if GasRevert != 0 {
		t.Errorf("GasRevert = %d, want 0", GasRevert)
	}
}

func TestGasFlowControlCosts(t *testing.T) {
	if GasJumpDest != 1 {
		t.Errorf("GasJumpDest = %d, want 1", GasJumpDest)
	}
	if GasJump != 8 {
		t.Errorf("GasJump = %d, want 8", GasJump)
	}
	if GasJumpi != 10 {
		t.Errorf("GasJumpi = %d, want 10", GasJumpi)
	}
}

func TestGasStackOpsCosts(t *testing.T) {
	if GasPush0 != 2 {
		t.Errorf("GasPush0 = %d, want 2", GasPush0)
	}
	if GasPush != 3 {
		t.Errorf("GasPush = %d, want 3", GasPush)
	}
	if GasDup != 3 {
		t.Errorf("GasDup = %d, want 3", GasDup)
	}
	if GasSwap != 3 {
		t.Errorf("GasSwap = %d, want 3", GasSwap)
	}
	if GasPop != 2 {
		t.Errorf("GasPop = %d, want 2", GasPop)
	}
}

func TestGasCreateCosts(t *testing.T) {
	if GasCreate != 32000 {
		t.Errorf("GasCreate = %d, want 32000", GasCreate)
	}
	if GasSelfdestruct != 5000 {
		t.Errorf("GasSelfdestruct = %d, want 5000", GasSelfdestruct)
	}
}

func TestGasCancunOpcodeCosts(t *testing.T) {
	if GasTload != 100 {
		t.Errorf("GasTload = %d, want 100", GasTload)
	}
	if GasTstore != 100 {
		t.Errorf("GasTstore = %d, want 100", GasTstore)
	}
	if GasBlobHash != 3 {
		t.Errorf("GasBlobHash = %d, want 3", GasBlobHash)
	}
	if GasBlobBaseFee != 2 {
		t.Errorf("GasBlobBaseFee = %d, want 2", GasBlobBaseFee)
	}
	if GasMcopyBase != 3 {
		t.Errorf("GasMcopyBase = %d, want 3", GasMcopyBase)
	}
}

func TestGasGlamsterdanRepricedCosts(t *testing.T) {
	// EIP-7904 repriced opcodes.
	if GasDivGlamsterdan != 15 {
		t.Errorf("GasDivGlamsterdan = %d, want 15", GasDivGlamsterdan)
	}
	if GasSdivGlamsterdan != 20 {
		t.Errorf("GasSdivGlamsterdan = %d, want 20", GasSdivGlamsterdan)
	}
	if GasModGlamsterdan != 12 {
		t.Errorf("GasModGlamsterdan = %d, want 12", GasModGlamsterdan)
	}
	if GasMulmodGlamsterdan != 11 {
		t.Errorf("GasMulmodGlamsterdan = %d, want 11", GasMulmodGlamsterdan)
	}
	if GasKeccak256Glamsterdan != 45 {
		t.Errorf("GasKeccak256Glamsterdan = %d, want 45", GasKeccak256Glamsterdan)
	}
}

func TestGasGlamsterdanPrecompileCosts(t *testing.T) {
	if GasECADDGlamsterdan != 314 {
		t.Errorf("GasECADDGlamsterdan = %d, want 314", GasECADDGlamsterdan)
	}
	if GasBlake2fConstGlamsterdan != 170 {
		t.Errorf("GasBlake2fConstGlamsterdan = %d, want 170", GasBlake2fConstGlamsterdan)
	}
	if GasBlake2fPerRoundGlamsterdan != 2 {
		t.Errorf("GasBlake2fPerRoundGlamsterdan = %d, want 2", GasBlake2fPerRoundGlamsterdan)
	}
	if GasPointEvalGlamsterdan != 89363 {
		t.Errorf("GasPointEvalGlamsterdan = %d, want 89363", GasPointEvalGlamsterdan)
	}
}

func TestGasVerkleCosts(t *testing.T) {
	if WitnessBranchCost != 1900 {
		t.Errorf("WitnessBranchCost = %d, want 1900", WitnessBranchCost)
	}
	if WitnessChunkCost != 200 {
		t.Errorf("WitnessChunkCost = %d, want 200", WitnessChunkCost)
	}
	if SubtreeEditCost != 3000 {
		t.Errorf("SubtreeEditCost = %d, want 3000", SubtreeEditCost)
	}
	if ChunkEditCost != 500 {
		t.Errorf("ChunkEditCost = %d, want 500", ChunkEditCost)
	}
	if ChunkFillCost != 6200 {
		t.Errorf("ChunkFillCost = %d, want 6200", ChunkFillCost)
	}
	if GasCreateVerkle != 1000 {
		t.Errorf("GasCreateVerkle = %d, want 1000", GasCreateVerkle)
	}
	if CodeChunkSize != 31 {
		t.Errorf("CodeChunkSize = %d, want 31", CodeChunkSize)
	}
}
