package core

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"

	"github.com/eth2030/eth2030/core/types"
	"github.com/eth2030/eth2030/crypto"
)

// EIP-6110: Supply Validator Deposits On Chain
//
// Moves deposit processing from the CL deposit contract event parsing
// to the execution layer, embedding deposit operations directly in
// block processing. This removes the deposit voting mechanism and
// the associated latency.

// DepositContractAddr is the canonical beacon chain deposit contract.
var DepositContractAddr = types.HexToAddress("0x00000000219ab540356cBB839Cbe05303d7705Fa")

// DEPOSIT_EVENT_SIGNATURE is keccak256("DepositEvent(bytes,bytes,bytes,bytes,bytes)").
var DepositEventSignature = crypto.Keccak256Hash([]byte("DepositEvent(bytes,bytes,bytes,bytes,bytes)"))

// MaxDepositsPerBlock is the maximum number of deposits that can be processed
// in a single block (from consensus specs).
const MaxDepositsPerBlock = 8192

// Deposit processing errors.
var (
	ErrDepositEmptyPubkey          = errors.New("eip6110: empty validator pubkey")
	ErrDepositInvalidPubkeySize    = errors.New("eip6110: pubkey must be 48 bytes")
	ErrDepositInvalidSigSize       = errors.New("eip6110: signature must be 96 bytes")
	ErrDepositInvalidCredentials   = errors.New("eip6110: invalid withdrawal credentials")
	ErrDepositZeroAmount           = errors.New("eip6110: deposit amount is zero")
	ErrDepositBelowMinimum         = errors.New("eip6110: deposit amount below minimum (1 ETH)")
	ErrTooManyDeposits             = errors.New("eip6110: too many deposits in block")
	ErrDepositLogWrongAddress      = errors.New("eip6110: log not from deposit contract")
	ErrDepositLogWrongTopic        = errors.New("eip6110: log topic mismatch")
	ErrDepositLogDataTooShort      = errors.New("eip6110: log data too short")
)

// MinDepositAmount is the minimum deposit amount in Gwei (1 ETH).
const MinDepositAmount = 1_000_000_000

// DepositLog represents a raw deposit event log before conversion to a DepositRequest.
type DepositLog struct {
	Pubkey                [48]byte
	WithdrawalCredentials [32]byte
	Amount                uint64
	Signature             [96]byte
	Index                 uint64
}

// ParseDepositLogs extracts deposit events from transaction receipts.
// It scans all logs for DepositEvent emissions from the deposit contract.
func ParseDepositLogs(receipts []*types.Receipt) []types.DepositRequest {
	var deposits []types.DepositRequest

	for _, receipt := range receipts {
		if receipt.Status != types.ReceiptStatusSuccessful {
			continue
		}
		for _, log := range receipt.Logs {
			if log.Address != DepositContractAddr {
				continue
			}
			if len(log.Topics) < 1 || log.Topics[0] != DepositEventSignature {
				continue
			}
			dep, err := parseDepositLogData(log.Data)
			if err != nil {
				continue // skip malformed logs
			}
			deposits = append(deposits, *dep)
		}
	}

	return deposits
}

// parseDepositLogData decodes the ABI-encoded DepositEvent log data.
// The deposit contract emits:
//
//	DepositEvent(bytes pubkey, bytes withdrawal_credentials, bytes amount, bytes signature, bytes index)
//
// Each field is ABI-encoded as a dynamic bytes with offset/length prefix.
// Layout: 5 offsets (5*32=160 bytes) + 5 length-prefixed data fields.
func parseDepositLogData(data []byte) (*types.DepositRequest, error) {
	// Minimum: 5 offsets (160) + 5 length words (160) + 48+32+8+96+8 = 512 bytes min
	if len(data) < 512 {
		return nil, ErrDepositLogDataTooShort
	}

	// Read the 5 ABI offsets (each is a uint256 pointing to the start of that field's data).
	offsets := make([]int, 5)
	for i := 0; i < 5; i++ {
		off := new(bytes.Buffer)
		off.Write(data[i*32 : (i+1)*32])
		// ABI offsets are uint256, but will fit in int for our purposes.
		offsetVal := binary.BigEndian.Uint64(data[i*32+24 : (i+1)*32])
		offsets[i] = int(offsetVal)
	}

	readField := func(offset int) ([]byte, error) {
		if offset+32 > len(data) {
			return nil, ErrDepositLogDataTooShort
		}
		length := int(binary.BigEndian.Uint64(data[offset+24 : offset+32]))
		start := offset + 32
		end := start + length
		if end > len(data) {
			return nil, ErrDepositLogDataTooShort
		}
		return data[start:end], nil
	}

	// Field 0: pubkey (48 bytes)
	pubkeyBytes, err := readField(offsets[0])
	if err != nil {
		return nil, err
	}
	if len(pubkeyBytes) != 48 {
		return nil, ErrDepositInvalidPubkeySize
	}

	// Field 1: withdrawal_credentials (32 bytes)
	wcBytes, err := readField(offsets[1])
	if err != nil {
		return nil, err
	}
	if len(wcBytes) != 32 {
		return nil, ErrDepositInvalidCredentials
	}

	// Field 2: amount (8 bytes, little-endian)
	amountBytes, err := readField(offsets[2])
	if err != nil {
		return nil, err
	}
	if len(amountBytes) != 8 {
		return nil, fmt.Errorf("eip6110: invalid amount size: %d", len(amountBytes))
	}
	amount := binary.LittleEndian.Uint64(amountBytes)

	// Field 3: signature (96 bytes)
	sigBytes, err := readField(offsets[3])
	if err != nil {
		return nil, err
	}
	if len(sigBytes) != 96 {
		return nil, ErrDepositInvalidSigSize
	}

	// Field 4: index (8 bytes, little-endian)
	indexBytes, err := readField(offsets[4])
	if err != nil {
		return nil, err
	}
	if len(indexBytes) != 8 {
		return nil, fmt.Errorf("eip6110: invalid index size: %d", len(indexBytes))
	}
	index := binary.LittleEndian.Uint64(indexBytes)

	dep := &types.DepositRequest{
		Amount: amount,
		Index:  index,
	}
	copy(dep.Pubkey[:], pubkeyBytes)
	copy(dep.WithdrawalCredentials[:], wcBytes)
	copy(dep.Signature[:], sigBytes)

	return dep, nil
}

