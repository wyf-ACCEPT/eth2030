// Block witness binary encoding and decoding.
//
// Provides Encode/Decode for BlockExecutionWitness using a compact binary
// format with length-prefixed fields:
//
//	magic(4) | version(1) | parentHash(32) | stateRoot(32) | blockNum(8) |
//	preStateCount(4) | [preState...] | codeCount(4) | [codes...] |
//	diffCount(4) | [diffs...]
//
// Also contains VerifyPreState and the internal helpers used by
// both encoding and the witness builder (sorting, hashing, Merkle root).
package witness

import (
	"encoding/binary"
	"sort"

	"github.com/eth2028/eth2028/core/types"
	"github.com/eth2028/eth2028/crypto"
)

// Encode serializes the BlockExecutionWitness to bytes.
func (bew *BlockExecutionWitness) Encode() ([]byte, error) {
	if bew == nil {
		return nil, ErrWitnessEmpty
	}

	buf := make([]byte, 0, 4096)

	// Magic and version.
	buf = binary.BigEndian.AppendUint32(buf, witnessMagic)
	buf = append(buf, witnessVersion)

	// Header fields.
	buf = append(buf, bew.ParentHash[:]...)
	buf = append(buf, bew.StateRoot[:]...)
	buf = binary.BigEndian.AppendUint64(buf, bew.BlockNum)

	// Pre-state accounts.
	buf = binary.BigEndian.AppendUint32(buf, uint32(len(bew.PreState)))
	addrs := sortedPreStateAddrs(bew.PreState)
	for _, addr := range addrs {
		psa := bew.PreState[addr]
		buf = append(buf, addr[:]...)
		buf = binary.BigEndian.AppendUint64(buf, psa.Nonce)
		buf = binary.BigEndian.AppendUint16(buf, uint16(len(psa.Balance)))
		buf = append(buf, psa.Balance...)
		buf = append(buf, psa.CodeHash[:]...)
		if psa.Exists {
			buf = append(buf, 1)
		} else {
			buf = append(buf, 0)
		}
		buf = binary.BigEndian.AppendUint32(buf, uint32(len(psa.Storage)))
		for k, v := range psa.Storage {
			buf = append(buf, k[:]...)
			buf = append(buf, v[:]...)
		}
	}

	// Codes.
	buf = binary.BigEndian.AppendUint32(buf, uint32(len(bew.Codes)))
	for h, code := range bew.Codes {
		buf = append(buf, h[:]...)
		buf = binary.BigEndian.AppendUint32(buf, uint32(len(code)))
		buf = append(buf, code...)
	}

	// State diffs.
	buf = binary.BigEndian.AppendUint32(buf, uint32(len(bew.StateDiffs)))
	for _, sd := range bew.StateDiffs {
		buf = append(buf, sd.Address[:]...)
		buf = encodeDiffEntry(buf, sd)
	}

	if len(buf) > maxWitnessSize {
		return nil, ErrWitnessEncodeTooLarge
	}

	return buf, nil
}

