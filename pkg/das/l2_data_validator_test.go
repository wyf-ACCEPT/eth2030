package das

import (
	"sync"
	"testing"
	"time"

	"github.com/eth2030/eth2030/core/types"
)

func TestL2DataValidatorNew(t *testing.T) {
	v := NewL2DataValidator(10)
	if v == nil {
		t.Fatal("expected non-nil validator")
	}
	if v.maxChains != 10 {
		t.Errorf("maxChains = %d, want 10", v.maxChains)
	}
	if v.ChainCount() != 0 {
		t.Errorf("chain count = %d, want 0", v.ChainCount())
	}

	// Default maxChains when <= 0.
	v2 := NewL2DataValidator(0)
	if v2.maxChains != 256 {
		t.Errorf("default maxChains = %d, want 256", v2.maxChains)
	}
	v3 := NewL2DataValidator(-1)
	if v3.maxChains != 256 {
		t.Errorf("negative maxChains = %d, want 256", v3.maxChains)
	}
}

func TestL2DataValidatorRegisterChain(t *testing.T) {
	v := NewL2DataValidator(10)
	cfg := &L2ChainConfig{
		ChainID:          42,
		MaxBlobSize:      1024,
		RequiredCustody:  4,
		CompressionCodec: "zstd",
	}

	if err := v.RegisterChain(42, cfg); err != nil {
		t.Fatalf("RegisterChain: %v", err)
	}
	if v.ChainCount() != 1 {
		t.Errorf("chain count = %d, want 1", v.ChainCount())
	}

	// Verify config was stored.
	got := v.GetChainConfig(42)
	if got == nil {
		t.Fatal("GetChainConfig returned nil")
	}
	if got.MaxBlobSize != 1024 {
		t.Errorf("MaxBlobSize = %d, want 1024", got.MaxBlobSize)
	}
	if got.CompressionCodec != "zstd" {
		t.Errorf("CompressionCodec = %q, want zstd", got.CompressionCodec)
	}

	// Register with nil config should use defaults.
	if err := v.RegisterChain(99, nil); err != nil {
		t.Fatalf("RegisterChain with nil config: %v", err)
	}
	nilCfg := v.GetChainConfig(99)
	if nilCfg.MaxBlobSize != 4*1024*1024 {
		t.Errorf("default MaxBlobSize = %d, want %d", nilCfg.MaxBlobSize, 4*1024*1024)
	}
}

func TestL2DataValidatorValidateData(t *testing.T) {
	v := NewL2DataValidator(10)
	_ = v.RegisterChain(1, &L2ChainConfig{MaxBlobSize: 1 << 20})

	data := []byte("hello teragas world")
	commitment := computeL2Commitment(1, data)

	if err := v.ValidateL2Data(1, data, commitment); err != nil {
		t.Fatalf("ValidateL2Data: %v", err)
	}
}

func TestL2DataValidatorInvalidCommitment(t *testing.T) {
	v := NewL2DataValidator(10)
	_ = v.RegisterChain(1, &L2ChainConfig{MaxBlobSize: 1 << 20})

	data := []byte("some data")
	badCommitment := make([]byte, 32)
	for i := range badCommitment {
		badCommitment[i] = 0xff
	}

	err := v.ValidateL2Data(1, data, badCommitment)
	if err != ErrL2ValidatorInvalidCommitment {
		t.Errorf("expected ErrL2ValidatorInvalidCommitment, got %v", err)
	}

	// Wrong length commitment.
	err = v.ValidateL2Data(1, data, []byte{0x01, 0x02})
	if err != ErrL2ValidatorInvalidCommitment {
		t.Errorf("expected ErrL2ValidatorInvalidCommitment for short commitment, got %v", err)
	}
}

func TestL2DataValidatorValidateAndStore(t *testing.T) {
	v := NewL2DataValidator(10)
	_ = v.RegisterChain(5, &L2ChainConfig{MaxBlobSize: 1 << 20})

	data := []byte("block data for slot 100")
	receipt, err := v.ValidateAndStore(5, 100, data)
	if err != nil {
		t.Fatalf("ValidateAndStore: %v", err)
	}
	if receipt == nil {
		t.Fatal("expected non-nil receipt")
	}
	if receipt.ChainID != 5 {
		t.Errorf("receipt ChainID = %d, want 5", receipt.ChainID)
	}
	if receipt.Slot != 100 {
		t.Errorf("receipt Slot = %d, want 100", receipt.Slot)
	}
	if receipt.Size != uint64(len(data)) {
		t.Errorf("receipt Size = %d, want %d", receipt.Size, len(data))
	}
	if receipt.Commitment == (types.Hash{}) {
		t.Error("receipt commitment should not be zero")
	}
	if receipt.ProofHash == (types.Hash{}) {
		t.Error("receipt proof hash should not be zero")
	}

	// Verify the commitment matches.
	expected := computeL2Commitment(5, data)
	var expectedHash types.Hash
	copy(expectedHash[:], expected)
	if receipt.Commitment != expectedHash {
		t.Error("receipt commitment does not match expected")
	}
}

