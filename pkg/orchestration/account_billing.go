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
	"errors"
	"math"
	"math/bits"
	"strconv"
	"time"
)

// =============================================================================
// How an account is billed — go-quicbaas#1058, ruling F4
// =============================================================================
//
// Every recorded turn carries two money figures. The LIST-PRICE EQUIVALENT is
// what its tokens would cost on the provider's metered API (PriceBook.Stamp);
// it is the business insight for a factory that runs on subscriptions. The
// BILLED figure is what the turn adds to the bill the operator actually pays,
// and that depends on how the paying account is billed, which is a fact about
// the account and not about the model:
//
//   - subscription: a flat fee. A turn adds nothing to the bill, so its billed
//     figure is zero. The fee is recorded on the account and PRO-RATED OVER A
//     REPORT'S RANGE, per account (AccountBilling.FeeShare) — never smeared
//     across turns, where it would depend on how many turns happened.
//   - api: metered. The bill is the list price.
//   - local (never set on an account): local inference on the operator's own
//     hardware — a turn costed from electricity, or priced by a table entry
//     whose provider is "local". Its bill is that cost.
//   - unpooled (never set on an account): any other turn that drew on no
//     pooled account — a cloud CLI on the host's own credentials, say. Nobody
//     told QuicBaaS how that login is billed, so its bill is UNKNOWN.
//
// An account whose billing nobody configured bills UNKNOWN, never zero:
// inventing "subscription" would claim a fact about someone's plan.

// BillingMode is how the account that paid for a turn is billed.
type BillingMode string

const (
	// BillingSubscription is a flat-fee plan: a turn adds nothing to the bill.
	BillingSubscription BillingMode = "subscription"

	// BillingAPI is a metered API key: a turn is billed at list price.
	BillingAPI BillingMode = "api"

	// BillingLocal marks local inference on the operator's own hardware,
	// billed at its electricity or configured hardware cost. It is stamped on
	// ledger entries and is never an account's mode.
	BillingLocal BillingMode = "local"

	// BillingUnpooled marks any other turn that drew on no pooled account: a
	// cloud CLI on the host's own credentials. Its bill is unknown. It is
	// stamped on ledger entries and is never an account's mode.
	BillingUnpooled BillingMode = "unpooled"
)

// validBillingModes are the modes a ledger entry may carry.
var validBillingModes = map[BillingMode]struct{}{
	BillingSubscription: {},
	BillingAPI:          {},
	BillingLocal:        {},
	BillingUnpooled:     {},
}

// validate refuses a mode that is not one of validBillingModes.
func (m BillingMode) validate(field string) error {
	if _, ok := validBillingModes[m]; ok {
		return nil
	}
	return &ConsumptionInvalidError{
		Field:   field,
		Message: "must be one of subscription, api, unpooled; got " + string(m),
	}
}

// AccountBilling is how a pooled account is billed. The zero value is
// "not configured", which bills every turn as unknown.
type AccountBilling struct {
	// Mode is subscription or api. Empty means not configured.
	Mode BillingMode `json:"mode,omitempty"`

	// MonthlyFeeMicros is a subscription's flat monthly fee in millionths of
	// a US dollar. A tenant-level spend report pro-rates it over its range
	// (FeeShare). Zero with a subscription means the fee is not configured.
	// Refused on an api account, whose bill is its usage.
	MonthlyFeeMicros int64 `json:"monthly_fee_micros,omitempty"`
}

// Validate refuses a billing an operator cannot have meant.
func (b AccountBilling) Validate() error {
	switch b.Mode {
	case "", BillingSubscription, BillingAPI:
	default:
		return &AccountBillingInvalidError{Field: "billing.mode",
			Message: "must be subscription or api; got " + string(b.Mode)}
	}
	if b.MonthlyFeeMicros < 0 {
		return &AccountBillingInvalidError{Field: "billing.monthly_fee_micros",
			Message: "must not be negative; got " + strconv.FormatInt(b.MonthlyFeeMicros, 10)}
	}
	if b.MonthlyFeeMicros > 0 && b.Mode != BillingSubscription {
		return &AccountBillingInvalidError{Field: "billing.monthly_fee_micros",
			Message: "applies only to a subscription account; an api account is billed by its usage"}
	}
	return nil
}

// billingMonth is the month a monthly fee is pro-rated by: the mean Gregorian
// month, 365.2425 / 12 days. A calendar month would make one fee a different
// daily rate in February and in March, and a report spanning both would have
// no single answer.
const billingMonth = 2629746 * time.Second

// FeeShare is the part of a subscription's monthly fee that falls in
// [from, to), in millionths of a US dollar, rounded down. The second result is
// false when there is no fee to share — the account is not a subscription, or
// its fee is not configured — which a reader renders as a dash, never as zero.
//
// Integer arithmetic over a 128-bit product: money is int64 micros everywhere
// in the ledger, and a fee multiplied by a year of nanoseconds overflows 64
// bits.
func (b AccountBilling) FeeShare(from, to time.Time) (int64, bool) {
	if b.Mode != BillingSubscription || b.MonthlyFeeMicros <= 0 {
		return 0, false
	}
	span := to.Sub(from)
	if span <= 0 {
		return 0, true
	}
	hi, lo := bits.Mul64(uint64(b.MonthlyFeeMicros), uint64(span))
	// Div64 panics when the quotient does not fit 64 bits. That needs a fee
	// and a range whose share exceeds every int64 anyway — millions of years
	// of a trillion-dollar fee — so the share saturates rather than panics.
	if hi >= uint64(billingMonth) {
		return math.MaxInt64, true
	}
	// Rounded down: the discarded remainder is a fraction of a micro-dollar.
	share, _ := bits.Div64(hi, lo, uint64(billingMonth))
	if share > math.MaxInt64 {
		return math.MaxInt64, true
	}
	return int64(share), true
}

// stampBilling sets entry's billing mode and billed figure from how the
// paying account is billed. It runs after the entry is priced.
//
// AN UNPOOLED TURN IS BILLED ONLY WHEN IT IS IDENTIFIED AS LOCAL. Unpooled
// sessions include cloud CLIs on the host's own login (accounts.md), whose
// plan nobody configured; billing those at list price would claim a bill
// nobody knows (go-quicbaas#1058 review).
func stampBilling(entry *ConsumptionEntry, billing AccountBilling) {
	switch {
	case !entry.Pooled() && entry.Local():
		entry.Billing = BillingLocal
	case !entry.Pooled():
		entry.Billing = BillingUnpooled
	default:
		entry.Billing = billing.Mode
	}
	switch entry.Billing {
	case BillingAPI, BillingLocal:
		entry.BilledMicros = entry.EquivalentMicros
	default:
		entry.BilledMicros = 0
	}
}

// ErrAccountBillingInvalid is the sentinel for a refused billing
// configuration.
var ErrAccountBillingInvalid = errors.New("orchestration: invalid account billing")

// AccountBillingInvalidError is an account billing configuration refused at
// the write.
type AccountBillingInvalidError struct {
	Field   string
	Message string
}

// Error implements the error interface.
func (e *AccountBillingInvalidError) Error() string {
	return "orchestration: invalid " + e.Field + ": " + e.Message
}

// Unwrap returns ErrAccountBillingInvalid.
func (e *AccountBillingInvalidError) Unwrap() error { return ErrAccountBillingInvalid }
