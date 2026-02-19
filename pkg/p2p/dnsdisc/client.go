// Package dnsdisc implements EIP-1459 DNS-based node discovery.
// It resolves Ethereum node records from DNS TXT records organized as
// a Merkle tree, allowing light clients to bootstrap without connecting
// to a DHT.
package dnsdisc

import (
	"errors"
	"fmt"
	"math/rand"
	"net"
	"strings"
	"sync"
	"time"

	"github.com/eth2028/eth2028/p2p/enode"
)

// Default refresh interval for DNS cache.
const defaultRefreshInterval = 30 * time.Minute

// DNSConfig holds configuration for the DNS discovery client.
type DNSConfig struct {
	Domain          string
	PublicKey       []byte
	RefreshInterval time.Duration
}

// TreeRoot represents a parsed enr-root TXT record.
// Format: enrtree-root:v1 e=<eroot> l=<lroot> seq=<seq> sig=<sig>
type TreeRoot struct {
	ERoot string // hash of the ENR subtree root
	LRoot string // hash of the link subtree root
	Seq   uint64 // sequence number
	Sig   string // base64-encoded signature
}

// TreeLink represents a parsed enrtree-branch or link TXT record.
type TreeLink struct {
	Domain    string // linked domain
	PublicKey string // public key of the linked tree
}

// DNSClient resolves Ethereum node records from DNS.
type DNSClient struct {
	config   DNSConfig
	mu       sync.RWMutex
	cache    map[string]*enode.Node
	resolver Resolver
}

// Resolver abstracts DNS TXT record lookup for testing.
type Resolver interface {
	LookupTXT(domain string) ([]string, error)
}

// defaultResolver uses net.LookupTXT.
type defaultResolver struct{}

func (d *defaultResolver) LookupTXT(domain string) ([]string, error) {
	return net.LookupTXT(domain)
}

// NewDNSClient creates a new DNS discovery client.
func NewDNSClient(config DNSConfig) *DNSClient {
	if config.RefreshInterval == 0 {
		config.RefreshInterval = defaultRefreshInterval
	}
	return &DNSClient{
		config:   config,
		cache:    make(map[string]*enode.Node),
		resolver: &defaultResolver{},
	}
}

// NewDNSClientWithResolver creates a DNS client with a custom resolver (for testing).
func NewDNSClientWithResolver(config DNSConfig, resolver Resolver) *DNSClient {
	c := NewDNSClient(config)
	c.resolver = resolver
	return c
}

// Resolve queries DNS TXT records for the given domain and returns discovered nodes.
func (c *DNSClient) Resolve(domain string) ([]*enode.Node, error) {
	// Look up the root TXT record.
	root, err := c.resolveRoot(domain)
	if err != nil {
		return nil, fmt.Errorf("dnsdisc: failed to resolve root for %s: %w", domain, err)
	}

	// Resolve the ENR subtree.
	nodes, err := c.resolveTree(domain, root.ERoot)
	if err != nil {
		return nil, fmt.Errorf("dnsdisc: failed to resolve tree for %s: %w", domain, err)
	}

	// Cache discovered nodes.
	c.mu.Lock()
	for _, n := range nodes {
		c.cache[n.ID.String()] = n
	}
	c.mu.Unlock()

	return nodes, nil
}

// resolveRoot fetches and parses the root TXT record for a domain.
func (c *DNSClient) resolveRoot(domain string) (*TreeRoot, error) {
	records, err := c.resolver.LookupTXT(domain)
	if err != nil {
		return nil, err
	}
	for _, txt := range records {
		if root, err := ParseTreeRoot(txt); err == nil {
			return root, nil
		}
	}
	return nil, errors.New("no valid enrtree-root record found")
}