func TestL2DataValidatorPruneData(t *testing.T) {
	v := NewL2DataValidator(10)
	_ = v.RegisterChain(1, &L2ChainConfig{MaxBlobSize: 1 << 20})

	// Store entries at slots 1..5.
	for i := uint64(1); i <= 5; i++ {
		data := []byte{byte(i), byte(i + 1), byte(i + 2)}
		_, err := v.ValidateAndStore(1, i, data)
		if err != nil {
			t.Fatalf("ValidateAndStore slot %d: %v", i, err)
		}
	}

	m := v.GetChainMetrics(1)
	if m.TotalBlobs != 5 {
		t.Fatalf("before prune TotalBlobs = %d, want 5", m.TotalBlobs)
	}

	// Prune entries with slot < 3 (slots 1 and 2).
	pruned := v.PruneChainData(1, 3)
	if pruned != 2 {
		t.Errorf("pruned = %d, want 2", pruned)
	}

	m = v.GetChainMetrics(1)
	if m.TotalBlobs != 3 {
		t.Errorf("after prune TotalBlobs = %d, want 3", m.TotalBlobs)
	}

	// Prune on unregistered chain returns 0.
	if v.PruneChainData(999, 100) != 0 {
		t.Error("expected 0 pruned for unregistered chain")
	}
}

func TestL2DataValidatorMultipleChains(t *testing.T) {
	v := NewL2DataValidator(100)

	for id := uint64(1); id <= 5; id++ {
		err := v.RegisterChain(id, &L2ChainConfig{MaxBlobSize: 1 << 20})
		if err != nil {
			t.Fatalf("RegisterChain %d: %v", id, err)
		}
	}

	if v.ChainCount() != 5 {
		t.Errorf("chain count = %d, want 5", v.ChainCount())
	}

	// Store data on different chains.
	for id := uint64(1); id <= 5; id++ {
		data := make([]byte, 100*int(id))
		for i := range data {
			data[i] = byte(id)
		}
		_, err := v.ValidateAndStore(id, 1, data)
		if err != nil {
			t.Fatalf("ValidateAndStore chain %d: %v", id, err)
		}
	}

	// Verify metrics per chain.
	for id := uint64(1); id <= 5; id++ {
		m := v.GetChainMetrics(id)
		if m == nil {
			t.Fatalf("chain %d metrics nil", id)
		}
		if m.TotalBlobs != 1 {
			t.Errorf("chain %d TotalBlobs = %d, want 1", id, m.TotalBlobs)
		}
		if m.TotalBytes != 100*id {
			t.Errorf("chain %d TotalBytes = %d, want %d", id, m.TotalBytes, 100*id)
		}
	}
}

func TestL2DataValidatorChainMetrics(t *testing.T) {
	v := NewL2DataValidator(10)
	_ = v.RegisterChain(7, &L2ChainConfig{MaxBlobSize: 1 << 20})

	// No data yet.
	m := v.GetChainMetrics(7)
	if m == nil {
		t.Fatal("expected non-nil metrics for registered chain")
	}
	if m.TotalBlobs != 0 || m.TotalBytes != 0 {
		t.Errorf("initial metrics: blobs=%d bytes=%d, want 0,0", m.TotalBlobs, m.TotalBytes)
	}

	// Store some data.
	_, _ = v.ValidateAndStore(7, 1, make([]byte, 200))
	_, _ = v.ValidateAndStore(7, 2, make([]byte, 400))

	m = v.GetChainMetrics(7)
	if m.TotalBlobs != 2 {
		t.Errorf("TotalBlobs = %d, want 2", m.TotalBlobs)
	}
	if m.TotalBytes != 600 {
		t.Errorf("TotalBytes = %d, want 600", m.TotalBytes)
	}
	if m.AvgBlobSize != 300 {
		t.Errorf("AvgBlobSize = %d, want 300", m.AvgBlobSize)
	}

	// Unregistered chain returns nil.
	if v.GetChainMetrics(999) != nil {
		t.Error("expected nil metrics for unregistered chain")
	}
}

