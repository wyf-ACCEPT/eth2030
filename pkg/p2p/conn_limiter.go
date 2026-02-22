// conn_limiter.go implements connection management with per-subnet limits,
// inbound/outbound ratio management, rate limiting, connection deduplication,
// and metrics export to prevent eclipse attacks and resource exhaustion.
package p2p

import (
	"errors"
	"fmt"
	"net"
	"sync"
	"time"

	"github.com/eth2030/eth2030/metrics"
)

// ConnLim errors.
var (
	ErrConnLimMaxPeers       = errors.New("p2p: connection limit reached")
	ErrConnLimSubnet16Full   = errors.New("p2p: /16 subnet limit reached")
	ErrConnLimSubnet24Full   = errors.New("p2p: /24 subnet limit reached")
	ErrConnLimInboundFull    = errors.New("p2p: inbound connection limit reached")
	ErrConnLimOutboundFull   = errors.New("p2p: outbound connection limit reached")
	ErrConnLimRateLimited    = errors.New("p2p: inbound rate limit exceeded")
	ErrConnLimDuplicate      = errors.New("p2p: duplicate connection attempt")
	ErrConnLimReservedSlot   = errors.New("p2p: reserved slot not available")
	ErrConnLimAlreadyTracked = errors.New("p2p: connection already tracked")
)

// ConnDirection identifies the direction of a connection.
type ConnDirection int

const (
	ConnInbound  ConnDirection = iota // Peer connected to us.
	ConnOutbound                      // We connected to peer.
)

// String returns the direction name.
func (d ConnDirection) String() string {
	if d == ConnInbound {
		return "inbound"
	}
	return "outbound"
}

// ConnLimConfig configures the ConnLim connection manager.
type ConnLimConfig struct {
	MaxPeers          int           // Total max connections (default 50).
	MaxInbound        int           // Max inbound connections (default MaxPeers/2).
	MaxOutbound       int           // Max outbound connections (default MaxPeers/2).
	MaxPerSubnet16    int           // Max connections from a /16 subnet (default 4).
	MaxPerSubnet24    int           // Max connections from a /24 subnet (default 2).
	StaticSlots       int           // Reserved slots for static peers (default 5).
	TrustedSlots      int           // Reserved slots for trusted peers (default 5).
	InboundRateLimit  int           // Max new inbound conns per InboundRateWindow.
	InboundRateWindow time.Duration // Window for inbound rate limiting (default 10s).
}

// DefaultConnLimConfig returns production defaults.
func DefaultConnLimConfig() ConnLimConfig {
	return ConnLimConfig{
		MaxPeers:          50,
		MaxInbound:        25,
		MaxOutbound:       25,
		MaxPerSubnet16:    4,
		MaxPerSubnet24:    2,
		StaticSlots:       5,
		TrustedSlots:      5,
		InboundRateLimit:  10,
		InboundRateWindow: 10 * time.Second,
	}
}

// ConnEntry tracks a single active connection.
type ConnEntry struct {
	PeerID    string
	RemoteIP  net.IP
	Direction ConnDirection
	IsStatic  bool
	IsTrusted bool
	OpenedAt  time.Time
}

// ConnLim enforces connection limits including per-subnet caps, inbound/outbound
// ratio management, reserved slots, rate limiting, and deduplication. All methods
// are safe for concurrent use.
type ConnLim struct {
	mu     sync.RWMutex
	config ConnLimConfig
	conns  map[string]*ConnEntry // peerID -> entry

	// Subnet tracking: key = subnet prefix string.
	subnet16 map[string]int // /16 prefix -> count
	subnet24 map[string]int // /24 prefix -> count

	// Rate limiting for inbound connections.
	inboundTimes []time.Time

	// Deduplication: tracks recent connection attempts.
	recentAttempts map[string]time.Time // peerID -> last attempt time

	// Metrics.
	metricActive       *metrics.Gauge
	metricInbound      *metrics.Gauge
	metricOutbound     *metrics.Gauge
	metricSubnetReject *metrics.Counter
	metricRateReject   *metrics.Counter
	metricDupReject    *metrics.Counter
}

