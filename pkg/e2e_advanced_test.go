package e2e_test

import (
	"bytes"
	"math/big"
	"testing"

	e2e "github.com/eth2030/eth2030"
	"github.com/eth2030/eth2030/consensus"
	"github.com/eth2030/eth2030/core"
	"github.com/eth2030/eth2030/core/state"
	"github.com/eth2030/eth2030/core/types"
	"github.com/eth2030/eth2030/crypto/pqc"
	"github.com/eth2030/eth2030/das"
	"github.com/eth2030/eth2030/engine"
	"github.com/eth2030/eth2030/rollup"
	"github.com/eth2030/eth2030/txpool"
)

// =====================================================================
// Consensus Advanced
// =====================================================================

func TestE2EAdv_FinalityPipelineEndToEnd(t *testing.T) {
	// Create pipeline with 10 validators, each with weight 100.
	fp, eng := e2e.MakeFinalityPipeline(10, 100)
	if fp == nil || eng == nil {
		t.Fatal("failed to create finality pipeline")
	}

	// Submit votes from validators 0..6 (7 votes = 700/1000 = 70% > 66.7%).
	blockHash := e2e.DeterministicHash(42)
	slot := uint64(1)
	var finalResult *consensus.FPFinalityResult

	for i := uint64(0); i < 7; i++ {
		vote := e2e.MakeFPVote(slot, i, 100, blockHash)
		result, err := fp.SubmitVote(vote)
		if err != nil {
			t.Fatalf("SubmitVote(%d): %v", i, err)
		}
		if result != nil {
			finalResult = result
		}
	}

	if finalResult == nil {
		t.Fatal("expected finality after 7/10 votes (70%)")
	}
	if finalResult.Slot != slot {
		t.Errorf("slot: got %d, want %d", finalResult.Slot, slot)
	}
	if finalResult.BlockRoot != blockHash {
		t.Error("block root mismatch")
	}
	if finalResult.VoteCount < 7 {
		t.Errorf("vote count: got %d, want >= 7", finalResult.VoteCount)
	}
	if !fp.IsFPSlotFinalized(slot) {
		t.Error("slot should be marked as finalized")
	}
}

func TestE2EAdv_AttackRecoveryDetection(t *testing.T) {
	detector, report, plan, err := e2e.ExecuteAttackRecovery(10, 50, 60)
	if err != nil {
		t.Fatalf("ExecuteAttackRecovery: %v", err)
	}
	if !report.Detected {
		t.Fatal("expected attack to be detected at reorg depth 10")
	}
	if report.Severity != consensus.SeverityHigh {
		t.Errorf("severity: got %s, want %s", report.Severity, consensus.SeverityHigh)
	}
	if plan == nil {
		t.Fatal("expected recovery plan")
	}
	if !plan.IsolationMode {
		t.Error("expected isolation mode for high severity")
	}
	if !plan.FallbackToFinalized {
		t.Error("expected fallback to finalized for high severity")
	}
	status := detector.GetRecoveryStatus()
	if !status.Active {
		t.Error("recovery should be active")
	}
	if !status.PeersIsolated {
		t.Error("peers should be isolated")
	}
	if !status.FellBackToFinalized {
		t.Error("should have fallen back to finalized")
	}
}

