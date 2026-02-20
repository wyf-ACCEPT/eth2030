# eth2028 Gap Analysis vs L1 Strawmap Roadmap

Last updated: 2026-02-20

## Summary

Systematic audit of all 65 roadmap items across Consensus, Data, and Execution layers.
- **COMPLETE**: 12 items
- **FUNCTIONAL**: 33 items
- **PARTIAL**: 17 items
- **STUB**: 1 item (PQC primitives)
- **MISSING**: 3 items (jeanVM, PQ available chain, exposed zkISA)

---

## Consensus Layer

| # | Item | Status | Key Files | Notes |
|---|------|--------|-----------|-------|
| 1 | fast confirmation | FUNCTIONAL | `consensus/fast_confirm.go` | Quorum-based tracker, no BLS sig verification wired |
| 2 | quick slots (6-sec) | FUNCTIONAL | `consensus/quick_slots.go` | Scheduler with correct timing math |
| 3 | 1-epoch finality | FUNCTIONAL | `consensus/finality.go` | `singleEpochMode` plugged into FinalityTracker |
| 4 | 4-slot epochs | FUNCTIONAL | `consensus/quick_slots.go` | `QuickSlotConfig{SlotsPerEpoch: 4}` |
| 5 | endgame finality | PARTIAL | `consensus/endgame_finality.go`, `endgame_engine*.go` | BLS aggregate sig is `[96]byte` placeholder |
| 6 | fast L1 finality in seconds | PARTIAL | `consensus/endgame_engine.go` | TargetFinalityMs=500; no real BLS or block execution |
| 7 | modernized beacon specs | PARTIAL | `consensus/modern_beacon.go`, `beacon_state_v2.go`, `bsn_beacon_state.go` | 4 parallel implementations, no single merged spec |
| 8 | attester stake cap | FUNCTIONAL | `consensus/attester_cap.go` + `attester_cap_extended.go` | Cap enforcement well-implemented |
| 9 | 128K attester cap | FUNCTIONAL | `consensus/attester_cap.go` | ~125K attesters with 128 ETH cap |
| 10 | APS (committee selection) | FUNCTIONAL | `consensus/aps.go` | DutyScheduler assigns attester/proposer roles |
| 11 | 1 ETH includers | FUNCTIONAL | `consensus/one_eth_includers.go` | Registration, slashing, pseudorandom selection |
| 12 | tech debt reset | PARTIAL | `consensus/tech_debt_reset.go`, `core/state/tech_debt_reset.go` | Tracks deprecated fields only |
| 13 | PQ attestations | PARTIAL | `consensus/pq_attestation.go` | Type + verifier wired to `crypto/pqc`, but PQC is stub |
| 14 | **jeanVM aggregation** | **MISSING** | None | No file matching "jeanVM" anywhere; research-stage item |
| 15 | 1M attestations/slot | FUNCTIONAL | `consensus/attestation_scaler.go`, `parallel_bls.go`, `committee_subnet.go`, `batch_verifier.go` | Full parallel pipeline |
| 16 | 51% attack auto-recovery | FUNCTIONAL | `consensus/attack_recovery.go` | Severity classification + recovery plans |
| 17 | distributed block building | FUNCTIONAL | `consensus/dist_builder.go`, `engine/distributed_builder.go` | Bid/fragment merging, auction |
| 18 | VDF randomness | FUNCTIONAL | `crypto/vdf.go`, `consensus/vdf_consensus.go` | Wesolowski protocol with RSA squaring |
| 19 | secret proposers | FUNCTIONAL | `consensus/secret_proposer.go`, `consensus/vrf_election.go` | Commit/reveal with VRF election |
| 20 | PQ pubkey registry | PARTIAL | `crypto/pqc/pubkey_registry.go` | Registry logic solid; Dilithium/Falcon are stubs |
| 21 | **PQ available chain** | **MISSING** | None | No PQ-secure fork choice / block hashing / chain history |
| 22 | real-time CL proofs | PARTIAL | `light/cl_proofs.go`, `light/proof_generator.go` | STARK/SNARK types defined but all proofs simulated |

## Data Layer

