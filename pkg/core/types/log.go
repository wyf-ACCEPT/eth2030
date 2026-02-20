// log.go implements EVM log types with bloom filter integration,
// RLP and JSON serialization, and log filter matching.
package types

import (
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/eth2028/eth2028/rlp"
)

// MaxTopicsPerLog is the maximum number of indexed topics in a single log event.
// EVM LOG0..LOG4 opcodes allow 0-4 topics.
const MaxTopicsPerLog = 4

// LogFilter defines criteria for matching logs. A log matches if:
//   - Addresses is empty OR the log address is in Addresses.
//   - For each position i in Topics: Topics[i] is empty (wildcard)
//     OR the log's topic at position i is in Topics[i].
type LogFilter struct {
	// Addresses restricts matching to logs from these contract addresses.
	// An empty slice matches all addresses.
	Addresses []Address

	// Topics is a positional filter. Each inner slice represents acceptable
	// values for that topic index (OR within position, AND across positions).
	// A nil or empty inner slice matches any value at that position.
	Topics [][]Hash

	// BlockRange restricts matching to logs within a block number range.
	FromBlock uint64
	ToBlock   uint64
}

// logRLP is the RLP-serializable consensus representation: [Address, Topics, Data].
type logRLP struct {
	Address Address
	Topics  []Hash
	Data    []byte
}

// EncodeLogRLP returns the RLP encoding of a log's consensus fields:
// [Address, [Topic1, Topic2, ...], Data].
func EncodeLogRLP(l *Log) ([]byte, error) {
	if l == nil {
		return nil, errors.New("log: cannot encode nil log")
	}
	if len(l.Topics) > MaxTopicsPerLog {
		return nil, fmt.Errorf("log: too many topics: %d > %d", len(l.Topics), MaxTopicsPerLog)
	}

	addrEnc, err := rlp.EncodeToBytes(l.Address)
	if err != nil {
		return nil, fmt.Errorf("log: encode address: %w", err)
	}

	var topicsPayload []byte
	for _, t := range l.Topics {
		enc, err := rlp.EncodeToBytes(t)
		if err != nil {
			return nil, fmt.Errorf("log: encode topic: %w", err)
		}
		topicsPayload = append(topicsPayload, enc...)
	}

	dataEnc, err := rlp.EncodeToBytes(l.Data)
	if err != nil {
		return nil, fmt.Errorf("log: encode data: %w", err)
	}

	var payload []byte
	payload = append(payload, addrEnc...)
	payload = append(payload, rlp.WrapList(topicsPayload)...)
	payload = append(payload, dataEnc...)
	return rlp.WrapList(payload), nil
}

// DecodeLogRLP decodes an RLP-encoded log from raw bytes.
func DecodeLogRLP(data []byte) (*Log, error) {
	s := rlp.NewStreamFromBytes(data)
	if _, err := s.List(); err != nil {
		return nil, fmt.Errorf("log: decode outer list: %w", err)
	}

	l := &Log{}

	// Decode address (20 bytes).
	addrBytes, err := s.Bytes()
	if err != nil {
		return nil, fmt.Errorf("log: decode address: %w", err)
	}
	if len(addrBytes) != AddressLength {
		return nil, fmt.Errorf("log: invalid address length: %d", len(addrBytes))
	}
	copy(l.Address[:], addrBytes)

	// Decode topics list.
	if _, err := s.List(); err != nil {
		return nil, fmt.Errorf("log: decode topics list: %w", err)
	}
	for !s.AtListEnd() {
		topicBytes, err := s.Bytes()
		if err != nil {
			return nil, fmt.Errorf("log: decode topic: %w", err)
		}
		if len(topicBytes) != HashLength {
			return nil, fmt.Errorf("log: invalid topic length: %d", len(topicBytes))
		}
		var topic Hash
		copy(topic[:], topicBytes)
		l.Topics = append(l.Topics, topic)
	}
	if err := s.ListEnd(); err != nil {
		return nil, fmt.Errorf("log: decode topics list end: %w", err)
	}

	if len(l.Topics) > MaxTopicsPerLog {
		return nil, fmt.Errorf("log: too many topics: %d", len(l.Topics))
	}

	// Decode data.
	l.Data, err = s.Bytes()
	if err != nil {
		return nil, fmt.Errorf("log: decode data: %w", err)
	}

	if err := s.ListEnd(); err != nil {
		return nil, fmt.Errorf("log: decode outer list end: %w", err)
	}
	return l, nil
}