func TestE2EAdv_SecretProposerElection(t *testing.T) {
	// Create VRF election entries for 10 validators.
	entries := e2e.MakeVRFElectionEntries(10, 100, 5)
	if len(entries) != 10 {
		t.Fatalf("entries: got %d, want 10", len(entries))
	}

	election := consensus.NewSecretElection()
	winner, err := election.ElectProposer(entries)
	if err != nil {
		t.Fatalf("ElectProposer: %v", err)
	}
	if winner == nil {
		t.Fatal("expected a winner")
	}

	// Submit a reveal from the winner.
	reveal := &consensus.VRFReveal{
		ValidatorIndex: winner.ValidatorIndex,
		Slot:           5,
		BlockHash:      e2e.DeterministicHash(999),
		Output:         winner.Output,
		Proof:          winner.Proof,
	}
	if err := election.SubmitReveal(reveal); err != nil {
		t.Fatalf("SubmitReveal: %v", err)
	}
	reveals := election.GetReveals(5)
	if len(reveals) != 1 {
		t.Errorf("reveals: got %d, want 1", len(reveals))
	}

	// Verify VRF proof.
	kp := consensus.GenerateVRFKeyPair([]byte{byte(winner.ValidatorIndex), byte(winner.ValidatorIndex + 1)})
	input := consensus.ComputeVRFElectionInput(100, 5)
	ok := consensus.VRFVerify(kp.PublicKey, input, winner.Output, winner.Proof)
	if !ok {
		t.Error("VRF verification failed")
	}
}

func TestE2EAdv_OneEthIncluderRegistration(t *testing.T) {
	pool := consensus.NewIncluderPool()
	addr1 := e2e.DeterministicAddress(0x10)
	addr2 := e2e.DeterministicAddress(0x20)

	// Register two includers with exactly 1 ETH.
	if err := pool.RegisterIncluder(addr1, consensus.OneETH); err != nil {
		t.Fatalf("RegisterIncluder(1): %v", err)
	}
	if err := pool.RegisterIncluder(addr2, consensus.OneETH); err != nil {
		t.Fatalf("RegisterIncluder(2): %v", err)
	}
	if pool.ActiveCount() != 2 {
		t.Errorf("active count: got %d, want 2", pool.ActiveCount())
	}

	// Select an includer for a slot.
	seed := e2e.DeterministicHash(77)
	selected, err := pool.SelectIncluder(consensus.Slot(10), seed)
	if err != nil {
		t.Fatalf("SelectIncluder: %v", err)
	}
	if selected != addr1 && selected != addr2 {
		t.Error("selected includer is not one of the registered ones")
	}

	// Slash for misbehavior.
	if err := pool.SlashIncluder(addr1, "missed duty"); err != nil {
		t.Fatalf("SlashIncluder: %v", err)
	}
	record := pool.GetIncluder(addr1)
	if record == nil {
		t.Fatal("expected slashed includer record")
	}
	if record.Status != consensus.IncluderSlashed {
		t.Errorf("status: got %s, want slashed", record.Status.String())
	}
	// Stake should be reduced by 10%.
	expected := new(big.Int).Sub(consensus.OneETH, new(big.Int).Div(consensus.OneETH, big.NewInt(10)))
	if record.Stake.Cmp(expected) != 0 {
		t.Errorf("stake: got %s, want %s", record.Stake.String(), expected.String())
	}
}

func TestE2EAdv_DistributedBlockBuilding(t *testing.T) {
	bn := e2e.MakeBuilderNetwork(3)
	if bn.ActiveBuilders() != 3 {
		t.Fatalf("builders: got %d, want 3", bn.ActiveBuilders())
	}

	// Submit bids from each builder for slot 5.
	for i := 0; i < 3; i++ {
		builderID := e2e.DeterministicHash(uint64(i + 100))
		bid := e2e.MakeBuilderBidForSlot(builderID, 5, uint64(100*(i+1)))
		if err := bn.SubmitBid(bid); err != nil {
			t.Fatalf("SubmitBid(%d): %v", i, err)
		}
	}

	winner := bn.GetWinningBid(5)
	if winner == nil {
		t.Fatal("expected a winning bid")
	}
	// Builder 2 (value=300) should win.
	if winner.Value.Int64() != 300 {
		t.Errorf("winning value: got %d, want 300", winner.Value.Int64())
	}
}

// =====================================================================
// State & Migration Advanced
// =====================================================================

