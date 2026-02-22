// Package geth provides an adapter layer between eth2028's type system and
// go-ethereum's execution engine. This is the only package that imports
// go-ethereum directly; all other eth2028 packages use eth2028/core/types.
package geth

import (
	"math/big"

	gethcommon "github.com/ethereum/go-ethereum/common"
	gethtypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/holiman/uint256"

	"github.com/eth2028/eth2028/core/types"
)

// --- Address and Hash conversion (zero-copy, layout-compatible) ---

// ToGethAddress converts an eth2028 Address to a go-ethereum Address.
func ToGethAddress(a types.Address) gethcommon.Address {
	return gethcommon.Address(a)
}

// FromGethAddress converts a go-ethereum Address to an eth2028 Address.
func FromGethAddress(a gethcommon.Address) types.Address {
	return types.Address(a)
}

// ToGethHash converts an eth2028 Hash to a go-ethereum Hash.
func ToGethHash(h types.Hash) gethcommon.Hash {
	return gethcommon.Hash(h)
}

// FromGethHash converts a go-ethereum Hash to an eth2028 Hash.
func FromGethHash(h gethcommon.Hash) types.Hash {
	return types.Hash(h)
}

// --- Balance conversion ---

// ToUint256 converts *big.Int to *uint256.Int for go-ethereum balance operations.
func ToUint256(b *big.Int) *uint256.Int {
	if b == nil {
		return new(uint256.Int)
	}
	u, _ := uint256.FromBig(b)
	return u
}

// FromUint256 converts *uint256.Int to *big.Int.
func FromUint256(u *uint256.Int) *big.Int {
	if u == nil {
		return new(big.Int)
	}
	return u.ToBig()
}

// --- AccessList conversion ---

// ToGethAccessList converts eth2028 AccessList to go-ethereum AccessList.
func ToGethAccessList(al types.AccessList) gethtypes.AccessList {
	if al == nil {
		return nil
	}
	result := make(gethtypes.AccessList, len(al))
	for i, tuple := range al {
		keys := make([]gethcommon.Hash, len(tuple.StorageKeys))
		for j, k := range tuple.StorageKeys {
			keys[j] = ToGethHash(k)
		}
		result[i] = gethtypes.AccessTuple{
			Address:     ToGethAddress(tuple.Address),
			StorageKeys: keys,
		}
	}
	return result
}

// --- Log conversion ---

// FromGethLog converts a go-ethereum Log to an eth2028 Log.
func FromGethLog(l *gethtypes.Log) *types.Log {
	if l == nil {
		return nil
	}
	topics := make([]types.Hash, len(l.Topics))
	for i, t := range l.Topics {
		topics[i] = FromGethHash(t)
	}
	return &types.Log{
		Address:     FromGethAddress(l.Address),
		Topics:      topics,
		Data:        l.Data,
		BlockNumber: l.BlockNumber,
		TxHash:      FromGethHash(l.TxHash),
		TxIndex:     l.TxIndex,
		BlockHash:   FromGethHash(l.BlockHash),
		Index:       l.Index,
		Removed:     l.Removed,
	}
}

// FromGethLogs converts a slice of go-ethereum Logs.
func FromGethLogs(logs []*gethtypes.Log) []*types.Log {
	result := make([]*types.Log, len(logs))
	for i, l := range logs {
		result[i] = FromGethLog(l)
	}
	return result
}
