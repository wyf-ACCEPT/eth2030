package types

import (
	"errors"
	"fmt"
	"math/big"

	"github.com/eth2030/eth2030/rlp"
	"golang.org/x/crypto/sha3"
)

// EIP-4844 blob transaction utility functions.
//
// The BlobTx struct and BlobTxType constant are defined in transaction.go.
// Blob gas constants (BlobTxBlobGasPerBlob, TargetBlobGasPerBlock,
// MaxBlobGasPerBlock, VersionedHashVersionKZG) and the CalcExcessBlobGas /
// CalcBlobFee functions are defined in blob_gas.go.
//
// This file provides constructors, hashing, validation, encoding/decoding
// helpers, and gas-price calculation for blob transactions.

// MaxBlobsPerBlock is the maximum number of blobs per block.
const MaxBlobsPerBlock = MaxBlobGasPerBlock / BlobTxBlobGasPerBlob // 6

var (
	errNoBlobHashes         = errors.New("blob transaction must have at least one blob hash")
	errTooManyBlobs         = errors.New("blob transaction exceeds max blobs per block")
	errInvalidVersionedHash = errors.New("blob versioned hash must start with 0x01")
	errNilTo                = errors.New("blob transaction must have a non-nil To address")
	errBlobTxDecode         = errors.New("failed to decode blob transaction")
)

// NewBlobTx creates a new BlobTx with the given parameters.
// Blob transactions always require a non-nil To address.
func NewBlobTx(chainID, nonce, gas uint64, to *Address, value, maxFee, maxPriority, maxBlobFee *big.Int, data []byte, blobHashes []Hash) *BlobTx {
	tx := &BlobTx{
		ChainID:    new(big.Int).SetUint64(chainID),
		Nonce:      nonce,
		GasTipCap:  new(big.Int),
		GasFeeCap:  new(big.Int),
		Gas:        gas,
		Value:      new(big.Int),
		BlobFeeCap: new(big.Int),
	}
	if maxPriority != nil {
		tx.GasTipCap.Set(maxPriority)
	}
	if maxFee != nil {
		tx.GasFeeCap.Set(maxFee)
	}
	if value != nil {
		tx.Value.Set(value)
	}
	if maxBlobFee != nil {
		tx.BlobFeeCap.Set(maxBlobFee)
	}
	if to != nil {
		tx.To = *to
	}
	if data != nil {
		tx.Data = make([]byte, len(data))
		copy(tx.Data, data)
	}
	if blobHashes != nil {
		tx.BlobHashes = make([]Hash, len(blobHashes))
		copy(tx.BlobHashes, blobHashes)
	}
	return tx
}

// BlobTxHash computes the transaction hash of a blob transaction.
// hash = keccak256(0x03 || RLP([chainID, nonce, ...]))
func BlobTxHash(tx *BlobTx) Hash {
	enc := blobTxRLP{
		ChainID:    bigOrZero(tx.ChainID),
		Nonce:      tx.Nonce,
		GasTipCap:  bigOrZero(tx.GasTipCap),
		GasFeeCap:  bigOrZero(tx.GasFeeCap),
		Gas:        tx.Gas,
		To:         tx.To,
		Value:      bigOrZero(tx.Value),
		Data:       tx.Data,
		AccessList: encodeAccessList(tx.AccessList),
		BlobFeeCap: bigOrZero(tx.BlobFeeCap),
		BlobHashes: tx.BlobHashes,
		V:          bigOrZero(tx.V),
		R:          bigOrZero(tx.R),
		S:          bigOrZero(tx.S),
	}
	payload, err := rlp.EncodeToBytes(enc)
	if err != nil {
		return Hash{}
	}
	d := sha3.NewLegacyKeccak256()
	d.Write([]byte{BlobTxType})
	d.Write(payload)
	var h Hash
	copy(h[:], d.Sum(nil))
	return h
}

// BlobGas returns the total blob gas consumed by a blob transaction.
// Each blob uses BlobTxBlobGasPerBlob (131072) gas.
func BlobGas(tx *BlobTx) uint64 {
	return uint64(len(tx.BlobHashes)) * BlobTxBlobGasPerBlob
}

// ValidateBlobVersionedHashes checks that every versioned hash in the
// transaction starts with VersionedHashVersionKZG (0x01).
func ValidateBlobVersionedHashes(tx *BlobTx) error {
	if len(tx.BlobHashes) == 0 {
		return errNoBlobHashes
	}
	if len(tx.BlobHashes) > MaxBlobsPerBlock {
		return errTooManyBlobs
	}
	for i, h := range tx.BlobHashes {
		if h[0] != VersionedHashVersionKZG {
			return fmt.Errorf("blob hash %d: %w (got 0x%02x)", i, errInvalidVersionedHash, h[0])
		}
	}
	return nil
}

// EncodeBlobTx encodes a blob transaction to its wire format:
// 0x03 || RLP([fields...])
func EncodeBlobTx(tx *BlobTx) ([]byte, error) {
	enc := blobTxRLP{
		ChainID:    bigOrZero(tx.ChainID),
		Nonce:      tx.Nonce,
		GasTipCap:  bigOrZero(tx.GasTipCap),
		GasFeeCap:  bigOrZero(tx.GasFeeCap),
		Gas:        tx.Gas,
		To:         tx.To,
		Value:      bigOrZero(tx.Value),
		Data:       tx.Data,
		AccessList: encodeAccessList(tx.AccessList),
		BlobFeeCap: bigOrZero(tx.BlobFeeCap),
		BlobHashes: tx.BlobHashes,
		V:          bigOrZero(tx.V),
		R:          bigOrZero(tx.R),
		S:          bigOrZero(tx.S),
	}
	payload, err := rlp.EncodeToBytes(enc)
	if err != nil {
		return nil, fmt.Errorf("encode blob tx: %w", err)
	}
	result := make([]byte, 1+len(payload))
	result[0] = BlobTxType
	copy(result[1:], payload)
	return result, nil
}

// DecodeBlobTx decodes a blob transaction from wire format bytes.
// The first byte must be BlobTxType (0x03).
func DecodeBlobTx(data []byte) (*BlobTx, error) {
	if len(data) < 2 {
		return nil, errBlobTxDecode
	}
	if data[0] != BlobTxType {
		return nil, fmt.Errorf("expected blob tx type 0x03, got 0x%02x", data[0])
	}
	var dec blobTxRLP
	if err := rlp.DecodeBytes(data[1:], &dec); err != nil {
		return nil, fmt.Errorf("decode blob tx: %w", err)
	}
	return &BlobTx{
		ChainID:    dec.ChainID,
		Nonce:      dec.Nonce,
		GasTipCap:  dec.GasTipCap,
		GasFeeCap:  dec.GasFeeCap,
		Gas:        dec.Gas,
		To:         dec.To,
		Value:      dec.Value,
		Data:       dec.Data,
		AccessList: decodeAccessList(dec.AccessList),
		BlobFeeCap: dec.BlobFeeCap,
		BlobHashes: dec.BlobHashes,
		V:          dec.V,
		R:          dec.R,
		S:          dec.S,
	}, nil
}

// BlobGasPrice calculates the blob gas price from the excess blob gas
// using the EIP-4844 fake_exponential formula.
// This is a convenience wrapper around CalcBlobFee from blob_gas.go.
func BlobGasPrice(excessBlobGas uint64) *big.Int {
	return CalcBlobFee(excessBlobGas)
}