| # | Item | Status | Key Files | Notes |
|---|------|--------|-----------|-------|
| 23 | sparse blobpool | COMPLETE | `das/sparse_blobpool.go`, `txpool/sparse_blobpool.go` | WAL, custody, price eviction |
| 24 | cell-level messages | FUNCTIONAL | `das/cell_messages.go`, `das/cell_gossip.go` | Full codec; handler |
| 25 | EIP-7702 broadcast | PARTIAL | `p2p/eip2p_broadcast.go` | General enhanced fanout, not SetCode-specific |
| 26 | BPO blobs increase | FUNCTIONAL | `das/rpo_slots.go`, `core/blob_schedule.go` | BPO1/BPO2 schedules |
| 27 | local blob reconstruction | FUNCTIONAL | `das/blob_reconstruct.go`, `das/erasure/` | Reed-Solomon + Lagrange |
| 28 | decrease sample size | FUNCTIONAL | `das/sample_optimize.go` | Adaptive optimizer with security parameter math |
| 29 | blob streaming | FUNCTIONAL | `das/streaming.go`, `das/streaming_proto.go` | Chunk-based with backpressure |
| 30 | short-dated blob futures | FUNCTIONAL | `das/blob_futures.go`, `das/futures_market.go` | Futures contracts with expiry |
| 31 | PQ blobs | PARTIAL | `das/pq_blobs.go`, `crypto/pqc/lattice_commit.go`, `batch_blob_verify.go` | Lattice commit works but underlying crypto stubs |
| 32 | variable-size blobs | FUNCTIONAL | `das/varblob.go`, `das/variable_blobs.go` | Two complementary implementations |
| 33 | proofs of custody | FUNCTIONAL | `das/custody_proof.go`, `das/custody_verify.go` | Challenge/response system |
| 34 | teragas L2 | PARTIAL | `das/teradata.go`, `das/bandwidth_enforcer.go`, `das/stream_pipeline.go` | Accounting only, no real bandwidth enforcement |

## Execution Layer

| # | Item | Status | Key Files | Notes |
|---|------|--------|-----------|-------|
| 35 | Glamsterdam repricing | COMPLETE | `core/glamsterdam_repricing.go` | 18-EIP repricing table |
| 36 | optional proofs | FUNCTIONAL | `proofs/optional.go` | Policy-based optional proof store |
| 37 | Hogota repricing | FUNCTIONAL | `core/hogota_repricing.go` | Repricing tables with tests |
| 38 | 3x/year gas limit | FUNCTIONAL | `core/gas_limit.go` | Schedule-based gas limit increase |
| 39 | multidimensional pricing | FUNCTIONAL | `core/multidim_gas.go`, `core/multidim.go` | EIP-7706/7999 |
| 40 | payload chunking | FUNCTIONAL | `core/payload_chunking.go`, `engine/payload_chunking.go` | Chunked payload assembly |
| 41 | block in blobs | FUNCTIONAL | `core/block_in_blobs.go`, `das/block_in_blob.go` | Block-as-blob encoding |
| 42 | announce nonce | FUNCTIONAL | `p2p/announce_nonce.go`, `eth/announce_nonce.go` | ETH/72 nonce announcement |
| 43 | mandatory 3-of-5 proofs | FUNCTIONAL | `proofs/mandatory_proofs.go`, `proofs/mandatory.go` | Prover assignment, submission, validation |
| 44 | canonical guest | PARTIAL | `zkvm/canonical.go` | GuestRegistry exists; Execute() is stub (keccak hash) |
| 45 | canonical zkVM | PARTIAL | `zkvm/riscv_cpu.go`, `riscv_memory.go`, `proof_backend.go` | RV32IM works; proof backend uses MockVerifier |
| 46 | long-dated gas futures | FUNCTIONAL | `core/vm/gas_futures_long.go`, `core/gas_market.go` | Full position/margin/liquidation cycle |
| 47 | sharded mempool | FUNCTIONAL | `txpool/sharding.go`, `txpool/shared/` | Consistent hash sharding + bloom dedup |
| 48 | gigagas L1 | PARTIAL | `core/gigagas.go`, `core/work_stealing.go`, `core/rate_meter.go` | Infrastructure exists; not wired to live chain |
| 49 | BALs | COMPLETE | `bal/` | Full Block Access List (EIP-7928) |
| 50 | binary tree | COMPLETE | `trie/bintrie/` | SHA-256 binary Merkle trie with proofs |
| 51 | validity-only partial state | FUNCTIONAL | `core/vops/` | Executor, validator, complete validator |
| 52 | endgame state | FUNCTIONAL | `core/state/endgame_state.go` | SSF-aware root tracker with snapshot reversion |
| 53 | native AA | FUNCTIONAL | `core/vm/aa_executor.go`, `core/aa_entrypoint.go` | EIP-7701/7702 |
| 54 | misc purges | FUNCTIONAL | `core/state/misc_purges.go` | SELFDESTRUCT, empty-account, storage purges |
| 55 | transaction assertions | FUNCTIONAL | `core/tx_assertions.go`, `core/types/` | Tx assertions support |
| 56 | NTT precompile | FUNCTIONAL | `core/vm/precompile_ntt.go` | Number Theoretic Transform |
| 57 | precompiles in zkISA | FUNCTIONAL | `core/vm/zkisa_precompiles.go` | ExecutionProof-producing wrappers |
| 58 | STF in zkISA | PARTIAL | `zkvm/stf.go` | Types + proof size checks; proof generation simulated |
| 59 | native rollups | FUNCTIONAL | `rollup/` | EIP-8079 EXECUTE precompile + anchor contract |
| 60 | proof aggregation | FUNCTIONAL | `proofs/aggregation.go`, `proofs/recursive_aggregator.go` | KZG/SNARK/STARK registry and aggregator |
| 61 | **exposed zkISA** | **MISSING** | None | No host ABI bridge exposing zkISA to EVM callers |
| 62 | AA proofs | PARTIAL | `proofs/aa_proofs.go`, `core/vm/precompile_aa_proof.go` | Sigma protocol simulated, not real ZK |
| 63 | encrypted mempool | FUNCTIONAL | `txpool/encrypted/` | Commit-reveal, threshold decryption |
| 64 | PQ transactions | PARTIAL | `core/types/pq_transaction.go`, `crypto/pqc/pq_tx_signer.go` | Type 0x07 exists; Dilithium/Falcon stubs |
| 65 | private L1 shielded | PARTIAL | `crypto/zk_transfer.go`, `crypto/shielded.go`, `crypto/nullifier_set.go` | Proof generation simulated; nullifier/commitment trees real |

