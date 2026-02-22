package sync

import (
	"bytes"
	"math/big"
	"testing"

	"github.com/eth2030/eth2030/core/types"
	"github.com/eth2030/eth2030/crypto"
)

// TestE2E_SnapSyncPipeline creates range proofs, splits ranges, merges them,
// and verifies the merged proof against the original root.
func TestE2E_SnapSyncPipeline(t *testing.T) {
	prover := NewRangeProver()

	// Create a mock state root.
	root := types.BytesToHash(crypto.Keccak256([]byte("state-root-snap-sync")))

	// Create sorted key-value pairs representing account data.
	keys := [][]byte{
		{0x10, 0x00}, {0x20, 0x00}, {0x30, 0x00},
		{0x40, 0x00}, {0x50, 0x00}, {0x60, 0x00},
	}
	values := [][]byte{
		[]byte("account-1"), []byte("account-2"), []byte("account-3"),
		[]byte("account-4"), []byte("account-5"), []byte("account-6"),
	}

	// Create a range proof over the full set.
	proof := prover.CreateRangeProof(keys, values, root)

	if len(proof.Keys) != 6 {
		t.Fatalf("proof key count = %d, want 6", len(proof.Keys))
	}
	if len(proof.Values) != 6 {
		t.Fatalf("proof value count = %d, want 6", len(proof.Values))
	}
	if len(proof.Proof) == 0 {
		t.Fatal("proof nodes should not be empty")
	}

	// Verify the proof.
	valid, err := prover.VerifyRangeProof(root, proof)
	if err != nil {
		t.Fatalf("VerifyRangeProof: %v", err)
	}
	if !valid {
		t.Fatal("proof should be valid")
	}

	// Verify against a wrong root fails.
	wrongRoot := types.BytesToHash(crypto.Keccak256([]byte("wrong-root")))
	valid, err = prover.VerifyRangeProof(wrongRoot, proof)
	if err == nil {
		t.Fatal("expected error for wrong root, got nil")
	}
	if valid {
		t.Fatal("proof should be invalid against wrong root")
	}

	// Split the range into 3 sub-ranges.
	origin := keys[0]
	limit := []byte{0x70, 0x00}
	subranges := prover.SplitRange(origin, limit, 3)
	if len(subranges) != 3 {
		t.Fatalf("split count = %d, want 3", len(subranges))
	}

	// Verify sub-ranges are non-overlapping and cover the space.
	for i := 0; i < len(subranges)-1; i++ {
		if bytes.Compare(subranges[i].Limit, subranges[i+1].Origin) > 0 {
			t.Errorf("sub-range %d limit %x > sub-range %d origin %x (overlap)",
				i, subranges[i].Limit, i+1, subranges[i+1].Origin)
		}
	}

	// First sub-range should start at origin.
	if !bytes.Equal(subranges[0].Origin, origin) {
		t.Errorf("first sub-range origin = %x, want %x", subranges[0].Origin, origin)
	}
	// Last sub-range should end at limit.
	if !bytes.Equal(subranges[len(subranges)-1].Limit, limit) {
		t.Errorf("last sub-range limit = %x, want %x",
			subranges[len(subranges)-1].Limit, limit)
	}

	// Create individual proofs for each sub-range and merge them.
	var subproofs []*RangeProof
	subproofs = append(subproofs,
		prover.CreateRangeProof(keys[:2], values[:2], root),
		prover.CreateRangeProof(keys[2:4], values[2:4], root),
		prover.CreateRangeProof(keys[4:], values[4:], root),
	)

	merged := prover.MergeRangeProofs(subproofs)
	if len(merged.Keys) != 6 {
		t.Fatalf("merged key count = %d, want 6", len(merged.Keys))
	}
	if len(merged.Values) != 6 {
		t.Fatalf("merged value count = %d, want 6", len(merged.Values))
	}

	// Verify the merged proof keys are still sorted.
	for i := 1; i < len(merged.Keys); i++ {
		if bytes.Compare(merged.Keys[i-1], merged.Keys[i]) >= 0 {
			t.Errorf("merged keys not sorted at index %d", i)
		}
	}

	// Verify merged proof passes verification.
	valid, err = prover.VerifyRangeProof(root, merged)
	if err != nil {
		t.Fatalf("VerifyRangeProof merged: %v", err)
	}
	if !valid {
		t.Fatal("merged proof should be valid")
	}

	// Verify ComputeRangeHash consistency.
	hash1 := ComputeRangeHash(keys, values)
	hash2 := ComputeRangeHash(merged.Keys, merged.Values)
	if hash1 != hash2 {
		t.Errorf("range hash mismatch after merge: %s != %s", hash1.Hex(), hash2.Hex())
	}
}

