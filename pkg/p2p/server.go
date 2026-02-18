package p2p

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"log"
	"net"
	"sync"
)

// Config holds the configuration for a P2P Server.
type Config struct {
	// ListenAddr is the TCP address to listen on (e.g., ":30303").
	ListenAddr string

	// MaxPeers is the maximum number of connected peers.
	MaxPeers int

	// Protocols is the list of supported sub-protocols.
	Protocols []Protocol

	// StaticNodes is an initial list of enode URLs to always connect to.
	StaticNodes []string

	// EnableRLPx enables the RLPx encrypted transport (default: plaintext framing).
	EnableRLPx bool
}

// Protocol represents a sub-protocol that runs on top of the devp2p connection.
type Protocol struct {
	Name    string
	Version uint
	Length  uint64 // Number of message codes used by this protocol.

	// Run is called for each peer that supports this protocol.
	// It should read/write messages and return when done.
	Run func(peer *Peer, t Transport) error
}

// Server manages TCP connections and peer lifecycle.
type Server struct {
	config   Config
	listener net.Listener
	peers    *ManagedPeerSet
	nodes    *NodeTable
	scores   *ScoreMap

	mu      sync.Mutex
	running bool
	quit    chan struct{}
	wg      sync.WaitGroup
}

// ScoreMap tracks scores for all connected peers.
type ScoreMap struct {
	mu     sync.RWMutex
	scores map[string]*PeerScore
}

// NewScoreMap creates an empty score map.
func NewScoreMap() *ScoreMap {
	return &ScoreMap{scores: make(map[string]*PeerScore)}
}

// Get returns the score for a peer, creating one if it doesn't exist.
func (sm *ScoreMap) Get(id string) *PeerScore {
	sm.mu.Lock()
	defer sm.mu.Unlock()
	if s, ok := sm.scores[id]; ok {
		return s
	}
	s := NewPeerScore()
	sm.scores[id] = s
	return s
}

// Remove deletes the score for a peer.
func (sm *ScoreMap) Remove(id string) {
	sm.mu.Lock()
	defer sm.mu.Unlock()
	delete(sm.scores, id)
}

// All returns a snapshot of all peer IDs and their current scores.
func (sm *ScoreMap) All() map[string]float64 {
	sm.mu.RLock()
	defer sm.mu.RUnlock()
	result := make(map[string]float64, len(sm.scores))
	for id, s := range sm.scores {
		result[id] = s.Value()
	}
	return result
}

// NewServer creates a new P2P server with the given configuration.
func NewServer(cfg Config) *Server {
	if cfg.MaxPeers <= 0 {
		cfg.MaxPeers = 25
	}
	return &Server{
		config: cfg,
		peers:  NewManagedPeerSet(cfg.MaxPeers),
		nodes:  NewNodeTable(),
		scores: NewScoreMap(),
		quit:   make(chan struct{}),
	}
}

// Start begins listening for incoming connections.
func (srv *Server) Start() error {
	srv.mu.Lock()
	defer srv.mu.Unlock()

	if srv.running {
		return errors.New("p2p: server already running")
	}

	ln, err := net.Listen("tcp", srv.config.ListenAddr)
	if err != nil {
		return fmt.Errorf("p2p: listen error: %w", err)
	}
	srv.listener = ln
	srv.running = true

	// Load static nodes into the node table.
	for _, rawurl := range srv.config.StaticNodes {
		if node, err := ParseEnode(rawurl); err == nil {
			srv.nodes.AddStatic(node)
		}
	}

	srv.wg.Add(1)
	go srv.listenLoop()
	return nil
}

// Stop shuts down the server and disconnects all peers.
func (srv *Server) Stop() {
	srv.mu.Lock()
	if !srv.running {
		srv.mu.Unlock()
		return
	}
	srv.running = false
	close(srv.quit)
	srv.listener.Close()
	srv.mu.Unlock()

	srv.wg.Wait()
	srv.peers.Close()
}

// ListenAddr returns the actual listen address (useful when using ":0").
func (srv *Server) ListenAddr() net.Addr {
	srv.mu.Lock()
	defer srv.mu.Unlock()
	if srv.listener == nil {
		return nil
	}
	return srv.listener.Addr()
}

// AddPeer dials the given address and adds the connection as a peer.
func (srv *Server) AddPeer(addr string) error {
	conn, err := net.Dial("tcp", addr)
	if err != nil {
		return fmt.Errorf("p2p: dial error: %w", err)
	}

	srv.wg.Add(1)
	go func() {
		defer srv.wg.Done()
		srv.setupConn(conn, true)
	}()
	return nil
}