// NewConnLim creates a connection limiter with the given config.
func NewConnLim(cfg ConnLimConfig) *ConnLim {
	if cfg.MaxPeers <= 0 {
		cfg.MaxPeers = 50
	}
	if cfg.MaxInbound <= 0 {
		cfg.MaxInbound = cfg.MaxPeers / 2
	}
	if cfg.MaxOutbound <= 0 {
		cfg.MaxOutbound = cfg.MaxPeers / 2
	}
	if cfg.MaxPerSubnet16 <= 0 {
		cfg.MaxPerSubnet16 = 4
	}
	if cfg.MaxPerSubnet24 <= 0 {
		cfg.MaxPerSubnet24 = 2
	}
	if cfg.InboundRateWindow <= 0 {
		cfg.InboundRateWindow = 10 * time.Second
	}
	if cfg.InboundRateLimit <= 0 {
		cfg.InboundRateLimit = 10
	}
	return &ConnLim{
		config:             cfg,
		conns:              make(map[string]*ConnEntry),
		subnet16:           make(map[string]int),
		subnet24:           make(map[string]int),
		recentAttempts:     make(map[string]time.Time),
		metricActive:       metrics.NewGauge("p2p_conn_active"),
		metricInbound:      metrics.NewGauge("p2p_conn_inbound"),
		metricOutbound:     metrics.NewGauge("p2p_conn_outbound"),
		metricSubnetReject: metrics.NewCounter("p2p_conn_subnet_reject"),
		metricRateReject:   metrics.NewCounter("p2p_conn_rate_reject"),
		metricDupReject:    metrics.NewCounter("p2p_conn_dup_reject"),
	}
}

// CanConnect checks whether a new connection is allowed without actually
// adding it. Returns nil if allowed, or an error explaining the rejection.
func (cl *ConnLim) CanConnect(peerID string, remoteIP net.IP, dir ConnDirection, isStatic, isTrusted bool) error {
	cl.mu.RLock()
	defer cl.mu.RUnlock()

	return cl.canConnectLocked(peerID, remoteIP, dir, isStatic, isTrusted)
}

// AddConn registers a new connection. Returns an error if the connection
// violates any limits or is a duplicate.
func (cl *ConnLim) AddConn(peerID string, remoteIP net.IP, dir ConnDirection, isStatic, isTrusted bool) error {
	cl.mu.Lock()
	defer cl.mu.Unlock()

	// Check for already tracked.
	if _, exists := cl.conns[peerID]; exists {
		return ErrConnLimAlreadyTracked
	}

	// Run all admission checks.
	if err := cl.canConnectLocked(peerID, remoteIP, dir, isStatic, isTrusted); err != nil {
		return err
	}

	now := time.Now()
	entry := &ConnEntry{
		PeerID:    peerID,
		RemoteIP:  remoteIP,
		Direction: dir,
		IsStatic:  isStatic,
		IsTrusted: isTrusted,
		OpenedAt:  now,
	}

	cl.conns[peerID] = entry

	// Update subnet counts.
	s16 := subnetKey16(remoteIP)
	s24 := subnetKey24(remoteIP)
	cl.subnet16[s16]++
	cl.subnet24[s24]++

	// Track inbound rate.
	if dir == ConnInbound {
		cl.inboundTimes = append(cl.inboundTimes, now)
	}

	// Clear dedup entry.
	delete(cl.recentAttempts, peerID)

	cl.updateMetricsLocked()
	return nil
}