func TestE2EAdv_StateMigrationV1ToV2(t *testing.T) {
	sched, err := e2e.MakeTestMigrationScheduler(5)
	if err != nil {
		t.Fatalf("MakeTestMigrationScheduler: %v", err)
	}
	db := e2e.MakeTestMemoryStateDB(10)

	// Start migration v1 -> v2.
	if err := sched.StartMigration(2, db); err != nil {
		t.Fatalf("StartMigration: %v", err)
	}
	if !sched.IsRunning() {
		t.Fatal("expected migration to be running")
	}

	// Process batches until complete.
	for sched.IsRunning() {
		progress, batchErr := sched.ProcessNextBatch(db)
		if batchErr != nil {
			t.Fatalf("ProcessNextBatch: %v", batchErr)
		}
		if progress == nil {
			break
		}
	}

	if sched.CurrentVersion() != 2 {
		t.Errorf("version: got %d, want 2", sched.CurrentVersion())
	}
	// Verify legacy slot 0x01 is cleared for the first account.
	addr := types.BytesToAddress([]byte{0x01, 0x01})
	legacySlot := types.BytesToHash([]byte{0x01})
	if db.GetState(addr, legacySlot) != (types.Hash{}) {
		t.Error("legacy slot should be cleared after v1->v2 migration")
	}
}

func TestE2EAdv_EndgameStateTracking(t *testing.T) {
	endgame, _ := e2e.MakeEndgameStateDB()

	// Add pending roots and finalize them.
	root1 := e2e.DeterministicHash(1)
	root2 := e2e.DeterministicHash(2)
	root3 := e2e.DeterministicHash(3)

	endgame.AddPendingRoot(root1, 10)
	endgame.AddPendingRoot(root2, 11)
	endgame.AddPendingRoot(root3, 12)

	if endgame.PendingCount() != 3 {
		t.Errorf("pending: got %d, want 3", endgame.PendingCount())
	}

	// Finalize root1.
	if err := endgame.MarkFinalized(root1, 10); err != nil {
		t.Fatalf("MarkFinalized: %v", err)
	}
	if !endgame.IsFinalized(root1) {
		t.Error("root1 should be finalized")
	}
	if endgame.PendingCount() != 2 {
		t.Errorf("pending after finalize: got %d, want 2", endgame.PendingCount())
	}

	// Finality digest should be non-zero.
	digest := endgame.ComputeFinalityDigest()
	if digest == (types.Hash{}) {
		t.Error("expected non-zero finality digest")
	}

	// Revert to finalized should clear pending.
	if err := endgame.RevertToFinalized(); err != nil {
		t.Fatalf("RevertToFinalized: %v", err)
	}
	if endgame.PendingCount() != 0 {
		t.Errorf("pending after revert: got %d, want 0", endgame.PendingCount())
	}
}

func TestE2EAdv_MigrationRollback(t *testing.T) {
	sched, err := e2e.MakeTestMigrationScheduler(100)
	if err != nil {
		t.Fatalf("MakeTestMigrationScheduler: %v", err)
	}
	db := e2e.MakeTestMemoryStateDB(5)

	// Capture pre-migration balance.
	addr := types.BytesToAddress([]byte{0x01, 0x01})
	preBal := new(big.Int).Set(db.GetBalance(addr))

	// Run migration.
	if err := sched.StartMigration(2, db); err != nil {
		t.Fatalf("StartMigration: %v", err)
	}
	for sched.IsRunning() {
		sched.ProcessNextBatch(db)
	}
	if sched.CurrentVersion() != 2 {
		t.Fatalf("expected v2, got v%d", sched.CurrentVersion())
	}

	// Rollback.
	if !sched.HasRollback() {
		t.Fatal("expected rollback snapshot to be available")
	}
	if err := sched.Rollback(db); err != nil {
		t.Fatalf("Rollback: %v", err)
	}
	if sched.CurrentVersion() != 1 {
		t.Errorf("version after rollback: got %d, want 1", sched.CurrentVersion())
	}
	// Balance should be restored.
	postBal := db.GetBalance(addr)
	if preBal.Cmp(postBal) != 0 {
		t.Errorf("balance mismatch: pre=%s post=%s", preBal, postBal)
	}
}

