package vm

// Gas cost constants following the Cancun hard fork specification.
// Gas tiers per Yellow Paper Appendix G:
//   Gzero=0, Gbase=2, Gverylow=3, Glow=5, Gmid=8, Ghigh=10, Gext=20
const (
	GasBase    uint64 = 2  // Gbase: ADDRESS, ORIGIN, CALLER, CALLVALUE, CALLDATASIZE, CODESIZE, GASPRICE, COINBASE, TIMESTAMP, NUMBER, PREVRANDAO, GASLIMIT, POP, PC, MSIZE, GAS, CHAINID, BASEFEE, RETURNDATASIZE, BLOBBASEFEE, PUSH0
	GasVerylow uint64 = 3  // Gverylow: ADD, SUB, NOT, LT, GT, SLT, SGT, EQ, ISZERO, AND, OR, XOR, BYTE, SHL, SHR, SAR, CALLDATALOAD, MLOAD, MSTORE, MSTORE8, PUSH*, DUP*, SWAP*, CALLDATACOPY, CODECOPY, RETURNDATACOPY
	GasLow     uint64 = 5  // Glow: DIV, SDIV, MOD, SMOD, SIGNEXTEND
	GasMid     uint64 = 8  // Gmid: ADDMOD, MULMOD, JUMP
	GasHigh    uint64 = 10 // Ghigh: JUMPI, EXP base cost
	GasExt     uint64 = 20 // Gext: BLOCKHASH

	// Legacy aliases for backward compatibility. Prefer the named tiers above.
	GasQuickStep   = GasBase
	GasFastestStep = GasVerylow
	GasFastStep    = GasLow
	GasMidStep     = GasMid
	GasSlowStep    = GasHigh
	GasExtStep     = GasExt

	GasBalanceCold uint64 = 2600  // BALANCE cold access
	GasBalanceWarm uint64 = 100   // BALANCE warm access
	GasSloadCold   uint64 = 2100  // SLOAD cold access
	GasSloadWarm   uint64 = 100   // SLOAD warm access
	GasSstoreSet   uint64 = 20000 // SSTORE from zero to non-zero
	GasSstoreReset uint64 = 2900  // SSTORE from non-zero to non-zero

	GasCreate       uint64 = 32000
	GasSelfdestruct uint64 = 5000

	GasCallCold uint64 = 2600 // CALL cold access
	GasCallWarm uint64 = 100  // CALL warm access

	GasLog      uint64 = 375 // per LOG operation
	GasLogTopic uint64 = 375 // per topic
	GasLogData  uint64 = 8   // per byte of log data

	GasKeccak256     uint64 = 30 // base cost
	GasKeccak256Word uint64 = 6  // per 32-byte word

	GasMemory uint64 = 3 // per word for memory expansion
	GasCopy   uint64 = 3 // per word for copy operations

	GasReturn uint64 = 0
	GasStop   uint64 = 0
	GasRevert uint64 = 0

	GasJumpDest uint64 = 1
	GasJump     uint64 = 8
	GasJumpi    uint64 = 10

	GasPush0     uint64 = 2
	GasPush      uint64 = 3 // PUSH1-PUSH32
	GasDup       uint64 = 3
	GasSwap      uint64 = 3
	GasPop       uint64 = 2
	GasMload     uint64 = 3
	GasMstore    uint64 = 3
	GasMstore8   uint64 = 3
	GasPc        uint64 = 2
	GasMsize     uint64 = 2
	GasGas       uint64 = 2

	// Cancun opcodes (EIP-1153, EIP-5656, EIP-4844, EIP-7516)
	GasTload       uint64 = 100 // EIP-1153: transient storage load
	GasTstore      uint64 = 100 // EIP-1153: transient storage store
	GasBlobHash    uint64 = 3   // EIP-4844: BLOBHASH
	GasBlobBaseFee uint64 = 2   // EIP-7516: BLOBBASEFEE
	GasMcopyBase   uint64 = 3   // EIP-5656: MCOPY base cost

	// EIP-7904: Glamsterdan compute gas cost increases.
	GasDivGlamsterdan       uint64 = 15 // DIV (was 5)
	GasSdivGlamsterdan      uint64 = 20 // SDIV (was 5)
	GasModGlamsterdan       uint64 = 12 // MOD (was 5)
	GasMulmodGlamsterdan    uint64 = 11 // MULMOD (was 8)
	GasKeccak256Glamsterdan uint64 = 45 // KECCAK256 constant (was 30)

	// EIP-7904: Precompile gas cost increases.
	GasECADDGlamsterdan           uint64 = 314   // bn256Add (was 150)
	GasBlake2fConstGlamsterdan    uint64 = 170   // blake2F constant (was 0)
	GasBlake2fPerRoundGlamsterdan uint64 = 2     // blake2F per round (was 1)
	GasPointEvalGlamsterdan       uint64 = 89363 // KZG point evaluation (was 50000)
	GasECPairingConstGlamsterdan  uint64 = 45000 // bn256Pairing constant (unchanged)
	GasECPairingPerPairGlamsterdan uint64 = 34103 // bn256Pairing per pair (was 34000)

	// EIP-4762: Statelessness gas cost changes (Verkle).
	// These costs reflect the witness size needed for state access proofs.
	WitnessBranchCost uint64 = 1900 // accessing a new subtree (branch node)
	WitnessChunkCost  uint64 = 200  // accessing a new leaf chunk
	SubtreeEditCost   uint64 = 3000 // first write to a subtree
	ChunkEditCost     uint64 = 500  // first write to a leaf chunk
	ChunkFillCost     uint64 = 6200 // writing to a previously-empty leaf

	// EIP-4762: reduced CREATE cost under Verkle.
	GasCreateVerkle uint64 = 1000

	// EIP-4762: code chunk size for witness gas accounting.
	CodeChunkSize uint64 = 31
)
