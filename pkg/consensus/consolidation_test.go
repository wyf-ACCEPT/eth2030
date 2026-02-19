package consensus

import (
	"testing"

	"github.com/eth2028/eth2028/core/types"
)

func makeCompoundingCreds() [32]byte {
	var creds [32]byte
	creds[0] = CompoundingWithdrawalPrefix // 0x02
	creds[1] = 0xAA
	return creds
}

func makeNonCompoundingCreds() [32]byte {
	var creds [32]byte
	creds[0] = 0x01
	creds[1] = 0xAA
	return creds
}

func makeValidator(pubkey byte, balance uint64, creds [32]byte, activation, exit Epoch, slashed bool) *ValidatorBalance {
	return &ValidatorBalance{
		Pubkey:                [48]byte{pubkey},
		WithdrawalCredentials: creds,
		EffectiveBalance:      balance,
		Slashed:               slashed,
		ActivationEpoch:       activation,
		ExitEpoch:             exit,
	}
}

func TestValidateConsolidation(t *testing.T) {
	creds := makeCompoundingCreds()
	currentEpoch := Epoch(100)

	tests := []struct {
		name   string
		source *ValidatorBalance
		target *ValidatorBalance
		err    error
	}{
		{
			name:   "valid consolidation",
			source: makeValidator(1, 32*GweiPerETH, creds, 0, FarFutureEpoch, false),
			target: makeValidator(2, 32*GweiPerETH, creds, 0, FarFutureEpoch, false),
			err:    nil,
		},
		{
			name:   "same validator",
			source: makeValidator(1, 32*GweiPerETH, creds, 0, FarFutureEpoch, false),
			target: makeValidator(1, 32*GweiPerETH, creds, 0, FarFutureEpoch, false),
			err:    ErrConsolidationSameValidator,
		},
		{
			name:   "source not active - not yet activated",
			source: makeValidator(1, 32*GweiPerETH, creds, 200, FarFutureEpoch, false),
			target: makeValidator(2, 32*GweiPerETH, creds, 0, FarFutureEpoch, false),
			err:    ErrConsolidationSourceNotActive,
		},
		{
			name:   "source not active - already exited",
			source: makeValidator(1, 32*GweiPerETH, creds, 0, 50, false),
			target: makeValidator(2, 32*GweiPerETH, creds, 0, FarFutureEpoch, false),
			err:    ErrConsolidationSourceNotActive,
		},
		{
			name:   "target not active",
			source: makeValidator(1, 32*GweiPerETH, creds, 0, FarFutureEpoch, false),
			target: makeValidator(2, 32*GweiPerETH, creds, 200, FarFutureEpoch, false),
			err:    ErrConsolidationTargetNotActive,
		},
		{
			name:   "source slashed",
			source: makeValidator(1, 32*GweiPerETH, creds, 0, FarFutureEpoch, true),
			target: makeValidator(2, 32*GweiPerETH, creds, 0, FarFutureEpoch, false),
			err:    ErrConsolidationSourceSlashed,
		},
		{
			name:   "target slashed",
			source: makeValidator(1, 32*GweiPerETH, creds, 0, FarFutureEpoch, false),
			target: makeValidator(2, 32*GweiPerETH, creds, 0, FarFutureEpoch, true),
			err:    ErrConsolidationTargetSlashed,
		},
		{
			name:   "credentials mismatch",
			source: makeValidator(1, 32*GweiPerETH, makeCompoundingCreds(), 0, FarFutureEpoch, false),
			target: func() *ValidatorBalance {
				c := makeCompoundingCreds()
				c[1] = 0xBB // different
				return makeValidator(2, 32*GweiPerETH, c, 0, FarFutureEpoch, false)
			}(),
			err: ErrConsolidationCredentialsMismatch,
		},
		{
			name:   "target not compounding",
			source: makeValidator(1, 32*GweiPerETH, makeNonCompoundingCreds(), 0, FarFutureEpoch, false),
			target: makeValidator(2, 32*GweiPerETH, makeNonCompoundingCreds(), 0, FarFutureEpoch, false),
			err:    ErrConsolidationNotCompounding,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateConsolidation(tt.source, tt.target, currentEpoch)
			if err != tt.err {
				t.Errorf("ValidateConsolidation() error = %v, want %v", err, tt.err)
			}
		})
	}
}

