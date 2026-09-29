// Copyright (c) 2026 Jeremy Hahn
// Copyright (c) 2026 Automate The Things, LLC
//
// This file is part of go-quicbaas.
//
// go-quicbaas is dual-licensed:
//
// 1. GNU Affero General Public License v3.0 (AGPL-3.0)
//    See LICENSE file or visit https://www.gnu.org/licenses/agpl-3.0.html
//
// 2. Commercial License
//    Contact licensing@automatethethings.com for commercial licensing options.

package orchestration

import (
	"context"
	"errors"
	"testing"

	"github.com/jeremyhahn/go-quicbaas/pkg/config"
)

func TestAccountBillingValidate(t *testing.T) {
	tests := []struct {
		name    string
		billing AccountBilling
		field   string
	}{
		{"not configured", AccountBilling{}, ""},
		{"subscription with a fee", AccountBilling{Mode: BillingSubscription, MonthlyFeeMicros: 200_000_000}, ""},
		{"subscription without a fee", AccountBilling{Mode: BillingSubscription}, ""},
		{"api", AccountBilling{Mode: BillingAPI}, ""},
		{"unknown mode", AccountBilling{Mode: "prepaid"}, "billing.mode"},
		{"unpooled is never an account's mode", AccountBilling{Mode: BillingUnpooled}, "billing.mode"},
		{"negative fee", AccountBilling{Mode: BillingSubscription, MonthlyFeeMicros: -1}, "billing.monthly_fee_micros"},
		{"a fee on a metered key", AccountBilling{Mode: BillingAPI, MonthlyFeeMicros: 5}, "billing.monthly_fee_micros"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.billing.Validate()
			if tc.field == "" {
				if err != nil {
					t.Fatalf("Validate = %v, want nil", err)
				}
				return
			}
			var invalid *AccountBillingInvalidError
			if !errors.As(err, &invalid) || invalid.Field != tc.field || !errors.Is(err, ErrAccountBillingInvalid) {
				t.Fatalf("Validate = %v, want an AccountBillingInvalidError on %s", err, tc.field)
			}
		})
	}
}

func TestAccountStoreRefusesAnInvalidBilling(t *testing.T) {
	store := NewInMemoryAccountStore()
	err := store.Create(context.Background(), &Account{
		ID: "a1", TenantID: "t1", Provider: AccountProviderClaude,
		Billing: AccountBilling{Mode: BillingAPI, MonthlyFeeMicros: 5},
	})
	if !errors.Is(err, ErrAccountBillingInvalid) {
		t.Fatalf("Create = %v, want ErrAccountBillingInvalid", err)
	}
}

func TestStampBilling(t *testing.T) {
	priced := ConsumptionEntry{AccountID: "a1", InputTokens: 10, EquivalentMicros: 700, PriceRef: "v1:default:*/m"}
	unpriced := ConsumptionEntry{AccountID: "a1", InputTokens: 10}
	tests := []struct {
		name       string
		entry      ConsumptionEntry
		billing    AccountBilling
		wantMode   BillingMode
		wantBilled int64
		wantKnown  bool
	}{
		{"a subscription turn adds nothing to the bill", priced, AccountBilling{Mode: BillingSubscription}, BillingSubscription, 0, true},
		{"even an unpriced subscription turn is known", unpriced, AccountBilling{Mode: BillingSubscription}, BillingSubscription, 0, true},
		{"a metered key bills the list price", priced, AccountBilling{Mode: BillingAPI}, BillingAPI, 700, true},
		{"an unpriced metered turn bills unknown", unpriced, AccountBilling{Mode: BillingAPI}, BillingAPI, 0, false},
		{"an unconfigured account bills unknown", priced, AccountBilling{}, "", 0, false},
		// go-quicbaas#1058 review: an unpooled turn is billed only when it is
		// identified as local inference; a cloud CLI on the host's own login
		// bills unknown.
		{"a local turn priced by a local entry bills its rate", ConsumptionEntry{InputTokens: 10, EquivalentMicros: 30, PriceRef: "r", Provider: "local"},
			AccountBilling{}, BillingLocal, 30, true},
		{"a local turn costed from electricity bills its electricity", ConsumptionEntry{EquivalentMicros: 12, PriceRef: "r", PriceBasis: PriceBasisElectricity, Provider: "local"},
			AccountBilling{}, BillingLocal, 12, true},
		{"a local turn of unknown duration bills unknown", ConsumptionEntry{InputTokens: 10, PriceBasis: PriceBasisElectricity, Provider: "local"},
			AccountBilling{}, BillingLocal, 0, false},
		{"an unpooled cloud turn on host credentials bills unknown", ConsumptionEntry{InputTokens: 10, EquivalentMicros: 700, PriceRef: "r", Provider: "anthropic"},
			AccountBilling{}, BillingUnpooled, 0, false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			entry := tc.entry
			stampBilling(&entry, tc.billing)
			if entry.Billing != tc.wantMode || entry.BilledMicros != tc.wantBilled || entry.BilledKnown() != tc.wantKnown {
				t.Fatalf("entry = mode %q billed %d known %t, want %q %d %t",
					entry.Billing, entry.BilledMicros, entry.BilledKnown(), tc.wantMode, tc.wantBilled, tc.wantKnown)
			}
		})
	}
}

