package consensus

import (
	"bytes"
	"errors"
	"sync"
	"testing"

	"github.com/eth2028/eth2028/core/types"
)

func TestDefaultKPSConfig(t *testing.T) {
	cfg := DefaultKPSConfig()
	if cfg.DefaultThreshold != 2 {
		t.Errorf("DefaultThreshold = %d, want 2", cfg.DefaultThreshold)
	}
	if cfg.MaxGroupSize != 10 {
		t.Errorf("MaxGroupSize = %d, want 10", cfg.MaxGroupSize)
	}
	if cfg.KeyRotationInterval != 256 {
		t.Errorf("KeyRotationInterval = %d, want 256", cfg.KeyRotationInterval)
	}
}

func TestNewKPSManager(t *testing.T) {
	cfg := DefaultKPSConfig()
	mgr := NewKPSManager(cfg)
	if mgr == nil {
		t.Fatal("NewKPSManager returned nil")
	}
	if mgr.Config().DefaultThreshold != 2 {
		t.Errorf("config threshold = %d, want 2", mgr.Config().DefaultThreshold)
	}
}

func TestNewKPSManager_DefaultsZeroConfig(t *testing.T) {
	mgr := NewKPSManager(KPSConfig{})
	cfg := mgr.Config()
	if cfg.DefaultThreshold != 2 {
		t.Errorf("DefaultThreshold = %d, want 2", cfg.DefaultThreshold)
	}
	if cfg.MaxGroupSize != 10 {
		t.Errorf("MaxGroupSize = %d, want 10", cfg.MaxGroupSize)
	}
	if cfg.KeyRotationInterval != 256 {
		t.Errorf("KeyRotationInterval = %d, want 256", cfg.KeyRotationInterval)
	}
}

func TestKPSManager_GenerateKeyPair(t *testing.T) {
	mgr := NewKPSManager(KPSConfig{
		DefaultThreshold: 3,
		MaxGroupSize:     5,
	})

	kp, err := mgr.GenerateKeyPair()
	if err != nil {
		t.Fatalf("GenerateKeyPair: %v", err)
	}
	if kp == nil {
		t.Fatal("GenerateKeyPair returned nil")
	}
	if len(kp.PublicKey) != 32 {
		t.Errorf("PublicKey length = %d, want 32", len(kp.PublicKey))
	}
	if kp.Threshold != 3 {
		t.Errorf("Threshold = %d, want 3", kp.Threshold)
	}
	if kp.TotalShares != 5 {
		t.Errorf("TotalShares = %d, want 5", kp.TotalShares)
	}
	if len(kp.PrivateShares) != 5 {
		t.Errorf("PrivateShares count = %d, want 5", len(kp.PrivateShares))
	}
}

func TestSplitKey_Basic(t *testing.T) {
	key := []byte{0x42, 0xAA, 0xFF, 0x01}
	shares, err := SplitKey(key, 2, 3)
	if err != nil {
		t.Fatalf("SplitKey: %v", err)
	}
	if len(shares) != 3 {
		t.Fatalf("got %d shares, want 3", len(shares))
	}

	// Each share should be 4 bytes.
	for i, s := range shares {
		if len(s.Data) != 4 {
			t.Errorf("share[%d] data length = %d, want 4", i, len(s.Data))
		}
		if s.Index != i+1 {
			t.Errorf("share[%d] index = %d, want %d", i, s.Index, i+1)
		}
		if s.GroupID.IsZero() {
			t.Errorf("share[%d] has zero GroupID", i)
		}
	}
}

func TestSplitKey_InvalidInputs(t *testing.T) {
	tests := []struct {
		name      string
		key       []byte
		threshold int
		total     int
		wantErr   error
	}{
		{"empty key", nil, 2, 3, ErrKPSInvalidPrivateKey},
		{"zero total", []byte{1}, 1, 0, ErrKPSInvalidShares},
		{"negative total", []byte{1}, 1, -1, ErrKPSInvalidShares},
		{"zero threshold", []byte{1}, 0, 3, ErrKPSInvalidThreshold},
		{"threshold > total", []byte{1}, 4, 3, ErrKPSInvalidThreshold},
		{"negative threshold", []byte{1}, -1, 3, ErrKPSInvalidThreshold},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := SplitKey(tt.key, tt.threshold, tt.total)
			if !errors.Is(err, tt.wantErr) {
				t.Errorf("SplitKey error = %v, want %v", err, tt.wantErr)
			}
		})
	}
}

