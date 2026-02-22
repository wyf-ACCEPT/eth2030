package types

import (
	"errors"
	"fmt"
	"math/big"

	"github.com/eth2030/eth2030/rlp"
	"golang.org/x/crypto/sha3"
)

var (
	errUnknownTxType = errors.New("unknown transaction type")
	errShortTypedTx  = errors.New("typed transaction too short")
)

// ---- RLP helper structs (field order matches Ethereum consensus spec) ----

// legacyTxRLP is the RLP encoding layout for LegacyTx.
// Fields: [nonce, gasPrice, gasLimit, to, value, data, v, r, s]
type legacyTxRLP struct {
	Nonce    uint64
	GasPrice *big.Int
	Gas      uint64
	To       []byte // empty for contract creation, 20 bytes otherwise
	Value    *big.Int
	Data     []byte
	V        *big.Int
	R        *big.Int
	S        *big.Int
}

// accessListTxRLP is the RLP encoding layout for AccessListTx (EIP-2930).
// Fields: [chainID, nonce, gasPrice, gasLimit, to, value, data, accessList, v, r, s]
type accessListTxRLP struct {
	ChainID    *big.Int
	Nonce      uint64
	GasPrice   *big.Int
	Gas        uint64
	To         []byte
	Value      *big.Int
	Data       []byte
	AccessList []accessTupleRLP
	V          *big.Int
	R          *big.Int
	S          *big.Int
}

// dynamicFeeTxRLP is the RLP encoding layout for DynamicFeeTx (EIP-1559).
// Fields: [chainID, nonce, maxPriorityFeePerGas, maxFeePerGas, gasLimit, to, value, data, accessList, v, r, s]
type dynamicFeeTxRLP struct {
	ChainID    *big.Int
	Nonce      uint64
	GasTipCap  *big.Int
	GasFeeCap  *big.Int
	Gas        uint64
	To         []byte
	Value      *big.Int
	Data       []byte
	AccessList []accessTupleRLP
	V          *big.Int
	R          *big.Int
	S          *big.Int
}

// blobTxRLP is the RLP encoding layout for BlobTx (EIP-4844).
// Fields: [chainID, nonce, maxPriorityFeePerGas, maxFeePerGas, gasLimit, to, value, data, accessList, maxFeePerBlobGas, blobVersionedHashes, v, r, s]
type blobTxRLP struct {
	ChainID    *big.Int
	Nonce      uint64
	GasTipCap  *big.Int
	GasFeeCap  *big.Int
	Gas        uint64
	To         Address
	Value      *big.Int
	Data       []byte
	AccessList []accessTupleRLP
	BlobFeeCap *big.Int
	BlobHashes []Hash
	V          *big.Int
	R          *big.Int
	S          *big.Int
}

// setCodeTxRLP is the RLP encoding layout for SetCodeTx (EIP-7702).
// Fields: [chainID, nonce, maxPriorityFeePerGas, maxFeePerGas, gasLimit, to, value, data, accessList, authorizationList, v, r, s]
type setCodeTxRLP struct {
	ChainID    *big.Int
	Nonce      uint64
	GasTipCap  *big.Int
	GasFeeCap  *big.Int
	Gas        uint64
	To         Address
	Value      *big.Int
	Data       []byte
	AccessList []accessTupleRLP
	AuthList   []authorizationRLP
	V          *big.Int
	R          *big.Int
	S          *big.Int
}

type accessTupleRLP struct {
	Address     Address
	StorageKeys []Hash
}

type authorizationRLP struct {
	ChainID *big.Int
	Address Address
	Nonce   uint64
	V       *big.Int
	R       *big.Int
	S       *big.Int
}

// ---- Encoding ----

// EncodeRLP returns the RLP envelope encoding of the transaction.
// For legacy txs: RLP([nonce, gasPrice, ...])
// For typed txs: type_byte || RLP([chainID, nonce, ...])
func (tx *Transaction) EncodeRLP() ([]byte, error) {
	switch inner := tx.inner.(type) {
	case *LegacyTx:
		return encodeLegacyTx(inner)
	case *AccessListTx:
		return encodeTypedTx(AccessListTxType, inner)
	case *DynamicFeeTx:
		return encodeTypedTx(DynamicFeeTxType, inner)
	case *BlobTx:
		return encodeTypedTx(BlobTxType, inner)
	case *SetCodeTx:
		return encodeTypedTx(SetCodeTxType, inner)
	case *FrameTx:
		return EncodeFrameTx(inner)
	default:
		return nil, errUnknownTxType
	}
}

