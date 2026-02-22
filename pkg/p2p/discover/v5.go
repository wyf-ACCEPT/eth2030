package discover

import (
	"crypto/ecdsa"
	"crypto/rand"
	"encoding/binary"
	"errors"
	"net"
	"sync"

	"github.com/eth2030/eth2030/crypto"
	"github.com/eth2030/eth2030/p2p/enode"
	"github.com/eth2030/eth2030/p2p/enr"
	"github.com/eth2030/eth2030/rlp"
)

// Discovery V5 message type codes.
const (
	MsgPing      byte = 0x01
	MsgPong      byte = 0x02
	MsgFindNode  byte = 0x03
	MsgNodes     byte = 0x04
	MsgWhoAreYou byte = 0x05
	MsgHandshake byte = 0x06
)

// Protocol constants.
const (
	// MaxNodesPerPacket is the maximum number of node records in a Nodes response.
	MaxNodesPerPacket = 16

	// NonceSize is the size of the message nonce.
	NonceSize = 12

	// HeaderSize is the size of the static packet header.
	HeaderSize = 63 // simplified
)

// Errors.
var (
	ErrSessionNotFound = errors.New("discover: session not found")
	ErrInvalidMessage  = errors.New("discover: invalid message")
	ErrClosed          = errors.New("discover: protocol closed")
)

// Ping is the Discovery V5 PING message.
type Ping struct {
	ReqID  []byte
	ENRSeq uint64 // local ENR sequence number
}

// Pong is the Discovery V5 PONG response.
type Pong struct {
	ReqID  []byte
	ENRSeq uint64 // remote ENR sequence number
	ToIP   net.IP
	ToPort uint16
}

// FindNode is the Discovery V5 FINDNODE request.
type FindNode struct {
	ReqID     []byte
	Distances []uint // log distances to search
}

// Nodes is the Discovery V5 NODES response.
type Nodes struct {
	ReqID    []byte
	Total    uint8
	ENRs     [][]byte // RLP-encoded ENR records
}

// WhoAreYou is the Discovery V5 WHOAREYOU challenge.
type WhoAreYou struct {
	Nonce     [NonceSize]byte
	IDNonce   [16]byte // random data for identity proof
	ENRSeq    uint64   // highest known ENR seq of the challenged node
}

// Handshake is the Discovery V5 handshake response.
type Handshake struct {
	SrcID     enode.NodeID
	IDSig     []byte     // signature proving identity
	EPubkey   []byte     // ephemeral public key
	Record    []byte     // optional ENR record (if ENRSeq in challenge < local)
}

// Session holds state for an active V5 session with a remote node.
type Session struct {
	NodeID      enode.NodeID
	RemoteKey   []byte // compressed secp256k1 public key
	ReadKey     [16]byte
	WriteKey    [16]byte
	Counter     uint64
	Established bool
}

// V5Protocol implements the Discovery V5 UDP protocol.
type V5Protocol struct {
	mu        sync.RWMutex
	table     *Table
	conn      net.PacketConn
	localNode *enode.Node
	privKey   *ecdsa.PrivateKey
	sessions  map[enode.NodeID]*Session
	closed    bool
	closeCh   chan struct{}
}

// NewV5Protocol creates a new Discovery V5 protocol handler.
func NewV5Protocol(conn net.PacketConn, privKey *ecdsa.PrivateKey, localNode *enode.Node) *V5Protocol {
	return &V5Protocol{
		table:     NewTable(localNode.ID),
		conn:      conn,
		localNode: localNode,
		privKey:   privKey,
		sessions:  make(map[enode.NodeID]*Session),
		closeCh:   make(chan struct{}),
	}
}

// Table returns the underlying routing table.
func (p *V5Protocol) Table() *Table {
	return p.table
}

// Start begins listening for incoming packets.
func (p *V5Protocol) Start() error {
	go p.readLoop()
	return nil
}

// Stop shuts down the protocol.
func (p *V5Protocol) Stop() {
	p.mu.Lock()
	defer p.mu.Unlock()
	if !p.closed {
		p.closed = true
		close(p.closeCh)
		p.conn.Close()
	}
}

