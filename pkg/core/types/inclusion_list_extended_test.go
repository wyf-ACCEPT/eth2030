package types

import (
	"testing"
)

func makeTestInclusionList() *InclusionList {
	return &InclusionList{
		Slot:           100,
		ValidatorIndex: 42,
		CommitteeRoot:  HexToHash("0xaabb"),
		Transactions: [][]byte{
			{0x01, 0x02, 0x03},
			{0x04, 0x05, 0x06},
		},
		Summary: []InclusionListEntry{
			{Address: HexToAddress("0x1111"), GasLimit: 100000},
			{Address: HexToAddress("0x2222"), GasLimit: 200000},
		},
	}
}

func TestValidateInclusionList_Valid(t *testing.T) {
	il := makeTestInclusionList()
	if err := ValidateInclusionList(il); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestValidateInclusionList_Nil(t *testing.T) {
	if err := ValidateInclusionList(nil); err != ErrILNil {
		t.Errorf("nil list: want ErrILNil, got %v", err)
	}
}

func TestValidateInclusionList_EmptyTxs(t *testing.T) {
	il := makeTestInclusionList()
	il.Transactions = nil
	if err := ValidateInclusionList(il); err != ErrILEmptyTransactions {
		t.Errorf("empty txs: want ErrILEmptyTransactions, got %v", err)
	}
}

func TestValidateInclusionList_TooManyTxs(t *testing.T) {
	il := makeTestInclusionList()
	il.Transactions = make([][]byte, MaxTransactionsPerInclusionList+1)
	il.Summary = make([]InclusionListEntry, MaxTransactionsPerInclusionList+1)
	for i := range il.Transactions {
		il.Transactions[i] = []byte{0x01}
		addr := Address{}
		addr[19] = byte(i)
		il.Summary[i] = InclusionListEntry{Address: addr, GasLimit: 1000}
	}
	err := ValidateInclusionList(il)
	if err == nil {
		t.Fatal("expected error for too many transactions")
	}
}

func TestValidateInclusionList_SummaryMismatch(t *testing.T) {
	il := makeTestInclusionList()
	il.Summary = il.Summary[:1] // mismatch with 2 txs
	err := ValidateInclusionList(il)
	if err == nil {
		t.Fatal("expected summary mismatch error")
	}
}

func TestValidateInclusionList_GasExceedsMax(t *testing.T) {
	il := makeTestInclusionList()
	il.Summary[0].GasLimit = MaxGasPerInclusionList // exceeds max alone
	err := ValidateInclusionList(il)
	if err == nil {
		t.Fatal("expected gas exceeds error")
	}
}

func TestValidateInclusionList_DuplicateSender(t *testing.T) {
	il := makeTestInclusionList()
	il.Summary[1].Address = il.Summary[0].Address // duplicate sender
	err := ValidateInclusionList(il)
	if err == nil {
		t.Fatal("expected duplicate sender error")
	}
}

func TestValidateSignedInclusionList(t *testing.T) {
	il := makeTestInclusionList()
	sig := [96]byte{}
	sig[0] = 0x01 // non-zero signature

	sil := &SignedInclusionList{
		Message:   il,
		Signature: sig,
	}
	if err := ValidateSignedInclusionList(sil); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Zero signature.
	sil.Signature = [96]byte{}
	if err := ValidateSignedInclusionList(sil); err != ErrILInvalidSignature {
		t.Errorf("zero sig: want ErrILInvalidSignature, got %v", err)
	}

	// Nil message.
	sil.Message = nil
	if err := ValidateSignedInclusionList(sil); err != ErrILNil {
		t.Errorf("nil msg: want ErrILNil, got %v", err)
	}

	// Nil signed IL.
	if err := ValidateSignedInclusionList(nil); err != ErrILNil {
		t.Errorf("nil sil: want ErrILNil, got %v", err)
	}
}

func TestIsILExpired(t *testing.T) {
	il := makeTestInclusionList()

	// Not expired: current slot <= il.Slot + ILExpirySlots
	if IsILExpired(il, 100) {
		t.Error("slot 100 should not be expired")
	}
	if IsILExpired(il, 101) {
		t.Error("slot 101 should not be expired")
	}
	if IsILExpired(il, 102) {
		t.Error("slot 102 should not be expired")
	}

	// Expired: current slot > il.Slot + ILExpirySlots
	if !IsILExpired(il, 103) {
		t.Error("slot 103 should be expired")
	}

	// Nil.
	if !IsILExpired(nil, 0) {
		t.Error("nil should be expired")
	}
}

func TestCheckBlockCompliance_FullyCompliant(t *testing.T) {
	il := makeTestInclusionList()
	senders := []Address{
		HexToAddress("0x1111"),
		HexToAddress("0x2222"),
		HexToAddress("0x3333"), // extra tx is fine
	}
	gas := []uint64{100000, 200000, 50000}

	result := CheckBlockCompliance(il, senders, gas)
	if !result.Compliant {
		t.Error("block should be compliant")
	}
	if result.TotalSatisfied != 2 {
		t.Errorf("TotalSatisfied = %d, want 2", result.TotalSatisfied)
	}
	if len(result.MissingSenders) != 0 {
		t.Error("should have no missing senders")
	}
}

func TestCheckBlockCompliance_MissingSender(t *testing.T) {
	il := makeTestInclusionList()
	senders := []Address{
		HexToAddress("0x1111"),
		// 0x2222 is missing
	}
	gas := []uint64{100000}

	result := CheckBlockCompliance(il, senders, gas)
	if result.Compliant {
		t.Error("block should not be compliant")
	}
	if result.TotalSatisfied != 1 {
		t.Errorf("TotalSatisfied = %d, want 1", result.TotalSatisfied)
	}
	if len(result.MissingSenders) != 1 {
		t.Fatalf("MissingSenders count = %d, want 1", len(result.MissingSenders))
	}
	if result.MissingSenders[0] != HexToAddress("0x2222") {
		t.Errorf("missing sender = %s, want 0x2222", result.MissingSenders[0].Hex())
	}
}

func TestCheckBlockCompliance_InsufficientGas(t *testing.T) {
	il := makeTestInclusionList()
	senders := []Address{
		HexToAddress("0x1111"),
		HexToAddress("0x2222"),
	}
	gas := []uint64{100000, 100000} // 0x2222 has 100000 but needs 200000

	result := CheckBlockCompliance(il, senders, gas)
	if result.Compliant {
		t.Error("block should not be compliant due to insufficient gas")
	}
	if shortfall, ok := result.MissingGas[HexToAddress("0x2222")]; !ok || shortfall != 100000 {
		t.Errorf("expected 100000 gas shortfall for 0x2222, got %d", shortfall)
	}
}

func TestEncodeDecodeInclusionList(t *testing.T) {
	il := makeTestInclusionList()

	encoded, err := EncodeInclusionList(il)
	if err != nil {
		t.Fatalf("encode error: %v", err)
	}
	if len(encoded) == 0 {
		t.Fatal("encoded data should not be empty")
	}

	decoded, err := DecodeInclusionList(encoded)
	if err != nil {
		t.Fatalf("decode error: %v", err)
	}

	if decoded.Slot != il.Slot {
		t.Errorf("Slot = %d, want %d", decoded.Slot, il.Slot)
	}
	if decoded.ValidatorIndex != il.ValidatorIndex {
		t.Errorf("ValidatorIndex = %d, want %d", decoded.ValidatorIndex, il.ValidatorIndex)
	}
	if decoded.CommitteeRoot != il.CommitteeRoot {
		t.Error("CommitteeRoot mismatch")
	}
	if len(decoded.Transactions) != len(il.Transactions) {
		t.Fatalf("Transactions count = %d, want %d", len(decoded.Transactions), len(il.Transactions))
	}
	if len(decoded.Summary) != len(il.Summary) {
		t.Fatalf("Summary count = %d, want %d", len(decoded.Summary), len(il.Summary))
	}
	for i, entry := range decoded.Summary {
		if entry.Address != il.Summary[i].Address {
			t.Errorf("Summary[%d].Address mismatch", i)
		}
		if entry.GasLimit != il.Summary[i].GasLimit {
			t.Errorf("Summary[%d].GasLimit = %d, want %d", i, entry.GasLimit, il.Summary[i].GasLimit)
		}
	}
}

func TestEncodeInclusionList_Nil(t *testing.T) {
	_, err := EncodeInclusionList(nil)
	if err != ErrILNil {
		t.Errorf("nil encode: want ErrILNil, got %v", err)
	}
}

func TestInclusionListHash_Deterministic(t *testing.T) {
	il := makeTestInclusionList()
	h1 := InclusionListHash(il)
	h2 := InclusionListHash(il)
	if h1 != h2 {
		t.Error("InclusionListHash should be deterministic")
	}
	if h1.IsZero() {
		t.Error("hash should not be zero")
	}
}

func TestInclusionListHash_DifferentContent(t *testing.T) {
	il1 := makeTestInclusionList()
	il2 := makeTestInclusionList()
	il2.Slot = 999

	h1 := InclusionListHash(il1)
	h2 := InclusionListHash(il2)
	if h1 == h2 {
		t.Error("different inclusion lists should have different hashes")
	}
}

func TestMergeInclusionLists_Empty(t *testing.T) {
	agg := MergeInclusionLists(nil)
	if len(agg.MergedSummary) != 0 {
		t.Error("empty merge should have no entries")
	}
}

func TestMergeInclusionLists_TwoLists(t *testing.T) {
	sig := [96]byte{0x01}
	il1 := &SignedInclusionList{
		Message: &InclusionList{
			Slot: 100,
			Summary: []InclusionListEntry{
				{Address: HexToAddress("0x1111"), GasLimit: 100000},
				{Address: HexToAddress("0x2222"), GasLimit: 200000},
			},
		},
		Signature: sig,
	}
	il2 := &SignedInclusionList{
		Message: &InclusionList{
			Slot: 100,
			Summary: []InclusionListEntry{
				{Address: HexToAddress("0x2222"), GasLimit: 300000}, // higher gas for same sender
				{Address: HexToAddress("0x3333"), GasLimit: 150000},
			},
		},
		Signature: sig,
	}

	agg := MergeInclusionLists([]*SignedInclusionList{il1, il2})
	if agg.Slot != 100 {
		t.Errorf("Slot = %d, want 100", agg.Slot)
	}
	if len(agg.MergedSummary) != 3 {
		t.Fatalf("merged entries = %d, want 3", len(agg.MergedSummary))
	}

	// Find 0x2222 and verify it took the max gas.
	for _, entry := range agg.MergedSummary {
		if entry.Address == HexToAddress("0x2222") {
			if entry.GasLimit != 300000 {
				t.Errorf("0x2222 gas = %d, want 300000", entry.GasLimit)
			}
			return
		}
	}
	t.Error("0x2222 not found in merged summary")
}

func TestSummaryTotalGas(t *testing.T) {
	entries := []InclusionListEntry{
		{GasLimit: 100000},
		{GasLimit: 200000},
		{GasLimit: 50000},
	}
	want := uint64(350000)
	if got := SummaryTotalGas(entries); got != want {
		t.Errorf("SummaryTotalGas() = %d, want %d", got, want)
	}
}

func TestSummaryTotalGas_Empty(t *testing.T) {
	if got := SummaryTotalGas(nil); got != 0 {
		t.Errorf("SummaryTotalGas(nil) = %d, want 0", got)
	}
}

func TestILConstants(t *testing.T) {
	if MaxILCommitteeSize != 16 {
		t.Errorf("MaxILCommitteeSize = %d, want 16", MaxILCommitteeSize)
	}
	if ILExpirySlots != 2 {
		t.Errorf("ILExpirySlots = %d, want 2", ILExpirySlots)
	}
	if MaxILPerSlot != 16 {
		t.Errorf("MaxILPerSlot = %d, want 16", MaxILPerSlot)
	}
	if MaxTransactionsPerInclusionList != 16 {
		t.Errorf("MaxTransactionsPerInclusionList = %d, want 16", MaxTransactionsPerInclusionList)
	}
}