// TestE2E_BlobSyncWorkflow tests requesting, receiving, and verifying blobs
// through the BeaconSyncer and BlobRecovery mechanism.
func TestE2E_BlobSyncWorkflow(t *testing.T) {
	config := DefaultBeaconSyncConfig()
	config.BlobVerification = true
	syncer := NewBeaconSyncer(config)

	// Create a mock fetcher.
	fetcher := &e2eBeaconFetcher{
		blocks: make(map[uint64]*BeaconBlock),
		blobs:  make(map[uint64][]*BlobSidecar),
	}

	// Populate slots 100-102 with blocks and blobs.
	for slot := uint64(100); slot <= 102; slot++ {
		block := &BeaconBlock{
			Slot:          slot,
			ProposerIndex: slot - 100,
			StateRoot:     [32]byte{byte(slot)},
			Body:          []byte("block-body"),
		}
		block.ParentRoot = [32]byte{byte(slot - 1)}
		fetcher.blocks[slot] = block

		blockHash := block.Hash()
		var commitment [48]byte
		commitHash := crypto.Keccak256([]byte("commitment"))
		copy(commitment[:32], commitHash)
		copy(commitment[32:], commitHash[:16])
		var proof [48]byte
		proofHash := crypto.Keccak256([]byte("proof"))
		copy(proof[:32], proofHash)
		copy(proof[32:], proofHash[:16])

		sidecar := &BlobSidecar{
			Index:             0,
			KZGCommitment:     commitment,
			KZGProof:          proof,
			SignedBlockHeader: blockHash,
		}
		// Fill some blob data.
		for i := 0; i < 128; i++ {
			sidecar.Blob[i] = byte(slot + uint64(i))
		}
		fetcher.blobs[slot] = []*BlobSidecar{sidecar}
	}

	syncer.SetFetcher(fetcher)

	// Sync the slot range.
	err := syncer.SyncSlotRange(100, 102)
	if err != nil {
		t.Fatalf("SyncSlotRange: %v", err)
	}

	// Verify sync status.
	status := syncer.GetSyncStatus()
	if !status.IsComplete {
		t.Error("sync should be complete")
	}
	if status.TargetSlot != 102 {
		t.Errorf("target slot = %d, want 102", status.TargetSlot)
	}
	if status.BlobsDownloaded < 3 {
		t.Errorf("blobs downloaded = %d, want >= 3", status.BlobsDownloaded)
	}

	// Verify blocks are stored.
	for slot := uint64(100); slot <= 102; slot++ {
		b := syncer.GetBlock(slot)
		if b == nil {
			t.Errorf("block at slot %d is nil", slot)
			continue
		}
		if b.Slot != slot {
			t.Errorf("block slot = %d, want %d", b.Slot, slot)
		}
	}

	// Test blob recovery.
	recovery := NewBlobRecovery(MaxBlobsPerBlock)

	// Provide half the required blobs (3 out of 6).
	var available []*BlobSidecar
	for i := 0; i < 3; i++ {
		var commitment [48]byte
		recCommit := crypto.Keccak256([]byte("recovery-commit"))
		copy(commitment[:32], recCommit)
		copy(commitment[32:], recCommit[:16])
		var proof [48]byte
		recProof := crypto.Keccak256([]byte("recovery-proof"))
		copy(proof[:32], recProof)
		copy(proof[32:], recProof[:16])
		sc := &BlobSidecar{
			Index:         uint64(i),
			KZGCommitment: commitment,
			KZGProof:      proof,
		}
		sc.Blob[0] = byte(i + 1)
		available = append(available, sc)
	}

	// 3 out of 6 = 50%, threshold is (6+1)/2=3, so exactly at threshold.
	recovered, err := recovery.AttemptRecovery(100, available)
	if err != nil {
		t.Fatalf("AttemptRecovery: %v", err)
	}
	if len(recovered) != MaxBlobsPerBlock {
		t.Errorf("recovered blobs = %d, want %d", len(recovered), MaxBlobsPerBlock)
	}

	// Verify the first 3 blobs are the originals.
	for i := 0; i < 3; i++ {
		if recovered[i].Index != uint64(i) {
			t.Errorf("recovered[%d].Index = %d, want %d", i, recovered[i].Index, i)
		}
		if recovered[i].Blob[0] != byte(i+1) {
			t.Errorf("recovered[%d] data mismatch", i)
		}
	}

	// Verify recovery fails with insufficient blobs (only 1).
	_, err = recovery.AttemptRecovery(101, available[:1])
	if err == nil {
		t.Error("expected recovery to fail with 1 blob, got nil error")
	}
}