// readLoop reads incoming packets from the connection.
func (p *V5Protocol) readLoop() {
	buf := make([]byte, 1280) // max UDP payload
	for {
		select {
		case <-p.closeCh:
			return
		default:
		}
		n, addr, err := p.conn.ReadFrom(buf)
		if err != nil {
			select {
			case <-p.closeCh:
				return
			default:
				continue
			}
		}
		udpAddr, ok := addr.(*net.UDPAddr)
		if !ok {
			continue
		}
		data := make([]byte, n)
		copy(data, buf[:n])
		p.HandlePacket(*udpAddr, data)
	}
}

// HandlePacket processes an incoming UDP packet.
func (p *V5Protocol) HandlePacket(from net.UDPAddr, data []byte) {
	if len(data) < 2 {
		return
	}
	msgType := data[0]

	switch msgType {
	case MsgPing:
		p.handlePing(from, data[1:])
	case MsgPong:
		p.handlePong(from, data[1:])
	case MsgFindNode:
		p.handleFindNode(from, data[1:])
	case MsgNodes:
		p.handleNodes(from, data[1:])
	case MsgWhoAreYou:
		p.handleWhoAreYou(from, data[1:])
	case MsgHandshake:
		p.handleHandshake(from, data[1:])
	}
}

func (p *V5Protocol) handlePing(from net.UDPAddr, data []byte) {
	var ping Ping
	if err := rlp.DecodeBytes(data, &ping); err != nil {
		return
	}
	// Respond with pong.
	pong := Pong{
		ReqID:  ping.ReqID,
		ENRSeq: p.localNode.Record.Seq,
		ToIP:   from.IP,
		ToPort: uint16(from.Port),
	}
	p.sendMessage(from, MsgPong, pong)
}

func (p *V5Protocol) handlePong(_ net.UDPAddr, data []byte) {
	var pong Pong
	if err := rlp.DecodeBytes(data, &pong); err != nil {
		return
	}
	// Pong received - session confirmed. In a full implementation this
	// would resolve a pending callback.
}

func (p *V5Protocol) handleFindNode(from net.UDPAddr, data []byte) {
	var req FindNode
	if err := rlp.DecodeBytes(data, &req); err != nil {
		return
	}

	// Collect nodes at the requested distances.
	var matches []*enode.Node
	for _, dist := range req.Distances {
		if dist == 0 {
			matches = append(matches, p.localNode)
			continue
		}
		if int(dist) > NumBuckets {
			continue
		}
		entries := p.table.BucketEntries(int(dist) - 1)
		matches = append(matches, entries...)
	}

	// Encode matched nodes as ENR records.
	var enrs [][]byte
	for _, n := range matches {
		if n.Record != nil {
			encoded, err := enr.EncodeENR(n.Record)
			if err == nil {
				enrs = append(enrs, encoded)
			}
		}
	}

	// Split into packets of MaxNodesPerPacket.
	total := (len(enrs) + MaxNodesPerPacket - 1) / MaxNodesPerPacket
	if total == 0 {
		total = 1
	}
	for i := 0; i < total; i++ {
		start := i * MaxNodesPerPacket
		end := start + MaxNodesPerPacket
		if end > len(enrs) {
			end = len(enrs)
		}
		var chunk [][]byte
		if start < len(enrs) {
			chunk = enrs[start:end]
		}
		resp := Nodes{
			ReqID: req.ReqID,
			Total: uint8(total),
			ENRs:  chunk,
		}
		p.sendMessage(from, MsgNodes, resp)
	}
}

func (p *V5Protocol) handleNodes(_ net.UDPAddr, data []byte) {
	var nodes Nodes
	if err := rlp.DecodeBytes(data, &nodes); err != nil {
		return
	}
	// Decode each ENR and add to the routing table.
	for _, raw := range nodes.ENRs {
		rec, err := enr.DecodeENR(raw)
		if err != nil {
			continue
		}
		id := rec.NodeID()
		ipBytes := rec.Get(enr.KeyIP)
		if len(ipBytes) < 4 {
			continue
		}
		udpBytes := rec.Get(enr.KeyUDP)
		tcpBytes := rec.Get(enr.KeyTCP)

		node := &enode.Node{
			ID:     enode.NodeID(id),
			IP:     net.IP(ipBytes),
			Record: rec,
		}
		if len(udpBytes) >= 2 {
			node.UDP = binary.BigEndian.Uint16(udpBytes)
		}
		if len(tcpBytes) >= 2 {
			node.TCP = binary.BigEndian.Uint16(tcpBytes)
		}
		p.table.AddNode(node)
	}
}