// PeerCount returns the number of connected peers.
func (srv *Server) PeerCount() int {
	return srv.peers.Len()
}

// PeersList returns a snapshot of connected peers.
func (srv *Server) PeersList() []*Peer {
	return srv.peers.Peers()
}

// NodeTable returns the server's node discovery table.
func (srv *Server) NodeTable() *NodeTable {
	return srv.nodes
}

// Scores returns the server's peer score map.
func (srv *Server) Scores() *ScoreMap {
	return srv.scores
}

// PeerScore returns the score tracker for a connected peer.
func (srv *Server) PeerScore(id string) *PeerScore {
	return srv.scores.Get(id)
}

func (srv *Server) listenLoop() {
	defer srv.wg.Done()

	for {
		conn, err := srv.listener.Accept()
		if err != nil {
			select {
			case <-srv.quit:
				return
			default:
				log.Printf("p2p: accept error: %v", err)
				continue
			}
		}

		srv.wg.Add(1)
		go func() {
			defer srv.wg.Done()
			srv.setupConn(conn, false)
		}()
	}
}

// setupConn handles a new connection: creates a peer, sets up transport,
// and runs all matching protocols via the multiplexer.
func (srv *Server) setupConn(conn net.Conn, dialed bool) {
	var tr Transport

	if srv.config.EnableRLPx {
		rlpx := NewRLPxTransport(conn)
		if err := rlpx.Handshake(dialed); err != nil {
			conn.Close()
			return
		}
		tr = rlpx
	} else {
		tr = NewFrameTransport(conn)
	}

	// Generate a random peer ID for now (no ECIES handshake).
	id := randomID()
	peer := NewPeer(id, conn.RemoteAddr().String(), nil)
	score := srv.scores.Get(id)

	if err := srv.peers.Add(peer); err != nil {
		tr.Close()
		return
	}

	defer func() {
		srv.peers.Remove(peer.ID())
		srv.scores.Remove(peer.ID())
		tr.Close()
	}()

	protos := srv.config.Protocols
	if len(protos) == 0 {
		// No protocol handler; wait until quit.
		<-srv.quit
		return
	}

	// Single protocol: run directly (backwards compatible with existing tests).
	if len(protos) == 1 {
		proto := protos[0]
		if proto.Run != nil {
			err := proto.Run(peer, tr)
			if err != nil {
				score.BadResponse()
			} else {
				score.HandshakeOK()
			}
		}
		return
	}

	// Multiple protocols: use multiplexer.
	mux := NewMultiplexer(tr, protos)

	// Start the read loop in the background.
	readErr := make(chan error, 1)
	go func() {
		readErr <- mux.ReadLoop()
	}()

	// Run each protocol in its own goroutine.
	var protoWG sync.WaitGroup
	for _, rw := range mux.Protocols() {
		protoWG.Add(1)
		go func(rw *ProtoRW) {
			defer protoWG.Done()
			if rw.proto.Run != nil {
				// Create a multiplexed transport adapter.
				adapter := &muxTransportAdapter{mux: mux, rw: rw}
				if err := rw.proto.Run(peer, adapter); err != nil {
					score.BadResponse()
				} else {
					score.GoodResponse()
				}
			}
		}(rw)
	}

	// Wait for the read loop to end (connection closed) or all protocols to finish.
	done := make(chan struct{})
	go func() {
		protoWG.Wait()
		close(done)
	}()

	select {
	case <-done:
		mux.Close()
	case <-readErr:
		mux.Close()
		protoWG.Wait()
	case <-srv.quit:
		mux.Close()
		protoWG.Wait()
	}
}

// muxTransportAdapter wraps the multiplexer to implement the Transport interface
// for a single protocol.
type muxTransportAdapter struct {
	mux *Multiplexer
	rw  *ProtoRW
}

func (a *muxTransportAdapter) ReadMsg() (Msg, error) {
	return a.rw.ReadMsg()
}

func (a *muxTransportAdapter) WriteMsg(msg Msg) error {
	return a.mux.WriteMsg(a.rw, msg)
}

func (a *muxTransportAdapter) Close() error {
	a.mux.Close()
	return nil
}

// randomID generates a random 32-byte hex-encoded peer ID.
func randomID() string {
	var b [32]byte
	rand.Read(b[:])
	return hex.EncodeToString(b[:])
}
