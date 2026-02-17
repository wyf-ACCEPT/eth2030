package rawdb

import "encoding/binary"

// --- Header Accessors ---

// WriteHeader stores a header's RLP-encoded data.
func WriteHeader(db KeyValueWriter, number uint64, hash [32]byte, data []byte) error {
	if err := db.Put(headerKey(number, hash), data); err != nil {
		return err
	}
	// Store hash -> number mapping for reverse lookups.
	return db.Put(headerNumberKey(hash), encodeBlockNumber(number))
}

// ReadHeader retrieves a header's RLP-encoded data.
func ReadHeader(db KeyValueReader, number uint64, hash [32]byte) ([]byte, error) {
	return db.Get(headerKey(number, hash))
}

// ReadHeaderNumber retrieves the block number for a given header hash.
func ReadHeaderNumber(db KeyValueReader, hash [32]byte) (uint64, error) {
	data, err := db.Get(headerNumberKey(hash))
	if err != nil {
		return 0, err
	}
	if len(data) != 8 {
		return 0, ErrNotFound
	}
	return binary.BigEndian.Uint64(data), nil
}

// HasHeader checks if a header exists in the database.
func HasHeader(db KeyValueReader, number uint64, hash [32]byte) bool {
	ok, _ := db.Has(headerKey(number, hash))
	return ok
}

// DeleteHeader removes a header and its hash->number mapping.
func DeleteHeader(db KeyValueWriter, number uint64, hash [32]byte) error {
	if err := db.Delete(headerKey(number, hash)); err != nil {
		return err
	}
	return db.Delete(headerNumberKey(hash))
}

// --- Body Accessors ---

// WriteBody stores a block body's RLP-encoded data.
func WriteBody(db KeyValueWriter, number uint64, hash [32]byte, data []byte) error {
	return db.Put(bodyKey(number, hash), data)
}

// ReadBody retrieves a block body's RLP-encoded data.
func ReadBody(db KeyValueReader, number uint64, hash [32]byte) ([]byte, error) {
	return db.Get(bodyKey(number, hash))
}

// HasBody checks if a block body exists.
func HasBody(db KeyValueReader, number uint64, hash [32]byte) bool {
	ok, _ := db.Has(bodyKey(number, hash))
	return ok
}

// DeleteBody removes a block body.
func DeleteBody(db KeyValueWriter, number uint64, hash [32]byte) error {
	return db.Delete(bodyKey(number, hash))
}

// --- Receipt Accessors ---

// WriteReceipts stores receipts' RLP-encoded data.
func WriteReceipts(db KeyValueWriter, number uint64, hash [32]byte, data []byte) error {
	return db.Put(receiptKey(number, hash), data)
}

// ReadReceipts retrieves receipts' RLP-encoded data.
func ReadReceipts(db KeyValueReader, number uint64, hash [32]byte) ([]byte, error) {
	return db.Get(receiptKey(number, hash))
}

// DeleteReceipts removes receipts.
func DeleteReceipts(db KeyValueWriter, number uint64, hash [32]byte) error {
	return db.Delete(receiptKey(number, hash))
}

// --- Transaction Lookup ---

// WriteTxLookup stores a transaction hash -> block number mapping.
func WriteTxLookup(db KeyValueWriter, txHash [32]byte, blockNumber uint64) error {
	return db.Put(txLookupKey(txHash), encodeBlockNumber(blockNumber))
}

// ReadTxLookup retrieves the block number for a transaction hash.
func ReadTxLookup(db KeyValueReader, txHash [32]byte) (uint64, error) {
	data, err := db.Get(txLookupKey(txHash))
	if err != nil {
		return 0, err
	}
	if len(data) != 8 {
		return 0, ErrNotFound
	}
	return binary.BigEndian.Uint64(data), nil
}

// DeleteTxLookup removes a transaction lookup entry.
func DeleteTxLookup(db KeyValueWriter, txHash [32]byte) error {
	return db.Delete(txLookupKey(txHash))
}

// --- Canonical Chain ---

// WriteCanonicalHash stores the canonical hash for a block number.
func WriteCanonicalHash(db KeyValueWriter, number uint64, hash [32]byte) error {
	return db.Put(canonicalKey(number), hash[:])
}

// ReadCanonicalHash retrieves the canonical hash for a block number.
func ReadCanonicalHash(db KeyValueReader, number uint64) ([32]byte, error) {
	data, err := db.Get(canonicalKey(number))
	if err != nil {
		return [32]byte{}, err
	}
	if len(data) != 32 {
		return [32]byte{}, ErrNotFound
	}
	var hash [32]byte
	copy(hash[:], data)
	return hash, nil
}

// DeleteCanonicalHash removes a canonical hash mapping.
func DeleteCanonicalHash(db KeyValueWriter, number uint64) error {
	return db.Delete(canonicalKey(number))
}

// --- Head Pointers ---

// WriteHeadHeaderHash stores the hash of the current head header.
func WriteHeadHeaderHash(db KeyValueWriter, hash [32]byte) error {
	return db.Put(headHeaderKey, hash[:])
}

// ReadHeadHeaderHash retrieves the hash of the current head header.
func ReadHeadHeaderHash(db KeyValueReader) ([32]byte, error) {
	data, err := db.Get(headHeaderKey)
	if err != nil {
		return [32]byte{}, err
	}
	if len(data) != 32 {
		return [32]byte{}, ErrNotFound
	}
	var hash [32]byte
	copy(hash[:], data)
	return hash, nil
}

// WriteHeadBlockHash stores the hash of the current head block.
func WriteHeadBlockHash(db KeyValueWriter, hash [32]byte) error {
	return db.Put(headBlockKey, hash[:])
}

// ReadHeadBlockHash retrieves the hash of the current head block.
func ReadHeadBlockHash(db KeyValueReader) ([32]byte, error) {
	data, err := db.Get(headBlockKey)
	if err != nil {
		return [32]byte{}, err
	}
	if len(data) != 32 {
		return [32]byte{}, ErrNotFound
	}
	var hash [32]byte
	copy(hash[:], data)
	return hash, nil
}

// --- Contract Code ---

// WriteCode stores contract bytecode keyed by code hash.
func WriteCode(db KeyValueWriter, codeHash [32]byte, code []byte) error {
	return db.Put(codeKey(codeHash), code)
}

// ReadCode retrieves contract bytecode by code hash.
func ReadCode(db KeyValueReader, codeHash [32]byte) ([]byte, error) {
	return db.Get(codeKey(codeHash))
}

// HasCode checks if contract code exists.
func HasCode(db KeyValueReader, codeHash [32]byte) bool {
	ok, _ := db.Has(codeKey(codeHash))
	return ok
}

// DeleteCode removes contract code.
func DeleteCode(db KeyValueWriter, codeHash [32]byte) error {
	return db.Delete(codeKey(codeHash))
}
