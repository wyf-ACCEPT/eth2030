package types

import (
	"fmt"

	"github.com/eth2028/eth2028/rlp"
)

// EncodeRLP returns the RLP encoding of the block: [header, [tx1, tx2, ...], [uncle1, uncle2, ...]].
func (b *Block) EncodeRLP() ([]byte, error) {
	headerEnc, err := b.header.EncodeRLP()
	if err != nil {
		return nil, fmt.Errorf("encoding header: %w", err)
	}

	// Encode transactions list using proper Transaction RLP encoding.
	var txsPayload []byte
	for i, tx := range b.body.Transactions {
		txEnc, err := tx.EncodeRLP()
		if err != nil {
			return nil, fmt.Errorf("encoding tx %d: %w", i, err)
		}
		// Wrap the raw tx bytes as an RLP byte string.
		wrapped, err := rlp.EncodeToBytes(txEnc)
		if err != nil {
			return nil, fmt.Errorf("wrapping tx %d: %w", i, err)
		}
		txsPayload = append(txsPayload, wrapped...)
	}

	// Encode uncles list.
	var unclesPayload []byte
	for _, uncle := range b.body.Uncles {
		uncleEnc, err := uncle.EncodeRLP()
		if err != nil {
			return nil, fmt.Errorf("encoding uncle: %w", err)
		}
		unclesPayload = append(unclesPayload, uncleEnc...)
	}

	var blockPayload []byte
	blockPayload = append(blockPayload, headerEnc...)
	blockPayload = append(blockPayload, rlp.WrapList(txsPayload)...)
	blockPayload = append(blockPayload, rlp.WrapList(unclesPayload)...)

	return rlp.WrapList(blockPayload), nil
}

// DecodeBlockRLP decodes an RLP-encoded block.
func DecodeBlockRLP(data []byte) (*Block, error) {
	s := rlp.NewStreamFromBytes(data)
	_, err := s.List()
	if err != nil {
		return nil, fmt.Errorf("opening block list: %w", err)
	}

	// Decode header.
	headerBytes, err := s.RawItem()
	if err != nil {
		return nil, fmt.Errorf("reading header: %w", err)
	}
	header, err := DecodeHeaderRLP(headerBytes)
	if err != nil {
		return nil, fmt.Errorf("decoding header: %w", err)
	}

	// Decode transactions list.
	_, err = s.List()
	if err != nil {
		return nil, fmt.Errorf("opening txs list: %w", err)
	}
	var txs []*Transaction
	for !s.AtListEnd() {
		txBytes, err := s.Bytes()
		if err != nil {
			return nil, fmt.Errorf("reading tx bytes: %w", err)
		}
		tx, err := DecodeTxRLP(txBytes)
		if err != nil {
			return nil, fmt.Errorf("decoding tx: %w", err)
		}
		txs = append(txs, tx)
	}
	if err := s.ListEnd(); err != nil {
		return nil, fmt.Errorf("closing txs list: %w", err)
	}

	// Decode uncles list.
	_, err = s.List()
	if err != nil {
		return nil, fmt.Errorf("opening uncles list: %w", err)
	}
	var uncles []*Header
	for !s.AtListEnd() {
		uncleBytes, err := s.RawItem()
		if err != nil {
			return nil, fmt.Errorf("reading uncle: %w", err)
		}
		uncle, err := DecodeHeaderRLP(uncleBytes)
		if err != nil {
			return nil, fmt.Errorf("decoding uncle: %w", err)
		}
		uncles = append(uncles, uncle)
	}
	if err := s.ListEnd(); err != nil {
		return nil, fmt.Errorf("closing uncles list: %w", err)
	}

	if err := s.ListEnd(); err != nil {
		return nil, fmt.Errorf("closing block list: %w", err)
	}

	block := &Block{header: header}
	block.body.Transactions = txs
	block.body.Uncles = uncles
	return block, nil
}
