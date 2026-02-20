package verkle

import (
	"testing"

	"github.com/eth2028/eth2028/core/types"
)

func TestGetTreeKeyForVersion(t *testing.T) {
	addr := types.BytesToAddress([]byte{0x01, 0x02, 0x03})
	key := GetTreeKeyForVersion(addr)

	if len(key) != KeySize {
		t.Fatalf("key size = %d, want %d", len(key), KeySize)
	}
	if key[StemSize] != VersionLeafKey {
		t.Errorf("suffix = %d, want %d (VersionLeafKey)", key[StemSize], VersionLeafKey)
	}
}

func TestGetTreeKeyForBalance(t *testing.T) {
	addr := types.BytesToAddress([]byte{0x01, 0x02, 0x03})
	key := GetTreeKeyForBalance(addr)

	if key[StemSize] != BalanceLeafKey {
		t.Errorf("suffix = %d, want %d (BalanceLeafKey)", key[StemSize], BalanceLeafKey)
	}
}

func TestGetTreeKeyForNonce(t *testing.T) {
	addr := types.BytesToAddress([]byte{0x01, 0x02, 0x03})
	key := GetTreeKeyForNonce(addr)

	if key[StemSize] != NonceLeafKey {
		t.Errorf("suffix = %d, want %d (NonceLeafKey)", key[StemSize], NonceLeafKey)
	}
}

func TestGetTreeKeyForCodeHash(t *testing.T) {
	addr := types.BytesToAddress([]byte{0x01, 0x02, 0x03})
	key := GetTreeKeyForCodeHash(addr)

	if key[StemSize] != CodeHashLeafKey {
		t.Errorf("suffix = %d, want %d (CodeHashLeafKey)", key[StemSize], CodeHashLeafKey)
	}
}

func TestGetTreeKeyForCodeSize(t *testing.T) {
	addr := types.BytesToAddress([]byte{0x01, 0x02, 0x03})
	key := GetTreeKeyForCodeSize(addr)

	if key[StemSize] != CodeSizeLeafKey {
		t.Errorf("suffix = %d, want %d (CodeSizeLeafKey)", key[StemSize], CodeSizeLeafKey)
	}
}

func TestAccountHeaderKeysSameStem(t *testing.T) {
	addr := types.BytesToAddress([]byte{0xaa, 0xbb, 0xcc})
	keys := AccountHeaderKeys(addr)

	if len(keys) != 5 {
		t.Fatalf("AccountHeaderKeys count = %d, want 5", len(keys))
	}

	// All five keys should share the same 31-byte stem.
	stem := StemFromKey(keys[0])
	for i := 1; i < 5; i++ {
		s := StemFromKey(keys[i])
		if s != stem {
			t.Errorf("key[%d] has different stem", i)
		}
	}

	// Suffixes should be 0, 1, 2, 3, 4.
	expectedSuffixes := []byte{VersionLeafKey, BalanceLeafKey, NonceLeafKey, CodeHashLeafKey, CodeSizeLeafKey}
	for i, expected := range expectedSuffixes {
		got := SuffixFromKey(keys[i])
		if got != expected {
			t.Errorf("key[%d] suffix = %d, want %d", i, got, expected)
		}
	}
}

func TestGetTreeKeyForCodeChunkSmall(t *testing.T) {
	addr := types.BytesToAddress([]byte{0x01})

	// Chunk 0 should be in the header stem at CodeOffset+0.
	key := GetTreeKeyForCodeChunk(addr, 0)
	if key[StemSize] != CodeOffset {
		t.Errorf("chunk 0 suffix = %d, want %d", key[StemSize], CodeOffset)
	}

	// Chunk 127 should still be in the header stem.
	key = GetTreeKeyForCodeChunk(addr, 127)
	if key[StemSize] != CodeOffset+127 {
		t.Errorf("chunk 127 suffix = %d, want %d", key[StemSize], CodeOffset+127)
	}

	// All small chunks share the same stem.
	stem0 := StemFromKey(GetTreeKeyForCodeChunk(addr, 0))
	stem50 := StemFromKey(GetTreeKeyForCodeChunk(addr, 50))
	if stem0 != stem50 {
		t.Error("small code chunks should share the same stem")
	}
}

func TestGetTreeKeyForCodeChunkLarge(t *testing.T) {
	addr := types.BytesToAddress([]byte{0x01})

	// Chunk 128 exceeds MaxCodeChunksPerStem, uses storage stem.
	key128 := GetTreeKeyForCodeChunk(addr, 128)
	key0 := GetTreeKeyForCodeChunk(addr, 0)

	// Should have a different stem than small chunks.
	stem0 := StemFromKey(key0)
	stem128 := StemFromKey(key128)
	if stem0 == stem128 {
		t.Error("large code chunk should have different stem than small chunks")
	}
	if len(key128) != KeySize {
		t.Errorf("large code chunk key size = %d, want %d", len(key128), KeySize)
	}
}