func TestRecombineKey_ExactThreshold(t *testing.T) {
	key := make([]byte, 32)
	for i := range key {
		key[i] = byte(i * 7)
	}

	shares, err := SplitKey(key, 3, 5)
	if err != nil {
		t.Fatalf("SplitKey: %v", err)
	}

	// Use exactly threshold (3) shares.
	recovered, err := RecombineKey(shares[:3])
	if err != nil {
		t.Fatalf("RecombineKey: %v", err)
	}
	if !bytes.Equal(recovered, key) {
		t.Errorf("recovered key does not match original\n  got:  %x\n  want: %x", recovered, key)
	}
}

func TestRecombineKey_AllShares(t *testing.T) {
	key := []byte{0xDE, 0xAD, 0xBE, 0xEF}

	shares, err := SplitKey(key, 2, 5)
	if err != nil {
		t.Fatalf("SplitKey: %v", err)
	}

	// Using all 5 shares should also work.
	recovered, err := RecombineKey(shares)
	if err != nil {
		t.Fatalf("RecombineKey: %v", err)
	}
	if !bytes.Equal(recovered, key) {
		t.Errorf("recovered key does not match original\n  got:  %x\n  want: %x", recovered, key)
	}
}

func TestRecombineKey_DifferentSubsets(t *testing.T) {
	key := []byte{0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07, 0x08}

	shares, err := SplitKey(key, 3, 5)
	if err != nil {
		t.Fatalf("SplitKey: %v", err)
	}

	// Try different subsets of threshold shares.
	subsets := [][]*KeyShare{
		{shares[0], shares[1], shares[2]},
		{shares[0], shares[2], shares[4]},
		{shares[1], shares[3], shares[4]},
		{shares[2], shares[3], shares[4]},
	}

	for i, subset := range subsets {
		recovered, err := RecombineKey(subset)
		if err != nil {
			t.Fatalf("subset %d: RecombineKey: %v", i, err)
		}
		if !bytes.Equal(recovered, key) {
			t.Errorf("subset %d: recovered key mismatch\n  got:  %x\n  want: %x",
				i, recovered, key)
		}
	}
}

func TestRecombineKey_SingleShare_Threshold1(t *testing.T) {
	key := []byte{0xAB, 0xCD}

	shares, err := SplitKey(key, 1, 3)
	if err != nil {
		t.Fatalf("SplitKey: %v", err)
	}

	// With threshold=1, any single share should suffice.
	for i, s := range shares {
		recovered, err := RecombineKey([]*KeyShare{s})
		if err != nil {
			t.Fatalf("share %d: RecombineKey: %v", i, err)
		}
		if !bytes.Equal(recovered, key) {
			t.Errorf("share %d: recovered key mismatch\n  got:  %x\n  want: %x",
				i, recovered, key)
		}
	}
}

func TestRecombineKey_EmptyShares(t *testing.T) {
	_, err := RecombineKey(nil)
	if !errors.Is(err, ErrKPSInsufficientShares) {
		t.Errorf("expected ErrKPSInsufficientShares, got: %v", err)
	}
}

func TestRecombineKey_DuplicateShares(t *testing.T) {
	key := []byte{0x42}
	shares, err := SplitKey(key, 2, 3)
	if err != nil {
		t.Fatalf("SplitKey: %v", err)
	}

	// Provide duplicate shares.
	_, err = RecombineKey([]*KeyShare{shares[0], shares[0]})
	if !errors.Is(err, ErrKPSDuplicateShare) {
		t.Errorf("expected ErrKPSDuplicateShare, got: %v", err)
	}
}

func TestRecombineKey_MismatchedGroupID(t *testing.T) {
	key := []byte{0x42}
	shares, err := SplitKey(key, 2, 3)
	if err != nil {
		t.Fatalf("SplitKey: %v", err)
	}

	// Modify one share's group ID.
	badShare := &KeyShare{
		Index:   shares[1].Index,
		Data:    shares[1].Data,
		GroupID: types.HexToHash("0xDEAD"),
	}

	_, err = RecombineKey([]*KeyShare{shares[0], badShare})
	if !errors.Is(err, ErrKPSInvalidShareData) {
		t.Errorf("expected ErrKPSInvalidShareData, got: %v", err)
	}
}

