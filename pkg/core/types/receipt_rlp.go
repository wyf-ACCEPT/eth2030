package types

import (
	"github.com/eth2030/eth2030/rlp"
	"golang.org/x/crypto/sha3"
)

// EncodeRLP returns the RLP encoding of the receipt's consensus fields:
// [Status, CumulativeGasUsed, Bloom, Logs].
// For typed receipts (Type > 0), the encoding is prefixed with the type byte.
func (r *Receipt) EncodeRLP() ([]byte, error) {
	// Encode logs as a list of [Address, Topics, Data].
	var logsPayload []byte
	for _, log := range r.Logs {
		enc, err := encodeLog(log)
		if err != nil {
			return nil, err
		}
		logsPayload = append(logsPayload, enc...)
	}

	items := []interface{}{
		r.Status,
		r.CumulativeGasUsed,
		r.Bloom,
	}

	var payload []byte
	for _, item := range items {
		enc, err := rlp.EncodeToBytes(item)
		if err != nil {
			return nil, err
		}
		payload = append(payload, enc...)
	}
	// Append the logs list.
	payload = append(payload, rlp.WrapList(logsPayload)...)

	encoded := rlp.WrapList(payload)

	// For typed receipts, prepend the type byte.
	if r.Type != 0 {
		typed := make([]byte, 1+len(encoded))
		typed[0] = r.Type
		copy(typed[1:], encoded)
		return typed, nil
	}
	return encoded, nil
}

// encodeLog RLP-encodes a single log as [Address, [Topic1, Topic2, ...], Data].
func encodeLog(l *Log) ([]byte, error) {
	addrEnc, err := rlp.EncodeToBytes(l.Address)
	if err != nil {
		return nil, err
	}

	var topicsPayload []byte
	for _, t := range l.Topics {
		enc, err := rlp.EncodeToBytes(t)
		if err != nil {
			return nil, err
		}
		topicsPayload = append(topicsPayload, enc...)
	}

	dataEnc, err := rlp.EncodeToBytes(l.Data)
	if err != nil {
		return nil, err
	}

	var payload []byte
	payload = append(payload, addrEnc...)
	payload = append(payload, rlp.WrapList(topicsPayload)...)
	payload = append(payload, dataEnc...)
	return rlp.WrapList(payload), nil
}

// DecodeReceiptRLP decodes an RLP-encoded receipt.
func DecodeReceiptRLP(data []byte) (*Receipt, error) {
	r := &Receipt{}

	// Check for typed receipt (non-list prefix byte).
	if len(data) > 0 && data[0] < 0x80 {
		r.Type = data[0]
		data = data[1:]
	}

	s := rlp.NewStreamFromBytes(data)
	_, err := s.List()
	if err != nil {
		return nil, err
	}

	r.Status, err = s.Uint64()
	if err != nil {
		return nil, err
	}
	r.CumulativeGasUsed, err = s.Uint64()
	if err != nil {
		return nil, err
	}
	if err := decodeBloom(s, &r.Bloom); err != nil {
		return nil, err
	}

	// Decode logs list.
	_, err = s.List()
	if err != nil {
		return nil, err
	}
	for !s.AtListEnd() {
		log, err := decodeLog(s)
		if err != nil {
			return nil, err
		}
		r.Logs = append(r.Logs, log)
	}
	if err := s.ListEnd(); err != nil {
		return nil, err
	}

	if err := s.ListEnd(); err != nil {
		return nil, err
	}
	return r, nil
}

// decodeLog decodes a single log from the stream.
func decodeLog(s *rlp.Stream) (*Log, error) {
	_, err := s.List()
	if err != nil {
		return nil, err
	}

	l := &Log{}
	if err := decodeAddress(s, &l.Address); err != nil {
		return nil, err
	}

	// Decode topics list.
	_, err = s.List()
	if err != nil {
		return nil, err
	}
	for !s.AtListEnd() {
		var topic Hash
		if err := decodeHash(s, &topic); err != nil {
			return nil, err
		}
		l.Topics = append(l.Topics, topic)
	}
	if err := s.ListEnd(); err != nil {
		return nil, err
	}

	l.Data, err = s.Bytes()
	if err != nil {
		return nil, err
	}

	if err := s.ListEnd(); err != nil {
		return nil, err
	}
	return l, nil
}

// DeriveSha computes a simple receipt root hash by hashing the ordered
// concatenation of RLP-encoded receipts. This is a simplified implementation;
// a full Ethereum node would use a Merkle Patricia Trie.
func DeriveSha(receipts []*Receipt) Hash {
	d := sha3.NewLegacyKeccak256()
	for _, r := range receipts {
		enc, err := r.EncodeRLP()
		if err != nil {
			continue
		}
		d.Write(enc)
	}
	var h Hash
	copy(h[:], d.Sum(nil))
	return h
}
