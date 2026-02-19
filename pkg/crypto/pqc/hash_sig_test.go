package pqc

import (
	"sync"
	"testing"

	"github.com/eth2028/eth2028/core/types"
	"github.com/eth2028/eth2028/crypto"
)

func TestNewHashSigScheme(t *testing.T) {
	s := NewHashSigScheme(10, 16)
	if s == nil {
		t.Fatal("NewHashSigScheme returned nil")
	}
	if s.height != 10 {
		t.Errorf("height = %d, want 10", s.height)
	}
	if s.winternitzParam != 16 {
		t.Errorf("winternitzParam = %d, want 16", s.winternitzParam)
	}
	if s.chainLen != hashSigChainLen {
		t.Errorf("chainLen = %d, want %d", s.chainLen, hashSigChainLen)
	}
}

func TestNewHashSigSchemeDefaults(t *testing.T) {
	// Invalid height clamped.
	s := NewHashSigScheme(0, 99)
	if s.height != 10 {
		t.Errorf("expected default height=10, got %d", s.height)
	}
	if s.winternitzParam != 16 {
		t.Errorf("expected default w=16, got %d", s.winternitzParam)
	}

	// Height too large clamped to 20.
	s2 := NewHashSigScheme(25, 4)
	if s2.height != 20 {
		t.Errorf("expected clamped height=20, got %d", s2.height)
	}
	if s2.winternitzParam != 4 {
		t.Errorf("w = %d, want 4", s2.winternitzParam)
	}
	if s2.chainLen != 133 {
		t.Errorf("chainLen for w=4 = %d, want 133", s2.chainLen)
	}
}

func TestGenerateKeyPair(t *testing.T) {
	s := NewHashSigScheme(4, 16) // small tree for speed
	kp, err := s.GenerateKeyPair()
	if err != nil {
		t.Fatalf("GenerateKeyPair: %v", err)
	}
	if len(kp.PublicKey) != 32 {
		t.Errorf("PublicKey len = %d, want 32", len(kp.PublicKey))
	}
	if len(kp.PrivateKey) != hashSigSeedSize+4 {
		t.Errorf("PrivateKey len = %d, want %d", len(kp.PrivateKey), hashSigSeedSize+4)
	}
	if kp.Height != 4 {
		t.Errorf("Height = %d, want 4", kp.Height)
	}
	if kp.RemainingSignatures != 16 {
		t.Errorf("RemainingSignatures = %d, want 16", kp.RemainingSignatures)
	}
}

func TestSignAndVerify(t *testing.T) {
	s := NewHashSigScheme(4, 16)
	kp, err := s.GenerateKeyPair()
	if err != nil {
		t.Fatalf("GenerateKeyPair: %v", err)
	}

	msg := []byte("hello, post-quantum world")
	sig, err := s.Sign(kp.PrivateKey, msg)
	if err != nil {
		t.Fatalf("Sign: %v", err)
	}

	if sig.LeafIndex != 0 {
		t.Errorf("LeafIndex = %d, want 0", sig.LeafIndex)
	}
	if len(sig.AuthPath) != 4 {
		t.Errorf("AuthPath len = %d, want 4", len(sig.AuthPath))
	}
	if len(sig.OTSSignature) != hashSigChainLen {
		t.Errorf("OTSSignature len = %d, want %d", len(sig.OTSSignature), hashSigChainLen)
	}

	if !s.Verify(kp.PublicKey, msg, sig) {
		t.Error("Verify returned false for valid signature")
	}
}

func TestSignAndVerifyW4(t *testing.T) {
	s := NewHashSigScheme(3, 4)
	kp, err := s.GenerateKeyPair()
	if err != nil {
		t.Fatalf("GenerateKeyPair: %v", err)
	}

	msg := []byte("winternitz w=4 test")
	sig, err := s.Sign(kp.PrivateKey, msg)
	if err != nil {
		t.Fatalf("Sign: %v", err)
	}

	if len(sig.OTSSignature) != 133 {
		t.Errorf("OTSSignature len for w=4 = %d, want 133", len(sig.OTSSignature))
	}

	if !s.Verify(kp.PublicKey, msg, sig) {
		t.Error("Verify returned false for valid w=4 signature")
	}
}

func TestVerifyTamperedMessage(t *testing.T) {
	s := NewHashSigScheme(4, 16)
	kp, _ := s.GenerateKeyPair()

	msg := []byte("original message")
	sig, _ := s.Sign(kp.PrivateKey, msg)

	tampered := []byte("tampered message")
	if s.Verify(kp.PublicKey, tampered, sig) {
		t.Error("Verify should reject signature for wrong message")
	}
}