func TestRecombineKey_MismatchedDataLength(t *testing.T) {
	key := []byte{0x42, 0x43}
	shares, err := SplitKey(key, 2, 3)
	if err != nil {
		t.Fatalf("SplitKey: %v", err)
	}

	// Modify one share's data length.
	badShare := &KeyShare{
		Index:   shares[1].Index,
		Data:    []byte{0x01},
		GroupID: shares[1].GroupID,
	}

	_, err = RecombineKey([]*KeyShare{shares[0], badShare})
	if !errors.Is(err, ErrKPSInvalidShareData) {
		t.Errorf("expected ErrKPSInvalidShareData, got: %v", err)
	}
}

func TestVerifyKeyShare_Valid(t *testing.T) {
	mgr := NewKPSManager(DefaultKPSConfig())
	kp, err := mgr.GenerateKeyPair()
	if err != nil {
		t.Fatalf("GenerateKeyPair: %v", err)
	}

	for i, share := range kp.PrivateShares {
		if !VerifyKeyShare(share, kp.PublicKey) {
			t.Errorf("share %d should be valid", i)
		}
	}
}

func TestVerifyKeyShare_Invalid(t *testing.T) {
	tests := []struct {
		name      string
		share     *KeyShare
		publicKey []byte
	}{
		{"nil share", nil, []byte{1}},
		{"empty data", &KeyShare{Index: 1, Data: nil, GroupID: types.HexToHash("0x01")}, []byte{1}},
		{"zero index", &KeyShare{Index: 0, Data: make([]byte, 32), GroupID: types.HexToHash("0x01")}, []byte{1}},
		{"zero group", &KeyShare{Index: 1, Data: make([]byte, 32)}, []byte{1}},
		{"nil pubkey", &KeyShare{Index: 1, Data: make([]byte, 32), GroupID: types.HexToHash("0x01")}, nil},
		{"wrong data length", &KeyShare{Index: 1, Data: make([]byte, 16), GroupID: types.HexToHash("0x01")}, []byte{1}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if VerifyKeyShare(tt.share, tt.publicKey) {
				t.Error("expected VerifyKeyShare to return false")
			}
		})
	}
}

func TestKeyGroup_AddRemoveMembers(t *testing.T) {
	groupID := types.HexToHash("0x01")
	kg := NewKeyGroup(groupID, 2, 3)

	addr1 := types.HexToAddress("0x1111111111111111111111111111111111111111")
	addr2 := types.HexToAddress("0x2222222222222222222222222222222222222222")
	addr3 := types.HexToAddress("0x3333333333333333333333333333333333333333")

	// Add members.
	if err := kg.AddMember(addr1); err != nil {
		t.Fatalf("AddMember addr1: %v", err)
	}
	if err := kg.AddMember(addr2); err != nil {
		t.Fatalf("AddMember addr2: %v", err)
	}
	if err := kg.AddMember(addr3); err != nil {
		t.Fatalf("AddMember addr3: %v", err)
	}

	members := kg.GetMembers()
	if len(members) != 3 {
		t.Fatalf("member count = %d, want 3", len(members))
	}

	// Remove one.
	if err := kg.RemoveMember(addr2); err != nil {
		t.Fatalf("RemoveMember: %v", err)
	}

	members = kg.GetMembers()
	if len(members) != 2 {
		t.Fatalf("member count after remove = %d, want 2", len(members))
	}
}

func TestKeyGroup_AddMember_Duplicate(t *testing.T) {
	kg := NewKeyGroup(types.HexToHash("0x01"), 2, 5)
	addr := types.HexToAddress("0x1111111111111111111111111111111111111111")

	if err := kg.AddMember(addr); err != nil {
		t.Fatalf("first add: %v", err)
	}
	if err := kg.AddMember(addr); !errors.Is(err, ErrKPSMemberExists) {
		t.Fatalf("expected ErrKPSMemberExists, got: %v", err)
	}
}

func TestKeyGroup_AddMember_Full(t *testing.T) {
	kg := NewKeyGroup(types.HexToHash("0x01"), 1, 2)
	addr1 := types.HexToAddress("0x1111111111111111111111111111111111111111")
	addr2 := types.HexToAddress("0x2222222222222222222222222222222222222222")
	addr3 := types.HexToAddress("0x3333333333333333333333333333333333333333")

	_ = kg.AddMember(addr1)
	_ = kg.AddMember(addr2)

	if err := kg.AddMember(addr3); !errors.Is(err, ErrKPSGroupFull) {
		t.Fatalf("expected ErrKPSGroupFull, got: %v", err)
	}
}

