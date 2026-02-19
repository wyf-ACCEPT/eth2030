package types

// FrameResult holds the execution result of a single frame within a Frame transaction.
type FrameResult struct {
	Status  uint64
	GasUsed uint64
	Logs    []*Log
}

// FrameTxReceipt is the receipt for a Frame transaction per EIP-8141.
// Receipt payload: [cumulative_gas_used, payer, [frame_receipt, ...]]
// where frame_receipt = [status, gas_used, logs]
type FrameTxReceipt struct {
	CumulativeGasUsed uint64
	Payer             Address
	FrameResults      []FrameResult
}

// TotalGasUsed returns the sum of gas used across all frame results.
func (r *FrameTxReceipt) TotalGasUsed() uint64 {
	var total uint64
	for _, fr := range r.FrameResults {
		total += fr.GasUsed
	}
	return total
}

// AllLogs returns all logs from all frame results, in order.
func (r *FrameTxReceipt) AllLogs() []*Log {
	var logs []*Log
	for _, fr := range r.FrameResults {
		logs = append(logs, fr.Logs...)
	}
	return logs
}