func encodeLegacyTx(tx *LegacyTx) ([]byte, error) {
	enc := legacyTxRLP{
		Nonce:    tx.Nonce,
		GasPrice: bigOrZero(tx.GasPrice),
		Gas:      tx.Gas,
		To:       addressPtrToBytes(tx.To),
		Value:    bigOrZero(tx.Value),
		Data:     tx.Data,
		V:        bigOrZero(tx.V),
		R:        bigOrZero(tx.R),
		S:        bigOrZero(tx.S),
	}
	return rlp.EncodeToBytes(enc)
}

func encodeTypedTx(txType byte, inner TxData) ([]byte, error) {
	var payload []byte
	var err error

	switch tx := inner.(type) {
	case *AccessListTx:
		enc := accessListTxRLP{
			ChainID:    bigOrZero(tx.ChainID),
			Nonce:      tx.Nonce,
			GasPrice:   bigOrZero(tx.GasPrice),
			Gas:        tx.Gas,
			To:         addressPtrToBytes(tx.To),
			Value:      bigOrZero(tx.Value),
			Data:       tx.Data,
			AccessList: encodeAccessList(tx.AccessList),
			V:          bigOrZero(tx.V),
			R:          bigOrZero(tx.R),
			S:          bigOrZero(tx.S),
		}
		payload, err = rlp.EncodeToBytes(enc)

	case *DynamicFeeTx:
		enc := dynamicFeeTxRLP{
			ChainID:    bigOrZero(tx.ChainID),
			Nonce:      tx.Nonce,
			GasTipCap:  bigOrZero(tx.GasTipCap),
			GasFeeCap:  bigOrZero(tx.GasFeeCap),
			Gas:        tx.Gas,
			To:         addressPtrToBytes(tx.To),
			Value:      bigOrZero(tx.Value),
			Data:       tx.Data,
			AccessList: encodeAccessList(tx.AccessList),
			V:          bigOrZero(tx.V),
			R:          bigOrZero(tx.R),
			S:          bigOrZero(tx.S),
		}
		payload, err = rlp.EncodeToBytes(enc)

	case *BlobTx:
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
		payload, err = rlp.EncodeToBytes(enc)

	case *SetCodeTx:
		enc := setCodeTxRLP{
			ChainID:    bigOrZero(tx.ChainID),
			Nonce:      tx.Nonce,
			GasTipCap:  bigOrZero(tx.GasTipCap),
			GasFeeCap:  bigOrZero(tx.GasFeeCap),
			Gas:        tx.Gas,
			To:         tx.To,
			Value:      bigOrZero(tx.Value),
			Data:       tx.Data,
			AccessList: encodeAccessList(tx.AccessList),
			AuthList:   encodeAuthList(tx.AuthorizationList),
			V:          bigOrZero(tx.V),
			R:          bigOrZero(tx.R),
			S:          bigOrZero(tx.S),
		}
		payload, err = rlp.EncodeToBytes(enc)

	default:
		return nil, errUnknownTxType
	}

	if err != nil {
		return nil, err
	}
	// Prepend type byte.
	result := make([]byte, 1+len(payload))
	result[0] = txType
	copy(result[1:], payload)
	return result, nil
}

// ---- Decoding ----

// DecodeTxRLP decodes an RLP-encoded transaction.
// If the first byte is < 0x7f, it's treated as a typed transaction envelope.
// Otherwise, it's decoded as a legacy RLP list.
func DecodeTxRLP(data []byte) (*Transaction, error) {
	if len(data) == 0 {
		return nil, errors.New("empty transaction data")
	}
	// Typed transaction: first byte is the type (0x01-0x04 are all < 0x7f).
	if data[0] <= 0x7f && data[0] != 0 {
		return decodeTypedTx(data[0], data[1:])
	}
	// Legacy transaction: first byte is an RLP list prefix (>= 0xc0) or type 0.
	// Type 0x00 could be ambiguous; check if it starts with a list prefix.
	if data[0] >= 0xc0 {
		return decodeLegacyTx(data)
	}
	// If first byte is 0x00, it could be a typed legacy tx (type 0).
	// Per EIP-2718, type 0 is not formally an envelope type, but we handle
	// it: strip the 0x00 byte and decode the rest as legacy.
	if data[0] == 0x00 {
		if len(data) < 2 {
			return nil, errShortTypedTx
		}
		return decodeLegacyTx(data[1:])
	}
	return nil, fmt.Errorf("invalid transaction encoding, first byte: 0x%02x", data[0])
}

