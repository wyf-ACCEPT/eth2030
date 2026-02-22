// Package eftest implements an Ethereum Foundation state test runner.
// It parses the standard EF state test JSON format and executes tests
// against ETH2030's EVM and state implementation.
package eftest

import (
	"crypto/ecdsa"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math/big"
	"os"
	"strings"

	"github.com/eth2030/eth2030/core"
	"github.com/eth2030/eth2030/core/state"
	"github.com/eth2030/eth2030/core/types"
	"github.com/eth2030/eth2030/crypto"
)

// rlpEmptyList is the RLP encoding of an empty list (0xc0).
var rlpEmptyList = []byte{0xc0}

// stJSON is the top-level JSON structure for a single state test.
type stJSON struct {
	Env  stEnv                    `json:"env"`
	Pre  map[string]stAccount     `json:"pre"`
	Tx   stTransaction            `json:"transaction"`
	Post map[string][]stPostState `json:"post"`
	Out  string                   `json:"out"`
}

// stEnv holds the block environment fields.
type stEnv struct {
	CurrentCoinbase   string `json:"currentCoinbase"`
	CurrentDifficulty string `json:"currentDifficulty"`
	CurrentGasLimit   string `json:"currentGasLimit"`
	CurrentNumber     string `json:"currentNumber"`
	CurrentTimestamp  string `json:"currentTimestamp"`
	PreviousHash      string `json:"previousHash"`
	CurrentBaseFee    string `json:"currentBaseFee"`
	CurrentRandom     string `json:"currentRandom"`
	ExcessBlobGas     string `json:"currentExcessBlobGas"`
}

// stAccount holds the pre-state for a single account.
type stAccount struct {
	Balance string            `json:"balance"`
	Code    string            `json:"code"`
	Nonce   string            `json:"nonce"`
	Storage map[string]string `json:"storage"`
}

// stTransaction holds the transaction specification.
type stTransaction struct {
	Data                 []string `json:"data"`
	GasLimit             []string `json:"gasLimit"`
	Value                []string `json:"value"`
	GasPrice             string   `json:"gasPrice"`
	Nonce                string   `json:"nonce"`
	To                   string   `json:"to"`
	Sender               string   `json:"sender"`
	SecretKey            string   `json:"secretKey"`
	MaxFeePerGas         string   `json:"maxFeePerGas"`
	MaxPriorityFeePerGas string   `json:"maxPriorityFeePerGas"`
}

// stPostState holds expected results for a specific fork and index combination.
type stPostState struct {
	Hash            string    `json:"hash"`
	Logs            string    `json:"logs"`
	Indexes         stIndexes `json:"indexes"`
	ExpectException string    `json:"expectException"`
	TxBytes         string    `json:"txbytes"`
}

// stIndexes selects which data/gas/value variant to use from the transaction.
type stIndexes struct {
	Data  int `json:"data"`
	Gas   int `json:"gas"`
	Value int `json:"value"`
}

// StateTest wraps a parsed state test with its name.
type StateTest struct {
	Name string
	json stJSON
}

// StateSubtest identifies a specific subtest within a state test.
type StateSubtest struct {
	Fork  string
	Index int
}

// RunResult holds the outcome of running a single subtest.
type RunResult struct {
	Name         string
	Fork         string
	Index        int
	Passed       bool
	ExpectedRoot types.Hash
	GotRoot      types.Hash
	ExpectedLogs types.Hash
	GotLogs      types.Hash
	Error        error
	StateDB      *state.MemoryStateDB
}

// LoadStateTests parses a state test JSON file and returns the tests.
func LoadStateTests(path string) (map[string]*StateTest, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read state test file: %w", err)
	}

	var raw map[string]stJSON
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, fmt.Errorf("parse state test JSON: %w", err)
	}

	tests := make(map[string]*StateTest, len(raw))
	for name, j := range raw {
		tests[name] = &StateTest{Name: name, json: j}
	}
	return tests, nil
}

