package vm

// Gas cost constants following the Cancun hard fork specification.
const (
	GasQuickStep   uint64 = 2     // ADD, SUB, etc.
	GasFastestStep uint64 = 3     // MUL, etc.
	GasFastStep    uint64 = 5     // DIV, SDIV, MOD, SMOD, etc.
	GasMidStep     uint64 = 8     // ADDMOD, MULMOD, etc.
	GasSlowStep    uint64 = 10    // EXP base cost
	GasExtStep     uint64 = 20    // BLOCKHASH

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
)
