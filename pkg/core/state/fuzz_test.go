package state

import (
	"math/big"
	"testing"

	"github.com/eth2028/eth2028/core/types"
)

// Operation constants for FuzzStateDBOperations.
const (
	opCreateAccount = iota
	opSetBalance
	opAddBalance
	opSubBalance
	opSetNonce
	opSetCode
	opSetState
	opGetBalance
	opGetNonce
	opGetCode
	opGetState
	opSelfDestruct
	opExist
	opEmpty
	opAddRefund
	opAddressToAccessList
	opSlotToAccessList
	opSetTransientState
	opGetTransientState
	numOps
)

// FuzzStateDBOperations creates a MemoryStateDB and performs a random sequence
// of operations using fuzz data to determine operation type and parameters.
// Must not panic on any input sequence.
func FuzzStateDBOperations(f *testing.F) {
	// Seed: a minimal sequence of operations.
	// Each op: type(1) + addr_byte(1) + extra(up to 40 bytes).
	f.Add([]byte{
		byte(opCreateAccount), 0x01,
		byte(opSetBalance), 0x01, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x01, 0x00,
		byte(opSetNonce), 0x01, 0x00, 0x00, 0x00, 0x05,
		byte(opGetBalance), 0x01,
		byte(opGetNonce), 0x01,
	})
	// Seed with multiple addresses.
	f.Add([]byte{
		byte(opCreateAccount), 0x01,
		byte(opCreateAccount), 0x02,
		byte(opAddBalance), 0x01, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x64,
		byte(opSubBalance), 0x01, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x0a,
		byte(opSetState), 0x01,
	})
	// Seed with self-destruct.
	f.Add([]byte{
		byte(opCreateAccount), 0x03,
		byte(opSelfDestruct), 0x03,
		byte(opExist), 0x03,
	})

	f.Fuzz(func(t *testing.T, data []byte) {
		db := NewMemoryStateDB()

		for len(data) >= 2 {
			op := data[0] % byte(numOps)
			addrByte := data[1]
			data = data[2:]

			// Build a deterministic address from the byte.
			addr := types.BytesToAddress([]byte{addrByte})

			switch op {
			case opCreateAccount:
				db.CreateAccount(addr)

			case opSetBalance:
				if len(data) < 8 {
					continue
				}
				val := new(big.Int).SetBytes(data[:8])
				data = data[8:]
				db.CreateAccount(addr) // Ensure account exists.
				db.AddBalance(addr, val)

			case opAddBalance:
				if len(data) < 8 {
					continue
				}
				val := new(big.Int).SetBytes(data[:8])
				data = data[8:]
				db.AddBalance(addr, val)

			case opSubBalance:
				if len(data) < 8 {
					continue
				}
				val := new(big.Int).SetBytes(data[:8])
				data = data[8:]
				// Only sub if balance is sufficient to avoid underflow
				// in production code. Here we just exercise the path.
				db.SubBalance(addr, val)

			case opSetNonce:
				if len(data) < 4 {
					continue
				}
				nonce := uint64(data[0])<<24 | uint64(data[1])<<16 | uint64(data[2])<<8 | uint64(data[3])
				data = data[4:]
				db.SetNonce(addr, nonce)

			case opSetCode:
				codeLen := int(addrByte) % 64
				if codeLen > len(data) {
					codeLen = len(data)
				}
				code := make([]byte, codeLen)
				copy(code, data[:codeLen])
				data = data[codeLen:]
				db.SetCode(addr, code)

			case opSetState:
				if len(data) < 64 {
					continue
				}
				var key, val types.Hash
				copy(key[:], data[:32])
				copy(val[:], data[32:64])
				data = data[64:]
				db.SetState(addr, key, val)

			case opGetBalance:
				_ = db.GetBalance(addr)

			case opGetNonce:
				_ = db.GetNonce(addr)

			case opGetCode:
				_ = db.GetCode(addr)
				_ = db.GetCodeHash(addr)
				_ = db.GetCodeSize(addr)

			case opGetState:
				if len(data) < 32 {
					continue
				}
				var key types.Hash
				copy(key[:], data[:32])
				data = data[32:]
				_ = db.GetState(addr, key)
				_ = db.GetCommittedState(addr, key)

			case opSelfDestruct:
				db.SelfDestruct(addr)
				_ = db.HasSelfDestructed(addr)

			case opExist:
				_ = db.Exist(addr)

			case opEmpty:
				_ = db.Empty(addr)

			case opAddRefund:
				if len(data) < 2 {
					continue
				}
				gas := uint64(data[0])<<8 | uint64(data[1])
				data = data[2:]
				db.AddRefund(gas)

			case opAddressToAccessList:
				db.AddAddressToAccessList(addr)
				_ = db.AddressInAccessList(addr)

			case opSlotToAccessList:
				if len(data) < 32 {
					continue
				}
				var slot types.Hash
				copy(slot[:], data[:32])
				data = data[32:]
				db.AddSlotToAccessList(addr, slot)
				_, _ = db.SlotInAccessList(addr, slot)

			case opSetTransientState:
				if len(data) < 64 {
					continue
				}
				var key, val types.Hash
				copy(key[:], data[:32])
				copy(val[:], data[32:64])
				data = data[64:]
				db.SetTransientState(addr, key, val)

			case opGetTransientState:
				if len(data) < 32 {
					continue
				}
				var key types.Hash
				copy(key[:], data[:32])
				data = data[32:]
				_ = db.GetTransientState(addr, key)
			}
		}

		// Final operations that must not panic.
		_ = db.GetRefund()
		_ = db.GetRoot()
		_, _ = db.Commit()
	})
}

