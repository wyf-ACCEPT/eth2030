package node

import (
	"errors"
	"fmt"
	"log"
	"math/big"
	"net/http"
	"sync"

	"github.com/eth2030/eth2030/core"
	"github.com/eth2030/eth2030/core/rawdb"
	"github.com/eth2030/eth2030/core/state"
	"github.com/eth2030/eth2030/core/types"
	"github.com/eth2030/eth2030/engine"
	"github.com/eth2030/eth2030/p2p"
	"github.com/eth2030/eth2030/rpc"
	"github.com/eth2030/eth2030/txpool"
)

// Node is the top-level ETH2030 node that manages all subsystems.
type Node struct {
	config *Config

	// Subsystems.
	db           rawdb.Database
	blockchain   *core.Blockchain
	txPool       *txpool.TxPool
	rpcServer    *http.Server
	rpcHandler   *rpc.Server
	engineServer *engine.EngineAPI
	p2pServer    *p2p.Server

	mu      sync.Mutex
	running bool
	stop    chan struct{}
}

// New creates a new Node with the given configuration. It initializes
// all subsystems but does not start any network services.
func New(config *Config) (*Node, error) {
	if config == nil {
		c := DefaultConfig()
		config = &c
	}
	if err := config.Validate(); err != nil {
		return nil, fmt.Errorf("invalid config: %w", err)
	}

	n := &Node{
		config: config,
		stop:   make(chan struct{}),
	}

	// Initialize in-memory database.
	n.db = rawdb.NewMemoryDB()

	// Initialize blockchain with a genesis block.
	chainConfig := chainConfigForNetwork(config.Network)
	genesis := makeGenesisBlock()
	statedb := state.NewMemoryStateDB()

	bc, err := core.NewBlockchain(chainConfig, genesis, statedb, n.db)
	if err != nil {
		return nil, fmt.Errorf("init blockchain: %w", err)
	}
	n.blockchain = bc

	// Initialize transaction pool.
	poolCfg := txpool.DefaultConfig()
	n.txPool = txpool.New(poolCfg, bc.State())

	// Initialize P2P server.
	n.p2pServer = p2p.NewServer(p2p.Config{
		ListenAddr: config.P2PAddr(),
		MaxPeers:   config.MaxPeers,
	})

	// Initialize RPC server with blockchain backend.
	backend := newNodeBackend(n)
	n.rpcHandler = rpc.NewServer(backend)

	// Initialize Engine API server.
	engineBackend := newEngineBackend(n)
	n.engineServer = engine.NewEngineAPI(engineBackend)

	return n, nil
}

// Start starts all node subsystems in order.
func (n *Node) Start() error {
	n.mu.Lock()
	defer n.mu.Unlock()

	if n.running {
		return errors.New("node already running")
	}

	log.Printf("Starting ETH2030 node (network=%s)", n.config.Network)

	// Start P2P server.
	if err := n.p2pServer.Start(); err != nil {
		return fmt.Errorf("start p2p: %w", err)
	}
	log.Printf("P2P server listening on %s", n.p2pServer.ListenAddr())

	// Start JSON-RPC server.
	n.rpcServer = &http.Server{
		Addr:    n.config.RPCAddr(),
		Handler: n.rpcHandler.Handler(),
	}
	go func() {
		log.Printf("RPC server listening on %s", n.config.RPCAddr())
		if err := n.rpcServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Printf("RPC server error: %v", err)
		}
	}()

	// Start Engine API server.
	go func() {
		log.Printf("Engine API server listening on %s", n.config.EngineAddr())
		if err := n.engineServer.Start(n.config.EngineAddr()); err != nil {
			log.Printf("Engine API error: %v", err)
		}
	}()

	n.running = true
	log.Println("Node started successfully")
	return nil
}

// Stop gracefully shuts down all subsystems in reverse order.
func (n *Node) Stop() error {
	n.mu.Lock()
	defer n.mu.Unlock()

	if !n.running {
		return nil
	}

	log.Println("Stopping ETH2030 node...")

	// Stop Engine API.
	if err := n.engineServer.Stop(); err != nil {
		log.Printf("Engine API stop error: %v", err)
	}

	// Stop RPC server.
	if n.rpcServer != nil {
		if err := n.rpcServer.Close(); err != nil {
			log.Printf("RPC server stop error: %v", err)
		}
	}

	// Stop P2P server.
	n.p2pServer.Stop()

	// Close database.
	if err := n.db.Close(); err != nil {
		log.Printf("Database close error: %v", err)
	}

	n.running = false
	close(n.stop)
	log.Println("Node stopped")
	return nil
}

// Wait blocks until the node is stopped.
func (n *Node) Wait() {
	<-n.stop
}

// Blockchain returns the blockchain instance.
func (n *Node) Blockchain() *core.Blockchain {
	return n.blockchain
}

// TxPool returns the transaction pool.
func (n *Node) TxPool() *txpool.TxPool {
	return n.txPool
}

// Config returns the node configuration.
func (n *Node) Config() *Config {
	return n.config
}

// Running reports whether the node is currently running.
func (n *Node) Running() bool {
	n.mu.Lock()
	defer n.mu.Unlock()
	return n.running
}

// chainConfigForNetwork returns the chain config for the given network name.
func chainConfigForNetwork(network string) *core.ChainConfig {
	switch network {
	case "mainnet":
		return core.MainnetConfig
	case "sepolia":
		return core.SepoliaConfig
	case "holesky":
		return core.HoleskyConfig
	default:
		return core.MainnetConfig
	}
}

// genesisForNetwork returns the genesis specification for the given network.
func genesisForNetwork(network string) *core.Genesis {
	switch network {
	case "mainnet":
		return core.DefaultGenesisBlock()
	case "sepolia":
		return core.DefaultSepoliaGenesisBlock()
	case "holesky":
		return core.DefaultHoleskyGenesisBlock()
	default:
		return core.DefaultGenesisBlock()
	}
}

// makeGenesisBlock creates a minimal genesis block.
func makeGenesisBlock() *types.Block {
	header := &types.Header{
		Number:     big.NewInt(0),
		GasLimit:   30_000_000,
		GasUsed:    0,
		Time:       0,
		Difficulty: new(big.Int),
		BaseFee:    big.NewInt(1_000_000_000), // 1 gwei
		UncleHash:  types.EmptyUncleHash,
	}
	return types.NewBlock(header, nil)
}
