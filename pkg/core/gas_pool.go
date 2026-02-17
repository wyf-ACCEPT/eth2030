package core

import "errors"

// ErrGasPoolExhausted is returned when the block gas pool has insufficient gas.
var ErrGasPoolExhausted = errors.New("gas pool exhausted")

// GasPool tracks the amount of gas available during block execution.
type GasPool uint64

// AddGas adds gas to the pool.
func (gp *GasPool) AddGas(amount uint64) *GasPool {
	*gp += GasPool(amount)
	return gp
}

// SubGas subtracts gas from the pool, returning an error if insufficient.
func (gp *GasPool) SubGas(amount uint64) error {
	if uint64(*gp) < amount {
		return ErrGasPoolExhausted
	}
	*gp -= GasPool(amount)
	return nil
}

// Gas returns the amount of gas remaining in the pool.
func (gp *GasPool) Gas() uint64 {
	return uint64(*gp)
}
