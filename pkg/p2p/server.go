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

	mu      sync.Mutex
	running bool
	quit    chan struct{}
	wg      sync.WaitGroup
}

// NewServer creates a new P2P server with the given configuration.
func NewServer(cfg Config) *Server {
	if cfg.MaxPeers <= 0 {
		cfg.MaxPeers = 25
	}
	return &Server{
		config: cfg,
		peers:  NewManagedPeerSet(cfg.MaxPeers),
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

// Peers returns a snapshot of connected peers.
func (srv *Server) PeersList() []*Peer {
	return srv.peers.Peers()
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

// setupConn handles a new connection: creates a peer and runs protocols.
func (srv *Server) setupConn(conn net.Conn, dialed bool) {
	t := NewFrameTransport(conn)

	// Generate a random peer ID for now (no ECIES handshake).
	id := randomID()
	peer := NewPeer(id, conn.RemoteAddr().String(), nil)

	if err := srv.peers.Add(peer); err != nil {
		t.Close()
		return
	}

	defer func() {
		srv.peers.Remove(peer.ID())
		t.Close()
	}()

	// Run the first matching protocol (simplified; real impl runs all).
	if len(srv.config.Protocols) > 0 {
		proto := srv.config.Protocols[0]
		if proto.Run != nil {
			_ = proto.Run(peer, t)
		}
	} else {
		// No protocol handler; just wait until quit.
		<-srv.quit
	}
}

// randomID generates a random 32-byte hex-encoded peer ID.
func randomID() string {
	var b [32]byte
	rand.Read(b[:])
	return hex.EncodeToString(b[:])
}