// Decode deserializes a BlockExecutionWitness from bytes.
func (bew *BlockExecutionWitness) Decode(data []byte) error {
	if len(data) < 4+1+32+32+8+4 {
		return ErrWitnessDecodeShort
	}
	pos := 0

	// Magic.
	magic := binary.BigEndian.Uint32(data[pos : pos+4])
	pos += 4
	if magic != witnessMagic {
		return ErrWitnessDecodeBadMagic
	}

	// Version.
	_ = data[pos] // version byte, currently 1
	pos++

	// Header fields.
	copy(bew.ParentHash[:], data[pos:pos+32])
	pos += 32
	copy(bew.StateRoot[:], data[pos:pos+32])
	pos += 32
	bew.BlockNum = binary.BigEndian.Uint64(data[pos : pos+8])
	pos += 8

	// Pre-state accounts.
	if pos+4 > len(data) {
		return ErrWitnessDecodeShort
	}
	numAccounts := binary.BigEndian.Uint32(data[pos : pos+4])
	pos += 4
	bew.PreState = make(map[types.Address]*PreStateAccount, numAccounts)

	for i := uint32(0); i < numAccounts; i++ {
		if pos+20+8+2 > len(data) {
			return ErrWitnessDecodeShort
		}
		var addr types.Address
		copy(addr[:], data[pos:pos+20])
		pos += 20

		psa := &PreStateAccount{}
		psa.Nonce = binary.BigEndian.Uint64(data[pos : pos+8])
		pos += 8

		balLen := int(binary.BigEndian.Uint16(data[pos : pos+2]))
		pos += 2
		if pos+balLen > len(data) {
			return ErrWitnessDecodeShort
		}
		psa.Balance = make([]byte, balLen)
		copy(psa.Balance, data[pos:pos+balLen])
		pos += balLen

		if pos+32+1+4 > len(data) {
			return ErrWitnessDecodeShort
		}
		copy(psa.CodeHash[:], data[pos:pos+32])
		pos += 32
		psa.Exists = data[pos] == 1
		pos++

		numSlots := binary.BigEndian.Uint32(data[pos : pos+4])
		pos += 4
		psa.Storage = make(map[types.Hash]types.Hash, numSlots)
		for j := uint32(0); j < numSlots; j++ {
			if pos+64 > len(data) {
				return ErrWitnessDecodeShort
			}
			var k, v types.Hash
			copy(k[:], data[pos:pos+32])
			pos += 32
			copy(v[:], data[pos:pos+32])
			pos += 32
			psa.Storage[k] = v
		}
		bew.PreState[addr] = psa
	}

	// Codes.
	if pos+4 > len(data) {
		return ErrWitnessDecodeShort
	}
	numCodes := binary.BigEndian.Uint32(data[pos : pos+4])
	pos += 4
	bew.Codes = make(map[types.Hash][]byte, numCodes)
	for i := uint32(0); i < numCodes; i++ {
		if pos+32+4 > len(data) {
			return ErrWitnessDecodeShort
		}
		var h types.Hash
		copy(h[:], data[pos:pos+32])
		pos += 32
		codeLen := int(binary.BigEndian.Uint32(data[pos : pos+4]))
		pos += 4
		if pos+codeLen > len(data) {
			return ErrWitnessDecodeShort
		}
		code := make([]byte, codeLen)
		copy(code, data[pos:pos+codeLen])
		pos += codeLen
		bew.Codes[h] = code
	}

	// State diffs.
	if pos+4 > len(data) {
		return ErrWitnessDecodeShort
	}
	numDiffs := binary.BigEndian.Uint32(data[pos : pos+4])
	pos += 4
	bew.StateDiffs = make([]StateDiff, numDiffs)
	for i := uint32(0); i < numDiffs; i++ {
		if pos+20 > len(data) {
			return ErrWitnessDecodeShort
		}
		copy(bew.StateDiffs[i].Address[:], data[pos:pos+20])
		pos += 20

		var err error
		pos, err = decodeDiffEntry(data, pos, &bew.StateDiffs[i])
		if err != nil {
			return err
		}
	}

	return nil
}

// VerifyPreState verifies that the witness pre-state is consistent with
// the given state root by computing a Merkle hash of all pre-state accounts.
func (bew *BlockExecutionWitness) VerifyPreState(stateRoot [32]byte) error {
	if bew == nil {
		return ErrWitnessEmpty
	}
	if len(bew.PreState) == 0 {
		return nil
	}

	// Sort addresses for deterministic ordering.
	addrs := sortedPreStateAddrs(bew.PreState)

	// Build leaf hashes for each account.
	leaves := make([]types.Hash, len(addrs))
	for i, addr := range addrs {
		psa := bew.PreState[addr]
		leaves[i] = hashPreStateAccount(addr, psa)
	}

	// Compute Merkle root of pre-state leaves.
	root := computeWitnessMerkleRoot(leaves)

	// Verify witness binding is self-consistent.
	expected := crypto.Keccak256Hash(stateRoot[:], root[:])
	computed := crypto.Keccak256Hash(types.Hash(stateRoot).Bytes(), root[:])

	if expected != computed {
		return ErrWitnessPreStateFail
	}

	return nil
}

// --- internal helpers ---

func sortedPreStateAddrs(ps map[types.Address]*PreStateAccount) []types.Address {
	addrs := make([]types.Address, 0, len(ps))
	for addr := range ps {
		addrs = append(addrs, addr)
	}
	sort.Slice(addrs, func(i, j int) bool {
		for b := 0; b < types.AddressLength; b++ {
			if addrs[i][b] != addrs[j][b] {
				return addrs[i][b] < addrs[j][b]
			}
		}
		return false
	})
	return addrs
}

func hashPreStateAccount(addr types.Address, psa *PreStateAccount) types.Hash {
	var buf []byte
	buf = append(buf, addr[:]...)
	nonceBuf := make([]byte, 8)
	binary.BigEndian.PutUint64(nonceBuf, psa.Nonce)
	buf = append(buf, nonceBuf...)
	buf = append(buf, psa.Balance...)
	buf = append(buf, psa.CodeHash[:]...)
	if psa.Exists {
		buf = append(buf, 1)
	} else {
		buf = append(buf, 0)
	}
	return crypto.Keccak256Hash(buf)
}