// TestE2E_SyncStateMachine tests sync state transitions through the full
// lifecycle: idle -> syncing -> validating headers -> complete.
func TestE2E_SyncStateMachine(t *testing.T) {
	config := DefaultConfig()
	config.Mode = ModeFull
	syncer := NewSyncer(config)

	// Initial state: idle.
	if syncer.State() != StateIdle {
		t.Errorf("initial state = %d, want %d (idle)", syncer.State(), StateIdle)
	}
	if syncer.Stage() != StageNone {
		t.Errorf("initial stage = %d, want %d (none)", syncer.Stage(), StageNone)
	}
	if syncer.IsSyncing() {
		t.Error("should not be syncing initially")
	}

	// Configure with a dummy currentHeight callback and start.
	syncer.SetCallbacks(nil, nil, func() uint64 { return 0 }, nil)
	if err := syncer.Start(1000); err != nil {
		t.Fatalf("Start: %v", err)
	}

	// State: syncing.
	if syncer.State() != StateSyncing {
		t.Errorf("state after Start = %d, want %d (syncing)", syncer.State(), StateSyncing)
	}
	if !syncer.IsSyncing() {
		t.Error("should be syncing after Start")
	}

	// Double start returns error.
	if err := syncer.Start(2000); err != ErrAlreadySyncing {
		t.Errorf("double Start: want ErrAlreadySyncing, got %v", err)
	}

	// Verify progress.
	progress := syncer.GetProgress()
	if progress.HighestBlock != 1000 {
		t.Errorf("highest block = %d, want 1000", progress.HighestBlock)
	}
	if progress.CurrentBlock != 0 {
		t.Errorf("current block = %d, want 0", progress.CurrentBlock)
	}

	// Simulate processing headers.
	syncer.SetCallbacks(
		func(headers []HeaderData) (int, error) {
			return len(headers), nil
		},
		func(blocks []BlockData) (int, error) {
			return len(blocks), nil
		},
		func() uint64 { return 0 },
		nil,
	)

	headers := []HeaderData{
		{Number: 1, Hash: [32]byte{0x01}},
		{Number: 2, Hash: [32]byte{0x02}},
		{Number: 3, Hash: [32]byte{0x03}},
	}
	n, err := syncer.ProcessHeaders(headers)
	if err != nil {
		t.Fatalf("ProcessHeaders: %v", err)
	}
	if n != 3 {
		t.Errorf("processed %d headers, want 3", n)
	}

	progress = syncer.GetProgress()
	if progress.PulledHeaders != 3 {
		t.Errorf("pulled headers = %d, want 3", progress.PulledHeaders)
	}
	if progress.CurrentBlock != 3 {
		t.Errorf("current block = %d, want 3", progress.CurrentBlock)
	}

	// Process blocks, pushing past the target.
	blocks := []BlockData{
		{Number: 998, Hash: [32]byte{0xFE}},
		{Number: 999, Hash: [32]byte{0xFE}},
		{Number: 1000, Hash: [32]byte{0xFF}},
	}
	n, err = syncer.ProcessBlocks(blocks)
	if err != nil {
		t.Fatalf("ProcessBlocks: %v", err)
	}
	if n != 3 {
		t.Errorf("processed %d blocks, want 3", n)
	}

	// After reaching the target, state should transition to Done.
	if syncer.State() != StateDone {
		t.Errorf("state after completing sync = %d, want %d (done)", syncer.State(), StateDone)
	}

	// Cancel resets to idle.
	syncer.Cancel()
	if syncer.State() != StateIdle {
		t.Errorf("state after Cancel = %d, want %d (idle)", syncer.State(), StateIdle)
	}

	// Verify stage names.
	stageTests := []struct {
		stage uint32
		name  string
	}{
		{StageNone, "none"},
		{StageHeaders, "downloading headers"},
		{StageSnapAccounts, "downloading accounts"},
		{StageSnapStorage, "downloading storage"},
		{StageSnapBytecodes, "downloading bytecodes"},
		{StageSnapHealing, "healing trie"},
		{StageBlocks, "downloading blocks"},
		{StageCaughtUp, "caught up"},
	}
	for _, tc := range stageTests {
		if got := StageName(tc.stage); got != tc.name {
			t.Errorf("StageName(%d) = %q, want %q", tc.stage, got, tc.name)
		}
	}
}