func decodeLegacyTx(data []byte) (*Transaction, error) {
	var dec legacyTxRLP
	if err := rlp.DecodeBytes(data, &dec); err != nil {
		return nil, fmt.Errorf("decode legacy tx: %w", err)
	}
	inner := &LegacyTx{
		Nonce:    dec.Nonce,
		GasPrice: dec.GasPrice,
		Gas:      dec.Gas,
		To:       bytesToAddressPtr(dec.To),
		Value:    dec.Value,
		Data:     dec.Data,
		V:        dec.V,
		R:        dec.R,
		S:        dec.S,
	}
	return NewTransaction(inner), nil
}

func decodeTypedTx(txType byte, payload []byte) (*Transaction, error) {
	if len(payload) == 0 {
		return nil, errShortTypedTx
	}
	switch txType {
	case AccessListTxType:
		return decodeAccessListTx(payload)
	case DynamicFeeTxType:
		return decodeDynamicFeeTx(payload)
	case BlobTxType:
		return decodeBlobTx(payload)
	case SetCodeTxType:
		return decodeSetCodeTx(payload)
	case FrameTxType:
		return decodeFrameTxWrapped(payload)
	default:
		return nil, fmt.Errorf("unsupported transaction type: 0x%02x", txType)
	}
}

func decodeAccessListTx(data []byte) (*Transaction, error) {
	var dec accessListTxRLP
	if err := rlp.DecodeBytes(data, &dec); err != nil {
		return nil, fmt.Errorf("decode access list tx: %w", err)
	}
	inner := &AccessListTx{
		ChainID:    dec.ChainID,
		Nonce:      dec.Nonce,
		GasPrice:   dec.GasPrice,
		Gas:        dec.Gas,
		To:         bytesToAddressPtr(dec.To),
		Value:      dec.Value,
		Data:       dec.Data,
		AccessList: decodeAccessList(dec.AccessList),
		V:          dec.V,
		R:          dec.R,
		S:          dec.S,
	}
	return NewTransaction(inner), nil
}

func decodeDynamicFeeTx(data []byte) (*Transaction, error) {
	var dec dynamicFeeTxRLP
	if err := rlp.DecodeBytes(data, &dec); err != nil {
		return nil, fmt.Errorf("decode dynamic fee tx: %w", err)
	}
	inner := &DynamicFeeTx{
		ChainID:    dec.ChainID,
		Nonce:      dec.Nonce,
		GasTipCap:  dec.GasTipCap,
		GasFeeCap:  dec.GasFeeCap,
		Gas:        dec.Gas,
		To:         bytesToAddressPtr(dec.To),
		Value:      dec.Value,
		Data:       dec.Data,
		AccessList: decodeAccessList(dec.AccessList),
		V:          dec.V,
		R:          dec.R,
		S:          dec.S,
	}
	return NewTransaction(inner), nil
}

func decodeBlobTx(data []byte) (*Transaction, error) {
	var dec blobTxRLP
	if err := rlp.DecodeBytes(data, &dec); err != nil {
		return nil, fmt.Errorf("decode blob tx: %w", err)
	}
	inner := &BlobTx{
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
	}
	return NewTransaction(inner), nil
}

func decodeSetCodeTx(data []byte) (*Transaction, error) {
	var dec setCodeTxRLP
	if err := rlp.DecodeBytes(data, &dec); err != nil {
		return nil, fmt.Errorf("decode set code tx: %w", err)
	}
	inner := &SetCodeTx{
		ChainID:           dec.ChainID,
		Nonce:             dec.Nonce,
		GasTipCap:         dec.GasTipCap,
		GasFeeCap:         dec.GasFeeCap,
		Gas:               dec.Gas,
		To:                dec.To,
		Value:             dec.Value,
		Data:              dec.Data,
		AccessList:        decodeAccessList(dec.AccessList),
		AuthorizationList: decodeAuthList(dec.AuthList),
		V:                 dec.V,
		R:                 dec.R,
		S:                 dec.S,
	}
	return NewTransaction(inner), nil
}

// ---- Access list / authorization helpers ----

func encodeAccessList(al AccessList) []accessTupleRLP {
	if al == nil {
		return nil
	}
	out := make([]accessTupleRLP, len(al))
	for i, t := range al {
		out[i] = accessTupleRLP{
			Address:     t.Address,
			StorageKeys: t.StorageKeys,
		}
	}
	return out
}