func TestL2DataValidatorActiveChains(t *testing.T) {
	v := NewL2DataValidator(10)
	_ = v.RegisterChain(30, nil)
	_ = v.RegisterChain(10, nil)
	_ = v.RegisterChain(20, nil)

	chains := v.ActiveChains()
	if len(chains) != 3 {
		t.Fatalf("ActiveChains len = %d, want 3", len(chains))
	}
	// Should be sorted.
	if chains[0] != 10 || chains[1] != 20 || chains[2] != 30 {
		t.Errorf("ActiveChains = %v, want [10 20 30]", chains)
	}

	// Empty validator.
	v2 := NewL2DataValidator(10)
	if len(v2.ActiveChains()) != 0 {
		t.Error("expected empty active chains")
	}
}

func TestL2DataValidatorMaxBlobSize(t *testing.T) {
	v := NewL2DataValidator(10)
	_ = v.RegisterChain(1, &L2ChainConfig{MaxBlobSize: 100})

	// Data within limit.
	data := make([]byte, 100)
	commitment := computeL2Commitment(1, data)
	if err := v.ValidateL2Data(1, data, commitment); err != nil {
		t.Fatalf("ValidateL2Data within limit: %v", err)
	}

	// Data exceeding limit.
	bigData := make([]byte, 101)
	err := v.ValidateL2Data(1, bigData, computeL2Commitment(1, bigData))
	if err != ErrL2ValidatorDataTooLarge {
		t.Errorf("expected ErrL2ValidatorDataTooLarge, got %v", err)
	}

	// ValidateAndStore also enforces limit.
	_, err = v.ValidateAndStore(1, 1, bigData)
	if err != ErrL2ValidatorDataTooLarge {
		t.Errorf("ValidateAndStore: expected ErrL2ValidatorDataTooLarge, got %v", err)
	}
}

func TestL2DataValidatorEmptyData(t *testing.T) {
	v := NewL2DataValidator(10)
	_ = v.RegisterChain(1, nil)

	err := v.ValidateL2Data(1, nil, []byte{0x01})
	if err != ErrL2ValidatorEmptyData {
		t.Errorf("nil data: expected ErrL2ValidatorEmptyData, got %v", err)
	}

	err = v.ValidateL2Data(1, []byte{}, []byte{0x01})
	if err != ErrL2ValidatorEmptyData {
		t.Errorf("empty data: expected ErrL2ValidatorEmptyData, got %v", err)
	}

	_, err = v.ValidateAndStore(1, 1, nil)
	if err != ErrL2ValidatorEmptyData {
		t.Errorf("ValidateAndStore nil data: expected ErrL2ValidatorEmptyData, got %v", err)
	}
}

func TestL2DataValidatorLargeData(t *testing.T) {
	v := NewL2DataValidator(10)
	_ = v.RegisterChain(1, &L2ChainConfig{MaxBlobSize: 1 << 20}) // 1 MiB

	// Store a large blob and verify.
	data := make([]byte, 1<<20)
	for i := range data {
		data[i] = byte(i % 256)
	}

	receipt, err := v.ValidateAndStore(1, 50, data)
	if err != nil {
		t.Fatalf("ValidateAndStore large data: %v", err)
	}
	if receipt.Size != 1<<20 {
		t.Errorf("receipt size = %d, want %d", receipt.Size, 1<<20)
	}

	// Validate the commitment independently.
	commitment := computeL2Commitment(1, data)
	if err := v.ValidateL2Data(1, data, commitment); err != nil {
		t.Errorf("ValidateL2Data large data: %v", err)
	}
}

func TestL2DataValidatorConcurrentAccess(t *testing.T) {
	v := NewL2DataValidator(100)
	for id := uint64(1); id <= 10; id++ {
		_ = v.RegisterChain(id, &L2ChainConfig{MaxBlobSize: 1 << 20})
	}
	var wg sync.WaitGroup
	errCh := make(chan error, 100)
	for id := uint64(1); id <= 10; id++ {
		wg.Add(2)
		go func(cid uint64) {
			defer wg.Done()
			for s := uint64(0); s < 10; s++ {
				d := make([]byte, 64)
				d[0], d[1] = byte(cid), byte(s)
				if _, err := v.ValidateAndStore(cid, s, d); err != nil {
					errCh <- err
				}
			}
		}(id)
		go func(cid uint64) {
			defer wg.Done()
			for i := 0; i < 10; i++ {
				_ = v.GetChainMetrics(cid)
				_ = v.ActiveChains()
			}
		}(id)
	}
	wg.Wait()
	close(errCh)
	for err := range errCh {
		t.Errorf("concurrent error: %v", err)
	}
	for id := uint64(1); id <= 10; id++ {
		m := v.GetChainMetrics(id)
		if m.TotalBlobs != 10 {
			t.Errorf("chain %d: TotalBlobs = %d, want 10", id, m.TotalBlobs)
		}
	}
}