// Subtests returns all fork/index permutations for this state test.
func (st *StateTest) Subtests() []StateSubtest {
	var subs []StateSubtest
	for fork, posts := range st.json.Post {
		for i := range posts {
			subs = append(subs, StateSubtest{Fork: fork, Index: i})
		}
	}
	return subs
}

// Run executes a single subtest and returns the result.
func (st *StateTest) Run(subtest StateSubtest) *RunResult {
	result := &RunResult{
		Name:  st.Name,
		Fork:  subtest.Fork,
		Index: subtest.Index,
	}

	posts, ok := st.json.Post[subtest.Fork]
	if !ok || subtest.Index >= len(posts) {
		result.Error = fmt.Errorf("no post state for fork %s index %d", subtest.Fork, subtest.Index)
		return result
	}
	post := posts[subtest.Index]
	result.ExpectedRoot = hexToHash(post.Hash)
	result.ExpectedLogs = hexToHash(post.Logs)

	// Get chain config for this fork.
	config := ForkConfig(subtest.Fork)
	if config == nil {
		result.Error = fmt.Errorf("unsupported fork: %s", subtest.Fork)
		return result
	}

	// Build pre-state.
	statedb := state.NewMemoryStateDB()
	for addrHex, acct := range st.json.Pre {
		addr := hexToAddress(addrHex)
		statedb.CreateAccount(addr)
		statedb.AddBalance(addr, hexToBigInt(acct.Balance))
		statedb.SetNonce(addr, hexToUint64(acct.Nonce))
		code := hexToBytes(acct.Code)
		if len(code) > 0 {
			statedb.SetCode(addr, code)
		}
		for keyHex, valHex := range acct.Storage {
			statedb.SetState(addr, hexToHash(keyHex), hexToHash(valHex))
		}
	}

	// Finalize pre-state so GetCommittedState returns correct original values
	// for SSTORE gas calculations (EIP-2200, EIP-3529).
	statedb.FinalizePreState()

	// Determine sender address.
	var senderAddr types.Address
	if st.json.Tx.Sender != "" {
		senderAddr = hexToAddress(st.json.Tx.Sender)
	} else if st.json.Tx.SecretKey != "" {
		key, err := hexToPrivateKey(st.json.Tx.SecretKey)
		if err != nil {
			result.Error = fmt.Errorf("parse secret key: %w", err)
			return result
		}
		senderAddr = crypto.PubkeyToAddress(key.PublicKey)
	}

	// Build header from env.
	header := st.buildHeader(config)

	// Build transaction.
	tx, err := st.buildTransaction(post.Indexes, senderAddr)
	if err != nil {
		// If we expect an exception, a build failure might be valid.
		if post.ExpectException != "" {
			result.Passed = true
			return result
		}
		result.Error = fmt.Errorf("build transaction: %w", err)
		return result
	}

	// Set up gas pool.
	gasPool := new(core.GasPool).AddGas(header.GasLimit)

	// Set tx context.
	statedb.SetTxContext(tx.Hash(), 0)

	// Apply transaction with panic recovery (some edge-case EVM inputs may panic).
	var applyErr error
	func() {
		defer func() {
			if r := recover(); r != nil {
				applyErr = fmt.Errorf("EVM panic: %v", r)
			}
		}()
		_, _, applyErr = core.ApplyTransaction(config, statedb, header, tx, gasPool)
	}()

	// If we expect an exception and got one, that's a pass.
	if post.ExpectException != "" {
		if applyErr != nil {
			result.Passed = true
		} else {
			result.Error = fmt.Errorf("expected exception %q but tx succeeded", post.ExpectException)
		}
		return result
	}

	// For some tests, the tx is expected to fail (e.g. intrinsic gas too low)
	// but still produce a valid state root. We continue even if applyErr != nil.
	_ = applyErr

	// Compute state root.
	stateRoot, err := statedb.Commit()
	if err != nil {
		result.Error = fmt.Errorf("commit state: %w", err)
		return result
	}
	result.GotRoot = stateRoot
	result.StateDB = statedb

	// Compute logs hash.
	logs := statedb.GetLogs(tx.Hash())
	logsHash := computeLogsHash(logs)
	result.GotLogs = logsHash

	// Compare state root.
	if result.ExpectedRoot != (types.Hash{}) {
		result.Passed = (stateRoot == result.ExpectedRoot)
		if !result.Passed {
			result.Error = fmt.Errorf("state root mismatch: expected %s, got %s",
				result.ExpectedRoot.Hex(), stateRoot.Hex())
		}
	} else {
		// Zero expected root means the test doesn't validate state root.
		result.Passed = true
	}

	// If state root matched, also verify logs hash.
	if result.Passed && result.ExpectedLogs != (types.Hash{}) {
		if logsHash != result.ExpectedLogs {
			result.Passed = false
			result.Error = fmt.Errorf("logs hash mismatch: expected %s, got %s",
				result.ExpectedLogs.Hex(), logsHash.Hex())
		}
	}

	return result
}

