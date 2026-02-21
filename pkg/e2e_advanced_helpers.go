// e2e_advanced_helpers.go provides helper functions for advanced roadmap
// integration tests (e2e_advanced_test.go). These helpers create pre-configured
// components for finality, bandwidth enforcement, migration, attack recovery,
// distributed building, and gas futures.
package e2e

import (
	"math/big"

	"github.com/eth2028/eth2028/consensus"
	"github.com/eth2028/eth2028/core"
	"github.com/eth2028/eth2028/core/state"
	"github.com/eth2028/eth2028/core/types"
	"github.com/eth2028/eth2028/crypto"
	"github.com/eth2028/eth2028/das"
	"github.com/eth2028/eth2028/engine"
	"github.com/eth2028/eth2028/rollup"
	"github.com/eth2028/eth2028/txpool"
)

// ---------------------------------------------------------------------------
// FinalityPipeline helpers
// ---------------------------------------------------------------------------

// MockBlockExecutor implements consensus.FPBlockExecutor for testing.
type MockBlockExecutor struct {
	StateRoot types.Hash
	Err       error
}

func (m *MockBlockExecutor) ExecuteBlock(slot uint64, blockRoot types.Hash) (types.Hash, error) {
	if m.Err != nil {
		return types.Hash{}, m.Err
	}
	if m.StateRoot == (types.Hash{}) {
		return crypto.Keccak256Hash(blockRoot[:], []byte{byte(slot)}), nil
	}
	return m.StateRoot, nil
}

// MockProofValidator implements consensus.FPProofValidator for testing.
type MockProofValidator struct {
	Valid bool
}

func (m *MockProofValidator) ValidateProof(blockRoot, stateRoot types.Hash, proofData []byte) bool {
	return m.Valid
}

// AlwaysValidBLS is a BLS backend that always passes verification.
// This is used in tests because the pure-Go BLS backend lacks real
// pairing operations for correct signature verification.
type AlwaysValidBLS struct{}

func (b *AlwaysValidBLS) Name() string                                           { return "test-always-valid" }
func (b *AlwaysValidBLS) Verify(pubkey, msg, sig []byte) bool                    { return true }
func (b *AlwaysValidBLS) AggregateVerify(pubkeys, msgs [][]byte, sig []byte) bool { return true }
func (b *AlwaysValidBLS) FastAggregateVerify(pubkeys [][]byte, msg, sig []byte) bool { return true }

// MakeFinalityPipeline creates a FinalityPipeline with mock executor and prover.
// Uses an always-valid BLS backend since the pure-Go BLS lacks real pairing.
func MakeFinalityPipeline(numValidators int, stakePerValidator uint64) (*consensus.FinalityPipeline, *consensus.EndgameEngine) {
	cfg := consensus.DefaultFPConfig()
	cfg.SkipExecution = true // skip real execution in tests

	engineCfg := consensus.DefaultEndgameEngineConfig()
	eng := consensus.NewEndgameEngine(engineCfg)

	weights := make(map[uint64]uint64, numValidators)
	for i := 0; i < numValidators; i++ {
		weights[uint64(i)] = stakePerValidator
	}
	eng.SetValidatorSet(weights)

	bls := &AlwaysValidBLS{}
	exec := &MockBlockExecutor{}
	prover := &MockProofValidator{Valid: true}

	fp, _ := consensus.NewFinalityPipeline(cfg, eng, bls, exec, prover)
	return fp, eng
}

// MakeFPVote creates a signed FPVote using the pure-Go BLS backend.
// Uses a known secret key for deterministic signatures.
func MakeFPVote(slot, validatorIdx, weight uint64, blockHash types.Hash) *consensus.FPVote {
	secret := big.NewInt(int64(validatorIdx + 1))
	pubkey := crypto.BLSPubkeyFromSecret(secret)
	signingData := crypto.Keccak256(blockHash[:], []byte{byte(slot), byte(validatorIdx)})
	sig := crypto.BLSSign(secret, signingData)
	return &consensus.FPVote{
		Slot:           slot,
		ValidatorIndex: validatorIdx,
		BlockHash:      blockHash,
		Weight:         weight,
		Pubkey:         pubkey,
		Signature:      sig,
		SigningData:     signingData,
	}
}