// =====================================================================
// Data Layer Advanced
// =====================================================================

func TestE2EAdv_TeragasBandwidthEnforcement(t *testing.T) {
	be := e2e.MakeTestBandwidthEnforcer(100_000, 50_000)

	// Register chains.
	be.RegisterChain(1, 0) // uses default quota
	be.RegisterChain(2, 0)

	// Request within quota should succeed.
	if err := be.RequestBandwidth(1, 1000); err != nil {
		t.Fatalf("RequestBandwidth(1, 1000): %v", err)
	}

	// Check congestion status.
	if be.IsCongested() {
		t.Error("should not be congested after a small request")
	}
	util := be.GlobalUtilization()
	if util < 0 {
		t.Error("utilization should be non-negative")
	}

	// Request from unregistered chain should fail.
	if err := be.RequestBandwidth(999, 100); err == nil {
		t.Error("expected error for unregistered chain")
	}
}

func TestE2EAdv_CustodyProofChallengeResponse(t *testing.T) {
	challenger := e2e.DeterministicAddress(0xAA)
	target := e2e.DeterministicAddress(0xBB)

	// Create a challenge for column 5, epoch 10.
	challenge, err := das.CreateChallenge(challenger, target, 5, 10, 100)
	if err != nil {
		t.Fatalf("CreateChallenge: %v", err)
	}
	if challenge.Column != 5 {
		t.Errorf("column: got %d, want 5", challenge.Column)
	}

	// Generate a valid custody proof.
	nodeID := e2e.DeterministicNodeID(42)
	data := bytes.Repeat([]byte{0xAB}, 256)
	proof := das.GenerateCustodyProof(nodeID, 10, []uint64{3, 5, 7}, data)

	// Respond to the challenge.
	ok := das.RespondToChallenge(challenge, proof)
	if !ok {
		t.Error("expected valid response to custody challenge")
	}

	// Verify proof structure.
	if !das.VerifyCustodyProof(proof) {
		t.Error("proof structural validation failed")
	}
}

func TestE2EAdv_BlobStreamingBackpressure(t *testing.T) {
	cfg := das.DefaultSessionConfig()
	cfg.MaxConcurrentStreams = 2
	manager := das.NewStreamManager(cfg)

	// Start two streams (max).
	s1, err := manager.StartStream(0, 1024)
	if err != nil {
		t.Fatalf("StartStream(0): %v", err)
	}
	_, err = manager.StartStream(1, 1024)
	if err != nil {
		t.Fatalf("StartStream(1): %v", err)
	}
	// Third should hit the limit.
	_, err = manager.StartStream(2, 1024)
	if err == nil {
		t.Error("expected error when exceeding max concurrent streams")
	}

	// Cancel the first stream to make room.
	manager.CancelStream(s1.ID)

	// Now a new stream should work.
	_, err = manager.StartStream(2, 1024)
	if err != nil {
		t.Fatalf("StartStream(2) after cancellation: %v", err)
	}
}