func TestVerifyTamperedSignature(t *testing.T) {
	s := NewHashSigScheme(4, 16)
	kp, _ := s.GenerateKeyPair()

	msg := []byte("test message")
	sig, _ := s.Sign(kp.PrivateKey, msg)

	// Tamper with OTS signature.
	sig.OTSSignature[0][0] ^= 0xff
	if s.Verify(kp.PublicKey, msg, sig) {
		t.Error("Verify should reject tampered OTS signature")
	}
}

func TestVerifyWrongPublicKey(t *testing.T) {
	s := NewHashSigScheme(4, 16)
	kp, _ := s.GenerateKeyPair()

	msg := []byte("test message")
	sig, _ := s.Sign(kp.PrivateKey, msg)

	wrongKey := make([]byte, 32)
	wrongKey[0] = 0xff
	if s.Verify(wrongKey, msg, sig) {
		t.Error("Verify should reject signature with wrong public key")
	}
}

func TestVerifyNilInputs(t *testing.T) {
	s := NewHashSigScheme(4, 16)
	if s.Verify(nil, []byte("msg"), &HashSignature{}) {
		t.Error("Verify(nil pubkey) should return false")
	}
	if s.Verify([]byte{1}, nil, &HashSignature{}) {
		t.Error("Verify(nil msg) should return false")
	}
	if s.Verify([]byte{1}, []byte("msg"), nil) {
		t.Error("Verify(nil sig) should return false")
	}
}

func TestVerifyBadAuthPathLength(t *testing.T) {
	s := NewHashSigScheme(4, 16)
	kp, _ := s.GenerateKeyPair()
	msg := []byte("test")
	sig, _ := s.Sign(kp.PrivateKey, msg)

	// Truncate auth path.
	sig.AuthPath = sig.AuthPath[:2]
	if s.Verify(kp.PublicKey, msg, sig) {
		t.Error("Verify should reject signature with wrong auth path length")
	}
}

func TestVerifyBadLeafIndex(t *testing.T) {
	s := NewHashSigScheme(4, 16)
	kp, _ := s.GenerateKeyPair()
	msg := []byte("test")
	sig, _ := s.Sign(kp.PrivateKey, msg)

	sig.LeafIndex = 999 // way out of range
	if s.Verify(kp.PublicKey, msg, sig) {
		t.Error("Verify should reject signature with out-of-range leaf index")
	}
}

func TestMultipleSignatures(t *testing.T) {
	s := NewHashSigScheme(3, 16) // 8 max signatures
	kp, _ := s.GenerateKeyPair()

	messages := []string{
		"message 0", "message 1", "message 2", "message 3",
		"message 4", "message 5", "message 6", "message 7",
	}

	for i, msg := range messages {
		sig, err := s.Sign(kp.PrivateKey, []byte(msg))
		if err != nil {
			t.Fatalf("Sign(%d): %v", i, err)
		}
		if sig.LeafIndex != uint32(i) {
			t.Errorf("sig %d LeafIndex = %d, want %d", i, sig.LeafIndex, i)
		}
		if !s.Verify(kp.PublicKey, []byte(msg), sig) {
			t.Errorf("Verify(%d) failed", i)
		}
	}
}

func TestSignExhausted(t *testing.T) {
	s := NewHashSigScheme(2, 16) // only 4 signatures
	kp, _ := s.GenerateKeyPair()

	for i := 0; i < 4; i++ {
		_, err := s.Sign(kp.PrivateKey, []byte("msg"))
		if err != nil {
			t.Fatalf("Sign(%d): %v", i, err)
		}
	}

	// 5th should fail.
	_, err := s.Sign(kp.PrivateKey, []byte("msg"))
	if err != ErrHashSigNoKeysLeft {
		t.Errorf("expected ErrHashSigNoKeysLeft, got %v", err)
	}
}

func TestSignNilPrivateKey(t *testing.T) {
	s := NewHashSigScheme(4, 16)
	_, err := s.Sign(nil, []byte("msg"))
	if err != ErrHashSigNilPrivateKey {
		t.Errorf("expected ErrHashSigNilPrivateKey, got %v", err)
	}
}

func TestSignNilMessage(t *testing.T) {
	s := NewHashSigScheme(4, 16)
	kp, _ := s.GenerateKeyPair()
	_, err := s.Sign(kp.PrivateKey, nil)
	if err != ErrHashSigNilMessage {
		t.Errorf("expected ErrHashSigNilMessage, got %v", err)
	}

	_, err = s.Sign(kp.PrivateKey, []byte{})
	if err != ErrHashSigNilMessage {
		t.Errorf("expected ErrHashSigNilMessage for empty msg, got %v", err)
	}
}

