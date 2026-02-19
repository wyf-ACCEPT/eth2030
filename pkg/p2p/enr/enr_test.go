package enr

import (
	"testing"

	"github.com/eth2028/eth2028/crypto"
)

func TestSetGet(t *testing.T) {
	r := &Record{}
	r.Set("foo", []byte("bar"))
	r.Set("baz", []byte("qux"))

	got := r.Get("foo")
	if string(got) != "bar" {
		t.Fatalf("Get(foo) = %q, want bar", got)
	}
	got = r.Get("baz")
	if string(got) != "qux" {
		t.Fatalf("Get(baz) = %q, want qux", got)
	}
	got = r.Get("missing")
	if got != nil {
		t.Fatalf("Get(missing) = %v, want nil", got)
	}
}

func TestSetOverwrite(t *testing.T) {
	r := &Record{}
	r.Set("key", []byte("v1"))
	r.Set("key", []byte("v2"))

	if got := r.Get("key"); string(got) != "v2" {
		t.Fatalf("Get(key) = %q, want v2", got)
	}
	if len(r.Pairs) != 1 {
		t.Fatalf("len(Pairs) = %d, want 1", len(r.Pairs))
	}
}

func TestPairsSorted(t *testing.T) {
	r := &Record{}
	r.Set("z", []byte("1"))
	r.Set("a", []byte("2"))
	r.Set("m", []byte("3"))

	for i := 1; i < len(r.Pairs); i++ {
		if r.Pairs[i-1].Key >= r.Pairs[i].Key {
			t.Fatalf("pairs not sorted: %q >= %q", r.Pairs[i-1].Key, r.Pairs[i].Key)
		}
	}
}

func TestSetSeqInvalidatesSignature(t *testing.T) {
	r := &Record{Signature: []byte{1, 2, 3}}
	r.SetSeq(5)
	if r.Signature != nil {
		t.Fatal("SetSeq should invalidate signature")
	}
	if r.Seq != 5 {
		t.Fatalf("Seq = %d, want 5", r.Seq)
	}
}

func TestSignAndVerifyENR(t *testing.T) {
	key, err := crypto.GenerateKey()
	if err != nil {
		t.Fatal(err)
	}

	r := &Record{Seq: 1}
	r.Set(KeyIP, []byte{127, 0, 0, 1})
	r.Set(KeyUDP, []byte{0x76, 0x5f}) // 30303
	r.Set(KeyTCP, []byte{0x76, 0x5f})

	if err := SignENR(r, key); err != nil {
		t.Fatal(err)
	}

	if r.Signature == nil {
		t.Fatal("signature should not be nil after signing")
	}
	if len(r.Signature) != 64 {
		t.Fatalf("signature length = %d, want 64", len(r.Signature))
	}

	// Verify the identity scheme was set.
	if got := r.Get(KeyID); string(got) != "v4" {
		t.Fatalf("id = %q, want v4", got)
	}

	// Verify signature.
	if err := VerifyENR(r); err != nil {
		t.Fatalf("VerifyENR failed: %v", err)
	}
}

func TestVerifyTamperedENR(t *testing.T) {
	key, err := crypto.GenerateKey()
	if err != nil {
		t.Fatal(err)
	}

	r := &Record{Seq: 1}
	r.Set(KeyIP, []byte{10, 0, 0, 1})
	if err := SignENR(r, key); err != nil {
		t.Fatal(err)
	}

	// Tamper with a value without re-signing.
	r.Pairs[0].Value = []byte("tampered")

	if err := VerifyENR(r); err == nil {
		t.Fatal("expected verification to fail on tampered record")
	}
}

func TestEncodeDecodeENR(t *testing.T) {
	key, err := crypto.GenerateKey()
	if err != nil {
		t.Fatal(err)
	}

	r := &Record{Seq: 42}
	r.Set(KeyIP, []byte{192, 168, 1, 1})
	r.Set(KeyUDP, []byte{0x76, 0x5f})
	if err := SignENR(r, key); err != nil {
		t.Fatal(err)
	}

	data, err := EncodeENR(r)
	if err != nil {
		t.Fatalf("EncodeENR: %v", err)
	}

	if len(data) > SizeLimit {
		t.Fatalf("encoded size %d exceeds limit %d", len(data), SizeLimit)
	}

	decoded, err := DecodeENR(data)
	if err != nil {
		t.Fatalf("DecodeENR: %v", err)
	}

	if decoded.Seq != r.Seq {
		t.Fatalf("Seq = %d, want %d", decoded.Seq, r.Seq)
	}
	if len(decoded.Signature) != len(r.Signature) {
		t.Fatalf("Signature length = %d, want %d", len(decoded.Signature), len(r.Signature))
	}
	if len(decoded.Pairs) != len(r.Pairs) {
		t.Fatalf("Pairs count = %d, want %d", len(decoded.Pairs), len(r.Pairs))
	}
	for i, p := range decoded.Pairs {
		if p.Key != r.Pairs[i].Key {
			t.Fatalf("Pair[%d].Key = %q, want %q", i, p.Key, r.Pairs[i].Key)
		}
	}
}

func TestNodeID(t *testing.T) {
	key, err := crypto.GenerateKey()
	if err != nil {
		t.Fatal(err)
	}

	r := &Record{}
	if err := SignENR(r, key); err != nil {
		t.Fatal(err)
	}

	id := r.NodeID()
	if id == ([32]byte{}) {
		t.Fatal("NodeID should not be zero after signing")
	}

	// NodeID should be keccak256(compressed pubkey).
	compressed := crypto.CompressPubkey(&key.PublicKey)
	expected := crypto.Keccak256(compressed)
	for i := range id {
		if id[i] != expected[i] {
			t.Fatalf("NodeID mismatch at byte %d", i)
		}
	}
}

func TestDecodeUnsigned(t *testing.T) {
	r := &Record{Seq: 1}
	r.Set("foo", []byte("bar"))

	_, err := EncodeENR(r)
	if err != ErrNotSigned {
		t.Fatalf("EncodeENR on unsigned record: got %v, want ErrNotSigned", err)
	}
}

func TestVerifyNoKey(t *testing.T) {
	r := &Record{Signature: make([]byte, 64)}
	err := VerifyENR(r)
	if err == nil {
		t.Fatal("expected error verifying record with no secp256k1 key")
	}
}