func TestL2DataValidatorUnregisteredChain(t *testing.T) {
	v := NewL2DataValidator(10)

	err := v.ValidateL2Data(42, []byte("data"), []byte("commit"))
	if err != ErrL2ValidatorChainNotRegistered {
		t.Errorf("validate on unregistered: expected ErrL2ValidatorChainNotRegistered, got %v", err)
	}

	_, err = v.ValidateAndStore(42, 1, []byte("data"))
	if err != ErrL2ValidatorChainNotRegistered {
		t.Errorf("store on unregistered: expected ErrL2ValidatorChainNotRegistered, got %v", err)
	}

	if v.GetChainConfig(42) != nil {
		t.Error("expected nil config for unregistered chain")
	}

	// ChainID 0 is invalid.
	err = v.RegisterChain(0, nil)
	if err != ErrL2ValidatorInvalidChainID {
		t.Errorf("register chain 0: expected ErrL2ValidatorInvalidChainID, got %v", err)
	}
}

func TestL2DataValidatorDuplicateRegister(t *testing.T) {
	v := NewL2DataValidator(10)
	if err := v.RegisterChain(1, nil); err != nil {
		t.Fatalf("first register: %v", err)
	}

	err := v.RegisterChain(1, nil)
	if err != ErrL2ValidatorChainAlreadyExists {
		t.Errorf("duplicate register: expected ErrL2ValidatorChainAlreadyExists, got %v", err)
	}
}

func TestL2DataValidatorMaxChainsReached(t *testing.T) {
	v := NewL2DataValidator(3)
	for id := uint64(1); id <= 3; id++ {
		if err := v.RegisterChain(id, nil); err != nil {
			t.Fatalf("RegisterChain %d: %v", id, err)
		}
	}

	err := v.RegisterChain(4, nil)
	if err != ErrL2ValidatorMaxChainsReached {
		t.Errorf("expected ErrL2ValidatorMaxChainsReached, got %v", err)
	}
}

func TestL2DataValidatorPruneNoData(t *testing.T) {
	v := NewL2DataValidator(10)
	_ = v.RegisterChain(1, nil)

	// Prune on chain with no entries.
	pruned := v.PruneChainData(1, 100)
	if pruned != 0 {
		t.Errorf("pruned = %d, want 0 for empty chain", pruned)
	}
}

func TestL2DataValidatorMetricsAfterPrune(t *testing.T) {
	v := NewL2DataValidator(10)
	_ = v.RegisterChain(1, &L2ChainConfig{MaxBlobSize: 1 << 20})

	// Store 3 entries of 100 bytes each.
	for slot := uint64(1); slot <= 3; slot++ {
		_, _ = v.ValidateAndStore(1, slot, make([]byte, 100))
	}

	// Prune all entries.
	pruned := v.PruneChainData(1, 100)
	if pruned != 3 {
		t.Errorf("pruned = %d, want 3", pruned)
	}

	m := v.GetChainMetrics(1)
	if m.TotalBlobs != 0 {
		t.Errorf("after full prune TotalBlobs = %d, want 0", m.TotalBlobs)
	}
	if m.TotalBytes != 0 {
		t.Errorf("after full prune TotalBytes = %d, want 0", m.TotalBytes)
	}
	if m.AvgBlobSize != 0 {
		t.Errorf("after full prune AvgBlobSize = %d, want 0", m.AvgBlobSize)
	}
}

func TestL2DataValidatorPeakThroughput(t *testing.T) {
	v := NewL2DataValidator(10)
	// Inject a fake time function so all stores happen in the same window.
	now := time.Now()
	v.timeFunc = func() time.Time { return now }

	_ = v.RegisterChain(1, &L2ChainConfig{MaxBlobSize: 1 << 20})

	// Store multiple blobs in the same time window.
	for i := 0; i < 5; i++ {
		_, _ = v.ValidateAndStore(1, uint64(i), make([]byte, 1000))
	}

	m := v.GetChainMetrics(1)
	if m.PeakThroughputBps < 5000 {
		t.Errorf("PeakThroughputBps = %d, want >= 5000", m.PeakThroughputBps)
	}
}