// EncodeLogsRLP RLP-encodes a list of logs as a top-level RLP list.
func EncodeLogsRLP(logs []*Log) ([]byte, error) {
	var payload []byte
	for _, l := range logs {
		enc, err := EncodeLogRLP(l)
		if err != nil {
			return nil, err
		}
		payload = append(payload, enc...)
	}
	return rlp.WrapList(payload), nil
}

// jsonLog is the JSON-serializable representation of a log.
type jsonLog struct {
	Address     string   `json:"address"`
	Topics      []string `json:"topics"`
	Data        string   `json:"data"`
	BlockNumber string   `json:"blockNumber"`
	TxHash      string   `json:"transactionHash"`
	TxIndex     string   `json:"transactionIndex"`
	BlockHash   string   `json:"blockHash"`
	LogIndex    string   `json:"logIndex"`
	Removed     bool     `json:"removed"`
}

// MarshalLogJSON serializes a log to JSON using Ethereum hex encoding conventions.
func MarshalLogJSON(l *Log) ([]byte, error) {
	if l == nil {
		return nil, errors.New("log: cannot marshal nil log")
	}

	topics := make([]string, len(l.Topics))
	for i, t := range l.Topics {
		topics[i] = fmt.Sprintf("0x%s", hex.EncodeToString(t[:]))
	}

	jl := jsonLog{
		Address:     fmt.Sprintf("0x%s", hex.EncodeToString(l.Address[:])),
		Topics:      topics,
		Data:        fmt.Sprintf("0x%s", hex.EncodeToString(l.Data)),
		BlockNumber: fmt.Sprintf("0x%x", l.BlockNumber),
		TxHash:      fmt.Sprintf("0x%s", hex.EncodeToString(l.TxHash[:])),
		TxIndex:     fmt.Sprintf("0x%x", l.TxIndex),
		BlockHash:   fmt.Sprintf("0x%s", hex.EncodeToString(l.BlockHash[:])),
		LogIndex:    fmt.Sprintf("0x%x", l.Index),
		Removed:     l.Removed,
	}
	return json.Marshal(jl)
}

// UnmarshalLogJSON deserializes a log from Ethereum-style JSON.
func UnmarshalLogJSON(data []byte) (*Log, error) {
	var jl jsonLog
	if err := json.Unmarshal(data, &jl); err != nil {
		return nil, fmt.Errorf("log: json unmarshal: %w", err)
	}

	l := &Log{Removed: jl.Removed}

	// Parse address.
	addrBytes, err := decodeHexField(jl.Address)
	if err != nil {
		return nil, fmt.Errorf("log: parse address: %w", err)
	}
	l.Address = BytesToAddress(addrBytes)

	// Parse topics.
	for _, ts := range jl.Topics {
		topicBytes, err := decodeHexField(ts)
		if err != nil {
			return nil, fmt.Errorf("log: parse topic: %w", err)
		}
		l.Topics = append(l.Topics, BytesToHash(topicBytes))
	}

	// Parse data.
	l.Data, err = decodeHexField(jl.Data)
	if err != nil {
		return nil, fmt.Errorf("log: parse data: %w", err)
	}

	// Parse block number.
	l.BlockNumber, err = decodeHexUint64(jl.BlockNumber)
	if err != nil {
		return nil, fmt.Errorf("log: parse blockNumber: %w", err)
	}

	// Parse tx hash and block hash.
	txHashBytes, err := decodeHexField(jl.TxHash)
	if err != nil {
		return nil, fmt.Errorf("log: parse txHash: %w", err)
	}
	l.TxHash = BytesToHash(txHashBytes)

	blockHashBytes, err := decodeHexField(jl.BlockHash)
	if err != nil {
		return nil, fmt.Errorf("log: parse blockHash: %w", err)
	}
	l.BlockHash = BytesToHash(blockHashBytes)

	// Parse indices.
	txIdx, err := decodeHexUint64(jl.TxIndex)
	if err != nil {
		return nil, fmt.Errorf("log: parse txIndex: %w", err)
	}
	l.TxIndex = uint(txIdx)

	logIdx, err := decodeHexUint64(jl.LogIndex)
	if err != nil {
		return nil, fmt.Errorf("log: parse logIndex: %w", err)
	}
	l.Index = uint(logIdx)

	return l, nil
}