// ---------------------------------------------------------------------------
// Bandwidth enforcer helpers
// ---------------------------------------------------------------------------

// MakeTestBandwidthEnforcer creates a BandwidthEnforcer with test config.
func MakeTestBandwidthEnforcer(globalCap, defaultQuota uint64) *das.BandwidthEnforcer {
	cfg := das.DefaultBandwidthConfig()
	cfg.GlobalCapBytesPerSec = globalCap
	cfg.DefaultChainQuota = defaultQuota
	be, _ := das.NewBandwidthEnforcer(cfg)
	return be
}

// ---------------------------------------------------------------------------
// MigrationScheduler helpers
// ---------------------------------------------------------------------------

// MakeTestMigrationScheduler creates a scheduler with the v1->v2 migration.
func MakeTestMigrationScheduler(batchSize int) (*state.StateMigrationScheduler, error) {
	cfg := state.DefaultSchedulerConfig()
	cfg.BatchSize = batchSize
	sched, err := state.NewStateMigrationScheduler(cfg)
	if err != nil {
		return nil, err
	}
	migrations := state.DefaultVersionedMigrations()
	for _, m := range migrations {
		if regErr := sched.RegisterMigration(m); regErr != nil {
			return nil, regErr
		}
	}
	return sched, nil
}

// MakeTestMemoryStateDB creates a MemoryStateDB with pre-populated accounts.
func MakeTestMemoryStateDB(numAccounts int) *state.MemoryStateDB {
	db := state.NewMemoryStateDB()
	for i := 0; i < numAccounts; i++ {
		addr := types.BytesToAddress([]byte{byte(i + 1), byte(i>>8 + 1)})
		db.CreateAccount(addr)
		db.AddBalance(addr, big.NewInt(int64(1000+i)))
		db.SetNonce(addr, uint64(i))
		// Set a legacy slot for migration testing.
		legacySlot := types.BytesToHash([]byte{0x01})
		db.SetState(addr, legacySlot, types.BytesToHash([]byte{byte(i + 1)}))
	}
	return db
}

// ---------------------------------------------------------------------------
// Attack scenario helpers
// ---------------------------------------------------------------------------

// SimulateAttack runs a reorg detection and builds a recovery plan.
func SimulateAttack(reorgDepth, finalizedEpoch, currentEpoch uint64) (
	*consensus.AttackReport, *consensus.RecoveryPlan, error,
) {
	detector := consensus.NewAttackDetector()
	report := detector.DetectAttack(reorgDepth, finalizedEpoch, currentEpoch)
	if !report.Detected {
		return report, nil, nil
	}
	plan, err := consensus.BuildRecoveryPlan(report)
	if err != nil {
		return report, nil, err
	}
	return report, plan, nil
}

// ExecuteAttackRecovery runs the full attack detection + recovery flow.
func ExecuteAttackRecovery(reorgDepth, finalizedEpoch, currentEpoch uint64) (
	*consensus.AttackDetector, *consensus.AttackReport, *consensus.RecoveryPlan, error,
) {
	detector := consensus.NewAttackDetector()
	report := detector.DetectAttack(reorgDepth, finalizedEpoch, currentEpoch)
	if !report.Detected {
		return detector, report, nil, nil
	}
	plan, err := consensus.BuildRecoveryPlan(report)
	if err != nil {
		return detector, report, nil, err
	}
	err = detector.ExecuteRecovery(plan)
	return detector, report, plan, err
}

// ---------------------------------------------------------------------------
// Distributed builder helpers
// ---------------------------------------------------------------------------

// MakeBuilderBidForSlot creates a builder bid with the given parameters.
func MakeBuilderBidForSlot(builderID types.Hash, slot, value uint64) *engine.BuilderBid {
	return &engine.BuilderBid{
		BuilderID: builderID,
		Slot:      slot,
		BlockHash: crypto.Keccak256Hash(builderID[:], []byte{byte(slot)}),
		Value:     big.NewInt(int64(value)),
		Payload:   []byte{byte(slot), byte(value)},
	}
}

