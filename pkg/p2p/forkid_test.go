package p2p

import (
	"encoding/binary"
	"hash/crc32"
	"testing"

	"github.com/eth2028/eth2028/core/types"
)

// Ethereum mainnet genesis hash for test vectors.
var mainnetGenesisHash = types.HexToHash("d4e56740f876aef8c010b86a40d5f56745a118d0906a34e69aec8c0db1cb8fa3")

func TestCalcForkID_GenesisOnly(t *testing.T) {
	// No forks: the ForkID is simply CRC32(genesis) with Next=0.
	fid := CalcForkID(mainnetGenesisHash, 0, nil)

	wantHash := crc32.ChecksumIEEE(mainnetGenesisHash[:])
	var wantBytes [4]byte
	binary.BigEndian.PutUint32(wantBytes[:], wantHash)

	if fid.Hash != wantBytes {
		t.Errorf("Hash = %x, want %x", fid.Hash, wantBytes)
	}
	if fid.Next != 0 {
		t.Errorf("Next = %d, want 0", fid.Next)
	}
}

func TestCalcForkID_MainnetFrontier(t *testing.T) {
	// Mainnet at block 0 with Homestead fork at 1150000.
	// CRC32(genesis hash) XOR'd with no forks yet, Next=1150000.
	forks := []uint64{1150000}
	fid := CalcForkID(mainnetGenesisHash, 0, forks)

	wantHash := crc32.ChecksumIEEE(mainnetGenesisHash[:])
	var wantBytes [4]byte
	binary.BigEndian.PutUint32(wantBytes[:], wantHash)

	if fid.Hash != wantBytes {
		t.Errorf("Hash = %x, want %x (CRC32 of genesis only)", fid.Hash, wantBytes)
	}
	if fid.Next != 1150000 {
		t.Errorf("Next = %d, want 1150000", fid.Next)
	}
}

func TestCalcForkID_AfterHomestead(t *testing.T) {
	// Mainnet past Homestead (1150000), next is DAO (1920000).
	forks := []uint64{1150000, 1920000}
	fid := CalcForkID(mainnetGenesisHash, 1150000, forks)

	// Checksum should include genesis + homestead fork.
	hash := crc32.ChecksumIEEE(mainnetGenesisHash[:])
	var blob [8]byte
	binary.BigEndian.PutUint64(blob[:], 1150000)
	hash = crc32.Update(hash, crc32.IEEETable, blob[:])

	var wantBytes [4]byte
	binary.BigEndian.PutUint32(wantBytes[:], hash)

	if fid.Hash != wantBytes {
		t.Errorf("Hash = %x, want %x", fid.Hash, wantBytes)
	}
	if fid.Next != 1920000 {
		t.Errorf("Next = %d, want 1920000", fid.Next)
	}
}

func TestCalcForkID_AllForksPassed(t *testing.T) {
	forks := []uint64{1150000, 1920000, 2463000}
	fid := CalcForkID(mainnetGenesisHash, 5000000, forks)

	// All forks passed, compute cumulative checksum.
	hash := crc32.ChecksumIEEE(mainnetGenesisHash[:])
	for _, f := range forks {
		var blob [8]byte
		binary.BigEndian.PutUint64(blob[:], f)
		hash = crc32.Update(hash, crc32.IEEETable, blob[:])
	}
	var wantBytes [4]byte
	binary.BigEndian.PutUint32(wantBytes[:], hash)

	if fid.Hash != wantBytes {
		t.Errorf("Hash = %x, want %x", fid.Hash, wantBytes)
	}
	if fid.Next != 0 {
		t.Errorf("Next = %d, want 0 (all forks passed)", fid.Next)
	}
}

func TestCalcForkID_DeduplicateAndSort(t *testing.T) {
	// Duplicate and unsorted forks should be handled gracefully.
	forks := []uint64{1920000, 1150000, 1920000, 0, 1150000}
	fid := CalcForkID(mainnetGenesisHash, 5000000, forks)

	// Equivalent to sorted, deduplicated: [1150000, 1920000].
	hash := crc32.ChecksumIEEE(mainnetGenesisHash[:])
	for _, f := range []uint64{1150000, 1920000} {
		var blob [8]byte
		binary.BigEndian.PutUint64(blob[:], f)
		hash = crc32.Update(hash, crc32.IEEETable, blob[:])
	}
	var wantBytes [4]byte
	binary.BigEndian.PutUint32(wantBytes[:], hash)

	if fid.Hash != wantBytes {
		t.Errorf("Hash = %x, want %x (after dedup+sort)", fid.Hash, wantBytes)
	}
	if fid.Next != 0 {
		t.Errorf("Next = %d, want 0", fid.Next)
	}
}

func TestCalcForkID_EmptyForks(t *testing.T) {
	fid := CalcForkID(mainnetGenesisHash, 999999, []uint64{})
	wantHash := crc32.ChecksumIEEE(mainnetGenesisHash[:])
	var wantBytes [4]byte
	binary.BigEndian.PutUint32(wantBytes[:], wantHash)

	if fid.Hash != wantBytes {
		t.Errorf("Hash = %x, want %x", fid.Hash, wantBytes)
	}
	if fid.Next != 0 {
		t.Errorf("Next = %d, want 0", fid.Next)
	}
}