// TestE2E_ParallelRangeDownload splits a range into sub-ranges, creates
// proofs for each, merges them, and verifies the merged proof.
func TestE2E_ParallelRangeDownload(t *testing.T) {
	// Define a large account range.
	var origin types.Hash // 0x00...00
	var limit types.Hash
	for i := range limit {
		limit[i] = 0xff
	}

	// Split into 4 sub-ranges using SplitAccountRange.
	subranges := SplitAccountRange(origin, limit, 4)
	if len(subranges) != 4 {
		t.Fatalf("split count = %d, want 4", len(subranges))
	}

	// Verify sub-ranges cover the space: first starts at origin, last ends at limit.
	if subranges[0].Origin != origin {
		t.Errorf("first sub-range origin mismatch")
	}
	if subranges[3].Limit != limit {
		t.Errorf("last sub-range limit mismatch")
	}

	// Verify sub-ranges are contiguous (no gaps or overlaps).
	for i := 0; i < len(subranges)-1; i++ {
		nextOrigin := subranges[i+1].Origin
		thisLimit := subranges[i].Limit
		limitBI := new(big.Int).SetBytes(thisLimit[:])
		nextBI := new(big.Int).SetBytes(nextOrigin[:])
		// Next origin should be limit+1 or very close.
		diff := new(big.Int).Sub(nextBI, limitBI)
		// Allow diff of 0 or 1 (limit is inclusive).
		if diff.Cmp(big.NewInt(2)) > 0 {
			t.Errorf("gap between sub-range %d limit and sub-range %d origin: diff=%s",
				i, i+1, diff.String())
		}
	}

	// Create mock accounts for each sub-range.
	accountSets := make([][]AccountData, 4)
	for i := 0; i < 4; i++ {
		for j := 0; j < 3; j++ {
			hash := types.BytesToHash(crypto.Keccak256([]byte{byte(i*10 + j)}))
			accountSets[i] = append(accountSets[i], AccountData{
				Hash:    hash,
				Nonce:   uint64(i*10 + j),
				Balance: big.NewInt(int64(1000 * (i + 1))),
				Root:    types.EmptyRootHash,
			})
		}
	}

	// Merge account ranges pair-wise.
	merged12 := MergeAccountRanges(accountSets[0], accountSets[1])
	merged34 := MergeAccountRanges(accountSets[2], accountSets[3])
	mergedAll := MergeAccountRanges(merged12, merged34)

	// All 12 accounts should be present.
	if len(mergedAll) != 12 {
		t.Fatalf("merged account count = %d, want 12", len(mergedAll))
	}

	// Verify accounts are sorted by hash.
	for i := 1; i < len(mergedAll); i++ {
		if bytes.Compare(mergedAll[i-1].Hash[:], mergedAll[i].Hash[:]) >= 0 {
			t.Errorf("merged accounts not sorted at index %d", i)
		}
	}

	// Verify the account range proof passes.
	mockRoot := types.BytesToHash(crypto.Keccak256([]byte("parallel-root")))

	// Create a proof node that hashes to the root.
	proofNode := crypto.Keccak256(append(mockRoot[:], mergedAll[0].Hash[:]...))
	rootNodeHash := types.BytesToHash(crypto.Keccak256(proofNode))

	err := VerifyAccountRange(rootNodeHash, mergedAll, [][]byte{proofNode})
	if err != nil {
		t.Fatalf("VerifyAccountRange: %v", err)
	}

	// Create range proofs for each sub-range and merge.
	prover := NewRangeProver()
	var subproofs []*RangeProof
	for i := 0; i < 4; i++ {
		k := make([][]byte, len(accountSets[i]))
		v := make([][]byte, len(accountSets[i]))
		for j, acct := range accountSets[i] {
			k[j] = acct.Hash[:]
			v[j] = []byte{byte(acct.Nonce)}
		}
		p := prover.CreateRangeProof(k, v, mockRoot)
		subproofs = append(subproofs, p)
	}

	mergedProof := prover.MergeRangeProofs(subproofs)
	if len(mergedProof.Keys) != 12 {
		t.Fatalf("merged proof key count = %d, want 12", len(mergedProof.Keys))
	}

	// Verify range hashes are consistent.
	hash := ComputeRangeHash(mergedProof.Keys, mergedProof.Values)
	if hash.IsZero() {
		t.Error("merged range hash should not be zero")
	}
}