func decodeAccessList(al []accessTupleRLP) AccessList {
	if al == nil {
		return nil
	}
	out := make(AccessList, len(al))
	for i, t := range al {
		out[i] = AccessTuple{
			Address:     t.Address,
			StorageKeys: t.StorageKeys,
		}
	}
	return out
}

func encodeAuthList(auths []Authorization) []authorizationRLP {
	if auths == nil {
		return nil
	}
	out := make([]authorizationRLP, len(auths))
	for i, a := range auths {
		out[i] = authorizationRLP{
			ChainID: bigOrZero(a.ChainID),
			Address: a.Address,
			Nonce:   a.Nonce,
			V:       bigOrZero(a.V),
			R:       bigOrZero(a.R),
			S:       bigOrZero(a.S),
		}
	}
	return out
}

func decodeAuthList(auths []authorizationRLP) []Authorization {
	if auths == nil {
		return nil
	}
	out := make([]Authorization, len(auths))
	for i, a := range auths {
		out[i] = Authorization{
			ChainID: a.ChainID,
			Address: a.Address,
			Nonce:   a.Nonce,
			V:       a.V,
			R:       a.R,
			S:       a.S,
		}
	}
	return out
}

// ---- Address encoding helpers ----

func addressPtrToBytes(a *Address) []byte {
	if a == nil {
		return nil
	}
	return a[:]
}

func bytesToAddressPtr(b []byte) *Address {
	if len(b) == 0 {
		return nil
	}
	a := BytesToAddress(b)
	return &a
}

// bigOrZero returns i if non-nil, otherwise a zero big.Int.
func bigOrZero(i *big.Int) *big.Int {
	if i != nil {
		return i
	}
	return new(big.Int)
}

// ---- Hash using Keccak-256 of RLP encoding ----

// hashRLP computes Keccak-256 of the transaction's RLP envelope encoding.
func (tx *Transaction) hashRLP() Hash {
	enc, err := tx.EncodeRLP()
	if err != nil {
		return Hash{}
	}
	d := sha3.NewLegacyKeccak256()
	d.Write(enc)
	var h Hash
	copy(h[:], d.Sum(nil))
	return h
}

// SigningHash returns the hash that was signed to produce the transaction's signature.
// For legacy (pre-EIP-155): Keccak256(RLP([nonce, gasPrice, gas, to, value, data]))
// For EIP-155 legacy: Keccak256(RLP([nonce, gasPrice, gas, to, value, data, chainID, 0, 0]))
// For typed transactions: Keccak256(type || RLP([fields without v, r, s]))
func (tx *Transaction) SigningHash() Hash {
	switch t := tx.inner.(type) {
	case *LegacyTx:
		return signingHashLegacy(t)
	case *AccessListTx:
		return signingHashAccessList(t)
	case *DynamicFeeTx:
		return signingHashDynamicFee(t)
	case *BlobTx:
		return signingHashBlob(t)
	case *SetCodeTx:
		return signingHashSetCode(t)
	case *FrameTx:
		return ComputeFrameSigHash(t)
	default:
		return Hash{}
	}
}

// signingHashLegacy computes signing hash for legacy transactions.
func signingHashLegacy(tx *LegacyTx) Hash {
	chainID := deriveChainID(tx.V)
	toBytes := make([]byte, 0)
	if tx.To != nil {
		toBytes = tx.To[:]
	}

	var items [][]byte
	enc := func(v interface{}) {
		b, _ := rlp.EncodeToBytes(v)
		items = append(items, b)
	}

	enc(tx.Nonce)
	enc(tx.GasPrice)
	enc(tx.Gas)
	enc(toBytes)
	enc(tx.Value)
	enc(tx.Data)

	if chainID != nil && chainID.Sign() > 0 {
		enc(chainID)
		enc(uint(0))
		enc(uint(0))
	}

	var payload []byte
	for _, item := range items {
		payload = append(payload, item...)
	}
	encoded := rlp.WrapList(payload)

	d := sha3.NewLegacyKeccak256()
	d.Write(encoded)
	var h Hash
	copy(h[:], d.Sum(nil))
	return h
}

// signingHashAccessList computes signing hash for EIP-2930 transactions.
func signingHashAccessList(tx *AccessListTx) Hash {
	toBytes := make([]byte, 0)
	if tx.To != nil {
		toBytes = tx.To[:]
	}
	payload := encodeUnsignedFields(
		tx.ChainID, tx.Nonce, tx.GasPrice, tx.Gas, toBytes, tx.Value, tx.Data,
	)
	payload = append(payload, encodeAccessListBytes(tx.AccessList)...)
	return typedSigningHash(AccessListTxType, payload)
}