// FuzzStateDBSnapshot creates a MemoryStateDB, applies random operations,
// takes a snapshot, applies more operations, then reverts to the snapshot.
// Verifies that the state matches the snapshot point.
func FuzzStateDBSnapshot(f *testing.F) {
	// Seed: sequences of (addr_byte, balance_byte, nonce_byte, storage_key_byte, storage_val_byte)
	// before and after snapshot.
	f.Add(
		// Before snapshot.
		[]byte{0x01, 0x64, 0x05, 0xaa, 0xbb},
		// After snapshot.
		[]byte{0x01, 0xc8, 0x0a, 0xcc, 0xdd},
	)
	f.Add(
		[]byte{0x02, 0x10, 0x01, 0x01, 0x02},
		[]byte{0x02, 0x20, 0x02, 0x03, 0x04},
	)

	f.Fuzz(func(t *testing.T, before, after []byte) {
		db := NewMemoryStateDB()

		// Apply "before" operations.
		applyOps(db, before)

		// Record state at snapshot point.
		snap := db.Snapshot()
		snapState := captureState(db, before)

		// Apply "after" operations.
		applyOps(db, after)

		// Revert to snapshot.
		db.RevertToSnapshot(snap)

		// Verify state matches snapshot point.
		verifyState(t, db, before, snapState)
	})
}

// opsChunkSize is the size of each operation chunk in the before/after data.
const opsChunkSize = 5

// applyOps applies a series of operations from the given data to the state.
// Each 5-byte chunk: addr_byte, balance_byte, nonce_byte, storage_key_byte, storage_val_byte.
func applyOps(db *MemoryStateDB, data []byte) {
	for len(data) >= opsChunkSize {
		chunk := data[:opsChunkSize]
		data = data[opsChunkSize:]

		addr := types.BytesToAddress([]byte{chunk[0]})
		balance := new(big.Int).SetUint64(uint64(chunk[1]) * 1000)
		nonce := uint64(chunk[2])

		var storageKey, storageVal types.Hash
		storageKey[31] = chunk[3]
		storageVal[31] = chunk[4]

		db.CreateAccount(addr)
		db.AddBalance(addr, balance)
		db.SetNonce(addr, nonce)
		if chunk[3] != 0 || chunk[4] != 0 {
			db.SetState(addr, storageKey, storageVal)
		}
	}
}

// capturedAccount holds the relevant state for comparison after revert.
type capturedAccount struct {
	balance *big.Int
	nonce   uint64
	storage map[types.Hash]types.Hash
}

// captureState records the current state of accounts mentioned in the data.
func captureState(db *MemoryStateDB, data []byte) map[types.Address]*capturedAccount {
	result := make(map[types.Address]*capturedAccount)
	for len(data) >= opsChunkSize {
		chunk := data[:opsChunkSize]
		data = data[opsChunkSize:]

		addr := types.BytesToAddress([]byte{chunk[0]})
		if _, exists := result[addr]; exists {
			// Update with latest state.
		}

		var storageKey types.Hash
		storageKey[31] = chunk[3]

		result[addr] = &capturedAccount{
			balance: db.GetBalance(addr),
			nonce:   db.GetNonce(addr),
			storage: map[types.Hash]types.Hash{
				storageKey: db.GetState(addr, storageKey),
			},
		}
	}
	return result
}

// verifyState checks that the current state matches the captured state.
func verifyState(t *testing.T, db *MemoryStateDB, data []byte, expected map[types.Address]*capturedAccount) {
	t.Helper()
	for len(data) >= opsChunkSize {
		chunk := data[:opsChunkSize]
		data = data[opsChunkSize:]

		addr := types.BytesToAddress([]byte{chunk[0]})
		exp, ok := expected[addr]
		if !ok {
			continue
		}

		gotBalance := db.GetBalance(addr)
		if gotBalance.Cmp(exp.balance) != 0 {
			t.Errorf("addr %x: balance = %s, want %s", addr, gotBalance, exp.balance)
		}

		gotNonce := db.GetNonce(addr)
		if gotNonce != exp.nonce {
			t.Errorf("addr %x: nonce = %d, want %d", addr, gotNonce, exp.nonce)
		}

		for key, expVal := range exp.storage {
			gotVal := db.GetState(addr, key)
			if gotVal != expVal {
				t.Errorf("addr %x: storage[%x] = %x, want %x", addr, key, gotVal, expVal)
			}
		}
	}
}