// resolveTree resolves nodes from a subtree hash.
func (c *DNSClient) resolveTree(domain, hash string) ([]*enode.Node, error) {
	subdomain := hash + "." + domain
	records, err := c.resolver.LookupTXT(subdomain)
	if err != nil {
		return nil, err
	}

	var nodes []*enode.Node
	for _, txt := range records {
		// Check for branch records (comma-separated hashes).
		if strings.HasPrefix(txt, "enrtree-branch:") {
			children := strings.TrimPrefix(txt, "enrtree-branch:")
			for _, child := range strings.Split(children, ",") {
				child = strings.TrimSpace(child)
				if child == "" {
					continue
				}
				childNodes, err := c.resolveTree(domain, child)
				if err != nil {
					continue // skip failed branches
				}
				nodes = append(nodes, childNodes...)
			}
		}

		// Check for ENR records.
		if strings.HasPrefix(txt, "enr:") {
			node, err := parseENRRecord(txt)
			if err != nil {
				continue
			}
			nodes = append(nodes, node)
		}

		// Check for link records.
		if strings.HasPrefix(txt, "enrtree://") {
			link, err := ParseTreeLink(txt)
			if err != nil {
				continue
			}
			// Recursively resolve linked tree.
			linkedNodes, err := c.Resolve(link.Domain)
			if err != nil {
				continue
			}
			nodes = append(nodes, linkedNodes...)
		}
	}

	return nodes, nil
}

// ParseTreeRoot parses an enrtree-root TXT record.
// Format: enrtree-root:v1 e=<eroot> l=<lroot> seq=<seq> sig=<sig>
func ParseTreeRoot(txt string) (*TreeRoot, error) {
	if !strings.HasPrefix(txt, "enrtree-root:v1") {
		return nil, errors.New("not an enrtree-root record")
	}

	root := &TreeRoot{}
	parts := strings.Fields(txt)
	for _, part := range parts[1:] {
		kv := strings.SplitN(part, "=", 2)
		if len(kv) != 2 {
			continue
		}
		switch kv[0] {
		case "e":
			root.ERoot = kv[1]
		case "l":
			root.LRoot = kv[1]
		case "seq":
			fmt.Sscanf(kv[1], "%d", &root.Seq)
		case "sig":
			root.Sig = kv[1]
		}
	}

	if root.ERoot == "" {
		return nil, errors.New("enrtree-root: missing eroot")
	}
	return root, nil
}

// ParseTreeLink parses an enrtree:// link record.
// Format: enrtree://<pubkey>@<domain>
func ParseTreeLink(txt string) (*TreeLink, error) {
	if !strings.HasPrefix(txt, "enrtree://") {
		return nil, errors.New("not an enrtree link")
	}

	rest := strings.TrimPrefix(txt, "enrtree://")
	atIdx := strings.Index(rest, "@")
	if atIdx < 0 {
		return nil, errors.New("enrtree link: missing @ separator")
	}

	return &TreeLink{
		PublicKey: rest[:atIdx],
		Domain:   rest[atIdx+1:],
	}, nil
}

// parseENRRecord parses a simplified ENR TXT record into a Node.
// A full implementation would decode the RLP-encoded ENR; here we
// create a node with a deterministic ID from the record content.
func parseENRRecord(txt string) (*enode.Node, error) {
	if !strings.HasPrefix(txt, "enr:") {
		return nil, errors.New("not an ENR record")
	}

	// Create a deterministic node ID from the ENR content.
	content := strings.TrimPrefix(txt, "enr:")
	var id enode.NodeID
	for i := 0; i < 32 && i < len(content); i++ {
		id[i] = content[i]
	}

	return enode.NewNode(id, net.IPv4(127, 0, 0, 1), 30303, 30303), nil
}

// RefreshCache re-resolves the configured domain and updates the cache.
func (c *DNSClient) RefreshCache() error {
	_, err := c.Resolve(c.config.Domain)
	return err
}

// RandomNodes returns up to count randomly selected nodes from the cache.
func (c *DNSClient) RandomNodes(count int) []*enode.Node {
	c.mu.RLock()
	defer c.mu.RUnlock()

	all := make([]*enode.Node, 0, len(c.cache))
	for _, n := range c.cache {
		all = append(all, n)
	}

	if len(all) <= count {
		return all
	}

	// Fisher-Yates shuffle and take first count.
	r := rand.New(rand.NewSource(time.Now().UnixNano()))
	r.Shuffle(len(all), func(i, j int) {
		all[i], all[j] = all[j], all[i]
	})
	return all[:count]
}

// CachedNodes returns all nodes currently in the cache.
func (c *DNSClient) CachedNodes() []*enode.Node {
	c.mu.RLock()
	defer c.mu.RUnlock()

	nodes := make([]*enode.Node, 0, len(c.cache))
	for _, n := range c.cache {
		nodes = append(nodes, n)
	}
	return nodes
}
