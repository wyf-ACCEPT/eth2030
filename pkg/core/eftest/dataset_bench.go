package eftest

import (
	"archive/zip"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"math/big"
	"os"
	"strings"
	"time"

	gethcommon "github.com/ethereum/go-ethereum/common"
	gethcore "github.com/ethereum/go-ethereum/core"
	gethstate "github.com/ethereum/go-ethereum/core/state"
	gethtracing "github.com/ethereum/go-ethereum/core/tracing"
	gethvm "github.com/ethereum/go-ethereum/core/vm"
	gethcrypto "github.com/ethereum/go-ethereum/crypto"
	gethparams "github.com/ethereum/go-ethereum/params"
	"github.com/holiman/uint256"

	"github.com/eth2030/eth2030/geth"
)

// datasetJSON is the top-level JSON structure for the Altius/Uniswap dataset format.
// Unlike standard EF tests, the transaction field is an array of individual transactions.
type datasetJSON struct {
	Env  stEnv                `json:"env"`
	Pre  map[string]stAccount `json:"pre"`
	Txs  json.RawMessage      `json:"transaction"`
	Post map[string]stPostState `json:"post"`
}

// datasetTx represents a single transaction in the dataset.
type datasetTx struct {
	Data      string `json:"data"`
	GasLimit  string `json:"gasLimit"`
	GasPrice  string `json:"gasPrice"`
	Nonce     string `json:"nonce"`
	SecretKey string `json:"secretKey"`
	To        string `json:"to"`
	Value     string `json:"value"`
}

// DatasetBenchResult holds the benchmark results.
type DatasetBenchResult struct {
	TxCount      int
	TxSuccess    int
	TxFailed     int
	TotalGasUsed uint64
	Duration     time.Duration
	TPS          float64
	MGasPerSec   float64 // Mgas/s
	GasPerTx     float64
	Errors       []string
}

// resolveDatasetPath returns the JSON path, extracting from zip if needed.
// If path exists as-is, return it. Otherwise try path+".zip" and extract.
func resolveDatasetPath(path string) (string, error) {
	if _, err := os.Stat(path); err == nil {
		return path, nil
	}
	zipPath := path + ".zip"
	if _, err := os.Stat(zipPath); err != nil {
		return "", fmt.Errorf("dataset not found at %s or %s", path, zipPath)
	}
	fmt.Printf("Extracting %s...\n", zipPath)
	r, err := zip.OpenReader(zipPath)
	if err != nil {
		return "", fmt.Errorf("open zip: %w", err)
	}
	defer r.Close()

	for _, f := range r.File {
		if strings.HasSuffix(f.Name, ".json") {
			rc, err := f.Open()
			if err != nil {
				return "", fmt.Errorf("open zip entry: %w", err)
			}
			defer rc.Close()
			// Extract next to the zip file.
			outPath := strings.TrimSuffix(zipPath, ".zip")
			out, err := os.Create(outPath)
			if err != nil {
				return "", fmt.Errorf("create output: %w", err)
			}
			if _, err := io.Copy(out, rc); err != nil {
				out.Close()
				return "", fmt.Errorf("extract: %w", err)
			}
			out.Close()
			fmt.Printf("Extracted to %s\n", outPath)
			return outPath, nil
		}
	}
	return "", fmt.Errorf("no .json file found in %s", zipPath)
}

