package core

import (
	"math/big"

	"github.com/eth2028/eth2028/core/types"
)

// Message represents a transaction message prepared for EVM execution.
type Message struct {
	From       types.Address
	To         *types.Address // nil for contract creation
	Nonce      uint64
	Value      *big.Int
	GasLimit   uint64
	GasPrice   *big.Int
	GasFeeCap  *big.Int
	GasTipCap  *big.Int
	Data       []byte
	AccessList types.AccessList
	BlobHashes []types.Hash
	AuthList   []types.Authorization // EIP-7702 authorization list for SetCode transactions
	TxType     uint8                 // transaction type (for fork-specific processing)
}

// TransactionToMessage converts a transaction into a Message for execution.
// If the transaction has a cached sender (via SetSender), it is used.
// Otherwise the From field must be set by the caller after signature recovery.
func TransactionToMessage(tx *types.Transaction) Message {
	msg := Message{
		Nonce:      tx.Nonce(),
		GasLimit:   tx.Gas(),
		GasPrice:   tx.GasPrice(),
		GasFeeCap:  tx.GasFeeCap(),
		GasTipCap:  tx.GasTipCap(),
		Data:       tx.Data(),
		AccessList: tx.AccessList(),
		BlobHashes: tx.BlobHashes(),
		AuthList:   tx.AuthorizationList(),
		TxType:     tx.Type(),
	}
	if sender := tx.Sender(); sender != nil {
		msg.From = *sender
	}
	if tx.To() != nil {
		to := *tx.To()
		msg.To = &to
	}
	if tx.Value() != nil {
		msg.Value = new(big.Int).Set(tx.Value())
	} else {
		msg.Value = new(big.Int)
	}
	return msg
}