func TestRemainingSignatures(t *testing.T) {
	s := NewHashSigScheme(3, 16) // 8 max
	kp, _ := s.GenerateKeyPair()

	if got := s.RemainingSignatures(kp.PrivateKey); got != 8 {
		t.Errorf("RemainingSignatures = %d, want 8", got)
	}

	s.Sign(kp.PrivateKey, []byte("msg1"))
	if got := s.RemainingSignatures(kp.PrivateKey); got != 7 {
		t.Errorf("after 1 sign, RemainingSignatures = %d, want 7", got)
	}

	s.Sign(kp.PrivateKey, []byte("msg2"))
	if got := s.RemainingSignatures(kp.PrivateKey); got != 6 {
		t.Errorf("after 2 signs, RemainingSignatures = %d, want 6", got)
	}
}

func TestRemainingSignaturesNilKey(t *testing.T) {
	s := NewHashSigScheme(4, 16)
	if got := s.RemainingSignatures(nil); got != 0 {
		t.Errorf("RemainingSignatures(nil) = %d, want 0", got)
	}
	if got := s.RemainingSignatures([]byte{1, 2}); got != 0 {
		t.Errorf("RemainingSignatures(short) = %d, want 0", got)
	}
}

func TestOTSSignAndVerify(t *testing.T) {
	s := NewHashSigScheme(4, 16)

	key := crypto.Keccak256([]byte("ots-test-key"))
	msg := []byte("ots-test-message")

	otsSig := s.OTSSign(key, msg)
	if len(otsSig) != hashSigChainLen {
		t.Fatalf("OTSSign len = %d, want %d", len(otsSig), hashSigChainLen)
	}

	// Compute OTS public key from the key.
	chains := s.deriveOTSChains(key)
	otsPub := s.otsPublicFromPrivate(chains)
	pubHash := crypto.Keccak256(otsPub...)

	if !s.OTSVerify(pubHash, msg, otsSig) {
		t.Error("OTSVerify returned false for valid OTS signature")
	}
}

func TestOTSSignNilInputs(t *testing.T) {
	s := NewHashSigScheme(4, 16)
	if sig := s.OTSSign(nil, []byte("msg")); sig != nil {
		t.Error("OTSSign(nil key) should return nil")
	}
	if sig := s.OTSSign([]byte{1}, nil); sig != nil {
		t.Error("OTSSign(nil msg) should return nil")
	}
}

func TestOTSVerifyWrongMessage(t *testing.T) {
	s := NewHashSigScheme(4, 16)

	key := crypto.Keccak256([]byte("key"))
	msg := []byte("correct")
	otsSig := s.OTSSign(key, msg)

	chains := s.deriveOTSChains(key)
	otsPub := s.otsPublicFromPrivate(chains)
	pubHash := crypto.Keccak256(otsPub...)

	if s.OTSVerify(pubHash, []byte("wrong"), otsSig) {
		t.Error("OTSVerify should reject wrong message")
	}
}

func TestBuildMerkleTree(t *testing.T) {
	leaves := make([]types.Hash, 8)
	for i := range leaves {
		leaves[i] = crypto.Keccak256Hash([]byte{byte(i)})
	}

	tree := BuildMerkleTree(leaves)
	if tree.Height != 3 {
		t.Errorf("Height = %d, want 3", tree.Height)
	}
	if tree.Leaves != 8 {
		t.Errorf("Leaves = %d, want 8", tree.Leaves)
	}

	// Root should be non-zero.
	if tree.Nodes[1].IsZero() {
		t.Error("root should not be zero")
	}

	// Verify internal node: node[4] = H(node[8] || node[9]).
	expected := crypto.Keccak256Hash(tree.Nodes[8][:], tree.Nodes[9][:])
	if tree.Nodes[4] != expected {
		t.Error("internal node mismatch")
	}
}

func TestBuildMerkleTreeEmpty(t *testing.T) {
	tree := BuildMerkleTree(nil)
	if tree.Height != 0 {
		t.Errorf("empty tree Height = %d, want 0", tree.Height)
	}
	if tree.Leaves != 0 {
		t.Errorf("empty tree Leaves = %d, want 0", tree.Leaves)
	}
}

func TestBuildMerkleTreeSingleLeaf(t *testing.T) {
	leaves := []types.Hash{crypto.Keccak256Hash([]byte("only-leaf"))}
	tree := BuildMerkleTree(leaves)
	if tree.Height != 0 {
		t.Errorf("single leaf Height = %d, want 0", tree.Height)
	}
	if tree.Leaves != 1 {
		t.Errorf("single leaf Leaves = %d, want 1", tree.Leaves)
	}
	// With a single leaf, the root is the leaf itself (no internal nodes).
	if tree.Nodes[1] != leaves[0] {
		t.Errorf("single leaf root = %s, want %s", tree.Nodes[1].Hex(), leaves[0].Hex())
	}
}

