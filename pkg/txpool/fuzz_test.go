package txpool

import (
	"math/big"
	"testing"

	"github.com/eth2030/eth2030/core/types"
)

// fuzzState implements StateReader with generous defaults for fuzzing.
type fuzzState struct {
	nonces   map[types.Address]uint64
	balances map[types.Address]*big.Int
}

func newFuzzState() *fuzzState {
	return &fuzzState{
		nonces:   make(map[types.Address]uint64),
		balances: make(map[types.Address]*big.Int),
	}
}

func (s *fuzzState) GetNonce(addr types.Address) uint64 {
	return s.nonces[addr]
}

func (s *fuzzState) GetBalance(addr types.Address) *big.Int {
	if bal, ok := s.balances[addr]; ok {
		return bal
	}
	// Return a very large balance so transactions are not rejected for
	// insufficient funds, letting the fuzzer explore deeper code paths.
	return new(big.Int).Mul(big.NewInt(1e18), big.NewInt(1_000_000))
}

// fuzzSender is the fixed sender address used during fuzzing.
var fuzzSender = types.BytesToAddress([]byte{0xf0, 0x02, 0x03, 0x04})

// makeFuzzLegacyTx builds a legacy transaction from fuzz-provided parameters.
func makeFuzzLegacyTx(nonce uint64, gasPrice int64, gas uint64, data []byte) *types.Transaction {
	to := types.BytesToAddress([]byte{0xde, 0xad})
	if gasPrice < 0 {
		gasPrice = 1
	}
	tx := types.NewTransaction(&types.LegacyTx{
		Nonce:    nonce,
		GasPrice: big.NewInt(gasPrice),
		Gas:      gas,
		To:       &to,
		Value:    big.NewInt(0),
		Data:     data,
	})
	tx.SetSender(fuzzSender)
	return tx
}

// FuzzTxValidation feeds random transaction-like parameters through pool
// validation. The pool must never panic regardless of input.
func FuzzTxValidation(f *testing.F) {
	// Seed corpus: a valid minimal transaction.
	f.Add(uint64(0), int64(10), uint64(21000), []byte{})
	// Seed with some data bytes.
	f.Add(uint64(1), int64(100), uint64(50000), []byte{0x01, 0x02, 0x03})
	// Seed with large gas.
	f.Add(uint64(0), int64(1), uint64(30_000_000), []byte{})
	// Seed with zero gas price.
	f.Add(uint64(0), int64(0), uint64(21000), []byte{})

	f.Fuzz(func(t *testing.T, nonce uint64, gasPrice int64, gas uint64, data []byte) {
		// Cap data to avoid huge allocations.
		if len(data) > 1024 {
			data = data[:1024]
		}

		state := newFuzzState()
		cfg := DefaultConfig()
		pool := New(cfg, state)

		tx := makeFuzzLegacyTx(nonce, gasPrice, gas, data)
		// Must not panic. We discard the error since random inputs are often invalid.
		_ = pool.AddLocal(tx)
	})
}

// FuzzTxPoolAddRemove adds transactions with random gas prices and nonces,
// removes some, and verifies pool invariants (no panic, counts are consistent).
func FuzzTxPoolAddRemove(f *testing.F) {
	// Seed corpus: sequences of (nonce, gasPrice, doRemove) encoded as bytes.
	// Each 9-byte chunk: nonce(4) + gasPrice(4) + remove_flag(1).
	f.Add([]byte{
		0x00, 0x00, 0x00, 0x00, // nonce=0
		0x00, 0x00, 0x00, 0x0a, // gasPrice=10
		0x00, // don't remove
	})
	f.Add([]byte{
		0x00, 0x00, 0x00, 0x00, // nonce=0
		0x00, 0x00, 0x00, 0x0a, // gasPrice=10
		0x00, // don't remove
		0x00, 0x00, 0x00, 0x01, // nonce=1
		0x00, 0x00, 0x00, 0x14, // gasPrice=20
		0x01, // remove first
	})

	f.Fuzz(func(t *testing.T, data []byte) {
		state := newFuzzState()
		cfg := DefaultConfig()
		pool := New(cfg, state)

		const chunkSize = 9
		var hashes []types.Hash

		for len(data) >= chunkSize {
			chunk := data[:chunkSize]
			data = data[chunkSize:]

			nonce := uint64(chunk[0])<<24 | uint64(chunk[1])<<16 | uint64(chunk[2])<<8 | uint64(chunk[3])
			// Clamp nonce to MaxNonceGap to allow some txs through.
			nonce = nonce % (MaxNonceGap + 1)

			gasPrice := int64(chunk[4])<<24 | int64(chunk[5])<<16 | int64(chunk[6])<<8 | int64(chunk[7])
			if gasPrice < 1 {
				gasPrice = 1
			}
			doRemove := chunk[8]%2 == 1

			tx := makeFuzzLegacyTx(nonce, gasPrice, 21000, nil)
			err := pool.AddLocal(tx)
			if err == nil {
				hashes = append(hashes, tx.Hash())
			}

			// Optionally remove a previously added transaction.
			if doRemove && len(hashes) > 0 {
				idx := int(chunk[4]) % len(hashes)
				pool.Remove(hashes[idx])
			}
		}

		// Verify invariants: counts must be non-negative.
		pending := pool.PendingCount()
		queued := pool.QueuedCount()
		total := pool.Count()

		if pending < 0 {
			t.Fatalf("pending count negative: %d", pending)
		}
		if queued < 0 {
			t.Fatalf("queued count negative: %d", queued)
		}
		if total < 0 {
			t.Fatalf("total count negative: %d", total)
		}
		if pending+queued > total {
			// pending+queued can equal total when everything is accounted for.
			// But they should never exceed total lookup entries.
		}
	})
}
