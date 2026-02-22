package types

import (
	"encoding/binary"
	"errors"
	"math/big"

	"github.com/eth2030/eth2030/ssz"
)

// SSZ encoding limits for blocks.
const (
	sszMaxWithdrawals = 1 << 14 // 16384
	sszMaxExtra       = 1 << 5  // 32 bytes
)

// SSZ block errors.
var (
	ErrSSZBlockEncode  = errors.New("ssz: block encode error")
	ErrSSZBlockDecode  = errors.New("ssz: block decode error")
	ErrSSZHeaderDecode = errors.New("ssz: header decode error")
)

// HeaderToSSZ encodes a block header in SSZ format.
// Fixed fields are encoded in order; Extra is a variable-length field.
//
// Layout:
//
//	ParentHash(32) | UncleHash(32) | Coinbase(20) | Root(32) | TxHash(32) |
//	ReceiptHash(32) | Bloom(256) | Difficulty(32) | Number(32) | GasLimit(8) |
//	GasUsed(8) | Time(8) | MixDigest(32) | Nonce(8) | BaseFee(32) |
//	WithdrawalsHash(32) | BlobGasUsed(8) | ExcessBlobGas(8) |
//	ParentBeaconRoot(32) | RequestsHash(32) | Extra_offset(4)
func HeaderToSSZ(header *Header) ([]byte, error) {
	parentHash := header.ParentHash[:]
	uncleHash := header.UncleHash[:]
	coinbase := header.Coinbase[:]
	root := header.Root[:]
	txHash := header.TxHash[:]
	receiptHash := header.ReceiptHash[:]
	bloom := header.Bloom[:]
	difficulty := bigIntTo32(header.Difficulty)
	number := bigIntTo32(header.Number)
	gasLimit := ssz.MarshalUint64(header.GasLimit)
	gasUsed := ssz.MarshalUint64(header.GasUsed)
	time := ssz.MarshalUint64(header.Time)
	mixDigest := header.MixDigest[:]
	nonce := header.Nonce[:]

	baseFee := bigIntTo32(header.BaseFee)

	var withdrawalsHash []byte
	if header.WithdrawalsHash != nil {
		withdrawalsHash = header.WithdrawalsHash[:]
	} else {
		withdrawalsHash = make([]byte, 32)
	}

	var blobGasUsed []byte
	if header.BlobGasUsed != nil {
		blobGasUsed = ssz.MarshalUint64(*header.BlobGasUsed)
	} else {
		blobGasUsed = make([]byte, 8)
	}

	var excessBlobGas []byte
	if header.ExcessBlobGas != nil {
		excessBlobGas = ssz.MarshalUint64(*header.ExcessBlobGas)
	} else {
		excessBlobGas = make([]byte, 8)
	}

	var parentBeaconRoot []byte
	if header.ParentBeaconRoot != nil {
		parentBeaconRoot = header.ParentBeaconRoot[:]
	} else {
		parentBeaconRoot = make([]byte, 32)
	}

	var requestsHash []byte
	if header.RequestsHash != nil {
		requestsHash = header.RequestsHash[:]
	} else {
		requestsHash = make([]byte, 32)
	}

	extra := header.Extra
	if extra == nil {
		extra = []byte{}
	}

	// 20 fixed fields + 1 variable (extra)
	fixedParts := [][]byte{
		parentHash, uncleHash, coinbase, root, txHash,
		receiptHash, bloom, difficulty, number, gasLimit,
		gasUsed, time, mixDigest, nonce, baseFee,
		withdrawalsHash, blobGasUsed, excessBlobGas, parentBeaconRoot, requestsHash,
		nil, // extra offset
	}
	variableParts := [][]byte{extra}
	variableIndices := []int{20}

	return ssz.MarshalVariableContainer(fixedParts, variableParts, variableIndices), nil
}

// sszHeaderFixedSize is the fixed portion of an SSZ-encoded header.
// 32+32+20+32+32+32+256+32+32+8+8+8+32+8+32+32+8+8+32+32+4 = 672
const sszHeaderFixedSize = 32 + 32 + 20 + 32 + 32 + 32 + 256 + 32 + 32 + 8 + 8 + 8 + 32 + 8 + 32 + 32 + 8 + 8 + 32 + 32 + 4