func TestCalcForkID_HeadExactlyAtFork(t *testing.T) {
	// Head is exactly at the first fork block.
	forks := []uint64{1150000, 1920000}
	fid := CalcForkID(mainnetGenesisHash, 1150000, forks)

	// Fork 1150000 should be included in checksum (fork <= head).
	hash := crc32.ChecksumIEEE(mainnetGenesisHash[:])
	var blob [8]byte
	binary.BigEndian.PutUint64(blob[:], 1150000)
	hash = crc32.Update(hash, crc32.IEEETable, blob[:])
	var wantBytes [4]byte
	binary.BigEndian.PutUint32(wantBytes[:], hash)

	if fid.Hash != wantBytes {
		t.Errorf("Hash = %x, want %x", fid.Hash, wantBytes)
	}
	if fid.Next != 1920000 {
		t.Errorf("Next = %d, want 1920000", fid.Next)
	}
}

func TestCalcForkID_HeadOneBeforeFork(t *testing.T) {
	// Head is one block before the fork.
	forks := []uint64{1150000}
	fid := CalcForkID(mainnetGenesisHash, 1149999, forks)

	// Fork 1150000 is NOT included in checksum (fork > head).
	wantHash := crc32.ChecksumIEEE(mainnetGenesisHash[:])
	var wantBytes [4]byte
	binary.BigEndian.PutUint32(wantBytes[:], wantHash)

	if fid.Hash != wantBytes {
		t.Errorf("Hash = %x, want %x", fid.Hash, wantBytes)
	}
	if fid.Next != 1150000 {
		t.Errorf("Next = %d, want 1150000", fid.Next)
	}
}

func TestCalcForkID_MatchesKnownMainnetVector(t *testing.T) {
	// Known test vector: mainnet at genesis (block 0) with no forks.
	// CRC32("d4e56740...") = 0xfc64ec04 per EIP-2124.
	fid := CalcForkID(mainnetGenesisHash, 0, nil)

	want := [4]byte{0xfc, 0x64, 0xec, 0x04}
	if fid.Hash != want {
		t.Errorf("Hash = %x, want %x (EIP-2124 mainnet genesis)", fid.Hash, want)
	}
	if fid.Next != 0 {
		t.Errorf("Next = %d, want 0", fid.Next)
	}
}

func TestCalcForkID_MainnetHomesteadVector(t *testing.T) {
	// Known: mainnet at block 1150000 (past Homestead).
	// Next fork is DAO at 1920000 in the full chain config, but we only test
	// the Homestead fork here.
	forks := []uint64{1150000, 1920000}
	fid := CalcForkID(mainnetGenesisHash, 1150000, forks)

	// After folding Homestead: 0x97c2c34c per EIP-2124.
	want := [4]byte{0x97, 0xc2, 0xc3, 0x4c}
	if fid.Hash != want {
		t.Errorf("Hash = %x, want %x (EIP-2124 mainnet post-Homestead)", fid.Hash, want)
	}
	if fid.Next != 1920000 {
		t.Errorf("Next = %d, want 1920000", fid.Next)
	}
}

func TestCleanForks(t *testing.T) {
	tests := []struct {
		name  string
		input []uint64
		want  []uint64
	}{
		{"nil", nil, nil},
		{"empty", []uint64{}, nil},
		{"single", []uint64{100}, []uint64{100}},
		{"sorted", []uint64{100, 200, 300}, []uint64{100, 200, 300}},
		{"unsorted", []uint64{300, 100, 200}, []uint64{100, 200, 300}},
		{"duplicates", []uint64{100, 200, 100, 200}, []uint64{100, 200}},
		{"zeros", []uint64{0, 100, 0}, []uint64{100}},
		{"all zeros", []uint64{0, 0, 0}, nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := cleanForks(tt.input)
			if len(got) != len(tt.want) {
				t.Fatalf("cleanForks(%v) length = %d, want %d", tt.input, len(got), len(tt.want))
			}
			for i := range got {
				if got[i] != tt.want[i] {
					t.Errorf("cleanForks(%v)[%d] = %d, want %d", tt.input, i, got[i], tt.want[i])
				}
			}
		})
	}
}

func TestForkIDRoundtrip(t *testing.T) {
	// Encode and decode a ForkID through the Message system.
	original := CalcForkID(mainnetGenesisHash, 0, []uint64{1150000})

	msg, err := EncodeMessage(StatusMsg, original)
	if err != nil {
		t.Fatalf("EncodeMessage: %v", err)
	}

	var decoded ForkID
	if err := DecodeMessage(msg, &decoded); err != nil {
		t.Fatalf("DecodeMessage: %v", err)
	}

	if decoded.Hash != original.Hash {
		t.Errorf("decoded Hash = %x, want %x", decoded.Hash, original.Hash)
	}
	if decoded.Next != original.Next {
		t.Errorf("decoded Next = %d, want %d", decoded.Next, original.Next)
	}
}
