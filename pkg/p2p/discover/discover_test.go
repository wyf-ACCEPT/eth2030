package discover

import (
	"net"
	"testing"

	"github.com/eth2028/eth2028/p2p/enode"
)

func makeNode(b byte) *enode.Node {
	var id enode.NodeID
	id[31] = b
	return enode.NewNode(id, net.ParseIP("10.0.0.1"), 30303, 30303)
}

func makeNodeID(b byte) enode.NodeID {
	var id enode.NodeID
	id[31] = b
	return id
}

func TestNewTable(t *testing.T) {
	self := makeNodeID(0)
	tab := NewTable(self)
	if tab.Self() != self {
		t.Fatal("Self() mismatch")
	}
	if tab.Len() != 0 {
		t.Fatalf("Len() = %d, want 0", tab.Len())
	}
}

func TestAddNode(t *testing.T) {
	self := makeNodeID(0)
	tab := NewTable(self)

	n := makeNode(1)
	tab.AddNode(n)

	if tab.Len() != 1 {
		t.Fatalf("Len() = %d after AddNode", tab.Len())
	}
}

func TestAddSelf(t *testing.T) {
	self := makeNodeID(0)
	tab := NewTable(self)
	tab.AddNode(enode.NewNode(self, net.ParseIP("10.0.0.1"), 30303, 30303))

	if tab.Len() != 0 {
		t.Fatal("self should not be added to table")
	}
}

func TestAddDuplicate(t *testing.T) {
	self := makeNodeID(0)
	tab := NewTable(self)
	n := makeNode(1)

	tab.AddNode(n)
	tab.AddNode(n)

	if tab.Len() != 1 {
		t.Fatalf("Len() = %d after duplicate add", tab.Len())
	}
}

func TestBucketIndex(t *testing.T) {
	self := makeNodeID(0)
	tab := NewTable(self)

	// Distance 1 (last bit differs) -> bucket 0.
	if idx := tab.BucketIndex(makeNodeID(1)); idx != 0 {
		t.Fatalf("BucketIndex(0x01) = %d, want 0", idx)
	}

	// Self -> bucket -1.
	if idx := tab.BucketIndex(self); idx != -1 {
		t.Fatalf("BucketIndex(self) = %d, want -1", idx)
	}

	// Distance 256 (first bit differs): id with high bit set.
	var farID enode.NodeID
	farID[0] = 0x80
	if idx := tab.BucketIndex(farID); idx != 255 {
		t.Fatalf("BucketIndex(0x80...) = %d, want 255", idx)
	}
}

func TestBucketFull(t *testing.T) {
	self := makeNodeID(0)
	tab := NewTable(self)

	// Fill bucket 0 (distance 1) with BucketSize nodes.
	for i := byte(1); i <= BucketSize; i++ {
		var id enode.NodeID
		id[31] = i // all at distance 1..8 (within same bucket for small values)
		// Actually, different values produce different distances.
		// Use a single bucket by constructing IDs that all have distance 1.
		// id with only bit 0 set in last byte = distance 1 from 0.
		// We can't fit 16 nodes at exactly distance 1. Instead, let them
		// go into various nearby buckets.
		tab.AddNode(enode.NewNode(id, net.ParseIP("10.0.0.1"), 30303, 30303))
	}

	// All should be in the table.
	if tab.Len() != BucketSize {
		t.Fatalf("Len() = %d, want %d", tab.Len(), BucketSize)
	}
}

func TestRemoveNode(t *testing.T) {
	self := makeNodeID(0)
	tab := NewTable(self)

	n := makeNode(1)
	tab.AddNode(n)
	if tab.Len() != 1 {
		t.Fatal("node not added")
	}

	tab.RemoveNode(n.ID)
	if tab.Len() != 0 {
		t.Fatalf("Len() = %d after remove", tab.Len())
	}
}