func TestSignVerifyDifferentMessages(t *testing.T) {
	s := NewHashSigScheme(3, 16)
	kp, _ := s.GenerateKeyPair()

	msg1 := []byte("first message")
	msg2 := []byte("second message")

	sig1, _ := s.Sign(kp.PrivateKey, msg1)
	sig2, _ := s.Sign(kp.PrivateKey, msg2)

	// Each verifies with its own message.
	if !s.Verify(kp.PublicKey, msg1, sig1) {
		t.Error("sig1 should verify with msg1")
	}
	if !s.Verify(kp.PublicKey, msg2, sig2) {
		t.Error("sig2 should verify with msg2")
	}

	// Cross-verify should fail.
	if s.Verify(kp.PublicKey, msg2, sig1) {
		t.Error("sig1 should not verify with msg2")
	}
	if s.Verify(kp.PublicKey, msg1, sig2) {
		t.Error("sig2 should not verify with msg1")
	}
}

func TestDeterministicKeyGeneration(t *testing.T) {
	// Two schemes with same parameters should produce different keys
	// (since seed is random).
	s := NewHashSigScheme(3, 16)
	kp1, _ := s.GenerateKeyPair()
	kp2, _ := s.GenerateKeyPair()

	if types.BytesToHash(kp1.PublicKey) == types.BytesToHash(kp2.PublicKey) {
		t.Error("two key pairs should have different public keys")
	}
}

func TestHashSigConcurrentSignAndVerify(t *testing.T) {
	s := NewHashSigScheme(4, 16) // 16 max sigs
	kp, _ := s.GenerateKeyPair()
	pubKey := make([]byte, len(kp.PublicKey))
	copy(pubKey, kp.PublicKey)

	// Sign sequentially (since private key state is shared).
	type sigMsg struct {
		sig *HashSignature
		msg []byte
	}
	var sigs []sigMsg
	for i := 0; i < 16; i++ {
		msg := []byte{byte(i), 0xaa, 0xbb}
		sig, err := s.Sign(kp.PrivateKey, msg)
		if err != nil {
			t.Fatalf("Sign(%d): %v", i, err)
		}
		sigs = append(sigs, sigMsg{sig, msg})
	}

	// Verify concurrently.
	var wg sync.WaitGroup
	errs := make(chan int, 16)
	for i, sm := range sigs {
		wg.Add(1)
		go func(idx int, sig *HashSignature, msg []byte) {
			defer wg.Done()
			if !s.Verify(pubKey, msg, sig) {
				errs <- idx
			}
		}(i, sm.sig, sm.msg)
	}
	wg.Wait()
	close(errs)

	for idx := range errs {
		t.Errorf("concurrent Verify(%d) failed", idx)
	}
}

func TestMessageDigitsW16(t *testing.T) {
	s := NewHashSigScheme(4, 16)
	msgHash := make([]byte, 32)
	msgHash[0] = 0xab // nibbles: 0xa, 0xb

	digits := s.messageDigits(msgHash)
	if len(digits) != hashSigChainLen {
		t.Fatalf("digits len = %d, want %d", len(digits), hashSigChainLen)
	}
	if digits[0] != 0xa {
		t.Errorf("digit[0] = %d, want 10", digits[0])
	}
	if digits[1] != 0xb {
		t.Errorf("digit[1] = %d, want 11", digits[1])
	}
}

func TestMessageDigitsW4(t *testing.T) {
	s := NewHashSigScheme(4, 4)
	msgHash := make([]byte, 32)
	msgHash[0] = 0b11_10_01_00 // crumbs: 3, 2, 1, 0

	digits := s.messageDigits(msgHash)
	if len(digits) != 133 {
		t.Fatalf("digits len = %d, want 133", len(digits))
	}
	if digits[0] != 3 {
		t.Errorf("digit[0] = %d, want 3", digits[0])
	}
	if digits[1] != 2 {
		t.Errorf("digit[1] = %d, want 2", digits[1])
	}
	if digits[2] != 1 {
		t.Errorf("digit[2] = %d, want 1", digits[2])
	}
	if digits[3] != 0 {
		t.Errorf("digit[3] = %d, want 0", digits[3])
	}
}

func TestAuthPathConsistency(t *testing.T) {
	s := NewHashSigScheme(3, 16)
	seed := crypto.Keccak256([]byte("test-seed"))
	tree := s.buildTree(seed)

	// For each leaf, verify that the auth path reconstructs the root.
	numLeaves := 1 << s.height
	for i := 0; i < numLeaves; i++ {
		path := s.authPath(tree, uint32(i))
		leaf := tree.Nodes[numLeaves+i]

		computed := leaf
		idx := uint32(i)
		for _, sibling := range path {
			if idx&1 == 0 {
				computed = crypto.Keccak256Hash(computed[:], sibling[:])
			} else {
				computed = crypto.Keccak256Hash(sibling[:], computed[:])
			}
			idx >>= 1
		}
		if computed != tree.Nodes[1] {
			t.Errorf("auth path for leaf %d does not reconstruct root", i)
		}
	}
}