---

## Top 15 Priority Gaps

### MISSING (3)
1. **jeanVM aggregation** - No implementation; research-stage; ZK-circuit BLS aggregate verification for 1M attestations
2. **PQ available chain** - No PQ-secure fork choice / block hashing / chain history commitment
3. **Exposed zkISA** - No host ABI bridge exposing zkVM ISA to EVM callers

### STUB (1)
4. **PQC primitives** (Dilithium/Falcon/SPHINCS+) - All `Sign()` calls are `keccak256(sk||msg)` padded; breaks ALL downstream PQ features

### PARTIAL - High Priority (11)
5. **Real zkVM proof backend** - MockVerifier only; needs gnark Groth16/PLONK wiring
6. **Canonical guest execution wiring** - `canonical.go:Execute()` returns keccak hash, doesn't call `RVCPU.Run()`
7. **STF in zkISA** - Data model complete; proof generation simulated
8. **AA proofs** - Sigma protocol simulated, not real ZK circuit
9. **Private L1 shielded transfers** - `shielded.go` has `Proof: nil` stub; `zk_transfer.go` uses simulated Pedersen
10. **Real-time CL proofs** - All proofs are simulated Merkle branches
11. **Endgame finality BLS** - SSF voting machine uses `[96]byte` placeholder for BLS sigs
12. **Gigagas chain integration** - Infrastructure exists but not wired to live block execution
13. **PQ L1 hash-based** - Two split implementations (`l1_hash_sig.go` vs `l1_hash_sig_v2.go`)
14. **Modernized beacon specs** - 4 parallel BeaconState representations need convergence
15. **EIP-7702 SetCode broadcast** - Generic fanout, no SetCode-specific gossip

---

## Reference Code Available in refs/

| Gap | Reference File | Key Artifacts |
|-----|---------------|---------------|
| State access events | `refs/go-ethereum/core/state/access_events.go` | `AccessEvents`, `touchAddressAndChargeGas` |
| Stateless witness | `refs/go-ethereum/core/stateless/witness.go` | `Witness`, codes+state map |
| EVM opcode activation | `refs/go-ethereum/core/vm/eips.go` | `activators` map, `EnableEIP` |
| State history/expiry | `refs/go-ethereum/triedb/pathdb/history_reader.go` | `HistoryRange`, bounded readers |
| Execution proofs CL | `refs/consensus-specs/specs/_features/eip8025/` | `ExecutionProof`, `process_execution_proof` |
| DAS sample params | `refs/consensus-specs/specs/fulu/das-core.md` | `SAMPLES_PER_SLOT=8`, `CUSTODY_REQUIREMENT=4` |
| Secret proposers (Whisk) | `refs/consensus-specs/specs/_features/eip7441/` | `WhiskTracker`, Curdleproofs |
| Recursive PLONK | `refs/gnark/std/recursion/plonk/verifier.go` | `AssertSameProofs`, `AssertDifferentProofs` |
| PQ tx signing | `refs/circl/sign/mldsa/mldsa65/dilithium.go` | `GenerateKey`, `SignTo` with context |
| ML-DSA precompile | `refs/EIPs/EIPS/eip-8051.md` | VERIFY_MLDSA at 0x12, gas 4500 |
| PQ algorithm registry | `refs/EIPs/EIPS/eip-7932.md` | `Algorithm` trait, `SIGRECOVER` |
| 1-ETH includer auction | `refs/EIPs/EIPS/eip-8046.md` | UPIL, `marginal_ranking_fee_per_gas` |
| Unified multidim fees | `refs/EIPs/EIPS/eip-7999.md` | `max_fee` unified budget |

## External Libraries

| Gap | Library | Stars | Role |
|-----|---------|-------|------|
| PQ crypto | `cloudflare/circl` | ~1,600 | Direct dep (already in go.mod) |
| ZK proofs | `Consensys/gnark` | ~1,700 | Direct dep candidate |
| Cell gossip | `libp2p/go-libp2p-pubsub` | ~355 | Substrate (cell-layer is eth2028-specific) |
| Lattice ring | `tuneinsight/lattigo` | ~1,400 | Potential dep for ring arithmetic |
| Reed-Solomon | `klauspost/reedsolomon` | ~2,000 | Direct dep candidate |
