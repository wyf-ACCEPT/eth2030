package encrypted

import "sort"

// OrderByCommitTime sorts commit entries by their commit timestamp ascending.
// Earlier commits are ordered first, providing fair ordering that prevents
// MEV frontrunning: a transaction that was committed earlier gets priority
// regardless of gas price, removing the incentive to outbid for ordering.
func OrderByCommitTime(entries []*CommitEntry) []*CommitEntry {
	sorted := make([]*CommitEntry, len(entries))
	copy(sorted, entries)
	sort.Slice(sorted, func(i, j int) bool {
		return sorted[i].Commit.Timestamp < sorted[j].Commit.Timestamp
	})
	return sorted
}