// RemoveConn removes a connection and updates all tracking state.
func (cl *ConnLim) RemoveConn(peerID string) {
	cl.mu.Lock()
	defer cl.mu.Unlock()

	entry, ok := cl.conns[peerID]
	if !ok {
		return
	}

	delete(cl.conns, peerID)

	// Decrement subnet counts.
	s16 := subnetKey16(entry.RemoteIP)
	s24 := subnetKey24(entry.RemoteIP)
	if cl.subnet16[s16] > 0 {
		cl.subnet16[s16]--
		if cl.subnet16[s16] == 0 {
			delete(cl.subnet16, s16)
		}
	}
	if cl.subnet24[s24] > 0 {
		cl.subnet24[s24]--
		if cl.subnet24[s24] == 0 {
			delete(cl.subnet24, s24)
		}
	}

	cl.updateMetricsLocked()
}

// RecordAttempt records a connection attempt for deduplication. Subsequent
// calls to CanConnect within 5 seconds for the same peerID will be rejected.
func (cl *ConnLim) RecordAttempt(peerID string) {
	cl.mu.Lock()
	defer cl.mu.Unlock()
	cl.recentAttempts[peerID] = time.Now()
}

// ConnCount returns the total number of active connections.
func (cl *ConnLim) ConnCount() int {
	cl.mu.RLock()
	defer cl.mu.RUnlock()
	return len(cl.conns)
}

// InboundConnCount returns the number of inbound connections.
func (cl *ConnLim) InboundConnCount() int {
	cl.mu.RLock()
	defer cl.mu.RUnlock()
	return cl.countByDirection(ConnInbound)
}

// OutboundConnCount returns the number of outbound connections.
func (cl *ConnLim) OutboundConnCount() int {
	cl.mu.RLock()
	defer cl.mu.RUnlock()
	return cl.countByDirection(ConnOutbound)
}

// Subnet16Count returns the number of connections from the given /16 subnet.
func (cl *ConnLim) Subnet16Count(ip net.IP) int {
	cl.mu.RLock()
	defer cl.mu.RUnlock()
	return cl.subnet16[subnetKey16(ip)]
}

// Subnet24Count returns the number of connections from the given /24 subnet.
func (cl *ConnLim) Subnet24Count(ip net.IP) int {
	cl.mu.RLock()
	defer cl.mu.RUnlock()
	return cl.subnet24[subnetKey24(ip)]
}

// EvictLowestPriority identifies the best candidate for eviction when the
// connection table is full. It returns the peerID of the least-desirable
// non-static, non-trusted inbound peer, or empty string if none can be evicted.
func (cl *ConnLim) EvictLowestPriority() string {
	cl.mu.RLock()
	defer cl.mu.RUnlock()

	var candidate string
	var oldestTime time.Time

	for id, entry := range cl.conns {
		if entry.IsStatic || entry.IsTrusted {
			continue
		}
		// Prefer evicting inbound connections.
		if entry.Direction != ConnInbound {
			continue
		}
		if candidate == "" || entry.OpenedAt.Before(oldestTime) {
			candidate = id
			oldestTime = entry.OpenedAt
		}
	}

	// If no inbound candidate, try outbound non-static non-trusted.
	if candidate == "" {
		for id, entry := range cl.conns {
			if entry.IsStatic || entry.IsTrusted {
				continue
			}
			if candidate == "" || entry.OpenedAt.Before(oldestTime) {
				candidate = id
				oldestTime = entry.OpenedAt
			}
		}
	}
	return candidate
}

// ActiveConns returns a snapshot of all active connections.
func (cl *ConnLim) ActiveConns() []ConnEntry {
	cl.mu.RLock()
	defer cl.mu.RUnlock()

	result := make([]ConnEntry, 0, len(cl.conns))
	for _, entry := range cl.conns {
		result = append(result, *entry)
	}
	return result
}

// AvailableSlots returns the number of connection slots remaining.
func (cl *ConnLim) AvailableSlots() int {
	cl.mu.RLock()
	defer cl.mu.RUnlock()
	remaining := cl.config.MaxPeers - len(cl.conns)
	if remaining < 0 {
		return 0
	}
	return remaining
}

