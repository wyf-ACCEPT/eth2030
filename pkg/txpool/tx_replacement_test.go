package txpool

import (
	"math/big"
	"testing"

	"github.com/eth2030/eth2030/core/types"
)

// makeReplaceLegacyTx creates a legacy transaction for replacement tests.
func makeReplaceLegacyTx(nonce uint64, gasPrice int64) *types.Transaction {
	return types.NewTransaction(&types.LegacyTx{
		Nonce:    nonce,
		GasPrice: big.NewInt(gasPrice),
		Gas:      21000,
		Value:    big.NewInt(0),
	})
}

// makeReplaceDynTx creates a dynamic fee transaction for replacement tests.
func makeReplaceDynTx(nonce uint64, tipCap, feeCap int64) *types.Transaction {
	return types.NewTransaction(&types.DynamicFeeTx{
		ChainID:   big.NewInt(1),
		Nonce:     nonce,
		GasTipCap: big.NewInt(tipCap),
		GasFeeCap: big.NewInt(feeCap),
		Gas:       21000,
		Value:     big.NewInt(0),
	})
}

func TestDefaultReplacementPolicy(t *testing.T) {
	p := DefaultReplacementPolicy()
	if p.MinPriceBump != DefaultMinPriceBump {
		t.Errorf("MinPriceBump = %d, want %d", p.MinPriceBump, DefaultMinPriceBump)
	}
	if p.MaxPoolSize != DefaultMaxPoolSize {
		t.Errorf("MaxPoolSize = %d, want %d", p.MaxPoolSize, DefaultMaxPoolSize)
	}
	if p.AccountSlots != DefaultAccountSlots {
		t.Errorf("AccountSlots = %d, want %d", p.AccountSlots, DefaultAccountSlots)
	}
}

func TestNewReplacementPolicyDefaults(t *testing.T) {
	p := NewReplacementPolicy(0, -1, 0)
	if p.MinPriceBump != DefaultMinPriceBump {
		t.Errorf("MinPriceBump fallback = %d, want %d", p.MinPriceBump, DefaultMinPriceBump)
	}
	if p.MaxPoolSize != DefaultMaxPoolSize {
		t.Errorf("MaxPoolSize fallback = %d, want %d", p.MaxPoolSize, DefaultMaxPoolSize)
	}
	if p.AccountSlots != DefaultAccountSlots {
		t.Errorf("AccountSlots fallback = %d, want %d", p.AccountSlots, DefaultAccountSlots)
	}
}

func TestNewReplacementPolicyCustom(t *testing.T) {
	p := NewReplacementPolicy(20, 8192, 32)
	if p.MinPriceBump != 20 {
		t.Errorf("MinPriceBump = %d, want 20", p.MinPriceBump)
	}
	if p.MaxPoolSize != 8192 {
		t.Errorf("MaxPoolSize = %d, want 8192", p.MaxPoolSize)
	}
	if p.AccountSlots != 32 {
		t.Errorf("AccountSlots = %d, want 32", p.AccountSlots)
	}
}

func TestCanReplace_NilTransactions(t *testing.T) {
	p := DefaultReplacementPolicy()

	_, err := p.CanReplace(nil, makeReplaceLegacyTx(0, 100))
	if err != ErrNilTransaction {
		t.Errorf("nil existing: got %v, want ErrNilTransaction", err)
	}

	_, err = p.CanReplace(makeReplaceLegacyTx(0, 100), nil)
	if err != ErrNilTransaction {
		t.Errorf("nil new: got %v, want ErrNilTransaction", err)
	}
}

func TestCanReplace_NonceMismatch(t *testing.T) {
	p := DefaultReplacementPolicy()
	_, err := p.CanReplace(makeReplaceLegacyTx(0, 100), makeReplaceLegacyTx(1, 200))
	if err != ErrNonceMismatch {
		t.Errorf("got %v, want ErrNonceMismatch", err)
	}
}

