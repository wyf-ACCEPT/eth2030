package light

import (
	"math/big"
	"testing"

	"github.com/eth2028/eth2028/core/types"
)

func TestLightClient_StartStop(t *testing.T) {
	lc := NewLightClient()

	if lc.IsRunning() {
		t.Error("should not be running before Start")
	}

	if err := lc.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}
	if !lc.IsRunning() {
		t.Error("should be running after Start")
	}

	lc.Stop()
	if lc.IsRunning() {
		t.Error("should not be running after Stop")
	}
}

func TestLightClient_ProcessUpdateWhenStopped(t *testing.T) {
	lc := NewLightClient()
	update := makeValidUpdate(100, 90)

	if err := lc.ProcessUpdate(update); err != ErrClientStopped {
		t.Errorf("expected ErrClientStopped, got %v", err)
	}
}

func TestLightClient_ProcessUpdateWhenRunning(t *testing.T) {
	lc := NewLightClient()
	lc.Start()
	defer lc.Stop()

	update := makeValidUpdate(100, 90)
	if err := lc.ProcessUpdate(update); err != nil {
		t.Fatalf("ProcessUpdate: %v", err)
	}

	if !lc.IsSynced() {
		t.Error("should be synced after valid update")
	}

	finalized := lc.GetFinalizedHeader()
	if finalized == nil {
		t.Fatal("finalized header is nil")
	}
	if finalized.Number.Int64() != 90 {
		t.Errorf("finalized = %d, want 90", finalized.Number.Int64())
	}
}

func TestLightClient_GetHeaderByNumber(t *testing.T) {
	lc := NewLightClient()
	lc.Start()
	defer lc.Stop()

	update := makeValidUpdate(100, 90)
	lc.ProcessUpdate(update)

	got := lc.GetHeaderByNumber(90)
	if got == nil {
		t.Fatal("GetHeaderByNumber(90) returned nil")
	}
	if got.Number.Int64() != 90 {
		t.Errorf("number = %d, want 90", got.Number.Int64())
	}
}

func TestLightClient_VerifyStateProof(t *testing.T) {
	lc := NewLightClient()

	header := &types.Header{
		Number: big.NewInt(100),
		Root:   types.HexToHash("0xdeadbeef"),
	}

	key := []byte("test-key")
	value := []byte("test-value")
	proof := BuildStateProof(header.Root, key, value)

	got, err := lc.VerifyStateProof(header, key, proof)
	if err != nil {
		t.Fatalf("VerifyStateProof: %v", err)
	}
	if string(got) != "test-value" {
		t.Errorf("value = %s, want test-value", string(got))
	}
}

func TestLightClient_VerifyStateProofInvalid(t *testing.T) {
	lc := NewLightClient()

	header := &types.Header{
		Number: big.NewInt(100),
		Root:   types.HexToHash("0xdeadbeef"),
	}

	key := []byte("test-key")
	value := []byte("test-value")
	proof := BuildStateProof(header.Root, key, value)

	// Corrupt the proof.
	proof[len(proof)-1] ^= 0xff

	_, err := lc.VerifyStateProof(header, key, proof)
	if err != ErrInvalidProof {
		t.Errorf("expected ErrInvalidProof, got %v", err)
	}
}

func TestLightClient_VerifyStateProofNilHeader(t *testing.T) {
	lc := NewLightClient()
	_, err := lc.VerifyStateProof(nil, []byte("key"), []byte("proof"))
	if err != ErrNoFinalizedHdr {
		t.Errorf("expected ErrNoFinalizedHdr, got %v", err)
	}
}

func TestLightClient_VerifyStateProofShortProof(t *testing.T) {
	lc := NewLightClient()
	header := &types.Header{Number: big.NewInt(1), Root: types.Hash{}}
	_, err := lc.VerifyStateProof(header, []byte("key"), []byte{0x01})
	if err != ErrInvalidProof {
		t.Errorf("expected ErrInvalidProof, got %v", err)
	}
}

func TestBuildStateProof(t *testing.T) {
	root := types.HexToHash("0xabcd")
	key := []byte("mykey")
	value := []byte("myvalue")

	proof := BuildStateProof(root, key, value)

	// Verify structure: 4 bytes len + value + 32 bytes commitment
	expectedLen := 4 + len(value) + 32
	if len(proof) != expectedLen {
		t.Errorf("proof length = %d, want %d", len(proof), expectedLen)
	}

	// Extract value length.
	valLen := uint32(proof[0])<<24 | uint32(proof[1])<<16 | uint32(proof[2])<<8 | uint32(proof[3])
	if int(valLen) != len(value) {
		t.Errorf("encoded value length = %d, want %d", valLen, len(value))
	}
}

func TestLightClientWithStore(t *testing.T) {
	store := NewMemoryLightStore()
	lc := NewLightClientWithStore(store)
	lc.Start()
	defer lc.Stop()

	update := makeValidUpdate(100, 90)
	lc.ProcessUpdate(update)

	// The store should have headers.
	if store.Count() != 2 { // attested + finalized
		t.Errorf("store count = %d, want 2", store.Count())
	}
}

func TestLightClient_Syncer(t *testing.T) {
	lc := NewLightClient()
	if lc.Syncer() == nil {
		t.Error("Syncer() should not be nil")
	}
}