// buildHeader creates a types.Header from the test environment.
func (st *StateTest) buildHeader(config *core.ChainConfig) *types.Header {
	env := st.json.Env
	header := &types.Header{
		Coinbase:   hexToAddress(env.CurrentCoinbase),
		Difficulty: hexToBigInt(env.CurrentDifficulty),
		GasLimit:   hexToUint64(env.CurrentGasLimit),
		Number:     hexToBigInt(env.CurrentNumber),
		Time:       hexToUint64(env.CurrentTimestamp),
		ParentHash: hexToHash(env.PreviousHash),
	}

	if env.CurrentBaseFee != "" {
		header.BaseFee = hexToBigInt(env.CurrentBaseFee)
	}

	if env.CurrentRandom != "" {
		header.MixDigest = hexToHash(env.CurrentRandom)
	}

	if env.ExcessBlobGas != "" {
		ebg := hexToUint64(env.ExcessBlobGas)
		header.ExcessBlobGas = &ebg
	}

	return header
}

// buildTransaction creates a types.Transaction from the test specification.
func (st *StateTest) buildTransaction(indexes stIndexes, sender types.Address) (*types.Transaction, error) {
	txJSON := st.json.Tx

	// Select data, gasLimit, value by index.
	var data []byte
	if indexes.Data < len(txJSON.Data) {
		data = hexToBytes(txJSON.Data[indexes.Data])
	}

	var gasLimit uint64
	if indexes.Gas < len(txJSON.GasLimit) {
		gasLimit = hexToUint64(txJSON.GasLimit[indexes.Gas])
	}

	var value *big.Int
	if indexes.Value < len(txJSON.Value) {
		value = hexToBigInt(txJSON.Value[indexes.Value])
	} else {
		value = new(big.Int)
	}

	nonce := hexToUint64(txJSON.Nonce)

	// Determine recipient.
	var to *types.Address
	if txJSON.To != "" {
		addr := hexToAddress(txJSON.To)
		to = &addr
	}

	var tx *types.Transaction
	if txJSON.MaxFeePerGas != "" {
		// EIP-1559 transaction.
		tx = types.NewTransaction(&types.DynamicFeeTx{
			ChainID:   big.NewInt(1),
			Nonce:     nonce,
			GasTipCap: hexToBigInt(txJSON.MaxPriorityFeePerGas),
			GasFeeCap: hexToBigInt(txJSON.MaxFeePerGas),
			Gas:       gasLimit,
			To:        to,
			Value:     value,
			Data:      data,
		})
	} else {
		// Legacy transaction.
		gasPrice := hexToBigInt(txJSON.GasPrice)
		tx = types.NewTransaction(&types.LegacyTx{
			Nonce:    nonce,
			GasPrice: gasPrice,
			Gas:      gasLimit,
			To:       to,
			Value:    value,
			Data:     data,
		})
	}

	tx.SetSender(sender)
	return tx, nil
}

// computeLogsHash computes the keccak256 of the RLP-encoded logs.
func computeLogsHash(logs []*types.Log) types.Hash {
	if len(logs) == 0 {
		return crypto.Keccak256Hash(rlpEmptyList)
	}
	encoded, err := types.EncodeLogsRLP(logs)
	if err != nil {
		return crypto.Keccak256Hash(rlpEmptyList)
	}
	return crypto.Keccak256Hash(encoded)
}