// LoadAndRunDataset loads a dataset JSON file (or extracts from .zip) and
// executes all transactions sequentially, returning benchmark metrics.
func LoadAndRunDataset(path string) (*DatasetBenchResult, error) {
	resolvedPath, err := resolveDatasetPath(path)
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(resolvedPath)
	if err != nil {
		return nil, fmt.Errorf("read dataset file: %w", err)
	}

	// Parse outer wrapper: {"test-name": {...}}
	var raw map[string]datasetJSON
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, fmt.Errorf("parse dataset JSON: %w", err)
	}

	if len(raw) == 0 {
		return nil, fmt.Errorf("empty dataset")
	}

	// Take the first (and usually only) test entry.
	var ds datasetJSON
	for _, v := range raw {
		ds = v
		break
	}

	// Parse the transaction array.
	var txs []datasetTx
	if err := json.Unmarshal(ds.Txs, &txs); err != nil {
		return nil, fmt.Errorf("parse transactions: %w", err)
	}

	// Determine fork from post keys, default to Cancun.
	fork := "Cancun"
	for k := range ds.Post {
		fork = k
		break
	}

	config, err := geth.EFTestChainConfig(fork)
	if err != nil {
		return nil, fmt.Errorf("get chain config for fork %s: %w", fork, err)
	}

	// Build pre-state.
	fmt.Printf("Building pre-state (%d accounts)...\n", len(ds.Pre))
	preStart := time.Now()
	preAccounts := make(map[string]geth.PreAccount, len(ds.Pre))
	for addrHex, acct := range ds.Pre {
		preAccounts[addrHex] = geth.PreAccount{
			Balance: hexToBigInt(acct.Balance),
			Nonce:   hexToUint64(acct.Nonce),
			Code:    hexToBytes(acct.Code),
			Storage: parseStorage(acct.Storage),
		}
	}

	stState, err := geth.MakePreState(preAccounts)
	if err != nil {
		return nil, fmt.Errorf("make pre-state: %w", err)
	}
	defer stState.Close()
	fmt.Printf("Pre-state built in %v\n", time.Since(preStart))

	// Build block context.
	blockCtx := buildDatasetBlockContext(ds.Env, config)

	// Prepare results.
	result := &DatasetBenchResult{
		TxCount: len(txs),
	}

	// Pre-derive sender addresses (not counted in benchmark time).
	fmt.Printf("Deriving sender addresses for %d transactions...\n", len(txs))
	type preparedTx struct {
		from     gethcommon.Address
		to       *gethcommon.Address
		nonce    uint64
		value    *big.Int
		gasLimit uint64
		gasPrice *big.Int
		data     []byte
	}
	prepared := make([]preparedTx, len(txs))
	for i, tx := range txs {
		keyBytes := hexToBytes(tx.SecretKey)
		key, err := gethcrypto.ToECDSA(keyBytes)
		if err != nil {
			return nil, fmt.Errorf("tx[%d]: invalid secret key: %w", i, err)
		}

		ptx := preparedTx{
			from:     gethcrypto.PubkeyToAddress(key.PublicKey),
			nonce:    hexToUint64(tx.Nonce),
			gasLimit: hexToUint64(tx.GasLimit),
			gasPrice: hexToBigInt(tx.GasPrice),
		}

		if tx.To != "" {
			addr := gethcommon.HexToAddress(tx.To)
			ptx.to = &addr
		}

		if tx.Value != "" && tx.Value != "0x" && tx.Value != "0x00" {
			ptx.value = hexToBigInt(tx.Value)
		} else {
			ptx.value = new(big.Int)
		}

		if tx.Data != "" && tx.Data != "0x" {
			ptx.data, _ = hex.DecodeString(strings.TrimPrefix(strings.TrimPrefix(tx.Data, "0x"), "0X"))
		}

		prepared[i] = ptx
	}

	// Compute effective gas price for EIP-1559 chains.
	baseFee := blockCtx.BaseFee

	blockGasLimit := hexToUint64(ds.Env.CurrentGasLimit)

	// Execute all transactions sequentially and measure time.
	fmt.Printf("Executing %d transactions (fork=%s, blockGasLimit=%d)...\n", len(txs), fork, blockGasLimit)
	execStart := time.Now()

	for i, ptx := range prepared {
		// Compute effective gas price.
		effectiveGasPrice := ptx.gasPrice
		if baseFee != nil && effectiveGasPrice.Cmp(baseFee) > 0 {
			effectiveGasPrice = new(big.Int).Set(ptx.gasPrice)
		}

		msg := &gethcore.Message{
			From:      ptx.from,
			To:        ptx.to,
			Nonce:     ptx.nonce,
			Value:     ptx.value,
			GasLimit:  ptx.gasLimit,
			GasPrice:  effectiveGasPrice,
			GasFeeCap: ptx.gasPrice,
			GasTipCap: new(big.Int),
			Data:      ptx.data,
		}

		evm := gethvm.NewEVM(blockCtx, stState.StateDB, config, gethvm.Config{})
		gp := new(gethcore.GasPool).AddGas(blockGasLimit)

		stState.StateDB.SetTxContext(gethcommon.Hash{}, i)
		snapshot := stState.StateDB.Snapshot()

		execResult, err := gethcore.ApplyMessage(evm, msg, gp)
		if err != nil {
			stState.StateDB.RevertToSnapshot(snapshot)
			result.TxFailed++
			if len(result.Errors) < 10 {
				result.Errors = append(result.Errors, fmt.Sprintf("tx[%d]: %v", i, err))
			}
			continue
		}

		if execResult.Failed() {
			result.TxFailed++
			if len(result.Errors) < 10 {
				result.Errors = append(result.Errors,
					fmt.Sprintf("tx[%d]: EVM reverted (gas used: %d)", i, execResult.UsedGas))
			}
		} else {
			result.TxSuccess++
		}
		result.TotalGasUsed += execResult.UsedGas
	}

	result.Duration = time.Since(execStart)

	// Touch coinbase for correct state accounting.
	coinbase := blockCtx.Coinbase
	stState.StateDB.AddBalance(coinbase, new(uint256.Int), gethtracing.BalanceChangeUnspecified)

	// Calculate metrics.
	secs := result.Duration.Seconds()
	if secs > 0 {
		result.TPS = float64(result.TxCount) / secs
		result.MGasPerSec = float64(result.TotalGasUsed) / secs / 1_000_000
	}
	if result.TxCount > 0 {
		result.GasPerTx = float64(result.TotalGasUsed) / float64(result.TxCount)
	}

	return result, nil
}