func TestKeyGroup_RemoveMember_NotFound(t *testing.T) {
	kg := NewKeyGroup(types.HexToHash("0x01"), 1, 5)
	addr := types.HexToAddress("0x1111111111111111111111111111111111111111")

	if err := kg.RemoveMember(addr); !errors.Is(err, ErrKPSMemberNotFound) {
		t.Fatalf("expected ErrKPSMemberNotFound, got: %v", err)
	}
}

func TestKeyGroup_Properties(t *testing.T) {
	groupID := types.HexToHash("0xABCD")
	kg := NewKeyGroup(groupID, 3, 5)

	if kg.GroupID() != groupID {
		t.Errorf("GroupID mismatch")
	}
	if kg.Threshold() != 3 {
		t.Errorf("Threshold = %d, want 3", kg.Threshold())
	}
	if kg.TotalMembers() != 5 {
		t.Errorf("TotalMembers = %d, want 5", kg.TotalMembers())
	}
	if kg.MemberCount() != 0 {
		t.Errorf("MemberCount = %d, want 0", kg.MemberCount())
	}
}

func TestKPSManager_RegisterAndGetGroup(t *testing.T) {
	mgr := NewKPSManager(DefaultKPSConfig())
	groupID := types.HexToHash("0x01")
	group := NewKeyGroup(groupID, 2, 3)

	mgr.RegisterGroup(group)

	got, err := mgr.GetGroup(groupID)
	if err != nil {
		t.Fatalf("GetGroup: %v", err)
	}
	if got.GroupID() != groupID {
		t.Error("group ID mismatch")
	}
}

func TestKPSManager_GetGroup_NotFound(t *testing.T) {
	mgr := NewKPSManager(DefaultKPSConfig())
	_, err := mgr.GetGroup(types.HexToHash("0xDEAD"))
	if !errors.Is(err, ErrKPSGroupNotFound) {
		t.Fatalf("expected ErrKPSGroupNotFound, got: %v", err)
	}
}

func TestKPSManager_RotateKeys(t *testing.T) {
	mgr := NewKPSManager(DefaultKPSConfig())
	groupID := types.HexToHash("0x01")
	group := NewKeyGroup(groupID, 2, 4)
	mgr.RegisterGroup(group)

	kp1, err := mgr.RotateKeys(groupID)
	if err != nil {
		t.Fatalf("RotateKeys: %v", err)
	}
	if kp1 == nil {
		t.Fatal("RotateKeys returned nil")
	}
	if kp1.Threshold != 2 {
		t.Errorf("Threshold = %d, want 2", kp1.Threshold)
	}
	if kp1.TotalShares != 4 {
		t.Errorf("TotalShares = %d, want 4", kp1.TotalShares)
	}

	// Rotate again and verify different key.
	kp2, err := mgr.RotateKeys(groupID)
	if err != nil {
		t.Fatalf("RotateKeys(2): %v", err)
	}
	if bytes.Equal(kp1.PublicKey, kp2.PublicKey) {
		t.Error("rotated keys should be different")
	}

	// Verify the new key pair is retrievable.
	stored, err := mgr.GetKeyPair(groupID)
	if err != nil {
		t.Fatalf("GetKeyPair: %v", err)
	}
	if !bytes.Equal(stored.PublicKey, kp2.PublicKey) {
		t.Error("stored key pair should match latest rotation")
	}
}

func TestKPSManager_RotateKeys_GroupNotFound(t *testing.T) {
	mgr := NewKPSManager(DefaultKPSConfig())
	_, err := mgr.RotateKeys(types.HexToHash("0xBAD"))
	if !errors.Is(err, ErrKPSGroupNotFound) {
		t.Fatalf("expected ErrKPSGroupNotFound, got: %v", err)
	}
}

func TestKPSManager_GetKeyPair_NotFound(t *testing.T) {
	mgr := NewKPSManager(DefaultKPSConfig())
	_, err := mgr.GetKeyPair(types.HexToHash("0xBAD"))
	if !errors.Is(err, ErrKPSGroupNotFound) {
		t.Fatalf("expected ErrKPSGroupNotFound, got: %v", err)
	}
}