// signingHashDynamicFee computes signing hash for EIP-1559 transactions.
func signingHashDynamicFee(tx *DynamicFeeTx) Hash {
	toBytes := make([]byte, 0)
	if tx.To != nil {
		toBytes = tx.To[:]
	}
	payload := encodeUnsignedFields(
		tx.ChainID, tx.Nonce, tx.GasTipCap, tx.GasFeeCap, tx.Gas, toBytes, tx.Value, tx.Data,
	)
	payload = append(payload, encodeAccessListBytes(tx.AccessList)...)
	return typedSigningHash(DynamicFeeTxType, payload)
}

// signingHashBlob computes signing hash for EIP-4844 transactions.
func signingHashBlob(tx *BlobTx) Hash {
	payload := encodeUnsignedFields(
		tx.ChainID, tx.Nonce, tx.GasTipCap, tx.GasFeeCap, tx.Gas, tx.To[:], tx.Value, tx.Data,
	)
	payload = append(payload, encodeAccessListBytes(tx.AccessList)...)
	blobFeeCap, _ := rlp.EncodeToBytes(tx.BlobFeeCap)
	payload = append(payload, blobFeeCap...)
	payload = append(payload, encodeHashList(tx.BlobHashes)...)
	return typedSigningHash(BlobTxType, payload)
}

// signingHashSetCode computes signing hash for EIP-7702 transactions.
func signingHashSetCode(tx *SetCodeTx) Hash {
	payload := encodeUnsignedFields(
		tx.ChainID, tx.Nonce, tx.GasTipCap, tx.GasFeeCap, tx.Gas, tx.To[:], tx.Value, tx.Data,
	)
	payload = append(payload, encodeAccessListBytes(tx.AccessList)...)
	payload = append(payload, encodeAuthListBytes(tx.AuthorizationList)...)
	return typedSigningHash(SetCodeTxType, payload)
}

// encodeUnsignedFields RLP-encodes a sequence of values and concatenates them.
func encodeUnsignedFields(vals ...interface{}) []byte {
	var payload []byte
	for _, v := range vals {
		b, _ := rlp.EncodeToBytes(v)
		payload = append(payload, b...)
	}
	return payload
}

// typedSigningHash computes Keccak256(type || RLP_list(payload)).
func typedSigningHash(txType byte, payload []byte) Hash {
	d := sha3.NewLegacyKeccak256()
	d.Write([]byte{txType})
	d.Write(rlp.WrapList(payload))
	var h Hash
	copy(h[:], d.Sum(nil))
	return h
}

// encodeAccessListBytes RLP-encodes an access list as raw bytes.
func encodeAccessListBytes(list AccessList) []byte {
	var inner []byte
	for _, tuple := range list {
		keysPayload := encodeHashList(tuple.StorageKeys)
		addrEnc, _ := rlp.EncodeToBytes(tuple.Address[:])
		item := append(addrEnc, keysPayload...)
		inner = append(inner, rlp.WrapList(item)...)
	}
	return rlp.WrapList(inner)
}

// encodeHashList RLP-encodes a list of hashes.
func encodeHashList(hashes []Hash) []byte {
	var inner []byte
	for _, h := range hashes {
		encoded, _ := rlp.EncodeToBytes(h[:])
		inner = append(inner, encoded...)
	}
	return rlp.WrapList(inner)
}

// encodeAuthListBytes RLP-encodes an EIP-7702 authorization list as raw bytes.
func encodeAuthListBytes(list []Authorization) []byte {
	var inner []byte
	for _, auth := range list {
		chainEnc, _ := rlp.EncodeToBytes(auth.ChainID)
		addrEnc, _ := rlp.EncodeToBytes(auth.Address[:])
		nonceEnc, _ := rlp.EncodeToBytes(auth.Nonce)
		item := append(chainEnc, addrEnc...)
		item = append(item, nonceEnc...)
		inner = append(inner, rlp.WrapList(item)...)
	}
	return rlp.WrapList(inner)
}

// decodeFrameTxWrapped decodes a FrameTx from RLP payload and wraps it in a Transaction.
func decodeFrameTxWrapped(data []byte) (*Transaction, error) {
	inner, err := DecodeFrameTx(data)
	if err != nil {
		return nil, err
	}
	return NewTransaction(inner), nil
}