func TestRemoveWithReplacement(t *testing.T) {
	self := makeNodeID(0)
	tab := NewTable(self)

	// All these nodes have the same bucket if self == 0 and ID differs only
	// in bits 0-3 of the last byte. Bucket = distance - 1.
	// Fill one bucket manually. Put 16 + 1 nodes at the same distance.
	// We need IDs with distance=1 from self. That means XOR = 1.
	// Only one such ID exists: 0x..01. So use a different approach:
	// Pick a bucket, fill it, then add one more that goes to replacements.

	// Bucket 7 = distance 8. Node IDs with distance 8: XOR has leading zeros
	// in all but the 8th bit from the right. So the last byte has bit 7 set.
	// ID = 0x00...0080
	// Actually distance 8 means XOR has 8 bits: the MSB of the XOR is in bit position 7.
	// For self=0, XOR=ID, so we need ID[31] with bit 7 set = 0x80..0xFF (256-128=128 values).
	// But specifically distance=8 means 128 <= XOR[31] <= 255, so IDs 0x80..0xFF all have distance 8.

	for i := byte(0x80); i < 0x80+BucketSize; i++ {
		var id enode.NodeID
		id[31] = i
		tab.AddNode(enode.NewNode(id, net.ParseIP("10.0.0.1"), 30303, 30303))
	}

	if tab.Len() != BucketSize {
		t.Fatalf("Len() = %d, want %d", tab.Len(), BucketSize)
	}

	// Add one more -> goes to replacements.
	var replID enode.NodeID
	replID[31] = 0x80 + BucketSize
	replNode := enode.NewNode(replID, net.ParseIP("10.0.0.1"), 30303, 30303)
	tab.AddNode(replNode)

	if tab.Len() != BucketSize {
		t.Fatalf("Len() = %d after overflow add, want %d", tab.Len(), BucketSize)
	}

	// Remove first entry -> replacement should be promoted.
	var firstID enode.NodeID
	firstID[31] = 0x80
	tab.RemoveNode(firstID)

	if tab.Len() != BucketSize {
		t.Fatalf("Len() = %d after remove with replacement, want %d", tab.Len(), BucketSize)
	}
}

func TestFindNode(t *testing.T) {
	self := makeNodeID(0)
	tab := NewTable(self)

	// Add several nodes.
	for i := byte(1); i <= 10; i++ {
		tab.AddNode(makeNode(i))
	}

	target := makeNodeID(5)
	closest := tab.FindNode(target, 3)

	if len(closest) != 3 {
		t.Fatalf("FindNode returned %d nodes, want 3", len(closest))
	}

	// Verify they're sorted by distance to target.
	for i := 1; i < len(closest); i++ {
		if enode.DistCmp(target, closest[i-1].ID, closest[i].ID) > 0 {
			t.Fatal("FindNode results not sorted by distance")
		}
	}
}

func TestFindNodeEmpty(t *testing.T) {
	self := makeNodeID(0)
	tab := NewTable(self)

	result := tab.FindNode(makeNodeID(5), 3)
	if len(result) != 0 {
		t.Fatalf("FindNode on empty table returned %d nodes", len(result))
	}
}

func TestLookup(t *testing.T) {
	self := makeNodeID(0)
	tab := NewTable(self)

	// Seed the table.
	for i := byte(1); i <= 10; i++ {
		tab.AddNode(makeNode(i))
	}

	target := makeNodeID(20)

	// queryFn simulates a remote FindNode call.
	queryFn := func(n *enode.Node) []*enode.Node {
		// Return some nodes closer to the target.
		var result []*enode.Node
		for i := byte(15); i <= 25; i++ {
			result = append(result, makeNode(i))
		}
		return result
	}

	result := tab.Lookup(target, queryFn)
	if len(result) == 0 {
		t.Fatal("Lookup returned no nodes")
	}

	// Verify sorted by distance.
	for i := 1; i < len(result); i++ {
		if enode.DistCmp(target, result[i-1].ID, result[i].ID) > 0 {
			t.Fatal("Lookup results not sorted")
		}
	}
}

func TestBucketEntries(t *testing.T) {
	self := makeNodeID(0)
	tab := NewTable(self)

	n := makeNode(1) // distance 1 -> bucket 0
	tab.AddNode(n)

	entries := tab.BucketEntries(0)
	if len(entries) != 1 {
		t.Fatalf("BucketEntries(0) = %d entries, want 1", len(entries))
	}
	if entries[0].ID != n.ID {
		t.Fatal("wrong node in bucket")
	}

	// Out of range bucket.
	if entries := tab.BucketEntries(-1); entries != nil {
		t.Fatal("expected nil for invalid bucket index")
	}
	if entries := tab.BucketEntries(NumBuckets); entries != nil {
		t.Fatal("expected nil for invalid bucket index")
	}
}

func TestNodes(t *testing.T) {
	self := makeNodeID(0)
	tab := NewTable(self)

	for i := byte(1); i <= 5; i++ {
		tab.AddNode(makeNode(i))
	}

	all := tab.Nodes()
	if len(all) != 5 {
		t.Fatalf("Nodes() = %d, want 5", len(all))
	}
}
