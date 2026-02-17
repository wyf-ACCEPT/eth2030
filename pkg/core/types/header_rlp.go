package types

import (
	"math/big"

	"github.com/eth2028/eth2028/rlp"
	"golang.org/x/crypto/sha3"
)

// EncodeRLP returns the RLP encoding of the header in Yellow Paper field order:
// [ParentHash, UncleHash, Coinbase, Root, TxHash, ReceiptHash, Bloom,
//
//	Difficulty, Number, GasLimit, GasUsed, Time, Extra, MixDigest, Nonce,
//	BaseFee, WithdrawalsHash, BlobGasUsed, ExcessBlobGas, ParentBeaconRoot, RequestsHash]
//
// Optional fields are appended only if non-nil (and all preceding optionals are present).
func (h *Header) EncodeRLP() ([]byte, error) {
	// Encode the base 15 fields as an RLP list manually to control ordering.
	var items []interface{}

	items = append(items, h.ParentHash)
	items = append(items, h.UncleHash)
	items = append(items, h.Coinbase)
	items = append(items, h.Root)
	items = append(items, h.TxHash)
	items = append(items, h.ReceiptHash)
	items = append(items, h.Bloom)
	items = append(items, bigIntOrZero(h.Difficulty))
	items = append(items, bigIntOrZero(h.Number))
	items = append(items, h.GasLimit)
	items = append(items, h.GasUsed)
	items = append(items, h.Time)
	items = append(items, h.Extra)
	items = append(items, h.MixDigest)
	items = append(items, h.Nonce)

	// EIP-1559: BaseFee
	if h.BaseFee != nil {
		items = append(items, h.BaseFee)
	}
	// EIP-4895: WithdrawalsHash
	if h.WithdrawalsHash != nil {
		items = append(items, *h.WithdrawalsHash)
	}
	// EIP-4844: BlobGasUsed, ExcessBlobGas
	if h.BlobGasUsed != nil {
		items = append(items, *h.BlobGasUsed)
	}
	if h.ExcessBlobGas != nil {
		items = append(items, *h.ExcessBlobGas)
	}
	// EIP-4788: ParentBeaconBlockRoot
	if h.ParentBeaconRoot != nil {
		items = append(items, *h.ParentBeaconRoot)
	}
	// EIP-7685: RequestsHash
	if h.RequestsHash != nil {
		items = append(items, *h.RequestsHash)
	}

	return encodeRLPList(items)
}

// encodeRLPList encodes a list of items as an RLP list by encoding each item
// and wrapping the concatenated payload.
func encodeRLPList(items []interface{}) ([]byte, error) {
	var payload []byte
	for _, item := range items {
		enc, err := rlp.EncodeToBytes(item)
		if err != nil {
			return nil, err
		}
		payload = append(payload, enc...)
	}
	return rlp.WrapList(payload), nil
}

// bigIntOrZero returns v if non-nil, otherwise a zero big.Int.
func bigIntOrZero(v *big.Int) *big.Int {
	if v == nil {
		return new(big.Int)
	}
	return v
}

// DecodeHeaderRLP decodes an RLP-encoded header.
func DecodeHeaderRLP(data []byte) (*Header, error) {
	s := rlp.NewStreamFromBytes(data)
	_, err := s.List()
	if err != nil {
		return nil, err
	}

	h := &Header{}

	// 15 base fields
	if err := decodeHash(s, &h.ParentHash); err != nil {
		return nil, err
	}
	if err := decodeHash(s, &h.UncleHash); err != nil {
		return nil, err
	}
	if err := decodeAddress(s, &h.Coinbase); err != nil {
		return nil, err
	}
	if err := decodeHash(s, &h.Root); err != nil {
		return nil, err
	}
	if err := decodeHash(s, &h.TxHash); err != nil {
		return nil, err
	}
	if err := decodeHash(s, &h.ReceiptHash); err != nil {
		return nil, err
	}
	if err := decodeBloom(s, &h.Bloom); err != nil {
		return nil, err
	}

	h.Difficulty, err = s.BigInt()
	if err != nil {
		return nil, err
	}
	h.Number, err = s.BigInt()
	if err != nil {
		return nil, err
	}
	h.GasLimit, err = s.Uint64()
	if err != nil {
		return nil, err
	}
	h.GasUsed, err = s.Uint64()
	if err != nil {
		return nil, err
	}
	h.Time, err = s.Uint64()
	if err != nil {
		return nil, err
	}
	h.Extra, err = s.Bytes()
	if err != nil {
		return nil, err
	}
	if err := decodeHash(s, &h.MixDigest); err != nil {
		return nil, err
	}
	if err := decodeBlockNonce(s, &h.Nonce); err != nil {
		return nil, err
	}

	// Optional fields: try reading each in sequence. If we hit ListEnd, stop.
	if !s.AtListEnd() {
		h.BaseFee, err = s.BigInt()
		if err != nil {
			return nil, err
		}
	}
	if !s.AtListEnd() {
		var wh Hash
		if err := decodeHash(s, &wh); err != nil {
			return nil, err
		}
		h.WithdrawalsHash = &wh
	}
	if !s.AtListEnd() {
		bgu, err := s.Uint64()
		if err != nil {
			return nil, err
		}
		h.BlobGasUsed = &bgu
	}
	if !s.AtListEnd() {
		ebg, err := s.Uint64()
		if err != nil {
			return nil, err
		}
		h.ExcessBlobGas = &ebg
	}
	if !s.AtListEnd() {
		var pbr Hash
		if err := decodeHash(s, &pbr); err != nil {
			return nil, err
		}
		h.ParentBeaconRoot = &pbr
	}
	if !s.AtListEnd() {
		var rh Hash
		if err := decodeHash(s, &rh); err != nil {
			return nil, err
		}
		h.RequestsHash = &rh
	}

	if err := s.ListEnd(); err != nil {
		return nil, err
	}
	return h, nil
}

// decodeHash reads an RLP string into a Hash.
func decodeHash(s *rlp.Stream, h *Hash) error {
	b, err := s.Bytes()
	if err != nil {
		return err
	}
	copy(h[HashLength-len(b):], b)
	return nil
}

// decodeAddress reads an RLP string into an Address.
func decodeAddress(s *rlp.Stream, a *Address) error {
	b, err := s.Bytes()
	if err != nil {
		return err
	}
	copy(a[AddressLength-len(b):], b)
	return nil
}

// decodeBloom reads an RLP string into a Bloom.
func decodeBloom(s *rlp.Stream, bl *Bloom) error {
	b, err := s.Bytes()
	if err != nil {
		return err
	}
	copy(bl[BloomLength-len(b):], b)
	return nil
}

// decodeBlockNonce reads an RLP string into a BlockNonce.
func decodeBlockNonce(s *rlp.Stream, n *BlockNonce) error {
	b, err := s.Bytes()
	if err != nil {
		return err
	}
	copy(n[NonceLength-len(b):], b)
	return nil
}

// computeHeaderHash computes the Keccak-256 hash of the RLP-encoded header.
func computeHeaderHash(h *Header) Hash {
	enc, err := h.EncodeRLP()
	if err != nil {
		return Hash{}
	}
	d := sha3.NewLegacyKeccak256()
	d.Write(enc)
	var hash Hash
	copy(hash[:], d.Sum(nil))
	return hash
}