// MakeBuilderNetwork creates a BuilderNetwork with pre-registered builders.
func MakeBuilderNetwork(numBuilders int) *engine.BuilderNetwork {
	cfg := engine.DefaultBuilderConfig()
	cfg.MaxBuilders = numBuilders + 10
	bn := engine.NewBuilderNetwork(cfg)
	for i := 0; i < numBuilders; i++ {
		id := DeterministicHash(uint64(i + 100))
		addr := DeterministicAddress(byte(i + 1))
		bn.RegisterBuilder(id, addr, big.NewInt(32_000_000_000))
	}
	return bn
}

// ---------------------------------------------------------------------------
// Gas futures helpers
// ---------------------------------------------------------------------------

// MakeGasFuture creates a gas future in the market and returns the future ID.
func MakeGasFuture(market *core.GasFuturesMarket, expiryBlock uint64, strikePrice int64, volume uint64) *core.GasFuture {
	long := DeterministicAddress(0x01)
	short := DeterministicAddress(0x02)
	return market.CreateGasFuture(expiryBlock, big.NewInt(strikePrice), volume, long, short)
}

// ---------------------------------------------------------------------------
// Sharded mempool helpers
// ---------------------------------------------------------------------------

// MakeShardedPool creates a ShardedPool with given config.
func MakeShardedPool(numShards uint32, capacity int) *txpool.ShardedPool {
	cfg := txpool.ShardConfig{
		NumShards:     numShards,
		ShardCapacity: capacity,
	}
	return txpool.NewShardedPool(cfg)
}

// ---------------------------------------------------------------------------
// Native rollup helpers
// ---------------------------------------------------------------------------

// MakeAnchorUpdate creates an anchor state update for testing.
func MakeAnchorUpdate(blockNum uint64) rollup.AnchorState {
	blockHash := DeterministicHash(blockNum)
	stateRoot := DeterministicHash(blockNum + 10000)
	return rollup.AnchorState{
		LatestBlockHash: blockHash,
		LatestStateRoot: stateRoot,
		BlockNumber:     blockNum,
		Timestamp:       1700000000 + blockNum,
	}
}

// ---------------------------------------------------------------------------
// Cell gossip helpers
// ---------------------------------------------------------------------------

// MakeCellGossipMessage creates a cell gossip message for testing.
func MakeCellGossipMessage(blobIdx, cellIdx int, slot uint64, data []byte) das.CellGossipMessage {
	return das.CellGossipMessage{
		BlobIndex: blobIdx,
		CellIndex: cellIdx,
		Data:      data,
		Slot:      slot,
	}
}

// ---------------------------------------------------------------------------
// Endgame state helpers
// ---------------------------------------------------------------------------

// MakeEndgameStateDB creates a new EndgameStateDB wrapping a MemoryStateDB.
func MakeEndgameStateDB() (*state.EndgameStateDB, *state.MemoryStateDB) {
	memDB := state.NewMemoryStateDB()
	endgame, _ := state.NewEndgameStateDB(memDB)
	return endgame, memDB
}

// ---------------------------------------------------------------------------
// Secret proposer helpers
// ---------------------------------------------------------------------------

// MakeVRFElectionEntries creates VRF election entries for numValidators.
func MakeVRFElectionEntries(numValidators int, epoch, slot uint64) []*consensus.VRFElectionEntry {
	input := consensus.ComputeVRFElectionInput(epoch, slot)
	entries := make([]*consensus.VRFElectionEntry, numValidators)
	for i := 0; i < numValidators; i++ {
		kp := consensus.GenerateVRFKeyPair([]byte{byte(i), byte(i + 1)})
		output, proof := consensus.VRFProve(kp.SecretKey, input)
		entries[i] = &consensus.VRFElectionEntry{
			ValidatorIndex: uint64(i),
			Epoch:          epoch,
			Slot:           slot,
			Output:         output,
			Proof:          proof,
			Score:          consensus.ComputeProposerScore(output),
		}
	}
	return entries
}