func TestCanReplace_LegacySufficientBump(t *testing.T) {
	p := DefaultReplacementPolicy()

	// 10% bump: 100 -> 110 should succeed.
	ok, err := p.CanReplace(makeReplaceLegacyTx(5, 100), makeReplaceLegacyTx(5, 110))
	if err != nil || !ok {
		t.Errorf("110/100 (10%% bump): ok=%v, err=%v", ok, err)
	}

	// 20% bump should also succeed.
	ok, err = p.CanReplace(makeReplaceLegacyTx(5, 100), makeReplaceLegacyTx(5, 120))
	if err != nil || !ok {
		t.Errorf("120/100 (20%% bump): ok=%v, err=%v", ok, err)
	}
}

func TestCanReplace_LegacyInsufficientBump(t *testing.T) {
	p := DefaultReplacementPolicy()

	// 9% bump: 100 -> 109 should fail.
	ok, err := p.CanReplace(makeReplaceLegacyTx(5, 100), makeReplaceLegacyTx(5, 109))
	if ok || err != ErrInsufficientBump {
		t.Errorf("109/100 (9%% bump): ok=%v, err=%v", ok, err)
	}

	// Same price should fail.
	ok, err = p.CanReplace(makeReplaceLegacyTx(5, 100), makeReplaceLegacyTx(5, 100))
	if ok || err != ErrInsufficientBump {
		t.Errorf("100/100 (0%% bump): ok=%v, err=%v", ok, err)
	}

	// Lower price should fail.
	ok, err = p.CanReplace(makeReplaceLegacyTx(5, 100), makeReplaceLegacyTx(5, 90))
	if ok || err != ErrInsufficientBump {
		t.Errorf("90/100 (negative bump): ok=%v, err=%v", ok, err)
	}
}

func TestCanReplace_DynamicFeeSufficientBump(t *testing.T) {
	p := DefaultReplacementPolicy()

	// Both fee cap and tip cap have >= 10% bump.
	ok, err := p.CanReplace(
		makeReplaceDynTx(3, 50, 100),
		makeReplaceDynTx(3, 55, 110),
	)
	if err != nil || !ok {
		t.Errorf("dynamic 10%% bump: ok=%v, err=%v", ok, err)
	}
}

func TestCanReplace_DynamicFeeInsufficientTipBump(t *testing.T) {
	p := DefaultReplacementPolicy()

	// Fee cap has 10% bump but tip cap does not.
	ok, err := p.CanReplace(
		makeReplaceDynTx(3, 50, 100),
		makeReplaceDynTx(3, 54, 110),
	)
	if ok || err != ErrInsufficientTipBump {
		t.Errorf("insufficient tip bump: ok=%v, err=%v", ok, err)
	}
}

func TestComputePriceBump(t *testing.T) {
	tests := []struct {
		name     string
		old, new *types.Transaction
		want     int
	}{
		{"nil old", nil, makeReplaceLegacyTx(0, 100), 0},
		{"nil new", makeReplaceLegacyTx(0, 100), nil, 0},
		{"zero old positive new", makeReplaceLegacyTx(0, 0), makeReplaceLegacyTx(0, 100), 100},
		{"zero both", makeReplaceLegacyTx(0, 0), makeReplaceLegacyTx(0, 0), 0},
		{"10% bump", makeReplaceLegacyTx(0, 100), makeReplaceLegacyTx(0, 110), 10},
		{"50% bump", makeReplaceLegacyTx(0, 100), makeReplaceLegacyTx(0, 150), 50},
		{"100% bump", makeReplaceLegacyTx(0, 100), makeReplaceLegacyTx(0, 200), 100},
		{"no bump", makeReplaceLegacyTx(0, 100), makeReplaceLegacyTx(0, 100), 0},
		{"decrease", makeReplaceLegacyTx(0, 100), makeReplaceLegacyTx(0, 90), 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ComputePriceBump(tt.old, tt.new)
			if got != tt.want {
				t.Errorf("ComputePriceBump = %d, want %d", got, tt.want)
			}
		})
	}
}