// forkLevel maps fork names to numeric ordering for cumulative config building.
var forkLevel = map[string]int{
	"Frontier": 0, "Homestead": 1, "EIP150": 2,
	"EIP158": 3, "SpuriousDragon": 3, "TangerineWhistle": 3,
	"Byzantium":      4,
	"Constantinople": 5, "ConstantinopleFix": 5,
	"Istanbul": 6, "Berlin": 7, "London": 8,
	"Merge": 9, "Paris": 9,
	"Shanghai": 10, "Cancun": 11, "Prague": 12,
}

// ForkConfig returns the ChainConfig for a named fork.
func ForkConfig(fork string) *core.ChainConfig {
	level, ok := forkLevel[fork]
	if !ok {
		return nil
	}
	zero := big.NewInt(0)
	ts := uint64(0)
	c := &core.ChainConfig{ChainID: big.NewInt(1)}
	if level >= 1 {
		c.HomesteadBlock = zero
	}
	if level >= 2 {
		c.EIP150Block = zero
	}
	if level >= 3 {
		c.EIP155Block = zero
		c.EIP158Block = zero
	}
	if level >= 4 {
		c.ByzantiumBlock = zero
	}
	if level >= 5 {
		c.ConstantinopleBlock = zero
		c.PetersburgBlock = zero
	}
	if level >= 6 {
		c.IstanbulBlock = zero
	}
	if level >= 7 {
		c.BerlinBlock = zero
	}
	if level >= 8 {
		c.LondonBlock = zero
	}
	if level >= 9 {
		c.TerminalTotalDifficulty = zero
	}
	if level >= 10 {
		c.ShanghaiTime = &ts
	}
	if level >= 11 {
		c.CancunTime = &ts
	}
	if level >= 12 {
		c.PragueTime = &ts
	}
	return c
}

// ForkSupported returns whether the given fork name is supported.
func ForkSupported(fork string) bool {
	return ForkConfig(fork) != nil
}

// SupportedForks returns all supported fork names.
func SupportedForks() []string {
	return []string{
		"Frontier", "Homestead", "EIP150", "EIP158",
		"SpuriousDragon", "TangerineWhistle",
		"Byzantium", "Constantinople", "ConstantinopleFix",
		"Istanbul", "Berlin", "London",
		"Merge", "Paris", "Shanghai", "Cancun", "Prague",
	}
}

// Hex parsing utilities.

func hexToBytes(s string) []byte {
	s = strings.TrimPrefix(s, "0x")
	s = strings.TrimPrefix(s, "0X")
	if len(s) == 0 {
		return nil
	}
	if len(s)%2 == 1 {
		s = "0" + s
	}
	b, _ := hex.DecodeString(s)
	return b
}

func hexToUint64(s string) uint64 {
	s = strings.TrimPrefix(s, "0x")
	s = strings.TrimPrefix(s, "0X")
	if len(s) == 0 {
		return 0
	}
	val := new(big.Int)
	val.SetString(s, 16)
	return val.Uint64()
}

func hexToBigInt(s string) *big.Int {
	s = strings.TrimPrefix(s, "0x")
	s = strings.TrimPrefix(s, "0X")
	if len(s) == 0 {
		return new(big.Int)
	}
	val := new(big.Int)
	val.SetString(s, 16)
	return val
}

func hexToAddress(s string) types.Address {
	return types.HexToAddress(s)
}

func hexToHash(s string) types.Hash {
	return types.HexToHash(s)
}

func hexToPrivateKey(s string) (*ecdsa.PrivateKey, error) {
	b := hexToBytes(s)
	if len(b) == 0 {
		return nil, fmt.Errorf("empty secret key")
	}
	curve := crypto.S256()
	priv := new(ecdsa.PrivateKey)
	priv.PublicKey.Curve = curve
	priv.D = new(big.Int).SetBytes(b)
	priv.PublicKey.X, priv.PublicKey.Y = curve.ScalarBaseMult(b)
	return priv, nil
}