// LogBloom computes the bloom filter contribution of a single log.
func LogBloom(l *Log) Bloom {
	var bloom Bloom
	BloomAdd(&bloom, l.Address.Bytes())
	for _, topic := range l.Topics {
		BloomAdd(&bloom, topic.Bytes())
	}
	return bloom
}

// BloomMatchesLog checks if a bloom filter could contain the given log.
// This is a fast pre-check; false positives are possible.
func BloomMatchesLog(bloom Bloom, l *Log) bool {
	if !BloomContains(bloom, l.Address.Bytes()) {
		return false
	}
	for _, topic := range l.Topics {
		if !BloomContains(bloom, topic.Bytes()) {
			return false
		}
	}
	return true
}

// FilterMatch returns true if the log satisfies the given filter criteria.
func FilterMatch(l *Log, f *LogFilter) bool {
	if l == nil || f == nil {
		return false
	}

	// Check block range.
	if f.FromBlock > 0 && l.BlockNumber < f.FromBlock {
		return false
	}
	if f.ToBlock > 0 && l.BlockNumber > f.ToBlock {
		return false
	}

	// Check address filter.
	if len(f.Addresses) > 0 {
		found := false
		for _, addr := range f.Addresses {
			if l.Address == addr {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}

	// Check topic filters.
	for i, topicSet := range f.Topics {
		if len(topicSet) == 0 {
			// Wildcard: any topic at this position is acceptable.
			continue
		}
		if i >= len(l.Topics) {
			return false
		}
		found := false
		for _, t := range topicSet {
			if l.Topics[i] == t {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}
	return true
}

// FilterLogs applies the filter criteria to a list of logs and returns
// only those that match.
func FilterLogs(logs []*Log, f *LogFilter) []*Log {
	if f == nil || len(logs) == 0 {
		return nil
	}
	var result []*Log
	for _, l := range logs {
		if FilterMatch(l, f) {
			result = append(result, l)
		}
	}
	return result
}

// BloomMatchesFilter checks if a bloom filter could contain any log matching
// the given filter. This enables skipping entire blocks during log queries.
func BloomMatchesFilter(bloom Bloom, f *LogFilter) bool {
	if f == nil {
		return true
	}

	// If address filter is set, at least one address must be in the bloom.
	if len(f.Addresses) > 0 {
		found := false
		for _, addr := range f.Addresses {
			if BloomContains(bloom, addr.Bytes()) {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}

	// Each non-empty topic position must have at least one match in the bloom.
	for _, topicSet := range f.Topics {
		if len(topicSet) == 0 {
			continue
		}
		found := false
		for _, t := range topicSet {
			if BloomContains(bloom, t.Bytes()) {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}
	return true
}

// decodeHexField strips the "0x" prefix and hex-decodes a string.
func decodeHexField(s string) ([]byte, error) {
	if len(s) >= 2 && s[0] == '0' && (s[1] == 'x' || s[1] == 'X') {
		s = s[2:]
	}
	if len(s)%2 == 1 {
		s = "0" + s
	}
	return hex.DecodeString(s)
}

// decodeHexUint64 decodes a hex-encoded uint64 string (with optional 0x prefix).
func decodeHexUint64(s string) (uint64, error) {
	b, err := decodeHexField(s)
	if err != nil {
		return 0, err
	}
	if len(b) > 8 {
		return 0, errors.New("hex uint64 overflow")
	}
	var v uint64
	for _, x := range b {
		v = (v << 8) | uint64(x)
	}
	return v, nil
}