func TestGetTreeKeyForStorageSlotSmall(t *testing.T) {
	addr := types.BytesToAddress([]byte{0x01})

	// Slot 0 goes to header stem at HeaderStorageOffset+0.
	key := GetTreeKeyForStorageSlot(addr, 0)
	if key[StemSize] != HeaderStorageOffset {
		t.Errorf("slot 0 suffix = %d, want %d", key[StemSize], HeaderStorageOffset)
	}

	// Slot 63 is the last small slot.
	key = GetTreeKeyForStorageSlot(addr, 63)
	if key[StemSize] != HeaderStorageOffset+63 {
		t.Errorf("slot 63 suffix = %d, want %d", key[StemSize], HeaderStorageOffset+63)
	}
}

func TestGetTreeKeyForStorageSlotLarge(t *testing.T) {
	addr := types.BytesToAddress([]byte{0x01})

	// Slot 64 uses a separate stem.
	keySmall := GetTreeKeyForStorageSlot(addr, 0)
	keyLarge := GetTreeKeyForStorageSlot(addr, 64)

	stemSmall := StemFromKey(keySmall)
	stemLarge := StemFromKey(keyLarge)
	if stemSmall == stemLarge {
		t.Error("large storage slot should have different stem than small slot")
	}

	// The suffix should be slot % 256.
	if keyLarge[StemSize] != byte(64%256) {
		t.Errorf("large slot suffix = %d, want %d", keyLarge[StemSize], 64%256)
	}
}

func TestStemFromKey(t *testing.T) {
	var key [KeySize]byte
	for i := 0; i < StemSize; i++ {
		key[i] = byte(i)
	}
	key[StemSize] = 0xff

	stem := StemFromKey(key)
	for i := 0; i < StemSize; i++ {
		if stem[i] != byte(i) {
			t.Errorf("stem[%d] = %d, want %d", i, stem[i], i)
		}
	}
}

func TestSuffixFromKey(t *testing.T) {
	var key [KeySize]byte
	key[StemSize] = 42

	suffix := SuffixFromKey(key)
	if suffix != 42 {
		t.Errorf("SuffixFromKey = %d, want 42", suffix)
	}
}

func TestVerkleKeyFromAddressDerivation(t *testing.T) {
	addr := types.BytesToAddress([]byte{0x01, 0x02, 0x03})
	key := VerkleKeyFromAddress(addr, 5)

	if len(key) != KeySize {
		t.Fatalf("key length = %d, want %d", len(key), KeySize)
	}
	if key[StemSize] != 5 {
		t.Errorf("suffix = %d, want 5", key[StemSize])
	}

	// VerkleKeyFromAddress should produce same stem as getTreeKey.
	treeKey := getTreeKey(addr, 5)
	for i := 0; i < StemSize; i++ {
		if key[i] != treeKey[i] {
			t.Errorf("stem byte %d mismatch: VerkleKeyFromAddress=%d, getTreeKey=%d", i, key[i], treeKey[i])
		}
	}
}

func TestDifferentAddressesProduceDifferentStems(t *testing.T) {
	addr1 := types.BytesToAddress([]byte{0x01})
	addr2 := types.BytesToAddress([]byte{0x02})

	key1 := GetTreeKeyForBalance(addr1)
	key2 := GetTreeKeyForBalance(addr2)

	stem1 := StemFromKey(key1)
	stem2 := StemFromKey(key2)

	if stem1 == stem2 {
		t.Error("different addresses should produce different stems")
	}
}

func TestLeafKeyConstants(t *testing.T) {
	if VersionLeafKey != 0 {
		t.Errorf("VersionLeafKey = %d, want 0", VersionLeafKey)
	}
	if BalanceLeafKey != 1 {
		t.Errorf("BalanceLeafKey = %d, want 1", BalanceLeafKey)
	}
	if NonceLeafKey != 2 {
		t.Errorf("NonceLeafKey = %d, want 2", NonceLeafKey)
	}
	if CodeHashLeafKey != 3 {
		t.Errorf("CodeHashLeafKey = %d, want 3", CodeHashLeafKey)
	}
	if CodeSizeLeafKey != 4 {
		t.Errorf("CodeSizeLeafKey = %d, want 4", CodeSizeLeafKey)
	}
	if CodeOffset != 128 {
		t.Errorf("CodeOffset = %d, want 128", CodeOffset)
	}
	if HeaderStorageOffset != 64 {
		t.Errorf("HeaderStorageOffset = %d, want 64", HeaderStorageOffset)
	}
	if MaxCodeChunksPerStem != 128 {
		t.Errorf("MaxCodeChunksPerStem = %d, want 128", MaxCodeChunksPerStem)
	}
}