func TestCompareEffectiveGasPrice(t *testing.T) {
	baseFee := big.NewInt(10)

	tests := []struct {
		name string
		a, b *types.Transaction
		want int
	}{
		{"both nil", nil, nil, 0},
		{"a nil", nil, makeReplaceLegacyTx(0, 100), -1},
		{"b nil", makeReplaceLegacyTx(0, 100), nil, 1},
		{"a < b", makeReplaceLegacyTx(0, 50), makeReplaceLegacyTx(0, 100), -1},
		{"a == b", makeReplaceLegacyTx(0, 100), makeReplaceLegacyTx(0, 100), 0},
		{"a > b", makeReplaceLegacyTx(0, 100), makeReplaceLegacyTx(0, 50), 1},
		{"dynamic vs dynamic", makeReplaceDynTx(0, 20, 30), makeReplaceDynTx(0, 10, 20), 1},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := CompareEffectiveGasPrice(tt.a, tt.b, baseFee)
			if got != tt.want {
				t.Errorf("CompareEffectiveGasPrice = %d, want %d", got, tt.want)
			}
		})
	}
}

func TestSortByPrice(t *testing.T) {
	txs := []*types.Transaction{
		makeReplaceLegacyTx(0, 10),
		makeReplaceLegacyTx(1, 30),
		makeReplaceLegacyTx(2, 20),
	}

	sorted := SortByPrice(txs, nil)
	if len(sorted) != 3 {
		t.Fatalf("sorted len = %d, want 3", len(sorted))
	}

	// Verify descending order.
	for i := 0; i < len(sorted)-1; i++ {
		pi := sorted[i].GasPrice()
		pj := sorted[i+1].GasPrice()
		if pi.Cmp(pj) < 0 {
			t.Errorf("sorted[%d].GasPrice=%v < sorted[%d].GasPrice=%v", i, pi, i+1, pj)
		}
	}

	// Verify original is not modified.
	if txs[0].GasPrice().Int64() != 10 {
		t.Error("original slice was modified")
	}
}

func TestSortByPriceEmpty(t *testing.T) {
	sorted := SortByPrice(nil, nil)
	if sorted != nil {
		t.Errorf("SortByPrice(nil) = %v, want nil", sorted)
	}
}

func TestEffectiveTip(t *testing.T) {
	tests := []struct {
		name    string
		tx      *types.Transaction
		baseFee *big.Int
		want    int64
	}{
		{"nil tx", nil, big.NewInt(10), 0},
		{"nil basefee legacy", makeReplaceLegacyTx(0, 100), nil, 100},
		{"legacy tip = price - basefee", makeReplaceLegacyTx(0, 100), big.NewInt(40), 60},
		{"legacy basefee > price", makeReplaceLegacyTx(0, 30), big.NewInt(40), 0},
		{"dynamic: tipCap < feeCap-baseFee", makeReplaceDynTx(0, 5, 50), big.NewInt(10), 5},
		{"dynamic: feeCap-baseFee < tipCap", makeReplaceDynTx(0, 50, 20), big.NewInt(10), 10},
		{"dynamic: feeCap < baseFee", makeReplaceDynTx(0, 50, 5), big.NewInt(10), 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := EffectiveTip(tt.tx, tt.baseFee)
			if got.Int64() != tt.want {
				t.Errorf("EffectiveTip = %d, want %d", got.Int64(), tt.want)
			}
		})
	}
}

func TestAccountPending_Executable(t *testing.T) {
	ap := &AccountPending{
		Nonce: 5,
		Transactions: []*types.Transaction{
			makeReplaceLegacyTx(5, 100),
			makeReplaceLegacyTx(6, 100),
			makeReplaceLegacyTx(8, 100), // gap at nonce 7
		},
	}

	exec := ap.Executable()
	if len(exec) != 2 {
		t.Fatalf("Executable len = %d, want 2", len(exec))
	}
	if exec[0].Nonce() != 5 || exec[1].Nonce() != 6 {
		t.Errorf("Executable nonces = [%d, %d], want [5, 6]", exec[0].Nonce(), exec[1].Nonce())
	}
}