func TestProcessConsolidation(t *testing.T) {
	creds := makeCompoundingCreds()
	currentEpoch := Epoch(100)

	t.Run("basic consolidation", func(t *testing.T) {
		source := makeValidator(1, 32*GweiPerETH, creds, 0, FarFutureEpoch, false)
		target := makeValidator(2, 32*GweiPerETH, creds, 0, FarFutureEpoch, false)

		result, newSourceBal, newTargetBal, err := ProcessConsolidation(
			source, target,
			32*GweiPerETH, 32*GweiPerETH,
			currentEpoch,
		)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if result.AmountTransferred != 32*GweiPerETH {
			t.Errorf("amount transferred = %d, want %d", result.AmountTransferred, 32*GweiPerETH)
		}
		if newSourceBal != 0 {
			t.Errorf("new source balance = %d, want 0", newSourceBal)
		}
		if newTargetBal != 64*GweiPerETH {
			t.Errorf("new target balance = %d, want %d", newTargetBal, 64*GweiPerETH)
		}
		// Source should be marked for exit.
		if source.ExitEpoch != currentEpoch+1 {
			t.Errorf("source exit epoch = %d, want %d", source.ExitEpoch, currentEpoch+1)
		}
		if source.EffectiveBalance != 0 {
			t.Errorf("source effective balance = %d, want 0", source.EffectiveBalance)
		}
		if target.EffectiveBalance != 64*GweiPerETH {
			t.Errorf("target effective balance = %d, want %d", target.EffectiveBalance, 64*GweiPerETH)
		}
	})

	t.Run("consolidation capped at max", func(t *testing.T) {
		source := makeValidator(1, 1500*GweiPerETH, creds, 0, FarFutureEpoch, false)
		target := makeValidator(2, 1500*GweiPerETH, creds, 0, FarFutureEpoch, false)

		_, _, newTargetBal, err := ProcessConsolidation(
			source, target,
			1500*GweiPerETH, 1500*GweiPerETH,
			currentEpoch,
		)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		// Actual balance goes to 3000 ETH.
		if newTargetBal != 3000*GweiPerETH {
			t.Errorf("new target balance = %d, want %d", newTargetBal, 3000*GweiPerETH)
		}

		// Effective balance capped at MaxEffectiveBalance.
		if target.EffectiveBalance != MaxEffectiveBalance {
			t.Errorf("target effective balance = %d, want %d (max)", target.EffectiveBalance, MaxEffectiveBalance)
		}
	})

	t.Run("result pubkeys match", func(t *testing.T) {
		source := makeValidator(0xAA, 32*GweiPerETH, creds, 0, FarFutureEpoch, false)
		target := makeValidator(0xBB, 32*GweiPerETH, creds, 0, FarFutureEpoch, false)

		result, _, _, err := ProcessConsolidation(
			source, target,
			32*GweiPerETH, 32*GweiPerETH,
			currentEpoch,
		)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if result.SourcePubkey[0] != 0xAA {
			t.Errorf("source pubkey[0] = %x, want 0xAA", result.SourcePubkey[0])
		}
		if result.TargetPubkey[0] != 0xBB {
			t.Errorf("target pubkey[0] = %x, want 0xBB", result.TargetPubkey[0])
		}
	})
}

func TestConsolidationRequestEIP7685Roundtrip(t *testing.T) {
	req := &types.ConsolidationRequest{
		SourceAddress: types.HexToAddress("0x1111111111111111111111111111111111111111"),
		SourcePubkey:  [48]byte{1, 2, 3},
		TargetPubkey:  [48]byte{4, 5, 6},
	}

	// Convert to EIP-7685 request.
	r := ConsolidationRequestToEIP7685(req)
	if r.Type != types.ConsolidationRequestType {
		t.Errorf("request type = %d, want %d", r.Type, types.ConsolidationRequestType)
	}

	// Convert back.
	decoded, err := EIP7685ToConsolidationRequest(r)
	if err != nil {
		t.Fatalf("decode error: %v", err)
	}

	if decoded.SourceAddress != req.SourceAddress {
		t.Errorf("source address mismatch")
	}
	if decoded.SourcePubkey != req.SourcePubkey {
		t.Errorf("source pubkey mismatch")
	}
	if decoded.TargetPubkey != req.TargetPubkey {
		t.Errorf("target pubkey mismatch")
	}
}

func TestEIP7685ToConsolidationRequest_WrongType(t *testing.T) {
	r := types.NewRequest(types.DepositRequestType, []byte{1, 2, 3})
	_, err := EIP7685ToConsolidationRequest(r)
	if err == nil {
		t.Error("expected error for wrong request type")
	}
}