func TestE2EAdv_CellGossipPropagation(t *testing.T) {
	handler := das.NewCellGossipHandler(das.CellGossipHandlerConfig{
		ReconstructionThreshold: 3,
		MaxBlobIndex:            6,
		MaxCellIndex:            128,
	})

	// Send cells for blob 0.
	for i := 0; i < 3; i++ {
		msg := e2e.MakeCellGossipMessage(0, i, 100, bytes.Repeat([]byte{byte(i)}, 32))
		if err := handler.OnCellReceived(msg); err != nil {
			t.Fatalf("OnCellReceived(%d): %v", i, err)
		}
	}

	// Blob should be ready for reconstruction.
	if !handler.CheckReconstructionReady(0) {
		t.Error("blob 0 should be ready after 3 cells")
	}

	// Broadcast a cell.
	broadcastMsg := e2e.MakeCellGossipMessage(1, 0, 100, bytes.Repeat([]byte{0xCC}, 32))
	if err := handler.BroadcastCell(broadcastMsg); err != nil {
		t.Fatalf("BroadcastCell: %v", err)
	}

	queued := handler.DrainBroadcastQueue()
	if len(queued) != 1 {
		t.Errorf("broadcast queue: got %d, want 1", len(queued))
	}

	stats := handler.Stats()
	if stats.CellsReceived != 3 {
		t.Errorf("CellsReceived: got %d, want 3", stats.CellsReceived)
	}
}

// =====================================================================
// Execution Advanced
// =====================================================================

func TestE2EAdv_PayloadChunking(t *testing.T) {
	// Create a large payload.
	payload := make([]byte, 500_000) // 500 KB
	for i := range payload {
		payload[i] = byte(i * 3)
	}

	// Chunk it.
	chunks := core.ChunkPayload(payload, 0) // default 128KB chunks
	if len(chunks) < 4 {
		t.Fatalf("chunks: got %d, want >= 4", len(chunks))
	}

	// Validate each chunk.
	for i, c := range chunks {
		if err := core.ValidateChunk(c); err != nil {
			t.Fatalf("ValidateChunk(%d): %v", i, err)
		}
	}

	// Reassemble.
	reassembled, err := core.ReassemblePayload(chunks)
	if err != nil {
		t.Fatalf("ReassemblePayload: %v", err)
	}
	if !bytes.Equal(payload, reassembled) {
		t.Error("reassembled payload does not match original")
	}
}

func TestE2EAdv_BlockInBlobsEncoding(t *testing.T) {
	encoder := &core.BlobEncoder{}

	// Simulate block RLP data.
	blockRLP := make([]byte, 1000)
	for i := range blockRLP {
		blockRLP[i] = byte(i % 251) // quasi-random
	}

	blobs, err := encoder.EncodeBlockToBlobs(blockRLP)
	if err != nil {
		t.Fatalf("EncodeBlockToBlobs: %v", err)
	}
	if len(blobs) == 0 {
		t.Fatal("expected at least one blob")
	}

	decoded, err := encoder.DecodeBlobsToBlock(blobs)
	if err != nil {
		t.Fatalf("DecodeBlobsToBlock: %v", err)
	}
	if !bytes.Equal(blockRLP, decoded) {
		t.Error("decoded block RLP does not match original")
	}
}

func TestE2EAdv_NativeRollupAnchor(t *testing.T) {
	anchor := rollup.NewAnchorContract()

	// Update anchor with sequential blocks.
	for i := uint64(1); i <= 5; i++ {
		update := e2e.MakeAnchorUpdate(i)
		if err := anchor.UpdateState(update); err != nil {
			t.Fatalf("UpdateState(%d): %v", i, err)
		}
	}

	latest := anchor.GetLatestState()
	if latest.BlockNumber != 5 {
		t.Errorf("latest block: got %d, want 5", latest.BlockNumber)
	}

	// Query historical anchor.
	entry, found := anchor.GetAnchorByNumber(3)
	if !found {
		t.Fatal("expected anchor for block 3")
	}
	expected := e2e.DeterministicHash(3)
	if entry.BlockHash != expected {
		t.Error("anchor block hash mismatch")
	}

	// Stale block should fail.
	stale := e2e.MakeAnchorUpdate(3)
	if err := anchor.UpdateState(stale); err == nil {
		t.Error("expected error for stale block update")
	}
}