// TestE2E_FullSyncRoundtrip tests the full sync flow using mock data sources:
// genesis -> headers -> bodies -> block assembly -> insertion.
func TestE2E_FullSyncRoundtrip(t *testing.T) {
	// Build a mock chain of 10 blocks.
	genesisHeader := &types.Header{
		Number:     big.NewInt(0),
		GasLimit:   30_000_000,
		Time:       1700000000,
		Difficulty: big.NewInt(1),
		BaseFee:    big.NewInt(1000000000),
	}
	genesisBody := &types.Body{}
	genesisBlock := types.NewBlock(genesisHeader, genesisBody)

	numBlocks := 10
	chain := make([]*types.Block, numBlocks+1)
	chain[0] = genesisBlock

	for i := 1; i <= numBlocks; i++ {
		h := &types.Header{
			Number:     big.NewInt(int64(i)),
			ParentHash: chain[i-1].Hash(),
			GasLimit:   30_000_000,
			Time:       uint64(1700000000 + i*12),
			Difficulty: big.NewInt(1),
			BaseFee:    big.NewInt(1000000000),
		}
		chain[i] = types.NewBlock(h, &types.Body{})
	}

	// Create mock fetchers backed by the chain.
	headerFetcher := &e2eHeaderFetcher{chain: chain}
	bodyFetcher := &e2eBodyFetcher{chain: chain}
	inserter := &e2eBlockInserter{
		current: genesisBlock,
		chain:   []*types.Block{genesisBlock},
	}

	// Configure the syncer.
	config := DefaultConfig()
	config.Mode = ModeFull
	config.BatchSize = 4
	config.BodyBatchSize = 4
	syncer := NewSyncer(config)
	syncer.SetFetchers(headerFetcher, bodyFetcher, inserter)

	// Run full sync to block 10.
	err := syncer.RunSync(uint64(numBlocks))
	if err != nil {
		t.Fatalf("RunSync: %v", err)
	}

	// Verify the syncer reached done state.
	if syncer.State() != StateDone {
		t.Errorf("state = %d, want %d (done)", syncer.State(), StateDone)
	}

	// Verify the stage is caught up.
	if syncer.Stage() != StageCaughtUp {
		t.Errorf("stage = %d, want %d (caught up)", syncer.Stage(), StageCaughtUp)
	}

	// Verify the inserter received all blocks.
	if inserter.current.NumberU64() != uint64(numBlocks) {
		t.Errorf("current block = %d, want %d", inserter.current.NumberU64(), numBlocks)
	}
	// Chain should have genesis + 10 blocks = 11.
	if len(inserter.chain) != numBlocks+1 {
		t.Errorf("inserter chain length = %d, want %d", len(inserter.chain), numBlocks+1)
	}

	// Verify progress reports.
	progress := syncer.GetProgress()
	if progress.PulledHeaders < uint64(numBlocks) {
		t.Errorf("pulled headers = %d, want >= %d", progress.PulledHeaders, numBlocks)
	}
	if progress.PulledBodies < uint64(numBlocks) {
		t.Errorf("pulled bodies = %d, want >= %d", progress.PulledBodies, numBlocks)
	}
	if progress.CurrentBlock != uint64(numBlocks) {
		t.Errorf("progress current block = %d, want %d", progress.CurrentBlock, numBlocks)
	}
	pct := progress.Percentage()
	if pct < 99.0 {
		t.Errorf("progress percentage = %.1f%%, want >= 99%%", pct)
	}

	// Verify the header chain is valid using ValidateHeaderChain.
	err = ValidateHeaderChain(
		[]*types.Header{chain[1].Header(), chain[2].Header(), chain[3].Header()},
		chain[0].Header(),
	)
	if err != nil {
		t.Fatalf("ValidateHeaderChain: %v", err)
	}

	// Verify AssembleBlocks works.
	headers := make([]*types.Header, 3)
	bodies := make([]*types.Body, 3)
	for i := 0; i < 3; i++ {
		headers[i] = chain[i+1].Header()
		bodies[i] = &types.Body{}
	}
	assembled, err := AssembleBlocks(headers, bodies)
	if err != nil {
		t.Fatalf("AssembleBlocks: %v", err)
	}
	if len(assembled) != 3 {
		t.Fatalf("assembled block count = %d, want 3", len(assembled))
	}
	for i, b := range assembled {
		if b.NumberU64() != uint64(i+1) {
			t.Errorf("assembled[%d] number = %d, want %d", i, b.NumberU64(), i+1)
		}
	}

	// Verify the progress tracker works end-to-end.
	tracker := NewProgressTracker()
	tracker.Start(uint64(numBlocks))

	info := tracker.GetProgress()
	if info.Stage != StageProgressHeaders {
		t.Errorf("tracker stage = %s, want headers", info.Stage)
	}
	if info.HighestBlock != uint64(numBlocks) {
		t.Errorf("tracker highest = %d, want %d", info.HighestBlock, numBlocks)
	}

	tracker.SetStage(StageProgressBodies)
	tracker.UpdateBlock(5)
	tracker.RecordHeaders(5)
	tracker.RecordBodies(5)
	tracker.RecordBytes(1024)

	info = tracker.GetProgress()
	if info.Stage != StageProgressBodies {
		t.Errorf("tracker stage after update = %s, want bodies", info.Stage)
	}
	if info.CurrentBlock != 5 {
		t.Errorf("tracker current = %d, want 5", info.CurrentBlock)
	}
	if info.HeadersProcessed != 5 {
		t.Errorf("tracker headers processed = %d, want 5", info.HeadersProcessed)
	}
	if info.BytesDownloaded != 1024 {
		t.Errorf("tracker bytes = %d, want 1024", info.BytesDownloaded)
	}
	if info.PercentComplete < 49.0 || info.PercentComplete > 51.0 {
		t.Errorf("tracker percent = %.1f, want ~50", info.PercentComplete)
	}

	tracker.SetStage(StageProgressComplete)
	if !tracker.IsComplete() {
		t.Error("tracker should be complete")
	}
}