// buildDatasetBlockContext creates a go-ethereum BlockContext from the dataset env.
func buildDatasetBlockContext(env stEnv, config *gethparams.ChainConfig) gethvm.BlockContext {
	ctx := gethvm.BlockContext{
		CanTransfer: gethcore.CanTransfer,
		Transfer:    gethcore.Transfer,
		GetHash:     geth.TestBlockHash,
		Coinbase:    gethcommon.HexToAddress(env.CurrentCoinbase),
		GasLimit:    hexToUint64(env.CurrentGasLimit),
		BlockNumber: hexToBigInt(env.CurrentNumber),
		Time:        hexToUint64(env.CurrentTimestamp),
		Difficulty:  new(big.Int),
	}

	if env.CurrentDifficulty != "" {
		ctx.Difficulty = hexToBigInt(env.CurrentDifficulty)
	}

	if config.IsLondon(new(big.Int)) {
		baseFee := hexToBigIntNilable(env.CurrentBaseFee)
		if baseFee == nil {
			baseFee = big.NewInt(0x0a)
		}
		ctx.BaseFee = baseFee
	}

	if config.IsLondon(new(big.Int)) && env.CurrentRandom != "" {
		rnd := gethcommon.HexToHash(env.CurrentRandom)
		ctx.Random = &rnd
		ctx.Difficulty = big.NewInt(0)
	}

	if config.IsCancun(new(big.Int), ctx.Time) && env.ExcessBlobGas != "" {
		ebg := hexToUint64(env.ExcessBlobGas)
		ctx.BlobBaseFee = big.NewInt(1)
		if ebg > 0 {
			ctx.BlobBaseFee = fakeExponential(big.NewInt(1), new(big.Int).SetUint64(ebg), big.NewInt(3338477))
		}
	}

	return ctx
}

// keep gethstate imported
var _ *gethstate.StateDB
