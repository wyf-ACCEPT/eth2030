package vm

// EIP-7954: Increase Maximum Contract Size.
//
// Raises the deployed contract code size limit from 24,576 bytes (24 KiB) to
// 32,768 bytes (32 KiB) and the init code size limit from 49,152 bytes (48 KiB)
// to 65,536 bytes (64 KiB).

const (
	// MaxCodeSizeGlamsterdam is the EIP-7954 max deployed code size (32 KiB).
	MaxCodeSizeGlamsterdam = 32768 // 0x8000

	// MaxInitCodeSizeGlamsterdam is the EIP-7954 max init code size (64 KiB).
	MaxInitCodeSizeGlamsterdam = 65536 // 0x10000
)

// MaxCodeSizeForFork returns the maximum deployed contract code size for the
// given fork rules. Post-Glamsterdam uses EIP-7954 limits.
func MaxCodeSizeForFork(rules ForkRules) int {
	if rules.IsEIP7954 {
		return MaxCodeSizeGlamsterdam
	}
	return MaxCodeSize
}

// MaxInitCodeSizeForFork returns the maximum init code size for the given
// fork rules. Post-Glamsterdam uses EIP-7954 limits.
func MaxInitCodeSizeForFork(rules ForkRules) int {
	if rules.IsEIP7954 {
		return MaxInitCodeSizeGlamsterdam
	}
	return MaxInitCodeSize
}
