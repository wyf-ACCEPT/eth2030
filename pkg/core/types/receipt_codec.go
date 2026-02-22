package types

import (
	"errors"
	"math/big"

	"github.com/eth2030/eth2030/rlp"
)

// ReceiptCodec provides encoding/decoding for receipts with EIP-2718 typed
// receipt support.
type ReceiptCodec struct{}

// EncodeReceipt encodes a single receipt (typed or legacy) to bytes.
// For legacy receipts (Type == 0), this returns the plain RLP encoding.
// For typed receipts, this prepends the type byte to the RLP payload.
func (rc *ReceiptCodec) EncodeReceipt(receipt *Receipt) ([]byte, error) {
	if receipt == nil {
		return nil, errors.New("receipt_codec: nil receipt")
	}
	return receipt.EncodeRLP()
}

// DecodeReceipt decodes a single receipt from bytes.
func (rc *ReceiptCodec) DecodeReceipt(data []byte) (*Receipt, error) {
	if len(data) == 0 {
		return nil, errors.New("receipt_codec: empty data")
	}
	return DecodeReceiptRLP(data)
}

// EncodeReceipts batch-encodes a slice of receipts as an RLP list.
// Each receipt is individually encoded (with type prefix for typed receipts),
// then the set is wrapped in an outer RLP list as raw byte strings.
func (rc *ReceiptCodec) EncodeReceipts(receipts []*Receipt) ([]byte, error) {
	if receipts == nil {
		return rlp.WrapList(nil), nil
	}

	var payload []byte
	for i, r := range receipts {
		if r == nil {
			return nil, errors.New("receipt_codec: nil receipt in batch")
		}
		enc, err := r.EncodeRLP()
		if err != nil {
			return nil, err
		}
		// Each encoded receipt is stored as an RLP byte string in the list.
		item, err := rlp.EncodeToBytes(enc)
		if err != nil {
			_ = i
			return nil, err
		}
		payload = append(payload, item...)
	}
	return rlp.WrapList(payload), nil
}

// DecodeReceipts batch-decodes a slice of receipts from an RLP list.
func (rc *ReceiptCodec) DecodeReceipts(data []byte) ([]*Receipt, error) {
	if len(data) == 0 {
		return nil, errors.New("receipt_codec: empty data")
	}

	s := rlp.NewStreamFromBytes(data)
	_, err := s.List()
	if err != nil {
		return nil, err
	}

	var receipts []*Receipt
	for !s.AtListEnd() {
		// Each item in the list is an RLP-encoded byte string containing
		// the individual receipt encoding.
		itemBytes, err := s.Bytes()
		if err != nil {
			return nil, err
		}
		r, err := DecodeReceiptRLP(itemBytes)
		if err != nil {
			return nil, err
		}
		receipts = append(receipts, r)
	}
	if err := s.ListEnd(); err != nil {
		return nil, err
	}
	return receipts, nil
}

// DeriveReceiptCodecFields populates derived fields on a list of receipts
// after block processing. It sets block context and cumulative gas tracking.
// This is a simplified version that does not require transactions.
func DeriveReceiptCodecFields(receipts []*Receipt, blockHash Hash, blockNumber uint64) {
	var logIndex uint
	for i, receipt := range receipts {
		receipt.BlockHash = blockHash
		receipt.BlockNumber = new(big.Int).SetUint64(blockNumber)
		receipt.TransactionIndex = uint(i)

		for _, log := range receipt.Logs {
			log.BlockHash = blockHash
			log.BlockNumber = blockNumber
			log.TxIndex = uint(i)
			log.Index = logIndex
			logIndex++
		}
	}
}

// ReceiptEqual compares two receipts for equality on their consensus fields:
// Type, Status, CumulativeGasUsed, Bloom, and Logs.
func ReceiptEqual(a, b *Receipt) bool {
	if a == nil && b == nil {
		return true
	}
	if a == nil || b == nil {
		return false
	}

	if a.Type != b.Type {
		return false
	}
	if a.Status != b.Status {
		return false
	}
	if a.CumulativeGasUsed != b.CumulativeGasUsed {
		return false
	}
	if a.Bloom != b.Bloom {
		return false
	}

	if len(a.Logs) != len(b.Logs) {
		return false
	}
	for i := range a.Logs {
		if !logEqual(a.Logs[i], b.Logs[i]) {
			return false
		}
	}
	return true
}

// logEqual compares two logs for equality on their consensus fields.
func logEqual(a, b *Log) bool {
	if a == nil && b == nil {
		return true
	}
	if a == nil || b == nil {
		return false
	}
	if a.Address != b.Address {
		return false
	}
	if len(a.Topics) != len(b.Topics) {
		return false
	}
	for i := range a.Topics {
		if a.Topics[i] != b.Topics[i] {
			return false
		}
	}
	if len(a.Data) != len(b.Data) {
		return false
	}
	for i := range a.Data {
		if a.Data[i] != b.Data[i] {
			return false
		}
	}
	return true
}

// ReceiptSize estimates the byte size of a receipt by encoding it.
// Returns 0 if encoding fails.
func ReceiptSize(receipt *Receipt) int {
	if receipt == nil {
		return 0
	}
	enc, err := receipt.EncodeRLP()
	if err != nil {
		return 0
	}
	return len(enc)
}