func (p *V5Protocol) handleWhoAreYou(from net.UDPAddr, data []byte) {
	if len(data) < NonceSize+16+8 {
		return
	}
	var challenge WhoAreYou
	copy(challenge.Nonce[:], data[:NonceSize])
	copy(challenge.IDNonce[:], data[NonceSize:NonceSize+16])
	challenge.ENRSeq = binary.BigEndian.Uint64(data[NonceSize+16:])

	// Respond with handshake containing identity proof and optional ENR.
	p.respondHandshake(from, challenge)
}

func (p *V5Protocol) handleHandshake(from net.UDPAddr, data []byte) {
	var hs Handshake
	if err := rlp.DecodeBytes(data, &hs); err != nil {
		return
	}

	// Establish session.
	p.mu.Lock()
	p.sessions[hs.SrcID] = &Session{
		NodeID:      hs.SrcID,
		RemoteKey:   hs.EPubkey,
		Established: true,
	}
	p.mu.Unlock()
}

// respondHandshake sends a handshake response to a WHOAREYOU challenge.
func (p *V5Protocol) respondHandshake(to net.UDPAddr, challenge WhoAreYou) {
	// Sign the ID nonce to prove identity.
	sigInput := make([]byte, 0, 32+16)
	sigInput = append(sigInput, "discovery v5 identity proof"...)
	sigInput = append(sigInput, challenge.IDNonce[:]...)
	hash := crypto.Keccak256(sigInput)

	sig, err := crypto.Sign(hash, p.privKey)
	if err != nil {
		return
	}

	compressed := crypto.CompressPubkey(&p.privKey.PublicKey)

	hs := Handshake{
		SrcID:   p.localNode.ID,
		IDSig:   sig[:64],
		EPubkey: compressed,
	}

	// Include ENR if the challenger has an old version.
	if p.localNode.Record != nil && challenge.ENRSeq < p.localNode.Record.Seq {
		encoded, err := enr.EncodeENR(p.localNode.Record)
		if err == nil {
			hs.Record = encoded
		}
	}

	p.sendMessage(to, MsgHandshake, hs)
}

// SendPing sends a PING message to the target node.
func (p *V5Protocol) SendPing(to *enode.Node) error {
	p.mu.RLock()
	closed := p.closed
	p.mu.RUnlock()
	if closed {
		return ErrClosed
	}

	reqID := make([]byte, 8)
	rand.Read(reqID)

	var seq uint64
	if p.localNode.Record != nil {
		seq = p.localNode.Record.Seq
	}

	ping := Ping{
		ReqID:  reqID,
		ENRSeq: seq,
	}
	addr := to.Addr()
	return p.sendMessage(addr, MsgPing, ping)
}

// SendFindNode sends a FINDNODE request for the given distances.
func (p *V5Protocol) SendFindNode(to *enode.Node, distances []uint) error {
	p.mu.RLock()
	closed := p.closed
	p.mu.RUnlock()
	if closed {
		return ErrClosed
	}

	reqID := make([]byte, 8)
	rand.Read(reqID)

	req := FindNode{
		ReqID:     reqID,
		Distances: distances,
	}
	addr := to.Addr()
	return p.sendMessage(addr, MsgFindNode, req)
}

// sendMessage encodes and sends a message to the given address.
func (p *V5Protocol) sendMessage(to net.UDPAddr, msgType byte, msg interface{}) error {
	data, err := rlp.EncodeToBytes(msg)
	if err != nil {
		return err
	}
	packet := make([]byte, 1+len(data))
	packet[0] = msgType
	copy(packet[1:], data)
	_, err = p.conn.WriteTo(packet, &to)
	return err
}

// GetSession returns the session for a remote node, if one exists.
func (p *V5Protocol) GetSession(id enode.NodeID) (*Session, bool) {
	p.mu.RLock()
	defer p.mu.RUnlock()
	s, ok := p.sessions[id]
	return s, ok
}

// EncodeMessage encodes a V5 message for testing.
func EncodeMessage(msgType byte, msg interface{}) ([]byte, error) {
	data, err := rlp.EncodeToBytes(msg)
	if err != nil {
		return nil, err
	}
	packet := make([]byte, 1+len(data))
	packet[0] = msgType
	copy(packet[1:], data)
	return packet, nil
}

// DecodeMessage decodes a V5 message type from raw data.
func DecodeMessage(data []byte) (byte, []byte, error) {
	if len(data) < 2 {
		return 0, nil, ErrInvalidMessage
	}
	return data[0], data[1:], nil
}
