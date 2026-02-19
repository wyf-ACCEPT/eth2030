package light

import (
	"testing"
)

func TestSignerCount(t *testing.T) {
	tests := []struct {
		bits  []byte
		count int
	}{
		{nil, 0},
		{[]byte{0x00}, 0},
		{[]byte{0x01}, 1},
		{[]byte{0x03}, 2},
		{[]byte{0xff}, 8},
		{[]byte{0xff, 0xff}, 16},
		{[]byte{0x55}, 4}, // 0101_0101
	}

	for i, tt := range tests {
		u := &LightClientUpdate{SyncCommitteeBits: tt.bits}
		if got := u.SignerCount(); got != tt.count {
			t.Errorf("test %d: SignerCount = %d, want %d", i, got, tt.count)
		}
	}
}

func TestSupermajoritySigned(t *testing.T) {
	// 10 out of 12 signers = 83% >= 67%.
	bits := []byte{0xff, 0x03} // 10 bits set
	u := &LightClientUpdate{SyncCommitteeBits: bits}
	if !u.SupermajoritySigned(12) {
		t.Error("10/12 should be supermajority")
	}

	// 4 out of 12 signers = 33% < 67%.
	bits2 := []byte{0x0f} // 4 bits set
	u2 := &LightClientUpdate{SyncCommitteeBits: bits2}
	if u2.SupermajoritySigned(12) {
		t.Error("4/12 should not be supermajority")
	}

	// Zero committee size.
	u3 := &LightClientUpdate{SyncCommitteeBits: []byte{0xff}}
	if u3.SupermajoritySigned(0) {
		t.Error("zero committee size should not be supermajority")
	}
}

func TestLightBlockFields(t *testing.T) {
	lb := &LightBlock{
		StateProof: []byte{0x01},
		TxProofs:   [][]byte{{0x02}},
	}
	if len(lb.StateProof) != 1 {
		t.Error("StateProof wrong length")
	}
	if len(lb.TxProofs) != 1 {
		t.Error("TxProofs wrong length")
	}
}

func TestSyncCommitteeFields(t *testing.T) {
	sc := &SyncCommittee{
		Pubkeys:         [][]byte{{0x01}, {0x02}},
		AggregatePubkey: []byte{0x03},
		Period:          42,
	}
	if len(sc.Pubkeys) != 2 {
		t.Errorf("Pubkeys count = %d, want 2", len(sc.Pubkeys))
	}
	if sc.Period != 42 {
		t.Errorf("Period = %d, want 42", sc.Period)
	}
}