// ValidateDepositRequest checks that a deposit request is well-formed.
func ValidateDepositRequest(req *types.DepositRequest) error {
	// Pubkey must not be all zeros.
	empty48 := [48]byte{}
	if req.Pubkey == empty48 {
		return ErrDepositEmptyPubkey
	}

	// Amount must be non-zero and at least 1 ETH (MinDepositAmount Gwei).
	if req.Amount == 0 {
		return ErrDepositZeroAmount
	}
	if req.Amount < MinDepositAmount {
		return ErrDepositBelowMinimum
	}

	return nil
}

// ValidateBlockDeposits validates all deposit requests in a block.
func ValidateBlockDeposits(deposits []types.DepositRequest) error {
	if len(deposits) > MaxDepositsPerBlock {
		return ErrTooManyDeposits
	}
	for i := range deposits {
		if err := ValidateDepositRequest(&deposits[i]); err != nil {
			return fmt.Errorf("deposit %d: %w", i, err)
		}
	}
	return nil
}

// ProcessDeposits applies a list of deposit requests to the validator set.
// In a full implementation, this would update the beacon state's validator
// registry. Here we validate and prepare the deposits for CL consumption.
func ProcessDeposits(deposits []types.DepositRequest, validators *depositValidatorSet) error {
	if len(deposits) > MaxDepositsPerBlock {
		return ErrTooManyDeposits
	}

	for i := range deposits {
		if err := ValidateDepositRequest(&deposits[i]); err != nil {
			return fmt.Errorf("deposit %d: %w", i, err)
		}
		if err := validators.ApplyDeposit(&deposits[i]); err != nil {
			return fmt.Errorf("deposit %d: %w", i, err)
		}
	}
	return nil
}

// depositValidatorSet is a minimal validator tracking structure for deposits.
type depositValidatorSet struct {
	balances map[[48]byte]uint64 // pubkey -> balance in Gwei
	count    uint64
}

// NewDepositValidatorSet creates a new deposit validator set.
func NewDepositValidatorSet() *depositValidatorSet {
	return &depositValidatorSet{
		balances: make(map[[48]byte]uint64),
	}
}

// ApplyDeposit applies a single deposit, creating a new validator or
// topping up an existing one.
func (vs *depositValidatorSet) ApplyDeposit(dep *types.DepositRequest) error {
	existing, ok := vs.balances[dep.Pubkey]
	if ok {
		// Top-up existing validator.
		vs.balances[dep.Pubkey] = existing + dep.Amount
	} else {
		// New validator.
		vs.balances[dep.Pubkey] = dep.Amount
		vs.count++
	}
	return nil
}

// GetBalance returns the balance for a validator pubkey.
func (vs *depositValidatorSet) GetBalance(pubkey [48]byte) (uint64, bool) {
	bal, ok := vs.balances[pubkey]
	return bal, ok
}

// Count returns the number of validators.
func (vs *depositValidatorSet) Count() uint64 {
	return vs.count
}

// BuildDepositLogData constructs ABI-encoded DepositEvent log data.
// This is useful for testing and for building deposit contract logs.
func BuildDepositLogData(dep *types.DepositRequest) []byte {
	// ABI encoding: 5 dynamic fields, each with offset + length + data (padded to 32).
	// Offsets: 5 * 32 = 160 bytes
	// Then each field: 32 (length) + ceil(dataLen/32)*32 (padded data)
	offsets := make([]byte, 160)

	// Calculate data section offsets.
	fieldSizes := []int{48, 32, 8, 96, 8}
	currentOffset := 160 // start of data section

	for i := 0; i < 5; i++ {
		binary.BigEndian.PutUint64(offsets[i*32+24:], uint64(currentOffset))
		paddedLen := ((fieldSizes[i] + 31) / 32) * 32
		currentOffset += 32 + paddedLen // 32 for length word + padded data
	}

	var buf []byte
	buf = append(buf, offsets...)

	appendField := func(data []byte) {
		// Length word (32 bytes, big-endian).
		lenWord := make([]byte, 32)
		binary.BigEndian.PutUint64(lenWord[24:], uint64(len(data)))
		buf = append(buf, lenWord...)

		// Padded data.
		paddedLen := ((len(data) + 31) / 32) * 32
		padded := make([]byte, paddedLen)
		copy(padded, data)
		buf = append(buf, padded...)
	}

	// Field 0: pubkey
	appendField(dep.Pubkey[:])

	// Field 1: withdrawal_credentials
	appendField(dep.WithdrawalCredentials[:])

	// Field 2: amount (8 bytes LE)
	amountBytes := make([]byte, 8)
	binary.LittleEndian.PutUint64(amountBytes, dep.Amount)
	appendField(amountBytes)

	// Field 3: signature
	appendField(dep.Signature[:])

	// Field 4: index (8 bytes LE)
	indexBytes := make([]byte, 8)
	binary.LittleEndian.PutUint64(indexBytes, dep.Index)
	appendField(indexBytes)

	return buf
}