// TestRecordConsumptionStampsBothFigures is the ruling-F4 regression at the
// recording site: a subscription turn carries its list-price equivalent AND a
// known billed figure of zero; the same turn on a metered key bills the
// equivalent.
func TestRecordConsumptionStampsBothFigures(t *testing.T) {
	svc, ledger, _ := newConsumptionRecorder(t)
	ctx := ContextWithTenant(context.Background(), "t1")
	accounts := NewInMemoryAccountStore()
	for id, mode := range map[string]BillingMode{"sub": BillingSubscription, "key": BillingAPI} {
		if err := accounts.Create(ctx, &Account{ID: id, TenantID: "t1", Provider: AccountProviderClaude,
			Billing: AccountBilling{Mode: mode}}); err != nil {
			t.Fatalf("Create %s: %v", id, err)
		}
	}
	svc.SetAccountStore(accounts)
	book, err := NewPriceBook(config.PricingConfig{Version: "v1", Models: []config.ModelPriceConfig{
		{Model: "m", Provider: "anthropic", InputPerMTok: 5},
	}})
	if err != nil {
		t.Fatalf("NewPriceBook: %v", err)
	}
	svc.SetPriceBook(book)
	pooledSession(svc, "s-sub", "t1", "p1", "sub")
	pooledSession(svc, "s-key", "t1", "p1", "key")

	for _, session := range []string{"s-sub", "s-key"} {
		svc.recordTurnUsage(ctx, completedTurn{
			turnID: "turn-" + session, sessionID: session, projectID: "p1", engine: "claude", model: "m",
		}, &TokenUsage{InputTokens: 1_000_000})
	}

	rollup, err := ledger.Rollup(ctx, ConsumptionScope{TenantID: "t1"})
	if err != nil {
		t.Fatalf("Rollup: %v", err)
	}
	if rollup.EquivalentMicros != 10_000_000 {
		t.Fatalf("EquivalentMicros = %d, want $10 — both turns at list price", rollup.EquivalentMicros)
	}
	if rollup.BilledMicros != 5_000_000 || rollup.BilledEntries != 2 || rollup.UnbilledEntries != 0 {
		t.Fatalf("billed = %d over %d entries (%d unbilled), want $5 over 2 — the subscription turn bills zero",
			rollup.BilledMicros, rollup.BilledEntries, rollup.UnbilledEntries)
	}
	if sub := rollup.ByAccount["sub"]; sub == nil || sub.BilledMicros != 0 || sub.EquivalentMicros != 5_000_000 {
		t.Fatalf("subscription account row = %+v, want $5 equivalent and $0 billed", sub)
	}
}

// TestRecordConsumptionBillsUnknownWhenTheAccountCannotBeRead asserts a store
// fault leaves the billed figure unknown rather than guessed, and still
// records the turn.
func TestRecordConsumptionBillsUnknownWhenTheAccountCannotBeRead(t *testing.T) {
	svc, ledger, log := newConsumptionRecorder(t)
	ctx := ContextWithTenant(context.Background(), "t1")
	svc.SetAccountStore(NewInMemoryAccountStore()) // the account is not in it
	pooledSession(svc, "s1", "t1", "p1", "ghost")

	svc.recordTurnUsage(ctx, completedTurn{
		turnID: "turn-1", sessionID: "s1", projectID: "p1", engine: "claude", model: "m",
	}, &TokenUsage{InputTokens: 10})

	rollup, err := ledger.Rollup(ctx, ConsumptionScope{TenantID: "t1"})
	if err != nil {
		t.Fatalf("Rollup: %v", err)
	}
	if rollup.Entries != 1 || rollup.BilledEntries != 0 || rollup.UnbilledEntries != 1 {
		t.Fatalf("rollup = %d entries, %d billed, %d unbilled; want the turn recorded with an unknown bill",
			rollup.Entries, rollup.BilledEntries, rollup.UnbilledEntries)
	}
	if len(log.warnings()) == 0 {
		t.Fatal("the unreadable account was not reported")
	}
}
