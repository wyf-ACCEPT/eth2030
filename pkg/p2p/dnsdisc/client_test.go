package dnsdisc

import (
	"errors"
	"testing"
	"time"
)

// mockResolver provides canned DNS TXT responses for testing.
type mockResolver struct {
	records map[string][]string
}

func (m *mockResolver) LookupTXT(domain string) ([]string, error) {
	if recs, ok := m.records[domain]; ok {
		return recs, nil
	}
	return nil, errors.New("dns: no such domain")
}

func TestParseTreeRoot(t *testing.T) {
	txt := "enrtree-root:v1 e=ABCD1234 l=LINK5678 seq=42 sig=dGVzdHNpZw=="
	root, err := ParseTreeRoot(txt)
	if err != nil {
		t.Fatalf("ParseTreeRoot error: %v", err)
	}
	if root.ERoot != "ABCD1234" {
		t.Errorf("ERoot = %q, want ABCD1234", root.ERoot)
	}
	if root.LRoot != "LINK5678" {
		t.Errorf("LRoot = %q, want LINK5678", root.LRoot)
	}
	if root.Seq != 42 {
		t.Errorf("Seq = %d, want 42", root.Seq)
	}
	if root.Sig != "dGVzdHNpZw==" {
		t.Errorf("Sig = %q, want dGVzdHNpZw==", root.Sig)
	}
}

func TestParseTreeRootInvalid(t *testing.T) {
	tests := []string{
		"not-a-root",
		"enrtree-root:v2 e=foo",
		"enrtree-root:v1",
	}
	for _, txt := range tests {
		_, err := ParseTreeRoot(txt)
		if err == nil {
			t.Errorf("expected error for %q", txt)
		}
	}
}

func TestParseTreeLink(t *testing.T) {
	txt := "enrtree://PUBKEY123@nodes.example.org"
	link, err := ParseTreeLink(txt)
	if err != nil {
		t.Fatalf("ParseTreeLink error: %v", err)
	}
	if link.PublicKey != "PUBKEY123" {
		t.Errorf("PublicKey = %q, want PUBKEY123", link.PublicKey)
	}
	if link.Domain != "nodes.example.org" {
		t.Errorf("Domain = %q, want nodes.example.org", link.Domain)
	}
}

func TestParseTreeLinkInvalid(t *testing.T) {
	tests := []string{
		"not-a-link",
		"enrtree://noapart",
	}
	for _, txt := range tests {
		_, err := ParseTreeLink(txt)
		if err == nil {
			t.Errorf("expected error for %q", txt)
		}
	}
}

func TestDNSClientResolve(t *testing.T) {
	resolver := &mockResolver{
		records: map[string][]string{
			"nodes.example.org": {
				"enrtree-root:v1 e=BRANCH1 l=LINKS seq=1 sig=test",
			},
			"BRANCH1.nodes.example.org": {
				"enrtree-branch:LEAF1,LEAF2",
			},
			"LEAF1.nodes.example.org": {
				"enr:abcdef0123456789abcdef0123456789ab",
			},
			"LEAF2.nodes.example.org": {
				"enr:fedcba9876543210fedcba9876543210fe",
			},
		},
	}

	config := DNSConfig{
		Domain:          "nodes.example.org",
		RefreshInterval: 1 * time.Second,
	}
	client := NewDNSClientWithResolver(config, resolver)

	nodes, err := client.Resolve("nodes.example.org")
	if err != nil {
		t.Fatalf("Resolve error: %v", err)
	}

	if len(nodes) != 2 {
		t.Fatalf("got %d nodes, want 2", len(nodes))
	}
}

func TestDNSClientResolveNoRoot(t *testing.T) {
	resolver := &mockResolver{
		records: map[string][]string{
			"nodes.example.org": {"not-a-root-record"},
		},
	}

	config := DNSConfig{Domain: "nodes.example.org"}
	client := NewDNSClientWithResolver(config, resolver)

	_, err := client.Resolve("nodes.example.org")
	if err == nil {
		t.Fatal("expected error for missing root")
	}
}

func TestDNSClientRandomNodes(t *testing.T) {
	resolver := &mockResolver{
		records: map[string][]string{
			"test.org": {
				"enrtree-root:v1 e=B1 l=L1 seq=1 sig=s",
			},
			"B1.test.org": {
				"enrtree-branch:N1,N2,N3",
			},
			"N1.test.org": {"enr:node1_______________________________"},
			"N2.test.org": {"enr:node2_______________________________"},
			"N3.test.org": {"enr:node3_______________________________"},
		},
	}

	config := DNSConfig{Domain: "test.org"}
	client := NewDNSClientWithResolver(config, resolver)

	// Populate cache.
	_, err := client.Resolve("test.org")
	if err != nil {
		t.Fatalf("Resolve error: %v", err)
	}

	// Request fewer nodes than available.
	nodes := client.RandomNodes(2)
	if len(nodes) != 2 {
		t.Errorf("got %d random nodes, want 2", len(nodes))
	}

	// Request more than available.
	nodes = client.RandomNodes(100)
	if len(nodes) != 3 {
		t.Errorf("got %d random nodes, want 3 (all cached)", len(nodes))
	}
}

func TestDNSClientCachedNodes(t *testing.T) {
	resolver := &mockResolver{
		records: map[string][]string{
			"test.org": {
				"enrtree-root:v1 e=B1 l=L1 seq=1 sig=s",
			},
			"B1.test.org": {
				"enr:nodeA_______________________________",
			},
		},
	}

	config := DNSConfig{Domain: "test.org"}
	client := NewDNSClientWithResolver(config, resolver)

	_, _ = client.Resolve("test.org")
	cached := client.CachedNodes()
	if len(cached) != 1 {
		t.Errorf("cached nodes = %d, want 1", len(cached))
	}
}

func TestDNSClientRefreshCache(t *testing.T) {
	resolver := &mockResolver{
		records: map[string][]string{
			"test.org": {
				"enrtree-root:v1 e=B1 l=L1 seq=1 sig=s",
			},
			"B1.test.org": {
				"enr:nodeRefresh________________________",
			},
		},
	}

	config := DNSConfig{Domain: "test.org"}
	client := NewDNSClientWithResolver(config, resolver)

	if err := client.RefreshCache(); err != nil {
		t.Fatalf("RefreshCache error: %v", err)
	}

	cached := client.CachedNodes()
	if len(cached) != 1 {
		t.Errorf("cached nodes = %d, want 1", len(cached))
	}
}

func TestDNSClientDefaultRefreshInterval(t *testing.T) {
	config := DNSConfig{Domain: "test.org"}
	client := NewDNSClient(config)
	if client.config.RefreshInterval != defaultRefreshInterval {
		t.Errorf("default interval = %v, want %v", client.config.RefreshInterval, defaultRefreshInterval)
	}
}