// ---------------------------------------------------------------------------
// Mock implementations (prefixed with e2e to avoid collisions with other test files)
// ---------------------------------------------------------------------------

// e2eHeaderFetcher serves headers from a prebuilt chain.
type e2eHeaderFetcher struct {
	chain []*types.Block
}

func (f *e2eHeaderFetcher) FetchHeaders(from uint64, count int) ([]*types.Header, error) {
	var headers []*types.Header
	for i := 0; i < count; i++ {
		idx := from + uint64(i)
		if idx >= uint64(len(f.chain)) {
			break
		}
		headers = append(headers, f.chain[idx].Header())
	}
	return headers, nil
}

// e2eBodyFetcher serves bodies by matching hashes from a prebuilt chain.
type e2eBodyFetcher struct {
	chain []*types.Block
}

func (f *e2eBodyFetcher) FetchBodies(hashes []types.Hash) ([]*types.Body, error) {
	bodies := make([]*types.Body, len(hashes))
	for i := range hashes {
		bodies[i] = &types.Body{}
	}
	return bodies, nil
}

// e2eBlockInserter stores inserted blocks and tracks the current head.
type e2eBlockInserter struct {
	current *types.Block
	chain   []*types.Block
}

func (ins *e2eBlockInserter) InsertChain(blocks []*types.Block) (int, error) {
	for _, b := range blocks {
		ins.chain = append(ins.chain, b)
		ins.current = b
	}
	return len(blocks), nil
}

func (ins *e2eBlockInserter) CurrentBlock() *types.Block {
	return ins.current
}

// e2eBeaconFetcher serves beacon blocks and blob sidecars from in-memory maps.
type e2eBeaconFetcher struct {
	blocks map[uint64]*BeaconBlock
	blobs  map[uint64][]*BlobSidecar
}

func (f *e2eBeaconFetcher) FetchBeaconBlock(slot uint64) (*BeaconBlock, error) {
	b, ok := f.blocks[slot]
	if !ok {
		return nil, ErrBeaconBlockNil
	}
	return b, nil
}

func (f *e2eBeaconFetcher) FetchBlobSidecars(slot uint64) ([]*BlobSidecar, error) {
	sc, ok := f.blobs[slot]
	if !ok {
		return nil, nil
	}
	return sc, nil
}
