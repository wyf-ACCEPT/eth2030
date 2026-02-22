package core

import "github.com/eth2030/eth2030/core/types"

// BloomFilter provides bloom-based pre-filtering for log queries.
// Before performing expensive exact matching against every log in a block,
// callers can check the block's bloom filter to quickly skip blocks that
// cannot possibly contain matching logs.

// BloomMatchesAddresses checks whether the bloom filter could contain logs
// from any of the given addresses. Returns true if all addresses have their
// bloom bits set (or if the address list is empty, meaning no filter).
func BloomMatchesAddresses(bloom types.Bloom, addresses []types.Address) bool {
	if len(addresses) == 0 {
		return true // no address filter
	}
	for _, addr := range addresses {
		if types.BloomContains(bloom, addr.Bytes()) {
			return true // at least one address might match
		}
	}
	return false
}

// BloomMatchesTopics checks whether the bloom filter could contain logs
// matching the given topic filter. The topic filter follows Ethereum's
// convention: each position is a list of acceptable topics (OR within a
// position, AND across positions). An empty list at a position is a wildcard.
// Returns true if the bloom bits for at least one topic in each non-empty
// position are set.
func BloomMatchesTopics(bloom types.Bloom, topics [][]types.Hash) bool {
	for _, position := range topics {
		if len(position) == 0 {
			continue // wildcard
		}
		matched := false
		for _, topic := range position {
			if types.BloomContains(bloom, topic.Bytes()) {
				matched = true
				break
			}
		}
		if !matched {
			return false
		}
	}
	return true
}

// BloomMatchesFilter is a convenience function that checks both address and
// topic bloom filters in one call. It returns true if the bloom filter
// indicates the block could contain matching logs.
func BloomMatchesFilter(bloom types.Bloom, addresses []types.Address, topics [][]types.Hash) bool {
	return BloomMatchesAddresses(bloom, addresses) && BloomMatchesTopics(bloom, topics)
}