func TestE2EAdv_GasFuturesMarket(t *testing.T) {
	market := core.NewGasFuturesMarket()

	// Create a futures contract: strike = 50 gwei, volume = 1000.
	future := e2e.MakeGasFuture(market, 100, 50, 1000)
	if future == nil {
		t.Fatal("expected non-nil future")
	}
	if market.OpenContractCount() != 1 {
		t.Errorf("open contracts: got %d, want 1", market.OpenContractCount())
	}

	// Settle at a higher gas price (Long wins).
	settlement, err := market.SettleGasFuture(future.ID, big.NewInt(80))
	if err != nil {
		t.Fatalf("SettleGasFuture: %v", err)
	}
	if settlement.Winner != future.Long {
		t.Error("expected Long to win when actual > strike")
	}
	// Payout = |80-50| * 1000 = 30,000.
	expectedPayout := big.NewInt(30_000)
	if settlement.Payout.Cmp(expectedPayout) != 0 {
		t.Errorf("payout: got %s, want %s", settlement.Payout, expectedPayout)
	}
	if market.SettledContractCount() != 1 {
		t.Errorf("settled contracts: got %d, want 1", market.SettledContractCount())
	}

	// Price estimation: use larger values to avoid integer truncation.
	// price = strikePrice * blocksToExpiry * volatility / 10_000_000
	// With strikePrice=50000, blocks=90, volatility=5000:
	// 50000 * 90 * 5000 / 10_000_000 = 2,250
	price := core.PriceGasFuture(10, 100, big.NewInt(50000), 5000)
	if price.Sign() <= 0 {
		t.Errorf("expected positive price for active future, got %s", price)
	}
}

// =====================================================================
// Cross-Layer Advanced
// =====================================================================

func TestE2EAdv_FullConsensusEngineLoop(t *testing.T) {
	// Consensus layer: EndgameEngine with 5 validators.
	engCfg := consensus.DefaultEndgameEngineConfig()
	eng := consensus.NewEndgameEngine(engCfg)
	weights := map[uint64]uint64{0: 100, 1: 100, 2: 100, 3: 100, 4: 100}
	eng.SetValidatorSet(weights)

	blockHash := e2e.DeterministicHash(42)
	slot := uint64(1)

	// Submit votes from 4/5 validators (80% > 66.7%).
	for i := uint64(0); i < 4; i++ {
		vote := &consensus.EndgameVote{
			Slot: slot, ValidatorIndex: i,
			BlockHash: blockHash, Weight: 100,
			Timestamp: 1000 + i,
		}
		if err := eng.SubmitVote(vote); err != nil {
			t.Fatalf("SubmitVote(%d): %v", i, err)
		}
	}

	// Check finality.
	result := eng.CheckFinality(slot)
	if !result.IsFinalized {
		t.Fatal("expected finality after 4/5 votes")
	}
	if result.FinalizedHash != blockHash {
		t.Error("finalized hash mismatch")
	}

	// Engine layer: build payload chunks.
	payload := make([]byte, 200_000)
	chunks := core.ChunkPayload(payload, 0)
	if len(chunks) < 2 {
		t.Error("expected multiple chunks for 200KB payload")
	}

	// Verify finalized chain.
	chain := eng.GetFinalizedChain()
	if len(chain) == 0 {
		t.Error("expected non-empty finalized chain")
	}
}

func TestE2EAdv_PQRegistryGasAccounting(t *testing.T) {
	// Use the global PQ algorithm registry which has defaults registered.
	registry := pqc.GlobalRegistry()
	if registry == nil {
		t.Fatal("expected non-nil global registry")
	}

	// ML-DSA-65 should be pre-registered as MLDSA65 (type 2).
	desc, err := registry.GetAlgorithm(pqc.MLDSA65)
	if err != nil {
		t.Fatalf("GetAlgorithm(MLDSA65): %v", err)
	}
	if desc.Name == "" {
		t.Error("expected non-empty algorithm name")
	}

	// Gas cost for ML-DSA-65 verification.
	gasCost, err := registry.GasCost(pqc.MLDSA65)
	if err != nil {
		t.Fatalf("GasCost: %v", err)
	}
	if gasCost == 0 {
		t.Error("expected non-zero gas cost for ML-DSA-65")
	}

	// Compute total gas for a PQ transaction.
	baseCost := uint64(21000)
	sigGas := uint64(desc.SigSize) * 16 // 16 gas per byte of calldata
	totalGas := baseCost + sigGas + gasCost
	if totalGas <= baseCost {
		t.Error("expected additional gas for PQ signature")
	}

	// Check that we have multiple supported algorithms.
	supported := registry.SupportedAlgorithms()
	if len(supported) < 2 {
		t.Errorf("supported algorithms: got %d, want >= 2", len(supported))
	}
}