func TestAccountPending_ExecutableEmpty(t *testing.T) {
	ap := &AccountPending{Nonce: 5}
	exec := ap.Executable()
	if exec != nil {
		t.Errorf("Executable of empty = %v, want nil", exec)
	}

	var nilAp *AccountPending
	exec = nilAp.Executable()
	if exec != nil {
		t.Errorf("Executable of nil = %v, want nil", exec)
	}
}

func TestGetPromotable(t *testing.T) {
	addr1 := [20]byte{1}
	addr2 := [20]byte{2}

	pending := map[[20]byte]*AccountPending{
		addr1: {
			Nonce: 0,
			Transactions: []*types.Transaction{
				makeReplaceLegacyTx(0, 50),
				makeReplaceLegacyTx(1, 60),
			},
		},
		addr2: {
			Nonce: 10,
			Transactions: []*types.Transaction{
				makeReplaceLegacyTx(10, 100),
			},
		},
	}

	baseFee := big.NewInt(5)
	promotable := GetPromotable(pending, baseFee)

	if len(promotable) != 3 {
		t.Fatalf("GetPromotable len = %d, want 3", len(promotable))
	}

	// Should be sorted by effective gas price descending.
	if promotable[0].GasPrice().Int64() != 100 {
		t.Errorf("first promotable price = %d, want 100", promotable[0].GasPrice().Int64())
	}
}

func TestGetPromotableEmpty(t *testing.T) {
	result := GetPromotable(nil, nil)
	if result != nil {
		t.Errorf("GetPromotable(nil) = %v, want nil", result)
	}
}

func TestFilterByMinTip(t *testing.T) {
	txs := []*types.Transaction{
		makeReplaceLegacyTx(0, 5),
		makeReplaceLegacyTx(1, 15),
		makeReplaceLegacyTx(2, 25),
	}

	baseFee := big.NewInt(10)
	minTip := big.NewInt(5)

	filtered := FilterByMinTip(txs, baseFee, minTip)
	if len(filtered) != 2 {
		t.Fatalf("FilterByMinTip len = %d, want 2", len(filtered))
	}
}

func TestGroupByNonce(t *testing.T) {
	txs := []*types.Transaction{
		makeReplaceLegacyTx(5, 100),
		makeReplaceLegacyTx(5, 110),
		makeReplaceLegacyTx(6, 200),
		nil, // should be skipped
	}

	groups := GroupByNonce(txs)
	if len(groups) != 2 {
		t.Fatalf("GroupByNonce groups = %d, want 2", len(groups))
	}
	if len(groups[5]) != 2 {
		t.Errorf("nonce 5 group len = %d, want 2", len(groups[5]))
	}
	if len(groups[6]) != 1 {
		t.Errorf("nonce 6 group len = %d, want 1", len(groups[6]))
	}
}

func TestBestByNonce(t *testing.T) {
	txs := []*types.Transaction{
		makeReplaceLegacyTx(5, 100),
		makeReplaceLegacyTx(5, 150),
		makeReplaceLegacyTx(6, 200),
		makeReplaceLegacyTx(6, 180),
	}

	best := BestByNonce(txs, nil)
	if len(best) != 2 {
		t.Fatalf("BestByNonce len = %d, want 2", len(best))
	}

	// Should be sorted by nonce ascending.
	if best[0].Nonce() != 5 {
		t.Errorf("best[0] nonce = %d, want 5", best[0].Nonce())
	}
	if best[1].Nonce() != 6 {
		t.Errorf("best[1] nonce = %d, want 6", best[1].Nonce())
	}

	// best[0] should be the highest-priced at nonce 5.
	if best[0].GasPrice().Int64() != 150 {
		t.Errorf("best[0] price = %d, want 150", best[0].GasPrice().Int64())
	}
	// best[1] should be the highest-priced at nonce 6.
	if best[1].GasPrice().Int64() != 200 {
		t.Errorf("best[1] price = %d, want 200", best[1].GasPrice().Int64())
	}
}