// --- internal helpers ---

// canConnectLocked performs all admission checks. Caller must hold at least
// a read lock on cl.mu.
func (cl *ConnLim) canConnectLocked(peerID string, remoteIP net.IP, dir ConnDirection, isStatic, isTrusted bool) error {
	// Static and trusted peers bypass most limits but still respect max peers.
	if isStatic || isTrusted {
		totalReserved := cl.config.StaticSlots + cl.config.TrustedSlots
		if len(cl.conns) >= cl.config.MaxPeers+totalReserved {
			return ErrConnLimMaxPeers
		}
		return nil
	}

	// Check global limit.
	if len(cl.conns) >= cl.config.MaxPeers {
		return ErrConnLimMaxPeers
	}

	// Check direction limits.
	if dir == ConnInbound && cl.countByDirection(ConnInbound) >= cl.config.MaxInbound {
		return ErrConnLimInboundFull
	}
	if dir == ConnOutbound && cl.countByDirection(ConnOutbound) >= cl.config.MaxOutbound {
		return ErrConnLimOutboundFull
	}

	// Check subnet limits.
	if remoteIP != nil {
		s16 := subnetKey16(remoteIP)
		if cl.subnet16[s16] >= cl.config.MaxPerSubnet16 {
			cl.metricSubnetReject.Inc()
			return ErrConnLimSubnet16Full
		}
		s24 := subnetKey24(remoteIP)
		if cl.subnet24[s24] >= cl.config.MaxPerSubnet24 {
			cl.metricSubnetReject.Inc()
			return ErrConnLimSubnet24Full
		}
	}

	// Rate limiting for inbound connections.
	if dir == ConnInbound {
		now := time.Now()
		windowStart := now.Add(-cl.config.InboundRateWindow)
		recentCount := 0
		for _, t := range cl.inboundTimes {
			if t.After(windowStart) {
				recentCount++
			}
		}
		if recentCount >= cl.config.InboundRateLimit {
			cl.metricRateReject.Inc()
			return ErrConnLimRateLimited
		}
	}

	// Deduplication check: reject if attempted within last 5 seconds.
	if lastAttempt, ok := cl.recentAttempts[peerID]; ok {
		if time.Since(lastAttempt) < 5*time.Second {
			cl.metricDupReject.Inc()
			return ErrConnLimDuplicate
		}
	}

	return nil
}

// countByDirection counts connections in the given direction.
func (cl *ConnLim) countByDirection(dir ConnDirection) int {
	count := 0
	for _, entry := range cl.conns {
		if entry.Direction == dir {
			count++
		}
	}
	return count
}

// updateMetricsLocked updates the connection metrics gauges.
// Caller must hold cl.mu.
func (cl *ConnLim) updateMetricsLocked() {
	cl.metricActive.Set(int64(len(cl.conns)))
	cl.metricInbound.Set(int64(cl.countByDirection(ConnInbound)))
	cl.metricOutbound.Set(int64(cl.countByDirection(ConnOutbound)))
}

// subnetKey16 returns the /16 prefix string for an IP address.
func subnetKey16(ip net.IP) string {
	ip4 := ip.To4()
	if ip4 == nil {
		// For IPv6, use the first 4 bytes as a coarse prefix.
		if len(ip) >= 4 {
			return fmt.Sprintf("%d.%d", ip[0], ip[1])
		}
		return "unknown"
	}
	return fmt.Sprintf("%d.%d", ip4[0], ip4[1])
}

// subnetKey24 returns the /24 prefix string for an IP address.
func subnetKey24(ip net.IP) string {
	ip4 := ip.To4()
	if ip4 == nil {
		if len(ip) >= 6 {
			return fmt.Sprintf("%d.%d.%d", ip[0], ip[1], ip[2])
		}
		return "unknown"
	}
	return fmt.Sprintf("%d.%d.%d", ip4[0], ip4[1], ip4[2])
}