func TestSplitRecombine_LargeKey(t *testing.T) {
	// Test with a 32-byte key (standard private key size).
	key := make([]byte, 32)
	for i := range key {
		key[i] = byte(i*13 + 7)
	}

	shares, err := SplitKey(key, 5, 10)
	if err != nil {
		t.Fatalf("SplitKey: %v", err)
	}

	// Use shares 2,4,6,8,10 (every other one, 1-indexed).
	subset := []*KeyShare{shares[1], shares[3], shares[5], shares[7], shares[9]}
	recovered, err := RecombineKey(subset)
	if err != nil {
		t.Fatalf("RecombineKey: %v", err)
	}
	if !bytes.Equal(recovered, key) {
		t.Errorf("recovered key mismatch for 32-byte key")
	}
}

func TestGF256Mul(t *testing.T) {
	// Identity: a * 1 = a.
	for a := 0; a < 256; a++ {
		if gf256Mul(byte(a), 1) != byte(a) {
			t.Fatalf("gf256Mul(%d, 1) != %d", a, a)
		}
	}
	// Zero: a * 0 = 0.
	for a := 0; a < 256; a++ {
		if gf256Mul(byte(a), 0) != 0 {
			t.Fatalf("gf256Mul(%d, 0) != 0", a)
		}
	}
	// Commutativity: a * b = b * a.
	if gf256Mul(0x57, 0x83) != gf256Mul(0x83, 0x57) {
		t.Fatal("gf256Mul not commutative")
	}
}

func TestGF256Inv(t *testing.T) {
	// a * a^(-1) = 1 for all non-zero a.
	for a := 1; a < 256; a++ {
		inv := gf256Inv(byte(a))
		product := gf256Mul(byte(a), inv)
		if product != 1 {
			t.Fatalf("gf256Mul(%d, gf256Inv(%d)) = %d, want 1", a, a, product)
		}
	}
	// 0 has no inverse.
	if gf256Inv(0) != 0 {
		t.Fatal("gf256Inv(0) should be 0")
	}
}

func TestKPSManager_ThreadSafety(t *testing.T) {
	mgr := NewKPSManager(DefaultKPSConfig())

	var wg sync.WaitGroup
	// Concurrent key pair generations.
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, _ = mgr.GenerateKeyPair()
		}()
	}

	// Concurrent group registrations.
	for i := 0; i < 10; i++ {
		wg.Add(1)
		idx := i
		go func() {
			defer wg.Done()
			var gid types.Hash
			gid[0] = byte(idx)
			group := NewKeyGroup(gid, 2, 5)
			mgr.RegisterGroup(group)
		}()
	}

	wg.Wait()
}

func TestKeyGroup_ThreadSafety(t *testing.T) {
	kg := NewKeyGroup(types.HexToHash("0x01"), 2, 100)

	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(1)
		idx := i
		go func() {
			defer wg.Done()
			var addr types.Address
			addr[0] = byte(idx)
			_ = kg.AddMember(addr)
		}()
	}

	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_ = kg.GetMembers()
			_ = kg.MemberCount()
		}()
	}

	wg.Wait()
}

func TestSplitKey_Threshold1_Total1(t *testing.T) {
	key := []byte{0xFF}
	shares, err := SplitKey(key, 1, 1)
	if err != nil {
		t.Fatalf("SplitKey: %v", err)
	}
	if len(shares) != 1 {
		t.Fatalf("got %d shares, want 1", len(shares))
	}

	recovered, err := RecombineKey(shares)
	if err != nil {
		t.Fatalf("RecombineKey: %v", err)
	}
	if !bytes.Equal(recovered, key) {
		t.Errorf("recovered = %x, want %x", recovered, key)
	}
}

func TestSplitKey_ThresholdEqualsTotal(t *testing.T) {
	key := []byte{0x01, 0x02, 0x03}
	shares, err := SplitKey(key, 3, 3)
	if err != nil {
		t.Fatalf("SplitKey: %v", err)
	}

	// All 3 shares required.
	recovered, err := RecombineKey(shares)
	if err != nil {
		t.Fatalf("RecombineKey: %v", err)
	}
	if !bytes.Equal(recovered, key) {
		t.Errorf("recovered = %x, want %x", recovered, key)
	}
}

func TestSplitKey_AllZeroKey(t *testing.T) {
	key := make([]byte, 16)
	shares, err := SplitKey(key, 2, 4)
	if err != nil {
		t.Fatalf("SplitKey: %v", err)
	}

	recovered, err := RecombineKey(shares[:2])
	if err != nil {
		t.Fatalf("RecombineKey: %v", err)
	}
	if !bytes.Equal(recovered, key) {
		t.Errorf("recovered = %x, want all zeros", recovered)
	}
}
