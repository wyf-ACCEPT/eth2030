package rpc

import (
	"github.com/eth2028/eth2028/core/types"
)

// FilterLogs applies a FilterQuery against a set of logs and returns
// only the matching entries. It uses address and topic matching
// consistent with go-ethereum semantics.
func FilterLogs(logs []*types.Log, query FilterQuery) []*types.Log {
	var result []*types.Log
	for _, log := range logs {
		if MatchFilter(log, query) {
			result = append(result, log)
		}
	}
	return result
}

// FilterLogsWithBloom applies bloom-level pre-screening per block before
// falling back to exact log matching. blockLogs should be grouped by
// block -- the caller provides the block's header bloom and its logs.
// This avoids scanning logs in blocks whose bloom cannot match.
func FilterLogsWithBloom(bloom types.Bloom, logs []*types.Log, query FilterQuery) []*types.Log {
	if !bloomMatchesQuery(bloom, query) {
		return nil
	}
	return FilterLogs(logs, query)
}

// bloomMatchesAddress returns true if the bloom may contain the given
// address (false positives possible).
func bloomMatchesAddress(bloom types.Bloom, addr types.Address) bool {
	return types.BloomContains(bloom, addr.Bytes())
}

// bloomMatchesTopic returns true if the bloom may contain the given
// topic hash (false positives possible).
func bloomMatchesTopic(bloom types.Bloom, topic types.Hash) bool {
	return types.BloomContains(bloom, topic.Bytes())
}