// SSZToHeader decodes an SSZ-encoded header.
func SSZToHeader(data []byte) (*Header, error) {
	if len(data) < sszHeaderFixedSize {
		return nil, ErrSSZHeaderDecode
	}

	h := &Header{}
	off := 0

	copy(h.ParentHash[:], data[off:off+32])
	off += 32
	copy(h.UncleHash[:], data[off:off+32])
	off += 32
	copy(h.Coinbase[:], data[off:off+20])
	off += 20
	copy(h.Root[:], data[off:off+32])
	off += 32
	copy(h.TxHash[:], data[off:off+32])
	off += 32
	copy(h.ReceiptHash[:], data[off:off+32])
	off += 32
	copy(h.Bloom[:], data[off:off+256])
	off += 256
	h.Difficulty, off = readBigInt32LE(data, off)
	h.Number, off = readBigInt32LE(data, off)
	h.GasLimit, off = readUint64LE(data, off)
	h.GasUsed, off = readUint64LE(data, off)
	h.Time, off = readUint64LE(data, off)
	copy(h.MixDigest[:], data[off:off+32])
	off += 32
	copy(h.Nonce[:], data[off:off+8])
	off += 8

	h.BaseFee, off = readBigInt32LE(data, off)

	var wh Hash
	copy(wh[:], data[off:off+32])
	off += 32
	if wh != (Hash{}) {
		h.WithdrawalsHash = &wh
	}

	bgu, _ := readUint64LE(data, off)
	off += 8
	if bgu != 0 {
		h.BlobGasUsed = &bgu
	}

	ebg, _ := readUint64LE(data, off)
	off += 8
	if ebg != 0 {
		h.ExcessBlobGas = &ebg
	}

	var pbr Hash
	copy(pbr[:], data[off:off+32])
	off += 32
	if pbr != (Hash{}) {
		h.ParentBeaconRoot = &pbr
	}

	var rh Hash
	copy(rh[:], data[off:off+32])
	off += 32
	if rh != (Hash{}) {
		h.RequestsHash = &rh
	}

	// Read extra data offset.
	extraOffset, _ := readUint32LE(data, off)
	if int(extraOffset) > len(data) {
		return nil, ErrSSZHeaderDecode
	}
	h.Extra = make([]byte, len(data)-int(extraOffset))
	copy(h.Extra, data[extraOffset:])

	return h, nil
}

// BlockToSSZ encodes a full block in SSZ format.
// Layout: header_offset(4) | txs_offset(4) | withdrawals_offset(4) | header | txs | withdrawals
func BlockToSSZ(block *Block) ([]byte, error) {
	headerData, err := HeaderToSSZ(block.header)
	if err != nil {
		return nil, err
	}

	// Encode transactions: 4-byte count + (4-byte offset per tx) + tx data.
	txs := block.Transactions()
	var txsData []byte
	countBuf := make([]byte, 4)
	binary.LittleEndian.PutUint32(countBuf, uint32(len(txs)))
	txsData = append(txsData, countBuf...)

	encodedTxs := make([][]byte, len(txs))
	for i, tx := range txs {
		encoded, err := TransactionToSSZ(tx)
		if err != nil {
			return nil, err
		}
		encodedTxs[i] = encoded
	}

	// Offsets start after count(4) + offsets(4*len).
	txOffsetBase := 4 + 4*len(txs)
	currentTxOffset := txOffsetBase
	for _, enc := range encodedTxs {
		ob := make([]byte, 4)
		binary.LittleEndian.PutUint32(ob, uint32(currentTxOffset))
		txsData = append(txsData, ob...)
		currentTxOffset += len(enc)
	}
	for _, enc := range encodedTxs {
		txsData = append(txsData, enc...)
	}

	// Encode withdrawals: 4-byte count + withdrawal data.
	withdrawals := block.Withdrawals()
	var wdData []byte
	wdCountBuf := make([]byte, 4)
	if withdrawals != nil {
		binary.LittleEndian.PutUint32(wdCountBuf, uint32(len(withdrawals)))
	}
	wdData = append(wdData, wdCountBuf...)
	for _, w := range withdrawals {
		wdData = append(wdData, ssz.MarshalUint64(w.Index)...)
		wdData = append(wdData, ssz.MarshalUint64(w.ValidatorIndex)...)
		wdData = append(wdData, w.Address[:]...)
		wdData = append(wdData, ssz.MarshalUint64(w.Amount)...)
	}

	// Container: 3 variable fields.
	fixedParts := [][]byte{nil, nil, nil}
	variableParts := [][]byte{headerData, txsData, wdData}
	variableIndices := []int{0, 1, 2}

	return ssz.MarshalVariableContainer(fixedParts, variableParts, variableIndices), nil
}

// SSZToBlock decodes an SSZ-encoded block.
func SSZToBlock(data []byte) (*Block, error) {
	// Minimum: 3 offsets = 12 bytes.
	if len(data) < 12 {
		return nil, ErrSSZBlockDecode
	}

	headerOff := binary.LittleEndian.Uint32(data[0:4])
	txsOff := binary.LittleEndian.Uint32(data[4:8])
	wdOff := binary.LittleEndian.Uint32(data[8:12])

	if int(headerOff) > len(data) || int(txsOff) > len(data) || int(wdOff) > len(data) {
		return nil, ErrSSZBlockDecode
	}

	// Decode header.
	headerData := data[headerOff:txsOff]
	header, err := SSZToHeader(headerData)
	if err != nil {
		return nil, err
	}

	// Decode transactions.
	txsData := data[txsOff:wdOff]
	txs, err := decodeTxsSSZ(txsData)
	if err != nil {
		return nil, err
	}

	// Decode withdrawals.
	wdData := data[wdOff:]
	withdrawals := decodeWithdrawalsSSZ(wdData)

	body := &Body{
		Transactions: txs,
		Withdrawals:  withdrawals,
	}

	return NewBlock(header, body), nil
}

