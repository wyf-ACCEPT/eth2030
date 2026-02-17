package metrics

// Pre-defined metrics for the eth2028 Ethereum execution client. All metrics
// live in DefaultRegistry so they are globally accessible without passing a
// registry around.

var (
	// ---- Chain metrics ----

	// ChainHeight tracks the latest block number.
	ChainHeight = DefaultRegistry.Gauge("chain.height")
	// BlockProcessTime records block processing duration in milliseconds.
	BlockProcessTime = DefaultRegistry.Histogram("chain.block_process_ms")
	// BlocksInserted counts blocks successfully appended to the chain.
	BlocksInserted = DefaultRegistry.Counter("chain.blocks_inserted")
	// ReorgsDetected counts chain reorganisation events.
	ReorgsDetected = DefaultRegistry.Counter("chain.reorgs")

	// ---- Transaction pool metrics ----

	// TxPoolPending tracks the number of pending transactions.
	TxPoolPending = DefaultRegistry.Gauge("txpool.pending")
	// TxPoolQueued tracks the number of queued transactions.
	TxPoolQueued = DefaultRegistry.Gauge("txpool.queued")
	// TxPoolAdded counts transactions added to the pool.
	TxPoolAdded = DefaultRegistry.Counter("txpool.added")
	// TxPoolDropped counts transactions dropped from the pool.
	TxPoolDropped = DefaultRegistry.Counter("txpool.dropped")

	// ---- P2P metrics ----

	// PeersConnected tracks the current number of connected peers.
	PeersConnected = DefaultRegistry.Gauge("p2p.peers")
	// MessagesReceived counts devp2p messages received.
	MessagesReceived = DefaultRegistry.Counter("p2p.messages_received")
	// MessagesSent counts devp2p messages sent.
	MessagesSent = DefaultRegistry.Counter("p2p.messages_sent")

	// ---- RPC metrics ----

	// RPCRequests counts incoming JSON-RPC requests.
	RPCRequests = DefaultRegistry.Counter("rpc.requests")
	// RPCErrors counts JSON-RPC requests that returned an error.
	RPCErrors = DefaultRegistry.Counter("rpc.errors")
	// RPCLatency records JSON-RPC request latency in milliseconds.
	RPCLatency = DefaultRegistry.Histogram("rpc.latency_ms")

	// ---- EVM metrics ----

	// EVMExecutions counts EVM call/create invocations.
	EVMExecutions = DefaultRegistry.Counter("evm.executions")
	// EVMGasUsed counts total gas consumed by EVM execution.
	EVMGasUsed = DefaultRegistry.Counter("evm.gas_used")

	// ---- Engine API metrics ----

	// EngineNewPayload counts engine_newPayload calls.
	EngineNewPayload = DefaultRegistry.Counter("engine.new_payload")
	// EngineFCU counts engine_forkchoiceUpdated calls.
	EngineFCU = DefaultRegistry.Counter("engine.forkchoice_updated")
)