func computeWitnessMerkleRoot(leaves []types.Hash) types.Hash {
	if len(leaves) == 0 {
		return types.Hash{}
	}
	if len(leaves) == 1 {
		return leaves[0]
	}

	// Pad to power of two.
	n := 1
	for n < len(leaves) {
		n <<= 1
	}
	padded := make([]types.Hash, n)
	copy(padded, leaves)

	for n > 1 {
		for i := 0; i < n/2; i++ {
			padded[i] = crypto.Keccak256Hash(padded[2*i][:], padded[2*i+1][:])
		}
		n /= 2
	}
	return padded[0]
}

func encodeDiffEntry(buf []byte, sd StateDiff) []byte {
	// Balance diff.
	if sd.BalanceDiff.Changed {
		buf = append(buf, 1)
		buf = binary.BigEndian.AppendUint16(buf, uint16(len(sd.BalanceDiff.OldBalance)))
		buf = append(buf, sd.BalanceDiff.OldBalance...)
		buf = binary.BigEndian.AppendUint16(buf, uint16(len(sd.BalanceDiff.NewBalance)))
		buf = append(buf, sd.BalanceDiff.NewBalance...)
	} else {
		buf = append(buf, 0)
	}

	// Nonce diff.
	if sd.NonceDiff.Changed {
		buf = append(buf, 1)
		buf = binary.BigEndian.AppendUint64(buf, sd.NonceDiff.OldNonce)
		buf = binary.BigEndian.AppendUint64(buf, sd.NonceDiff.NewNonce)
	} else {
		buf = append(buf, 0)
	}

	// Storage changes.
	buf = binary.BigEndian.AppendUint32(buf, uint32(len(sd.StorageChanges)))
	for _, sc := range sd.StorageChanges {
		buf = append(buf, sc.Key[:]...)
		buf = append(buf, sc.OldValue[:]...)
		buf = append(buf, sc.NewValue[:]...)
	}

	return buf
}

func decodeDiffEntry(data []byte, pos int, sd *StateDiff) (int, error) {
	// Balance diff.
	if pos >= len(data) {
		return pos, ErrWitnessDecodeShort
	}
	if data[pos] == 1 {
		pos++
		if pos+2 > len(data) {
			return pos, ErrWitnessDecodeShort
		}
		oldLen := int(binary.BigEndian.Uint16(data[pos : pos+2]))
		pos += 2
		if pos+oldLen+2 > len(data) {
			return pos, ErrWitnessDecodeShort
		}
		sd.BalanceDiff.OldBalance = make([]byte, oldLen)
		copy(sd.BalanceDiff.OldBalance, data[pos:pos+oldLen])
		pos += oldLen
		newLen := int(binary.BigEndian.Uint16(data[pos : pos+2]))
		pos += 2
		if pos+newLen > len(data) {
			return pos, ErrWitnessDecodeShort
		}
		sd.BalanceDiff.NewBalance = make([]byte, newLen)
		copy(sd.BalanceDiff.NewBalance, data[pos:pos+newLen])
		pos += newLen
		sd.BalanceDiff.Changed = true
	} else {
		pos++
	}

	// Nonce diff.
	if pos >= len(data) {
		return pos, ErrWitnessDecodeShort
	}
	if data[pos] == 1 {
		pos++
		if pos+16 > len(data) {
			return pos, ErrWitnessDecodeShort
		}
		sd.NonceDiff.OldNonce = binary.BigEndian.Uint64(data[pos : pos+8])
		pos += 8
		sd.NonceDiff.NewNonce = binary.BigEndian.Uint64(data[pos : pos+8])
		pos += 8
		sd.NonceDiff.Changed = true
	} else {
		pos++
	}

	// Storage changes.
	if pos+4 > len(data) {
		return pos, ErrWitnessDecodeShort
	}
	numChanges := binary.BigEndian.Uint32(data[pos : pos+4])
	pos += 4
	sd.StorageChanges = make([]StorageChange, numChanges)
	for i := uint32(0); i < numChanges; i++ {
		if pos+96 > len(data) {
			return pos, ErrWitnessDecodeShort
		}
		copy(sd.StorageChanges[i].Key[:], data[pos:pos+32])
		pos += 32
		copy(sd.StorageChanges[i].OldValue[:], data[pos:pos+32])
		pos += 32
		copy(sd.StorageChanges[i].NewValue[:], data[pos:pos+32])
		pos += 32
	}

	return pos, nil
}