func TestE2EAdv_ShardedMempoolRouting(t *testing.T) {
	pool := e2e.MakeShardedPool(8, 1024)

	// Create transactions and verify shard assignment.
	sender := e2e.DeterministicAddress(0x01)
	receiver := e2e.DeterministicAddress(0x02)
	var addedTxs []*types.Transaction

	for i := uint64(0); i < 20; i++ {
		tx := e2e.MakeLegacyTx(sender, receiver, i, 100)
		if err := pool.AddTx(tx); err != nil {
			t.Fatalf("AddTx(%d): %v", i, err)
		}
		addedTxs = append(addedTxs, tx)
	}

	if pool.Count() != 20 {
		t.Errorf("pool count: got %d, want 20", pool.Count())
	}

	// Verify deterministic shard assignment.
	for _, tx := range addedTxs {
		hash := tx.Hash()
		shard := pool.ShardForTx(hash)
		if shard >= 8 {
			t.Errorf("shard %d out of range [0, 8)", shard)
		}
		// Verify the transaction is retrievable.
		retrieved := pool.GetTx(hash)
		if retrieved == nil {
			t.Errorf("tx %x not found in pool", hash[:4])
		}
	}

	// Check shard stats.
	stats := pool.GetShardStats()
	if len(stats) != 8 {
		t.Errorf("shard stats: got %d shards, want 8", len(stats))
	}
	totalFromStats := 0
	for _, s := range stats {
		totalFromStats += s.TxCount
	}
	if totalFromStats != 20 {
		t.Errorf("total from stats: got %d, want 20", totalFromStats)
	}
}

func TestE2EAdv_TechDebtFieldDeprecation(t *testing.T) {
	// Create a state with legacy fields, run the migration scheduler.
	db := e2e.MakeTestMemoryStateDB(3)

	// Verify legacy slot exists before migration.
	addr := types.BytesToAddress([]byte{0x01, 0x01})
	legacySlot := types.BytesToHash([]byte{0x01})
	val := db.GetState(addr, legacySlot)
	if val == (types.Hash{}) {
		t.Fatal("expected legacy slot to have a value before migration")
	}

	// Create scheduler and run v1->v2 migration.
	sched, err := e2e.MakeTestMigrationScheduler(100)
	if err != nil {
		t.Fatalf("MakeTestMigrationScheduler: %v", err)
	}
	if err := sched.StartMigration(2, db); err != nil {
		t.Fatalf("StartMigration: %v", err)
	}
	for sched.IsRunning() {
		if _, err := sched.ProcessNextBatch(db); err != nil {
			t.Fatalf("ProcessNextBatch: %v", err)
		}
	}

	// Legacy slot should be removed (migrated to 0x10).
	if db.GetState(addr, legacySlot) != (types.Hash{}) {
		t.Error("legacy slot 0x01 should be cleared after migration")
	}
	newSlot := types.BytesToHash([]byte{0x10})
	if db.GetState(addr, newSlot) == (types.Hash{}) {
		t.Error("new slot 0x10 should have been populated by migration")
	}
}

// Ensure unused imports are satisfied.
var _ = engine.DefaultBuilderConfig
var _ = state.NewMemoryStateDB
var _ = txpool.DefaultShardConfig