// decodeTxsSSZ decodes the transaction section: count(4) | offsets(4*count) | tx_data...
func decodeTxsSSZ(data []byte) ([]*Transaction, error) {
	if len(data) < 4 {
		return nil, ErrSSZBlockDecode
	}
	count := binary.LittleEndian.Uint32(data[0:4])
	if count == 0 {
		return nil, nil
	}

	offsetsEnd := 4 + 4*int(count)
	if offsetsEnd > len(data) {
		return nil, ErrSSZBlockDecode
	}

	offsets := make([]int, count)
	for i := 0; i < int(count); i++ {
		offsets[i] = int(binary.LittleEndian.Uint32(data[4+4*i : 4+4*i+4]))
	}

	txs := make([]*Transaction, count)
	for i := 0; i < int(count); i++ {
		start := offsets[i]
		var end int
		if i+1 < int(count) {
			end = offsets[i+1]
		} else {
			end = len(data)
		}
		if start > end || end > len(data) {
			return nil, ErrSSZBlockDecode
		}
		tx, err := SSZToTransaction(data[start:end])
		if err != nil {
			return nil, err
		}
		txs[i] = tx
	}
	return txs, nil
}

// decodeWithdrawalsSSZ decodes the withdrawals section.
// Each withdrawal: index(8) | validatorIndex(8) | address(20) | amount(8) = 44 bytes.
func decodeWithdrawalsSSZ(data []byte) []*Withdrawal {
	if len(data) < 4 {
		return nil
	}
	count := binary.LittleEndian.Uint32(data[0:4])
	if count == 0 {
		return nil
	}

	const wdSize = 8 + 8 + 20 + 8 // 44
	off := 4
	withdrawals := make([]*Withdrawal, 0, count)
	for i := uint32(0); i < count; i++ {
		if off+wdSize > len(data) {
			break
		}
		w := &Withdrawal{}
		w.Index = binary.LittleEndian.Uint64(data[off : off+8])
		off += 8
		w.ValidatorIndex = binary.LittleEndian.Uint64(data[off : off+8])
		off += 8
		copy(w.Address[:], data[off:off+20])
		off += 20
		w.Amount = binary.LittleEndian.Uint64(data[off : off+8])
		off += 8
		withdrawals = append(withdrawals, w)
	}
	return withdrawals
}

// BlockSSZRoot computes the SSZ hash tree root for an entire block.
func BlockSSZRoot(block *Block) (Hash, error) {
	// Compute header root.
	headerSSZ, err := HeaderToSSZ(block.header)
	if err != nil {
		return Hash{}, err
	}
	headerChunks := ssz.Pack(headerSSZ)
	headerRoot := ssz.Merkleize(headerChunks, 0)

	// Compute transactions root.
	txsRoot, err := TransactionsSSZRoot(block.Transactions())
	if err != nil {
		return Hash{}, err
	}

	// Compute withdrawals root.
	withdrawals := block.Withdrawals()
	var wdRoots [][32]byte
	for _, w := range withdrawals {
		var buf [44]byte
		binary.LittleEndian.PutUint64(buf[0:8], w.Index)
		binary.LittleEndian.PutUint64(buf[8:16], w.ValidatorIndex)
		copy(buf[16:36], w.Address[:])
		binary.LittleEndian.PutUint64(buf[36:44], w.Amount)
		chunks := ssz.Pack(buf[:])
		root := ssz.Merkleize(chunks, 0)
		wdRoots = append(wdRoots, root)
	}
	wdRoot := ssz.HashTreeRootList(wdRoots, sszMaxWithdrawals)

	// Container root: hash tree root of [headerRoot, txsRoot, wdRoot].
	var txsRoot32 [32]byte
	copy(txsRoot32[:], txsRoot[:])

	fieldRoots := [][32]byte{headerRoot, txsRoot32, wdRoot}
	root := ssz.HashTreeRootContainer(fieldRoots)

	var h Hash
	copy(h[:], root[:])
	return h, nil
}

// HeaderSSZRoot computes the SSZ hash tree root for a header.
func HeaderSSZRoot(header *Header) (Hash, error) {
	encoded, err := HeaderToSSZ(header)
	if err != nil {
		return Hash{}, err
	}
	chunks := ssz.Pack(encoded)
	root := ssz.Merkleize(chunks, 0)
	var h Hash
	copy(h[:], root[:])
	return h, nil
}

// bigIntTo32 is defined in tx_ssz.go, reuse it here.
// bigIntFrom32LE is defined in tx_ssz.go, reuse it here.
// readUint64LE, readUint32LE, readBigInt32LE are defined in tx_ssz.go.

// headerBigIntField extracts a *big.Int field, returning nil if zero.
func headerBigIntField(v *big.Int) *big.Int {
	if v == nil || v.Sign() == 0 {
		return nil
	}
	return new(big.Int).Set(v)
}
